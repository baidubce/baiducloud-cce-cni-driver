package vpceni

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	cloudtesting "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccefake "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/fake"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
)

type bccNodeTestEnv struct {
	base      *bceNetworkResourceSet
	node      *bccNetworkResourceSet
	manager   *InstancesManager
	mockCloud *cloudtesting.MockInterface
	k8sObj    *ccev2.NetResourceSet
}

type errorENILister struct {
	listErr error
	getErr  error
}

func (e *errorENILister) List(selector labels.Selector) ([]*ccev2.ENI, error) {
	if e.listErr != nil {
		return nil, e.listErr
	}
	return []*ccev2.ENI{}, nil
}

func (e *errorENILister) Get(name string) (*ccev2.ENI, error) {
	if e.getErr != nil {
		return nil, e.getErr
	}
	return nil, kerrors.NewNotFound(ccev2.Resource("eni"), name)
}

func setupBCCNodeTestEnv(t *testing.T) *bccNodeTestEnv {
	t.Helper()
	assert.NoError(t, ccemock.InitMockEnv())
	bcesync.InitBSM()

	mockCtl := gomock.NewController(t)
	manager := newMockInstancesManager(mockCtl)

	k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "bcc")
	if k8sObj.Annotations == nil {
		k8sObj.Annotations = map[string]string{}
	}
	k8sObj.Annotations[k8s.AnnotationNodeMaxENINum] = "8"
	k8sObj.Annotations[k8s.AnnotationNodeMaxPerENIIPsNum] = "16"
	k8sObj.Annotations[k8s.AnnotationIPResourceCapacitySynced] = time.Now().Format(time.RFC3339)
	k8sObj.Spec.ENI.MaxAllocateENI = 8
	k8sObj.Spec.ENI.MaxIPsPerENI = 16

	assert.NoError(t, ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj}))
	k8sNode := ccemock.NewMockNodeFromNrs(k8sObj)
	assert.NoError(t, ccemock.EnsureNodeToInformer(t, []*corev1.Node{k8sNode}))
	ensureSubnetIDsInCacheForIP(t, k8sObj.Spec.ENI.VpcID, k8sObj.Spec.ENI.SubnetIDs)

	base := NewBCENetworkResourceSet(nil, k8sObj, manager)
	if base.eniQuota == nil {
		base.eniQuota = newCustomerIPQuota(log, k8s.WatcherClient(), k8sObj.Name, k8sObj.Spec.InstanceID, manager.bceclientd)
		base.eniQuota.SetMaxENI(8)
		base.eniQuota.SetMaxIP(16)
	}
	base.lastResyncEniQuotaTime = time.Now()

	node := base.real.(*bccNetworkResourceSet)
	return &bccNodeTestEnv{
		base:      base,
		node:      node,
		manager:   manager,
		mockCloud: manager.GetMockCloudInterface(),
		k8sObj:    k8sObj,
	}
}

func setupBCCNodeTestEnvWithoutSubnets(t *testing.T) *bccNodeTestEnv {
	t.Helper()
	assert.NoError(t, ccemock.InitMockEnv())
	bcesync.InitBSM()

	mockCtl := gomock.NewController(t)
	manager := newMockInstancesManager(mockCtl)

	k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "bcc")
	if k8sObj.Annotations == nil {
		k8sObj.Annotations = map[string]string{}
	}
	k8sObj.Annotations[k8s.AnnotationNodeMaxENINum] = "8"
	k8sObj.Annotations[k8s.AnnotationNodeMaxPerENIIPsNum] = "16"
	k8sObj.Annotations[k8s.AnnotationIPResourceCapacitySynced] = time.Now().Format(time.RFC3339)
	k8sObj.Spec.ENI.MaxAllocateENI = 8
	k8sObj.Spec.ENI.MaxIPsPerENI = 16

	assert.NoError(t, ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj}))
	k8sNode := ccemock.NewMockNodeFromNrs(k8sObj)
	assert.NoError(t, ccemock.EnsureNodeToInformer(t, []*corev1.Node{k8sNode}))

	base := NewBCENetworkResourceSet(nil, k8sObj, manager)
	if base.eniQuota == nil {
		base.eniQuota = newCustomerIPQuota(log, k8s.WatcherClient(), k8sObj.Name, k8sObj.Spec.InstanceID, manager.bceclientd)
		base.eniQuota.SetMaxENI(8)
		base.eniQuota.SetMaxIP(16)
	}
	base.lastResyncEniQuotaTime = time.Now()

	node := base.real.(*bccNetworkResourceSet)
	return &bccNodeTestEnv{
		base:      base,
		node:      node,
		manager:   manager,
		mockCloud: manager.GetMockCloudInterface(),
		k8sObj:    k8sObj,
	}
}

