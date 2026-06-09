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

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	metricsmock "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/metrics/mock"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listerv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
)

var (
	k8sapi     = &k8sMock{}
	metricsapi = metricsmock.NewMockMetrics()
)

const testPoolID = ipamTypes.PoolID("global")

// init init 函数用于初始化 fake k8s 客户端，避免 nil 指针解引用
// 如果初始化失败则 panic 出错误信息
func init() {
	// 初始化 fake k8s 客户端，避免 nil 指针解引用
	if err := k8s.InitNewFakeK8sClient(); err != nil {
		panic(fmt.Sprintf("failed to initialize fake k8s client: %v", err))
	}
}

type allocationImplementationMock struct {
	// mutex protects all fields of this structure
	mutex        lock.RWMutex
	poolSize     int
	allocatedIPs int
	ipGenerator  int
}

func newAllocationImplementationMock() *allocationImplementationMock {
	return &allocationImplementationMock{poolSize: 2048}
}

func (a *allocationImplementationMock) CreateNetResource(obj *v2.NetResourceSet, node *NetResource) NetResourceOperations {
	return &nodeOperationsMock{allocator: a}
}

func (a *allocationImplementationMock) GetPoolQuota() ipamTypes.PoolQuotaMap {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return ipamTypes.PoolQuotaMap{
		testPoolID: ipamTypes.PoolQuota{AvailableIPs: a.poolSize - a.allocatedIPs},
	}
}

func (a *allocationImplementationMock) Resync(ctx context.Context) time.Time {
	return time.Now()
}

type nodeOperationsMock struct {
	allocator *allocationImplementationMock

	// mutex protects allocatedIPs
	mutex        lock.RWMutex
	allocatedIPs []string
}

func (n *nodeOperationsMock) GetUsedIPWithPrefixes() int {
	return len(n.allocatedIPs)
}

func (n *nodeOperationsMock) UpdatedNode(obj *v2.NetResourceSet) {}

func (n *nodeOperationsMock) PopulateStatusFields(resource *v2.NetResourceSet) {}

func (n *nodeOperationsMock) CreateInterface(ctx context.Context, allocation *AllocationAction, scopedLog *logrus.Entry) (int, string, error) {
	return 0, "operation not supported", fmt.Errorf("operation not supported")
}

func (n *nodeOperationsMock) ResyncInterfacesAndIPs(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
	available := ipamTypes.AllocationMap{}
	n.mutex.RLock()
	for _, ip := range n.allocatedIPs {
		available[ip] = ipamTypes.AllocationIP{}
	}
	n.mutex.RUnlock()
	return available, nil
}

func (n *nodeOperationsMock) PrepareIPAllocation(scopedLog *logrus.Entry) (*AllocationAction, error) {
	n.allocator.mutex.RLock()
	defer n.allocator.mutex.RUnlock()
	return &AllocationAction{
		PoolID:                     testPoolID,
		AvailableForAllocationIPv4: n.allocator.poolSize - n.allocator.allocatedIPs,
		AvailableInterfaces:        0,
	}, nil
}

func (n *nodeOperationsMock) AllocateIPs(ctx context.Context, allocation *AllocationAction) error {
	n.mutex.Lock()
	n.allocator.mutex.Lock()
	n.allocator.allocatedIPs += allocation.AvailableForAllocationIPv4
	for i := 0; i < allocation.AvailableForAllocationIPv4; i++ {
		n.allocator.ipGenerator++
		n.allocatedIPs = append(n.allocatedIPs, fmt.Sprintf("%d", n.allocator.ipGenerator))
	}
	n.allocator.mutex.Unlock()
	n.mutex.Unlock()
	return nil
}

func (n *nodeOperationsMock) PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ReleaseAction {
	n.mutex.RLock()
	excessIPs = math.IntMin(excessIPs, len(n.allocatedIPs))
	r := &ReleaseAction{PoolID: testPoolID}
	for i := 1; i <= excessIPs; i++ {
		// Release from the end of slice to avoid releasing used IPs
		releaseIndex := len(n.allocatedIPs) - (excessIPs + i - 1)
		r.IPsToRelease = append(r.IPsToRelease, n.allocatedIPs[releaseIndex])
	}
	n.mutex.RUnlock()
	return r
}

func (n *nodeOperationsMock) releaseIP(ip string) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.allocator.mutex.Lock()
	defer n.allocator.mutex.Unlock()
	for i, allocatedIP := range n.allocatedIPs {
		if allocatedIP == ip {
			n.allocatedIPs = append(n.allocatedIPs[:i], n.allocatedIPs[i+1:]...)
			n.allocator.allocatedIPs--
			return nil
		}
	}
	return fmt.Errorf("IP %s not found", ip)
}

