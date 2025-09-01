package executorclient

import (
	"context"
	"fmt"

	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
)

func NewExecutorManager[SP ShardProcessor](params Params[SP]) (ExecutorManager[SP], error) {
	if err := params.Config.Validate(); err != nil {
		return nil, fmt.Errorf("executor manager config: %w", err)
	}

	manager := &executorManager[SP]{
		logger:    params.Logger,
		executors: make(map[string]Executor[SP]),
	}

	// Create individual executors for each namespace
	for _, executorConfig := range params.Config.Executors {
		executor, err := NewExecutor(params, executorConfig)
		if err != nil {
			return nil, fmt.Errorf("create executor for namespace %s: %w", executorConfig.Namespace, err)
		}

		manager.executors[executorConfig.Namespace] = executor
		manager.logger.Info("Created executor for namespace",
			tag.ShardNamespace(executorConfig.Namespace),
			tag.ShardExecutor(executorConfig.ExecutorID),
		)
	}

	return manager, nil
}

type executorManager[SP ShardProcessor] struct {
	logger    log.Logger
	executors map[string]Executor[SP] // namespace -> executor
}

func (em *executorManager[SP]) Start(ctx context.Context) {
	for namespace, executor := range em.executors {
		em.logger.Info("Starting executor for namespace", tag.ShardNamespace(namespace))
		executor.Start(ctx)
	}
}

func (em *executorManager[SP]) Stop() {
	for namespace, executor := range em.executors {
		em.logger.Info("Stopping executor for namespace", tag.ShardNamespace(namespace))
		executor.Stop()
	}

}

func (em *executorManager[SP]) GetShardProcess(namespace, shardID string) (SP, error) {
	executor, ok := em.executors[namespace]
	if !ok {
		var zero SP
		return zero, fmt.Errorf("no executor found for namespace: %s", namespace)
	}

	return executor.GetShardProcess(shardID)
}

func (em *executorManager[SP]) GetExecutorForNamespace(namespace string) (Executor[SP], error) {
	executor, ok := em.executors[namespace]
	if !ok {
		return nil, fmt.Errorf("no executor found for namespace: %s", namespace)
	}
	return executor, nil
}