func ensureSubnetsInCacheForIP(t *testing.T, subnets []*ccev1.Subnet) {
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

func ensureSubnetIDsInCacheForIP(t *testing.T, vpcID string, subnetIDs []string) {
	t.Helper()

	var subnets []*ccev1.Subnet
	for i, subnetID := range subnetIDs {
		cidr := fmt.Sprintf("10.58.%d.0/24", i)
		sbn := ccemock.NewMockSubnet(subnetID, cidr)
		sbn.Spec.VPCID = vpcID
		subnets = append(subnets, sbn)
	}
	ensureSubnetsInCacheForIP(t, subnets)
}

func TestBCCNetworkResourceSetPrepareIPAllocation(t *testing.T) {
	t.Run("secondary eni mode returns available ips", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = false
		env.node.bccInfo = newMockBccInfo(env.k8sObj, 1)

		quota := newCustomerIPQuota(log, k8s.WatcherClient(), env.k8sObj.Name, env.k8sObj.Spec.InstanceID, env.manager.bceclientd)
		quota.SetMaxENI(1)
		quota.SetMaxIP(4)
		env.base.eniQuota = quota
		env.base.lastResyncEniQuotaTime = time.Now()

		sbn := ccemock.NewMockSubnet("sbn-test", "10.0.0.0/24")
		sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
		assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

		eni, err := ccemock.NewMockEni(env.k8sObj.Name, env.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 1)
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni}))

		action, err := env.node.prepareIPAllocation(env.node.log.WithField("test", "secondary"))
		if assert.NoError(t, err) {
			assert.Equal(t, eni.Spec.ENI.ID, action.InterfaceID)
			assert.Equal(t, 0, action.AvailableInterfaces)
			assert.Equal(t, 3, action.AvailableForAllocationIPv4)
		}
	})

	t.Run("primary eni mode sets available interface when none exists", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.bccInfo = &bccapi.InstanceModel{
			InstanceId: env.k8sObj.Spec.InstanceID,
			NicInfo: bccapi.NicInfo{
				VpcId:    env.k8sObj.Spec.ENI.VpcID,
				SubnetId: "sbn-primarysubnet",
				EniId:    "eni-primary",
			},
		}

		quota := newCustomerIPQuota(log, k8s.WatcherClient(), env.k8sObj.Name, env.k8sObj.Spec.InstanceID, env.manager.bceclientd)
		quota.SetMaxENI(0)
		quota.SetMaxIP(16)
		env.base.eniQuota = quota
		env.base.lastResyncEniQuotaTime = time.Now()

		sbn := ccemock.NewMockSubnet("sbn-primarysubnet", "10.0.1.0/24")
		sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
		assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

		action, err := env.node.prepareIPAllocation(env.node.log.WithField("test", "primary-no-eni"))
		if assert.NoError(t, err) {
			assert.Equal(t, 1, action.AvailableInterfaces)
			assert.Equal(t, "", action.InterfaceID)
			assert.False(t, env.node.haveCreatePrimaryENI)
			assert.Equal(t, "sbn-primarysubnet", env.node.primaryENISubnetID)
			assert.NotEmpty(t, env.node.availableSubnets)
		}
	})

	t.Run("primary eni mode marks existing primary eni", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.bccInfo = &bccapi.InstanceModel{
			InstanceId: env.k8sObj.Spec.InstanceID,
			NicInfo: bccapi.NicInfo{
				VpcId:    env.k8sObj.Spec.ENI.VpcID,
				SubnetId: "sbn-primarysubnet",
				EniId:    "eni-primary",
			},
		}

		quota := newCustomerIPQuota(log, k8s.WatcherClient(), env.k8sObj.Name, env.k8sObj.Spec.InstanceID, env.manager.bceclientd)
		quota.SetMaxENI(1)
		quota.SetMaxIP(1)
		env.base.eniQuota = quota
		env.base.lastResyncEniQuotaTime = time.Now()

		sbn := ccemock.NewMockSubnet("sbn-primarysubnet", "10.0.2.0/24")
		sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
		assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

		eni, err := ccemock.NewMockEni(env.k8sObj.Name, env.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 1)
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni}))

		action, err := env.node.prepareIPAllocation(env.node.log.WithField("test", "primary-existing-eni"))
		if assert.NoError(t, err) {
			assert.Equal(t, 0, action.AvailableInterfaces)
			assert.Equal(t, "", action.InterfaceID)
			assert.True(t, env.node.haveCreatePrimaryENI)
		}
	})
}

