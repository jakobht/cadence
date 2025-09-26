package shardcache

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/fx"

	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/executorstore"
)

type NamespaceToShards map[string]*namespaceShardToExecutor
type ShardToExecutorCache struct {
	sync.RWMutex
	namespaceToShards NamespaceToShards
	store             *executorstore.Store
	stopC             chan struct{}
	logger            log.Logger
}

type ShardToExecutorCacheParams struct {
	fx.In

	Store  *executorstore.Store
	Logger log.Logger
}

func NewShardToExecutorCache(p ShardToExecutorCacheParams) *ShardToExecutorCache {
	return &ShardToExecutorCache{
		namespaceToShards: make(NamespaceToShards),
		store:             p.Store,
		stopC:             make(chan struct{}),
		logger:            p.Logger,
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

func (s *ShardToExecutorCache) getNamespaceShardToExecutor(ctx context.Context, namespace string) (*namespaceShardToExecutor, error) {
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
