package process

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/uber/cadence/common/clock"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/common/metrics"
	"github.com/uber/cadence/common/types"
	"github.com/uber/cadence/service/sharddistributor/config"
	"github.com/uber/cadence/service/sharddistributor/store"
)

//go:generate mockgen -package $GOPACKAGE -source $GOFILE -destination=process_mock.go Factory,Processor

// Module provides processor factory for fx app.
var Module = fx.Module(
	"leader-process",
	fx.Provide(NewProcessorFactory),
)

// Processor represents a process that runs when the instance is the leader
type Processor interface {
	Run(ctx context.Context) error
	Terminate(ctx context.Context) error
}

// Factory creates processor instances
type Factory interface {
	// CreateProcessor creates a new processor, it takes the generic store
	// and the election object which provides the transactional guard.
	CreateProcessor(cfg config.Namespace, storage store.Store, election store.Election) Processor
}

const (
	_defaultPeriod      = time.Second
	_defaultHearbeatTTL = 10 * time.Second
)

type processorFactory struct {
	logger        log.Logger
	timeSource    clock.TimeSource
	cfg           config.LeaderProcess
	metricsClient metrics.Client
}

type namespaceProcessor struct {
	namespaceCfg        config.Namespace
	logger              log.Logger
	metricsClient       metrics.Client
	timeSource          clock.TimeSource
	running             bool
	cancel              context.CancelFunc
	cfg                 config.LeaderProcess
	wg                  sync.WaitGroup
	shardStore          store.Store
	election            store.Election
	lastAppliedRevision int64
}

// NewProcessorFactory creates a new processor factory
func NewProcessorFactory(
	logger log.Logger,
	metricsClient metrics.Client,
	timeSource clock.TimeSource,
	cfg config.ShardDistribution,
) Factory {
	if cfg.Process.Period == 0 {
		cfg.Process.Period = _defaultPeriod
	}
	if cfg.Process.HeartbeatTTL == 0 {
		cfg.Process.HeartbeatTTL = _defaultHearbeatTTL
	}

	return &processorFactory{
		logger:        logger,
		timeSource:    timeSource,
		cfg:           cfg.Process,
		metricsClient: metricsClient,
	}
}

// CreateProcessor creates a new processor for the given namespace
func (f *processorFactory) CreateProcessor(cfg config.Namespace, shardStore store.Store, election store.Election) Processor {
	return &namespaceProcessor{
		namespaceCfg:  cfg,
		logger:        f.logger.WithTags(tag.ComponentLeaderProcessor, tag.ShardNamespace(cfg.Name)),
		timeSource:    f.timeSource,
		cfg:           f.cfg,
		shardStore:    shardStore,
		election:      election, // Store the election object
		metricsClient: f.metricsClient,
	}
}

// Run begins processing for this namespace
func (p *namespaceProcessor) Run(ctx context.Context) error {
	if p.running {
		return fmt.Errorf("processor is already running")
	}

	pCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.running = true

	p.logger.Info("Starting")

	p.wg.Add(1)
	// Start the process in a goroutine
	go p.runProcess(pCtx)

	return nil
}

// Terminate halts processing for this namespace
func (p *namespaceProcessor) Terminate(ctx context.Context) error {
	if !p.running {
		return fmt.Errorf("processor has not been started")
	}

	p.logger.Info("Stopping")

	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}

	p.running = false

	// Ensure that the process has stopped.
	p.wg.Wait()

	return nil
}

// runProcess launches and manages the independent processing loops.
func (p *namespaceProcessor) runProcess(ctx context.Context) {
	defer p.wg.Done()

	var loopWg sync.WaitGroup
	loopWg.Add(2) // We have two loops to manage.

	// Launch the rebalancing process in its own goroutine.
	go func() {
		defer loopWg.Done()
		p.runRebalancingLoop(ctx)
	}()

	// Launch the heartbeat cleanup process in its own goroutine.
	go func() {
		defer loopWg.Done()
		p.runCleanupLoop(ctx)
	}()

	// Wait for both loops to exit.
	loopWg.Wait()
}

