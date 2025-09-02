package executorclient

import (
	"fmt"
	"time"
)

const (
	// MinHeartbeatInterval is the minimum allowed heartbeat interval for executors
	MinHeartbeatInterval = 100 * time.Millisecond
)

type ExecutorConfig struct {
	ExecutorID        string        `yaml:"executor_id"` // Optional: if not provided, will be auto-generated
	Namespace         string        `yaml:"namespace"`
	HeartBeatInterval time.Duration `yaml:"heartbeat_interval"`
}

type ExecutorManagerConfig struct {
	Executors []ExecutorConfig `yaml:"executors" json:"executors"`
}

// Validate validates the multi-executor configuration
func (c *ExecutorManagerConfig) Validate() error {
	if len(c.Executors) == 0 {
		return fmt.Errorf("at least one executor configuration is required")
	}

	// Check for duplicate namespaces
	namespaceExecutors := make(map[string]struct{})

	for i, executor := range c.Executors {
		if executor.Namespace == "" {
			return fmt.Errorf("executor %d: namespace is required", i)
		}
		if executor.HeartBeatInterval < MinHeartbeatInterval {
			return fmt.Errorf("executor %d: heartbeat_interval must be set and greater than %v", i, MinHeartbeatInterval)
		}

		if _, ok := namespaceExecutors[executor.Namespace]; ok {
			return fmt.Errorf("namespace '%s' is configured for multiple executors", executor.Namespace)
		}
		namespaceExecutors[executor.Namespace] = struct{}{}
	}

	return nil
}
