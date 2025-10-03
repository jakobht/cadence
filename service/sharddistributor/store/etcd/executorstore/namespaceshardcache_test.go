package executorstore

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/cadence/common/log/testlogger"
	"github.com/uber/cadence/common/types"
	"github.com/uber/cadence/service/sharddistributor/store"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/etcdkeys"
)

func TestNamespaceShardToExecutor_Lifecycle(t *testing.T) {
	testCluster := setupStoreTestCluster(t)

	logger := testlogger.New(t)

	stopCh := make(chan struct{})

	// Setup: Create an assigned state for the executor
	assignedState := &store.AssignedState{
		AssignedShards: map[string]*types.ShardAssignment{
			"shard-1": {Status: types.AssignmentStatusREADY},
		},
	}
	assignedStateJSON, err := json.Marshal(assignedState)
	require.NoError(t, err)

	executor1AssignedStateKey := etcdkeys.BuildExecutorKey(testCluster.etcdPrefix, "test-ns", "executor-1", executorAssignedStateKey)
	testCluster.client.Put(context.Background(), executor1AssignedStateKey, string(assignedStateJSON))

	// First call should get the state and return the owner as executor-1
	namespaceShardToExecutor, err := newNamespaceShardToExecutor(testCluster.etcdPrefix, "test-ns", testCluster.client, stopCh, logger)
	assert.NoError(t, err)
	namespaceShardToExecutor.Start(&sync.WaitGroup{})

	owner, err := namespaceShardToExecutor.GetShardOwner(context.Background(), "shard-1")
	assert.NoError(t, err)
	assert.Equal(t, "executor-1", owner)

	// Check the cache is populated
	_, ok := namespaceShardToExecutor.executorRevision["executor-1"]
	assert.True(t, ok)
	assert.Equal(t, "executor-1", namespaceShardToExecutor.shardToExecutor["shard-1"])

	// Send a message on the channel to trigger a refresh
	// Change the owner to executor-2
	delete(assignedState.AssignedShards, "shard-1")
	assignedState.AssignedShards["shard-2"] = &types.ShardAssignment{Status: types.AssignmentStatusREADY}
	assignedStateJSON, err = json.Marshal(assignedState)
	require.NoError(t, err)

	executor2AssignedStateKey := etcdkeys.BuildExecutorKey(testCluster.etcdPrefix, "test-ns", "executor-2", executorAssignedStateKey)
	testCluster.client.Put(context.Background(), executor2AssignedStateKey, string(assignedStateJSON))

	// Sleep a bit to allow the cache to refresh
	time.Sleep(100 * time.Millisecond)

	// Check that executor-2 and shard-2 is in the cache
	_, ok = namespaceShardToExecutor.executorRevision["executor-2"]
	assert.True(t, ok)
	assert.Equal(t, "executor-2", namespaceShardToExecutor.shardToExecutor["shard-2"])

	close(stopCh)
}

/*
func TestNamespaceShardToExecutor_GetShardOwner_RefreshError(t *testing.T) {
	testCluster := setupStoreTestCluster(t)

	logger := testlogger.New(t)

	nsCache := &namespaceShardToExecutor{
		shardToExecutor: make(map[string]string), // Empty cache
		namespace:       "non-existent-ns",
		client:          testCluster.client,
		logger:          logger,
	}

	// GetShardOwner should trigger refresh and return the error
	owner, err := nsCache.GetShardOwner(context.Background(), "shard-1")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "refresh for namespace non-existent-ns")
	assert.Empty(t, owner)
}
*/
