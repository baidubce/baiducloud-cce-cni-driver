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
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/check.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
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

type testNeededDef struct {
	available   int
	used        int
	preallocate int
	minallocate int
	maxallocate int
	result      int
}

type testExcessDef struct {
	available         int
	used              int
	preallocate       int
	minallocate       int
	maxabovewatermark int
	result            int
}

var neededDef = []testNeededDef{
	{0, 0, 0, 16, 0, 16},
	{0, 0, 8, 16, 0, 16},
	{0, 0, 16, 8, 0, 16},
	{0, 0, 16, 0, 0, 16},
	{8, 0, 0, 16, 0, 8},
	{8, 4, 8, 0, 0, 4},
	{8, 4, 8, 8, 0, 4},
	{8, 4, 8, 8, 6, 0},
	{8, 4, 8, 0, 8, 0},
	{4, 4, 8, 0, 8, 4},
}

var excessDef = []testExcessDef{
	{0, 0, 0, 16, 0, 0},
	{15, 0, 8, 16, 8, 0},
	{17, 0, 8, 16, 0, 9}, // 17 used, 8 pre-allocate, 16 min-allocate => 1 excess
	{20, 0, 8, 16, 4, 0}, // 20 used, 8 pre-allocate, 16 min-allocate, 4 max-above-watermark => 0 excess
	{21, 0, 8, 0, 4, 9},  // 21 used, 8 pre-allocate, 4 max-above-watermark => 9 excess
	{20, 0, 8, 20, 8, 0},
	{16, 1, 8, 16, 8, 0},
	{20, 4, 8, 17, 8, 0},
	{20, 4, 8, 0, 0, 8},
	{20, 4, 8, 0, 8, 0},
}

func (e *IPAMSuite) TestCalculateNeededIPs(c *check.C) {
	for _, d := range neededDef {
		result := calculateNeededIPs(d.available, d.used, d.preallocate, d.minallocate, d.maxallocate, 0)
		c.Assert(result, check.Equals, d.result)
	}
}

func (e *IPAMSuite) TestCalculateExcessIPs(c *check.C) {
	for _, d := range excessDef {
		result := calculateExcessIPs(d.available, d.used, d.preallocate, d.minallocate, d.maxabovewatermark, 0)
		c.Assert(result, check.Equals, d.result)
	}
}

// Mock for NetResourceOperations
type mockNetResourceOperations struct {
	onReleaseIPs                         func(ctx context.Context, release *ReleaseAction) error
	onResyncInterfacesAndIPs             func(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error)
	onGetMaximumAllocatableIPv4          func() int
	onGetMinimumAllocatableIPv4          func() int
	onIsPrefixDelegated                  func() bool
	onGetMaximumBurstableAllocatableIPv4 func() int
	onGetUsedIPWithPrefixes              func() int
}

// GetUsedIPWithPrefixes 获取已使用的IP地址，包括前缀。如果设置了回调函数onGetUsedIPWithPrefixes，则会调用该函数返回结果；否则返回0
func (m *mockNetResourceOperations) GetUsedIPWithPrefixes() int {
	if m.onGetUsedIPWithPrefixes != nil {
		return m.onGetUsedIPWithPrefixes()
	}
	return 0
}

func (m *mockNetResourceOperations) UpdatedNode(obj *ccev2.NetResourceSet) {}

func (m *mockNetResourceOperations) PopulateStatusFields(resource *ccev2.NetResourceSet) {}

func (m *mockNetResourceOperations) CreateInterface(ctx context.Context, allocation *AllocationAction, scopedLog *logrus.Entry) (int, string, error) {
	return 0, "", fmt.Errorf("not implemented")
}

// ResyncInterfacesAndIPs mockNetResourceOperations 类型的对象方法，用于同步接口和IP地址
// ctx: 上下文信息，context.Context类型
// scopedLog: 日志记录器，*logrus.Entry类型
// 返回值1: ipamTypes.AllocationMap类型，已分配的IP地址映射关系
// 返回值2: error类型，错误信息
func (m *mockNetResourceOperations) ResyncInterfacesAndIPs(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
	if m.onResyncInterfacesAndIPs != nil {
		return m.onResyncInterfacesAndIPs(ctx, scopedLog)
	}
	return ipamTypes.AllocationMap{}, nil
}

func (m *mockNetResourceOperations) PrepareIPAllocation(scopedLog *logrus.Entry) (*AllocationAction, error) {
	return nil, nil
}

func (m *mockNetResourceOperations) AllocateIPs(ctx context.Context, allocation *AllocationAction) error {
	return nil
}

func (m *mockNetResourceOperations) PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ReleaseAction {
	return nil
}

func (m *mockNetResourceOperations) ReleaseIPs(ctx context.Context, release *ReleaseAction) error {
	if m.onReleaseIPs != nil {
		return m.onReleaseIPs(ctx, release)
	}
	return nil
}

// GetMaximumAllocatableIPv4 返回一个int类型的值，表示可以分配的最大IPv4地址数量。如果mockNetResourceOperations对象中存在onGetMaximumAllocatableIPv4函数，则调用该函数并返回其返回值；否则返回100。
func (m *mockNetResourceOperations) GetMaximumAllocatableIPv4() int {
	if m.onGetMaximumAllocatableIPv4 != nil {
		return m.onGetMaximumAllocatableIPv4()
	}
	return 100
}

// GetMinimumAllocatableIPv4 返回一个int类型的值，表示可以预分配的最小IPv4地址数量。如果onGetMinimumAllocatableIPv4函数不为nil，则调用该函数并返回其返回值；否则返回默认值defaults.IPAMPreAllocation。
func (m *mockNetResourceOperations) GetMinimumAllocatableIPv4() int {
	if m.onGetMinimumAllocatableIPv4 != nil {
		return m.onGetMinimumAllocatableIPv4()
	}
	return defaults.IPAMPreAllocation
}

