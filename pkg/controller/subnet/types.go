// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/baidu/jpaas-caas/baiducloud-cce-cni-driver/pkg/controller/subnet/types.go - description

// modification history
// --------------------
// 2022/07/29, by wangeweiwei22, create types

// DESCRIPTION

package subnet

import (
	"context"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

// SubnetClient Subnet control interface, through which operations
// on subnet objects can be initiated
type SubnetControl interface {
	Get(name string) (*v1alpha1.Subnet, error)
	Create(ctx context.Context, name string) error
	DeclareSubnetHasNoMoreIP(ctx context.Context, subnetID string, hasNoMoreIP bool) error
}

var _ SubnetControl = &SubnetController{}

// SubnetClientInject sbnClient to read and write subnet
type SubnetClientInject interface {
	InjectSubnetClient(sbnClient SubnetControl) error
}

func subnetClientInto(sbnClient SubnetControl, i interface{}) (bool, error) {
	if is, ok := i.(SubnetClientInject); ok {
		return true, is.InjectSubnetClient(sbnClient)
	}
	return false, nil
}
