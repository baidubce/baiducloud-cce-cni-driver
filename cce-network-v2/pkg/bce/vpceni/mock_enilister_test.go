package vpceni

import (
	"errors"

	"k8s.io/apimachinery/pkg/labels"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

type MockENILister struct {
	ListFunc func(selector labels.Selector) ([]*ccev2.ENI, error)
	GetFunc  func(name string) (*ccev2.ENI, error)
}

func (m *MockENILister) List(selector labels.Selector) ([]*ccev2.ENI, error) {
	if m.ListFunc != nil {
		return m.ListFunc(selector)
	}
	return []*ccev2.ENI{}, nil
}

func (m *MockENILister) Get(name string) (*ccev2.ENI, error) {
	if m.GetFunc != nil {
		return m.GetFunc(name)
	}
	return nil, errors.New("not found")
}