// IsPrefixDelegated IsPrefixDelegated 判断是否为前缀委托，如果有回调函数则返回回调函数的结果，否则返回false
func (m *mockNetResourceOperations) IsPrefixDelegated() bool {
	if m.onIsPrefixDelegated != nil {
		return m.onIsPrefixDelegated()
	}
	return false
}

// GetMaximumBurstableAllocatableIPv4 返回值类型：int
// 返回最大可抢占分配的IPv4地址数量，如果没有设置回调函数则返回0
func (m *mockNetResourceOperations) GetMaximumBurstableAllocatableIPv4() int {
	if m.onGetMaximumBurstableAllocatableIPv4 != nil {
		return m.onGetMaximumBurstableAllocatableIPv4()
	}
	return 0
}

// Mock manager that can track metrics calls
type mockMetricsAPI struct {
	addIPReleaseCalls         []mockAddIPReleaseCall
	incAllocationAttemptCalls []mockIncAllocationAttemptCall
}

type mockAddIPReleaseCall struct {
	poolID string
	count  int64
}

type mockIncAllocationAttemptCall struct {
	reason string
	poolID string
}

func (m *mockMetricsAPI) AddIPRelease(poolID string, count int64) {
	m.addIPReleaseCalls = append(m.addIPReleaseCalls, mockAddIPReleaseCall{
		poolID: poolID,
		count:  count,
	})
}

func (m *mockMetricsAPI) IncAllocationAttempt(reason, poolID string) {
	m.incAllocationAttemptCalls = append(m.incAllocationAttemptCalls, mockIncAllocationAttemptCall{
		reason: reason,
		poolID: poolID,
	})
}

// Implement other required methods for MetricsAPI interface
func (m *mockMetricsAPI) AddIPAllocation(poolID string, count int64) {}
func (m *mockMetricsAPI) SetAllocatedIPs(typ string, allocated int)  {}
func (m *mockMetricsAPI) SetAvailableInterfaces(available int)       {}
func (m *mockMetricsAPI) SetAvailableIPsPerSubnet(subnetID string, availabilityZone string, available int) {
}
func (m *mockMetricsAPI) SetNetResourceSets(category string, nodes int)  {}
func (m *mockMetricsAPI) IncResyncCount()                                {}
func (m *mockMetricsAPI) PoolMaintainerTrigger() trigger.MetricsObserver { return nil }
func (m *mockMetricsAPI) K8sSyncTrigger() trigger.MetricsObserver        { return nil }
func (m *mockMetricsAPI) ResyncTrigger() trigger.MetricsObserver         { return nil }

// Mock NetResourceSetManager
type mockNetResourceSetManager struct {
	releaseExcessIPs bool
	prefixDelegation bool
	metricsAPI       MetricsAPI
	resyncTrigger    *trigger.Trigger
}

// InstancesAPIIsReady InstancesAPIIsReady 判断InstancesAPI是否已经准备好，返回true
func (m *mockNetResourceSetManager) InstancesAPIIsReady() bool {
	return true
}

// Helper function to create a mock NetResource for testing
func createMockNetResource(nodeName string) *NetResource {
	triggerInstance, _ := trigger.NewTrigger(trigger.Parameters{
		MinInterval: time.Millisecond,
		TriggerFunc: func([]string) {},
		Name:        "test-trigger",
	})

	mockMetrics := &mockMetricsAPI{
		addIPReleaseCalls:         []mockAddIPReleaseCall{},
		incAllocationAttemptCalls: []mockIncAllocationAttemptCall{},
	}

	resyncTrigger, _ := trigger.NewTrigger(trigger.Parameters{
		MinInterval: time.Millisecond,
		TriggerFunc: func([]string) {},
		Name:        "resync-trigger",
	})

	manager := &NetResourceSetManager{
		releaseExcessIPs: false,
		prefixDelegation: false,
		metricsAPI:       mockMetrics,
		resyncTrigger:    resyncTrigger,
	}

	resource := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		Spec: ccev2.NetResourceSpec{
			IPAM: ipamTypes.IPAMSpec{
				Pool:              ipamTypes.AllocationMap{},
				PreAllocate:       defaults.IPAMPreAllocation,
				MinAllocate:       0,
				MaxAllocate:       0,
				MaxAboveWatermark: 0,
			},
			ENI: &api.ENISpec{
				BurstableMehrfachENI: 0,
			},
		},
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				Used:       ipamTypes.AllocationMap{},
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{},
			},
		},
	}

	return &NetResource{
		name:                      nodeName,
		resource:                  resource,
		manager:                   manager,
		poolMaintainer:            triggerInstance,
		k8sSync:                   triggerInstance,
		retry:                     triggerInstance,
		retrySyncToAPIServer:      triggerInstance,
		ipsMarkedForRelease:       make(map[string]time.Time),
		ipReleaseStatus:           make(map[string]string),
		logLimiter:                logging.NewLimiter(time.Hour, 10),
		ops:                       &mockNetResourceOperations{},
		instanceRunning:           true,
		instanceStoppedRunning:    time.Time{},
		waitingForPoolMaintenance: false,
		available:                 ipamTypes.AllocationMap{},
		stats:                     Statistics{},
	}
}

// Test IsRunning method
func (e *IPAMSuite) TestIsRunning_GivenRunningInstance_WhenCalled_ThenReturnsTrue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.instanceRunning = true
	node.instanceStoppedRunning = time.Time{}

	// When
	result := node.IsRunning()

	// Then
	c.Assert(result, check.Equals, true)
}

// TestIsRunning_GivenStoppedInstance_WhenCalled_ThenReturnsFalse 函数TestIsRunning_GivenStoppedInstance_WhenCalled_ThenReturnsFalse用于在已停止的实例下调用，当被调用时返回false。
func (e *IPAMSuite) TestIsRunning_GivenStoppedInstance_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.instanceRunning = false

	// When
	result := node.IsRunning()

	// Then
	c.Assert(result, check.Equals, false)
}

