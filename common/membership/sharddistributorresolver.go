package membership

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"github.com/uber/cadence/client/sharddistributor"
	"github.com/uber/cadence/common/dynamicconfig"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/common/types"
)

type modeKey string

var (
	modeKeyHashRing                       modeKey = "hash_ring"
	modeKeyShardDistributor               modeKey = "shard_distributor"
	modeKeyHashRingShadowShardDistributor modeKey = "hash_ring-shadow-shard_distributor"
	modeKeyShardDistributorShadowHashRing modeKey = "shard_distributor-shadow-hash_ring"
)

type shardDistributorResolver struct {
	namespace             string
	shardDistributionMode dynamicconfig.StringPropertyFn
	client                sharddistributor.Client
	ring                  *ring
	logger                log.Logger
}

func newShardDistributorResolver(
	namespace string,
	client sharddistributor.Client,
	shardDistributionMode dynamicconfig.StringPropertyFn,
	ring *ring,
) SingleProvider {
	return &shardDistributorResolver{
		namespace:             namespace,
		client:                client,
		shardDistributionMode: shardDistributionMode,
		ring:                  ring,
	}
}

func (s shardDistributorResolver) Start() {
	// We do not need to start anything in the shard distributor, so just start the ring
	s.ring.Start()
}

func (s shardDistributorResolver) Stop() {
	// We do not need to stop anything in the shard distributor, so just stop the ring
	s.ring.Stop()
}

func (s shardDistributorResolver) Lookup(key string) (HostInfo, error) {
	switch modeKey(s.shardDistributionMode()) {
	case modeKeyHashRing:
		return s.ring.Lookup(key)
	case modeKeyShardDistributor:
		return s.lookUpInShardDistributor(key)
	case modeKeyHashRingShadowShardDistributor:
		hashRingResult, err := s.ring.Lookup(key)
		if err != nil {
			return HostInfo{}, err
		}
		shardDistributorResult, err := s.lookUpInShardDistributor(key)
		if err != nil {
			s.logger.Warn("Failed to lookup in shard distributor shadow", tag.Error(err))
		}

		if !cmp.Equal(hashRingResult, shardDistributorResult) {
			s.logger.Warn("Shadow lookup mismatch", tag.HashRingResult(hashRingResult), tag.ShardDistributorResult(shardDistributorResult))
		}

		return hashRingResult, nil
	case modeKeyShardDistributorShadowHashRing:
		shardDistributorResult, err := s.lookUpInShardDistributor(key)
		if err != nil {
			return HostInfo{}, err
		}
		hashRingResult, err := s.ring.Lookup(key)
		if err != nil {
			s.logger.Warn("Failed to lookup in hash ring shadow", tag.Error(err))
		}

		if !cmp.Equal(hashRingResult, shardDistributorResult) {
			s.logger.Warn("Shadow lookup mismatch", tag.HashRingResult(hashRingResult), tag.ShardDistributorResult(shardDistributorResult))
		}

		return shardDistributorResult, nil
	}

	// Default to hash ring
	s.logger.Warn("Unknown shard distribution mode, defaulting to hash ring", tag.Value(s.shardDistributionMode()))

	return s.ring.Lookup(key)
}

func (s shardDistributorResolver) Subscribe(name string, channel chan<- *ChangedEvent) error {
	// Shard distributor does not support subscription yet, so use the ring
	return s.ring.Subscribe(name, channel)
}

func (s shardDistributorResolver) Unsubscribe(name string) error {
	// Shard distributor does not support subscription yet, so use the ring
	return s.ring.Unsubscribe(name)
}

func (s shardDistributorResolver) Members() []HostInfo {
	// Shard distributor does not member tracking yet, so use the ring
	return s.ring.Members()
}

func (s shardDistributorResolver) MemberCount() int {
	// Shard distributor does not member tracking yet, so use the ring
	return s.ring.MemberCount()
}

func (s shardDistributorResolver) lookUpInShardDistributor(key string) (HostInfo, error) {
	request := &types.GetShardOwnerRequest{
		ShardKey:  key,
		Namespace: s.namespace,
	}
	response, err := s.client.GetShardOwner(context.Background(), request)
	if err != nil {
		return HostInfo{}, err
	}

	return s.ring.addressToHost(response.Owner)
}
