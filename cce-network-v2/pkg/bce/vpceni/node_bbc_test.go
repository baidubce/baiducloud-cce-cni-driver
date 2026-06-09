package vpceni

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/baidubce/bce-sdk-go/services/vpc"
)

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

		allocation, err := node.PrepareIPAllocation(log)
		if assert.NoError(t, err) {
			assert.Equalf(t, allocation.AvailableInterfaces, 0, "should have available interfaces")
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
		ensureSubnetIDsInCache(t, node.k8sObj.Spec.ENI.VpcID, exceptSubnetIDs)

		err = node.refreshAvailableSubnets()
		if !assert.NoError(t, err) {
			return
		}
		allocation, err := node.PrepareIPAllocation(log)
		if assert.NoError(t, err) {
			assert.Equalf(t, allocation.AvailableInterfaces, 0, "should have available interfaces")
			assert.Equalf(t, 19, allocation.AvailableForAllocationIPv4, "should have available ips")
			assert.Equalf(t, "eni-bbcprimary", allocation.InterfaceID, "should have available interface")

			var subnetIDs []string
			for _, s := range node.availableSubnets {
				subnetIDs = append(subnetIDs, s.Name)
			}
			assert.Equalf(t, node.k8sObj.Spec.ENI.SubnetIDs, subnetIDs, "should subnet ids equal")
			assert.Equalf(t, node.k8sObj.Spec.ENI.SubnetIDs, exceptSubnetIDs, "should subnet ids equal user specified subnet")
		}
	})
}

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
		ensureSubnetIDsInCache(t, node.k8sObj.Spec.ENI.VpcID, exceptSubnetIDs)
		sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), exceptSubnetIDs[0], metav1.GetOptions{})
		if !assert.NoErrorf(t, err, "failed to get subnet") {
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

// Prepare BCC test context environment
// Includes initializing mock objects, saving them to clientgo cache, and returning a BCCNode instance
func bbcTestContext(t *testing.T) (*bceNetworkResourceSet, error) {
	ccemock.InitMockEnv()
	mockCtl := gomock.NewController(t)
	im := newMockInstancesManager(mockCtl)

	k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "bbc")
	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj})
	if !assert.NoError(t, err, "ensure nrs to informer failed") {
		return nil, err
	}

	k8sNode := ccemock.NewMockNodeFromNrs(k8sObj)
	ensureNodesInCache(t, []*corev1.Node{k8sNode})

	sbn := ccemock.NewMockSubnet("sbn-bbcprimary", "10.128.34.0/24")
	ensureSubnetsInCache(t, []*ccev1.Subnet{sbn})
	ensureSubnetIDsInCache(t, k8sObj.Spec.ENI.VpcID, k8sObj.Spec.ENI.SubnetIDs)

	vpcSbn := []vpc.Subnet{
		{
			SubnetId:    sbn.Name,
			Cidr:        "10.128.34.0/24",
			AvailableIp: 233,
		},
	}
	im.GetMockCloudInterface().EXPECT().
		ListSubnets(gomock.Any(), gomock.Any()).Return(&vpc.ListSubnetResult{
		Subnets: vpcSbn,
	}, nil).AnyTimes()

	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-bbcprimary",
			Labels: map[string]string{
				k8s.LabelInstanceID: k8sObj.Spec.InstanceID,
				k8s.LabelNodeName:   k8sNode.GetName(),
			},
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				SubnetID: "sbn-bbcprimary",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse,
			VPCStatusChangeLog: []ccev2.ENIStatusChange{
				{
					VPCStatus: ccev2.VPCENIStatusInuse,
					StatusChange: ccev2.StatusChange{
						Code: "ok",
						Time: metav1.Now(),
					},
				},
			},
			CCEStatus: ccev2.ENIStatusReadyOnNode,
			CCEStatusChangeLog: []ccev2.ENIStatusChange{
				{
					CCEENIStatus: ccev2.ENIStatusReadyOnNode,
					StatusChange: ccev2.StatusChange{
						Code: "ok",
						Time: metav1.Now(),
					},
				},
			},
		},
	}
	bbcEniDetails := newMockBBCEniWithMultipleIPs(k8sObj, sbn)
	for _, ip := range bbcEniDetails.PrivateIpSet {
		eni.Spec.PrivateIPSet = append(eni.Spec.PrivateIPSet, &models.PrivateIP{
			PrivateIPAddress: ip.PrivateIpAddress,
			Primary:          ip.Primary,
			SubnetID:         ip.SubnetId,
		})
	}
	enis := []*ccev2.ENI{eni}
	ccemock.EnsureEnisToInformer(t, enis)

	im.GetMockCloudInterface().EXPECT().
		GetBBCInstanceENI(gomock.Any(), gomock.Eq(k8sObj.InstanceID())).Return(bbcEniDetails, nil).AnyTimes()

	bcesync.InitBSM()
	node := NewBCENetworkResourceSet(nil, k8sObj, im)
	assert.NotNil(t, node)

	node.eniQuota = newCustomerIPQuota(log, k8s.Client(), k8sNode.Name, k8sObj.Spec.InstanceID, im.bceclientd)
	node.eniQuota.SetMaxENI(1)
	node.eniQuota.SetMaxIP(40)

	return node, nil
}

func ensureSubnetsInCache(t *testing.T, subnets []*ccev1.Subnet) {
	t.Helper()

	ctx := context.Background()
	informer := k8s.CCEClient().Informers.Cce().V1().Subnets().Informer()
	store := informer.GetStore()

	for _, sbn := range subnets {
		if sbn == nil {
			continue
		}
		if _, err := k8s.CCEClient().CceV1().Subnets().Get(ctx, sbn.Name, metav1.GetOptions{}); err != nil {
			if _, err := k8s.CCEClient().CceV1().Subnets().Create(ctx, sbn, metav1.CreateOptions{}); err != nil && !kerrors.IsAlreadyExists(err) {
				t.Fatalf("create subnet failed: %v", err)
			}
		}
		if err := store.Add(sbn); err != nil {
			if err := store.Update(sbn); err != nil {
				t.Fatalf("cache subnet failed: %v", err)
			}
		}
	}
}

func ensureNodesInCache(t *testing.T, nodes []*corev1.Node) {
	t.Helper()

	ctx := context.Background()
	informer := k8s.WatcherClient().Informers.Core().V1().Nodes().Informer()
	store := informer.GetStore()

	for _, node := range nodes {
		if node == nil {
			continue
		}
		if _, err := k8s.Client().CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{}); err != nil {
			if _, err := k8s.Client().CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{}); err != nil && !kerrors.IsAlreadyExists(err) {
				t.Fatalf("create node failed: %v", err)
			}
		}
		if err := store.Add(node); err != nil {
			if err := store.Update(node); err != nil {
				t.Fatalf("cache node failed: %v", err)
			}
		}
	}
}

func ensureSubnetIDsInCache(t *testing.T, vpcID string, subnetIDs []string) {
	t.Helper()

	var subnets []*ccev1.Subnet
	for i, subnetID := range subnetIDs {
		cidr := fmt.Sprintf("10.58.%d.0/24", i)
		sbn := ccemock.NewMockSubnet(subnetID, cidr)
		sbn.Spec.VPCID = vpcID
		subnets = append(subnets, sbn)
	}
	ensureSubnetsInCache(t, subnets)
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
