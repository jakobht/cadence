package metadata

import (
	"go.uber.org/fx"

	"github.com/uber/cadence/common/rpc"
	"github.com/uber/cadence/service/sharddistributor/client/executorclient"
)

const (
	// MetadataKeyGRPCAddress is the metadata key for the executor's GRPC address
	MetadataKeyGRPCAddress = "grpc_address"
)

// Params are the parameters for creating executor metadata
type Params struct {
	fx.In

	RPCParams rpc.Params
}

// NewExecutorMetadata creates executor metadata with the GRPC address
func NewExecutorMetadata(params Params) executorclient.ExecutorMetadata {
	return executorclient.ExecutorMetadata{
		MetadataKeyGRPCAddress: params.RPCParams.GRPCAddress,
	}
}
