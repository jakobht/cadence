// Copyright (c) 2019 Uber Technologies, Inc.
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

package shardmanager

import (
	"sync/atomic"

	shardmanagerv1 "github.com/uber/cadence/.gen/proto/shardmanager/v1"
	"github.com/uber/cadence/client/matching"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/dynamicconfig"
	"github.com/uber/cadence/common/membership"
	"github.com/uber/cadence/common/resource"
	"github.com/uber/cadence/common/service"
	"github.com/uber/cadence/service/shardmanager/config"
	"github.com/uber/cadence/service/shardmanager/controlhandler"
	"github.com/uber/cadence/service/shardmanager/datastore"
	"github.com/uber/cadence/service/shardmanager/handler"
)

// Service represents the shard manager service
type Service struct {
	resource.Resource

	status         int32
	handler        shardmanagerv1.ShardManagerAPIYARPCServer
	controlHandler shardmanagerv1.ShardManagerControlAPIYARPCServer
	datastore      datastore.DataStore
	stopC          chan struct{}
	config         *config.Config
}

// NewService builds a new task manager service
func NewService(
	params *resource.Params,
	factory resource.ResourceFactory,
) (resource.Resource, error) {

	serviceConfig := config.NewConfig(
		dynamicconfig.NewCollection(
			params.DynamicConfig,
			params.Logger,
			dynamicconfig.ClusterNameFilter(params.ClusterMetadata.GetCurrentClusterName()),
		),
		params.HostName,
	)

	serviceResource, err := factory.NewResource(
		params,
		service.ShardManager,
		&service.Config{
			PersistenceMaxQPS:        serviceConfig.PersistenceMaxQPS,
			PersistenceGlobalMaxQPS:  serviceConfig.PersistenceGlobalMaxQPS,
			ThrottledLoggerMaxRPS:    serviceConfig.ThrottledLogRPS,
			IsErrorRetryableFunction: common.IsServiceTransientError,
			// shard manager doesn't need visibility config as it never read or write visibility
		},
	)
	if err != nil {
		return nil, err
	}

	return &Service{
		Resource: serviceResource,
		status:   common.DaemonStatusInitialized,
		config:   serviceConfig,
		stopC:    make(chan struct{}),
	}, nil
}

// Start starts the service
func (s *Service) Start() {
	if !atomic.CompareAndSwapInt32(&s.status, common.DaemonStatusInitialized, common.DaemonStatusStarted) {
		return
	}

	logger := s.GetLogger()
	logger.Info("shard manager starting")

	peerResolver := matching.NewPeerResolver(s.GetMembershipResolver(), membership.PortGRPC)

	s.datastore = datastore.NewDataStore(peerResolver, logger)
	s.datastore.Start()
	s.handler = handler.NewGrpcHandler(s.GetLogger(), peerResolver, s.datastore)
	s.controlHandler = controlhandler.NewGrpcHandler(s.GetLogger(), peerResolver, s.datastore)

	s.GetDispatcher().Register(shardmanagerv1.BuildShardManagerAPIYARPCProcedures(s.handler))
	s.GetDispatcher().Register(shardmanagerv1.BuildShardManagerControlAPIYARPCProcedures(s.controlHandler))

	// TODO: add health check handler
	s.Resource.Start()

	logger.Info("shard manager started")

	<-s.stopC
}

func (s *Service) Stop() {
	if !atomic.CompareAndSwapInt32(&s.status, common.DaemonStatusStarted, common.DaemonStatusStopped) {
		return
	}

	close(s.stopC)

	s.Resource.Stop()
	s.datastore.Stop()

	s.GetLogger().Info("shard manager stopped")
}
