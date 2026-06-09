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

	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/addressing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/mtu"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
)

// init init 函数用于初始化 fake k8s 客户端，避免 nil 指针解引用
// 如果初始化失败则 panic 出错误信息
func init() {
	// 初始化 fake k8s 客户端，避免 nil 指针解引用
	if err := k8s.InitNewFakeK8sClient(); err != nil {
		panic(fmt.Sprintf("failed to initialize fake k8s client: %v", err))
	}
}

type ownerMock struct{}

func (o *ownerMock) K8sEventReceived(resourceApiGroup, scope string, action string, valid, equal bool) {
}
func (o *ownerMock) K8sEventProcessed(scope string, action string, status bool)   {}
func (o *ownerMock) RegisterNetResourceSetSubscriber(s subscriber.NetResourceSet) {}

func (o *ownerMock) UpdateNetResourceSetResource()                                      {}
func (o *ownerMock) LocalAllocCIDRsUpdated(ipv4AllocCIDRs, ipv6AllocCIDRs []*cidr.CIDR) {}
func (o *ownerMock) ResourceType() string {
	return ccev2.NetResourceSetEventHandlerTypeEth
}

var mtuMock = mtu.NewConfiguration(0, false, false, false, 1500, nil)

func (s *IPAMSuite) TestAllocatedIPDump(c *C) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)

	for i := 0; i < 10; i++ {
		_, err := addressing.NewCCEIPv4(ipv4.String())
		c.Assert(err, IsNil)
		nextIP(ipv4)

		_, err = addressing.NewCCEIPv6(ipv6.String())
		c.Assert(err, IsNil)
		nextIP(ipv6)
	}

	allocv4, allocv6, status := ipam.Dump()
	c.Assert(status, Not(Equals), "")

	// Test the format of the dumped ip addresses
	for ip := range allocv4 {
		c.Assert(net.ParseIP(ip), NotNil)
	}
	for ip := range allocv6 {
		c.Assert(net.ParseIP(ip), NotNil)
	}
}