func (e *IPAMSuite) TestIsRunning_GivenRecentlyStoppedInstance_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.instanceRunning = true
	node.instanceStoppedRunning = time.Now() // Recently stopped

	// When
	result := node.IsRunning()

	// Then
	c.Assert(result, check.Equals, false)
}

// Test Stats method
func (e *IPAMSuite) TestStats_GivenNetResource_WhenCalled_ThenReturnsStatsCopy(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expectedStats := Statistics{
		UsedIPs:      5,
		AvailableIPs: 10,
		NeededIPs:    2,
		ExcessIPs:    1,
	}
	node.stats = expectedStats

	// When
	result := node.Stats()

	// Then
	c.Assert(result, check.DeepEquals, expectedStats)
	// Verify it's a copy, not a reference
	result.UsedIPs = 999
	c.Assert(node.stats.UsedIPs, check.Equals, 5)
}

// Test Ops method
func (e *IPAMSuite) TestOps_GivenNetResource_WhenCalled_ThenReturnsOps(c *check.C) {
	// Given
	node := createMockNetResource("test-node")

	// When
	result := node.Ops()

	// Then
	c.Assert(result, check.Equals, node.ops)
}

// Test IsPrefixDelegationEnabled method
func (e *IPAMSuite) TestIsPrefixDelegationEnabled_GivenNilNode_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	var node *NetResource

	// When
	result := node.IsPrefixDelegationEnabled()

	// Then
	c.Assert(result, check.Equals, false)
}

// TestIsPrefixDelegationEnabled_GivenNilManager_WhenCalled_ThenReturnsFalse 测试TestIsPrefixDelegationEnabled_GivenNilManager_WhenCalled_Then返回false
// 参数c：*check.C - go-check框架的一个checker，用于断言和记录结果
func (e *IPAMSuite) TestIsPrefixDelegationEnabled_GivenNilManager_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.manager = nil

	// When
	result := node.IsPrefixDelegationEnabled()

	// Then
	c.Assert(result, check.Equals, false)
}

// TestIsPrefixDelegationEnabled_GivenEnabledManager_WhenCalled_ThenReturnsTrue 函数用于测试IsPrefixDelegationEnabled方法，当给定已启用的管理器时，调用该方法，返回true。
func (e *IPAMSuite) TestIsPrefixDelegationEnabled_GivenEnabledManager_WhenCalled_ThenReturnsTrue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.manager.prefixDelegation = true

	// When
	result := node.IsPrefixDelegationEnabled()

	// Then
	c.Assert(result, check.Equals, true)
}

// Test logger method
func (e *IPAMSuite) TestLogger_GivenNilNode_WhenCalled_ThenReturnsBaseLogger(c *check.C) {
	// Given
	var node *NetResource

	// When
	result := node.logger()

	// Then
	c.Assert(result, check.Equals, log)
}

// TestLogger_GivenValidNode_WhenCalled_ThenReturnsLoggerWithFields 参数c *check.C - 用于检查的对象指针，不能为nil
// 参数node *netResource - 待测试的网络资源节点，不能为nil
// 返回值 check.Suite - 返回一个Suite类型的结构体，包含了测试的相关信息和结果
func (e *IPAMSuite) TestLogger_GivenValidNode_WhenCalled_ThenReturnsLoggerWithFields(c *check.C) {
	// Given
	node := createMockNetResource("test-node")

	// When
	result := node.logger()

	// Then
	c.Assert(result, check.NotNil)
	// The logger should have additional fields, so it shouldn't be the same as the base logger
	c.Assert(result, check.Not(check.Equals), log)
}

// Test getter methods
func (e *IPAMSuite) TestGetMaxAboveWatermark_GivenNetResource_WhenCalled_ThenReturnsValue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expected := 5
	node.resource.Spec.IPAM.MaxAboveWatermark = expected

	// When
	result := node.getMaxAboveWatermark()

	// Then
	c.Assert(result, check.Equals, expected)
}

// TestGetPreAllocate_GivenZeroValue_WhenCalled_ThenReturnsDefault 测试TestGetPreAllocate_GivenZeroValue_WhenCalled_Then返回默认值
func (e *IPAMSuite) TestGetPreAllocate_GivenZeroValue_WhenCalled_ThenReturnsDefault(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.resource.Spec.IPAM.PreAllocate = 0

	// When
	result := node.getPreAllocate()

	// Then
	c.Assert(result, check.Equals, defaults.IPAMPreAllocation)
}

// TestGetPreAllocate_GivenNonZeroValue_WhenCalled_ThenReturnsValue 参数c *check.C - 测试检查点，用于断言结果
// 参数node *IPANode - IPAMSuite中的一个节点对象，包含了资源信息和操作方法
// 返回值 int - 预分配的IP数量，如果没有设置则为0
func (e *IPAMSuite) TestGetPreAllocate_GivenNonZeroValue_WhenCalled_ThenReturnsValue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expected := 10
	node.resource.Spec.IPAM.PreAllocate = expected

	// When
	result := node.getPreAllocate()

	// Then
	c.Assert(result, check.Equals, expected)
}

// TestGetMinAllocate_GivenNetResource_WhenCalled_ThenReturnsValue 参数c *check.C - 断言库的一个检查点，用于测试结果是否符合预期
// 参数node *mockNetResource - 包含网络资源信息的 mock 对象
// 返回值 int - 返回值为 MinAllocate 字段的值，如果没有设置则为 0
func (e *IPAMSuite) TestGetMinAllocate_GivenNetResource_WhenCalled_ThenReturnsValue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expected := 5
	node.resource.Spec.IPAM.MinAllocate = expected

	// When
	result := node.getMinAllocate()

	// Then
	c.Assert(result, check.Equals, expected)
}

