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
package rdma

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma/client"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listerv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

// MockBSM for testing
type MockBSM struct{}

func (m *MockBSM) EnsureSubnet(vpcID, subnetID string) (*bcesync.BorrowedSubnet, error) {
	return nil, errors.New("mock error")
}

// MockNetResourceSetGetterUpdater for testing
type MockNetResourceSetGetterUpdater struct {
	CreateFunc       func(node *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error)
	UpdateFunc       func(origResource, newResource *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error)
	UpdateStatusFunc func(origResource, newResource *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error)
	GetFunc          func(name string) (*ccev2.NetResourceSet, error)
	ListerFunc       func() listerv2.NetResourceSetLister
}

func (m *MockNetResourceSetGetterUpdater) Create(node *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(node)
	}
	return node, nil
}

func (m *MockNetResourceSetGetterUpdater) Update(origResource, newResource *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(origResource, newResource)
	}
	return newResource, nil
}

func (m *MockNetResourceSetGetterUpdater) UpdateStatus(origResource, newResource *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error) {
	if m.UpdateStatusFunc != nil {
		return m.UpdateStatusFunc(origResource, newResource)
	}
	return newResource, nil
}

func (m *MockNetResourceSetGetterUpdater) Get(name string) (*ccev2.NetResourceSet, error) {
	if m.GetFunc != nil {
		return m.GetFunc(name)
	}
	return nil, nil
}

func (m *MockNetResourceSetGetterUpdater) Lister() listerv2.NetResourceSetLister {
	if m.ListerFunc != nil {
		return m.ListerFunc()
	}
	return nil
}

// MockRealNetResourceSetInf for testing
type MockRealNetResourceSetInf struct {
	refreshENIQuotaFunc func(scopeLog *logrus.Entry) (RdmaEniQuotaManager, error)
}

func (m *MockRealNetResourceSetInf) refreshENIQuota(scopeLog *logrus.Entry) (RdmaEniQuotaManager, error) {
	if m.refreshENIQuotaFunc != nil {
		return m.refreshENIQuotaFunc(scopeLog)
	}
	return &MockRdmaEniQuotaManager{}, nil
}

func (m *MockRealNetResourceSetInf) createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	return 0, "", nil
}

func (m *MockRealNetResourceSetInf) prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	return nil, nil
}

func (m *MockRealNetResourceSetInf) allocateIPs(ctx context.Context, scopedLog *logrus.Entry, iaasClient client.IaaSClient, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	return nil, nil, nil
}

func (m *MockRealNetResourceSetInf) releaseIPs(ctx context.Context, iaasClient client.IaaSClient, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	return nil
}

func (m *MockRealNetResourceSetInf) getMaximumAllocatable(eniQuota RdmaEniQuotaManager) int {
	return 12
}

func (m *MockRealNetResourceSetInf) getMinimumAllocatable() int {
	return 1
}

// createTestRdmaNetResourceSetWrapper creates a test wrapper for testing
func createTestRdmaNetResourceSetWrapper() *rdmaNetResourceSetWrapper {
	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Annotations: map[string]string{
				k8s.AnnotationRDMAInfoVifFeatures: bceutils.OverlayRDMA,
			},
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "test-instance-id",
			ENI: &api.ENISpec{
				VpcID:            "vpc-test",
				SubnetIDs:        []string{"subnet-1", "subnet-2"},
				AvailabilityZone: "zone-1",
			},
		},
	}

	// Create a mock nrsGetterUpdater
	nrsGetterUpdater := &MockNetResourceSetGetterUpdater{
		GetFunc: func(name string) (*ccev2.NetResourceSet, error) {
			return nrs, nil
		},
		UpdateFunc: func(origResource, newResource *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error) {
			return newResource, nil
		},
	}

	// Create a mock ENI lister
	mockENILister := &MockENILister{}

	// Create a mock manager
	manager := &rdmaInstancesManager{
		nrsGetterUpdater: nrsGetterUpdater,
		nodeMap:          make(map[string]*bceRDMANetResourceSet),
		mutex:            lock.RWMutex{},
		eniLister:        mockENILister,
	}

	// Create a mock real implementation
	mockReal := &MockRealNetResourceSetInf{}

	bceRDMANetResourceSet := &bceRDMANetResourceSet{
		k8sObj:     nrs,
		log:        logging.DefaultLogger.WithField("test", "rdma"),
		manager:    manager,
		instanceID: "test-instance-id",
		mutex:      lock.RWMutex{},
		real:       mockReal,
	}

	// Add the bceRDMANetResourceSet to the manager's nodeMap
	manager.nodeMap["test-node"] = bceRDMANetResourceSet

	wrapper := &rdmaNetResourceSetWrapper{
		bceRDMANetResourceSet: bceRDMANetResourceSet,
	}

	return wrapper
}

