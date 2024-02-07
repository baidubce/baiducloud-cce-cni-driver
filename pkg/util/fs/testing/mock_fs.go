// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs (interfaces: FileSystem)

// Package testing is a generated GoMock package.
package testing

import (
	fs "io/fs"
	os "os"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockFileSystem is a mock of FileSystem interface.
type MockFileSystem struct {
	ctrl     *gomock.Controller
	recorder *MockFileSystemMockRecorder
}

// MockFileSystemMockRecorder is the mock recorder for MockFileSystem.
type MockFileSystemMockRecorder struct {
	mock *MockFileSystem
}

// NewMockFileSystem creates a new mock instance.
func NewMockFileSystem(ctrl *gomock.Controller) *MockFileSystem {
	mock := &MockFileSystem{ctrl: ctrl}
	mock.recorder = &MockFileSystemMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockFileSystem) EXPECT() *MockFileSystemMockRecorder {
	return m.recorder
}

// MD5Sum mocks base method.
func (m *MockFileSystem) MD5Sum(arg0 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MD5Sum", arg0)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// MD5Sum indicates an expected call of MD5Sum.
func (mr *MockFileSystemMockRecorder) MD5Sum(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MD5Sum", reflect.TypeOf((*MockFileSystem)(nil).MD5Sum), arg0)
}

// Open mocks base method.
func (m *MockFileSystem) Open(arg0 string) (*os.File, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Open", arg0)
	ret0, _ := ret[0].(*os.File)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Open indicates an expected call of Open.
func (mr *MockFileSystemMockRecorder) Open(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Open", reflect.TypeOf((*MockFileSystem)(nil).Open), arg0)
}

// ReadFile mocks base method.
func (m *MockFileSystem) ReadFile(arg0 string) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReadFile", arg0)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ReadFile indicates an expected call of ReadFile.
func (mr *MockFileSystemMockRecorder) ReadFile(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReadFile", reflect.TypeOf((*MockFileSystem)(nil).ReadFile), arg0)
}

// Remove mocks base method.
func (m *MockFileSystem) Remove(arg0 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Remove", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Remove indicates an expected call of Remove.
func (mr *MockFileSystemMockRecorder) Remove(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Remove", reflect.TypeOf((*MockFileSystem)(nil).Remove), arg0)
}

// Rename mocks base method.
func (m *MockFileSystem) Rename(arg0, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Rename", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Rename indicates an expected call of Rename.
func (mr *MockFileSystemMockRecorder) Rename(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Rename", reflect.TypeOf((*MockFileSystem)(nil).Rename), arg0, arg1)
}

// Stat mocks base method.
func (m *MockFileSystem) Stat(arg0 string) (fs.FileInfo, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Stat", arg0)
	ret0, _ := ret[0].(fs.FileInfo)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Stat indicates an expected call of Stat.
func (mr *MockFileSystemMockRecorder) Stat(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Stat", reflect.TypeOf((*MockFileSystem)(nil).Stat), arg0)
}

// WriteFile mocks base method.
func (m *MockFileSystem) WriteFile(arg0 string, arg1 []byte, arg2 fs.FileMode) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WriteFile", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// WriteFile indicates an expected call of WriteFile.
func (mr *MockFileSystemMockRecorder) WriteFile(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WriteFile", reflect.TypeOf((*MockFileSystem)(nil).WriteFile), arg0, arg1, arg2)
}