func TestBCCNetworkResourceSetRefreshPrimarySubnet(t *testing.T) {
	t.Run("skip when not primary eni mode", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = false
		env.node.primaryENISubnetID = "keep"

		err := env.node.refreshPrimarySubnet()
		assert.NoError(t, err)
		assert.Equal(t, "keep", env.node.primaryENISubnetID)
	})

	t.Run("refreshes primary subnet and available subnets", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.bccInfo = &bccapi.InstanceModel{
			InstanceId: env.k8sObj.Spec.InstanceID,
			NicInfo: bccapi.NicInfo{
				VpcId:    env.k8sObj.Spec.ENI.VpcID,
				SubnetId: "sbn-primary",
				EniId:    "eni-primary",
			},
		}

		sbn := ccemock.NewMockSubnet("sbn-primary", "10.0.3.0/24")
		sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
		assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

		err := env.node.refreshPrimarySubnet()
		if assert.NoError(t, err) {
			assert.Equal(t, "sbn-primary", env.node.primaryENISubnetID)
			assert.Len(t, env.node.availableSubnets, 1)
			assert.Equal(t, "sbn-primary", env.node.availableSubnets[0].Name)
		}
	})
}

func TestBCCNetworkResourceSetCreatePrimaryENIOnCluster(t *testing.T) {
	env := setupBCCNodeTestEnv(t)
	env.node.usePrimaryENIWithSecondaryMode = true

	env.node.bccInfo = &bccapi.InstanceModel{
		InstanceId: env.k8sObj.Spec.InstanceID,
		NicInfo: bccapi.NicInfo{
			VpcId:          env.k8sObj.Spec.ENI.VpcID,
			SubnetId:       "sbn-primary",
			EniId:          "eni-primary",
			Name:           "primary",
			MacAddress:     "02:16:3e:58:9a:4b",
			SecurityGroups: []string{"sg-test"},
			Ips: []bccapi.IpModel{
				{PrivateIp: "10.0.3.10", Primary: "true"},
				{PrivateIp: "10.0.3.11", Primary: "false"},
			},
		},
	}

	sbn := ccemock.NewMockSubnet("sbn-primary", "10.0.3.0/24")
	sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
	assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

	resource, err := k8s.CCEClient().CceV2().NetResourceSets().Get(context.Background(), env.k8sObj.Name, metav1.GetOptions{})
	if !assert.NoError(t, err) {
		return
	}

	err = env.node.createPrimaryENIOnCluster(context.Background(), env.node.log.WithField("test", "create-primary"), resource)
	if !assert.NoError(t, err) {
		return
	}

	assert.True(t, env.node.haveCreatePrimaryENI)

	created, err := k8s.CCEClient().CceV2().ENIs().Get(context.Background(), "eni-primary", metav1.GetOptions{})
	if assert.NoError(t, err) {
		assert.Equal(t, string(ccev2.ENIUseModePrimaryWithSecondaryIP), created.Labels[k8s.LabelENIUseMode])
		assert.Equal(t, ccev2.ENIUseModePrimaryWithSecondaryIP, created.Spec.UseMode)
		assert.Equal(t, "sbn-primary", created.Spec.ENI.SubnetID)
		assert.Len(t, created.Spec.ENI.PrivateIPSet, 2)
		assert.Equal(t, ccev2.VPCENIStatusInuse, created.Status.VPCStatus)
		assert.Contains(t, created.Finalizers, bcesync.FinalizerENI)
	}
}

