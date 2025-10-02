package process

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/uber/cadence/common/clock"
	"github.com/uber/cadence/common/log/testlogger"
	"github.com/uber/cadence/common/metrics"
	"github.com/uber/cadence/common/types"
	"github.com/uber/cadence/service/sharddistributor/config"
	"github.com/uber/cadence/service/sharddistributor/store"
)

type testDependencies struct {
	ctrl       *gomock.Controller
	store      *store.MockStore
	election   *store.MockElection
	timeSource clock.MockedTimeSource
	factory    Factory
	cfg        config.Namespace
}

func setupProcessorTest(t *testing.T, namespaceType string) *testDependencies {
	ctrl := gomock.NewController(t)
	mockedClock := clock.NewMockedTimeSource()
	return &testDependencies{
		ctrl:       ctrl,
		store:      store.NewMockStore(ctrl),
		election:   store.NewMockElection(ctrl),
		timeSource: mockedClock,
		factory: NewProcessorFactory(
			testlogger.New(t),
			metrics.NewNoopMetricsClient(),
			mockedClock,
			config.ShardDistribution{
				Process: config.LeaderProcess{
					Period:       time.Second,
					HeartbeatTTL: time.Second,
				},
			},
		),
		cfg: config.Namespace{Name: "test-ns", ShardNum: 2, Type: namespaceType},
	}
}

func TestRunAndTerminate(t *testing.T) {
	defer goleak.VerifyNone(t)

	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election)
	ctx, cancel := context.WithCancel(context.Background())

	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{GlobalRevision: 0}, nil).AnyTimes()
	mocks.store.EXPECT().Subscribe(gomock.Any(), mocks.cfg.Name).Return(make(chan int64), nil).AnyTimes()

	err := processor.Run(ctx)
	require.NoError(t, err)

	err = processor.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "processor is already running")

	err = processor.Terminate(context.Background())
	require.NoError(t, err)

	err = processor.Terminate(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "processor has not been started")

	cancel()
}

func TestRebalanceShards_InitialDistribution(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)

	state := map[string]store.HeartbeatState{
		"exec-1": {Status: types.ExecutorStatusACTIVE},
		"exec-2": {Status: types.ExecutorStatusACTIVE},
	}
	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{Executors: state, GlobalRevision: 1}, nil)
	mocks.election.EXPECT().Guard().Return(store.NopGuard())
	mocks.store.EXPECT().AssignShards(gomock.Any(), mocks.cfg.Name, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, request store.AssignShardsRequest, _ store.GuardFunc) error {
			assert.Len(t, request.NewState.ShardAssignments, 2)
			assert.Len(t, request.NewState.ShardAssignments["exec-1"].AssignedShards, 1)
			assert.Len(t, request.NewState.ShardAssignments["exec-2"].AssignedShards, 1)
			return nil
		},
	)

	err := processor.rebalanceShards(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), processor.lastAppliedRevision)
}

func TestRebalanceShards_ExecutorRemoved(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)

	heartbeats := map[string]store.HeartbeatState{
		"exec-1": {Status: types.ExecutorStatusACTIVE},
		"exec-2": {Status: types.ExecutorStatusDRAINING},
	}
	assignments := map[string]store.AssignedState{
		"exec-2": {
			AssignedShards: map[string]*types.ShardAssignment{
				"0": {Status: types.AssignmentStatusREADY},
				"1": {Status: types.AssignmentStatusREADY},
			},
		},
	}
	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{
		Executors:        heartbeats,
		ShardAssignments: assignments,
		GlobalRevision:   1,
	}, nil)
	mocks.election.EXPECT().Guard().Return(store.NopGuard())
	mocks.store.EXPECT().AssignShards(gomock.Any(), mocks.cfg.Name, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, request store.AssignShardsRequest, _ store.GuardFunc) error {
			assert.Len(t, request.NewState.ShardAssignments["exec-1"].AssignedShards, 2)
			assert.Len(t, request.NewState.ShardAssignments["exec-2"].AssignedShards, 0)
			return nil
		},
	)

	err := processor.rebalanceShards(context.Background())
	require.NoError(t, err)
}

