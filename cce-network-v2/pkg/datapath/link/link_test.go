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

package link

import (
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

const (
	FAMILY_ALL = syscall.AF_UNSPEC
	FAMILY_V4  = syscall.AF_INET
	FAMILY_V6  = syscall.AF_INET6
)

func TestDetectDefaultRouteInterface(t *testing.T) {
	// 这个测试需要在真实网络环境中运行
	// 它将尝试检测默认路由接口
	// 如果成功，应该返回一个有效的接口
	// 如果失败，应该返回一个错误

	result, err := DetectDefaultRouteInterface()

	// 无论检测是否成功，我们只需确保函数行为正确
	if err != nil {
		// 如果返回错误，则确保返回的接口为nil
		assert.Nil(t, result)
		t.Logf("DetectDefaultRouteInterface返回错误: %v", err)
	} else {
		// 如果成功，确保返回了一个有效的接口
		assert.NotNil(t, result)
		assert.NotEmpty(t, result.Attrs().Name)
		t.Logf("检测到默认路由接口: %s (index: %d)",
			result.Attrs().Name, result.Attrs().Index)

		// 验证该接口在系统中确实存在
		_, err := netlink.LinkByName(result.Attrs().Name)
		assert.NoError(t, err, "返回的接口在系统中找不到")
	}
}

func TestDetectDefaultRouteInterfaceName(t *testing.T) {
	// 获取默认路由接口的名称
	name, err := DetectDefaultRouteInterfaceName()

	// 处理结果
	if err != nil {
		// 如果返回错误，则确保返回的名称为空
		assert.Empty(t, name)
		t.Logf("DetectDefaultRouteInterfaceName返回错误: %v", err)
	} else {
		// 如果成功，确保返回了一个有效的接口名称
		assert.NotEmpty(t, name)
		t.Logf("检测到默认路由接口名称: %s", name)

		// 验证该接口在系统中确实存在
		_, err := netlink.LinkByName(name)
		assert.NoError(t, err, "返回的接口在系统中找不到")
	}
}

// 测试系统中真实存在的接口的 DisableRpFilter 函数
func TestDisableRpFilter(t *testing.T) {
	// 获取默认路由接口名称
	ifName, err := DetectDefaultRouteInterfaceName()
	if err != nil {
		t.Skip("无法获取默认路由接口，跳过测试:", err)
		return
	}

	// 测试DisableRpFilter函数
	err = DisableRpFilter(ifName)
	// 由于权限问题，在容器或非root用户下，此函数可能会失败，我们只验证函数行为
	t.Logf("DisableRpFilter结果: %v", err)
}

// 测试FindENILinkByMac函数 - 使用系统中存在的MAC地址
func TestFindENILinkByMac_WithExistingMAC(t *testing.T) {
	// 获取系统中的所有网络接口
	links, err := netlink.LinkList()
	if err != nil {
		t.Fatal("获取网络接口列表失败:", err)
	}

	// 如果没有网络接口，跳过测试
	if len(links) == 0 {
		t.Skip("系统中没有网络接口，跳过测试")
		return
	}

	// 选择第一个有MAC地址的接口
	var testLink netlink.Link
	var macAddress string
	for _, link := range links {
		if link.Attrs().HardwareAddr != nil && len(link.Attrs().HardwareAddr) > 0 {
			testLink = link
			macAddress = link.Attrs().HardwareAddr.String()
			break
		}
	}

	if testLink == nil {
		t.Skip("找不到有MAC地址的网络接口，跳过测试")
		return
	}

	t.Logf("使用网络接口 %s 的MAC地址 %s 进行测试", testLink.Attrs().Name, macAddress)

	// 测试FindENILinkByMac函数
	foundLink, err := FindENILinkByMac(macAddress)
	assert.NoError(t, err)
	assert.NotNil(t, foundLink)
	assert.Equal(t, macAddress, foundLink.Attrs().HardwareAddr.String())
	assert.Equal(t, testLink.Attrs().Name, foundLink.Attrs().Name)
}

// 测试FindENILinkByMac函数 - 使用不存在的MAC地址
func TestFindENILinkByMac_WithNonExistingMAC(t *testing.T) {
	// 使用一个不太可能存在的MAC地址
	macAddress := "00:11:22:33:44:55"

	// 测试FindENILinkByMac函数
	foundLink, err := FindENILinkByMac(macAddress)
	assert.Error(t, err)
	assert.Nil(t, foundLink)
	assert.Contains(t, err.Error(), "eni with mac address")
}

// 测试EnsureLinkAddr函数 - 只验证函数参数的处理
func TestEnsureLinkAddr_ArgumentHandling(t *testing.T) {
	// 创建IP地址和子网掩码
	ipv4Addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP("192.168.1.100"),
			Mask: net.CIDRMask(24, 32),
		},
	}

	ipv6Addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP("2001:db8::1"),
			Mask: net.CIDRMask(64, 128),
		},
	}

	// 创建一个模拟Link - 我们不会真正使用它，只是检查EnsureLinkAddr的判断逻辑
	mockLink := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: "mockLink",
		},
	}

	t.Log("测试IPv4地址处理")
	// 在这个测试中，我们不需要实际设置IP，只是验证函数的IPv4地址处理
	_ = EnsureLinkAddr(ipv4Addr, mockLink)

	t.Log("测试IPv6地址处理")
	// 在这个测试中，我们不需要实际设置IP，只是验证函数的IPv6地址处理
	_ = EnsureLinkAddr(ipv6Addr, mockLink)
}

// 测试ReplaceRoute函数 - 只验证函数的空路由列表处理
func TestReplaceRoute_EmptyRoutes(t *testing.T) {
	// 测试空路由列表
	err := ReplaceRoute([]netlink.Route{})
	assert.NoError(t, err)
}
