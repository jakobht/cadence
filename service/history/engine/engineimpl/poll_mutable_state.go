// Copyright (c) 2017-2021 Uber Technologies, Inc.
// Portions of the Software are attributed to Copyright (c) 2021 Temporal Technologies Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package engineimpl

import (
	"bytes"
	"context"
	"time"

	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/definition"
	"github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/types"
)

// GetMutableState retrieves the mutable state of the workflow execution
func (e *historyEngineImpl) GetMutableState(ctx context.Context, request *types.GetMutableStateRequest) (*types.GetMutableStateResponse, error) {
	return e.getMutableStateOrPolling(ctx, request)
}

// PollMutableState retrieves the mutable state of the workflow execution with long polling
func (e *historyEngineImpl) PollMutableState(ctx context.Context, request *types.PollMutableStateRequest) (*types.PollMutableStateResponse, error) {
	response, err := e.getMutableStateOrPolling(ctx, &types.GetMutableStateRequest{
		DomainUUID:          request.DomainUUID,
		WorkflowExecution:   request.WorkflowExecution,
		ExpectedNextEventID: request.ExpectedNextEventID,
		CurrentBranchToken:  request.CurrentBranchToken})

	if err != nil {
		return nil, e.updateEntityNotExistsErrorOnPassiveCluster(err, request.GetDomainUUID())
	}

	return &types.PollMutableStateResponse{
		Execution:                            response.Execution,
		WorkflowType:                         response.WorkflowType,
		NextEventID:                          response.NextEventID,
		PreviousStartedEventID:               response.PreviousStartedEventID,
		LastFirstEventID:                     response.LastFirstEventID,
		TaskList:                             response.TaskList,
		StickyTaskList:                       response.StickyTaskList,
		ClientLibraryVersion:                 response.ClientLibraryVersion,
		ClientFeatureVersion:                 response.ClientFeatureVersion,
		ClientImpl:                           response.ClientImpl,
		StickyTaskListScheduleToStartTimeout: response.StickyTaskListScheduleToStartTimeout,
		CurrentBranchToken:                   response.CurrentBranchToken,
		VersionHistories:                     response.VersionHistories,
		WorkflowState:                        response.WorkflowState,
		WorkflowCloseState:                   response.WorkflowCloseState,
	}, nil
}

func (e *historyEngineImpl) getMutableState(
	ctx context.Context,
	domainID string,
	execution types.WorkflowExecution,
) (retResp *types.GetMutableStateResponse, retError error) {

	wfContext, release, retError := e.executionCache.GetOrCreateWorkflowExecution(ctx, domainID, execution)
	if retError != nil {
		return
	}
	defer func() { release(retError) }()

	mutableState, retError := wfContext.LoadWorkflowExecution(ctx)
	if retError != nil {
		return
	}

	currentBranchToken, err := mutableState.GetCurrentBranchToken()
	if err != nil {
		return nil, err
	}

	executionInfo := mutableState.GetExecutionInfo()
	execution.RunID = wfContext.GetExecution().RunID
	workflowState, workflowCloseState := mutableState.GetWorkflowStateCloseStatus()
	retResp = &types.GetMutableStateResponse{
		Execution:                            &execution,
		WorkflowType:                         &types.WorkflowType{Name: executionInfo.WorkflowTypeName},
		LastFirstEventID:                     mutableState.GetLastFirstEventID(),
		NextEventID:                          mutableState.GetNextEventID(),
		PreviousStartedEventID:               common.Int64Ptr(mutableState.GetPreviousStartedEventID()),
		TaskList:                             &types.TaskList{Name: executionInfo.TaskList},
		StickyTaskList:                       &types.TaskList{Name: executionInfo.StickyTaskList, Kind: types.TaskListKindSticky.Ptr()},
		ClientLibraryVersion:                 executionInfo.ClientLibraryVersion,
		ClientFeatureVersion:                 executionInfo.ClientFeatureVersion,
		ClientImpl:                           executionInfo.ClientImpl,
		IsWorkflowRunning:                    mutableState.IsWorkflowExecutionRunning(),
		StickyTaskListScheduleToStartTimeout: common.Int32Ptr(executionInfo.StickyScheduleToStartTimeout),
		CurrentBranchToken:                   currentBranchToken,
		WorkflowState:                        common.Int32Ptr(int32(workflowState)),
		WorkflowCloseState:                   common.Int32Ptr(int32(workflowCloseState)),
		IsStickyTaskListEnabled:              mutableState.IsStickyTaskListEnabled(),
		HistorySize:                          mutableState.GetHistorySize(),
	}
	versionHistories := mutableState.GetVersionHistories()
	if versionHistories != nil {
		retResp.VersionHistories = versionHistories.ToInternalType()
	}
	return
}