// TestGetBurstableENIs_GivenNetResource_WhenCalled_ThenReturnsValue 参数c：*check.C - 断言工具，用于检查测试结果是否符合预期
// 返回值 bool - 如果测试通过则返回true，否则返回false
func (e *IPAMSuite) TestGetBurstableENIs_GivenNetResource_WhenCalled_ThenReturnsValue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expected := 3
	node.resource.Spec.ENI.BurstableMehrfachENI = expected

	// When
	result := node.getBurstableENIs()

	// Then
	c.Assert(result, check.Equals, expected)
}

// TestGetMaxAllocate_GivenZeroMaxAllocate_WhenCalled_ThenReturnsInstanceMax 函数接受一个check.C类型的参数c，用于检查测试结果是否符合预期
// 当给定maxAllocate为0时，调用此函数，返回实例最大值
// 参数c：*check.C，用于检查测试结果是否符合预期
func (e *IPAMSuite) TestGetMaxAllocate_GivenZeroMaxAllocate_WhenCalled_ThenReturnsInstanceMax(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.resource.Spec.IPAM.MaxAllocate = 0
	expectedInstanceMax := 50
	mockOps := &mockNetResourceOperations{
		onGetMaximumAllocatableIPv4: func() int {
			return expectedInstanceMax
		},
	}
	node.ops = mockOps

	// When
	result := node.getMaxAllocate()

	// Then
	c.Assert(result, check.Equals, expectedInstanceMax)
}

// TestGetMaxAllocate_GivenMaxAllocateExceedsInstanceMax_WhenCalled_ThenReturnsInstanceMax 参数c *check.C - 断言库的一个check类型的指针，用于检查测试结果是否符合预期。
func (e *IPAMSuite) TestGetMaxAllocate_GivenMaxAllocateExceedsInstanceMax_WhenCalled_ThenReturnsInstanceMax(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.resource.Spec.IPAM.MaxAllocate = 100
	expectedInstanceMax := 50
	mockOps := &mockNetResourceOperations{
		onGetMaximumAllocatableIPv4: func() int {
			return expectedInstanceMax
		},
	}
	node.ops = mockOps

	// When
	result := node.getMaxAllocate()

	// Then
	c.Assert(result, check.Equals, expectedInstanceMax)
}

// TestGetMaxAllocate_GivenValidMaxAllocate_WhenCalled_ThenReturnsMaxAllocate 参数c *check.C - 测试用例断言库的一个对象，用于检查函数返回值是否符合预期
// 参数node *IPAMNode - IP地址管理器节点的指针，包含了该节点的资源信息和操作接口
// 参数expected int - 预期的最大分配值，应与节点的MaxAllocate字段相同
// 参数mockOps *mockNetResourceOperations - 模拟网络资源操作的结构体，包含了获取最大可分配IPv4地址的方法
// 参数result int - 实际返回的最大分配值，应与预期值相同
// 返回值 check.Equals - 断言库中的一个方法，用于比较两个值是否相等，如果不相等则会引发错误
func (e *IPAMSuite) TestGetMaxAllocate_GivenValidMaxAllocate_WhenCalled_ThenReturnsMaxAllocate(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expected := 30
	node.resource.Spec.IPAM.MaxAllocate = expected
	mockOps := &mockNetResourceOperations{
		onGetMaximumAllocatableIPv4: func() int {
			return 50
		},
	}
	node.ops = mockOps

	// When
	result := node.getMaxAllocate()

	// Then
	c.Assert(result, check.Equals, expected)
}

// TestGetMaxIPBurstableIPCount_GivenNetResource_WhenCalled_ThenReturnsValue 函数测试GetMaxIPBurstableIPCount方法，给定一个netResource，当调用该方法时，返回值为预期的值。
func (e *IPAMSuite) TestGetMaxIPBurstableIPCount_GivenNetResource_WhenCalled_ThenReturnsValue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expected := 20
	mockOps := &mockNetResourceOperations{
		onGetMaximumBurstableAllocatableIPv4: func() int {
			return expected
		},
	}
	node.ops = mockOps

	// When
	result := node.getMaxIPBurstableIPCount()

	// Then
	c.Assert(result, check.Equals, expected)
}

// Test GetNeededAddresses method
func (e *IPAMSuite) TestGetNeededAddresses_GivenPositiveNeededIPs_WhenCalled_ThenReturnsNeededIPs(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.stats.NeededIPs = 5

	// When
	result := node.GetNeededAddresses()

	// Then
	c.Assert(result, check.Equals, 5)
}

// TestGetNeededAddresses_GivenZeroNeededIPsAndPositiveExcessIPs_WhenReleaseEnabled_ThenReturnsNegativeExcessIPs 功能：给定需要的IP数量为0，且有正整数个多余IP，当启用释放功能时，返回负数的多余IP数量
func (e *IPAMSuite) TestGetNeededAddresses_GivenZeroNeededIPsAndPositiveExcessIPs_WhenReleaseEnabled_ThenReturnsNegativeExcessIPs(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.stats.NeededIPs = 0
	node.stats.ExcessIPs = 3
	node.manager.releaseExcessIPs = true

	// When
	result := node.GetNeededAddresses()

	// Then
	c.Assert(result, check.Equals, -3)
}

// TestGetNeededAddresses_GivenZeroNeededIPsAndPositiveExcessIPs_WhenReleaseDisabled_ThenReturnsZero 函数接受一个check.C类型的参数c，用于断言测试结果
// 参数node是*IPAMSuite类型，表示要进行测试的节点对象
func (e *IPAMSuite) TestGetNeededAddresses_GivenZeroNeededIPsAndPositiveExcessIPs_WhenReleaseDisabled_ThenReturnsZero(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.stats.NeededIPs = 0
	node.stats.ExcessIPs = 3
	node.manager.releaseExcessIPs = false

	// When
	result := node.GetNeededAddresses()

	// Then
	c.Assert(result, check.Equals, 0)
}

// Test pool maintenance methods
func (e *IPAMSuite) TestRequirePoolMaintenance_GivenNetResource_WhenCalled_ThenSetsFlag(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.waitingForPoolMaintenance = false

	// When
	node.requirePoolMaintenance()

	// Then
	c.Assert(node.waitingForPoolMaintenance, check.Equals, true)
}

