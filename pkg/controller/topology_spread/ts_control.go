// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/baidu/jpaas-caas/baiducloud-cce-cni-driver/pkg/controller/subnet/types.go - description

// modification history
// --------------------
// 2022/07/29, by wangeweiwei22, create types

// DESCRIPTION

package topology_spread

import (
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

// SubnetClient Subnet control interface, through which operations
// on subnet objects can be initiated
type TSControl interface {
	Get(namespace, name string) (*networkingv1alpha1.PodSubnetTopologySpread, error)
}

var _ TSControl = &TopologySpreadController{}

func (tsc *TopologySpreadController) Get(namespace, name string) (*networkingv1alpha1.PodSubnetTopologySpread, error) {
	return tsc.pstsLister.PodSubnetTopologySpreads(namespace).Get(name)
}
