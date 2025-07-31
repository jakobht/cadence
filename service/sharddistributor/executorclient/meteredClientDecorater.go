package executorclient

import (
	"context"

	"go.uber.org/yarpc"

	"github.com/uber-go/tally"
	"github.com/uber/cadence/client/sharddistributorexecutor"
	"github.com/uber/cadence/common/metrics"
	"github.com/uber/cadence/common/types"
)

// TODO: consider using gowrap to generate this code
type meteredShardDistributorExecutorClient struct {
	client       sharddistributorexecutor.Client
	metricsScope tally.Scope
}

// NewShardDistributorExecutorClient creates a new instance of sharddistributorexecutorClient with retry policy
func NewMeteredShardDistributorExecutorClient(client sharddistributorexecutor.Client, metricsScope tally.Scope) sharddistributorexecutor.Client {
	return &meteredShardDistributorExecutorClient{
		client:       client,
		metricsScope: metricsScope,
	}
}

func (c *meteredShardDistributorExecutorClient) Heartbeat(ctx context.Context, ep1 *types.ExecutorHeartbeatRequest, p1 ...yarpc.CallOption) (ep2 *types.ExecutorHeartbeatResponse, err error) {
	var scope tally.Scope
	scope = c.metricsScope.Tagged(map[string]string{
		metrics.OperationTagName: "ShardDistributorExecutorHeartbeat",
	})

	scope.Counter("shard_distributor_executor_client_requests").Inc(1)

	sw := scope.Timer("shard_distributor_executor_client_latency").Start()
	ep2, err = c.client.Heartbeat(ctx, ep1, p1...)
	sw.Stop()

	if err != nil {
		scope.Counter("shard_distributor_executor_client_failures").Inc(1)
	}
	return ep2, err
}
