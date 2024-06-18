package ccemock

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/addressing"
)

func NewMockSimpleNrs(name, instanceType string) *ccev2.NetResourceSet {
	var eniUseMode string
	if instanceType == "BBC" {
		eniUseMode = string(ccev2.ENIUseModeSecondaryIP)
	} else {
		eniUseMode = string(ccev2.ENIUseModeSecondaryIP)
	}
	return NewMockNrs(name, instanceType, eniUseMode, []string{"sbn-abc", "sbn-def"})
}

func NewMockNrs(name, instanceType, useMode string, subnetIDs []string) *ccev2.NetResourceSet {
	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"cluster-id":             "cce-test",
				"cluster-role":           "node",
				"kubernetes.io/arch":     "amd64",
				"kubernetes.io/os":       "linux",
				"kubernetes.io/hostname": name,
				"kubernetes.io/tor":      "sw-BTWy0mQsCnnB2lgEPR3MJg",

				"beta.kubernetes.io/instance-gpu":          "false",
				"cce.baidubce.com/baidu-cgpu-priority":     "disable",
				"cce.baidubce.com/gpu-share-device-plugin": "false",

				"failure-domain.beta.kubernetes.io/region": "bj",
				"failure-domain.beta.kubernetes.io/zone":   "zoneD",
				"topology.kubernetes.io/region":            "bj",
				"topology.kubernetes.io/zone":              "zoneD",
				"node.kubernetes.io/instance-type":         instanceType,
			},
			Annotations: map[string]string{
				k8s.AnnotationNodeAnnotationSynced: "false",
			},
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: AppendHashString("i"),
			ENI: &api.ENISpec{
				SubnetIDs:                 subnetIDs,
				AvailabilityZone:          "zoneD",
				InstanceType:              instanceType,
				UseMode:                   string(ccev2.ENIUseModeSecondaryIP),
				VpcID:                     "vpc-test",
				SecurityGroups:            []string{"sg-test"},
				RouteTableOffset:          127,
				InstallSourceBasedRouting: true,
			},
			IPAM: types.IPAMSpec{
				Pool:              make(types.AllocationMap),
				MinAllocate:       2,
				PreAllocate:       2,
				MaxAboveWatermark: 10,
			},
			Addresses: []ccev2.NodeAddress{
				{
					Type: addressing.NodeInternalIP,
					IP:   name,
				},
			},
		},
		Status: ccev2.NetResourceStatus{
			ENIs: make(map[string]ccev2.SimpleENIStatus),
			IPAM: types.IPAMStatus{
				Used:            make(types.AllocationMap),
				CrossSubnetUsed: make(types.AllocationMap),
			},
		},
	}
	return nrs
}

func EnsureNrsToInformer(t *testing.T, nrss []*ccev2.NetResourceSet) error {
	createFunc := func(ctx context.Context) []metav1.Object {
		var toWaitObj []metav1.Object

		lister := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister()
		for _, nrs := range nrss {
			_, err := lister.Get(nrs.Name)
			if err == nil {
				continue
			}

			result, err := k8s.CCEClient().CceV2().NetResourceSets().Create(ctx, nrs, metav1.CreateOptions{})
			if err == nil {
				toWaitObj = append(toWaitObj, result)
			}
		}
		return toWaitObj
	}
	return EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Informer(), createFunc)
}
