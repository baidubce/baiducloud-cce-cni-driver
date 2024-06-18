package vpceni

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func Test_searchMaxAvailableSubnet(t *testing.T) {
	subnets := []*ccev1.Subnet{
		{
			Spec: ccev1.SubnetSpec{},
			Status: ccev1.SubnetStatus{
				AvailableIPNum: 100,
			},
		},
		{
			Spec: ccev1.SubnetSpec{
				Exclusive: true,
			},
			Status: ccev1.SubnetStatus{
				AvailableIPNum: 200,
			},
		},
		{
			Spec: ccev1.SubnetSpec{},
			Status: ccev1.SubnetStatus{
				AvailableIPNum: 300,
			},
		},
	}
	best := searchMaxAvailableSubnet(subnets)
	assert.NotNil(t, best)
	assert.Equal(t, 300, best.Status.AvailableIPNum)
}

func Test_bceNode_FilterAvailableSubnetIds(t *testing.T) {
	ccemock.InitMockEnv()

	n := &bceNode{
		availableSubnets: []*ccev1.Subnet{},
		k8sObj: &ccev2.NetResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "192.168.0.2",
			},
			Spec: ccev2.NetResourceSpec{
				InstanceID: "i-testbcc",
				ENI: &api.ENISpec{
					SubnetIDs:        []string{"sbn-abc", "sbn-def"},
					AvailabilityZone: "zoneD",
					InstanceType:     "BCC",
					UseMode:          string(ccev2.ENIUseModeSecondaryIP),
					VpcID:            "vpc-test",
				},
			},
		},
	}

	t.Run("no subnet ids", func(t *testing.T) {
		var subnetIDs []string
		result := n.FilterAvailableSubnetIds(subnetIDs)
		assert.Equal(t, []*ccev1.Subnet(nil), result)
	})

	t.Run("no available subnets", func(t *testing.T) {
		subnetIDs := []string{"a", "b"}
		result := n.FilterAvailableSubnetIds(subnetIDs)
		assert.Equal(t, []*ccev1.Subnet(nil), result)
	})

	t.Run("have available subnets", func(t *testing.T) {
		vpcID := n.k8sObj.Spec.ENI.VpcID
		subnetIDs := []string{"sbn-abc", "sbn-def", "sbn-ghi"}
		var (
			exceptSubnets []*ccev1.Subnet
		)

		for i, subnetID := range subnetIDs {
			sbn := ccemock.NewMockSubnet(subnetID, fmt.Sprintf("10.58.%d.0/24", i+10))
			sbn.Spec.VPCID = vpcID

			exceptSubnets = append(exceptSubnets, sbn)
		}
		ccemock.EnsureSubnetsToInformer(t, exceptSubnets)

		result := n.FilterAvailableSubnetIds(subnetIDs)
		assert.EqualValues(t, exceptSubnets, result)
	})
}

func TestNewNode(t *testing.T) {
	t.Run("test newBBCNode", func(t *testing.T) {
		ccemock.InitMockEnv()
		k8sObj := ccemock.NewMockSimpleNrs("10.128.34.57", "BBC")
		mockCtl := gomock.NewController(t)
		im := newMockInstancesManager(mockCtl)

		node := NewNode(nil, k8sObj, im)
		assert.NotNil(t, node)
		assert.Implements(t, new(realNodeInf), node.real)
	})
	t.Run("test newBCCNode", func(t *testing.T) {
		ccemock.InitMockEnv()
		mockCtl := gomock.NewController(t)
		im := newMockInstancesManager(mockCtl)

		k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "BCC")
		node := NewNode(nil, k8sObj, im)
		assert.NotNil(t, node)
		assert.Implements(t, new(realNodeInf), node.real)
	})
}

func TestPrepareIPAllocation(t *testing.T) {
	var (
		ctx = context.Background()
	)
	// 新建 ENI 申请 IP
	caseName := "test BCCNode need create new eni"
	t.Run(caseName, func(t *testing.T) {
		node, err := bccTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		logfield := log.WithField("caseName", caseName)
		allocation, err := node.PrepareIPAllocation(logfield)
		if assert.NoError(t, err) {
			assert.Greaterf(t, allocation.AvailableInterfaces, 0, "should have available interfaces")
			assert.Equalf(t, 0, allocation.AvailableForAllocationIPv4, "should have available ips")
			assert.Equalf(t, "", allocation.InterfaceID, "should have available interface")
		}
	})

	// 复用 ENI 申请 IP
	caseName = "test BCCNode need allocate ips by exsits eni"
	t.Run(caseName, func(t *testing.T) {
		node, err := bccTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		sbn, err := k8s.CCEClient().CceV1().Subnets().Get(ctx, node.k8sObj.Spec.ENI.SubnetIDs[0], metav1.GetOptions{})
		if !assert.NoError(t, err, "get subnet failed") {
			return
		}

		// add a exsits eni to node
		mockEni, err := ccemock.NewMockEni(node.k8sObj.Name, node.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 5)
		if !assert.NoError(t, err) {
			return
		}
		err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{mockEni})
		if !assert.NoError(t, err, "ensure enis to informer failed") {
			return
		}

		logfield := log.WithField("caseName", caseName)
		allocation, err := node.PrepareIPAllocation(logfield)
		if assert.NoError(t, err) {
			assert.Equalf(t, allocation.AvailableInterfaces, 7, "should have available interfaces")
			assert.Equalf(t, 11, allocation.AvailableForAllocationIPv4, "should have available ips")
			assert.Equalf(t, 0, allocation.AvailableForAllocationIPv6, "should have available ipv6 ips")
			assert.Equalf(t, mockEni.Name, allocation.InterfaceID, "should have available interface")
			assert.Equalf(t, mockEni.Spec.SubnetID, string(allocation.PoolID), "should have available pool id")
		}
	})
}