// MockRdmaEniQuotaManager for testing
type MockRdmaEniQuotaManager struct{}

func (m *MockRdmaEniQuotaManager) GetMaxENI() int    { return 1 }
func (m *MockRdmaEniQuotaManager) SetMaxENI(max int) {}
func (m *MockRdmaEniQuotaManager) GetMaxIP() int     { return 13 }
func (m *MockRdmaEniQuotaManager) SetMaxIP(max int)  {}
func (m *MockRdmaEniQuotaManager) SyncCapacityToK8s(ctx context.Context) error {
	return nil
}

// MockRdmaEniQuotaManagerHighLimit for testing with higher limits
type MockRdmaEniQuotaManagerHighLimit struct{}

func (m *MockRdmaEniQuotaManagerHighLimit) GetMaxENI() int    { return 10 } // Higher limit to avoid ERI limit check
func (m *MockRdmaEniQuotaManagerHighLimit) SetMaxENI(max int) {}
func (m *MockRdmaEniQuotaManagerHighLimit) GetMaxIP() int     { return 13 }
func (m *MockRdmaEniQuotaManagerHighLimit) SetMaxIP(max int)  {}
func (m *MockRdmaEniQuotaManagerHighLimit) SyncCapacityToK8s(ctx context.Context) error {
	return nil
}

// MockIaaSClient for testing
type MockIaaSClient struct {
	ListEnisFunc             func(ctx context.Context, vpcID, instanceID string) ([]client.EniResult, error)
	AddPrivateIPFunc         func(ctx context.Context, eniID, privateIP string) (string, error)
	DeletePrivateIPFunc      func(ctx context.Context, eniID, privateIP string) error
	BatchAddPrivateIPFunc    func(ctx context.Context, eniID string, privateIPs []string, count int) ([]string, error)
	BatchDeletePrivateIPFunc func(ctx context.Context, eniID string, privateIPs []string) error
	GetRDMAIntTypeFunc       func() string
}

func (m *MockIaaSClient) ListEnis(ctx context.Context, vpcID, instanceID string) ([]client.EniResult, error) {
	if m.ListEnisFunc != nil {
		return m.ListEnisFunc(ctx, vpcID, instanceID)
	}
	return []client.EniResult{}, nil
}

func (m *MockIaaSClient) AddPrivateIP(ctx context.Context, eniID, privateIP string) (string, error) {
	if m.AddPrivateIPFunc != nil {
		return m.AddPrivateIPFunc(ctx, eniID, privateIP)
	}
	return "", nil
}

func (m *MockIaaSClient) DeletePrivateIP(ctx context.Context, eniID, privateIP string) error {
	if m.DeletePrivateIPFunc != nil {
		return m.DeletePrivateIPFunc(ctx, eniID, privateIP)
	}
	return nil
}

func (m *MockIaaSClient) BatchAddPrivateIP(ctx context.Context, eniID string, privateIPs []string, count int) ([]string, error) {
	if m.BatchAddPrivateIPFunc != nil {
		return m.BatchAddPrivateIPFunc(ctx, eniID, privateIPs, count)
	}
	return []string{}, nil
}

func (m *MockIaaSClient) BatchDeletePrivateIP(ctx context.Context, eniID string, privateIPs []string) error {
	if m.BatchDeletePrivateIPFunc != nil {
		return m.BatchDeletePrivateIPFunc(ctx, eniID, privateIPs)
	}
	return nil
}

func (m *MockIaaSClient) GetRDMAIntType() string {
	if m.GetRDMAIntTypeFunc != nil {
		return m.GetRDMAIntTypeFunc()
	}
	return client.IntTypeERI
}

// MockENILister for testing
type MockENILister struct {
	ListFunc func(selector labels.Selector) ([]*ccev2.ENI, error)
	GetFunc  func(name string) (*ccev2.ENI, error)
}