// TestPoolMaintenanceComplete_GivenNetResource_WhenCalled_ThenClearsFlag 参数：
//   - c *check.C: 测试用例断言库，必须传入
//
// 返回值：
//   - (none)<void>
func (e *IPAMSuite) TestPoolMaintenanceComplete_GivenNetResource_WhenCalled_ThenClearsFlag(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.waitingForPoolMaintenance = true

	// When
	node.poolMaintenanceComplete()

	// Then
	c.Assert(node.waitingForPoolMaintenance, check.Equals, false)
}

// Test InstanceID method
func (e *IPAMSuite) TestInstanceID_GivenNilResource_WhenCalled_ThenReturnsEmpty(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.resource = nil

	// When
	result := node.InstanceID()

	// Then
	c.Assert(result, check.Equals, "")
}

// Test allocationNeeded method
func (e *IPAMSuite) TestAllocationNeeded_GivenNoWaitingAndNoResyncNeededAndPositiveNeededIPs_WhenCalled_ThenReturnsTrue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.waitingForPoolMaintenance = false
	node.resyncNeeded = time.Time{}
	node.stats.NeededIPs = 5

	// When
	result := node.allocationNeeded()

	// Then
	c.Assert(result, check.Equals, true)
}

// TestAllocationNeeded_GivenWaitingForMaintenance_WhenCalled_ThenReturnsFalse 测试AllocationNeeded方法，给定等待维护的情况下，当被调用时，返回false
func (e *IPAMSuite) TestAllocationNeeded_GivenWaitingForMaintenance_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.waitingForPoolMaintenance = true
	node.resyncNeeded = time.Time{}
	node.stats.NeededIPs = 5

	// When
	result := node.allocationNeeded()

	// Then
	c.Assert(result, check.Equals, false)
}

// TestAllocationNeeded_GivenResyncNeeded_WhenCalled_ThenReturnsFalse 测试AllocationNeeded方法，给定resyncNeeded为true，当调用时，返回false
func (e *IPAMSuite) TestAllocationNeeded_GivenResyncNeeded_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.waitingForPoolMaintenance = false
	node.resyncNeeded = time.Now()
	node.stats.NeededIPs = 5

	// When
	result := node.allocationNeeded()

	// Then
	c.Assert(result, check.Equals, false)
}

func (e *IPAMSuite) TestAllocationNeeded_GivenZeroNeededIPs_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.waitingForPoolMaintenance = false
	node.resyncNeeded = time.Time{}
	node.stats.NeededIPs = 0

	// When
	result := node.allocationNeeded()

	// Then
	c.Assert(result, check.Equals, false)
}

// Test releaseNeeded method
func (e *IPAMSuite) TestReleaseNeeded_GivenConditionsMetAndPositiveExcessIPs_WhenCalled_ThenReturnsTrue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.manager.releaseExcessIPs = true
	node.waitingForPoolMaintenance = false
	node.resyncNeeded = time.Time{}
	node.stats.ExcessIPs = 5

	// When
	result := node.releaseNeeded()

	// Then
	c.Assert(result, check.Equals, true)
}

// TestReleaseNeeded_GivenReleaseDisabled_WhenCalled_ThenReturnsFalse 测试ReleaseNeeded_GivenReleaseDisabled_WhenCalled_Then返回false
// 参数c *check.C - go-check单元测试框架，用于断言
func (e *IPAMSuite) TestReleaseNeeded_GivenReleaseDisabled_WhenCalled_ThenReturnsFalse(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.manager.releaseExcessIPs = false
	node.waitingForPoolMaintenance = false
	node.resyncNeeded = time.Time{}
	node.stats.ExcessIPs = 5

	// When
	result := node.releaseNeeded()

	// Then
	c.Assert(result, check.Equals, false)
}

// TestReleaseNeeded_GivenReleaseIPsInProgress_WhenCalled_ThenReturnsTrue 测试ReleaseNeeded_给定正在进行的释放IPs_当被调用时_则返回true
func (e *IPAMSuite) TestReleaseNeeded_GivenReleaseIPsInProgress_WhenCalled_ThenReturnsTrue(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.manager.releaseExcessIPs = false
	node.resource.Status.IPAM.ReleaseIPs = map[string]ipamTypes.IPReleaseStatus{
		"192.168.1.10": ipamOption.IPAMMarkForRelease,
	}

	// When
	result := node.releaseNeeded()

	// Then
	c.Assert(result, check.Equals, true)
}

// Test Pool method
func (e *IPAMSuite) TestPool_GivenNetResource_WhenCalled_ThenReturnsCopyOfAvailable(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	expected := ipamTypes.AllocationMap{
		"192.168.1.10": ipamTypes.AllocationIP{Resource: "test-resource"},
		"192.168.1.11": ipamTypes.AllocationIP{Resource: "test-resource-2"},
	}
	node.available = expected

	// When
	result := node.Pool()

	// Then
	c.Assert(result, check.DeepEquals, expected)
	// Verify it's a copy
	result["192.168.1.12"] = ipamTypes.AllocationIP{Resource: "new-resource"}
	c.Assert(len(node.available), check.Equals, 2)
}

// Test ResourceCopy method
func (e *IPAMSuite) TestResourceCopy_GivenNetResource_WhenCalled_ThenReturnsDeepCopy(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	originalName := node.resource.Name

	// When
	result := node.ResourceCopy()

	// Then
	c.Assert(result.Name, check.Equals, originalName)
	// Verify it's a copy
	result.Name = "modified-name"
	c.Assert(node.resource.Name, check.Equals, originalName)
}

// Test recalculate method
func (e *IPAMSuite) TestRecalculate_GivenNoResource_WhenCalled_ThenReturnsEarly(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.resource = nil

	// When
	node.recalculate()

	// Then - Should not panic and stats should remain unchanged
	c.Assert(node.stats.UsedIPs, check.Equals, 0)
}

