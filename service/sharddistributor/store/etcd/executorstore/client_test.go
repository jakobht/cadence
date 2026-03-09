package executorstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/uber/cadence/service/sharddistributor/config"
)

func TestNewETCDConfig_WithValidConfig(t *testing.T) {
	etcdCfg := ETCDConfig{
		Endpoints:   []string{"127.0.0.1:2379"},
		DialTimeout: 5 * time.Second,
		Prefix:      "/prefix",
		Compression: "none",
	}

	encoded, err := yaml.Marshal(etcdCfg)
	require.NoError(t, err)

	decoded := &config.YamlNode{}
	err = yaml.Unmarshal(encoded, decoded)
	require.NoError(t, err)

	sdConfig := config.ShardDistribution{
		Store: config.Store{
			StorageParams: decoded,
		},
	}

	resultCfg, err := NewETCDConfig(sdConfig)
	require.NoError(t, err)
	require.Equal(t, etcdCfg, resultCfg)
}

func TestNewETCDConfig_WithInvalidConfig(t *testing.T) {
	encoded, err := yaml.Marshal("")
	require.NoError(t, err)

	decoded := &config.YamlNode{}
	err = yaml.Unmarshal(encoded, decoded)
	require.NoError(t, err)

	sdConfig := config.ShardDistribution{
		Store: config.Store{
			StorageParams: decoded,
		},
	}

	_, err = NewETCDConfig(sdConfig)
	require.Error(t, err)
}