func TestRebalanceShards_NoActiveExecutors(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)

	state := map[string]store.HeartbeatState{
		"exec-1": {Status: types.ExecutorStatusDRAINING},
	}
	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{Executors: state, GlobalRevision: int64(1)}, nil)

	err := processor.rebalanceShards(context.Background())
	require.NoError(t, err)
}

func TestRebalanceShards_NoRebalanceNeeded(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)
	processor.lastAppliedRevision = 1

	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{GlobalRevision: int64(1)}, nil)

	err := processor.rebalanceShards(context.Background())
	require.NoError(t, err)
}

func TestCleanupStaleExecutors(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)
	now := mocks.timeSource.Now()

	heartbeats := map[string]store.HeartbeatState{
		"exec-active": {LastHeartbeat: now.Unix()},
		"exec-stale":  {LastHeartbeat: now.Add(-2 * time.Second).Unix()},
	}

	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{Executors: heartbeats}, nil)
	mocks.election.EXPECT().Guard().Return(store.NopGuard())
	mocks.store.EXPECT().DeleteExecutors(gomock.Any(), mocks.cfg.Name, []string{"exec-stale"}, gomock.Any()).Return(nil)

	processor.cleanupStaleExecutors(context.Background())
}

func TestRebalance_StoreErrors(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)
	expectedErr := errors.New("store is down")

	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(nil, expectedErr)
	err := processor.rebalanceShards(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), expectedErr.Error())

	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{
		Executors:      map[string]store.HeartbeatState{"e": {Status: types.ExecutorStatusACTIVE}},
		GlobalRevision: 1,
	}, nil)
	mocks.election.EXPECT().Guard().Return(store.NopGuard())
	mocks.store.EXPECT().AssignShards(gomock.Any(), mocks.cfg.Name, gomock.Any(), gomock.Any()).Return(expectedErr)
	err = processor.rebalanceShards(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), expectedErr.Error())
}

func TestCleanup_StoreErrors(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)
	expectedErr := errors.New("store is down")

	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(nil, expectedErr)
	processor.cleanupStaleExecutors(context.Background())

	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{
		Executors:      map[string]store.HeartbeatState{"stale": {LastHeartbeat: 0}},
		GlobalRevision: 1,
	}, nil)
	mocks.election.EXPECT().Guard().Return(store.NopGuard())
	mocks.store.EXPECT().DeleteExecutors(gomock.Any(), mocks.cfg.Name, gomock.Any(), gomock.Any()).Return(expectedErr)
	processor.cleanupStaleExecutors(context.Background())
}

func TestRunLoop_SubscriptionError(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)

	expectedErr := errors.New("subscription failed")
	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{GlobalRevision: 0}, nil)
	mocks.store.EXPECT().Subscribe(gomock.Any(), mocks.cfg.Name).Return(nil, expectedErr)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		processor.runRebalancingLoop(context.Background())
	}()
	wg.Wait()
}

func TestRunLoop_ContextCancellation(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)
	ctx, cancel := context.WithCancel(context.Background())

	// Setup for the initial call to rebalanceShards and the subscription
	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{GlobalRevision: 0}, nil)
	mocks.store.EXPECT().Subscribe(gomock.Any(), mocks.cfg.Name).Return(make(chan int64), nil)

	processor.wg.Add(1)
	// Run the process in a separate goroutine to avoid blocking the test
	go processor.runProcess(ctx)

	// Wait for the two loops (rebalance and cleanup) to create their tickers
	mocks.timeSource.BlockUntil(2)

	// Now, cancel the context to signal the loops to stop
	cancel()

	// Wait for the main process loop to exit gracefully
	processor.wg.Wait()
}

