// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud (interfaces: Interface)

// Package testing is a generated GoMock package.
package testing

import (
	context "context"
	reflect "reflect"

	hpc "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	bbc "github.com/baidubce/bce-sdk-go/services/bbc"
	api "github.com/baidubce/bce-sdk-go/services/bcc/api"
	eni "github.com/baidubce/bce-sdk-go/services/eni"
	vpc "github.com/baidubce/bce-sdk-go/services/vpc"
	gomock "github.com/golang/mock/gomock"
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

// AddPrivateIP mocks base method.
func (m *MockInterface) AddPrivateIP(arg0 context.Context, arg1, arg2 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddPrivateIP", arg0, arg1, arg2)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AddPrivateIP indicates an expected call of AddPrivateIP.
func (mr *MockInterfaceMockRecorder) AddPrivateIP(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddPrivateIP", reflect.TypeOf((*MockInterface)(nil).AddPrivateIP), arg0, arg1, arg2)
}

// AttachENI mocks base method.
func (m *MockInterface) AttachENI(arg0 context.Context, arg1 *eni.EniInstance) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AttachENI", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// AttachENI indicates an expected call of AttachENI.
func (mr *MockInterfaceMockRecorder) AttachENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AttachENI", reflect.TypeOf((*MockInterface)(nil).AttachENI), arg0, arg1)
}

// BBCBatchAddIP mocks base method.
func (m *MockInterface) BBCBatchAddIP(arg0 context.Context, arg1 *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BBCBatchAddIP", arg0, arg1)
	ret0, _ := ret[0].(*bbc.BatchAddIpResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BBCBatchAddIP indicates an expected call of BBCBatchAddIP.
func (mr *MockInterfaceMockRecorder) BBCBatchAddIP(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BBCBatchAddIP", reflect.TypeOf((*MockInterface)(nil).BBCBatchAddIP), arg0, arg1)
}

// BBCBatchAddIPCrossSubnet mocks base method.
func (m *MockInterface) BBCBatchAddIPCrossSubnet(arg0 context.Context, arg1 *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BBCBatchAddIPCrossSubnet", arg0, arg1)
	ret0, _ := ret[0].(*bbc.BatchAddIpResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BBCBatchAddIPCrossSubnet indicates an expected call of BBCBatchAddIPCrossSubnet.
func (mr *MockInterfaceMockRecorder) BBCBatchAddIPCrossSubnet(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BBCBatchAddIPCrossSubnet", reflect.TypeOf((*MockInterface)(nil).BBCBatchAddIPCrossSubnet), arg0, arg1)
}

// BBCBatchDelIP mocks base method.
func (m *MockInterface) BBCBatchDelIP(arg0 context.Context, arg1 *bbc.BatchDelIpArgs) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BBCBatchDelIP", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// BBCBatchDelIP indicates an expected call of BBCBatchDelIP.
func (mr *MockInterfaceMockRecorder) BBCBatchDelIP(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BBCBatchDelIP", reflect.TypeOf((*MockInterface)(nil).BBCBatchDelIP), arg0, arg1)
}

// BatchAddHpcEniPrivateIP mocks base method.
func (m *MockInterface) BatchAddHpcEniPrivateIP(arg0 context.Context, arg1 *hpc.EniBatchPrivateIPArgs) (*hpc.BatchAddPrivateIPResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BatchAddHpcEniPrivateIP", arg0, arg1)
	ret0, _ := ret[0].(*hpc.BatchAddPrivateIPResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BatchAddHpcEniPrivateIP indicates an expected call of BatchAddHpcEniPrivateIP.
func (mr *MockInterfaceMockRecorder) BatchAddHpcEniPrivateIP(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BatchAddHpcEniPrivateIP", reflect.TypeOf((*MockInterface)(nil).BatchAddHpcEniPrivateIP), arg0, arg1)
}

// BatchAddPrivateIP mocks base method.
func (m *MockInterface) BatchAddPrivateIP(arg0 context.Context, arg1 []string, arg2 int, arg3 string) ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BatchAddPrivateIP", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BatchAddPrivateIP indicates an expected call of BatchAddPrivateIP.
func (mr *MockInterfaceMockRecorder) BatchAddPrivateIP(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BatchAddPrivateIP", reflect.TypeOf((*MockInterface)(nil).BatchAddPrivateIP), arg0, arg1, arg2, arg3)
}

// BatchAddPrivateIpCrossSubnet mocks base method.
func (m *MockInterface) BatchAddPrivateIpCrossSubnet(arg0 context.Context, arg1, arg2 string, arg3 []string, arg4 int) ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BatchAddPrivateIpCrossSubnet", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BatchAddPrivateIpCrossSubnet indicates an expected call of BatchAddPrivateIpCrossSubnet.
func (mr *MockInterfaceMockRecorder) BatchAddPrivateIpCrossSubnet(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BatchAddPrivateIpCrossSubnet", reflect.TypeOf((*MockInterface)(nil).BatchAddPrivateIpCrossSubnet), arg0, arg1, arg2, arg3, arg4)
}

// BatchDeleteHpcEniPrivateIP mocks base method.
func (m *MockInterface) BatchDeleteHpcEniPrivateIP(arg0 context.Context, arg1 *hpc.EniBatchDeleteIPArgs) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BatchDeleteHpcEniPrivateIP", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// BatchDeleteHpcEniPrivateIP indicates an expected call of BatchDeleteHpcEniPrivateIP.
func (mr *MockInterfaceMockRecorder) BatchDeleteHpcEniPrivateIP(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BatchDeleteHpcEniPrivateIP", reflect.TypeOf((*MockInterface)(nil).BatchDeleteHpcEniPrivateIP), arg0, arg1)
}

// BatchDeletePrivateIP mocks base method.
func (m *MockInterface) BatchDeletePrivateIP(arg0 context.Context, arg1 []string, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BatchDeletePrivateIP", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// BatchDeletePrivateIP indicates an expected call of BatchDeletePrivateIP.
func (mr *MockInterfaceMockRecorder) BatchDeletePrivateIP(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BatchDeletePrivateIP", reflect.TypeOf((*MockInterface)(nil).BatchDeletePrivateIP), arg0, arg1, arg2)
}

// CreateENI mocks base method.
func (m *MockInterface) CreateENI(arg0 context.Context, arg1 *eni.CreateEniArgs) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateENI", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateENI indicates an expected call of CreateENI.
func (mr *MockInterfaceMockRecorder) CreateENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateENI", reflect.TypeOf((*MockInterface)(nil).CreateENI), arg0, arg1)
}

// CreateRouteRule mocks base method.
func (m *MockInterface) CreateRouteRule(arg0 context.Context, arg1 *vpc.CreateRouteRuleArgs) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateRouteRule", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateRouteRule indicates an expected call of CreateRouteRule.
func (mr *MockInterfaceMockRecorder) CreateRouteRule(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateRouteRule", reflect.TypeOf((*MockInterface)(nil).CreateRouteRule), arg0, arg1)
}

// DeleteENI mocks base method.
func (m *MockInterface) DeleteENI(arg0 context.Context, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteENI", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteENI indicates an expected call of DeleteENI.
func (mr *MockInterfaceMockRecorder) DeleteENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteENI", reflect.TypeOf((*MockInterface)(nil).DeleteENI), arg0, arg1)
}

// DeletePrivateIP mocks base method.
func (m *MockInterface) DeletePrivateIP(arg0 context.Context, arg1, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeletePrivateIP", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeletePrivateIP indicates an expected call of DeletePrivateIP.
func (mr *MockInterfaceMockRecorder) DeletePrivateIP(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePrivateIP", reflect.TypeOf((*MockInterface)(nil).DeletePrivateIP), arg0, arg1, arg2)
}

// DeleteRouteRule mocks base method.
func (m *MockInterface) DeleteRouteRule(arg0 context.Context, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteRouteRule", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteRouteRule indicates an expected call of DeleteRouteRule.
func (mr *MockInterfaceMockRecorder) DeleteRouteRule(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteRouteRule", reflect.TypeOf((*MockInterface)(nil).DeleteRouteRule), arg0, arg1)
}

// DescribeSubnet mocks base method.
func (m *MockInterface) DescribeSubnet(arg0 context.Context, arg1 string) (*vpc.Subnet, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DescribeSubnet", arg0, arg1)
	ret0, _ := ret[0].(*vpc.Subnet)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DescribeSubnet indicates an expected call of DescribeSubnet.
func (mr *MockInterfaceMockRecorder) DescribeSubnet(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DescribeSubnet", reflect.TypeOf((*MockInterface)(nil).DescribeSubnet), arg0, arg1)
}

// DetachENI mocks base method.
func (m *MockInterface) DetachENI(arg0 context.Context, arg1 *eni.EniInstance) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DetachENI", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DetachENI indicates an expected call of DetachENI.
func (mr *MockInterfaceMockRecorder) DetachENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DetachENI", reflect.TypeOf((*MockInterface)(nil).DetachENI), arg0, arg1)
}

// GetBBCInstanceDetail mocks base method.
func (m *MockInterface) GetBBCInstanceDetail(arg0 context.Context, arg1 string) (*bbc.InstanceModel, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBBCInstanceDetail", arg0, arg1)
	ret0, _ := ret[0].(*bbc.InstanceModel)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBBCInstanceDetail indicates an expected call of GetBBCInstanceDetail.
func (mr *MockInterfaceMockRecorder) GetBBCInstanceDetail(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBBCInstanceDetail", reflect.TypeOf((*MockInterface)(nil).GetBBCInstanceDetail), arg0, arg1)
}

// GetBBCInstanceENI mocks base method.
func (m *MockInterface) GetBBCInstanceENI(arg0 context.Context, arg1 string) (*bbc.GetInstanceEniResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBBCInstanceENI", arg0, arg1)
	ret0, _ := ret[0].(*bbc.GetInstanceEniResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBBCInstanceENI indicates an expected call of GetBBCInstanceENI.
func (mr *MockInterfaceMockRecorder) GetBBCInstanceENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBBCInstanceENI", reflect.TypeOf((*MockInterface)(nil).GetBBCInstanceENI), arg0, arg1)
}

// GetBCCInstanceDetail mocks base method.
func (m *MockInterface) GetBCCInstanceDetail(arg0 context.Context, arg1 string) (*api.InstanceModel, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBCCInstanceDetail", arg0, arg1)
	ret0, _ := ret[0].(*api.InstanceModel)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBCCInstanceDetail indicates an expected call of GetBCCInstanceDetail.
func (mr *MockInterfaceMockRecorder) GetBCCInstanceDetail(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBCCInstanceDetail", reflect.TypeOf((*MockInterface)(nil).GetBCCInstanceDetail), arg0, arg1)
}

// GetHPCEniID mocks base method.
func (m *MockInterface) GetHPCEniID(arg0 context.Context, arg1 string) (*hpc.EniList, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetHPCEniID", arg0, arg1)
	ret0, _ := ret[0].(*hpc.EniList)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetHPCEniID indicates an expected call of GetHPCEniID.
func (mr *MockInterfaceMockRecorder) GetHPCEniID(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetHPCEniID", reflect.TypeOf((*MockInterface)(nil).GetHPCEniID), arg0, arg1)
}

// ListENIs mocks base method.
func (m *MockInterface) ListENIs(arg0 context.Context, arg1 eni.ListEniArgs) ([]eni.Eni, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListENIs", arg0, arg1)
	ret0, _ := ret[0].([]eni.Eni)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListENIs indicates an expected call of ListENIs.
func (mr *MockInterfaceMockRecorder) ListENIs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListENIs", reflect.TypeOf((*MockInterface)(nil).ListENIs), arg0, arg1)
}

// ListRouteTable mocks base method.
func (m *MockInterface) ListRouteTable(arg0 context.Context, arg1, arg2 string) ([]vpc.RouteRule, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListRouteTable", arg0, arg1, arg2)
	ret0, _ := ret[0].([]vpc.RouteRule)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListRouteTable indicates an expected call of ListRouteTable.
func (mr *MockInterfaceMockRecorder) ListRouteTable(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListRouteTable", reflect.TypeOf((*MockInterface)(nil).ListRouteTable), arg0, arg1, arg2)
}

// ListSecurityGroup mocks base method.
func (m *MockInterface) ListSecurityGroup(arg0 context.Context, arg1, arg2 string) ([]api.SecurityGroupModel, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListSecurityGroup", arg0, arg1, arg2)
	ret0, _ := ret[0].([]api.SecurityGroupModel)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListSecurityGroup indicates an expected call of ListSecurityGroup.
func (mr *MockInterfaceMockRecorder) ListSecurityGroup(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListSecurityGroup", reflect.TypeOf((*MockInterface)(nil).ListSecurityGroup), arg0, arg1, arg2)
}

// ListSubnets mocks base method.
func (m *MockInterface) ListSubnets(arg0 context.Context, arg1 *vpc.ListSubnetArgs) ([]vpc.Subnet, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListSubnets", arg0, arg1)
	ret0, _ := ret[0].([]vpc.Subnet)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListSubnets indicates an expected call of ListSubnets.
func (mr *MockInterfaceMockRecorder) ListSubnets(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListSubnets", reflect.TypeOf((*MockInterface)(nil).ListSubnets), arg0, arg1)
}

// StatENI mocks base method.
func (m *MockInterface) StatENI(arg0 context.Context, arg1 string) (*eni.Eni, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StatENI", arg0, arg1)
	ret0, _ := ret[0].(*eni.Eni)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// StatENI indicates an expected call of StatENI.
func (mr *MockInterfaceMockRecorder) StatENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StatENI", reflect.TypeOf((*MockInterface)(nil).StatENI), arg0, arg1)
}
