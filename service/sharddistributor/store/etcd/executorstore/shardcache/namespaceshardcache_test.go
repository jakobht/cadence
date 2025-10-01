package shardcache

import (
	"context"
	"testing"

	"time"

	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/assert"

	"github.com/uber/cadence/common/log/testlogger"
	"github.com/uber/cadence/common/types"
	"github.com/uber/cadence/service/sharddistributor/store"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/executorstore"
)

func TestNamespaceShardToExecutor_Lifecycle(t *testing.T) {
	goleak.VerifyNone(t)

	logger := testlogger.New(t)
	ctrl := gomock.NewController(t)

	mockStore := executorstore.NewMockExecutorStore(ctrl)
	mockChan := make(chan int64, 1)
	stopCh := make(chan struct{})

	mockStore.EXPECT().Subscribe(gomock.Any(), "test-ns").Return(mockChan, nil)

	// First call to get state, since the cache is empty
	mockStore.EXPECT().GetState(gomock.Any(), "test-ns").Return(&store.NamespaceState{
		ShardAssignments: map[string]store.AssignedState{
			"executor-1": {
				AssignedShards: map[string]*types.ShardAssignment{
					"shard-1": {Status: types.AssignmentStatusREADY},
				},
			},
		},
	}, nil).Times(1)

	// Second call to get state, after the watch triggers - the shard owner moved
	mockStore.EXPECT().GetState(gomock.Any(), "test-ns").Return(&store.NamespaceState{
		ShardAssignments: map[string]store.AssignedState{
			"executor-2": {
				AssignedShards: map[string]*types.ShardAssignment{
					"shard-1": {Status: types.AssignmentStatusREADY},
				},
			},
		},
	}, nil).Times(1)

	// First call should get the state and return the owner as executor-1
	namespaceShardToExecutor, err := newNamespaceShardToExecutor("test-ns", mockStore, stopCh, logger)
	assert.NoError(t, err)
	namespaceShardToExecutor.Start()

	owner, err := namespaceShardToExecutor.GetShardOwner(context.Background(), "shard-1")
	assert.NoError(t, err)
	assert.Equal(t, "executor-1", owner)

	// Send a message on the channel to trigger a refresh
	mockChan <- 1

	// Sleep a bit to allow the goroutine to refresh
	time.Sleep(10 * time.Millisecond)

	owner, err = namespaceShardToExecutor.GetShardOwner(context.Background(), "shard-1")
	assert.NoError(t, err)
	assert.Equal(t, "executor-2", owner)

	close(stopCh)
}

func TestNamespaceShardToExecutor_GetShardOwner_RefreshError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := executorstore.NewMockExecutorStore(ctrl)
	logger := testlogger.New(t)

	nsCache := &namespaceShardToExecutor{
		shardToExecutor: make(map[string]string), // Empty cache
		namespace:       "test-ns",
		store:           mockStore,
		logger:          logger,
	}

	// Mock GetState to return error during refresh
	mockStore.EXPECT().GetState(gomock.Any(), "test-ns").Return(nil, assert.AnError)

	// GetShardOwner should trigger refresh and return the error
	owner, err := nsCache.GetShardOwner(context.Background(), "shard-1")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "refresh for namespace test-ns")
	assert.Empty(t, owner)
}
