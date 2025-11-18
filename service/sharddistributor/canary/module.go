package canary

import (
	"go.uber.org/fx"
	"go.uber.org/yarpc/api/peer"
	"go.uber.org/yarpc/api/transport"
	"go.uber.org/yarpc/transport/grpc"

	sharddistributorv1 "github.com/uber/cadence/.gen/proto/sharddistributor/v1"
	"github.com/uber/cadence/service/sharddistributor/canary/executors"
	"github.com/uber/cadence/service/sharddistributor/canary/factory"
	"github.com/uber/cadence/service/sharddistributor/canary/handler"
	"github.com/uber/cadence/service/sharddistributor/canary/metadata"
	"github.com/uber/cadence/service/sharddistributor/canary/pinger"
	"github.com/uber/cadence/service/sharddistributor/canary/processor"
	"github.com/uber/cadence/service/sharddistributor/canary/processorephemeral"
	"github.com/uber/cadence/service/sharddistributor/canary/sharddistributorclient"
	"github.com/uber/cadence/service/sharddistributor/client/executorclient"
	"github.com/uber/cadence/service/sharddistributor/client/spectatorclient"
)

type NamespacesNames struct {
	fx.In
	FixedNamespace              string
	EphemeralNamespace          string
	ExternalAssignmentNamespace string
	SharddistributorServiceName string
}

func Module(namespacesNames NamespacesNames) fx.Option {
	return fx.Module("shard-distributor-canary", opts(namespacesNames))
}

func opts(names NamespacesNames) fx.Option {
	return fx.Options(
		fx.Provide(sharddistributorv1.NewFxShardDistributorExecutorAPIYARPCClient(names.SharddistributorServiceName)),
		fx.Provide(sharddistributorv1.NewFxShardDistributorAPIYARPCClient(names.SharddistributorServiceName)),

		fx.Provide(sharddistributorclient.NewShardDistributorClient),

		// Provide executor metadata with GRPC address
		fx.Provide(metadata.NewExecutorMetadata),

		// Modules for the shard distributor canary
		fx.Provide(
			func(params factory.Params) executorclient.ShardProcessorFactory[*processor.ShardProcessor] {
				return factory.NewShardProcessorFactory(params, processor.NewShardProcessor)
			},
			func(params factory.Params) executorclient.ShardProcessorFactory[*processorephemeral.ShardProcessor] {
				return factory.NewShardProcessorFactory(params, processorephemeral.NewShardProcessor)
			},
		),

		// Simple way to instantiate executor if only one namespace is used
		// executorclient.ModuleWithNamespace[*processor.ShardProcessor](names.FixedNamespace),
		// executorclient.ModuleWithNamespace[*processorephemeral.ShardProcessor](names.EphemeralNamespace),

		// Instantiate executors for multiple namespaces
		executors.Module(names.FixedNamespace, names.EphemeralNamespace, names.ExternalAssignmentNamespace),

		spectatorclient.Module(),
		fx.Provide(spectatorclient.NewSpectatorPeerChooser),
		fx.Invoke(func(chooser peer.Chooser, lc fx.Lifecycle) {
			lc.Append(fx.StartStopHook(chooser.Start, chooser.Stop))
		}),

		// Create canary client with SpectatorPeerChooser for canary-to-canary communication
		fx.Provide(func(chooser peer.Chooser, tsp *grpc.Transport) sharddistributorv1.ShardDistributorExecutorCanaryAPIYARPCClient {
			outbound := tsp.NewOutbound(chooser)

			// Create a simple client config with the outbound
			return sharddistributorv1.NewShardDistributorExecutorCanaryAPIYARPCClient(&transport.OutboundConfig{
				CallerName: "cadence-shard-distributor-canary",
				Outbounds:  transport.Outbounds{Unary: outbound},
			})
		}),

		fx.Provide(func(params pinger.Params, lc fx.Lifecycle) *pinger.Pinger {
			pinger := pinger.NewPinger(params, names.FixedNamespace, 32)
			lc.Append(fx.StartStopHook(pinger.Start, pinger.Stop))
			return pinger
		}),

		// Register canary ping handler to receive ping requests from other executors
		fx.Provide(handler.NewPingHandler),
		fx.Provide(fx.Annotate(
			func(h *handler.PingHandler) sharddistributorv1.ShardDistributorExecutorCanaryAPIYARPCServer {
				return h
			},
		)),
		fx.Provide(sharddistributorv1.NewFxShardDistributorExecutorCanaryAPIYARPCProcedures()),
	)
}
