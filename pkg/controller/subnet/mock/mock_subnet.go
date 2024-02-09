// Code generated by MockGen. DO NOT EDIT.
// Source: types.go

// Package mock is a generated GoMock package.
package mock

import (
	context "context"
	v1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	subnet "github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet"
	gomock "github.com/golang/mock/gomock"
	reflect "reflect"
)

// MockSubnetControl is a mock of SubnetControl interface
type MockSubnetControl struct {
	ctrl     *gomock.Controller
	recorder *MockSubnetControlMockRecorder
}

// MockSubnetControlMockRecorder is the mock recorder for MockSubnetControl
type MockSubnetControlMockRecorder struct {
	mock *MockSubnetControl
}

// NewMockSubnetControl creates a new mock instance
func NewMockSubnetControl(ctrl *gomock.Controller) *MockSubnetControl {
	mock := &MockSubnetControl{ctrl: ctrl}
	mock.recorder = &MockSubnetControlMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockSubnetControl) EXPECT() *MockSubnetControlMockRecorder {
	return m.recorder
}

// Get mocks base method
func (m *MockSubnetControl) Get(name string) (*v1alpha1.Subnet, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", name)
	ret0, _ := ret[0].(*v1alpha1.Subnet)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get
func (mr *MockSubnetControlMockRecorder) Get(name interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockSubnetControl)(nil).Get), name)
}

// Create mocks base method
func (m *MockSubnetControl) Create(ctx context.Context, name string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Create", ctx, name)
	ret0, _ := ret[0].(error)
	return ret0
}

// Create indicates an expected call of Create
func (mr *MockSubnetControlMockRecorder) Create(ctx, name interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Create", reflect.TypeOf((*MockSubnetControl)(nil).Create), ctx, name)
}

// DeclareSubnetHasNoMoreIP mocks base method
func (m *MockSubnetControl) DeclareSubnetHasNoMoreIP(ctx context.Context, subnetID string, hasNoMoreIP bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeclareSubnetHasNoMoreIP", ctx, subnetID, hasNoMoreIP)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeclareSubnetHasNoMoreIP indicates an expected call of DeclareSubnetHasNoMoreIP
func (mr *MockSubnetControlMockRecorder) DeclareSubnetHasNoMoreIP(ctx, subnetID, hasNoMoreIP interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeclareSubnetHasNoMoreIP", reflect.TypeOf((*MockSubnetControl)(nil).DeclareSubnetHasNoMoreIP), ctx, subnetID, hasNoMoreIP)
}

// MockSubnetClientInject is a mock of SubnetClientInject interface
type MockSubnetClientInject struct {
	ctrl     *gomock.Controller
	recorder *MockSubnetClientInjectMockRecorder
}

// MockSubnetClientInjectMockRecorder is the mock recorder for MockSubnetClientInject
type MockSubnetClientInjectMockRecorder struct {
	mock *MockSubnetClientInject
}

// NewMockSubnetClientInject creates a new mock instance
func NewMockSubnetClientInject(ctrl *gomock.Controller) *MockSubnetClientInject {
	mock := &MockSubnetClientInject{ctrl: ctrl}
	mock.recorder = &MockSubnetClientInjectMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockSubnetClientInject) EXPECT() *MockSubnetClientInjectMockRecorder {
	return m.recorder
}

// InjectSubnetClient mocks base method
func (m *MockSubnetClientInject) InjectSubnetClient(sbnClient subnet.SubnetControl) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "InjectSubnetClient", sbnClient)
	ret0, _ := ret[0].(error)
	return ret0
}

// InjectSubnetClient indicates an expected call of InjectSubnetClient
func (mr *MockSubnetClientInjectMockRecorder) InjectSubnetClient(sbnClient interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InjectSubnetClient", reflect.TypeOf((*MockSubnetClientInject)(nil).InjectSubnetClient), sbnClient)
}
