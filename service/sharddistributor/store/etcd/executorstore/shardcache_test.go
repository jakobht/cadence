package executorstore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/cadence/common/log/testlogger"
	"github.com/uber/cadence/common/types"
	"github.com/uber/cadence/service/sharddistributor/store"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/etcdkeys"
)

func TestNewShardToExecutorCache(t *testing.T) {
	logger := testlogger.New(t)

	cache := NewShardToExecutorCache(ShardToExecutorCacheParams{
		Logger: logger,
	})

	assert.NotNil(t, cache)

	assert.NotNil(t, cache.namespaceToShards)
	assert.NotNil(t, cache.stopC)

	assert.Equal(t, logger, cache.logger)
}

func TestShardExecutorCache(t *testing.T) {
	testCluster := setupStoreTestCluster(t)

	logger := testlogger.New(t)

	// Setup: Create an assigned state for the executor
	assignedState := &store.AssignedState{
		AssignedShards: map[string]*types.ShardAssignment{
			"shard-1": {Status: types.AssignmentStatusREADY},
		},
	}
	assignedStateJSON, err := json.Marshal(assignedState)
	require.NoError(t, err)

	executorAssignedStateKey := etcdkeys.BuildExecutorKey(testCluster.etcdPrefix, "test-ns", "executor-1", executorAssignedStateKey)
	testCluster.client.Put(context.Background(), executorAssignedStateKey, string(assignedStateJSON))

	cache := NewShardToExecutorCache(ShardToExecutorCacheParams{
		Logger: logger,
	})
	cache.prefix = testCluster.etcdPrefix
	cache.client = testCluster.client

	cache.Start()
	defer cache.Stop()

	// This will read the namespace from the store as the cache is empty
	owner, err := cache.GetShardOwner(context.Background(), "test-ns", "shard-1")
	assert.NoError(t, err)
	assert.Equal(t, "executor-1", owner)

	// Check the cache is populated
	assert.Greater(t, cache.namespaceToShards["test-ns"].executorRevision["executor-1"], int64(0))
	assert.Equal(t, "executor-1", cache.namespaceToShards["test-ns"].shardToExecutor["shard-1"])
}

/*
func TestShardExecutorCache_SubscribeFailure(t *testing.T) {
	goleak.VerifyNone(t)

	logger := testlogger.New(t)
	ctrl := gomock.NewController(t)
	mockStore := NewMockExecutorStore(ctrl)

	mockChan := make(chan int64, 1)

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
*/
