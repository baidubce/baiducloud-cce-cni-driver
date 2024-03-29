//go:build !privileged_tests

/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package types

import (
	"net"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/addressing"
	. "gopkg.in/check.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	TestingT(t)
}

type NodeSuite struct{}

var _ = Suite(&NodeSuite{})

func (s *NodeSuite) TestGetNodeIP(c *C) {
	n := Node{
		Name: "node-1",
		IPAddresses: []Address{
			{IP: net.ParseIP("192.0.2.3"), Type: addressing.NodeExternalIP},
		},
	}
	ip := n.GetNodeIP(false)
	// Return the only IP present
	c.Assert(ip.Equal(net.ParseIP("192.0.2.3")), Equals, true)

	n.IPAddresses = append(n.IPAddresses, Address{IP: net.ParseIP("192.0.2.3"), Type: addressing.NodeExternalIP})
	ip = n.GetNodeIP(false)
	// The next priority should be NodeExternalIP
	c.Assert(ip.Equal(net.ParseIP("192.0.2.3")), Equals, true)

	n.IPAddresses = append(n.IPAddresses, Address{IP: net.ParseIP("198.51.100.2"), Type: addressing.NodeInternalIP})
	ip = n.GetNodeIP(false)
	// The next priority should be NodeInternalIP
	c.Assert(ip.Equal(net.ParseIP("198.51.100.2")), Equals, true)

	n.IPAddresses = append(n.IPAddresses, Address{IP: net.ParseIP("2001:DB8::1"), Type: addressing.NodeExternalIP})
	ip = n.GetNodeIP(true)
	// The next priority should be NodeExternalIP and IPv6
	c.Assert(ip.Equal(net.ParseIP("2001:DB8::1")), Equals, true)

	n.IPAddresses = append(n.IPAddresses, Address{IP: net.ParseIP("2001:DB8::2"), Type: addressing.NodeInternalIP})
	ip = n.GetNodeIP(true)
	// The next priority should be NodeInternalIP and IPv6
	c.Assert(ip.Equal(net.ParseIP("2001:DB8::2")), Equals, true)

	n.IPAddresses = append(n.IPAddresses, Address{IP: net.ParseIP("198.51.100.2"), Type: addressing.NodeInternalIP})
	ip = n.GetNodeIP(false)
	// Should still return NodeInternalIP and IPv4
	c.Assert(ip.Equal(net.ParseIP("198.51.100.2")), Equals, true)

}

func (s *NodeSuite) TestGetIPByType(c *C) {
	n := Node{
		Name: "node-1",
		IPAddresses: []Address{
			{IP: net.ParseIP("192.0.2.3"), Type: addressing.NodeExternalIP},
		},
	}

	ip := n.GetIPByType(addressing.NodeInternalIP, false)
	c.Assert(ip, IsNil)
	ip = n.GetIPByType(addressing.NodeInternalIP, true)
	c.Assert(ip, IsNil)

	ip = n.GetIPByType(addressing.NodeExternalIP, false)
	c.Assert(ip.Equal(net.ParseIP("192.0.2.3")), Equals, true)
	ip = n.GetIPByType(addressing.NodeExternalIP, true)
	c.Assert(ip, IsNil)

	n = Node{
		Name: "node-2",
		IPAddresses: []Address{
			{IP: net.ParseIP("f00b::1"), Type: addressing.NodeCCEInternalIP},
		},
	}

	ip = n.GetIPByType(addressing.NodeExternalIP, false)
	c.Assert(ip, IsNil)
	ip = n.GetIPByType(addressing.NodeExternalIP, true)
	c.Assert(ip, IsNil)

	ip = n.GetIPByType(addressing.NodeCCEInternalIP, false)
	c.Assert(ip, IsNil)
	ip = n.GetIPByType(addressing.NodeCCEInternalIP, true)
	c.Assert(ip.Equal(net.ParseIP("f00b::1")), Equals, true)

	n = Node{
		Name: "node-3",
		IPAddresses: []Address{
			{IP: net.ParseIP("192.42.0.3"), Type: addressing.NodeExternalIP},
			{IP: net.ParseIP("f00d::1"), Type: addressing.NodeExternalIP},
		},
	}

	ip = n.GetIPByType(addressing.NodeInternalIP, false)
	c.Assert(ip, IsNil)
	ip = n.GetIPByType(addressing.NodeInternalIP, true)
	c.Assert(ip, IsNil)

	ip = n.GetIPByType(addressing.NodeExternalIP, false)
	c.Assert(ip.Equal(net.ParseIP("192.42.0.3")), Equals, true)
	ip = n.GetIPByType(addressing.NodeExternalIP, true)
	c.Assert(ip.Equal(net.ParseIP("f00d::1")), Equals, true)
}