// TestRecalculate_GivenValidResource_WhenCalled_ThenUpdatesStats 参数c *check.C - 用于断言的check库提供的对象，表示一个测试结果
// 参数c *check.C - 用于断言的check库提供的对象，表示一个测试结果
func (e *IPAMSuite) TestRecalculate_GivenValidResource_WhenCalled_ThenUpdatesStats(c *check.C) {
	// Given
	node := createMockNetResource("test-node")

	// Set up used IPs
	node.resource.Status.IPAM.Used = ipamTypes.AllocationMap{
		"192.168.1.10": ipamTypes.AllocationIP{},
		"192.168.1.11": ipamTypes.AllocationIP{},
	}

	// Set up available IPs
	expectedAvailable := ipamTypes.AllocationMap{
		"192.168.1.10": ipamTypes.AllocationIP{},
		"192.168.1.11": ipamTypes.AllocationIP{},
		"192.168.1.12": ipamTypes.AllocationIP{},
	}

	mockOps := &mockNetResourceOperations{
		onResyncInterfacesAndIPs: func(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
			return expectedAvailable, nil
		},
	}
	node.ops = mockOps

	// When
	node.recalculate()

	// Then
	c.Assert(node.stats.UsedIPs, check.Equals, 2)
	c.Assert(node.stats.AvailableIPs, check.Equals, 3)
	c.Assert(node.available, check.DeepEquals, expectedAvailable)
}

// TestRecalculate_GivenResyncError_WhenCalled_ThenResetsStats 参数c *check.C - 用于断言的对象，类型为*check.C
// 参数node *IPAMNode - IPAM节点对象，类型为*IPAMNode
// 返回值 bool - 是否通过测试，类型为bool
func (e *IPAMSuite) TestRecalculate_GivenResyncError_WhenCalled_ThenResetsStats(c *check.C) {
	// Given
	node := createMockNetResource("test-node")

	mockOps := &mockNetResourceOperations{
		onResyncInterfacesAndIPs: func(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
			return nil, fmt.Errorf("resync failed")
		},
	}
	node.ops = mockOps

	// When
	node.recalculate()

	// Then
	c.Assert(node.stats.NeededIPs, check.Equals, 0)
	c.Assert(node.stats.ExcessIPs, check.Equals, 0)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenNilMaintenanceAction_WhenCalled_ThenReturnsNoMutation(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	ctx := context.Background()

	// When
	instanceMutated, err := node.handleIPRelease(ctx, &maintenanceAction{})

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenEmptyReleaseAction_WhenCalled_ThenReturnsNoMutation(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	ctx := context.Background()
	action := &maintenanceAction{}

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
	c.Assert(len(node.ipsMarkedForRelease), check.Equals, 0)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenNilReleaseInMaintenanceAction_WhenCalled_ThenReturnsNoMutation(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	ctx := context.Background()
	action := &maintenanceAction{release: nil}

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
	c.Assert(len(node.ipsMarkedForRelease), check.Equals, 0)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenNewIPsToRelease_WhenDelayNotElapsed_ThenMarksIPsWithTimestamp(c *check.C) {
	// Given
	operatorOption.Config.ExcessIPReleaseDelay = 60 // 60 seconds delay
	node := createMockNetResource("test-node")
	ctx := context.Background()
	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10", "192.168.1.11"},
			PoolID:       "test-pool",
		},
	}

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
	c.Assert(len(node.ipsMarkedForRelease), check.Equals, 2)
	c.Assert(node.ipsMarkedForRelease["192.168.1.10"], check.Not(check.Equals), time.Time{})
	c.Assert(node.ipsMarkedForRelease["192.168.1.11"], check.Not(check.Equals), time.Time{})
}

func (e *IPAMSuite) TestHandleIPRelease_GivenIPsMarkedForReleaseWithElapsedDelay_WhenCalled_ThenMarksIPsForRelease(c *check.C) {
	// Given
	operatorOption.Config.ExcessIPReleaseDelay = 1 // 1 second delay
	node := createMockNetResource("test-node")
	ctx := context.Background()

	// Pre-mark IPs with past timestamp
	pastTime := time.Now().Add(-2 * time.Second)
	node.ipsMarkedForRelease["192.168.1.10"] = pastTime
	node.ipsMarkedForRelease["192.168.1.11"] = pastTime

	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10", "192.168.1.11"},
			PoolID:       "test-pool",
		},
	}

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
	c.Assert(node.ipReleaseStatus["192.168.1.10"], check.Equals, ipamOption.IPAMMarkForRelease)
	c.Assert(node.ipReleaseStatus["192.168.1.11"], check.Equals, ipamOption.IPAMMarkForRelease)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenIPsReadyForRelease_WhenCalled_ThenReleasesIPs(c *check.C) {
	// Given
	operatorOption.Config.ExcessIPReleaseDelay = 1
	node := createMockNetResource("test-node")
	ctx := context.Background()

	// Setup IPs ready for release
	pastTime := time.Now().Add(-2 * time.Second)
	node.ipsMarkedForRelease["192.168.1.10"] = pastTime
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.10"] = ipamOption.IPAMReadyForRelease

	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10"},
			PoolID:       "test-pool",
		},
	}

	// Setup mock for successful release
	mockOps := &mockNetResourceOperations{
		onReleaseIPs: func(ctx context.Context, release *ReleaseAction) error {
			return nil
		},
	}
	node.ops = mockOps

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, true)
	c.Assert(node.ipReleaseStatus["192.168.1.10"], check.Equals, ipamOption.IPAMReleased)
	c.Assert(node.ipsMarkedForRelease["192.168.1.10"], check.Equals, time.Time{}) // Should be deleted
}

