package etcd

import (
	"fmt"

	"go.uber.org/fx"

	"github.com/uber/cadence/service/sharddistributor/config"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/etcdclient"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/executorstore"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/leaderstore"
)

var Module = fx.Module("etcd",
	executorstore.Module,
	fx.Provide(leaderstore.NewLeaderStore),
	fx.Provide(NewExecutorStoreClient),
	fx.Provide(NewLeaderStoreClient),
)

// ExecutorStoreClientOutput provides the executor store client.
type ExecutorStoreClientOutput struct {
	fx.Out

	Client etcdclient.Client `name:"executorstore"`
}

// NewExecutorStoreClient creates a new ETCD client for the executor store.
func NewExecutorStoreClient(cfg executorstore.ETCDConfig, lc fx.Lifecycle) (ExecutorStoreClientOutput, error) {
	client, err := etcdclient.NewClientFromConfig(etcdclient.ClientConfig{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	}, lc)
	if err != nil {
		return ExecutorStoreClientOutput{}, fmt.Errorf("executor store client: %w", err)
	}
	return ExecutorStoreClientOutput{Client: client}, nil
}

// LeaderStoreClientOutput provides the leader store client.
type LeaderStoreClientOutput struct {
	fx.Out

	Client etcdclient.Client `name:"leaderstore"`
}

// NewLeaderStoreClient creates a new ETCD client for the leader store.
func NewLeaderStoreClient(cfg config.ShardDistribution, lc fx.Lifecycle) (LeaderStoreClientOutput, error) {
	var clientCfg etcdclient.ClientConfig
	if err := cfg.LeaderStore.StorageParams.Decode(&clientCfg); err != nil {
		return LeaderStoreClientOutput{}, fmt.Errorf("bad config for leader store client: %w", err)
	}

	client, err := etcdclient.NewClientFromConfig(clientCfg, lc)
	if err != nil {
		return LeaderStoreClientOutput{}, fmt.Errorf("leader store client: %w", err)
	}
	return LeaderStoreClientOutput{Client: client}, nil
}
