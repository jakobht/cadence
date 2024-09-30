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

// Code generated by protoc-gen-yarpc-go. DO NOT EDIT.
// source: uber/cadence/shardmanager/v1/service.proto

package shardmanagerv1

import (
	"context"
	"io/ioutil"
	"reflect"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"go.uber.org/fx"
	"go.uber.org/yarpc"
	"go.uber.org/yarpc/api/transport"
	"go.uber.org/yarpc/api/x/restriction"
	"go.uber.org/yarpc/encoding/protobuf"
	"go.uber.org/yarpc/encoding/protobuf/reflection"
)

var _ = ioutil.NopCloser

// ShardManagerAPIYARPCClient is the YARPC client-side interface for the ShardManagerAPI service.
type ShardManagerAPIYARPCClient interface {
	GetShardOwner(context.Context, ...yarpc.CallOption) (ShardManagerAPIServiceGetShardOwnerYARPCClient, error)
}

// ShardManagerAPIServiceGetShardOwnerYARPCClient sends GetShardOwnerRequests and receives the single GetShardOwnerResponse when sending is done.
type ShardManagerAPIServiceGetShardOwnerYARPCClient interface {
	Context() context.Context
	Send(*GetShardOwnerRequest, ...yarpc.StreamOption) error
	CloseAndRecv(...yarpc.StreamOption) (*GetShardOwnerResponse, error)
}

func newShardManagerAPIYARPCClient(clientConfig transport.ClientConfig, anyResolver jsonpb.AnyResolver, options ...protobuf.ClientOption) ShardManagerAPIYARPCClient {
	return &_ShardManagerAPIYARPCCaller{protobuf.NewStreamClient(
		protobuf.ClientParams{
			ServiceName:  "uber.cadence.shardmanager.v1.ShardManagerAPI",
			ClientConfig: clientConfig,
			AnyResolver:  anyResolver,
			Options:      options,
		},
	)}
}

// NewShardManagerAPIYARPCClient builds a new YARPC client for the ShardManagerAPI service.
func NewShardManagerAPIYARPCClient(clientConfig transport.ClientConfig, options ...protobuf.ClientOption) ShardManagerAPIYARPCClient {
	return newShardManagerAPIYARPCClient(clientConfig, nil, options...)
}

// ShardManagerAPIYARPCServer is the YARPC server-side interface for the ShardManagerAPI service.
type ShardManagerAPIYARPCServer interface {
	GetShardOwner(ShardManagerAPIServiceGetShardOwnerYARPCServer) (*GetShardOwnerResponse, error)
}

// ShardManagerAPIServiceGetShardOwnerYARPCServer receives GetShardOwnerRequests.
type ShardManagerAPIServiceGetShardOwnerYARPCServer interface {
	Context() context.Context
	Recv(...yarpc.StreamOption) (*GetShardOwnerRequest, error)
}

type buildShardManagerAPIYARPCProceduresParams struct {
	Server      ShardManagerAPIYARPCServer
	AnyResolver jsonpb.AnyResolver
}

func buildShardManagerAPIYARPCProcedures(params buildShardManagerAPIYARPCProceduresParams) []transport.Procedure {
	handler := &_ShardManagerAPIYARPCHandler{params.Server}
	return protobuf.BuildProcedures(
		protobuf.BuildProceduresParams{
			ServiceName:         "uber.cadence.shardmanager.v1.ShardManagerAPI",
			UnaryHandlerParams:  []protobuf.BuildProceduresUnaryHandlerParams{},
			OnewayHandlerParams: []protobuf.BuildProceduresOnewayHandlerParams{},
			StreamHandlerParams: []protobuf.BuildProceduresStreamHandlerParams{

				{
					MethodName: "GetShardOwner",
					Handler: protobuf.NewStreamHandler(
						protobuf.StreamHandlerParams{
							Handle: handler.GetShardOwner,
						},
					),
				},
			},
		},
	)
}

