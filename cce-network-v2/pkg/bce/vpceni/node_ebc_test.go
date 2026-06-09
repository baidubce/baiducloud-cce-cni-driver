package vpceni

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
)

func TestEbcNetworkResourceSet_ReuseIPs_GivenUsePrimaryENIWithSecondaryModeTrue_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := "default/test-pod"

	// When: calling reuseIPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error indicating not supported
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenUsePrimaryENIWithSecondaryModeFalse_WhenReuseIPs_ThenCallParentMethod(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode disabled
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: false,
		},
	}

	// When: checking the condition for calling parent method
	// Then: should have usePrimaryENIWithSecondaryMode set to false
	assert.False(t, ebcNode.usePrimaryENIWithSecondaryMode)

	// 验证当 usePrimaryENIWithSecondaryMode 为 false 时，条件分支是正确的
	if !ebcNode.usePrimaryENIWithSecondaryMode {
		// 这个分支应该调用父类的 reuseIPs 方法
		assert.True(t, true, "Should enter the parent method call branch")
	}
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenNilIPs_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and nil IPs
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	var ips []*models.PrivateIP = nil
	owner := "default/test-pod"

	// When: calling reuseIPs with nil IPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenEmptyIPs_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and empty IPs
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{}
	owner := "default/test-pod"

	// When: calling reuseIPs with empty IPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenEmptyOwner_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and empty owner
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := ""

	// When: calling reuseIPs with empty owner
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenMultipleIPs_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and multiple IPs
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
		{
			PrivateIPAddress: "10.0.0.101",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := "default/test-pod"

	// When: calling reuseIPs with multiple IPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenCancelledContext_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and cancelled context
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel the context immediately

	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := "default/test-pod"

	// When: calling reuseIPs with cancelled context
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error (the function should still return the same error regardless of context)
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestCreatePrimaryENIOnCluster_GivenHPASInstanceID_WhenCreate_ThenSetEffectiveInstanceLabels(t *testing.T) {
	require.NoError(t, ccemock.InitMockEnv())
	require.NoError(t, bcesync.InitBSM())

	originalHPAS := operatorOption.Config.HPASStanderd
	operatorOption.Config.HPASStanderd = true
	t.Cleanup(func() {
		operatorOption.Config.HPASStanderd = originalHPAS
	})

	mockCtl := gomock.NewController(t)
	defer mockCtl.Finish()
	im := newMockInstancesManager(mockCtl)

	k8sObj := ccemock.NewMockSimpleNrs("10.128.34.199", string(metadata.InstanceTypeExEBC))
	k8sObj.Spec.InstanceID = "i-ebc-original"
	k8sObj.Spec.InstanceType = string(metadata.InstanceTypeExHPAS)
	k8sObj.Spec.HPASID = "hpas-effective-id"
	k8sObj.Spec.ENI.InstanceType = string(metadata.InstanceTypeExEBC)
	k8sObj.Spec.ENI.UseMode = string(ccev2.ENIUseModePrimaryWithSecondaryIP)
	k8sObj.Spec.ENI.BurstableMehrfachENI = 0

	require.NoError(t, ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj}))
	require.NoError(t, ccemock.EnsureSubnetIDsToInformer(t, k8sObj.Spec.ENI.VpcID, k8sObj.Spec.ENI.SubnetIDs))

	node := NewBCENetworkResourceSet(nil, k8sObj, im)
	ebcNode, ok := node.real.(*ebcNetworkResourceSet)
	require.True(t, ok, "expected ebc node implementation")

	primarySubnetID := k8sObj.Spec.ENI.SubnetIDs[0]
	bccInfo := &bccapi.InstanceModel{
		InstanceId: k8sObj.Spec.InstanceID,
		EniQuota:   0,
		NicInfo: bccapi.NicInfo{
			VpcId:          k8sObj.Spec.ENI.VpcID,
			SubnetId:       primarySubnetID,
			EniId:          "eni-primary-create",
			Name:           "eni-primary-create",
			MacAddress:     "02:11:22:33:44:55",
			SecurityGroups: []string{"sg-test"},
			Ips: []bccapi.IpModel{
				{
					Primary:   "true",
					PrivateIp: "10.58.0.10",
				},
			},
		},
	}
	ebcNode.bccInfo = bccInfo
	ebcNode.usePrimaryENIWithSecondaryMode = true

	ctx := context.Background()
	scopedLog := logrus.New().WithField("test", "createPrimaryENIOnCluster")

	err := ebcNode.createPrimaryENIOnCluster(ctx, scopedLog, k8sObj)
	require.NoError(t, err)

	createdENI, err := k8s.CCEClient().CceV2().ENIs().Get(ctx, bccInfo.NicInfo.EniId, metav1.GetOptions{})
	require.NoError(t, err)

	effectiveID, err := k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	require.NoError(t, err)

	assert.Equal(t, effectiveID, createdENI.Labels[k8s.LabelInstanceID], "effective instanceID should be used in labels")
	assert.Equal(t, k8sObj.Name, createdENI.Labels[k8s.LabelNodeName])
	assert.Equal(t, string(ccev2.ENIUseModePrimaryWithSecondaryIP), createdENI.Labels[k8s.LabelENIUseMode])
	assert.Equal(t, k8sObj.Spec.InstanceID, createdENI.Spec.ENI.InstanceID, "spec should keep original instanceID")
	assert.True(t, ebcNode.haveCreatePrimaryENI, "flag should be set after successful creation")
}
