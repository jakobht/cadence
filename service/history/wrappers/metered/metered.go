package metered

import (
	"context"
	"errors"

	"github.com/pborman/uuid"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/common/membership"
	"github.com/uber/cadence/common/metrics"
	"github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/types"
	"github.com/uber/cadence/service/history/constants"
	"github.com/uber/cadence/service/history/lookup"
	"github.com/uber/cadence/service/history/shard"
	"go.uber.org/yarpc/yarpcerrors"
)

func (h *historyHandler) startRequestProfile(ctx context.Context, scope int) (metrics.Scope, metrics.Stopwatch) {
	metricsScope := h.metricsClient.Scope(scope, metrics.GetContextTags(ctx)...)
	metricsScope.IncCounter(metrics.CadenceRequests)
	sw := metricsScope.StartTimer(metrics.CadenceLatency)
	return metricsScope, sw
}

// convertError is a helper method to convert ShardOwnershipLostError from persistence layer returned by various
// HistoryEngine API calls to ShardOwnershipLost error return by HistoryService for client to be redirected to the
// correct shard.
func (h *historyHandler) convertError(err error) error {
	switch err := err.(type) {
	case *persistence.ShardOwnershipLostError:
		info, err2 := lookup.HistoryServerByShardID(h.memberShipResolver, err.ShardID)
		if err2 != nil {
			return shard.CreateShardOwnershipLostError(h.hostInfo, membership.HostInfo{})
		}

		return shard.CreateShardOwnershipLostError(h.hostInfo, info)
	case *persistence.WorkflowExecutionAlreadyStartedError:
		return &types.InternalServiceError{Message: err.Msg}
	case *persistence.CurrentWorkflowConditionFailedError:
		return &types.InternalServiceError{Message: err.Msg}
	case *persistence.TimeoutError:
		return &types.InternalServiceError{Message: err.Msg}
	case *persistence.TransactionSizeLimitError:
		return &types.BadRequestError{Message: err.Msg}
	}

	return err
}

func (h *historyHandler) updateErrorMetric(
	scope metrics.Scope,
	domainID string,
	workflowID string,
	runID string,
	err error,
) {

	var yarpcE *yarpcerrors.Status

	var shardOwnershipLostError *types.ShardOwnershipLostError
	var eventAlreadyStartedError *types.EventAlreadyStartedError
	var badRequestError *types.BadRequestError
	var domainNotActiveError *types.DomainNotActiveError
	var workflowExecutionAlreadyStartedError *types.WorkflowExecutionAlreadyStartedError
	var entityNotExistsError *types.EntityNotExistsError
	var workflowExecutionAlreadyCompletedError *types.WorkflowExecutionAlreadyCompletedError
	var cancellationAlreadyRequestedError *types.CancellationAlreadyRequestedError
	var limitExceededError *types.LimitExceededError
	var retryTaskV2Error *types.RetryTaskV2Error
	var serviceBusyError *types.ServiceBusyError
	var internalServiceError *types.InternalServiceError
	var queryFailedError *types.QueryFailedError

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		scope.IncCounter(metrics.CadenceErrContextTimeoutCounter)
		return
	}

	if errors.As(err, &shardOwnershipLostError) {
		scope.IncCounter(metrics.CadenceErrShardOwnershipLostCounter)

	} else if errors.As(err, &eventAlreadyStartedError) {
		scope.IncCounter(metrics.CadenceErrEventAlreadyStartedCounter)

	} else if errors.As(err, &badRequestError) {
		scope.IncCounter(metrics.CadenceErrBadRequestCounter)

	} else if errors.As(err, &domainNotActiveError) {
		scope.IncCounter(metrics.CadenceErrDomainNotActiveCounter)

	} else if errors.As(err, &workflowExecutionAlreadyStartedError) {
		scope.IncCounter(metrics.CadenceErrExecutionAlreadyStartedCounter)

	} else if errors.As(err, &entityNotExistsError) {
		scope.IncCounter(metrics.CadenceErrEntityNotExistsCounter)

	} else if errors.As(err, &workflowExecutionAlreadyCompletedError) {
		scope.IncCounter(metrics.CadenceErrWorkflowExecutionAlreadyCompletedCounter)

	} else if errors.As(err, &cancellationAlreadyRequestedError) {
		scope.IncCounter(metrics.CadenceErrCancellationAlreadyRequestedCounter)

	} else if errors.As(err, &limitExceededError) {
		scope.IncCounter(metrics.CadenceErrLimitExceededCounter)

	} else if errors.As(err, &retryTaskV2Error) {
		scope.IncCounter(metrics.CadenceErrRetryTaskCounter)

	} else if errors.As(err, &serviceBusyError) {
		scope.IncCounter(metrics.CadenceErrServiceBusyCounter)

	} else if errors.As(err, &queryFailedError) {
		scope.IncCounter(metrics.CadenceErrQueryFailedCounter)

	} else if errors.As(err, &yarpcE) {
		if yarpcE.Code() == yarpcerrors.CodeDeadlineExceeded {
			scope.IncCounter(metrics.CadenceErrContextTimeoutCounter)
		}
		scope.IncCounter(metrics.CadenceFailures)

	} else if errors.As(err, &internalServiceError) {
		scope.IncCounter(metrics.CadenceFailures)
		h.logger.Error("Internal service error",
			tag.Error(err),
			tag.WorkflowID(workflowID),
			tag.WorkflowRunID(runID),
			tag.WorkflowDomainID(domainID))

	} else {
		// Default / unknown error fallback
		scope.IncCounter(metrics.CadenceFailures)
		h.logger.Error("Uncategorized error",
			tag.Error(err),
			tag.WorkflowID(workflowID),
			tag.WorkflowRunID(runID),
			tag.WorkflowDomainID(domainID))
	}
}

func (h *historyHandler) error(
	err error,
	scope metrics.Scope,
	domainID string,
	workflowID string,
	runID string,
) error {
	err = h.convertError(err)

	h.updateErrorMetric(scope, domainID, workflowID, runID, err)
	return err
}

func validateTaskToken(token *common.TaskToken) error {
	if token.WorkflowID == "" {
		return constants.ErrWorkflowIDNotSet
	}
	if token.RunID != "" && uuid.Parse(token.RunID) == nil {
		return constants.ErrRunIDNotValid
	}
	return nil
}
