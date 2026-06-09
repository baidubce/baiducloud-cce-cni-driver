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
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/addressing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/trigger"
)

// init init 函数用于初始化 fake k8s 客户端，避免 nil 指针解引用
// 如果初始化失败则 panic 出错误信息
func init() {
	// 初始化 fake k8s 客户端，避免 nil 指针解引用
	if err := k8s.InitNewFakeK8sClient(); err != nil {
		panic(fmt.Sprintf("failed to initialize fake k8s client: %v", err))
	}
}

func TestIPNotAvailableInPoolError(t *testing.T) {
	err := NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	err2 := NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	assert.Equal(t, err, err2)
	assert.True(t, errors.Is(err, err2))

	err = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))

	err = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	err2 = errors.New("another error")
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))

	err = errors.New("another error")
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))

	err = NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	err2 = nil
	assert.False(t, errors.Is(err, err2))

	err = nil
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	assert.False(t, errors.Is(err, err2))

	// We don't match against strings. It must be the sentinel value.
	err = errors.New("IP 2.1.1.1 is not available")
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))
}

type testConfigurationCRD struct{}

func (t *testConfigurationCRD) IPv4Enabled() bool                        { return true }
func (t *testConfigurationCRD) IPv6Enabled() bool                        { return false }
func (t *testConfigurationCRD) HealthCheckingEnabled() bool              { return true }
func (t *testConfigurationCRD) UnreachableRoutesEnabled() bool           { return false }
func (t *testConfigurationCRD) IPAMMode() string                         { return ipamOption.IPAMCRD }
func (t *testConfigurationCRD) BlacklistConflictingRoutesEnabled() bool  { return false }
func (t *testConfigurationCRD) SetIPv4NativeRoutingCIDR(cidr *cidr.CIDR) {}
func (t *testConfigurationCRD) GetIPv4NativeRoutingCIDR() *cidr.CIDR     { return nil }
func (t *testConfigurationCRD) IPv4NativeRoutingCIDR() *cidr.CIDR        { return nil }
func (t *testConfigurationCRD) GetCCEEndpointGC() time.Duration          { return 0 }
func (t *testConfigurationCRD) GetFixedIPTimeout() time.Duration         { return 0 }

// newFakeNetResourceSetStore 新建一个 fakeNetResourceSetStore，用于测试
// 参数：conf - 配置信息，类型为 Configuration
//
//	c - C 对象指针，用于断言
//
// 返回值：*nodeStore - 返回一个 nodeStore 指针，包含了初始化的 allocators、allocationPoolSize、conf、refreshTrigger 和 hasRestoreFinished 字段
func newFakeNetResourceSetStore(conf Configuration, c *C) *nodeStore {
	t, err := trigger.NewTrigger(trigger.Parameters{
		Name:        "fake-crd-allocator-node-refresher",
		MinInterval: 3 * time.Second,
		TriggerFunc: func(reasons []string) {},
	})
	if err != nil {
		log.WithError(err).Fatal("Unable to initialize NetResourceSet synchronization trigger")
	}
	store := &nodeStore{
		allocators:         []*crdAllocator{},
		allocationPoolSize: map[Family]int{},
		conf:               conf,
		refreshTrigger:     t,
		hasRestoreFinished: true,
	}
	return store
}