func TestBCCNetworkResourceSetAllocateIPsPrimaryENIMode(t *testing.T) {
	env := setupBCCNodeTestEnv(t)
	allocation := &ipam.AllocationAction{
		InterfaceID: "eni-primary",
		PoolID:      ipamTypes.PoolID("sbn-primary"),
	}

	env.mockCloud.EXPECT().
		BCCBatchAddIP(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, args *bccapi.BatchAddIpArgs) (*bccapi.BatchAddIpResponse, error) {
			assert.Equal(t, env.k8sObj.Spec.InstanceID, args.InstanceId)
			assert.Equal(t, 2, args.SecondaryPrivateIpAddressCount)
			assert.True(t, args.AllocateMultiIpv6Addr)
			return &bccapi.BatchAddIpResponse{PrivateIps: []string{"10.0.0.10", "fd00::1"}}, nil
		})

	ipv4Set, ipv6Set, err := env.node.allocateIPsPrimaryENIMode(context.Background(), logrus.NewEntry(logrus.New()), allocation, 2, 1)
	if assert.NoError(t, err) {
		assert.Len(t, ipv4Set, 1)
		assert.Equal(t, "10.0.0.10", ipv4Set[0].PrivateIPAddress)
		assert.Equal(t, "sbn-primary", ipv4Set[0].SubnetID)
		assert.Len(t, ipv6Set, 1)
		assert.Equal(t, "fd00::1", ipv6Set[0].PrivateIPAddress)
		assert.Equal(t, "sbn-primary", ipv6Set[0].SubnetID)
	}
}

func TestBCCNetworkResourceSetAllocateIPs(t *testing.T) {
	t.Run("primary eni mode uses instance api", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true

		allocation := &ipam.AllocationAction{
			InterfaceID: "eni-primary",
			PoolID:      ipamTypes.PoolID("sbn-primary"),
		}

		env.mockCloud.EXPECT().
			BCCBatchAddIP(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, args *bccapi.BatchAddIpArgs) (*bccapi.BatchAddIpResponse, error) {
				assert.Equal(t, env.k8sObj.Spec.InstanceID, args.InstanceId)
				assert.Equal(t, 1, args.SecondaryPrivateIpAddressCount)
				assert.False(t, args.AllocateMultiIpv6Addr)
				return &bccapi.BatchAddIpResponse{PrivateIps: []string{"10.0.0.20"}}, nil
			})

		ipv4Set, ipv6Set, err := env.node.allocateIPs(context.Background(), logrus.NewEntry(logrus.New()), allocation, 1, 0)
		if assert.NoError(t, err) {
			assert.Len(t, ipv4Set, 1)
			assert.Equal(t, "10.0.0.20", ipv4Set[0].PrivateIPAddress)
			assert.Len(t, ipv6Set, 0)
		}
	})

	t.Run("secondary eni mode uses eni api", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = false

		allocation := &ipam.AllocationAction{
			InterfaceID: "eni-secondary",
			PoolID:      ipamTypes.PoolID("sbn-secondary"),
		}

		env.mockCloud.EXPECT().
			BatchAddPrivateIPWithClientToken(gomock.Any(), gomock.Any(), gomock.Eq(1), gomock.Eq("eni-secondary"), gomock.Eq(false), gomock.Any()).
			DoAndReturn(func(ctx context.Context, ips []string, count int, eniID string, isIPv6 bool, clientToken string) ([]string, error) {
				assert.NotEmpty(t, clientToken)
				return []string{"10.0.0.21"}, nil
			})
		env.mockCloud.EXPECT().
			BatchAddPrivateIPWithClientToken(gomock.Any(), gomock.Any(), gomock.Eq(1), gomock.Eq("eni-secondary"), gomock.Eq(true), gomock.Any()).
			DoAndReturn(func(ctx context.Context, ips []string, count int, eniID string, isIPv6 bool, clientToken string) ([]string, error) {
				assert.NotEmpty(t, clientToken)
				return []string{"fd00::2"}, nil
			})

		ipv4Set, ipv6Set, err := env.node.allocateIPs(context.Background(), logrus.NewEntry(logrus.New()), allocation, 1, 1)
		if assert.NoError(t, err) {
			assert.Len(t, ipv4Set, 1)
			assert.Equal(t, "10.0.0.21", ipv4Set[0].PrivateIPAddress)
			assert.Len(t, ipv6Set, 1)
			assert.Equal(t, "fd00::2", ipv6Set[0].PrivateIPAddress)
		}
	})
}

