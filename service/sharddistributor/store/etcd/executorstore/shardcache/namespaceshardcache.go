package shardcache

import (
	"context"
	"fmt"
	"sync"

	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/service/sharddistributor/store"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/executorstore"
)

type namespaceShardToExecutor struct {
	sync.RWMutex

	shardToExecutor     map[string]string
	namespace           string
	changeUpdateChannel <-chan int64
	stopCh              chan struct{}
	store               *executorstore.ExecutorStore
	logger              log.Logger
}

func newNamespaceShardToExecutor(namespace string, store *executorstore.ExecutorStore, stopCh chan struct{}, logger log.Logger) (*namespaceShardToExecutor, error) {
	// Start listening
	changeUpdateChannel, err := store.Subscribe(context.Background(), namespace)
	if err != nil {
		return nil, fmt.Errorf("subscribe to state changes for namespace %s: %w", namespace, err)
	}

	return &namespaceShardToExecutor{
		shardToExecutor:     make(map[string]string),
		namespace:           namespace,
		changeUpdateChannel: changeUpdateChannel,
		stopCh:              stopCh,
		store:               store,
		logger:              logger,
	}, nil
}

func (n *namespaceShardToExecutor) Start() {
	go n.nameSpaceRefreashLoop()
}

func (n *namespaceShardToExecutor) GetShardOwner(ctx context.Context, shardID string) (string, error) {
	n.RLock()
	owner, ok := n.shardToExecutor[shardID]
	n.RUnlock()

	if ok {
		return owner, nil
	}

	// Force refresh the cache
	err := n.refresh(ctx)
	if err != nil {
		return "", fmt.Errorf("refresh for namespace %s: %w", n.namespace, err)
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

func (n *namespaceShardToExecutor) nameSpaceRefreashLoop() {
	for {
		select {
		case <-n.stopCh:
			return
		case <-n.changeUpdateChannel:
			err := n.refresh(context.Background())
			if err != nil {
				n.logger.Error("refresh", tag.ShardNamespace(n.namespace), tag.Error(err))
			}
		}
	}
}

func (n *namespaceShardToExecutor) refresh(ctx context.Context) error {
	namespaceState, err := n.store.GetState(ctx, n.namespace)
	if err != nil {
		return fmt.Errorf("get state for namespace %s: %w", n.namespace, err)
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
