// The MIT License (MIT)

// Copyright (c) 2017-2020 Uber Technologies Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package handler

import (
	"context"
	"fmt"

	shardmanagerv1 "github.com/uber/cadence/.gen/proto/shardmanager/v1"
	"github.com/uber/cadence/client/matching"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
	"github.com/uber/cadence/service/shardmanager/datastore"
)

func NewGrpcHandler(logger log.Logger, peerResolver matching.PeerResolver, datastore datastore.DataStore) shardmanagerv1.ShardManagerAPIYARPCServer {
	return handlerImpl{
		logger:       logger,
		peerResolver: peerResolver,
		dataStore:    datastore,
	}
}

type handlerImpl struct {
	logger       log.Logger
	peerResolver matching.PeerResolver
	dataStore    datastore.DataStore
}

func (h handlerImpl) GetShardOwner(ctx context.Context, request *shardmanagerv1.GetShardOwnerRequest) (*shardmanagerv1.GetShardOwnerResponse, error) {
	h.logger.Info("GetShardOwner", tag.ShardKey(request.ShardKey))
	var owner string
	var err error

	if false {
		owner, err = h.peerResolver.FromTaskList(request.ShardKey)
	} else {
		owner, err = h.dataStore.GetShardOwner(datastore.ShardID(request.ShardKey))
	}
	if err != nil {
		return nil, fmt.Errorf("get shard owner %w", err)
	}

	resp := &shardmanagerv1.GetShardOwnerResponse{
		ShardKey: request.ShardKey,
		Owner:    owner,
	}

	h.logger.Info("GetShardOwner response", tag.ShardKey(resp.ShardKey), tag.ShardOwner(resp.Owner))

	return resp, nil
}
