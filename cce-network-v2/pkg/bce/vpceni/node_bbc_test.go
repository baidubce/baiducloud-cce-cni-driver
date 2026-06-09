package vpceni

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sutilnet "k8s.io/utils/net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/baidubce/bce-sdk-go/services/bbc"
)

// Test_bbcNode_prepareIPAllocation 测试bbcNode的prepareIPAllocation方法，包括使用主网卡和指定子网的场景。
// 该函数会在两个测试用例中运行：
// - 使用主网卡的场景，应该返回可用接口、可分配IP地址和接口ID等信息；
// - 使用用户指定子网的场景，应该根据用户指定的子网更新节点的可用子网列表，并返回可用接口、可分配IP地址和接口ID等信息。
func Test_bbcNode_prepareIPAllocation(t *testing.T) {
	t.Run("use primary subnet of bbc eni", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}
		operatorOption.Config.EnableNodeAnnotationSync = false

		_, err = k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}

		logfield := logging.DefaultLogger.WithField("component", "test").WithField("testcase", "use primary subnet of bbc eni")
		allocation, err := node.PrepareIPAllocation(logfield)
		if assert.NoError(t, err) {
			assert.Equalf(t, 0, allocation.AvailableInterfaces, "should have available interfaces")
			assert.Equalf(t, 19, allocation.AvailableForAllocationIPv4, "should have available ips")
			assert.Equalf(t, "eni-bbcprimary", allocation.InterfaceID, "should have available interface")
		}
	})

	t.Run("use user specifical subnet of bbc eni", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		// wait eni to be created
		eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}
		ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
			return []metav1.Object{
				eni,
			}
		})

		// update annotation to nrs
		operatorOption.Config.EnableNodeAnnotationSync = true
		defer func() {
			operatorOption.Config.EnableNodeAnnotationSync = false
		}()
		node.k8sObj.Annotations[k8s.AnnotationNodeAnnotationSynced] = "true"
		node.k8sObj.Annotations[k8s.AnnotationNodeEniSubnetIDs] = "sbn-vxda1,sbn-vxda2"
		k8s.CCEClient().CceV2().NetResourceSets().Update(context.TODO(), node.k8sObj, metav1.UpdateOptions{})
		wait.PollImmediate(time.Microsecond, 5*time.Second, func() (bool, error) {
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

		// Refresh available subnets to initialize n.availableSubnets
		err = node.refreshAvailableSubnets()
		if !assert.NoError(t, err) {
			return
		}
		logfield := logging.DefaultLogger.WithField("component", "test").WithField("testcase", "use user specifical subnet of bbc eni")
		allocation, err := node.PrepareIPAllocation(logfield)
		if assert.NoError(t, err) {
			assert.Equalf(t, 0, allocation.AvailableInterfaces, "should have available interfaces")
			assert.Equalf(t, 19, allocation.AvailableForAllocationIPv4, "should have available ips")
			assert.Equalf(t, "eni-bbcprimary", allocation.InterfaceID, "should have available interface")

			// 验证子网配置是否正确更新
			assert.Equalf(t, exceptSubnetIDs, node.k8sObj.Spec.ENI.SubnetIDs, "should subnet ids equal user specified subnet")
		}
	})
}

