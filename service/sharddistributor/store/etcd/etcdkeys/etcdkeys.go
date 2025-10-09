package etcdkeys

import (
	"fmt"
	"strings"
)

const (
	ExecutorHeartbeatKey      = "heartbeat"
	ExecutorStatusKey         = "status"
	ExecutorReportedShardsKey = "reported_shards"
	ExecutorAssignedStateKey  = "assigned_state"
	ShardAssignedKey          = "assigned"
)

func BuildNamespacePrefix(prefix string, namespace string) string {
	return fmt.Sprintf("%s/%s", prefix, namespace)
}

func BuildExecutorPrefix(prefix string, namespace string) string {
	return fmt.Sprintf("%s/executors/", BuildNamespacePrefix(prefix, namespace))
}

func BuildExecutorKey(prefix string, namespace, executorID, keyType string) string {
	return fmt.Sprintf("%s%s/%s", BuildExecutorPrefix(prefix, namespace), executorID, keyType)
}

func ParseExecutorKey(prefix string, namespace, key string) (executorID, keyType string, err error) {
	prefix = BuildExecutorPrefix(prefix, namespace)
	if !strings.HasPrefix(key, prefix) {
		return "", "", fmt.Errorf("key '%s' does not have expected prefix '%s'", key, prefix)
	}
	remainder := strings.TrimPrefix(key, prefix)
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected key format: %s", key)
	}
	return parts[0], parts[1], nil
}
