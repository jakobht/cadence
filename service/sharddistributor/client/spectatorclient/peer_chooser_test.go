package spectatorclient

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/yarpc/api/peer"
	"go.uber.org/yarpc/api/transport"
	"go.uber.org/yarpc/yarpcerrors"

	"github.com/uber/cadence/common/log/testlogger"
)

func TestSpectatorPeerChooser_Choose_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTransport := newMockTransport(ctrl)
	mockSpectator := NewMockSpectator(ctrl)
	mockPeer := newMockPeer()

	chooser := &SpectatorPeerChooser{
		transport: mockTransport,
		logger:    testlogger.New(t),
		peers:     make(map[string]peer.Peer),
		spectators: &mockSpectators{
			spectators: map[string]Spectator{"test-namespace": mockSpectator},
		},
	}

	ctx := context.Background()
	req := &transport.Request{
		ShardKey: "shard-1",
		Headers:  transport.NewHeaders().With(NamespaceHeader, "test-namespace"),
	}

	mockSpectator.EXPECT().GetShardOwner(ctx, "shard-1").Return(&ExecutorOwnership{
		ExecutorID: "executor-1",
		Metadata:   map[string]string{"grpc_address": "127.0.0.1:7953"},
	}, nil)
	mockTransport.expectRetainPeer(mockPeer, nil)

	p, onFinish, err := chooser.Choose(ctx, req)

	assert.NoError(t, err)
	assert.Equal(t, mockPeer, p)
	assert.NotNil(t, onFinish)
	assert.Len(t, chooser.peers, 1)
}

func TestSpectatorPeerChooser_Choose_ReusesPeer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSpectator := NewMockSpectator(ctrl)
	mockPeer := newMockPeer()

	chooser := &SpectatorPeerChooser{
		logger: testlogger.New(t),
		peers:  map[string]peer.Peer{"127.0.0.1:7953": mockPeer},
		spectators: &mockSpectators{
			spectators: map[string]Spectator{"test-namespace": mockSpectator},
		},
	}

	req := &transport.Request{
		ShardKey: "shard-1",
		Headers:  transport.NewHeaders().With(NamespaceHeader, "test-namespace"),
	}

	mockSpectator.EXPECT().GetShardOwner(gomock.Any(), "shard-1").Return(&ExecutorOwnership{
		ExecutorID: "executor-1",
		Metadata:   map[string]string{"grpc_address": "127.0.0.1:7953"},
	}, nil)

	p, _, err := chooser.Choose(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, mockPeer, p)
	assert.Len(t, chooser.peers, 1)
}

func TestSpectatorPeerChooser_Choose_Errors(t *testing.T) {
	tests := []struct {
		name          string
		shardKey      string
		namespace     string
		setupMock     func(*gomock.Controller) Spectator
		expectedError string
		errorType     func(error) bool
	}{
		{
			name:          "missing shard key",
			shardKey:      "",
			namespace:     "test-ns",
			expectedError: "ShardKey to be non-empty",
			errorType:     yarpcerrors.IsInvalidArgument,
		},
		{
			name:          "missing namespace header",
			shardKey:      "shard-1",
			namespace:     "",
			expectedError: "x-shard-distributor-namespace",
			errorType:     yarpcerrors.IsInvalidArgument,
		},
		{
			name:      "spectator returns error",
			shardKey:  "shard-1",
			namespace: "test-ns",
			setupMock: func(ctrl *gomock.Controller) Spectator {
				m := NewMockSpectator(ctrl)
				m.EXPECT().GetShardOwner(gomock.Any(), "shard-1").
					Return(nil, errors.New("shard not assigned"))
				return m
			},
			expectedError: "failed to get shard owner",
			errorType:     yarpcerrors.IsUnavailable,
		},
		{
			name:      "missing grpc_address in metadata",
			shardKey:  "shard-1",
			namespace: "test-ns",
			setupMock: func(ctrl *gomock.Controller) Spectator {
				m := NewMockSpectator(ctrl)
				m.EXPECT().GetShardOwner(gomock.Any(), "shard-1").
					Return(&ExecutorOwnership{ExecutorID: "executor-1", Metadata: map[string]string{}}, nil)
				return m
			},
			expectedError: "no grpc_address in metadata",
			errorType:     yarpcerrors.IsInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var spectators Spectators
			if tt.setupMock != nil {
				spectators = &mockSpectators{
					spectators: map[string]Spectator{"test-ns": tt.setupMock(ctrl)},
				}
			} else {
				spectators = &mockSpectators{spectators: map[string]Spectator{}}
			}

			chooser := &SpectatorPeerChooser{
				logger:     testlogger.New(t),
				peers:      make(map[string]peer.Peer),
				spectators: spectators,
			}

			req := &transport.Request{
				ShardKey: tt.shardKey,
				Headers:  transport.NewHeaders().With(NamespaceHeader, tt.namespace),
			}

			p, onFinish, err := chooser.Choose(context.Background(), req)

			assert.Error(t, err)
			assert.Nil(t, p)
			assert.Nil(t, onFinish)
			assert.Contains(t, err.Error(), tt.expectedError)
			if tt.errorType != nil {
				assert.True(t, tt.errorType(err))
			}
		})
	}
}