func (s *NodeSuite) TestParseNetResourceSet(c *C) {
	nodeResource := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"},
		Spec: ccev2.NetResourceSpec{
			Addresses: []ccev2.NodeAddress{
				{Type: addressing.NodeInternalIP, IP: "2.2.2.2"},
				{Type: addressing.NodeExternalIP, IP: "3.3.3.3"},
				{Type: addressing.NodeInternalIP, IP: "c0de::1"},
				{Type: addressing.NodeExternalIP, IP: "c0de::2"},
			},

			IPAM: ipamTypes.IPAMSpec{
				PodCIDRs: []string{
					"10.10.0.0/16",
					"c0de::/96",
					"10.20.0.0/16",
					"c0fe::/96",
				},
			},
		},
	}

	n := ParseNetResourceSet(nodeResource)
	c.Assert(n, checker.DeepEquals, Node{
		Name: "foo",
		IPAddresses: []Address{
			{Type: addressing.NodeInternalIP, IP: net.ParseIP("2.2.2.2")},
			{Type: addressing.NodeExternalIP, IP: net.ParseIP("3.3.3.3")},
			{Type: addressing.NodeInternalIP, IP: net.ParseIP("c0de::1")},
			{Type: addressing.NodeExternalIP, IP: net.ParseIP("c0de::2")},
		},
		IPv4AllocCIDR:           cidr.MustParseCIDR("10.10.0.0/16"),
		IPv6AllocCIDR:           cidr.MustParseCIDR("c0de::/96"),
		IPv4SecondaryAllocCIDRs: []*cidr.CIDR{cidr.MustParseCIDR("10.20.0.0/16")},
		IPv6SecondaryAllocCIDRs: []*cidr.CIDR{cidr.MustParseCIDR("c0fe::/96")},
	})
}

func (s *NodeSuite) TestNode_ToNetResourceSet(c *C) {
	nodeResource := Node{
		Name: "foo",
		IPAddresses: []Address{
			{Type: addressing.NodeInternalIP, IP: net.ParseIP("2.2.2.2")},
			{Type: addressing.NodeExternalIP, IP: net.ParseIP("3.3.3.3")},
			{Type: addressing.NodeInternalIP, IP: net.ParseIP("c0de::1")},
			{Type: addressing.NodeExternalIP, IP: net.ParseIP("c0de::2")},
		},
		IPv4AllocCIDR:           cidr.MustParseCIDR("10.10.0.0/16"),
		IPv6AllocCIDR:           cidr.MustParseCIDR("c0de::/96"),
		IPv4SecondaryAllocCIDRs: []*cidr.CIDR{cidr.MustParseCIDR("10.20.0.0/16")},
		IPv6SecondaryAllocCIDRs: []*cidr.CIDR{cidr.MustParseCIDR("c0fe::/96")},
	}

	n := nodeResource.ToNetResourceSet()
	c.Assert(n, checker.DeepEquals, &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "foo",
			Namespace:   "",
			Annotations: map[string]string{},
		},
		Spec: ccev2.NetResourceSpec{
			Addresses: []ccev2.NodeAddress{
				{Type: addressing.NodeInternalIP, IP: "2.2.2.2"},
				{Type: addressing.NodeExternalIP, IP: "3.3.3.3"},
				{Type: addressing.NodeInternalIP, IP: "c0de::1"},
				{Type: addressing.NodeExternalIP, IP: "c0de::2"},
			},
			IPAM: ipamTypes.IPAMSpec{
				PodCIDRs: []string{
					"10.10.0.0/16",
					"c0de::/96",
					"10.20.0.0/16",
					"c0fe::/96",
				},
			},
		},
	})
}