func (e *IPAMSuite) TestHandleIPRelease_GivenIPsDoNotRelease_WhenCalled_ThenRemovesFromMaps(c *check.C) {
	// Given
	operatorOption.Config.ExcessIPReleaseDelay = 1
	node := createMockNetResource("test-node")
	ctx := context.Background()

	// Setup IPs marked for do-not-release
	pastTime := time.Now().Add(-2 * time.Second)
	node.ipsMarkedForRelease["192.168.1.10"] = pastTime
	node.ipReleaseStatus["192.168.1.10"] = ipamOption.IPAMMarkForRelease
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.10"] = ipamOption.IPAMDoNotRelease

	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10"},
			PoolID:       "test-pool",
		},
	}

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
	_, existsInMarked := node.ipsMarkedForRelease["192.168.1.10"]
	c.Assert(existsInMarked, check.Equals, false)
	_, existsInStatus := node.ipReleaseStatus["192.168.1.10"]
	c.Assert(existsInStatus, check.Equals, false)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenIPReleaseFailure_WhenCalled_ThenReturnsError(c *check.C) {
	// Given
	operatorOption.Config.ExcessIPReleaseDelay = 1
	node := createMockNetResource("test-node")
	ctx := context.Background()

	// Setup IPs ready for release
	pastTime := time.Now().Add(-2 * time.Second)
	node.ipsMarkedForRelease["192.168.1.10"] = pastTime
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.10"] = ipamOption.IPAMReadyForRelease

	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10"},
			PoolID:       "test-pool",
		},
	}

	// Setup mock for failed release
	expectedError := fmt.Errorf("failed to release IP")
	mockOps := &mockNetResourceOperations{
		onReleaseIPs: func(ctx context.Context, release *ReleaseAction) error {
			return expectedError
		},
	}
	node.ops = mockOps

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.Equals, expectedError)
	c.Assert(instanceMutated, check.Equals, false)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenIPNotInCurrentReleaseList_WhenCalled_ThenRemovesFromMaps(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	ctx := context.Background()

	// Setup IP marked for release but not in current action
	node.ipsMarkedForRelease["192.168.1.99"] = time.Now()
	node.ipReleaseStatus["192.168.1.99"] = ipamOption.IPAMMarkForRelease

	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10"}, // Different IP
			PoolID:       "test-pool",
		},
	}

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
	_, existsInMarked := node.ipsMarkedForRelease["192.168.1.99"]
	c.Assert(existsInMarked, check.Equals, false)
	_, existsInStatus := node.ipReleaseStatus["192.168.1.99"]
	c.Assert(existsInStatus, check.Equals, false)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenMixedIPStates_WhenCalled_ThenHandlesCorrectly(c *check.C) {
	// Given
	operatorOption.Config.ExcessIPReleaseDelay = 1
	node := createMockNetResource("test-node")
	ctx := context.Background()

	pastTime := time.Now().Add(-2 * time.Second)

	// IP1: Ready for release
	node.ipsMarkedForRelease["192.168.1.10"] = pastTime
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.10"] = ipamOption.IPAMReadyForRelease

	// IP2: Do not release
	node.ipsMarkedForRelease["192.168.1.11"] = pastTime
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.11"] = ipamOption.IPAMDoNotRelease

	// IP3: New IP to be marked
	node.ipsMarkedForRelease["192.168.1.12"] = pastTime

	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10", "192.168.1.11", "192.168.1.12"},
			PoolID:       "test-pool",
		},
	}

	// Setup mock for successful release
	mockOps := &mockNetResourceOperations{
		onReleaseIPs: func(ctx context.Context, release *ReleaseAction) error {
			return nil
		},
	}
	node.ops = mockOps

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, true)

	// IP1 should be released
	c.Assert(node.ipReleaseStatus["192.168.1.10"], check.Equals, ipamOption.IPAMReleased)

	// IP2 should be removed from maps
	_, existsInMarked := node.ipsMarkedForRelease["192.168.1.11"]
	c.Assert(existsInMarked, check.Equals, false)
	_, existsInStatus := node.ipReleaseStatus["192.168.1.11"]
	c.Assert(existsInStatus, check.Equals, false)

	// IP3 should be marked for release
	c.Assert(node.ipReleaseStatus["192.168.1.12"], check.Equals, ipamOption.IPAMMarkForRelease)
}

func (e *IPAMSuite) TestHandleIPRelease_GivenMultipleIPsReadyForRelease_WhenCalled_ThenReleasesAllIPs(c *check.C) {
	// Given
	operatorOption.Config.ExcessIPReleaseDelay = 1
	node := createMockNetResource("test-node")
	ctx := context.Background()

	pastTime := time.Now().Add(-2 * time.Second)

	// Setup multiple IPs ready for release
	node.ipsMarkedForRelease["192.168.1.10"] = pastTime
	node.ipsMarkedForRelease["192.168.1.11"] = pastTime
	node.ipsMarkedForRelease["192.168.1.12"] = pastTime
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.10"] = ipamOption.IPAMReadyForRelease
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.11"] = ipamOption.IPAMReadyForRelease
	node.resource.Status.IPAM.ReleaseIPs["192.168.1.12"] = ipamOption.IPAMReadyForRelease

	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{"192.168.1.10", "192.168.1.11", "192.168.1.12"},
			PoolID:       "test-pool",
		},
	}

	// Setup mock for successful release
	releasedIPs := []string{}
	mockOps := &mockNetResourceOperations{
		onReleaseIPs: func(ctx context.Context, release *ReleaseAction) error {
			releasedIPs = release.IPsToRelease
			return nil
		},
	}
	node.ops = mockOps

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, true)
	c.Assert(len(releasedIPs), check.Equals, 3)

	// All IPs should be released
	for _, ip := range []string{"192.168.1.10", "192.168.1.11", "192.168.1.12"} {
		c.Assert(node.ipReleaseStatus[ip], check.Equals, ipamOption.IPAMReleased)
		_, existsInMarked := node.ipsMarkedForRelease[ip]
		c.Assert(existsInMarked, check.Equals, false)
	}
}