// runRebalancingLoop handles shard assignment and redistribution.
func (p *namespaceProcessor) runRebalancingLoop(ctx context.Context) {
	ticker := p.timeSource.NewTicker(p.cfg.Period)
	defer ticker.Stop()

	// Perform an initial rebalance on startup.
	err := p.rebalanceShards(ctx)
	if err != nil {
		p.logger.Error("initial rebalance failed", tag.Error(err))
	}

	updateChan, err := p.shardStore.Subscribe(ctx, p.namespaceCfg.Name)
	if err != nil {
		p.logger.Error("Failed to subscribe to state changes, stopping rebalancing loop.", tag.Error(err))
		return
	}

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Rebalancing loop cancelled.")
			return
		case latestRevision, ok := <-updateChan:
			if !ok {
				p.logger.Info("Update channel closed, stopping rebalancing loop.")
				return
			}
			if latestRevision <= p.lastAppliedRevision {
				continue
			}
			p.logger.Info("State change detected, triggering rebalance.")
			err = p.rebalanceShards(ctx)
		case <-ticker.Chan():
			p.logger.Info("Periodic reconciliation triggered, rebalancing.")
			err = p.rebalanceShards(ctx)
		}
		if err != nil {
			p.logger.Error("rebalance failed", tag.Error(err))
		}
	}
}

// runCleanupLoop periodically removes stale executors.
func (p *namespaceProcessor) runCleanupLoop(ctx context.Context) {
	ticker := p.timeSource.NewTicker(p.cfg.HeartbeatTTL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Cleanup loop cancelled.")
			return
		case <-ticker.Chan():
			p.logger.Info("Periodic heartbeat cleanup triggered.")
			p.cleanupStaleExecutors(ctx)
		}
	}
}

// cleanupStaleExecutors removes executors who have not reported a heartbeat recently.
func (p *namespaceProcessor) cleanupStaleExecutors(ctx context.Context) {
	namespaceState, err := p.shardStore.GetState(ctx, p.namespaceCfg.Name)
	if err != nil {
		p.logger.Error("Failed to get state for heartbeat cleanup", tag.Error(err))
		return
	}

	var expiredExecutors []string
	now := p.timeSource.Now().Unix()
	heartbeatTTL := int64(p.cfg.HeartbeatTTL.Seconds())

	for executorID, state := range namespaceState.Executors {
		if (now - state.LastHeartbeat) > heartbeatTTL {
			expiredExecutors = append(expiredExecutors, executorID)
		}
	}

	if len(expiredExecutors) == 0 {
		return // Nothing to do.
	}

	p.logger.Info("Removing stale executors", tag.ShardExecutors(expiredExecutors))
	// Use the leader guard for the delete operation.
	if err := p.shardStore.DeleteExecutors(ctx, p.namespaceCfg.Name, expiredExecutors, p.election.Guard()); err != nil {
		p.logger.Error("Failed to delete stale executors", tag.Error(err))
	}
}

// rebalanceShards is the core logic for distributing shards among active executors.
func (p *namespaceProcessor) rebalanceShards(ctx context.Context) (err error) {
	metricsLoopScope := p.metricsClient.Scope(metrics.ShardDistributorAssignLoopScope)
	metricsLoopScope.AddCounter(metrics.ShardDistributorAssignLoopAttempts, 1)
	defer func() {
		if err != nil {
			metricsLoopScope.AddCounter(metrics.ShardDistributorAssignLoopFail, 1)
		} else {
			metricsLoopScope.AddCounter(metrics.ShardDistributorAssignLoopSuccess, 1)
		}
	}()

	start := p.timeSource.Now()
	defer func() {
		metricsLoopScope.RecordHistogramDuration(metrics.ShardDistributorAssignLoopShardRebalanceLatency, p.timeSource.Now().Sub(start))
	}()

	return p.rebalanceShardsImpl(ctx, metricsLoopScope)
}