func TestBCCNetworkResourceSetReleaseIPsPrimaryENIMode(t *testing.T) {
	env := setupBCCNodeTestEnv(t)

	env.mockCloud.EXPECT().
		BCCBatchDelIP(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, args *bccapi.BatchDelIpArgs) error {
			assert.Equal(t, env.k8sObj.Spec.InstanceID, args.InstanceId)
			assert.Equal(t, []string{"10.0.0.30"}, args.PrivateIps)
			return nil
		})
	env.mockCloud.EXPECT().
		BCCBatchDelIP(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, args *bccapi.BatchDelIpArgs) error {
			assert.Equal(t, env.k8sObj.Spec.InstanceID, args.InstanceId)
			assert.Equal(t, []string{"fd00::3"}, args.PrivateIps)
			return nil
		})

	err := env.node.releaseIPsPrimaryENIMode(context.Background(), &ipam.ReleaseAction{InterfaceID: "eni-primary"}, []string{"10.0.0.30"}, []string{"fd00::3"})
	assert.NoError(t, err)
}

func TestBCCNetworkResourceSetAllocateIPCrossSubnet(t *testing.T) {
	t.Run("primary eni with secondary mode not supported", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true

		_, _, err := env.node.allocateIPCrossSubnet(context.Background(), "sbn-cross")
		assert.Error(t, err)
	})

	t.Run("primary ip mode not supported", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = false
		env.k8sObj.Spec.ENI.UseMode = string(ccev2.ENIUseModePrimaryIP)

		_, _, err := env.node.allocateIPCrossSubnet(context.Background(), "sbn-cross")
		assert.Error(t, err)
	})

	t.Run("allocates cross subnet ipv4", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = false
		prevIPv6 := operatorOption.Config.EnableIPv6
		operatorOption.Config.EnableIPv6 = false
		t.Cleanup(func() {
			operatorOption.Config.EnableIPv6 = prevIPv6
		})

		quota := newCustomerIPQuota(log, k8s.WatcherClient(), env.k8sObj.Name, env.k8sObj.Spec.InstanceID, env.manager.bceclientd)
		quota.SetMaxENI(1)
		quota.SetMaxIP(3)
		env.base.eniQuota = quota
		env.base.lastResyncEniQuotaTime = time.Now()

		sbn := ccemock.NewMockSubnet("sbn-cross", "10.0.4.0/24")
		sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
		assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

		eni, err := ccemock.NewMockEni(env.k8sObj.Name, env.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 1)
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni}))

		env.mockCloud.EXPECT().
			BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Eq(eni.Spec.ENI.ID), gomock.Eq("sbn-cross"), gomock.Any(), gomock.Eq(1), gomock.Eq(false)).
			Return([]string{"10.0.4.10"}, nil)

		result, eniID, err := env.node.allocateIPCrossSubnet(context.Background(), "sbn-cross")
		if assert.NoError(t, err) {
			assert.Equal(t, eni.Spec.ENI.ID, eniID)
			assert.Len(t, result, 1)
			assert.Equal(t, "10.0.4.10", result[0].PrivateIPAddress)
			assert.Equal(t, "sbn-cross", result[0].SubnetID)
		}
	})
}