func TestRebalanceShards_NoShardsToReassign(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)

	heartbeats := map[string]store.HeartbeatState{
		"exec-1": {Status: types.ExecutorStatusACTIVE},
	}
	assignments := map[string]store.AssignedState{
		"exec-1": {
			AssignedShards: map[string]*types.ShardAssignment{
				"0": {Status: types.AssignmentStatusREADY},
				"1": {Status: types.AssignmentStatusREADY},
			},
		},
	}
	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{
		Executors:        heartbeats,
		ShardAssignments: assignments,
		GlobalRevision:   2,
	}, nil)

	err := processor.rebalanceShards(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2), processor.lastAppliedRevision, "Revision should be updated even if no shards were moved")
}

func TestRebalanceShards_WithUnassignedShards(t *testing.T) {
	mocks := setupProcessorTest(t, config.NamespaceTypeFixed)
	defer mocks.ctrl.Finish()
	processor := mocks.factory.CreateProcessor(mocks.cfg, mocks.store, mocks.election).(*namespaceProcessor)

	heartbeats := map[string]store.HeartbeatState{
		"exec-1": {Status: types.ExecutorStatusACTIVE},
	}
	// Note: shard "1" is missing from assignments
	assignments := map[string]store.AssignedState{
		"exec-1": {
			AssignedShards: map[string]*types.ShardAssignment{
				"0": {Status: types.AssignmentStatusREADY},
			},
		},
	}
	mocks.store.EXPECT().GetState(gomock.Any(), mocks.cfg.Name).Return(&store.NamespaceState{
		Executors:        heartbeats,
		ShardAssignments: assignments,
		GlobalRevision:   3,
	}, nil)
	mocks.election.EXPECT().Guard().Return(store.NopGuard())
	mocks.store.EXPECT().AssignShards(gomock.Any(), mocks.cfg.Name, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, request store.AssignShardsRequest, _ store.GuardFunc) error {
			assert.Len(t, request.NewState.ShardAssignments["exec-1"].AssignedShards, 2, "Both shards should now be assigned to exec-1")
			return nil
		},
	)

	err := processor.rebalanceShards(context.Background())
	require.NoError(t, err)
}

func TestGetShards_Utility(t *testing.T) {
	t.Run("Fixed type", func(t *testing.T) {
		cfg := config.Namespace{Type: config.NamespaceTypeFixed, ShardNum: 5}
		shards := getShards(cfg, nil, nil)
		assert.Equal(t, []string{"0", "1", "2", "3", "4"}, shards)
	})

	t.Run("Ephemeral type", func(t *testing.T) {
		cfg := config.Namespace{Type: config.NamespaceTypeEphemeral}
		nsState := &store.NamespaceState{
			ShardAssignments: map[string]store.AssignedState{
				"executor1": {
					AssignedShards: map[string]*types.ShardAssignment{
						"s0": {Status: types.AssignmentStatusREADY},
						"s1": {Status: types.AssignmentStatusREADY},
						"s2": {Status: types.AssignmentStatusREADY},
					},
				},
				"executor2": {
					AssignedShards: map[string]*types.ShardAssignment{
						"s3": {Status: types.AssignmentStatusREADY},
						"s4": {Status: types.AssignmentStatusREADY},
					},
				},
			},
		}
		shards := getShards(cfg, nsState, nil)
		slices.Sort(shards)
		assert.Equal(t, []string{"s0", "s1", "s2", "s3", "s4"}, shards)
	})

	t.Run("Ephemeral type with deleted shards", func(t *testing.T) {
		cfg := config.Namespace{Type: config.NamespaceTypeEphemeral}
		nsState := &store.NamespaceState{
			ShardAssignments: map[string]store.AssignedState{
				"executor1": {
					AssignedShards: map[string]*types.ShardAssignment{
						"s0": {Status: types.AssignmentStatusREADY},
						"s1": {Status: types.AssignmentStatusREADY},
						"s2": {Status: types.AssignmentStatusREADY},
					},
				},
				"executor2": {
					AssignedShards: map[string]*types.ShardAssignment{
						"s3": {Status: types.AssignmentStatusREADY},
						"s4": {Status: types.AssignmentStatusREADY},
					},
				},
			},
		}
		deletedShards := map[string]store.ShardState{
			"s0": {},
			"s1": {},
		}
		shards := getShards(cfg, nsState, deletedShards)
		slices.Sort(shards)
		assert.Equal(t, []string{"s2", "s3", "s4"}, shards)
	})

	// Unknown type
	t.Run("Other type", func(t *testing.T) {
		cfg := config.Namespace{Type: "other"}
		shards := getShards(cfg, nil, nil)
		assert.Nil(t, shards)
	})
}

