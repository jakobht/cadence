package executorclient

import (
	"context"

	"github.com/uber-go/tally"
	"go.uber.org/fx"

	sharddistributorv1 "github.com/uber/cadence/.gen/proto/sharddistributor/v1"
	"github.com/uber/cadence/client/sharddistributorexecutor"
	"github.com/uber/cadence/client/wrappers/grpc"
	timeoutwrapper "github.com/uber/cadence/client/wrappers/timeout"
	"github.com/uber/cadence/common/clock"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/types"
)

//go:generate mockgen -package $GOPACKAGE -source $GOFILE -destination interface_mock.go . ShardProcessorFactory,ShardProcessor,ExecutorManager

type ShardReport struct {
	ShardLoad float64
	Status    types.ShardStatus
}

type ShardProcessor interface {
	Start(ctx context.Context)
	Stop()
	GetShardReport() ShardReport
}

type ShardProcessorFactory[SP ShardProcessor] interface {
	NewShardProcessor(shardID string) (SP, error)
}

type Executor[SP ShardProcessor] interface {
	Start(ctx context.Context)
	Stop()

	GetShardProcessor(shardID string) (SP, error)
}

type ExecutorManager[SP ShardProcessor] interface {
	Start(ctx context.Context)
	Stop()

	GetShardProcessor(namespace, shardID string) (SP, error)
	GetExecutorForNamespace(namespace string) (Executor[SP], error)
}

type Params[SP ShardProcessor] struct {
	fx.In

	ShardDistributorClient sharddistributorexecutor.Client
	MetricsScope           tally.Scope
	Logger                 log.Logger
	ShardProcessorFactory  ShardProcessorFactory[SP]
	Config                 ExecutorManagerConfig
	TimeSource             clock.TimeSource
}

func createShardDistributorExecutorClient(yarpcClient sharddistributorv1.ShardDistributorExecutorAPIYARPCClient, metricsScope tally.Scope) (sharddistributorexecutor.Client, error) {
	shardDistributorExecutorClient := grpc.NewShardDistributorExecutorClient(yarpcClient)

	shardDistributorExecutorClient = timeoutwrapper.NewShardDistributorExecutorClient(shardDistributorExecutorClient, timeoutwrapper.ShardDistributorExecutorDefaultTimeout)

	if metricsScope != nil {
		shardDistributorExecutorClient = NewMeteredShardDistributorExecutorClient(shardDistributorExecutorClient, metricsScope)
	}

	return shardDistributorExecutorClient, nil
}

func Module[SP ShardProcessor]() fx.Option {
	return fx.Module("shard-distributor-executor-client",
		fx.Provide(createShardDistributorExecutorClient),
		fx.Provide(NewExecutorManager[SP]),
		fx.Invoke(func(executorManager ExecutorManager[SP], lc fx.Lifecycle) {
			lc.Append(fx.StartStopHook(executorManager.Start, executorManager.Stop))
		}),
	)
}
