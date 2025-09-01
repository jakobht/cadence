package executorclient

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uber-go/tally"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/uber/cadence/client/sharddistributorexecutor"
	"github.com/uber/cadence/common/clock"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/types"
)

func TestExecutorManagerLifeCycle(t *testing.T) {
	goleak.VerifyNone(t)

	ctrl := gomock.NewController(t)

	// Verify that the heartbeat requests are made
	mockShardDistributorClient := sharddistributorexecutor.NewMockClient(ctrl)
	mockShardDistributorClient.EXPECT().Heartbeat(gomock.Any(), &types.ExecutorHeartbeatRequest{
		Namespace:          "test-namespace1",
		ExecutorID:         "test-executor-id1",
		Status:             types.ExecutorStatusACTIVE,
		ShardStatusReports: map[string]*types.ShardStatusReport{},
	}).Return(&types.ExecutorHeartbeatResponse{
		ShardAssignments: map[string]*types.ShardAssignment{},
	}, nil)

	mockShardDistributorClient.EXPECT().Heartbeat(gomock.Any(), &types.ExecutorHeartbeatRequest{
		Namespace:          "test-namespace2",
		ExecutorID:         "test-executor-id2",
		Status:             types.ExecutorStatusACTIVE,
		ShardStatusReports: map[string]*types.ShardStatusReport{},
	}).Return(&types.ExecutorHeartbeatResponse{
		ShardAssignments: map[string]*types.ShardAssignment{},
	}, nil)

	timeSource := clock.NewMockedTimeSource()

	params := Params[*MockShardProcessor]{
		ShardProcessorFactory:  NewMockShardProcessorFactory[*MockShardProcessor](ctrl),
		Logger:                 log.NewNoop(),
		MetricsScope:           tally.NoopScope,
		ShardDistributorClient: mockShardDistributorClient,
		TimeSource:             timeSource,
		Config: ExecutorManagerConfig{
			Executors: []ExecutorConfig{
				{Namespace: "test-namespace1", ExecutorID: "test-executor-id1", HeartBeatInterval: time.Second},
				{Namespace: "test-namespace2", ExecutorID: "test-executor-id2", HeartBeatInterval: time.Second},
			},
		},
	}

	executorManager, err := NewExecutorManager(params)
	assert.NoError(t, err)
	executorManager.Start(context.Background())

	// Assert the two executors are different
	executor1, err := executorManager.GetExecutorForNamespace("test-namespace1")
	assert.NoError(t, err)
	executor2, err := executorManager.GetExecutorForNamespace("test-namespace2")
	assert.NoError(t, err)
	assert.NotEqual(t, executor1, executor2)

	// Wait for the heartbeat loop to run to verify that the heartbeat requests are made
	timeSource.BlockUntil(2)
	timeSource.Advance(1500 * time.Millisecond)

	// Force the heartbeat loops to run
	time.Sleep(10 * time.Millisecond)

	// Stop everything, goleak will fail if there are any goroutines leaked
	executorManager.Stop()
}