func TestAssignShardsToEmptyExecutors(t *testing.T) {
	cases := []struct {
		name                       string
		inputAssignments           map[string][]string
		expectedAssignments        map[string][]string
		expectedDistributonChanged bool
	}{
		{
			name:                       "no executors",
			inputAssignments:           map[string][]string{},
			expectedAssignments:        map[string][]string{},
			expectedDistributonChanged: false,
		},
		{
			name: "no empty executors",
			inputAssignments: map[string][]string{
				"exec-1": {"shard-1", "shard-2", "shard-3", "shard-4", "shard-5", "shard-6"},
				"exec-2": {"shard-7", "shard-8"},
			},
			expectedAssignments: map[string][]string{
				"exec-1": {"shard-1", "shard-2", "shard-3", "shard-4", "shard-5", "shard-6"},
				"exec-2": {"shard-7", "shard-8"},
			},
			expectedDistributonChanged: false,
		},
		{
			name: "empty executor",
			inputAssignments: map[string][]string{
				"exec-1": {"shard-1", "shard-2", "shard-3", "shard-4", "shard-5", "shard-6"},
				"exec-2": {"shard-7", "shard-8", "shard-9", "shard-10"},
				"exec-3": {},
			},
			expectedAssignments: map[string][]string{
				"exec-1": {"shard-2", "shard-3", "shard-4", "shard-5", "shard-6"},
				"exec-2": {"shard-8", "shard-9", "shard-10"},
				"exec-3": {"shard-1", "shard-7"},
			},
			expectedDistributonChanged: true,
		},
		{
			name:                       "all empty executors",
			inputAssignments:           map[string][]string{"exec-1": {}, "exec-2": {}, "exec-3": {}},
			expectedAssignments:        map[string][]string{"exec-1": {}, "exec-2": {}, "exec-3": {}},
			expectedDistributonChanged: false,
		},
		{
			name: "multiple empty executors",
			inputAssignments: map[string][]string{
				"exec-1": {"shard-1", "shard-2", "shard-3", "shard-4", "shard-5", "shard-6", "shard-7", "shard-8", "shard-9", "shard-10"},
				"exec-2": {"shard-11", "shard-12", "shard-13", "shard-14", "shard-15", "shard-16", "shard-17"},
				"exec-3": {"shard-18", "shard-19", "shard-20", "shard-21", "shard-22", "shard-23", "shard-24"},
				"exec-4": {},
				"exec-5": {},
			},
			expectedAssignments: map[string][]string{
				"exec-1": {"shard-4", "shard-5", "shard-6", "shard-7", "shard-8", "shard-9", "shard-10"},
				"exec-2": {"shard-14", "shard-15", "shard-16", "shard-17"},
				"exec-3": {"shard-20", "shard-21", "shard-22", "shard-23", "shard-24"},
				"exec-4": {"shard-1", "shard-18", "shard-12", "shard-3"},
				"exec-5": {"shard-11", "shard-2", "shard-19", "shard-13"},
			},
			expectedDistributonChanged: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actualDistributionChanged := assignShardsToEmptyExecutors(c.inputAssignments)

			assert.Equal(t, c.expectedAssignments, c.inputAssignments)
			assert.Equal(t, c.expectedDistributonChanged, actualDistributionChanged)
		})
	}
}