// BuildShardManagerAPIYARPCProcedures prepares an implementation of the ShardManagerAPI service for YARPC registration.
func BuildShardManagerAPIYARPCProcedures(server ShardManagerAPIYARPCServer) []transport.Procedure {
	return buildShardManagerAPIYARPCProcedures(buildShardManagerAPIYARPCProceduresParams{Server: server})
}

// FxShardManagerAPIYARPCClientParams defines the input
// for NewFxShardManagerAPIYARPCClient. It provides the
// paramaters to get a ShardManagerAPIYARPCClient in an
// Fx application.
type FxShardManagerAPIYARPCClientParams struct {
	fx.In

	Provider    yarpc.ClientConfig
	AnyResolver jsonpb.AnyResolver  `name:"yarpcfx" optional:"true"`
	Restriction restriction.Checker `optional:"true"`
}

// FxShardManagerAPIYARPCClientResult defines the output
// of NewFxShardManagerAPIYARPCClient. It provides a
// ShardManagerAPIYARPCClient to an Fx application.
type FxShardManagerAPIYARPCClientResult struct {
	fx.Out

	Client ShardManagerAPIYARPCClient

	// We are using an fx.Out struct here instead of just returning a client
	// so that we can add more values or add named versions of the client in
	// the future without breaking any existing code.
}

// NewFxShardManagerAPIYARPCClient provides a ShardManagerAPIYARPCClient
// to an Fx application using the given name for routing.
//
//	fx.Provide(
//	  shardmanagerv1.NewFxShardManagerAPIYARPCClient("service-name"),
//	  ...
//	)
func NewFxShardManagerAPIYARPCClient(name string, options ...protobuf.ClientOption) interface{} {
	return func(params FxShardManagerAPIYARPCClientParams) FxShardManagerAPIYARPCClientResult {
		cc := params.Provider.ClientConfig(name)

		if params.Restriction != nil {
			if namer, ok := cc.GetUnaryOutbound().(transport.Namer); ok {
				if err := params.Restriction.Check(protobuf.Encoding, namer.TransportName()); err != nil {
					panic(err.Error())
				}
			}
		}

		return FxShardManagerAPIYARPCClientResult{
			Client: newShardManagerAPIYARPCClient(cc, params.AnyResolver, options...),
		}
	}
}

// FxShardManagerAPIYARPCProceduresParams defines the input
// for NewFxShardManagerAPIYARPCProcedures. It provides the
// paramaters to get ShardManagerAPIYARPCServer procedures in an
// Fx application.
type FxShardManagerAPIYARPCProceduresParams struct {
	fx.In

	Server      ShardManagerAPIYARPCServer
	AnyResolver jsonpb.AnyResolver `name:"yarpcfx" optional:"true"`
}

// FxShardManagerAPIYARPCProceduresResult defines the output
// of NewFxShardManagerAPIYARPCProcedures. It provides
// ShardManagerAPIYARPCServer procedures to an Fx application.
//
// The procedures are provided to the "yarpcfx" value group.
// Dig 1.2 or newer must be used for this feature to work.
type FxShardManagerAPIYARPCProceduresResult struct {
	fx.Out

	Procedures     []transport.Procedure `group:"yarpcfx"`
	ReflectionMeta reflection.ServerMeta `group:"yarpcfx"`
}

// NewFxShardManagerAPIYARPCProcedures provides ShardManagerAPIYARPCServer procedures to an Fx application.
// It expects a ShardManagerAPIYARPCServer to be present in the container.
//
//	fx.Provide(
//	  shardmanagerv1.NewFxShardManagerAPIYARPCProcedures(),
//	  ...
//	)
func NewFxShardManagerAPIYARPCProcedures() interface{} {
	return func(params FxShardManagerAPIYARPCProceduresParams) FxShardManagerAPIYARPCProceduresResult {
		return FxShardManagerAPIYARPCProceduresResult{
			Procedures: buildShardManagerAPIYARPCProcedures(buildShardManagerAPIYARPCProceduresParams{
				Server:      params.Server,
				AnyResolver: params.AnyResolver,
			}),
			ReflectionMeta: ShardManagerAPIReflectionMeta,
		}
	}
}

