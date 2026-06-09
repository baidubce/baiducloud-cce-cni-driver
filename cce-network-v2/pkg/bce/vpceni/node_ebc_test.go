package vpceni

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
)

func TestNewEBCNetworkResourceSet(t *testing.T) {
	t.Run("nil super initializes embedded base", func(t *testing.T) {
		node := newEBCNetworkResourceSet(nil)

		assert.NotNil(t, node)
		assert.NotNil(t, node.bccNetworkResourceSet)
		assert.NotNil(t, node.bccNetworkResourceSet.bceNetworkResourceSet)
		assert.Equal(t, string(metadata.InstanceTypeExEBC), node.instanceType)
	})

	t.Run("super with nil base initializes base", func(t *testing.T) {
		super := &bccNetworkResourceSet{}
		node := newEBCNetworkResourceSet(super)

		assert.Equal(t, super, node.bccNetworkResourceSet)
		assert.NotNil(t, super.bceNetworkResourceSet)
		assert.Equal(t, string(metadata.InstanceTypeExEBC), node.instanceType)
	})

	t.Run("super with base keeps base", func(t *testing.T) {
		base := &bceNetworkResourceSet{}
		super := &bccNetworkResourceSet{bceNetworkResourceSet: base}
		node := newEBCNetworkResourceSet(super)

		assert.Equal(t, super, node.bccNetworkResourceSet)
		assert.Equal(t, base, super.bceNetworkResourceSet)
		assert.Equal(t, string(metadata.InstanceTypeExEBC), node.instanceType)
	})
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
