package rdma

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma/client"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	fakev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/typed/cce.baidubce.com/v2/fake"
	v2listers "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
)

func TestRdmaInstancesManager_ForeachInstance_GivenERIHPASNode_WhenList_ThenUsesHPASID(t *testing.T) {
	ccemock.InitMockEnv()

	originalHPAS := operatorOption.Config.HPASStanderd
	operatorOption.Config.HPASStanderd = true
	defer func() { operatorOption.Config.HPASStanderd = originalHPAS }()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	manager := &rdmaInstancesManager{
		nodeMap:   make(map[string]*bceRDMANetResourceSet),
		eniLister: v2listers.NewENILister(indexer),
	}

	const (
		nodeName = "rdma-node"
		instance = "i-ordinary"
		hpasID   = "hpas-123456"
		mac      = "fa:20:20:1e:f4:e7"
		vif      = "elastic_rdma"
	)

	nrs := ccemock.NewMockSimpleNrs(nodeName, "bcc")
	nrs.Spec.InstanceID = instance
	nrs.Spec.InstanceType = "hpas"
	nrs.Spec.HPASID = hpasID
	nrs.Spec.ENI.InstanceType = string(ccev2.ENIForERI)
	nrs.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
		Kind: "Node",
		Name: nodeName,
	}}
	nrs.Annotations = map[string]string{
		k8s.AnnotationRDMAInfoMacAddress:  mac,
		k8s.AnnotationRDMAInfoVifFeatures: vif,
	}

	require.NoError(t, ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs}))

	manager.nodeMap[nodeName] = &bceRDMANetResourceSet{
		k8sObj: nrs,
	}

	labelSelectorValue := bceutils.GetRdmaNrsLabelSelectorValueFromNetResourceSetName(
		nrs.Name, nodeName, hpasID, mac, vif)

	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-rdma-test",
			Labels: map[string]string{
				k8s.LabelInstanceID: hpasID,
				k8s.LabelNodeName:   labelSelectorValue,
			},
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID: "eni-rdma-test",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse,
			CCEStatus: ccev2.ENIStatusReadyOnNode,
		},
	}
	require.NoError(t, indexer.Add(eni))

	var receivedInstanceID string
	var callbackCount int
	err := manager.ForeachInstance(instance, nodeName, func(instID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
		receivedInstanceID = instID
		callbackCount++
		resource, ok := iface.Resource.(*eniResource)
		require.True(t, ok)
		assert.Equal(t, "eni-rdma-test", resource.Spec.ENI.ID)
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, 1, callbackCount, "expected callback for single ENI")
	assert.Equal(t, hpasID, receivedInstanceID, "should iterate with HPAS effective ID")
}

func TestRdmaInstancesManager_ForeachInstance_GivenMissingNRS_WhenListing_ThenReturnError(t *testing.T) {
	ccemock.InitMockEnv()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	manager := &rdmaInstancesManager{
		nodeMap:   make(map[string]*bceRDMANetResourceSet),
		eniLister: v2listers.NewENILister(indexer),
	}

	const (
		nodeName = "rdma-node-missing"
		instance = "i-node"
	)

	nrs := ccemock.NewMockSimpleNrs(nodeName, "bcc")
	nrs.Spec.InstanceID = instance
	if nrs.Annotations == nil {
		nrs.Annotations = map[string]string{}
	}
	nrs.Annotations[k8s.AnnotationRDMAInfoMacAddress] = "fa:20:20:1e:f4:e7"
	nrs.Annotations[k8s.AnnotationRDMAInfoVifFeatures] = client.IntTypeERI

	manager.nodeMap[nodeName] = &bceRDMANetResourceSet{
		k8sObj: nrs,
	}

	iterCalled := false
	err := manager.ForeachInstance(instance, nodeName, func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
		iterCalled = true
		return nil
	})

	require.Error(t, err, "ForeachInstance should return error when NRS lookup fails")
	assert.True(t, kerrors.IsNotFound(err), "error should indicate that NRS is missing")
	assert.False(t, iterCalled, "interface iterator must not be invoked when NRS lookup fails")
}