func (s *IPAMSuite) TestMarkForReleaseNoAllocate(c *C) {
	cn := newNetResourceSet("node1", 4, 4, 0)
	dummyResource := ipamTypes.AllocationIP{Resource: "foo"}
	for i := 1; i <= 4; i++ {
		cn.Spec.IPAM.Pool[fmt.Sprintf("1.1.1.%d", i)] = dummyResource
	}

	fakeAddressing := linux.NewNodeAddressing()
	conf := &testConfigurationCRD{}
	initNetResourceSetStore.Do(func() {
		sharedNetResourceSetStore = newFakeNetResourceSetStore(conf, c)
		sharedNetResourceSetStore.ownNode = cn
	})
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, conf, &ownerMock{}, &ownerMock{}, &mtuMock)
	sharedNetResourceSetStore.updateLocalNodeResource(cn)

	// Allocate the first 3 IPs
	for i := 1; i <= 3; i++ {
		epipv4, _ := addressing.NewCCEIPv4(fmt.Sprintf("1.1.1.%d", i))
		_, err := ipam.IPv4Allocator.Allocate(epipv4.IP(), fmt.Sprintf("test%d", i))
		c.Assert(err, IsNil)
	}

	// Update 1.1.1.4 as marked for release like operator would.
	cn.Status.IPAM.ReleaseIPs["1.1.1.4"] = ipamOption.IPAMMarkForRelease
	// Attempts to allocate 1.1.1.4 should fail, since it's already marked for release
	epipv4, _ := addressing.NewCCEIPv4("1.1.1.4")
	_, err := ipam.IPv4Allocator.Allocate(epipv4.IP(), "test")
	c.Assert(err, NotNil)
	// Call agent's CRD update function. status for 1.1.1.4 should change from marked for release to ready for release
	sharedNetResourceSetStore.updateLocalNodeResource(cn)
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["1.1.1.4"]), checker.Equals, ipamOption.IPAMReadyForRelease)

	// Verify that 1.1.1.3 is denied for release, since it's already in use
	cn.Status.IPAM.ReleaseIPs["1.1.1.3"] = ipamOption.IPAMMarkForRelease
	sharedNetResourceSetStore.updateLocalNodeResource(cn)
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["1.1.1.3"]), checker.Equals, ipamOption.IPAMDoNotRelease)
}

// TestStaleReleasedIPHandling 测试stale released IP的处理逻辑
func (e *IPAMSuite) TestStaleReleasedIPHandling(c *C) {
	// Given - 创建包含IP池和释放IP状态的NetResourceSet
	cn := newCCENode("node1", addressing.MustNewCIDRFromString("1.1.0.0/16"), "1.1.1.0/24")
	cn.Spec.IPAM.Pool = ipamTypes.AllocationMap{
		"1.1.1.1": {Resource: "foo"},
		"1.1.1.2": {Resource: "foo"},
		"1.1.1.3": {Resource: "foo"}, // 这个IP在pool中但标记为released
		"1.1.1.4": {Resource: "foo"},
	}
	cn.Status.IPAM.ReleaseIPs = map[string]string{
		"1.1.1.3": ipamOption.IPAMReleased, // Stale released IP that's back in pool
		"1.1.1.5": ipamOption.IPAMReleased, // Properly released IP not in pool
	}

	// When - 调用updateLocalNodeResource处理stale released IP
	sharedNetResourceSetStore.updateLocalNodeResource(cn)

	// Then - 验证stale released IP被重置为ready-for-release状态
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["1.1.1.3"]), checker.Equals, ipamOption.IPAMReadyForRelease)

	// 验证不在pool中的released IP被正确删除
	_, exists := cn.Status.IPAM.ReleaseIPs["1.1.1.5"]
	c.Assert(exists, checker.Equals, false)

	// 验证其他IP状态不受影响
	c.Assert(len(cn.Status.IPAM.ReleaseIPs), checker.Equals, 1)
	c.Assert(cn.Status.IPAM.ReleaseIPs["1.1.1.3"], checker.Equals, ipamOption.IPAMReadyForRelease)
}