// Test_bbcNode_allocateIPs 测试bbcNode_allocateIPs函数，分别针对主网络和跨子网的情况进行测试。
// 该函数会调用mock接口来模拟云服务器的IP分配操作，并根据测试结果判定是否分配成功。
// 如果测试通过，则返回nil；如果测试失败，则返回错误信息。
func Test_bbcNode_allocateIPs(t *testing.T) {
	t.Run("allocateIP from primary subnet", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}
		operatorOption.Config.EnableNodeAnnotationSync = false

		// wait eni to be created
		eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}
		ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
			return []metav1.Object{
				eni,
			}
		})
		if !assert.Equalf(t, 21, len(eni.Spec.PrivateIPSet), "should have 21 ips") {
			return
		}

		sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), eni.Spec.SubnetID, metav1.GetOptions{})
		if !assert.NoErrorf(t, err, "failed to get subnet") {
			return
		}

		// mock bce openapi to allocate ip from bbc primary subnet
		mockInterface := node.manager.GetMockCloudInterface()
		first := mockInterface.EXPECT().
			BBCBatchAddIP(gomock.Any(), gomock.Eq(&bbc.BatchAddIpArgs{
				InstanceId:                     node.instanceID,
				SecondaryPrivateIpAddressCount: 10,
			})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 23, 10)}, nil).Times(1)
		second := mockInterface.EXPECT().BBCBatchAddIP(gomock.Any(), gomock.Eq(&bbc.BatchAddIpArgs{
			InstanceId:                     node.instanceID,
			SecondaryPrivateIpAddressCount: 1,
		})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 33, 1)}, nil).Times(1)
		gomock.InOrder(first, second)

		ctx := context.TODO()
		allocation := &ipam.AllocationAction{
			InterfaceID:                eni.Name,
			AvailableForAllocationIPv4: 11,
			PoolID:                     ipamTypes.PoolID(eni.Spec.SubnetID),
		}
		err = node.AllocateIPs(ctx, allocation)
		if assert.NoErrorf(t, err, "allocate ips failed") {
			assert.Equalf(t, 11, allocation.AvailableForAllocationIPv4, "should have available ips")
			eni, err = k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), eni.Name, metav1.GetOptions{})
			if !assert.NoError(t, err) {
				return
			}
			assert.Equalf(t, 32, len(eni.Spec.PrivateIPSet), "should have 32 secondary IPs")
			for i := range eni.Spec.PrivateIPSet {
				assert.Equalf(t, eni.Spec.PrivateIPSet[i].SubnetID, sbn.Name, "%s should use the primary subnet id", eni.Spec.PrivateIPSet[i].PrivateIPAddress)
			}
		}
	})

	t.Run("allocateIP cross subnet", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		// wait eni to be created
		eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}
		ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
			return []metav1.Object{
				eni,
			}
		})

		// update annotation to nrs
		operatorOption.Config.EnableNodeAnnotationSync = true
		defer func() {
			operatorOption.Config.EnableNodeAnnotationSync = false
		}()
		node.k8sObj.Annotations[k8s.AnnotationNodeAnnotationSynced] = "true"
		node.k8sObj.Annotations[k8s.AnnotationNodeEniSubnetIDs] = "sbn-vxda1,sbn-vxda2"
		k8s.CCEClient().CceV2().NetResourceSets().Update(context.TODO(), node.k8sObj, metav1.UpdateOptions{})
		wait.PollImmediate(time.Microsecond, 5*time.Second, func() (bool, error) {
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
		sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), exceptSubnetIDs[0], metav1.GetOptions{})
		if !assert.NoErrorf(t, err, "failed to get subnet") {
			return
		}

		// Refresh available subnets to initialize node.availableSubnets
		err = node.refreshAvailableSubnets()
		if !assert.NoError(t, err) {
			return
		}

		// mock bce openapi to allocate ip from bbc primary subnet
		mockInterface := node.manager.GetMockCloudInterface()
		first := mockInterface.EXPECT().
			BBCBatchAddIPCrossSubnet(gomock.Any(), gomock.Eq(&bbc.BatchAddIpCrossSubnetArgs{
				InstanceId: node.instanceID,
				SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
					{
						EniId:                          eni.Name,
						SubnetId:                       exceptSubnetIDs[0],
						SecondaryPrivateIpAddressCount: 10,
					},
				},
			})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 23, 10)}, nil).Times(1)
		second := mockInterface.EXPECT().BBCBatchAddIPCrossSubnet(gomock.Any(), gomock.Eq(&bbc.BatchAddIpCrossSubnetArgs{
			InstanceId: node.instanceID,
			SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
				{
					EniId:                          eni.Name,
					SubnetId:                       exceptSubnetIDs[0],
					SecondaryPrivateIpAddressCount: 1,
				},
			},
		})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 33, 1)}, nil).Times(1)
		gomock.InOrder(first, second)

		ctx := context.TODO()
		allocation := &ipam.AllocationAction{
			InterfaceID:                eni.Name,
			AvailableForAllocationIPv4: 11,
			PoolID:                     ipamTypes.PoolID(exceptSubnetIDs[0]),
		}
		err = node.AllocateIPs(ctx, allocation)
		if assert.NoErrorf(t, err, "allocate ips failed") {
			assert.Equalf(t, 11, allocation.AvailableForAllocationIPv4, "should have available ips")
			eni, err = k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), eni.Name, metav1.GetOptions{})
			if !assert.NoError(t, err) {
				return
			}
			assert.Equalf(t, 32, len(eni.Spec.PrivateIPSet), "should have 32 secondary IPs")
			for i := 21; i < len(eni.Spec.PrivateIPSet); i++ {
				assert.Equalf(t, exceptSubnetIDs[0], eni.Spec.PrivateIPSet[i].SubnetID, "%s should use the primary subnet id", eni.Spec.PrivateIPSet[i].PrivateIPAddress)
			}
		}
	})
}

