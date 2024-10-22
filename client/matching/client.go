// Copyright (c) 2017 Uber Technologies, Inc.
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

package matching

import (
	"context"

	"go.uber.org/yarpc"

	shardmanagerv1 "github.com/uber/cadence/.gen/proto/shardmanager/v1"
	"github.com/uber/cadence/common/future"
	"github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/types"
)

var _ Client = (*clientImpl)(nil)

type clientImpl struct {
	client             Client
	shardManagerClient shardmanagerv1.ShardManagerAPIYARPCClient
	peerResolver       PeerResolver
	loadBalancer       LoadBalancer
}

// NewClient creates a new history service TChannel client
func NewClient(
	client Client,
	peerResolver PeerResolver,
	shardManagerClient shardmanagerv1.ShardManagerAPIYARPCClient,
	lb LoadBalancer,
) Client {
	return &clientImpl{
		client:             client,
		peerResolver:       peerResolver,
		shardManagerClient: shardManagerClient,
		loadBalancer:       lb,
	}
}

func (c *clientImpl) AddActivityTask(
	ctx context.Context,
	request *types.AddActivityTaskRequest,
	opts ...yarpc.CallOption,
) (*types.AddActivityTaskResponse, error) {
	partition := c.loadBalancer.PickWritePartition(
		request.GetDomainUUID(),
		*request.GetTaskList(),
		persistence.TaskListTypeActivity,
		request.GetForwardedFrom(),
	)
	request.TaskList.Name = partition
	peer, err := c.getPeerFromTasklist(ctx, request.TaskList.GetName())
	if err != nil {
		return nil, err
	}
	return c.client.AddActivityTask(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) AddDecisionTask(
	ctx context.Context,
	request *types.AddDecisionTaskRequest,
	opts ...yarpc.CallOption,
) (*types.AddDecisionTaskResponse, error) {
	partition := c.loadBalancer.PickWritePartition(
		request.GetDomainUUID(),
		*request.GetTaskList(),
		persistence.TaskListTypeDecision,
		request.GetForwardedFrom(),
	)
	request.TaskList.Name = partition
	peer, err := c.getPeerFromTasklist(ctx, request.TaskList.GetName())
	if err != nil {
		return nil, err
	}
	return c.client.AddDecisionTask(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) PollForActivityTask(
	ctx context.Context,
	request *types.MatchingPollForActivityTaskRequest,
	opts ...yarpc.CallOption,
) (*types.MatchingPollForActivityTaskResponse, error) {
	partition := c.loadBalancer.PickReadPartition(
		request.GetDomainUUID(),
		*request.PollRequest.GetTaskList(),
		persistence.TaskListTypeActivity,
		request.GetForwardedFrom(),
	)
	request.PollRequest.TaskList.Name = partition
	peer, err := c.getPeerFromTasklist(ctx, request.PollRequest.TaskList.GetName())
	if err != nil {
		return nil, err
	}
	// TODO: update activity response to include backlog count hint and update the weight for partitions
	return c.client.PollForActivityTask(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) PollForDecisionTask(
	ctx context.Context,
	request *types.MatchingPollForDecisionTaskRequest,
	opts ...yarpc.CallOption,
) (*types.MatchingPollForDecisionTaskResponse, error) {
	partition := c.loadBalancer.PickReadPartition(
		request.GetDomainUUID(),
		*request.PollRequest.GetTaskList(),
		persistence.TaskListTypeDecision,
		request.GetForwardedFrom(),
	)
	originalTaskListName := request.PollRequest.GetTaskList().GetName()
	request.PollRequest.TaskList.Name = partition
	peer, err := c.getPeerFromTasklist(ctx, request.PollRequest.TaskList.GetName())
	if err != nil {
		return nil, err
	}
	resp, err := c.client.PollForDecisionTask(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
	if err != nil {
		return nil, err
	}
	request.PollRequest.TaskList.Name = originalTaskListName
	c.loadBalancer.UpdateWeight(
		request.GetDomainUUID(),
		*request.PollRequest.GetTaskList(),
		persistence.TaskListTypeDecision,
		request.GetForwardedFrom(),
		partition,
		resp.BacklogCountHint,
	)
	return resp, nil
}

func (c *clientImpl) QueryWorkflow(
	ctx context.Context,
	request *types.MatchingQueryWorkflowRequest,
	opts ...yarpc.CallOption,
) (*types.QueryWorkflowResponse, error) {
	partition := c.loadBalancer.PickReadPartition(
		request.GetDomainUUID(),
		*request.GetTaskList(),
		persistence.TaskListTypeDecision,
		request.GetForwardedFrom(),
	)
	request.TaskList.Name = partition
	peer, err := c.getPeerFromTasklist(ctx, request.TaskList.GetName())
	if err != nil {
		return nil, err
	}
	return c.client.QueryWorkflow(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) RespondQueryTaskCompleted(
	ctx context.Context,
	request *types.MatchingRespondQueryTaskCompletedRequest,
	opts ...yarpc.CallOption,
) error {
	peer, err := c.getPeerFromTasklist(ctx, request.TaskList.GetName())
	if err != nil {
		return err
	}
	return c.client.RespondQueryTaskCompleted(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) CancelOutstandingPoll(
	ctx context.Context,
	request *types.CancelOutstandingPollRequest,
	opts ...yarpc.CallOption,
) error {
	peer, err := c.getPeerFromTasklist(ctx, request.TaskList.GetName())
	if err != nil {
		return err
	}
	return c.client.CancelOutstandingPoll(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) DescribeTaskList(
	ctx context.Context,
	request *types.MatchingDescribeTaskListRequest,
	opts ...yarpc.CallOption,
) (*types.DescribeTaskListResponse, error) {
	peer, err := c.getPeerFromTasklist(ctx, request.DescRequest.TaskList.GetName())
	if err != nil {
		return nil, err
	}
	return c.client.DescribeTaskList(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) ListTaskListPartitions(
	ctx context.Context,
	request *types.MatchingListTaskListPartitionsRequest,
	opts ...yarpc.CallOption,
) (*types.ListTaskListPartitionsResponse, error) {
	peer, err := c.getPeerFromTasklist(ctx, request.TaskList.GetName())
	if err != nil {
		return nil, err
	}
	return c.client.ListTaskListPartitions(ctx, request, append(opts, yarpc.WithShardKey(peer))...)
}

func (c *clientImpl) GetTaskListsByDomain(
	ctx context.Context,
	request *types.GetTaskListsByDomainRequest,
	opts ...yarpc.CallOption,
) (*types.GetTaskListsByDomainResponse, error) {
	peers, err := c.peerResolver.GetAllPeers()
	if err != nil {
		return nil, err
	}

	var futures []future.Future
	for _, peer := range peers {
		future, settable := future.NewFuture()
		settable.Set(c.client.GetTaskListsByDomain(ctx, request, append(opts, yarpc.WithShardKey(peer))...))
		futures = append(futures, future)
	}

	decisionTaskListMap := make(map[string]*types.DescribeTaskListResponse)
	activityTaskListMap := make(map[string]*types.DescribeTaskListResponse)
	for _, future := range futures {
		var resp *types.GetTaskListsByDomainResponse
		if err = future.Get(ctx, &resp); err != nil {
			return nil, err
		}
		for name, tl := range resp.GetDecisionTaskListMap() {
			if _, ok := decisionTaskListMap[name]; !ok {
				decisionTaskListMap[name] = tl
			} else {
				decisionTaskListMap[name].Pollers = append(decisionTaskListMap[name].Pollers, tl.GetPollers()...)
			}
		}
		for name, tl := range resp.GetActivityTaskListMap() {
			if _, ok := activityTaskListMap[name]; !ok {
				activityTaskListMap[name] = tl
			} else {
				activityTaskListMap[name].Pollers = append(activityTaskListMap[name].Pollers, tl.GetPollers()...)
			}
		}
	}

	return &types.GetTaskListsByDomainResponse{
		DecisionTaskListMap: decisionTaskListMap,
		ActivityTaskListMap: activityTaskListMap,
	}, nil
}

func (c *clientImpl) getPeerFromTasklist(ctx context.Context, taskListName string) (string, error) {
	if false {
		return c.peerResolver.FromTaskList(taskListName)
	}

	resp, err := c.shardManagerClient.GetShardOwner(ctx, &shardmanagerv1.GetShardOwnerRequest{
		ShardKey: taskListName,
	})
	if err != nil {
		return "", err
	}

	return resp.Owner, nil
}