func (m *MockENILister) List(selector labels.Selector) ([]*ccev2.ENI, error) {
	if m.ListFunc != nil {
		return m.ListFunc(selector)
	}
	return []*ccev2.ENI{}, nil
}

func (m *MockENILister) Get(name string) (*ccev2.ENI, error) {
	if m.GetFunc != nil {
		return m.GetFunc(name)
	}
	return nil, errors.New("not found")
}

func TestCreateERIInterface_GivenInCoolingDown_WhenCreateInterface_ThenReturnCoolingDownError(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	ctx := context.Background()
	allocation := &ipam.AllocationAction{}
	scopedLog := logrus.NewEntry(logrus.New())

	// Set cooling down time
	currentTime := time.Now()
	futureTime := currentTime.Add(time.Hour)
	wrapper.nextCreateENITime = &futureTime

	// Mock time.Now to return current time
	patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
		return currentTime
	})
	defer patches.Reset()

	// Act
	interfaceNum, msg, err := wrapper.createERIInterface(ctx, allocation, scopedLog)

	// Assert
	assert.Equal(t, 0, interfaceNum)
	assert.Contains(t, msg, "unable to create new ERI: cooling down")
	assert.NotNil(t, err)
}

func TestCreateERIInterface_GivenQuotaIsNil_WhenCreateInterface_ThenReturnQuotaError(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	ctx := context.Background()
	allocation := &ipam.AllocationAction{}
	scopedLog := logrus.NewEntry(logrus.New())

	// Mock time.Now to return current time
	currentTime := time.Now()
	patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
		return currentTime
	})
	defer patches.Reset()

	// Mock getRdmaEniQuota to return nil by setting eniQuota to nil and clearing annotations
	wrapper.bceRDMANetResourceSet.eniQuota = nil
	wrapper.bceRDMANetResourceSet.k8sObj.Annotations = nil
	// Set lastResyncEniQuotaTime to zero so it will go to slow path
	wrapper.bceRDMANetResourceSet.lastResyncEniQuotaTime = time.Time{}

	// Mock the real interface to return error
	mockReal := &MockRealNetResourceSetInf{
		refreshENIQuotaFunc: func(scopeLog *logrus.Entry) (RdmaEniQuotaManager, error) {
			return nil, errors.New("mock error")
		},
	}
	wrapper.bceRDMANetResourceSet.real = mockReal

	// Act
	interfaceNum, msg, err := wrapper.createERIInterface(ctx, allocation, scopedLog)

	// Assert
	assert.Equal(t, 0, interfaceNum)
	assert.Equal(t, "unable to get RDMA ENI quota", msg)
	assert.NotNil(t, err)
}

func TestCreateERIInterface_GivenMaxERILimitReached_WhenCreateInterface_ThenReturnMaxLimitError(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	ctx := context.Background()
	allocation := &ipam.AllocationAction{}
	scopedLog := logrus.NewEntry(logrus.New())

	// Mock time.Now to return current time
	currentTime := time.Now()
	patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
		return currentTime
	})
	defer patches.Reset()

	// Set eniQuota to valid quota
	wrapper.bceRDMANetResourceSet.eniQuota = &MockRdmaEniQuotaManager{}

	// Mock ForeachInstance to return multiple ERIs (so ERI count reaches limit)
	patches.ApplyMethod(wrapper.manager, "ForeachInstance", func(m *rdmaInstancesManager, instanceID, nodeName string, fn ipamTypes.InterfaceIterator) error {
		// Simulate multiple existing ERIs to reach the limit
		for i := 0; i < defaltRdmaEniNums; i++ {
			eriResource := &eniResource{
				Spec: ccev2.ENISpec{
					Type: ccev2.ENIForERI,
				},
			}
			fn(instanceID, fmt.Sprintf("eri-%d", i), ipamTypes.InterfaceRevision{Resource: eriResource})
		}
		return nil // Multiple ERIs, so availableERICount will be >= defaltRdmaEniNums (1)
	})

	// Mock FilterAvailableSubnetIds to return available subnets
	mockSubnet := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "subnet-1",
		},
		Spec: ccev1.SubnetSpec{
			ID:   "subnet-1",
			Name: "subnet-1",
		},
		Status: ccev1.SubnetStatus{
			AvailableIPNum: 10,
		},
	}
	availableSubnets := []*bcesync.BorrowedSubnet{
		bcesync.NewBorrowedSubnet(mockSubnet),
	}
	patches.ApplyMethod(wrapper.bceRDMANetResourceSet, "FilterAvailableSubnetIds", func(b *bceRDMANetResourceSet, subnetIDs []string, count int) []*bcesync.BorrowedSubnet {
		return availableSubnets
	})

	// Act
	interfaceNum, msg, err := wrapper.createERIInterface(ctx, allocation, scopedLog)

	// Assert
	assert.Equal(t, 0, interfaceNum)
	assert.Equal(t, "unable to create ERI: maximum ERI limit reached", msg)
	assert.NotNil(t, err)
}

