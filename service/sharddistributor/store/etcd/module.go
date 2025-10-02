package etcd

import (
	"go.uber.org/fx"

	"github.com/uber/cadence/service/sharddistributor/store"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/executorstore"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/executorstore/shardcache"
	"github.com/uber/cadence/service/sharddistributor/store/etcd/leaderstore"
)

type Store struct {
	executorstore.ExecutorStore
	*shardcache.ShardToExecutorCache
}

type StoreParams struct {
	fx.In

	Store                executorstore.ExecutorStore
	ShardToExecutorCache *shardcache.ShardToExecutorCache
}

func NewStore(p StoreParams) store.Store {
	return &Store{
		ExecutorStore:        p.Store,
		ShardToExecutorCache: p.ShardToExecutorCache,
	}
}

var Module = fx.Module("etcd",
	fx.Provide(executorstore.NewStore),
	fx.Provide(shardcache.NewShardToExecutorCache),
	fx.Provide(leaderstore.NewLeaderStore),
	fx.Provide(NewStore),
	fx.Invoke(func(store executorstore.ExecutorStore, cache *shardcache.ShardToExecutorCache, lc fx.Lifecycle) {
		lc.Append(fx.StartStopHook(cache.Start, cache.Stop))
		lc.Append(fx.StartStopHook(store.Start, store.Stop))
	}),
)