func (n *nodeOperationsMock) ReleaseIPs(ctx context.Context, release *ReleaseAction) ([]string, error) {
	for _, ipToDelete := range release.IPsToRelease {
		if err := n.releaseIP(ipToDelete); err != nil {
			return nil, fmt.Errorf("unable to release IP %s: %s", ipToDelete, err)
		}
	}
	// Return all requested IPs as successfully released for testing
	return release.IPsToRelease, nil
}

func (n *nodeOperationsMock) GetMaximumAllocatableIPv4() int {
	return 0
}

func (n *nodeOperationsMock) GetMinimumAllocatableIPv4() int {
	return defaults.IPAMPreAllocation
}

func (n *nodeOperationsMock) IsPrefixDelegated() bool {
	return false
}

func (n *nodeOperationsMock) GetMaximumBurstableAllocatableIPv4() int {
	return 0
}

func (e *IPAMSuite) TestGetNodeNames(c *check.C) {
	am := newAllocationImplementationMock()
	c.Assert(am, check.Not(check.IsNil))
	mngr, err := NewNetResourceSetManager(am, k8sapi, metricsapi, 10, false, false)
	c.Assert(err, check.IsNil)
	c.Assert(mngr, check.Not(check.IsNil))

	mngr.Update(newNetResourceSet("node1", 0, 0, 0))

	names := mngr.GetNames()
	c.Assert(len(names), check.Equals, 1)
	c.Assert(names[0], check.Equals, "node1")

	mngr.Update(newNetResourceSet("node2", 0, 0, 0))

	names = mngr.GetNames()
	c.Assert(len(names), check.Equals, 2)

	mngr.Delete("node1")

	names = mngr.GetNames()
	c.Assert(len(names), check.Equals, 1)
	c.Assert(names[0], check.Equals, "node2")
}

func (e *IPAMSuite) TestNodeManagerGet(c *check.C) {
	am := newAllocationImplementationMock()
	c.Assert(am, check.Not(check.IsNil))
	mngr, err := NewNetResourceSetManager(am, k8sapi, metricsapi, 10, false, false)
	c.Assert(err, check.IsNil)
	c.Assert(mngr, check.Not(check.IsNil))

	// instances.Resync(context.TODO())

	mngr.Update(newNetResourceSet("node1", 0, 0, 0))

	c.Assert(mngr.Get("node1"), check.Not(check.IsNil))
	c.Assert(mngr.Get("node2"), check.IsNil)

	mngr.Delete("node1")
	c.Assert(mngr.Get("node1"), check.IsNil)
	c.Assert(mngr.Get("node2"), check.IsNil)
}

type k8sMock struct{}

func (k *k8sMock) Update(origNode, orig *v2.NetResourceSet) (*v2.NetResourceSet, error) {
	return nil, nil
}

func (k *k8sMock) UpdateStatus(origNode, node *v2.NetResourceSet) (*v2.NetResourceSet, error) {
	return nil, nil
}

func (k *k8sMock) Get(node string) (*v2.NetResourceSet, error) {
	return &v2.NetResourceSet{}, nil
}

func (k *k8sMock) Create(*v2.NetResourceSet) (*v2.NetResourceSet, error) {
	return &v2.NetResourceSet{}, nil
}

func (k *k8sMock) Lister() listerv2.NetResourceSetLister {
	return nil
}

func newNetResourceSet(node string, preAllocate, minAllocate, used int) *v2.NetResourceSet {
	cn := &v2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{Name: node, Namespace: "default"},
		Spec: v2.NetResourceSpec{
			IPAM: ipamTypes.IPAMSpec{
				Pool:        ipamTypes.AllocationMap{},
				PreAllocate: preAllocate,
				MinAllocate: minAllocate,
			},
		},
		Status: v2.NetResourceStatus{
			IPAM: ipamTypes.IPAMStatus{
				Used:       ipamTypes.AllocationMap{},
				ReleaseIPs: map[string]ipamTypes.IPReleaseStatus{},
			},
		},
	}

	updateNetResourceSet(cn, used)

	return cn
}