// ShardManagerAPIReflectionMeta is the reflection server metadata
// required for using the gRPC reflection protocol with YARPC.
//
// See https://github.com/grpc/grpc/blob/master/doc/server-reflection.md.
var ShardManagerAPIReflectionMeta = reflection.ServerMeta{
	ServiceName:     "uber.cadence.shardmanager.v1.ShardManagerAPI",
	FileDescriptors: yarpcFileDescriptorClosure889886f494a83aa4,
}

type _ShardManagerAPIYARPCCaller struct {
	streamClient protobuf.StreamClient
}

func (c *_ShardManagerAPIYARPCCaller) GetShardOwner(ctx context.Context, options ...yarpc.CallOption) (ShardManagerAPIServiceGetShardOwnerYARPCClient, error) {
	stream, err := c.streamClient.CallStream(ctx, "GetShardOwner", options...)
	if err != nil {
		return nil, err
	}
	return &_ShardManagerAPIServiceGetShardOwnerYARPCClient{stream: stream}, nil
}

type _ShardManagerAPIYARPCHandler struct {
	server ShardManagerAPIYARPCServer
}

func (h *_ShardManagerAPIYARPCHandler) GetShardOwner(serverStream *protobuf.ServerStream) error {
	response, err := h.server.GetShardOwner(&_ShardManagerAPIServiceGetShardOwnerYARPCServer{serverStream: serverStream})
	if err != nil {
		return err
	}
	return serverStream.Send(response)
}

type _ShardManagerAPIServiceGetShardOwnerYARPCClient struct {
	stream *protobuf.ClientStream
}

func (c *_ShardManagerAPIServiceGetShardOwnerYARPCClient) Context() context.Context {
	return c.stream.Context()
}

func (c *_ShardManagerAPIServiceGetShardOwnerYARPCClient) Send(request *GetShardOwnerRequest, options ...yarpc.StreamOption) error {
	return c.stream.Send(request, options...)
}

func (c *_ShardManagerAPIServiceGetShardOwnerYARPCClient) CloseAndRecv(options ...yarpc.StreamOption) (*GetShardOwnerResponse, error) {
	if err := c.stream.Close(options...); err != nil {
		return nil, err
	}
	responseMessage, err := c.stream.Receive(newShardManagerAPIServiceGetShardOwnerYARPCResponse, options...)
	if responseMessage == nil {
		return nil, err
	}
	response, ok := responseMessage.(*GetShardOwnerResponse)
	if !ok {
		return nil, protobuf.CastError(emptyShardManagerAPIServiceGetShardOwnerYARPCResponse, responseMessage)
	}
	return response, err
}

type _ShardManagerAPIServiceGetShardOwnerYARPCServer struct {
	serverStream *protobuf.ServerStream
}

func (s *_ShardManagerAPIServiceGetShardOwnerYARPCServer) Context() context.Context {
	return s.serverStream.Context()
}

func (s *_ShardManagerAPIServiceGetShardOwnerYARPCServer) Recv(options ...yarpc.StreamOption) (*GetShardOwnerRequest, error) {
	requestMessage, err := s.serverStream.Receive(newShardManagerAPIServiceGetShardOwnerYARPCRequest, options...)
	if requestMessage == nil {
		return nil, err
	}
	request, ok := requestMessage.(*GetShardOwnerRequest)
	if !ok {
		return nil, protobuf.CastError(emptyShardManagerAPIServiceGetShardOwnerYARPCRequest, requestMessage)
	}
	return request, err
}

func newShardManagerAPIServiceGetShardOwnerYARPCRequest() proto.Message {
	return &GetShardOwnerRequest{}
}

