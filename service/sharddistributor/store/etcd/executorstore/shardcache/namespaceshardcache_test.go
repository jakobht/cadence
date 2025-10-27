package shardcache

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
	"github.com/uber/cadence/service/sharddistributor/store/etcd/testhelper"
)

func TestNamespaceShardToExecutor_Lifecycle(t *testing.T) {
	testCluster := testhelper.SetupStoreTestCluster(t)

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

	executor1AssignedStateKey, err := etcdkeys.BuildExecutorKey(testCluster.EtcdPrefix, "test-ns", "executor-1", etcdkeys.ExecutorAssignedStateKey)
	require.NoError(t, err)
	testCluster.Client.Put(context.Background(), executor1AssignedStateKey, string(assignedStateJSON))

	// Add metadata for executor-1
	executor1MetadataKey := etcdkeys.BuildMetadataKey(testCluster.EtcdPrefix, "test-ns", "executor-1", "hostname")
	testCluster.Client.Put(context.Background(), executor1MetadataKey, "executor-1-host")
	executor1MetadataKey2 := etcdkeys.BuildMetadataKey(testCluster.EtcdPrefix, "test-ns", "executor-1", "version")
	testCluster.Client.Put(context.Background(), executor1MetadataKey2, "v1.0.0")

	// First call should get the state and return the owner as executor-1
	namespaceShardToExecutor, err := newNamespaceShardToExecutor(testCluster.EtcdPrefix, "test-ns", testCluster.Client, stopCh, logger)
	assert.NoError(t, err)
	namespaceShardToExecutor.Start(&sync.WaitGroup{})

	// Give it a moment to start watching
	time.Sleep(50 * time.Millisecond)

	owner, err := namespaceShardToExecutor.GetShardOwner(context.Background(), "shard-1")
	assert.NoError(t, err)
	assert.Equal(t, "executor-1", owner.ExecutorID)
	assert.Equal(t, "executor-1-host", owner.Metadata["hostname"])
	assert.Equal(t, "v1.0.0", owner.Metadata["version"])

	// Check the cache is populated
	_, ok := namespaceShardToExecutor.executorRevision["executor-1"]
	assert.True(t, ok)
	assert.Equal(t, "executor-1", namespaceShardToExecutor.shardToExecutor["shard-1"].ExecutorID)

	// Send a message on the channel to trigger a refresh
	// Change the owner to executor-2
	delete(assignedState.AssignedShards, "shard-1")
	assignedState.AssignedShards["shard-2"] = &types.ShardAssignment{Status: types.AssignmentStatusREADY}
	assignedStateJSON, err = json.Marshal(assignedState)
	require.NoError(t, err)

	executor2AssignedStateKey, err := etcdkeys.BuildExecutorKey(testCluster.EtcdPrefix, "test-ns", "executor-2", etcdkeys.ExecutorAssignedStateKey)
	require.NoError(t, err)
	testCluster.Client.Put(context.Background(), executor2AssignedStateKey, string(assignedStateJSON))

	// Add metadata for executor-2
	executor2MetadataKey := etcdkeys.BuildMetadataKey(testCluster.EtcdPrefix, "test-ns", "executor-2", "hostname")
	testCluster.Client.Put(context.Background(), executor2MetadataKey, "executor-2-host")
	executor2MetadataKey2 := etcdkeys.BuildMetadataKey(testCluster.EtcdPrefix, "test-ns", "executor-2", "region")
	testCluster.Client.Put(context.Background(), executor2MetadataKey2, "us-west")

	// Sleep a bit to allow the cache to refresh
	time.Sleep(100 * time.Millisecond)

	// Check that executor-2 and shard-2 is in the cache
	_, ok = namespaceShardToExecutor.executorRevision["executor-2"]
	assert.True(t, ok)
	assert.Equal(t, "executor-2", namespaceShardToExecutor.shardToExecutor["shard-2"].ExecutorID)

	// Verify metadata is present for executor-2
	owner2, err := namespaceShardToExecutor.GetShardOwner(context.Background(), "shard-2")
	assert.NoError(t, err)
	assert.Equal(t, "executor-2", owner2.ExecutorID)
	assert.Equal(t, "executor-2-host", owner2.Metadata["hostname"])
	assert.Equal(t, "us-west", owner2.Metadata["region"])

	close(stopCh)
}
