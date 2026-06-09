package vpceni

import "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"

// ebcNetworkResourceSet 保留 EBC 特有的实例类型标记，其余逻辑复用 bccNetworkResourceSet。
type ebcNetworkResourceSet struct {
	*bccNetworkResourceSet
}

func newEBCNetworkResourceSet(super *bccNetworkResourceSet) *ebcNetworkResourceSet {
	if super == nil {
		super = &bccNetworkResourceSet{
			bceNetworkResourceSet: &bceNetworkResourceSet{},
		}
	} else if super.bceNetworkResourceSet == nil {
		super.bceNetworkResourceSet = &bceNetworkResourceSet{}
	}
	node := &ebcNetworkResourceSet{
		bccNetworkResourceSet: super,
	}
	node.instanceType = string(metadata.InstanceTypeExEBC)
	return node
}
