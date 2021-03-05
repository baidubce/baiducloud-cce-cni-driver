// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud (interfaces: Interface)

// Package testing is a generated GoMock package.
package testing

import (
	context "context"
	bbc "github.com/baidubce/bce-sdk-go/services/bbc"
	api "github.com/baidubce/bce-sdk-go/services/bcc/api"
	eni "github.com/baidubce/bce-sdk-go/services/eni"
	vpc "github.com/baidubce/bce-sdk-go/services/vpc"
	gomock "github.com/golang/mock/gomock"
	reflect "reflect"
)

// MockInterface is a mock of Interface interface
type MockInterface struct {
	ctrl     *gomock.Controller
	recorder *MockInterfaceMockRecorder
}

// MockInterfaceMockRecorder is the mock recorder for MockInterface
type MockInterfaceMockRecorder struct {
	mock *MockInterface
}

// NewMockInterface creates a new mock instance
func NewMockInterface(ctrl *gomock.Controller) *MockInterface {
	mock := &MockInterface{ctrl: ctrl}
	mock.recorder = &MockInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockInterface) EXPECT() *MockInterfaceMockRecorder {
	return m.recorder
}

// AddPrivateIP mocks base method
func (m *MockInterface) AddPrivateIP(arg0 context.Context, arg1, arg2 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddPrivateIP", arg0, arg1, arg2)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AddPrivateIP indicates an expected call of AddPrivateIP
func (mr *MockInterfaceMockRecorder) AddPrivateIP(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddPrivateIP", reflect.TypeOf((*MockInterface)(nil).AddPrivateIP), arg0, arg1, arg2)
}

// AttachENI mocks base method
func (m *MockInterface) AttachENI(arg0 context.Context, arg1 *eni.EniInstance) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AttachENI", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// AttachENI indicates an expected call of AttachENI
func (mr *MockInterfaceMockRecorder) AttachENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AttachENI", reflect.TypeOf((*MockInterface)(nil).AttachENI), arg0, arg1)
}