// TestUpdateLocalNodeResource_MultipleStaleReleasedIPs 测试多个stale released IP的处理
func (e *IPAMSuite) TestUpdateLocalNodeResource_MultipleStaleReleasedIPs(c *C) {
	// Given - 创建包含多个stale released IP的NetResourceSet
	cn := newCCENode("node2", addressing.MustNewCIDRFromString("2.2.0.0/16"), "2.2.1.0/24")
	cn.Spec.IPAM.Pool = ipamTypes.AllocationMap{
		"2.2.1.1": {Resource: "eni-1"},
		"2.2.1.2": {Resource: "eni-1"},
		"2.2.1.3": {Resource: "eni-2"},
		"2.2.1.4": {Resource: "eni-2"},
	}
	cn.Status.IPAM.ReleaseIPs = map[string]string{
		"2.2.1.1": ipamOption.IPAMReleased,       // Stale released IP
		"2.2.1.3": ipamOption.IPAMReleased,       // Another stale released IP
		"2.2.1.5": ipamOption.IPAMReleased,       // Properly released IP not in pool
		"2.2.1.6": ipamOption.IPAMMarkForRelease, // Different status, should not change
	}

	// When
	sharedNetResourceSetStore.updateLocalNodeResource(cn)

	// Then - 验证所有stale released IP都被重置
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["2.2.1.1"]), checker.Equals, ipamOption.IPAMReadyForRelease)
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["2.2.1.3"]), checker.Equals, ipamOption.IPAMReadyForRelease)

	// 验证不在pool中的released IP被删除
	_, exists := cn.Status.IPAM.ReleaseIPs["2.2.1.5"]
	c.Assert(exists, checker.Equals, false)

	// 验证其他状态的IP不受影响
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["2.2.1.6"]), checker.Equals, ipamOption.IPAMMarkForRelease)

	// 验证最终状态
	c.Assert(len(cn.Status.IPAM.ReleaseIPs), checker.Equals, 3)
}

// TestUpdateLocalNodeResource_EmptyPoolWithReleasedIPs 测试空IP池但有released IP的情况
func (e *IPAMSuite) TestUpdateLocalNodeResource_EmptyPoolWithReleasedIPs(c *C) {
	// Given - 创建空IP池但有released IP的NetResourceSet
	cn := newCCENode("node3", addressing.MustNewCIDRFromString("3.3.0.0/16"), "3.3.1.0/24")
	cn.Spec.IPAM.Pool = nil // Empty pool
	cn.Status.IPAM.ReleaseIPs = map[string]string{
		"3.3.1.1": ipamOption.IPAMReleased,
		"3.3.1.2": ipamOption.IPAMReleased,
	}

	// When
	sharedNetResourceSetStore.updateLocalNodeResource(cn)

	// Then - 验证所有released IP都被删除（因为不在pool中）
	c.Assert(len(cn.Status.IPAM.ReleaseIPs), checker.Equals, 0)
}

// TestUpdateLocalNodeResource_MixedIPStates 测试混合IP状态的处理
func (e *IPAMSuite) TestUpdateLocalNodeResource_MixedIPStates(c *C) {
	// Given - 创建包含各种状态IP的NetResourceSet
	cn := newCCENode("node4", addressing.MustNewCIDRFromString("4.4.0.0/16"), "4.4.1.0/24")
	cn.Spec.IPAM.Pool = ipamTypes.AllocationMap{
		"4.4.1.1": {Resource: "eni-1"},
		"4.4.1.2": {Resource: "eni-1"},
	}
	cn.Status.IPAM.ReleaseIPs = map[string]string{
		"4.4.1.1": ipamOption.IPAMReleased,        // Stale released IP
		"4.4.1.2": ipamOption.IPAMMarkForRelease,  // Marked for release
		"4.4.1.3": ipamOption.IPAMReleased,        // Properly released IP not in pool
		"4.4.1.4": ipamOption.IPAMReadyForRelease, // Ready for release not in pool
		"4.4.1.5": ipamOption.IPAMDoNotRelease,    // Do not release not in pool
	}

	// When
	sharedNetResourceSetStore.updateLocalNodeResource(cn)

	// Then
	// Stale released IP should be reset to ready-for-release
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["4.4.1.1"]), checker.Equals, ipamOption.IPAMReadyForRelease)

	// Marked for release IP in pool should remain unchanged
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["4.4.1.2"]), checker.Equals, ipamOption.IPAMMarkForRelease)

	// IPs not in pool with released status should be removed
	_, exists := cn.Status.IPAM.ReleaseIPs["4.4.1.3"]
	c.Assert(exists, checker.Equals, false)

	// Other states not in pool should remain as they don't match the removed conditions
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["4.4.1.4"]), checker.Equals, ipamOption.IPAMReadyForRelease)
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["4.4.1.5"]), checker.Equals, ipamOption.IPAMDoNotRelease)

	// Verify final count
	c.Assert(len(cn.Status.IPAM.ReleaseIPs), checker.Equals, 4)
}
