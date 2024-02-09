// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/typed/networking/v1alpha1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeCceV1alpha1 struct {
	*testing.Fake
}

func (c *FakeCceV1alpha1) CrossVPCEnis() v1alpha1.CrossVPCEniInterface {
	return &FakeCrossVPCEnis{c}
}

func (c *FakeCceV1alpha1) IPPools(namespace string) v1alpha1.IPPoolInterface {
	return &FakeIPPools{c, namespace}
}

func (c *FakeCceV1alpha1) PodSubnetTopologySpreads(namespace string) v1alpha1.PodSubnetTopologySpreadInterface {
	return &FakePodSubnetTopologySpreads{c, namespace}
}

func (c *FakeCceV1alpha1) PodSubnetTopologySpreadTables(namespace string) v1alpha1.PodSubnetTopologySpreadTableInterface {
	return &FakePodSubnetTopologySpreadTables{c, namespace}
}

func (c *FakeCceV1alpha1) Subnets(namespace string) v1alpha1.SubnetInterface {
	return &FakeSubnets{c, namespace}
}

func (c *FakeCceV1alpha1) WorkloadEndpoints(namespace string) v1alpha1.WorkloadEndpointInterface {
	return &FakeWorkloadEndpoints{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeCceV1alpha1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
