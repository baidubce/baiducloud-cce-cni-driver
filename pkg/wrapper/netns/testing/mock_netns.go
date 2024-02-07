// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netns (interfaces: NetNS)

// Package testing is a generated GoMock package.
package testing

import (
	reflect "reflect"

	ns "github.com/containernetworking/plugins/pkg/ns"
	gomock "github.com/golang/mock/gomock"
)

// MockNetNS is a mock of NetNS interface.
type MockNetNS struct {
	ctrl     *gomock.Controller
	recorder *MockNetNSMockRecorder
}

// MockNetNSMockRecorder is the mock recorder for MockNetNS.
type MockNetNSMockRecorder struct {
	mock *MockNetNS
}

// NewMockNetNS creates a new mock instance.
func NewMockNetNS(ctrl *gomock.Controller) *MockNetNS {
	mock := &MockNetNS{ctrl: ctrl}
	mock.recorder = &MockNetNSMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockNetNS) EXPECT() *MockNetNSMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockNetNS) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockNetNSMockRecorder) Close() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockNetNS)(nil).Close))
}

// Do mocks base method.
func (m *MockNetNS) Do(arg0 func(ns.NetNS) error) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Do", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Do indicates an expected call of Do.
func (mr *MockNetNSMockRecorder) Do(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Do", reflect.TypeOf((*MockNetNS)(nil).Do), arg0)
}

// Fd mocks base method.
func (m *MockNetNS) Fd() uintptr {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Fd")
	ret0, _ := ret[0].(uintptr)
	return ret0
}

// Fd indicates an expected call of Fd.
func (mr *MockNetNSMockRecorder) Fd() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Fd", reflect.TypeOf((*MockNetNS)(nil).Fd))
}

// Path mocks base method.
func (m *MockNetNS) Path() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Path")
	ret0, _ := ret[0].(string)
	return ret0
}

// Path indicates an expected call of Path.
func (mr *MockNetNSMockRecorder) Path() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Path", reflect.TypeOf((*MockNetNS)(nil).Path))
}

// Set mocks base method.
func (m *MockNetNS) Set() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Set")
	ret0, _ := ret[0].(error)
	return ret0
}

// Set indicates an expected call of Set.
func (mr *MockNetNSMockRecorder) Set() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Set", reflect.TypeOf((*MockNetNS)(nil).Set))
}
