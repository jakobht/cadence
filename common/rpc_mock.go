// Code generated by MockGen. DO NOT EDIT.
// Source: rpc.go

// Package common is a generated GoMock package.
package common

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	yarpc "go.uber.org/yarpc"
)

// MockRPCFactory is a mock of RPCFactory interface.
type MockRPCFactory struct {
	ctrl     *gomock.Controller
	recorder *MockRPCFactoryMockRecorder
}

// MockRPCFactoryMockRecorder is the mock recorder for MockRPCFactory.
type MockRPCFactoryMockRecorder struct {
	mock *MockRPCFactory
}

// NewMockRPCFactory creates a new mock instance.
func NewMockRPCFactory(ctrl *gomock.Controller) *MockRPCFactory {
	mock := &MockRPCFactory{ctrl: ctrl}
	mock.recorder = &MockRPCFactoryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRPCFactory) EXPECT() *MockRPCFactoryMockRecorder {
	return m.recorder
}

// GetDispatcher mocks base method.
func (m *MockRPCFactory) GetDispatcher() *yarpc.Dispatcher {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetDispatcher")
	ret0, _ := ret[0].(*yarpc.Dispatcher)
	return ret0
}

// GetDispatcher indicates an expected call of GetDispatcher.
func (mr *MockRPCFactoryMockRecorder) GetDispatcher() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetDispatcher", reflect.TypeOf((*MockRPCFactory)(nil).GetDispatcher))
}

// GetMaxMessageSize mocks base method.
func (m *MockRPCFactory) GetMaxMessageSize() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMaxMessageSize")
	ret0, _ := ret[0].(int)
	return ret0
}

// GetMaxMessageSize indicates an expected call of GetMaxMessageSize.
func (mr *MockRPCFactoryMockRecorder) GetMaxMessageSize() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMaxMessageSize", reflect.TypeOf((*MockRPCFactory)(nil).GetMaxMessageSize))
}