func (e *IPAMSuite) TestHandleIPRelease_GivenEmptyIPsMarkedForRelease_WhenCalled_ThenInitializesMap(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipsMarkedForRelease = nil // Set to nil to test initialization
	ctx := context.Background()
	action := &maintenanceAction{
		release: &ReleaseAction{
			IPsToRelease: []string{},
			PoolID:       "test-pool",
		},
	}

	// When
	instanceMutated, err := node.handleIPRelease(ctx, action)

	// Then
	c.Assert(err, check.IsNil)
	c.Assert(instanceMutated, check.Equals, false)
	c.Assert(node.ipsMarkedForRelease, check.Not(check.IsNil))
	c.Assert(len(node.ipsMarkedForRelease), check.Equals, 0)
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenEmptyIPReleaseStatus_WhenCalled_ThenSetsNodeReleaseIPsToNil(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = make(map[string]string)
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{
					"192.168.1.10": ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease),
				},
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(targetNode.Status.IPAM.ReleaseIPs, check.IsNil)
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenIPReleaseStatusWithMarkedForRelease_WhenCalled_ThenPopulatesNodeStatus(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = map[string]string{
		"192.168.1.10": ipamOption.IPAMMarkForRelease,
		"192.168.1.11": ipamOption.IPAMReadyForRelease,
		"192.168.1.12": ipamOption.IPAMDoNotRelease,
	}
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: make(map[string]ipamTypes.IPReleaseStatus),
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(len(targetNode.Status.IPAM.ReleaseIPs), check.Equals, 3)
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.10"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease))
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.11"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMReadyForRelease))
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.12"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMDoNotRelease))
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenExistingAgentResponse_WhenCalled_ThenRetainsAgentStatus(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = map[string]string{
		"192.168.1.10": ipamOption.IPAMMarkForRelease,
	}
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{
					"192.168.1.10": ipamTypes.IPReleaseStatus(ipamOption.IPAMReadyForRelease),
				},
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(len(targetNode.Status.IPAM.ReleaseIPs), check.Equals, 1)
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.10"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMReadyForRelease))
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenExistingAgentDoNotReleaseResponse_WhenCalled_ThenRetainsAgentStatus(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = map[string]string{
		"192.168.1.10": ipamOption.IPAMMarkForRelease,
	}
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{
					"192.168.1.10": ipamTypes.IPReleaseStatus(ipamOption.IPAMDoNotRelease),
				},
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(len(targetNode.Status.IPAM.ReleaseIPs), check.Equals, 1)
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.10"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMDoNotRelease))
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenMixedStatuses_WhenCalled_ThenHandlesCorrectly(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = map[string]string{
		"192.168.1.10": ipamOption.IPAMMarkForRelease,
		"192.168.1.11": ipamOption.IPAMReadyForRelease,
		"192.168.1.12": ipamOption.IPAMDoNotRelease,
		"192.168.1.13": ipamOption.IPAMReleased,
	}
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{
					"192.168.1.10": ipamTypes.IPReleaseStatus(ipamOption.IPAMReadyForRelease), // Agent responded
					"192.168.1.11": ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease),  // Agent hasn't responded yet
				},
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(len(targetNode.Status.IPAM.ReleaseIPs), check.Equals, 3)
	// IP1: Agent responded, should retain agent status
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.10"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMReadyForRelease))
	// IP2: Agent hasn't responded, should use local status
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.11"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMReadyForRelease))
	// IP3: No agent response, should use local status
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.12"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMDoNotRelease))
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenNonMarkedForReleaseStatus_WhenCalled_ThenUpdatesNodeStatus(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = map[string]string{
		"192.168.1.10": ipamOption.IPAMReadyForRelease,
	}
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{
					"192.168.1.10": ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease),
				},
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(len(targetNode.Status.IPAM.ReleaseIPs), check.Equals, 1)
	// Should update to local status since it's not marked-for-release
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.10"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMReadyForRelease))
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenNilIPReleaseStatus_WhenCalled_ThenHandlesGracefully(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = nil
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{
					"192.168.1.10": ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease),
				},
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(targetNode.Status.IPAM.ReleaseIPs, check.IsNil)
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenNilNodeStatus_WhenCalled_ThenHandlesGracefully(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = map[string]string{
		"192.168.1.10": ipamOption.IPAMMarkForRelease,
	}
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: nil,
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	c.Assert(len(targetNode.Status.IPAM.ReleaseIPs), check.Equals, 1)
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.10"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease))
}

func (e *IPAMSuite) TestPopulateIPReleaseStatus_GivenStaleReleasedIPs_WhenCalled_ThenRemovesStaleEntries(c *check.C) {
	// Given
	node := createMockNetResource("test-node")
	node.ipReleaseStatus = map[string]string{
		"192.168.1.10": ipamOption.IPAMReleased,
		"192.168.1.11": ipamOption.IPAMMarkForRelease,
	}
	// Set up resource with only one IP in ReleaseIPs (stale entry)
	node.resource.Status.IPAM.ReleaseIPs = map[string]ipamTypes.IPReleaseStatus{
		"192.168.1.11": ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease),
	}
	targetNode := &ccev2.NetResourceSet{
		Status: ccev2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				ReleaseIPs: make(map[string]ipamTypes.IPReleaseStatus),
			},
		},
	}

	// When
	node.PopulateIPReleaseStatus(targetNode)

	// Then
	// Stale released IP should be removed from local status
	_, exists := node.ipReleaseStatus["192.168.1.10"]
	c.Assert(exists, check.Equals, false)
	// Non-stale IP should remain
	c.Assert(node.ipReleaseStatus["192.168.1.11"], check.Equals, ipamOption.IPAMMarkForRelease)
	// Target node should have the remaining IP
	c.Assert(len(targetNode.Status.IPAM.ReleaseIPs), check.Equals, 1)
	c.Assert(targetNode.Status.IPAM.ReleaseIPs["192.168.1.11"], check.Equals, ipamTypes.IPReleaseStatus(ipamOption.IPAMMarkForRelease))
}
