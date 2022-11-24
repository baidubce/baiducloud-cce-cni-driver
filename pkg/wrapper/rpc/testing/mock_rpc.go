// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc (interfaces: Interface)

// Package testing is a generated GoMock package.
package testing

import (
	reflect "reflect"

	rpc "github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	gomock "github.com/golang/mock/gomock"
	grpc "google.golang.org/grpc"
)

// MockInterface is a mock of Interface interface.
type MockInterface struct {
	ctrl     *gomock.Controller
	recorder *MockInterfaceMockRecorder
}

// MockInterfaceMockRecorder is the mock recorder for MockInterface.
type MockInterfaceMockRecorder struct {
	mock *MockInterface
}

// NewMockInterface creates a new mock instance.
func NewMockInterface(ctrl *gomock.Controller) *MockInterface {
	mock := &MockInterface{ctrl: ctrl}
	mock.recorder = &MockInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockInterface) EXPECT() *MockInterfaceMockRecorder {
	return m.recorder
}

// NewCNIBackendClient mocks base method.
func (m *MockInterface) NewCNIBackendClient(arg0 *grpc.ClientConn) rpc.CNIBackendClient {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewCNIBackendClient", arg0)
	ret0, _ := ret[0].(rpc.CNIBackendClient)
	return ret0
}

// NewCNIBackendClient indicates an expected call of NewCNIBackendClient.
func (mr *MockInterfaceMockRecorder) NewCNIBackendClient(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewCNIBackendClient", reflect.TypeOf((*MockInterface)(nil).NewCNIBackendClient), arg0)
}