func (p *namespaceProcessor) rebalanceShardsImpl(ctx context.Context, metricsLoopScope metrics.Scope) (err error) {

	namespaceState, err := p.shardStore.GetState(ctx, p.namespaceCfg.Name)
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}

	if namespaceState.GlobalRevision <= p.lastAppliedRevision {
		return nil
	}
	p.lastAppliedRevision = namespaceState.GlobalRevision

	activeExecutors := p.getActiveExecutors(namespaceState)
	if len(activeExecutors) == 0 {
		p.logger.Warn("No active executors found. Cannot assign shards.")
		return nil
	}

	deletedShards := p.findDeletedShards(namespaceState)
	shardsToReassign, currentAssignments := p.findShardsToReassign(activeExecutors, namespaceState, deletedShards)

	metricsLoopScope.UpdateGauge(metrics.ShardDistributorAssignLoopNumRebalancedShards, float64(len(shardsToReassign)))

	// If there are deleted shards, we have removed them from the shard assignments, so the distribution has changed.
	distributionChanged := len(deletedShards) > 0
	distributionChanged = distributionChanged || assignShardsToEmptyExecutors(currentAssignments)
	distributionChanged = distributionChanged || p.updateAssignments(shardsToReassign, activeExecutors, currentAssignments)

	if !distributionChanged {
		return nil
	}

	p.addAssignmentsToNamespaceState(namespaceState, currentAssignments)

	p.logger.Info("Applying new shard distribution.")
	// Use the leader guard for the assign operation.
	err = p.shardStore.AssignShards(ctx, p.namespaceCfg.Name, store.AssignShardsRequest{
		NewState: namespaceState,
	}, p.election.Guard())
	if err != nil {
		return fmt.Errorf("assign shards: %w", err)
	}

	return nil
}

func (p *namespaceProcessor) findDeletedShards(namespaceState *store.NamespaceState) map[string]store.ShardState {
	deletedShards := make(map[string]store.ShardState)

	for executorID, executor := range namespaceState.Executors {
		for shardID, shardState := range executor.ReportedShards {
			if shardState.Status == types.ShardStatusDONE {
				deletedShards[shardID] = store.ShardState{
					ExecutorID: executorID,
				}
			}
		}
	}
	return deletedShards
}

func (p *namespaceProcessor) findShardsToReassign(activeExecutors []string, namespaceState *store.NamespaceState, deletedShards map[string]store.ShardState) ([]string, map[string][]string) {
	allShards := make(map[string]struct{})
	for _, shardID := range getShards(p.namespaceCfg, namespaceState, deletedShards) {
		allShards[shardID] = struct{}{}
	}

	shardsToReassign := make([]string, 0)
	currentAssignments := make(map[string][]string)

	for _, executorID := range activeExecutors {
		currentAssignments[executorID] = []string{}
	}

	for executorID, state := range namespaceState.ShardAssignments {
		isActive := namespaceState.Executors[executorID].Status == types.ExecutorStatusACTIVE
		for shardID := range state.AssignedShards {
			if _, ok := allShards[shardID]; ok {
				delete(allShards, shardID)
				if isActive {
					currentAssignments[executorID] = append(currentAssignments[executorID], shardID)
				} else {
					shardsToReassign = append(shardsToReassign, shardID)
				}
			}
		}
	}

	for shardID := range allShards {
		shardsToReassign = append(shardsToReassign, shardID)
	}
	return shardsToReassign, currentAssignments
}

func (*namespaceProcessor) updateAssignments(shardsToReassign []string, activeExecutors []string, currentAssignments map[string][]string) (distributionChanged bool) {
	if len(shardsToReassign) == 0 {
		return false
	}

	i := rand.Intn(len(activeExecutors))
	for _, shardID := range shardsToReassign {
		executorID := activeExecutors[i%len(activeExecutors)]
		currentAssignments[executorID] = append(currentAssignments[executorID], shardID)
		i++
	}
	return true
}

