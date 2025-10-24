package shardcache

import (
	"context"
	"fmt"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/etcdclient"
)

type NamespaceToShards map[string]*namespaceShardToExecutor
type ShardToExecutorCache struct {
	sync.RWMutex
	namespaceToShards NamespaceToShards
	client            etcdclient.Client
	stopC             chan struct{}
	logger            log.Logger
	prefix            string
	wg                sync.WaitGroup
}

func NewShardToExecutorCache(
	prefix string,
	client etcdclient.Client,
	logger log.Logger,
) *ShardToExecutorCache {
	shardCache := &ShardToExecutorCache{
		namespaceToShards: make(NamespaceToShards),
		stopC:             make(chan struct{}),
		logger:            logger,
		prefix:            prefix,
		client:            client,
		wg:                sync.WaitGroup{},
	}

	return shardCache
}

func (s *ShardToExecutorCache) Start() {}

func (s *ShardToExecutorCache) Stop() {
	close(s.stopC)
	s.wg.Wait()
}

func (s *ShardToExecutorCache) GetShardOwner(ctx context.Context, namespace, shardID string) (string, error) {
	namespaceShardToExecutor, err := s.getNamespaceShardToExecutor(namespace)
	if err != nil {
		return "", fmt.Errorf("get namespace shard to executor: %w", err)
	}
	return namespaceShardToExecutor.GetShardOwner(ctx, shardID)
}

func (s *ShardToExecutorCache) GetExecutorModRevisionCmp(namespace string) ([]clientv3.Cmp, error) {
	namespaceShardToExecutor, err := s.getNamespaceShardToExecutor(namespace)
	if err != nil {
		return nil, fmt.Errorf("get namespace shard to executor: %w", err)
	}
	return namespaceShardToExecutor.GetExecutorModRevisionCmp()
}

func (s *ShardToExecutorCache) getNamespaceShardToExecutor(namespace string) (*namespaceShardToExecutor, error) {
	s.RLock()
	namespaceShardToExecutor, ok := s.namespaceToShards[namespace]
	s.RUnlock()

	if ok {
		return namespaceShardToExecutor, nil
	}

	s.Lock()
	defer s.Unlock()

	namespaceShardToExecutor, err := newNamespaceShardToExecutor(s.prefix, namespace, s.client, s.stopC, s.logger)
	if err != nil {
		return nil, fmt.Errorf("new namespace shard to executor: %w", err)
	}
	namespaceShardToExecutor.Start(&s.wg)

	s.namespaceToShards[namespace] = namespaceShardToExecutor
	return namespaceShardToExecutor, nil
}
