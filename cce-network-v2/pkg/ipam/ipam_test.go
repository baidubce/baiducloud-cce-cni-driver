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
	"fmt"
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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
)

func Test(t *testing.T) {
	TestingT(t)
}

type IPAMSuite struct{}

var _ = Suite(&IPAMSuite{})

// init init 函数用于初始化 fake k8s 客户端，避免 nil 指针解引用
// 如果初始化失败则 panic 出错误信息
func init() {
	// 初始化 fake k8s 客户端，避免 nil 指针解引用
	if err := k8s.InitNewFakeK8sClient(); err != nil {
		panic(fmt.Sprintf("failed to initialize fake k8s client: %v", err))
	}
}

// fakeIPv4AllocCIDRIP 返回值net.IP类型，表示分配的CIDR IP地址
func fakeIPv4AllocCIDRIP(fakeAddressing types.NodeAddressing) net.IP {
	// force copy so net.IP can be modified
	cidr := fakeAddressing.IPv4().AllocationCIDR()
	if cidr == nil {
		return net.ParseIP("10.0.0.1")
	}
	return net.ParseIP(cidr.IP.String())
}

// fakeIPv6AllocCIDRIP 返回一个net.IP，该IP是fakeAddressing的IPv6分配CIDR的第一个IP地址。如果fakeAddressing没有设置IPv6分配CIDR，则返回"f00d::1"。
func fakeIPv6AllocCIDRIP(fakeAddressing types.NodeAddressing) net.IP {
	// force copy so net.IP can be modified
	cidr := fakeAddressing.IPv6().AllocationCIDR()
	if cidr == nil {
		return net.ParseIP("f00d::1")
	}
	return net.ParseIP(cidr.IP.String())
}

type testConfiguration struct{}

// IPv4Enabled 函数名: IPv4Enabled
// 功能：判断当前系统是否支持IPv4协议，返回一个bool值，表示是否支持。
func (t *testConfiguration) IPv4Enabled() bool { return true }

// IPv6Enabled 返回值bool类型，表示是否启用了IPv6。
// 如果启用了IPv6，则返回true；否则返回false。
func (t *testConfiguration) IPv6Enabled() bool                        { return true }
func (t *testConfiguration) HealthCheckingEnabled() bool              { return true }
func (t *testConfiguration) UnreachableRoutesEnabled() bool           { return false }
func (t *testConfiguration) IPAMMode() string                         { return ipamOption.IPAMClusterPool }
func (t *testConfiguration) SetIPv4NativeRoutingCIDR(cidr *cidr.CIDR) {}
func (t *testConfiguration) GetIPv4NativeRoutingCIDR() *cidr.CIDR     { return nil }
func (t *testConfiguration) GetCCEEndpointGC() time.Duration          { return 0 }
func (t *testConfiguration) GetFixedIPTimeout() time.Duration         { return 0 }

// TestLock 测试锁定功能
//
// 参数c：*C，C代表go-check框架的一个对象，用于断言和错误处理
//
// 返回值：无
func (s *IPAMSuite) TestLock(c *C) {
	// 设置 IPv4 和 IPv6 分配范围
	_, ipv4Net, _ := net.ParseCIDR("10.244.0.0/16")
	node.SetIPv4AllocRange(&cidr.CIDR{IPNet: ipv4Net})

	_, ipv6Net, _ := net.ParseCIDR("f00d::/104")
	node.SetIPv6NodeRange(&cidr.CIDR{IPNet: ipv6Net})

	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	// Since the IPs we have allocated to the endpoints might or might not
	// be in the allocrange specified in cce, we need to specify them
	// manually on the endpoint based on the alloc range.
	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	nextIP(ipv4)
	epipv4, err := addressing.NewCCEIPv4(ipv4.String())
	c.Assert(err, IsNil)

	// Only test IPv6 if it's enabled
	var epipv6 addressing.CCEIPv6
	var hasIPv6 bool
	if ipam.IPv6Allocator != nil {
		ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)
		nextIP(ipv6)
		epipv6, err = addressing.NewCCEIPv6(ipv6.String())
		c.Assert(err, IsNil)
		hasIPv6 = true
	}

	// Forcefully release possible allocated IPs
	err = ipam.IPv4Allocator.Release(epipv4.IP())
	c.Assert(err, IsNil)
	if ipam.IPv6Allocator != nil && hasIPv6 {
		err = ipam.IPv6Allocator.Release(epipv6.IP())
		c.Assert(err, IsNil)
	}

	// Let's allocate the IP first so we can see the tests failing
	result, err := ipam.IPv4Allocator.Allocate(epipv4.IP(), "test")
	c.Assert(err, IsNil)
	c.Assert(result.IP, checker.DeepEquals, epipv4.IP())

	err = ipam.IPv4Allocator.Release(epipv4.IP())
	c.Assert(err, IsNil)
}

func BenchmarkIPAMRun(b *testing.B) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
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

// TestBlackList 测试黑名单功能
//
// 参数c：*C，C测试框架指针
// 返回值：无返回值
func (s *IPAMSuite) TestBlackList(c *C) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	nextIP(ipv4)

	ipam.BlacklistIP(ipv4, "test")
	err := ipam.AllocateIP(ipv4, "test")
	c.Assert(err, Not(IsNil))
	ipam.ReleaseIP(ipv4)

	// Only test IPv6 if it's enabled
	if ipam.IPv6Allocator != nil {
		ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)
		nextIP(ipv6)

		ipam.BlacklistIP(ipv6, "test")
		err = ipam.AllocateIP(ipv6, "test")
		c.Assert(err, Not(IsNil))
		ipam.ReleaseIP(ipv6)
	}
}

func (s *IPAMSuite) TestDeriveFamily(c *C) {
	c.Assert(DeriveFamily(net.ParseIP("1.1.1.1")), Equals, IPv4)
	c.Assert(DeriveFamily(net.ParseIP("f00d::1")), Equals, IPv6)
}

// TestOwnerRelease 测试 OwnerRelease 函数，释放 IP 地址的所有权。
//
// 参数：
//
//	c (*C) - go-check 库中的一个测试对象，用于断言和记录错误信息。
//
// 返回值：
//
//	none - 无返回值，所有操作都是通过断言来确保结果正确。
func (s *IPAMSuite) TestOwnerRelease(c *C) {
	// 设置 IPv4 和 IPv6 分配范围
	_, ipv4Net, _ := net.ParseCIDR("10.244.0.0/16")
	node.SetIPv4AllocRange(&cidr.CIDR{IPNet: ipv4Net})

	_, ipv6Net, _ := net.ParseCIDR("f00d::/104")
	node.SetIPv6NodeRange(&cidr.CIDR{IPNet: ipv6Net})

	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	nextIP(ipv4)
	err := ipam.AllocateIP(ipv4, "default/test")
	c.Assert(err, IsNil)

	// Only test IPv6 if it's enabled
	if ipam.IPv6Allocator != nil {
		ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)
		nextIP(ipv6)
		err = ipam.AllocateIP(ipv6, "default/test")
		c.Assert(err, IsNil)
	}

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
	ipam := NewIPAM("nrs-test", fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	for i := 0; i < b.N; i++ {
		ipv4, _, _ := ipam.AllocateNext("", "foo")
		// IPv4 address must be in use
		ipam.AllocateIP(ipv4.IP, "foo")
		ipam.ReleaseIP(ipv4.IP)
	}

}