func TestSpectatorPeerChooser_Stop_ReleasesPeers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTransport := newMockTransport(ctrl)
	mockPeer1, mockPeer2 := newMockPeer(), newMockPeer()

	chooser := &SpectatorPeerChooser{
		transport: mockTransport,
		logger:    testlogger.New(t),
		peers: map[string]peer.Peer{
			"127.0.0.1:7953": mockPeer1,
			"127.0.0.1:7954": mockPeer2,
		},
	}

	mockTransport.expectReleasePeer(mockPeer1, nil)
	mockTransport.expectReleasePeer(mockPeer2, nil)

	err := chooser.Stop()
	assert.NoError(t, err)
	assert.Empty(t, chooser.peers)
}

// Test helpers

type mockSpectators struct {
	spectators map[string]Spectator
}

func (m *mockSpectators) ForNamespace(namespace string) (Spectator, error) {
	s, ok := m.spectators[namespace]
	if !ok {
		return nil, errors.New("spectator not found")
	}
	return s, nil
}

type mockTransport struct {
	ctrl            *gomock.Controller
	retainPeerFunc  func(peer.Identifier, peer.Subscriber) (peer.Peer, error)
	releasePeerFunc func(peer.Peer, peer.Subscriber) error
}

func newMockTransport(ctrl *gomock.Controller) *mockTransport {
	return &mockTransport{ctrl: ctrl}
}

func (m *mockTransport) expectRetainPeer(p peer.Peer, err error) {
	m.retainPeerFunc = func(peer.Identifier, peer.Subscriber) (peer.Peer, error) {
		return p, err
	}
}

func (m *mockTransport) expectReleasePeer(p peer.Peer, err error) {
	old := m.releasePeerFunc
	m.releasePeerFunc = func(peer peer.Peer, sub peer.Subscriber) error {
		if peer == p {
			return err
		}
		if old != nil {
			return old(peer, sub)
		}
		return nil
	}
}

func (m *mockTransport) RetainPeer(pid peer.Identifier, sub peer.Subscriber) (peer.Peer, error) {
	if m.retainPeerFunc != nil {
		return m.retainPeerFunc(pid, sub)
	}
	return nil, errors.New("unexpected call to RetainPeer")
}

func (m *mockTransport) ReleasePeer(p peer.Peer, sub peer.Subscriber) error {
	if m.releasePeerFunc != nil {
		return m.releasePeerFunc(p, sub)
	}
	return errors.New("unexpected call to ReleasePeer")
}

type mockPeer struct{}

func newMockPeer() peer.Peer                   { return &mockPeer{} }
func (m *mockPeer) Identifier() string         { return "mock-peer" }
func (m *mockPeer) Status() peer.Status        { return peer.Status{} }
func (m *mockPeer) StartRequest()              {}
func (m *mockPeer) EndRequest()                {}