func TestBCCNetworkResourceSetReuseIPs(t *testing.T) {
	t.Run("primary eni with secondary mode not supported", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true

		_, err := env.node.reuseIPs(context.Background(), []*models.PrivateIP{{PrivateIPAddress: "10.0.0.40", SubnetID: "sbn-1"}}, "default/pod")
		assert.Error(t, err)
	})

	t.Run("empty ip list returns error", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = false

		_, err := env.node.reuseIPs(context.Background(), []*models.PrivateIP{}, "default/pod")
		assert.Error(t, err)
	})

	t.Run("local ip returns existing eni", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		ip := "10.0.5.10"

		endpoint := &ccev2.CCEEndpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "default",
			},
			Status: ccev2.EndpointStatus{
				Networking: &ccev2.EndpointNetworking{
					Addressing: ccev2.AddressPairList{
						&ccev2.AddressPair{IP: ip},
					},
				},
			},
		}
		assert.NoError(t, ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
			result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{})
			if err != nil {
				return nil
			}
			return []metav1.Object{result}
		}))

		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "eni-local",
				Labels: map[string]string{
					k8s.LabelInstanceID: env.k8sObj.Spec.InstanceID,
					k8s.LabelNodeName:   env.k8sObj.Name,
				},
			},
			Spec: ccev2.ENISpec{
				NodeName: env.k8sObj.Name,
				UseMode:  ccev2.ENIUseModeSecondaryIP,
				ENI: models.ENI{
					ID:       "eni-local",
					SubnetID: "sbn-local",
					VpcID:    env.k8sObj.Spec.ENI.VpcID,
					PrivateIPSet: []*models.PrivateIP{
						{PrivateIPAddress: ip, SubnetID: "sbn-local", Primary: true},
					},
				},
			},
			Status: ccev2.ENIStatus{
				CCEStatus: ccev2.ENIStatusReadyOnNode,
				VPCStatus: ccev2.VPCENIStatusInuse,
			},
		}
		assert.NoError(t, ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni}))

		eniID, err := env.node.reuseIPs(context.Background(), []*models.PrivateIP{{PrivateIPAddress: ip, SubnetID: "sbn-local"}}, "default/pod")
		assert.NoError(t, err)
		assert.Equal(t, "eni-local", eniID)
	})

	t.Run("reuse ip via cross subnet allocation", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		reuseIP := "10.0.6.10"

		endpoint := &ccev2.CCEEndpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "default",
			},
			Status: ccev2.EndpointStatus{
				Networking: &ccev2.EndpointNetworking{
					Addressing: ccev2.AddressPairList{
						&ccev2.AddressPair{IP: reuseIP},
					},
				},
			},
		}
		assert.NoError(t, ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
			result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{})
			if err != nil {
				return nil
			}
			return []metav1.Object{result}
		}))

		quota := newCustomerIPQuota(log, k8s.WatcherClient(), env.k8sObj.Name, env.k8sObj.Spec.InstanceID, env.manager.bceclientd)
		quota.SetMaxENI(1)
		quota.SetMaxIP(3)
		env.base.eniQuota = quota
		env.base.lastResyncEniQuotaTime = time.Now()

		sbn := ccemock.NewMockSubnet("sbn-alloc", "10.0.6.0/24")
		sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
		assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

		eni, err := ccemock.NewMockEni(env.k8sObj.Name, env.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 1)
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni}))

		env.mockCloud.EXPECT().
			BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Eq(eni.Spec.ENI.ID), gomock.Eq("sbn-alloc"), gomock.Eq([]string{reuseIP}), gomock.Eq(1), gomock.Eq(false)).
			Return([]string{reuseIP}, nil)

		ipset := []*models.PrivateIP{{PrivateIPAddress: reuseIP, SubnetID: "sbn-alloc"}}
		eniID, err := env.node.reuseIPs(context.Background(), ipset, "default/pod")
		assert.NoError(t, err)
		assert.Equal(t, eni.Spec.ENI.ID, eniID)
	})
}

func TestBCCNetworkResourceSetForceUsePrimaryENIWithSecondaryMode(t *testing.T) {
	env := setupBCCNodeTestEnv(t)
	env.node.k8sObj.Spec.ENI.UseMode = string(ccev2.ENIUseModePrimaryWithSecondaryIP)

	assert.True(t, env.node.isForceUsePrimaryENIWithSecondaryMode())
}

func TestBCCNetworkResourceSetPrepareIPAllocationErrors(t *testing.T) {
	t.Run("returns error when eni list fails", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.bccInfo = newMockBccInfo(env.k8sObj, 1)
		env.manager.enilister = &errorENILister{listErr: errors.New("list eni failed")}

		action, err := env.node.prepareIPAllocation(env.node.log.WithField("test", "list-eni-error"))
		assert.Error(t, err)
		assert.Nil(t, action)
	})

	t.Run("skip refresh when primary eni already created", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.bccInfo = newMockBccInfo(env.k8sObj, 1)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.haveCreatePrimaryENI = true
		env.node.primaryENISubnetID = "keep"

		action, err := env.node.prepareIPAllocation(env.node.log.WithField("test", "primary-created"))
		if assert.NoError(t, err) {
			assert.NotNil(t, action)
			assert.Equal(t, "keep", env.node.primaryENISubnetID)
			assert.True(t, env.node.haveCreatePrimaryENI)
		}
	})
}

