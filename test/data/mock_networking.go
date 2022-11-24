package data

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
)

// new available subnet
func MockSubnet(namespace, name, cidr string) *networkingv1alpha1.Subnet {
	return &networkingv1alpha1.Subnet{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1alpha1.SubnetSpec{
			ID:   name,
			Name: name,
			CIDR: cidr,
		},
		Status: networkingv1alpha1.SubnetStatus{
			AvailableIPNum: 256,
			Enable:         true,
			HasNoMoreIP:    false,
		},
	}
}

// mock a PodSubnetTopologySpread
func MockPodSubnetTopologySpread(namespace, name, subnet string, label labels.Set) *networkingv1alpha1.PodSubnetTopologySpread {
	return &networkingv1alpha1.PodSubnetTopologySpread{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1alpha1.PodSubnetTopologySpreadSpec{
			Name: name,
			Subnets: map[string]networkingv1alpha1.SubnetAllocation{
				subnet: {
					Type:            networkingv1alpha1.IPAllocTypeElastic,
					ReleaseStrategy: networkingv1alpha1.ReleaseStrategyTTL,
				},
			},
			EnablePodTopologySpread: true,
			Priority:                1,
			Selector:                metav1.SetAsLabelSelector(label),
		},
		Status: networkingv1alpha1.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]networkingv1alpha1.SubnetPodStatus{
				subnet: {},
			},
		},
	}
}

func MockFixedWorkloadEndpoint() *networkingv1alpha1.WorkloadEndpoint {
	return &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":              "sbn-test",
				"cce.io/instance-type":          "bcc",
				ipamgeneric.WepLabelStsOwnerKey: "busybox",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypeSts,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "True",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       string(networkingv1alpha1.ReleaseStrategyNever),
		},
	}
}

func MockIPPool() *networkingv1alpha1.IPPool {
	pool := &networkingv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "ippool-test",
		},
		Spec: networkingv1alpha1.IPPoolSpec{
			PodSubnets:     []string{"sbn-test"},
			CreationSource: ipamgeneric.IPPoolCreationSourceCNI,
		},
	}
	return pool
}
