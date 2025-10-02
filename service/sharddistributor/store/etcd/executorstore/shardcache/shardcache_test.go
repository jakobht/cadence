package shardcache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/uber/cadence/common/log/testlogger"
	"github.com/uber/cadence/common/types"
	"github.com/uber/cadence/service/sharddistributor/store"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/executorstore"
)

func TestNewShardToExecutorCache(t *testing.T) {
	logger := testlogger.New(t)
	ctrl := gomock.NewController(t)
	mockStore := executorstore.NewMockExecutorStore(ctrl)

	cache := NewShardToExecutorCache(ShardToExecutorCacheParams{
		Store:  mockStore,
		Logger: logger,
	})

	assert.NotNil(t, cache)

	assert.NotNil(t, cache.namespaceToShards)
	assert.NotNil(t, cache.stopC)

	assert.Equal(t, mockStore, cache.store)
	assert.Equal(t, logger, cache.logger)
}

func TestShardExecutorCache(t *testing.T) {
	goleak.VerifyNone(t)

	logger := testlogger.New(t)
	ctrl := gomock.NewController(t)
	mockStore := executorstore.NewMockExecutorStore(ctrl)

	mockChan := make(chan store.NameSpaceEvent, 1)

	// We only expect one call to subscribe and get state, since the cache will then be
	// populated
	mockStore.EXPECT().Subscribe(gomock.Any(), "test-ns").Return(mockChan, nil).Times(1)
	mockStore.EXPECT().GetState(gomock.Any(), "test-ns").Return(&store.NamespaceState{
		ShardAssignments: map[string]store.AssignedState{
			"executor-1": {
				AssignedShards: map[string]*types.ShardAssignment{
					"shard-1": {Status: types.AssignmentStatusREADY},
				},
			},
			"executor-2": {
				AssignedShards: map[string]*types.ShardAssignment{
					"shard-2": {Status: types.AssignmentStatusREADY},
				},
			},
		},
	}, nil).Times(1)

	cache := NewShardToExecutorCache(ShardToExecutorCacheParams{
		Store:  mockStore,
		Logger: logger,
	})

	cache.Start()

	// This will read the namespace from the store as the cache is empty
	owner, err := cache.GetShardOwner(context.Background(), "test-ns", "shard-1")
	assert.NoError(t, err)
	assert.Equal(t, "executor-1", owner)

	// This will not read the namespace from the store as it's already in the cache
	owner, err = cache.GetShardOwner(context.Background(), "test-ns", "shard-2")
	assert.NoError(t, err)
	assert.Equal(t, "executor-2", owner)

	cache.Stop()
}

func TestShardExecutorCache_SubscribeFailure(t *testing.T) {
	goleak.VerifyNone(t)

	logger := testlogger.New(t)
	ctrl := gomock.NewController(t)
	mockStore := executorstore.NewMockExecutorStore(ctrl)

	mockChan := make(chan store.NameSpaceEvent, 1)

	// We only expect one call to subscribe and get state, since the cache will then be
	// populated
	mockStore.EXPECT().Subscribe(gomock.Any(), "test-ns").Return(mockChan, assert.AnError)

	cache := NewShardToExecutorCache(ShardToExecutorCacheParams{
		Store:  mockStore,
		Logger: logger,
	})

	cache.Start()

	// This will read the namespace from the store as the cache is empty
	_, err := cache.GetShardOwner(context.Background(), "test-ns", "shard-1")
	assert.ErrorContains(t, err, "get namespace shard to executor")

	cache.Stop()
}