func TestBCCNetworkResourceSetRefreshPrimarySubnetError(t *testing.T) {
	env := setupBCCNodeTestEnv(t)
	env.node.usePrimaryENIWithSecondaryMode = true
	env.node.bccInfo = nil

	env.mockCloud.EXPECT().
		GetBCCInstanceDetail(gomock.Any(), gomock.Eq(env.k8sObj.Spec.InstanceID)).
		Return(nil, errors.New("refresh failed"))

	err := env.node.refreshPrimarySubnet()
	assert.Error(t, err)
}

func TestBCCNetworkResourceSetCreatePrimaryENIOnClusterErrors(t *testing.T) {
	t.Run("refresh bcc info error", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.bccInfo = nil

		env.mockCloud.EXPECT().
			GetBCCInstanceDetail(gomock.Any(), gomock.Eq(env.k8sObj.Spec.InstanceID)).
			Return(nil, errors.New("get bcc info failed"))

		resource, err := k8s.CCEClient().CceV2().NetResourceSets().Get(context.Background(), env.k8sObj.Name, metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}

		err = env.node.createPrimaryENIOnCluster(context.Background(), env.node.log.WithField("test", "refresh-bcc-error"), resource)
		assert.Error(t, err)
	})

	t.Run("get eni error", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.bccInfo = newMockBccInfo(env.k8sObj, 1)
		env.manager.enilister = &errorENILister{getErr: errors.New("get eni failed")}

		resource, err := k8s.CCEClient().CceV2().NetResourceSets().Get(context.Background(), env.k8sObj.Name, metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}

		err = env.node.createPrimaryENIOnCluster(context.Background(), env.node.log.WithField("test", "get-eni-error"), resource)
		assert.Error(t, err)
	})

	t.Run("create eni error", func(t *testing.T) {
		env := setupBCCNodeTestEnvWithoutSubnets(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.bccInfo = &bccapi.InstanceModel{
			InstanceId: env.k8sObj.Spec.InstanceID,
			NicInfo: bccapi.NicInfo{
				VpcId:          env.k8sObj.Spec.ENI.VpcID,
				SubnetId:       "sbn-primary",
				EniId:          "eni-primary",
				Name:           "primary",
				MacAddress:     "02:16:3e:58:9a:4b",
				SecurityGroups: []string{"sg-test"},
				Ips: []bccapi.IpModel{
					{PrivateIp: "10.0.3.10", Primary: "true"},
				},
			},
		}

		fakeClient, ok := k8s.CCEClient().Interface.(*ccefake.Clientset)
		if !assert.True(t, ok) {
			return
		}
		fakeClient.PrependReactor("create", "enis", func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, ok := action.(k8stesting.CreateAction)
			if !ok {
				return true, nil, errors.New("unexpected action")
			}
			return true, createAction.GetObject(), errors.New("create eni failed")
		})

		resource, err := k8s.CCEClient().CceV2().NetResourceSets().Get(context.Background(), env.k8sObj.Name, metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}

		err = env.node.createPrimaryENIOnCluster(context.Background(), env.node.log.WithField("test", "create-eni-error"), resource)
		assert.Error(t, err)
	})

	t.Run("try borrow ips error", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.k8sObj.Spec.ENI.BurstableMehrfachENI = 1
		env.node.bccInfo = &bccapi.InstanceModel{
			InstanceId: env.k8sObj.Spec.InstanceID,
			NicInfo: bccapi.NicInfo{
				VpcId:          env.k8sObj.Spec.ENI.VpcID,
				SubnetId:       "sbn-missing",
				EniId:          "eni-primary",
				Name:           "primary",
				MacAddress:     "02:16:3e:58:9a:4b",
				SecurityGroups: []string{"sg-test"},
				Ips: []bccapi.IpModel{
					{PrivateIp: "10.0.4.10", Primary: "true"},
				},
			},
		}

		fakeClient, ok := k8s.CCEClient().Interface.(*ccefake.Clientset)
		if !assert.True(t, ok) {
			return
		}
		fakeClient.PrependReactor("create", "subnets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("create subnet failed")
		})

		resource, err := k8s.CCEClient().CceV2().NetResourceSets().Get(context.Background(), env.k8sObj.Name, metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}

		err = env.node.createPrimaryENIOnCluster(context.Background(), env.node.log.WithField("test", "borrow-ips-error"), resource)
		assert.Error(t, err)
	})

	t.Run("update status error", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.node.usePrimaryENIWithSecondaryMode = true
		env.node.bccInfo = &bccapi.InstanceModel{
			InstanceId: env.k8sObj.Spec.InstanceID,
			NicInfo: bccapi.NicInfo{
				VpcId:          env.k8sObj.Spec.ENI.VpcID,
				SubnetId:       "sbn-primary",
				EniId:          "eni-primary",
				Name:           "primary",
				MacAddress:     "02:16:3e:58:9a:4b",
				SecurityGroups: []string{"sg-test"},
				Ips: []bccapi.IpModel{
					{PrivateIp: "10.0.3.10", Primary: "true"},
				},
			},
		}

		sbn := ccemock.NewMockSubnet("sbn-primary", "10.0.3.0/24")
		sbn.Spec.VPCID = env.k8sObj.Spec.ENI.VpcID
		assert.NoError(t, ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn}))

		fakeClient, ok := k8s.CCEClient().Interface.(*ccefake.Clientset)
		if !assert.True(t, ok) {
			return
		}
		fakeClient.PrependReactor("update", "enis", func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() == "status" {
				return true, nil, errors.New("update status failed")
			}
			return false, nil, nil
		})

		resource, err := k8s.CCEClient().CceV2().NetResourceSets().Get(context.Background(), env.k8sObj.Name, metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}

		err = env.node.createPrimaryENIOnCluster(context.Background(), env.node.log.WithField("test", "update-status-error"), resource)
		assert.Error(t, err)
	})
}

