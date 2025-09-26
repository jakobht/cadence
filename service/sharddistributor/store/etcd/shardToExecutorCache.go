package etcd

import (
	"context"
	"fmt"
	"sync"

	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/service/sharddistributor/store"
)

type NamespaceToShards map[string]*NamespaceShardToExecutor
type ShardToExecutorCache struct {
	sync.RWMutex
	namespaceToShards NamespaceToShards
	store             store.Store
	stopC             chan struct{}
	logger            log.Logger
}

func NewShardToExecutorCache(store store.Store, logger log.Logger) *ShardToExecutorCache {
	return &ShardToExecutorCache{
		namespaceToShards: make(NamespaceToShards),
		store:             store,
		stopC:             make(chan struct{}),
		logger:            logger,
	}
}

func (s *ShardToExecutorCache) Start() {}

func (s *ShardToExecutorCache) Stop() {
	close(s.stopC)
}

func (s *ShardToExecutorCache) GetShardOwner(ctx context.Context, namespace, shardID string) (string, error) {
	namespaceShardToExecutor, err := s.getNamespaceShardToExecutor(ctx, namespace)
	if err != nil {
		return "", fmt.Errorf("get namespace shard to executor: %w", err)
	}
	return namespaceShardToExecutor.GetShardOwner(ctx, shardID)
}

func (s *ShardToExecutorCache) getNamespaceShardToExecutor(ctx context.Context, namespace string) (*NamespaceShardToExecutor, error) {
	s.RLock()
	namespaceShardToExecutor, ok := s.namespaceToShards[namespace]
	s.RUnlock()

	if ok {
		return namespaceShardToExecutor, nil
	}

	s.Lock()
	defer s.Unlock()

	namespaceShardToExecutor, err := newNamespaceShardToExecutor(namespace, s.store, s.stopC, s.logger)
	if err != nil {
		return nil, fmt.Errorf("new namespace shard to executor: %w", err)
	}

	s.namespaceToShards[namespace] = namespaceShardToExecutor
	return namespaceShardToExecutor, nil
}

type NamespaceShardToExecutor struct {
	sync.RWMutex

	shardToExecutor     map[string]string
	namespace           string
	changeUpdateChannel <-chan int64
	stopCh              chan struct{}
	store               store.Store
	logger              log.Logger
}

func newNamespaceShardToExecutor(namespace string, store store.Store, stopCh chan struct{}, logger log.Logger) (*NamespaceShardToExecutor, error) {
	// Start listening
	changeUpdateChannel, err := store.Subscribe(context.Background(), namespace)
	if err != nil {
		return nil, fmt.Errorf("subscribe to state changes: %w", err)
	}

	return &NamespaceShardToExecutor{
		shardToExecutor:     make(map[string]string),
		namespace:           namespace,
		changeUpdateChannel: changeUpdateChannel,
		stopCh:              stopCh,
		store:               store,
		logger:              logger,
	}, nil
}

func (n *NamespaceShardToExecutor) Start() {
	go n.nameSpaceRefreashLoop()
}

func (n *NamespaceShardToExecutor) GetShardOwner(ctx context.Context, shardID string) (string, error) {
	n.RLock()
	owner, ok := n.shardToExecutor[shardID]
	n.RUnlock()

	if ok {
		return owner, nil
	}

	// Force refresh the cache
	err := n.refresh(ctx)
	if err != nil {
		return "", fmt.Errorf("refresh: %w", err)
	}

	// Check the cache again after refresh
	n.RLock()
	owner, ok = n.shardToExecutor[shardID]
	n.RUnlock()
	if ok {
		return owner, nil
	}

	return "", store.ErrShardNotFound
}

func (n *NamespaceShardToExecutor) nameSpaceRefreashLoop() {
	for {
		select {
		case <-n.stopCh:
			return
		case <-n.changeUpdateChannel:
			err := n.refresh(context.Background())
			if err != nil {
				n.logger.Error("refresh", tag.Error(err))
			}
		}
	}
}

func (n *NamespaceShardToExecutor) refresh(ctx context.Context) error {
	namespaceState, err := n.store.GetState(ctx, n.namespace)
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}

	n.Lock()
	defer n.Unlock()

	for executor, assingedState := range namespaceState.ShardAssignments {
		for shardID := range assingedState.AssignedShards {
			n.shardToExecutor[shardID] = executor
		}
	}

	return nil
}
