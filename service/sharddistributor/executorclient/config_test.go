package executorclient

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorManagerConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      ExecutorManagerConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid single executor config",
			config: ExecutorManagerConfig{
				Executors: []ExecutorConfig{
					{ExecutorID: "executor1", Namespace: "namespace1", HeartBeatInterval: 1 * time.Second},
				},
			},
			expectError: false,
		},
		{
			name: "valid multiple executor config",
			config: ExecutorManagerConfig{
				Executors: []ExecutorConfig{
					{ExecutorID: "executor1", Namespace: "namespace1", HeartBeatInterval: 1 * time.Second},
					{ExecutorID: "executor2", Namespace: "namespace2", HeartBeatInterval: 1 * time.Second},
					{ExecutorID: "executor3", Namespace: "namespace3", HeartBeatInterval: 1 * time.Second},
				},
			},
			expectError: false,
		},
		{
			name:        "empty executors list",
			config:      ExecutorManagerConfig{Executors: []ExecutorConfig{}},
			expectError: true,
			errorMsg:    "at least one executor configuration is required",
		},
		{
			name:        "nil executors list",
			config:      ExecutorManagerConfig{Executors: nil},
			expectError: true,
			errorMsg:    "at least one executor configuration is required",
		},
		{
			name: "missing namespace for executor",
			config: ExecutorManagerConfig{
				Executors: []ExecutorConfig{
					{ExecutorID: "executor1", Namespace: "namespace1", HeartBeatInterval: 1 * time.Second},
					{ExecutorID: "executor2", Namespace: "", HeartBeatInterval: 1 * time.Second},
				},
			},
			expectError: true,
			errorMsg:    "executor 1: namespace is required",
		},
		{
			name: "zero heartbeat interval",
			config: ExecutorManagerConfig{
				Executors: []ExecutorConfig{
					{ExecutorID: "executor1", Namespace: "namespace1", HeartBeatInterval: 0},
				},
			},
			expectError: true,
			errorMsg:    "executor 0: heartbeat_interval must be set and greater than 100ms",
		},
		{
			name: "too low heartbeat interval",
			config: ExecutorManagerConfig{
				Executors: []ExecutorConfig{
					{ExecutorID: "executor1", Namespace: "namespace1", HeartBeatInterval: MinHeartbeatInterval / 2},
				},
			},
			expectError: true,
			errorMsg:    "executor 0: heartbeat_interval must be set and greater than 100ms",
		},
		{
			name: "duplicate namespace",
			config: ExecutorManagerConfig{
				Executors: []ExecutorConfig{
					{ExecutorID: "executor1", Namespace: "namespace1", HeartBeatInterval: 30 * time.Second},
					{ExecutorID: "executor2", Namespace: "namespace1", HeartBeatInterval: 60 * time.Second},
				},
			},
			expectError: true,
			errorMsg:    "namespace 'namespace1' is configured for multiple executors",
		},
		{
			name: "valid config with empty executor ID",
			config: ExecutorManagerConfig{
				Executors: []ExecutorConfig{
					{ExecutorID: "", Namespace: "namespace1", HeartBeatInterval: 30 * time.Second},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