func (p *namespaceProcessor) addAssignmentsToNamespaceState(namespaceState *store.NamespaceState, currentAssignments map[string][]string) {
	newState := make(map[string]store.AssignedState)
	for executorID, shards := range currentAssignments {
		assignedShardsMap := make(map[string]*types.ShardAssignment)
		for _, shardID := range shards {
			assignedShardsMap[shardID] = &types.ShardAssignment{Status: types.AssignmentStatusREADY}
		}
		newState[executorID] = store.AssignedState{
			AssignedShards: assignedShardsMap,
			LastUpdated:    p.timeSource.Now().Unix(),
		}
	}

	namespaceState.ShardAssignments = newState
}

func (*namespaceProcessor) getActiveExecutors(namespaceState *store.NamespaceState) []string {
	var activeExecutors []string
	for id, state := range namespaceState.Executors {
		if state.Status == types.ExecutorStatusACTIVE {
			activeExecutors = append(activeExecutors, id)
		}
	}

	sort.Strings(activeExecutors)
	return activeExecutors
}

func assignShardsToEmptyExecutors(currentAssignments map[string][]string) bool {
	emptyExecutors := make([]string, 0)
	executorsWithShards := make([]string, 0)
	minShardsCurrentlyAssigned := 0

	// Ensure the iteration is deterministic.
	executors := make([]string, 0, len(currentAssignments))
	for executorID := range currentAssignments {
		executors = append(executors, executorID)
	}
	slices.Sort(executors)

	for _, executorID := range executors {
		if len(currentAssignments[executorID]) == 0 {
			emptyExecutors = append(emptyExecutors, executorID)
		} else {
			executorsWithShards = append(executorsWithShards, executorID)
			if minShardsCurrentlyAssigned == 0 || len(currentAssignments[executorID]) < minShardsCurrentlyAssigned {
				minShardsCurrentlyAssigned = len(currentAssignments[executorID])
			}
		}
	}

	// If there are no empty executors or no executors with shards, we don't need to do anything.
	if len(emptyExecutors) == 0 || len(executorsWithShards) == 0 {
		return false
	}

	// We calculate the number of shards to assign each of the empty executors. The idea is to assume all current executors have
	// the same number of shards `minShardsCurrentlyAssigned`. We use the minimum so when steeling we don't have to worry about
	// steeling more shards that the executors have.
	// We then calculate the total number of assumed shards `minShardsCurrentlyAssigned * len(executorsWithShards)` and divide it by the
	// number of current executors. This gives us the number of shards per executor, thus the number of shards to assign to each of the
	// empty executors.
	numShardsToAssignEmptyExecutors := minShardsCurrentlyAssigned * len(executorsWithShards) / len(currentAssignments)

	stealRound := 0
	for i := 0; i < numShardsToAssignEmptyExecutors; i++ {
		for _, emptyExecutor := range emptyExecutors {
			executorToSteelFrom := executorsWithShards[stealRound%len(executorsWithShards)]
			stealRound++

			stolenShard := currentAssignments[executorToSteelFrom][0]

			currentAssignments[executorToSteelFrom] = currentAssignments[executorToSteelFrom][1:]
			currentAssignments[emptyExecutor] = append(currentAssignments[emptyExecutor], stolenShard)
		}
	}

	return true
}

func getShards(cfg config.Namespace, namespaceState *store.NamespaceState, deletedShards map[string]store.ShardState) []string {
	if cfg.Type == config.NamespaceTypeFixed {
		return makeShards(cfg.ShardNum)
	} else if cfg.Type == config.NamespaceTypeEphemeral {
		shards := make([]string, 0)
		for _, state := range namespaceState.ShardAssignments {
			for shardID := range state.AssignedShards {
				if _, ok := deletedShards[shardID]; !ok {
					shards = append(shards, shardID)
				}
			}
		}

		return shards
	}
	return nil
}

func makeShards(num int64) []string {
	shards := make([]string, num)
	for i := range num {
		shards[i] = strconv.FormatInt(i, 10)
	}
	return shards
}
