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

package ipam

import (
	"net"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/addressing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
)

func Test(t *testing.T) {
	TestingT(t)
}

type IPAMSuite struct{}

var _ = Suite(&IPAMSuite{})

func fakeIPv4AllocCIDRIP(fakeAddressing types.NodeAddressing) net.IP {
	// force copy so net.IP can be modified
	return net.ParseIP(fakeAddressing.IPv4().AllocationCIDR().IP.String())
}

func fakeIPv6AllocCIDRIP(fakeAddressing types.NodeAddressing) net.IP {
	// force copy so net.IP can be modified
	return net.ParseIP(fakeAddressing.IPv6().AllocationCIDR().IP.String())
}

type testConfiguration struct{}

func (t *testConfiguration) IPv4Enabled() bool                        { return true }
func (t *testConfiguration) IPv6Enabled() bool                        { return false }
func (t *testConfiguration) HealthCheckingEnabled() bool              { return true }
func (t *testConfiguration) UnreachableRoutesEnabled() bool           { return false }
func (t *testConfiguration) IPAMMode() string                         { return ipamOption.IPAMClusterPool }
func (t *testConfiguration) SetIPv4NativeRoutingCIDR(cidr *cidr.CIDR) {}
func (t *testConfiguration) GetIPv4NativeRoutingCIDR() *cidr.CIDR     { return nil }
func (t *testConfiguration) GetCCEEndpointGC() time.Duration          { return 0 }
func (t *testConfiguration) GetFixedIPTimeout() time.Duration         { return 0 }

func (s *IPAMSuite) TestLock(c *C) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	// Since the IPs we have allocated to the endpoints might or might not
	// be in the allocrange specified in cce, we need to specify them
	// manually on the endpoint based on the alloc range.
	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	nextIP(ipv4)
	epipv4, err := addressing.NewCCEIPv4(ipv4.String())
	c.Assert(err, IsNil)

	ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)
	nextIP(ipv6)
	epipv6, err := addressing.NewCCEIPv6(ipv6.String())
	c.Assert(err, IsNil)

	// Forcefully release possible allocated IPs
	err = ipam.IPv4Allocator.Release(epipv4.IP())
	c.Assert(err, IsNil)
	err = ipam.IPv6Allocator.Release(epipv6.IP())
	c.Assert(err, IsNil)

	// Let's allocate the IP first so we can see the tests failing
	result, err := ipam.IPv4Allocator.Allocate(epipv4.IP(), "test")
	c.Assert(err, IsNil)
	c.Assert(result.IP, checker.DeepEquals, epipv4.IP())

	err = ipam.IPv4Allocator.Release(epipv4.IP())
	c.Assert(err, IsNil)
}

func BenchmarkIPAMRun(b *testing.B) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
	// Since the IPs we have allocated to the endpoints might or might not
	// be in the allocrange specified in cce, we need to specify them
	// manually on the endpoint based on the alloc range.
	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	nextIP(ipv4)
	epipv4, _ := addressing.NewCCEIPv4(ipv4.String())

	for i := 0; i < b.N; i++ {
		// Forcefully release possible allocated IPs
		ipam.IPv4Allocator.Release(epipv4.IP())

		// Let's allocate the IP first so we can see the tests failing
		ipam.IPv4Allocator.Allocate(epipv4.IP(), "test")

		ipam.IPv4Allocator.Release(epipv4.IP())
	}
}

func (s *IPAMSuite) TestBlackList(c *C) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	nextIP(ipv4)

	ipam.BlacklistIP(ipv4, "test")
	err := ipam.AllocateIP(ipv4, "test")
	c.Assert(err, Not(IsNil))
	ipam.ReleaseIP(ipv4)

	ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)
	nextIP(ipv6)

	ipam.BlacklistIP(ipv6, "test")
	err = ipam.AllocateIP(ipv6, "test")
	c.Assert(err, Not(IsNil))
	ipam.ReleaseIP(ipv6)
}

func (s *IPAMSuite) TestDeriveFamily(c *C) {
	c.Assert(DeriveFamily(net.ParseIP("1.1.1.1")), Equals, IPv4)
	c.Assert(DeriveFamily(net.ParseIP("f00d::1")), Equals, IPv6)
}

func (s *IPAMSuite) TestOwnerRelease(c *C) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	nextIP(ipv4)
	err := ipam.AllocateIP(ipv4, "default/test")
	c.Assert(err, IsNil)

	ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)
	nextIP(ipv6)
	err = ipam.AllocateIP(ipv6, "default/test")
	c.Assert(err, IsNil)

	// unknown owner, must fail
	err = ipam.ReleaseIPString("default/test2")
	c.Assert(err, Not(IsNil))
	// 1st release by correct owner, must succeed
	err = ipam.ReleaseIPString("default/test")
	c.Assert(err, IsNil)
	// 2nd release by owner, must now fail
	err = ipam.ReleaseIPString("default/test")
	c.Assert(err, Not(IsNil))
}

func BenchmarkCNIPluginNoAPIServer(b *testing.B) {
	cidrs, _ := ip.ParseCIDRs([]string{"10.244.0.0/16"})
	node.SetIPv4AllocRange(&cidr.CIDR{IPNet: cidrs[0]})

	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	for i := 0; i < b.N; i++ {
		ipv4, _, _ := ipam.AllocateNext("", "foo")
		// IPv4 address must be in use
		ipam.AllocateIP(ipv4.IP, "foo")
		ipam.ReleaseIP(ipv4.IP)
	}

}