func TestAllocateIPs_GivenEniWithLegacyInstanceID_WhenAllocating_ThenSyncInstanceMetadata(t *testing.T) {
	ccemock.InitMockEnv()

	originalIPv4 := operatorOption.Config.EnableIPv4
	originalIPv6 := operatorOption.Config.EnableIPv6
	operatorOption.Config.EnableIPv4 = true
	operatorOption.Config.EnableIPv6 = false
	defer func() {
		operatorOption.Config.EnableIPv4 = originalIPv4
		operatorOption.Config.EnableIPv6 = originalIPv6
	}()

	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Annotations: map[string]string{
				k8s.AnnotationRDMAInfoVifFeatures: client.IntTypeERI,
			},
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "inst-effective",
			ENI: &api.ENISpec{
				UseMode: string(ccev2.ENIUseModeSecondaryIP),
			},
		},
	}

	testENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-test",
			Labels: map[string]string{
				k8s.LabelInstanceID: "legacy-id",
			},
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:         "eni-test",
				InstanceID: "legacy-id",
				MacAddress: "00:11:22:33:44:55",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "10.0.0.5", Primary: true},
				},
			},
		},
	}

	mockLister := &MockENILister{
		GetFunc: func(name string) (*ccev2.ENI, error) {
			assert.Equal(t, "eni-test", name)
			return testENI.DeepCopy(), nil
		},
	}

	mockReal := &MockRealNetResourceSetInf{
		allocateIPsFunc: func(_ context.Context, _ *logrus.Entry, _ client.IaaSClient,
			allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) ([]*models.PrivateIP, []*models.PrivateIP, error) {
			return []*models.PrivateIP{
				{
					PrivateIPAddress: "10.0.0.10",
					SubnetID:         string(allocation.PoolID),
				},
			}, nil, nil
		},
	}

	manager := &rdmaInstancesManager{
		eniLister: mockLister,
		eriClient: client.NewEriClient(nil, nil),
		hpcClient: client.NewHpcClient(nil, nil),
	}

	n := &bceRDMANetResourceSet{
		k8sObj:                 nrs,
		manager:                manager,
		eniQuota:               &MockRdmaEniQuotaManagerHighLimit{},
		lastResyncEniQuotaTime: time.Now(),
		log:                    logging.DefaultLogger.WithField("test", "allocate-ips"),
		real:                   mockReal,
		eventRecorder:          record.NewFakeRecorder(5),
		expiredVPCVersion:      make(map[string]int64),
	}

	allocation := &ipam.AllocationAction{
		InterfaceID:                "eni-test",
		PoolID:                     ipamTypes.PoolID("subnet-1"),
		AvailableForAllocationIPv4: 1,
	}

	require.Equal(t, "inst-effective", n.k8sObj.Spec.InstanceID)
	effectiveID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	require.NoError(t, err)
	require.Equal(t, "inst-effective", effectiveID)

	assert.NoError(t, k8s.InitNewFakeK8sClient())

	updateCalled := false
	patch := gomonkey.ApplyMethod(reflect.TypeOf(k8s.CCEClient().CceV2().ENIs()), "Update",
		func(_ *fakev2.FakeENIs, ctx context.Context, eni *ccev2.ENI, opts metav1.UpdateOptions) (*ccev2.ENI, error) {
			updateCalled = true
			return eni, nil
		})
	defer patch.Reset()

	err = n.AllocateIPs(context.Background(), allocation)
	require.NoError(t, err)
	assert.True(t, updateCalled, "updateENIWithPoll should be called to persist metadata")
}

func TestAllocateIPs_GivenGetEffectiveInstanceIDFails_WhenAllocating_ThenReturnError(t *testing.T) {
	ccemock.InitMockEnv()

	originalIPv4 := operatorOption.Config.EnableIPv4
	originalIPv6 := operatorOption.Config.EnableIPv6
	operatorOption.Config.EnableIPv4 = true
	operatorOption.Config.EnableIPv6 = false
	defer func() {
		operatorOption.Config.EnableIPv4 = originalIPv4
		operatorOption.Config.EnableIPv6 = originalIPv6
	}()

	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Annotations: map[string]string{
				k8s.AnnotationRDMAInfoVifFeatures: client.IntTypeERI,
			},
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "inst-effective",
			ENI: &api.ENISpec{
				UseMode: string(ccev2.ENIUseModeSecondaryIP),
			},
		},
	}

	testENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-test",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID: "eni-test",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "10.0.0.5", Primary: true},
				},
			},
		},
	}

	mockLister := &MockENILister{
		GetFunc: func(name string) (*ccev2.ENI, error) {
			assert.Equal(t, "eni-test", name)
			return testENI.DeepCopy(), nil
		},
	}

	manager := &rdmaInstancesManager{
		eniLister: mockLister,
		eriClient: client.NewEriClient(nil, nil),
		hpcClient: client.NewHpcClient(nil, nil),
	}

	n := &bceRDMANetResourceSet{
		k8sObj:                 nrs,
		manager:                manager,
		eniQuota:               &MockRdmaEniQuotaManagerHighLimit{},
		lastResyncEniQuotaTime: time.Now(),
		log:                    logging.DefaultLogger.WithField("test", "allocate-ips-effective-id"),
		real:                   &MockRealNetResourceSetInf{},
		eventRecorder:          record.NewFakeRecorder(5),
		expiredVPCVersion:      make(map[string]int64),
	}

	allocation := &ipam.AllocationAction{
		InterfaceID:                "eni-test",
		PoolID:                     ipamTypes.PoolID("subnet-1"),
		AvailableForAllocationIPv4: 1,
	}

	patch := gomonkey.ApplyMethod(reflect.TypeOf(n.k8sObj), "GetEffectiveInstanceID",
		func(_ *ccev2.NetResourceSet, _ bool) (string, error) {
			return "", fmt.Errorf("mock effective id error")
		})
	defer patch.Reset()

	err := n.AllocateIPs(context.Background(), allocation)
	require.Error(t, err, "AllocateIPs should fail when GetEffectiveInstanceID returns error")
	assert.Contains(t, err.Error(), "get effective instance id failed")
}
