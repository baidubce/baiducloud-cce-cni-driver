package vpceni

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
)

type hpasNetworkResourceSet struct {
	*bccNetworkResourceSet

	primaryENISubnetID   string
	haveCreatePrimaryENI bool
}

func newHPASNetworkResourceSet(super *bccNetworkResourceSet) *hpasNetworkResourceSet {
	node := &hpasNetworkResourceSet{
		bccNetworkResourceSet: super,
	}
	node.instanceType = string(metadata.InstanceTypeExHPAS)
	return node
}