func newShardManagerAPIServiceGetShardOwnerYARPCResponse() proto.Message {
	return &GetShardOwnerResponse{}
}

var (
	emptyShardManagerAPIServiceGetShardOwnerYARPCRequest  = &GetShardOwnerRequest{}
	emptyShardManagerAPIServiceGetShardOwnerYARPCResponse = &GetShardOwnerResponse{}
)

var yarpcFileDescriptorClosure889886f494a83aa4 = [][]byte{
	// uber/cadence/shardmanager/v1/service.proto
	[]byte{
		0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0xd2, 0x2a, 0x4d, 0x4a, 0x2d,
		0xd2, 0x4f, 0x4e, 0x4c, 0x49, 0xcd, 0x4b, 0x4e, 0xd5, 0x2f, 0xce, 0x48, 0x2c, 0x4a, 0xc9, 0x4d,
		0xcc, 0x4b, 0x4c, 0x4f, 0x2d, 0xd2, 0x2f, 0x33, 0xd4, 0x2f, 0x4e, 0x2d, 0x2a, 0xcb, 0x4c, 0x4e,
		0xd5, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x92, 0x01, 0xa9, 0xd5, 0x83, 0xaa, 0xd5, 0x43, 0x56,
		0xab, 0x57, 0x66, 0xa8, 0x64, 0xcc, 0x25, 0xe2, 0x9e, 0x5a, 0x12, 0x0c, 0x12, 0xf5, 0x2f, 0xcf,
		0x4b, 0x2d, 0x0a, 0x4a, 0x2d, 0x2c, 0x4d, 0x2d, 0x2e, 0x11, 0x92, 0xe6, 0xe2, 0x04, 0x2b, 0x8d,
		0xcf, 0x4e, 0xad, 0x94, 0x60, 0x54, 0x60, 0xd4, 0xe0, 0x0c, 0xe2, 0x00, 0x0b, 0x78, 0xa7, 0x56,
		0x2a, 0xe9, 0x72, 0x89, 0xa2, 0x69, 0x2a, 0x2e, 0xc8, 0xcf, 0x2b, 0x4e, 0x15, 0x12, 0xe1, 0x62,
		0xcd, 0x07, 0x09, 0x40, 0x75, 0x40, 0x38, 0x46, 0xbd, 0x8c, 0x5c, 0xfc, 0x60, 0xc5, 0xbe, 0x10,
		0x7b, 0x1d, 0x03, 0x3c, 0x85, 0xaa, 0xb8, 0x78, 0x51, 0x8c, 0x10, 0x32, 0xd2, 0xc3, 0xe7, 0x4e,
		0x3d, 0x6c, 0x8e, 0x94, 0x32, 0x26, 0x49, 0x0f, 0xc4, 0x8d, 0x1a, 0x8c, 0x4e, 0xce, 0x51, 0x8e,
		0xe9, 0x99, 0x25, 0x19, 0xa5, 0x49, 0x7a, 0xc9, 0xf9, 0xb9, 0xfa, 0x28, 0x41, 0xa9, 0x97, 0x9e,
		0x9a, 0xa7, 0x0f, 0x0e, 0x37, 0xf4, 0x50, 0xb5, 0x46, 0xe6, 0x97, 0x19, 0x26, 0xb1, 0x81, 0x55,
		0x19, 0x03, 0x02, 0x00, 0x00, 0xff, 0xff, 0x1b, 0x5e, 0xe1, 0x01, 0x8b, 0x01, 0x00, 0x00,
	},
}

func init() {
	yarpc.RegisterClientBuilder(
		func(clientConfig transport.ClientConfig, structField reflect.StructField) ShardManagerAPIYARPCClient {
			return NewShardManagerAPIYARPCClient(clientConfig, protobuf.ClientBuilderOptions(clientConfig, structField)...)
		},
	)
}