// 准备 BCC 测试上下文环境
// 包含初始化 mock 对象，保存到 clientgo缓存中，并返回 BCCNode 实例
func bbcTestContext(t *testing.T) (*bbcNetworkResourceSet, error) {
	ccemock.InitMockEnv()
	mockCtl := gomock.NewController(t)
	im := newMockInstancesManager(mockCtl)

	k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "bbc")
	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj})
	if !assert.NoError(t, err, "ensure nrs to informer failed") {
		return nil, err
	}

	k8sNode := ccemock.NewMockNodeFromNrs(k8sObj)
	err = ccemock.EnsureNodeToInformer(t, []*corev1.Node{k8sNode})
	if !assert.NoError(t, err, "ensure node to informer failed") {
		return nil, err
	}

	sbn := ccemock.NewMockSubnet("sbn-bbcprimary", "10.128.34.0/24")
	err = ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return nil, err
	}
	ccemock.EnsureSubnetIDsToInformer(t, k8sObj.Spec.ENI.VpcID, k8sObj.Spec.ENI.SubnetIDs)

	// 创建 BBC ENI 对象
	bbcEniResult := newMockBBCEniWithMultipleIPs(k8sObj, sbn)
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: bbcEniResult.Id,
			Labels: map[string]string{
				k8s.LabelInstanceID: k8sObj.Spec.InstanceID,
				k8s.LabelNodeName:   k8sObj.Name,
			},
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:           bbcEniResult.Id,
				Name:         bbcEniResult.Name,
				InstanceID:   k8sObj.Spec.InstanceID,
				VpcID:        bbcEniResult.VpcId,
				ZoneName:     bbcEniResult.ZoneName,
				SubnetID:     bbcEniResult.SubnetId,
				MacAddress:   bbcEniResult.MacAddress,
				PrivateIPSet: []*models.PrivateIP{},
			},
			NodeName:                  k8sObj.Name,
			UseMode:                   ccev2.ENIUseModeSecondaryIP,
			RouteTableOffset:          127,
			InstallSourceBasedRouting: true,
			Type:                      ccev2.ENIForBBC,
		},
		Status: ccev2.ENIStatus{
			CCEStatus: ccev2.ENIStatusReadyOnNode,
			VPCStatus: ccev2.VPCENIStatusInuse,
		},
	}

	// 添加 IP 地址到 ENI
	for _, ip := range bbcEniResult.PrivateIpSet {
		privateIP := &models.PrivateIP{
			PrivateIPAddress: ip.PrivateIpAddress,
			Primary:          ip.Primary,
			SubnetID:         ip.SubnetId,
		}
		eni.Spec.ENI.PrivateIPSet = append(eni.Spec.ENI.PrivateIPSet, privateIP)
	}

	// 将 ENI 添加到 informer
	err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
		result, err := k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{})
		if err != nil {
			return nil
		}
		return []metav1.Object{result}
	})
	if !assert.NoError(t, err, "ensure eni to informer failed") {
		return nil, err
	}

	im.GetMockCloudInterface().EXPECT().
		GetBBCInstanceENI(gomock.Any(), gomock.Eq(k8sObj.InstanceID())).Return(bbcEniResult, nil).AnyTimes()

	bcesync.InitBSM()
	baseNode := NewBCENetworkResourceSet(nil, k8sObj, im)
	assert.NotNil(t, baseNode)

	baseNode.eniQuota = newCustomerIPQuota(log, k8s.Client(), k8sObj.Name, k8sObj.Spec.InstanceID, im.bceclientd)
	baseNode.eniQuota.SetMaxENI(1)
	baseNode.eniQuota.SetMaxIP(40)

	// Create BBC node wrapper
	node := newBBCNetworkResourceSet(baseNode)
	assert.NotNil(t, node)

	return node, nil
}

// newMockBBCEniWithMultipleIPs generates a mock BBC ENI instance with multiple IP addresses based on the given k8sObj and sbn.
//
// Parameters:
// k8sObj: a pointer to a ccev2.NetResourceSet, representing the Kubernetes network resource configuration object.
// sbn: a pointer to a ccev1.Subnet, representing the subnet configuration object.
//
// Returns:
// a pointer to a bbc.GetInstanceEniResult,
func newMockBBCEniWithMultipleIPs(k8sObj *ccev2.NetResourceSet, sbn *ccev1.Subnet) *bbc.GetInstanceEniResult {
	ips := allocateIPsFromSubnet(sbn, 2, 21)
	result := &bbc.GetInstanceEniResult{
		Id:         "eni-bbcprimary",
		Name:       "primary",
		ZoneName:   k8sObj.Spec.ENI.AvailabilityZone,
		VpcId:      k8sObj.Spec.ENI.VpcID,
		SubnetId:   sbn.Name,
		MacAddress: "02:16:3e:58:9a:4b",
		Status:     "inuse",
		PrivateIpSet: []bbc.PrivateIP{
			{
				PrivateIpAddress: ips[0],
				Primary:          true,
				SubnetId:         sbn.Name,
			},
		},
	}

	for i := 1; i < 21; i++ {
		result.PrivateIpSet = append(result.PrivateIpSet, bbc.PrivateIP{
			PrivateIpAddress: ips[i],
			Primary:          false,
			SubnetId:         sbn.Name,
		})
	}
	return result
}

// allocate count IPs from subnet
// start: 0-based index of subnet CIDR
func allocateIPsFromSubnet(sbn *ccev1.Subnet, start, count int) []string {
	var result []string
	sbnCIDR := cidr.MustParseCIDR(sbn.Spec.CIDR)
	baseIntIP := k8sutilnet.BigForIP(sbnCIDR.IP)

	for i := 0; i < count; i++ {
		result = append(result, k8sutilnet.AddIPOffset(baseIntIP, start+i).String())
	}
	return result
}