// 准备 BCC 测试上下文环境
// 包含初始化 mock 对象，保存到 clientgo缓存中，并返回 BCCNode 实例
func bccTestContext(t *testing.T) (*bceNode, error) {
	ccemock.InitMockEnv()

	mockCtl := gomock.NewController(t)
	im := newMockInstancesManager(mockCtl)

	k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "bcc")
	k8sObj.Annotations = map[string]string{}
	k8sObj.Annotations[k8s.AnnotationNodeMaxENINum] = "8"
	k8sObj.Annotations[k8s.AnnotationNodeMaxPerENIIPsNum] = "16"
	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj})
	if !assert.NoError(t, err, "ensure nrs to informer failed") {
		return nil, err
	}

	k8sNode := ccemock.NewMockNodeFromNrs(k8sObj)
	err = ccemock.EnsureNodeToInformer(t, []*corev1.Node{k8sNode})
	if !assert.NoError(t, err, "ensure node to informer failed") {
		return nil, err
	}

	im.GetMockCloudInterface().EXPECT().
		GetBCCInstanceDetail(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error) {
		return newMockBccInfo(k8sObj, 8), nil
	}).AnyTimes()

	node := NewNode(nil, k8sObj, im)
	assert.NotNil(t, node)

	node.lastResyncEniQuotaTime = time.Now()
	node.eniQuota = newCustomerIPQuota(log, k8s.Client(), nil, k8sObj.Spec.InstanceID, im.bceclient)
	node.eniQuota.SetMaxENI(8)
	node.eniQuota.SetMaxIP(16)

	if bn, ok := node.real.(*bccNode); ok {
		bn.bccInfo = newMockBccInfo(k8sObj, node.eniQuota.GetMaxENI())
	}

	ccemock.EnsureSubnetIDsToInformer(t, node.k8sObj.Spec.ENI.VpcID, node.k8sObj.Spec.ENI.SubnetIDs)
	return node, nil
}

func newMockBccInfo(k8sObj *ccev2.NetResourceSet, maxENINum int) *bccapi.InstanceModel {
	return &bccapi.InstanceModel{
		InstanceId:         k8sObj.Spec.InstanceID,
		Hostname:           k8sObj.Name,
		Spec:               "",
		Status:             bccapi.InstanceStatusRunning,
		EniQuota:           maxENINum,
		InternalIP:         k8sObj.Name,
		CpuCount:           32,
		MemoryCapacityInGB: 32,
		NicInfo: bccapi.NicInfo{
			VpcId:    k8sObj.Spec.ENI.VpcID,
			SubnetId: "sbn-primarysubnet",
			EniId:    "eni-primary",
		},
	}
}

func Test_bceNode_refreshAvailableSubnets(t *testing.T) {
	t.Run("use agent specific subnet", func(t *testing.T) {
		node, err := bccTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		err = node.refreshAvailableSubnets()
		if !assert.NoError(t, err, "refresh available subnets failed") {
			return
		}

		var subnetIDs []string
		for _, s := range node.availableSubnets {
			subnetIDs = append(subnetIDs, s.Name)
		}
		assert.Equalf(t, node.k8sObj.Spec.ENI.SubnetIDs, subnetIDs, "should subnet ids equal")
	})

	t.Run("use user annotaion specific subnet", func(t *testing.T) {
		node, err := bccTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		operatorOption.Config.EnableNodeAnnotationSync = true
		defer func() {
			operatorOption.Config.EnableNodeAnnotationSync = false
		}()
		node.k8sObj.Annotations[k8s.AnnotationNodeAnnotationSynced] = "true"
		node.k8sObj.Annotations[k8s.AnnotationNodeEniSubnetIDs] = "sbn-vxda1,sbn-vxda2"
		k8s.CCEClient().CceV2().NetResourceSets().Update(context.TODO(), node.k8sObj, metav1.UpdateOptions{})
		wait.PollImmediate(time.Microsecond, wait.ForeverTestTimeout, func() (bool, error) {
			obj, err := node.manager.nrsGetterUpdater.Get(node.k8sObj.Name)
			if err != nil {
				return false, err
			}
			if len(obj.Annotations) > 0 && obj.Annotations[k8s.AnnotationNodeAnnotationSynced] == "true" {
				return true, nil
			}
			return false, nil
		})

		exceptSubnetIDs := []string{"sbn-vxda1", "sbn-vxda2"}
		ccemock.EnsureSubnetIDsToInformer(t, node.k8sObj.Spec.ENI.VpcID, exceptSubnetIDs)

		err = node.refreshAvailableSubnets()
		if !assert.NoError(t, err, "refresh available subnets failed") {
			return
		}

		var subnetIDs []string
		for _, s := range node.availableSubnets {
			subnetIDs = append(subnetIDs, s.Name)
		}
		assert.Equalf(t, node.k8sObj.Spec.ENI.SubnetIDs, subnetIDs, "should subnet ids equal")
		assert.Equalf(t, node.k8sObj.Spec.ENI.SubnetIDs, exceptSubnetIDs, "should subnet ids equal user specified subnet")
	})

}