func TestBCCNetworkResourceSetAllocateIPsPrimaryENIModeError(t *testing.T) {
	env := setupBCCNodeTestEnv(t)
	allocation := &ipam.AllocationAction{
		InterfaceID: "eni-primary",
		PoolID:      ipamTypes.PoolID("sbn-primary"),
	}

	env.mockCloud.EXPECT().
		BCCBatchAddIP(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("allocate failed"))

	ipv4Set, ipv6Set, err := env.node.allocateIPsPrimaryENIMode(context.Background(), logrus.NewEntry(logrus.New()), allocation, 1, 0)
	assert.Error(t, err)
	assert.Nil(t, ipv4Set)
	assert.Nil(t, ipv6Set)
}

func TestBCCNetworkResourceSetReleaseIPsPrimaryMode(t *testing.T) {
	env := setupBCCNodeTestEnv(t)
	env.node.usePrimaryENIWithSecondaryMode = true

	env.mockCloud.EXPECT().
		BCCBatchDelIP(gomock.Any(), gomock.Any()).
		Return(nil)

	err := env.node.releaseIPs(context.Background(), &ipam.ReleaseAction{InterfaceID: "eni-primary"}, []string{"10.0.0.30"}, nil)
	assert.NoError(t, err)
}

func TestBCCNetworkResourceSetReleaseIPsPrimaryENIModeErrors(t *testing.T) {
	t.Run("ipv4 release error", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.mockCloud.EXPECT().
			BCCBatchDelIP(gomock.Any(), gomock.Any()).
			Return(errors.New("release ipv4 failed"))

		err := env.node.releaseIPsPrimaryENIMode(context.Background(), &ipam.ReleaseAction{InterfaceID: "eni-primary"}, []string{"10.0.0.30"}, nil)
		assert.Error(t, err)
	})

	t.Run("ipv6 release error", func(t *testing.T) {
		env := setupBCCNodeTestEnv(t)
		env.mockCloud.EXPECT().
			BCCBatchDelIP(gomock.Any(), gomock.Any()).
			Return(errors.New("release ipv6 failed"))

		err := env.node.releaseIPsPrimaryENIMode(context.Background(), &ipam.ReleaseAction{InterfaceID: "eni-primary"}, nil, []string{"fd00::3"})
		assert.Error(t, err)
	})
}