func (e *historyEngineImpl) updateEntityNotExistsErrorOnPassiveCluster(err error, domainID string) error {
	switch err.(type) {
	case *types.EntityNotExistsError:
		domainEntry, domainCacheErr := e.shard.GetDomainCache().GetDomainByID(domainID)
		if domainCacheErr != nil {
			return err // if could not access domain cache simply return original error
		}

		if _, domainNotActiveErr := domainEntry.IsActiveIn(e.clusterMetadata.GetCurrentClusterName()); domainNotActiveErr != nil {
			domainNotActiveErrCasted := domainNotActiveErr.(*types.DomainNotActiveError)
			return &types.EntityNotExistsError{
				Message:        "Workflow execution not found in non-active cluster",
				ActiveCluster:  domainNotActiveErrCasted.GetActiveCluster(),
				CurrentCluster: domainNotActiveErrCasted.GetCurrentCluster(),
			}
		}
	}
	return err
}

func (e *historyEngineImpl) getMutableStateOrPolling(
	ctx context.Context,
	request *types.GetMutableStateRequest,
) (*types.GetMutableStateResponse, error) {

	if err := common.ValidateDomainUUID(request.DomainUUID); err != nil {
		return nil, err
	}
	domainID := request.DomainUUID
	execution := types.WorkflowExecution{
		WorkflowID: request.WorkflowExecution.WorkflowID,
		RunID:      request.WorkflowExecution.RunID,
	}
	response, err := e.getMutableState(ctx, domainID, execution)
	if err != nil {
		return nil, err
	}
	if request.CurrentBranchToken == nil {
		request.CurrentBranchToken = response.CurrentBranchToken
	}
	if !bytes.Equal(request.CurrentBranchToken, response.CurrentBranchToken) {
		return nil, &types.CurrentBranchChangedError{
			Message:            "current branch token and request branch token doesn't match",
			CurrentBranchToken: response.CurrentBranchToken}
	}
	// set the run id in case query the current running workflow
	execution.RunID = response.Execution.RunID

	// expectedNextEventID is 0 when caller want to get the current next event ID without blocking
	expectedNextEventID := common.FirstEventID
	if request.ExpectedNextEventID != 0 {
		expectedNextEventID = request.GetExpectedNextEventID()
	}

	// if caller decide to long poll on workflow execution
	// and the event ID we are looking for is smaller than current next event ID
	if expectedNextEventID >= response.GetNextEventID() && response.GetIsWorkflowRunning() {
		subscriberID, channel, err := e.historyEventNotifier.WatchHistoryEvent(definition.NewWorkflowIdentifier(domainID, execution.GetWorkflowID(), execution.GetRunID()))
		if err != nil {
			return nil, err
		}
		defer e.historyEventNotifier.UnwatchHistoryEvent(definition.NewWorkflowIdentifier(domainID, execution.GetWorkflowID(), execution.GetRunID()), subscriberID) //nolint:errcheck
		// check again in case the next event ID is updated
		response, err = e.getMutableState(ctx, domainID, execution)
		if err != nil {
			return nil, err
		}
		// check again if the current branch token changed
		if !bytes.Equal(request.CurrentBranchToken, response.CurrentBranchToken) {
			return nil, &types.CurrentBranchChangedError{
				Message:            "current branch token and request branch token doesn't match",
				CurrentBranchToken: response.CurrentBranchToken}
		}
		if expectedNextEventID < response.GetNextEventID() || !response.GetIsWorkflowRunning() {
			return response, nil
		}

		domainName, err := e.shard.GetDomainCache().GetDomainName(domainID)
		if err != nil {
			return nil, err
		}

		expirationInterval := e.shard.GetConfig().LongPollExpirationInterval(domainName)
		if deadline, ok := ctx.Deadline(); ok {
			remainingTime := deadline.Sub(e.shard.GetTimeSource().Now())
			// Here we return a safeguard error, to ensure that older clients are not stuck in long poll loop until context fully expires.
			// Otherwise it results in multiple additional requests being made that returns empty responses.
			// Newer clients will not make request with too small timeout remaining.
			if remainingTime < longPollCompletionBuffer {
				return nil, context.DeadlineExceeded
			}
			// longPollCompletionBuffer is here to leave some room to finish current request without its timeout.
			expirationInterval = common.MinDuration(
				expirationInterval,
				remainingTime-longPollCompletionBuffer,
			)
		}
		if expirationInterval <= 0 {
			return response, nil
		}
		timer := time.NewTimer(expirationInterval)
		defer timer.Stop()
		for {
			select {
			case event := <-channel:
				response.LastFirstEventID = event.LastFirstEventID
				response.NextEventID = event.NextEventID
				response.IsWorkflowRunning = event.WorkflowCloseState == persistence.WorkflowCloseStatusNone
				response.PreviousStartedEventID = common.Int64Ptr(event.PreviousStartedEventID)
				response.WorkflowState = common.Int32Ptr(int32(event.WorkflowState))
				response.WorkflowCloseState = common.Int32Ptr(int32(event.WorkflowCloseState))
				if !bytes.Equal(request.CurrentBranchToken, event.CurrentBranchToken) {
					return nil, &types.CurrentBranchChangedError{
						Message:            "Current branch token and request branch token doesn't match",
						CurrentBranchToken: event.CurrentBranchToken}
				}
				if expectedNextEventID < response.GetNextEventID() || !response.GetIsWorkflowRunning() {
					return response, nil
				}
			case <-timer.C:
				return response, nil
			}
		}
	}

	return response, nil
}