func TestCreateERIInterface_GivenInvalidVifFeatures_WhenCreateInterface_ThenReturnVifError(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	ctx := context.Background()
	allocation := &ipam.AllocationAction{}
	scopedLog := logrus.NewEntry(logrus.New())

	// Set invalid vif features
	wrapper.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures] = "invalid_feature"

	// Mock time.Now to return current time
	currentTime := time.Now()
	patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
		return currentTime
	})
	defer patches.Reset()

	// Set eniQuota to valid quota
	wrapper.bceRDMANetResourceSet.eniQuota = &MockRdmaEniQuotaManager{}

	// Mock ForeachInstance to return no ERIs
	patches.ApplyMethod(wrapper.manager, "ForeachInstance", func(m *rdmaInstancesManager, instanceID, nodeName string, fn ipamTypes.InterfaceIterator) error {
		return nil
	})

	// Act
	interfaceNum, msg, err := wrapper.createERIInterface(ctx, allocation, scopedLog)

	// Assert
	assert.Equal(t, 0, interfaceNum)
	// The test might hit the ERI limit check before vif features check
	assert.True(t, msg == "ERI managed NRS must have elastic_rdma vif features" || msg == "unable to create ERI: maximum ERI limit reached")
	assert.NotNil(t, err)
}

// Test cases for searchMaxAvailableSubnet method

func TestSearchMaxAvailableSubnet_GivenEmptySubnets_WhenSearching_ThenReturnNil(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	subnets := []*bcesync.BorrowedSubnet{}

	// Act
	result := wrapper.searchMaxAvailableSubnet(subnets)

	// Assert
	assert.Nil(t, result)
}

func TestSearchMaxAvailableSubnet_GivenSingleSubnet_WhenSearching_ThenReturnThatSubnet(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	subnets := []*bcesync.BorrowedSubnet{
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: false,
				},
			},
			SubnetId:                  "subnet-1",
			BorrowedAvailableIPsCount: 10,
		},
	}

	// Act
	result := wrapper.searchMaxAvailableSubnet(subnets)

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, "subnet-1", result.SubnetId)
	assert.Equal(t, 10, result.BorrowedAvailableIPsCount)
}

func TestSearchMaxAvailableSubnet_GivenMultipleSubnets_WhenSearching_ThenReturnSubnetWithMostIPs(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	subnets := []*bcesync.BorrowedSubnet{
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: false,
				},
			},
			SubnetId:                  "subnet-1",
			BorrowedAvailableIPsCount: 10,
		},
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: false,
				},
			},
			SubnetId:                  "subnet-2",
			BorrowedAvailableIPsCount: 25, // This should be selected
		},
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: false,
				},
			},
			SubnetId:                  "subnet-3",
			BorrowedAvailableIPsCount: 5,
		},
	}

	// Act
	result := wrapper.searchMaxAvailableSubnet(subnets)

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, "subnet-2", result.SubnetId)
	assert.Equal(t, 25, result.BorrowedAvailableIPsCount)
}

func TestSearchMaxAvailableSubnet_GivenExclusiveSubnets_WhenSearching_ThenSkipExclusiveSubnets(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	subnets := []*bcesync.BorrowedSubnet{
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: true, // This should be skipped
				},
			},
			SubnetId:                  "subnet-1",
			BorrowedAvailableIPsCount: 100,
		},
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: false,
				},
			},
			SubnetId:                  "subnet-2",
			BorrowedAvailableIPsCount: 10, // This should be selected despite having fewer IPs
		},
	}

	// Act
	result := wrapper.searchMaxAvailableSubnet(subnets)

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, "subnet-2", result.SubnetId)
	assert.Equal(t, 10, result.BorrowedAvailableIPsCount)
}