// BBCBatchAddIP mocks base method
func (m *MockInterface) BBCBatchAddIP(arg0 context.Context, arg1 *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BBCBatchAddIP", arg0, arg1)
	ret0, _ := ret[0].(*bbc.BatchAddIpResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BBCBatchAddIP indicates an expected call of BBCBatchAddIP
func (mr *MockInterfaceMockRecorder) BBCBatchAddIP(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BBCBatchAddIP", reflect.TypeOf((*MockInterface)(nil).BBCBatchAddIP), arg0, arg1)
}

// BBCBatchDelIP mocks base method
func (m *MockInterface) BBCBatchDelIP(arg0 context.Context, arg1 *bbc.BatchDelIpArgs) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BBCBatchDelIP", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// BBCBatchDelIP indicates an expected call of BBCBatchDelIP
func (mr *MockInterfaceMockRecorder) BBCBatchDelIP(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BBCBatchDelIP", reflect.TypeOf((*MockInterface)(nil).BBCBatchDelIP), arg0, arg1)
}

// BBCGetInstanceDetail mocks base method
func (m *MockInterface) BBCGetInstanceDetail(arg0 context.Context, arg1 string) (*bbc.InstanceModel, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BBCGetInstanceDetail", arg0, arg1)
	ret0, _ := ret[0].(*bbc.InstanceModel)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BBCGetInstanceDetail indicates an expected call of BBCGetInstanceDetail
func (mr *MockInterfaceMockRecorder) BBCGetInstanceDetail(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BBCGetInstanceDetail", reflect.TypeOf((*MockInterface)(nil).BBCGetInstanceDetail), arg0, arg1)
}

// BBCGetInstanceENI mocks base method
func (m *MockInterface) BBCGetInstanceENI(arg0 context.Context, arg1 string) (*bbc.GetInstanceEniResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BBCGetInstanceENI", arg0, arg1)
	ret0, _ := ret[0].(*bbc.GetInstanceEniResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BBCGetInstanceENI indicates an expected call of BBCGetInstanceENI
func (mr *MockInterfaceMockRecorder) BBCGetInstanceENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BBCGetInstanceENI", reflect.TypeOf((*MockInterface)(nil).BBCGetInstanceENI), arg0, arg1)
}

// CreateENI mocks base method
func (m *MockInterface) CreateENI(arg0 context.Context, arg1 *eni.CreateEniArgs) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateENI", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateENI indicates an expected call of CreateENI
func (mr *MockInterfaceMockRecorder) CreateENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateENI", reflect.TypeOf((*MockInterface)(nil).CreateENI), arg0, arg1)
}

// CreateRouteRule mocks base method
func (m *MockInterface) CreateRouteRule(arg0 context.Context, arg1 *vpc.CreateRouteRuleArgs) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateRouteRule", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateRouteRule indicates an expected call of CreateRouteRule
func (mr *MockInterfaceMockRecorder) CreateRouteRule(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateRouteRule", reflect.TypeOf((*MockInterface)(nil).CreateRouteRule), arg0, arg1)
}

// DeleteENI mocks base method
func (m *MockInterface) DeleteENI(arg0 context.Context, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteENI", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteENI indicates an expected call of DeleteENI
func (mr *MockInterfaceMockRecorder) DeleteENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteENI", reflect.TypeOf((*MockInterface)(nil).DeleteENI), arg0, arg1)
}

// DeletePrivateIP mocks base method
func (m *MockInterface) DeletePrivateIP(arg0 context.Context, arg1, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeletePrivateIP", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeletePrivateIP indicates an expected call of DeletePrivateIP
func (mr *MockInterfaceMockRecorder) DeletePrivateIP(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePrivateIP", reflect.TypeOf((*MockInterface)(nil).DeletePrivateIP), arg0, arg1, arg2)
}

// DeleteRoute mocks base method
func (m *MockInterface) DeleteRoute(arg0 context.Context, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteRoute", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteRoute indicates an expected call of DeleteRoute
func (mr *MockInterfaceMockRecorder) DeleteRoute(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteRoute", reflect.TypeOf((*MockInterface)(nil).DeleteRoute), arg0, arg1)
}

// DescribeInstance mocks base method
func (m *MockInterface) DescribeInstance(arg0 context.Context, arg1 string) (*api.InstanceModel, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DescribeInstance", arg0, arg1)
	ret0, _ := ret[0].(*api.InstanceModel)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DescribeInstance indicates an expected call of DescribeInstance
func (mr *MockInterfaceMockRecorder) DescribeInstance(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DescribeInstance", reflect.TypeOf((*MockInterface)(nil).DescribeInstance), arg0, arg1)
}

// DescribeSubnet mocks base method
func (m *MockInterface) DescribeSubnet(arg0 context.Context, arg1 string) (*vpc.Subnet, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DescribeSubnet", arg0, arg1)
	ret0, _ := ret[0].(*vpc.Subnet)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DescribeSubnet indicates an expected call of DescribeSubnet
func (mr *MockInterfaceMockRecorder) DescribeSubnet(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DescribeSubnet", reflect.TypeOf((*MockInterface)(nil).DescribeSubnet), arg0, arg1)
}

// DetachENI mocks base method
func (m *MockInterface) DetachENI(arg0 context.Context, arg1 *eni.EniInstance) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DetachENI", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DetachENI indicates an expected call of DetachENI
func (mr *MockInterfaceMockRecorder) DetachENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DetachENI", reflect.TypeOf((*MockInterface)(nil).DetachENI), arg0, arg1)
}

// ListENIs mocks base method
func (m *MockInterface) ListENIs(arg0 context.Context, arg1 string) ([]eni.Eni, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListENIs", arg0, arg1)
	ret0, _ := ret[0].([]eni.Eni)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListENIs indicates an expected call of ListENIs
func (mr *MockInterfaceMockRecorder) ListENIs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListENIs", reflect.TypeOf((*MockInterface)(nil).ListENIs), arg0, arg1)
}

// ListRouteTable mocks base method
func (m *MockInterface) ListRouteTable(arg0 context.Context, arg1, arg2 string) ([]vpc.RouteRule, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListRouteTable", arg0, arg1, arg2)
	ret0, _ := ret[0].([]vpc.RouteRule)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListRouteTable indicates an expected call of ListRouteTable
func (mr *MockInterfaceMockRecorder) ListRouteTable(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListRouteTable", reflect.TypeOf((*MockInterface)(nil).ListRouteTable), arg0, arg1, arg2)
}

// ListSubnets mocks base method
func (m *MockInterface) ListSubnets(arg0 context.Context, arg1 *vpc.ListSubnetArgs) ([]vpc.Subnet, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListSubnets", arg0, arg1)
	ret0, _ := ret[0].([]vpc.Subnet)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListSubnets indicates an expected call of ListSubnets
func (mr *MockInterfaceMockRecorder) ListSubnets(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListSubnets", reflect.TypeOf((*MockInterface)(nil).ListSubnets), arg0, arg1)
}

// StatENI mocks base method
func (m *MockInterface) StatENI(arg0 context.Context, arg1 string) (*eni.Eni, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StatENI", arg0, arg1)
	ret0, _ := ret[0].(*eni.Eni)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// StatENI indicates an expected call of StatENI
func (mr *MockInterfaceMockRecorder) StatENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StatENI", reflect.TypeOf((*MockInterface)(nil).StatENI), arg0, arg1)
}
