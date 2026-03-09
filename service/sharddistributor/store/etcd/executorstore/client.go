package executorstore

import (
	"fmt"
	"time"

	"github.com/uber/cadence/service/sharddistributor/config"
)

// ETCDConfig holds the ETCD connection and store configuration.
type ETCDConfig struct {
	Endpoints   []string      `yaml:"endpoints"`
	DialTimeout time.Duration `yaml:"dialTimeout"`
	Prefix      string        `yaml:"prefix"`
	Compression string        `yaml:"compression"`
}

// NewETCDConfig parses ETCDConfig from ShardDistribution config
func NewETCDConfig(cfg config.ShardDistribution) (ETCDConfig, error) {
	var etcdCfg ETCDConfig
	if err := cfg.Store.StorageParams.Decode(&etcdCfg); err != nil {
		return etcdCfg, fmt.Errorf("bad config for etcd store: %w", err)
	}

	return etcdCfg, nil
}