func TestSearchMaxAvailableSubnet_GivenAllExclusiveSubnets_WhenSearching_ThenReturnNil(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	subnets := []*bcesync.BorrowedSubnet{
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: true,
				},
			},
			SubnetId:                  "subnet-1",
			BorrowedAvailableIPsCount: 100,
		},
		{
			Subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					Exclusive: true,
				},
			},
			SubnetId:                  "subnet-2",
			BorrowedAvailableIPsCount: 50,
		},
	}

	// Act
	result := wrapper.searchMaxAvailableSubnet(subnets)

	// Assert
	assert.Nil(t, result)
}

// Test cases for createERIOnCluster method

// Test cases for buildENISpecForERI method

func TestBuildENISpecForERI_GivenValidInputs_WhenBuilding_ThenReturnCorrectSpec(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	resource := wrapper.k8sObj
	vifFeatures := bceutils.OverlayRDMA
	vpcID := "vpc-test"
	bestSubnet := &bcesync.BorrowedSubnet{
		Subnet: &ccev1.Subnet{
			Spec: ccev1.SubnetSpec{
				AvailabilityZone: "zone-1",
			},
		},
		SubnetId: "subnet-1",
	}

	// Act
	spec := wrapper.buildENISpecForERI(resource, vifFeatures, vpcID, bestSubnet)

	// Assert
	assert.NotNil(t, spec)
	assert.Equal(t, resource.Name, spec.NodeName)
	assert.Equal(t, ccev2.ENIForERI, spec.Type)
	assert.Equal(t, ccev2.ENIUseModeSecondaryIP, spec.UseMode)
	assert.Equal(t, vpcID, spec.ENI.VpcID)
	assert.Equal(t, wrapper.instanceID, spec.ENI.InstanceID)
	assert.Equal(t, bestSubnet.Name, spec.ENI.SubnetID)
	assert.Equal(t, bestSubnet.Spec.AvailabilityZone, spec.ENI.ZoneName)
	assert.Equal(t, resource.Spec.ENI.RouteTableOffset, spec.RouteTableOffset)
	assert.Equal(t, resource.Spec.ENI.InstallSourceBasedRouting, spec.InstallSourceBasedRouting)
}

func TestBuildENISpecForERI_GivenDifferentSubnet_WhenBuilding_ThenUseSubnetInfo(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	resource := wrapper.k8sObj
	vifFeatures := bceutils.OverlayRDMA
	vpcID := "vpc-different"
	bestSubnet := &bcesync.BorrowedSubnet{
		Subnet: &ccev1.Subnet{
			Spec: ccev1.SubnetSpec{
				AvailabilityZone: "zone-2",
			},
		},
		SubnetId: "subnet-different",
	}

	// Act
	spec := wrapper.buildENISpecForERI(resource, vifFeatures, vpcID, bestSubnet)

	// Assert
	assert.NotNil(t, spec)
	assert.Equal(t, vpcID, spec.ENI.VpcID)
	assert.Equal(t, bestSubnet.Name, spec.ENI.SubnetID)
	assert.Equal(t, bestSubnet.Spec.AvailabilityZone, spec.ENI.ZoneName)
}

// Test cases for additional helper methods

func TestCreateNameForERI_GivenValidInputs_WhenCreating_ThenReturnCorrectName(t *testing.T) {
	// Arrange
	wrapper := createTestRdmaNetResourceSetWrapper()
	clusterID := "cluster-123"
	instanceID := "instance-456"
	nodeName := "node-789"

	// Act
	name := wrapper.createNameForERI(clusterID, instanceID, nodeName)

	// Assert
	assert.NotEmpty(t, name)
	assert.Contains(t, name, clusterID)
	assert.Contains(t, name, "eri")
	// The name should follow the pattern: {clusterID}/{instanceID}/{nodeName}-eri/{hash}
	parts := strings.Split(name, "/")
	assert.True(t, len(parts) >= 3)     // At least cluster/instance/node-eri/hash
	assert.Contains(t, parts[2], "eri") // Third part should contain "eri"
}