func updateNetResourceSet(cn *v2.NetResourceSet, used int) *v2.NetResourceSet {
	cn.Spec.IPAM.Pool = ipamTypes.AllocationMap{}
	for i := 1; i <= used; i++ {
		cn.Spec.IPAM.Pool[fmt.Sprintf("1.1.1.%d", i)] = ipamTypes.AllocationIP{Resource: "foo"}
	}

	cn.Status.IPAM.Used = ipamTypes.AllocationMap{}
	for ip, ipAllocation := range cn.Spec.IPAM.Pool {
		if used > 0 {
			delete(cn.Spec.IPAM.Pool, ip)
			cn.Status.IPAM.Used[ip] = ipAllocation
			used--
		}
	}

	return cn
}

func reachedAddressesNeeded(mngr *NetResourceSetManager, nodeName string, needed int) (success bool) {
	if node := mngr.Get(nodeName); node != nil {
		success = node.GetNeededAddresses() == needed
	}
	return
}

type nodeState struct {
	cn           *v2.NetResourceSet
	name         string
	instanceName string
}

func benchmarkAllocWorker(c *check.C, workers int64, delay time.Duration, rateLimit float64, burst int) {
	am := newAllocationImplementationMock()
	c.Assert(am, check.Not(check.IsNil))
	mngr, err := NewNetResourceSetManager(am, k8sapi, metricsapi, 10, false, false)
	c.Assert(err, check.IsNil)
	c.Assert(mngr, check.Not(check.IsNil))

	state := make([]*nodeState, c.N)

	c.ResetTimer()
	for i := range state {
		s := &nodeState{name: fmt.Sprintf("node%d", i), instanceName: fmt.Sprintf("i-benchmarkAllocWorker-%d", i)}
		s.cn = newNetResourceSet(s.name, 1, 10, 0)
		state[i] = s
		mngr.Update(s.cn)
	}

restart:
	for _, s := range state {
		if !reachedAddressesNeeded(mngr, s.name, 0) {
			time.Sleep(5 * time.Millisecond)
			goto restart
		}
	}
	c.StopTimer()

}

func (e *IPAMSuite) BenchmarkAllocDelay20Worker1(c *check.C) {
	benchmarkAllocWorker(c, 1, 20*time.Millisecond, 100.0, 4)
}
func (e *IPAMSuite) BenchmarkAllocDelay20Worker10(c *check.C) {
	benchmarkAllocWorker(c, 10, 20*time.Millisecond, 100.0, 4)
}
func (e *IPAMSuite) BenchmarkAllocDelay20Worker50(c *check.C) {
	benchmarkAllocWorker(c, 50, 20*time.Millisecond, 100.0, 4)
}
func (e *IPAMSuite) BenchmarkAllocDelay50Worker1(c *check.C) {
	benchmarkAllocWorker(c, 1, 50*time.Millisecond, 100.0, 4)
}
func (e *IPAMSuite) BenchmarkAllocDelay50Worker10(c *check.C) {
	benchmarkAllocWorker(c, 10, 50*time.Millisecond, 100.0, 4)
}
func (e *IPAMSuite) BenchmarkAllocDelay50Worker50(c *check.C) {
	benchmarkAllocWorker(c, 50, 50*time.Millisecond, 100.0, 4)
}

// TestResyncStatsInitialization_GivenEmptyStats_WhenInitialized_ThenMapsAreNotNil tests resyncStats initialization
func (e *IPAMSuite) TestResyncStatsInitialization_GivenEmptyStats_WhenInitialized_ThenMapsAreNotNil(c *check.C) {
	// Given: Create a NetResourceSetManager instance with required fields
	manager := &NetResourceSetManager{
		metricsAPI:      metricsapi,
		instancesAPI:    &mockInstancesAPI{},
		parallelWorkers: 1,
	}

	// When: Call Resync with empty context and current time
	// Then: Verify it doesn't panic due to nil map assignment
	ctx := context.Background()
	syncTime := time.Now()

	// This should not panic because we fixed the resyncStats initialization
	// to include both subnetUsedCounts and subnetAZs maps
	manager.Resync(ctx, syncTime)

	// If we reach here without panic, the test passes
	c.Assert(true, check.Equals, true)
}

// mockInstancesAPI is a mock implementation for testing
type mockInstancesAPI struct{}

func (m *mockInstancesAPI) CreateNetResource(obj *v2.NetResourceSet, node *NetResource) NetResourceOperations {
	return nil
}

func (m *mockInstancesAPI) GetPoolQuota() ipamTypes.PoolQuotaMap {
	return make(ipamTypes.PoolQuotaMap)
}

func (m *mockInstancesAPI) Resync(ctx context.Context) time.Time {
	return time.Now()
}
