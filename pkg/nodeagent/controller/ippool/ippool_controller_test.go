/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package ippool

import (
	"context"
	"reflect"
	"testing"
	"time"

	bbcapi "github.com/baidubce/bce-sdk-go/services/bbc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestController_findSameZoneSubnets(t *testing.T) {
	type fields struct {
		ctrl                *gomock.Controller
		kubeClient          kubernetes.Interface
		crdClient           clientset.Interface
		metaClient          metadata.Interface
		eventRecorder       record.EventRecorder
		ippoolName          string
		cniMode             types.ContainerNetworkMode
		instanceID          string
		instanceType        metadata.InstanceTypeEx
		nodeName            string
		bccInstance         *bccapi.InstanceModel
		bbcInstance         *bbcapi.GetInstanceEniResult
		cloudClient         cloud.Interface
		eniSubnetCandidates []string
		eniSecurityGroups   []string
		preAttachedENINum   int
		podSubnetCandidates []string
		subnetZoneCache     map[string]string
	}
	type args struct {
		ctx     context.Context
		subnets []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []string
	}{
		{
			name: "normal case",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				cloudClient := mockcloud.NewMockInterface(ctrl)

				gomock.InOrder(
					cloudClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-a1").Return(&vpc.Subnet{ZoneName: "zoneA"}, nil),
					cloudClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-a2").Return(&vpc.Subnet{ZoneName: "zoneA"}, nil),
					cloudClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-b1").Return(&vpc.Subnet{ZoneName: "zoneB"}, nil),
				)

				return fields{
					ctrl: ctrl,
					bbcInstance: &bbcapi.GetInstanceEniResult{
						ZoneName:     "zoneB",
						PrivateIpSet: []bbcapi.PrivateIP{},
					},
					cloudClient:         cloudClient,
					eniSubnetCandidates: []string{},
					eniSecurityGroups:   []string{},
					preAttachedENINum:   0,
					podSubnetCandidates: []string{},
					subnetZoneCache:     map[string]string{},
				}
			}(),
			args: args{
				ctx:     context.TODO(),
				subnets: []string{"sbn-a1", "sbn-a2", "sbn-b1"},
			},
			want: []string{"sbn-b1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			c := &Controller{
				kubeClient:          tt.fields.kubeClient,
				crdClient:           tt.fields.crdClient,
				metaClient:          tt.fields.metaClient,
				eventRecorder:       tt.fields.eventRecorder,
				ippoolName:          tt.fields.ippoolName,
				cniMode:             tt.fields.cniMode,
				instanceID:          tt.fields.instanceID,
				instanceType:        tt.fields.instanceType,
				nodeName:            tt.fields.nodeName,
				bccInstance:         tt.fields.bccInstance,
				bbcInstance:         tt.fields.bbcInstance,
				cloudClient:         tt.fields.cloudClient,
				eniSubnetCandidates: tt.fields.eniSubnetCandidates,
				eniSecurityGroups:   tt.fields.eniSecurityGroups,
				preAttachedENINum:   tt.fields.preAttachedENINum,
				podSubnetCandidates: tt.fields.podSubnetCandidates,
				subnetZoneCache:     tt.fields.subnetZoneCache,
			}
			if got := c.findSameZoneSubnets(tt.args.ctx, tt.args.subnets); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Controller.findSameZoneSubnets() = %v, want %v", got, tt.want)
			}
		})
	}
}

func setupEnv(ctrl *gomock.Controller) (
	kubernetes.Interface,
	versioned.Interface,
	*mockcloud.MockInterface,
	record.EventBroadcaster,
	record.EventRecorder,
) {
	kubeClient := kubefake.NewSimpleClientset()
	crdClient := crdfake.NewSimpleClientset()
	cloudClient := mockcloud.NewMockInterface(ctrl)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	return kubeClient,
		crdClient, cloudClient,
		eventBroadcaster, recorder
}

func mockController(t *testing.T) *Controller {
	ctrl := gomock.NewController(t)
	kubeClient, crdClient, cloudClient, _, _ := setupEnv(ctrl)
	c := New(kubeClient,
		cloudClient,
		crdClient,
		types.CCEModeCrossVPCEni,
		"10.0.0.2",
		"i-QGUcXDdM",
		metadata.InstanceTypeExBCC,
		[]string{"sbn-test"},
		[]string{"sg-test"},
		[]string{},
		1,
		[]string{"sbn-test"},
	)
	return c
}

type IPPoolTestCase struct {
	suite.Suite
	c            *Controller
	kubeInformer informers.SharedInformerFactory
	wantErr      bool
	// want        *networkingv1alpha1.WorkloadEndpoint
	ctx context.Context
}

func (suite *IPPoolTestCase) SetupTest() {
	customerMaxIPPerENI = 0
	customerMaxENINum = 0
	suite.c = mockController(suite.T())
	suite.ctx = context.TODO()
	suite.kubeInformer = informers.NewSharedInformerFactory(suite.c.kubeClient, time.Hour)
}

func (suite *IPPoolTestCase) startInformer() {

}

func (suite *IPPoolTestCase) mockCloudInterface() {
	c := suite.c
	cc := suite.c.cloudClient.(*mockcloud.MockInterface)

	model := &bccapi.InstanceModel{
		InstanceId:         c.instanceID,
		Hostname:           c.nodeName,
		CpuCount:           8,
		MemoryCapacityInGB: 64,
		ZoneName:           "zoneF",
	}
	cc.EXPECT().GetBCCInstanceDetail(gomock.Any(), suite.c.instanceID).Return(model, nil).AnyTimes()

	subnet := &vpc.Subnet{
		SubnetId: "sbn-test",
		ZoneName: "zoneF",
		VPCId:    "vpc-test",
		Cidr:     "10.0.0.1/24",
	}
	cc.EXPECT().DescribeSubnet(gomock.Any(), gomock.Eq("sbn-test")).Return(subnet, nil).AnyTimes()

	cc.EXPECT().ListSecurityGroup(gomock.Any(), "", suite.c.instanceID).Return([]bccapi.SecurityGroupModel{{Id: "sg-test"}}, nil).AnyTimes()

	cc.EXPECT().GetBBCInstanceENI(gomock.Any(), suite.c.instanceID).Return(&bbcapi.GetInstanceEniResult{
		Id:       "bbc-eni",
		SubnetId: "sbn-test",
	}, nil).AnyTimes()
}

func (suite *IPPoolTestCase) TestIPv4CreateIPRange() {
	c := suite.c
	c.cniMode = types.CCEModeRouteVeth
	node := mockIPv4Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with ip range error")

	node, err = c.kubeClient.CoreV1().Nodes().Get(suite.ctx, node.Name, metav1.GetOptions{})
	suite.Assert().NoError(err, "get node error")

	resource, ok := node.Status.Capacity[networking.ResourceENIForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(1, resource.Value(), "eni capacity")
	resource, ok = node.Status.Capacity[networking.ResourceIPForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(255, resource.Value(), "eni capacity")
}

func (suite *IPPoolTestCase) TestIPv4IPv6CreateIPRange() {
	c := suite.c
	c.cniMode = types.CCEModeRouteVeth
	node := mockIPv4IPv6Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with ip range error")

	node, err = c.kubeClient.CoreV1().Nodes().Get(suite.ctx, node.Name, metav1.GetOptions{})
	suite.Assert().NoError(err, "get node error")

	resource, ok := node.Status.Capacity[networking.ResourceENIForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(1, resource.Value(), "eni capacity")
	resource, ok = node.Status.Capacity[networking.ResourceIPForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(9223372036854775806, resource.Value(), "eni capacity")
}

func (suite *IPPoolTestCase) TestCreateBCCENI() {
	c := suite.c
	c.cniMode = types.CCEModeSecondaryIPVeth
	node := mockIPv4Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with ip range error")

	node, err = c.kubeClient.CoreV1().Nodes().Get(suite.ctx, node.Name, metav1.GetOptions{})
	suite.Assert().NoError(err, "get node error")

	resource, ok := node.Status.Capacity[networking.ResourceENIForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(8, resource.Value(), "eni capacity")
	resource, ok = node.Status.Capacity[networking.ResourceIPForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(232, resource.Value(), "eni capacity")
}

func (suite *IPPoolTestCase) TestEmptyIPPool() {
	c := suite.c
	c.cniMode = types.CCEModeSecondaryIPVeth
	node := mockIPv4Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()
	cc := suite.c.cloudClient.(*mockcloud.MockInterface)
	cc.EXPECT().DescribeSubnet(gomock.Any(), gomock.Eq("sbn-test")).Return(&vpc.Subnet{
		SubnetId: "sbn-test",
		ZoneName: "zoneD",
		VPCId:    "vpc-test",
		Cidr:     "10.0.0.1/24",
	}, nil).AnyTimes()

	ippool := &networkingv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ippool-10-0-0-2",
			Namespace: v1.NamespaceDefault,
		},
		Spec: networkingv1alpha1.IPPoolSpec{},
	}
	c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(suite.ctx, ippool, metav1.CreateOptions{})

	c.eniSecurityGroups = []string{}
	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with ip range error")

	node, err = c.kubeClient.CoreV1().Nodes().Get(suite.ctx, node.Name, metav1.GetOptions{})
	suite.Assert().NoError(err, "get node error")

	resource, ok := node.Status.Capacity[networking.ResourceENIForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(8, resource.Value(), "eni capacity")
	resource, ok = node.Status.Capacity[networking.ResourceIPForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(232, resource.Value(), "eni capacity")
}

func (suite *IPPoolTestCase) TestCreateBBCENI() {
	c := suite.c
	c.cniMode = types.CCEModeBBCSecondaryIPVeth
	node := mockIPv4Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()
	c.instanceType = metadata.InstanceTypeExBBC

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with ip range error")

	node, err = c.kubeClient.CoreV1().Nodes().Get(suite.ctx, node.Name, metav1.GetOptions{})
	suite.Assert().NoError(err, "get node error")

	resource, ok := node.Status.Capacity[networking.ResourceENIForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(1, resource.Value(), "eni capacity")
	resource, ok = node.Status.Capacity[networking.ResourceIPForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(39, resource.Value(), "eni capacity")
}

func (suite *IPPoolTestCase) TestCreateBCCENIonVPCHybird() {
	c := suite.c
	c.cniMode = types.CCEModeBBCSecondaryIPVeth
	node := mockIPv4Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()
	c.instanceType = metadata.InstanceTypeExBCC

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with ip range error")

	node, err = c.kubeClient.CoreV1().Nodes().Get(suite.ctx, node.Name, metav1.GetOptions{})
	suite.Assert().NoError(err, "get node error")

	resource, ok := node.Status.Capacity[networking.ResourceENIForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(8, resource.Value(), "eni capacity")
	resource, ok = node.Status.Capacity[networking.ResourceIPForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(232, resource.Value(), "eni capacity")
}

func (suite *IPPoolTestCase) TestCreateBBCENIonHybirdWithCustomerMaxIP() {
	c := suite.c
	c.cniMode = types.CCEModeBBCSecondaryIPVeth
	node := mockIPv4Node(c.nodeName, c.instanceID)

	customerMaxIPPerENI = 100
	customerMaxENINum = 10

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()
	c.instanceType = metadata.InstanceTypeExBBC

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with ip range error")

	node, err = c.kubeClient.CoreV1().Nodes().Get(suite.ctx, node.Name, metav1.GetOptions{})
	suite.Assert().NoError(err, "get node error")

	resource, ok := node.Status.Capacity[networking.ResourceENIForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(10, resource.Value(), "eni capacity")
	resource, ok = node.Status.Capacity[networking.ResourceIPForNode]
	suite.Assert().True(ok, "node status donot contains eni resource")
	suite.Assert().EqualValues(990, resource.Value(), "eni capacity")
}

func (suite *IPPoolTestCase) TestCreateCrossVPCEni() {
	c := suite.c
	c.cniMode = types.CCEModeExclusiveCrossVPCEni
	node := mockIPv4Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.NoError(err, "sync ip pool manager with cross vpc eni error")
}

func (suite *IPPoolTestCase) TestUnknownMode() {
	c := suite.c
	c.cniMode = "foo"
	node := mockIPv4Node(c.nodeName, c.instanceID)

	c.kubeClient.CoreV1().Nodes().Create(suite.ctx, node, metav1.CreateOptions{})
	suite.startInformer()
	suite.mockCloudInterface()

	// wait for cache sync
	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		suite.kubeInformer.Start(stopChan)
	}()
	cache.WaitForNamedCacheSync("controller-test", stopChan, suite.kubeInformer.Core().V1().Nodes().Informer().HasSynced)

	err := c.SyncNode(c.nodeName, suite.kubeInformer.Core().V1().Nodes().Lister())
	suite.Error(err)
}

func mockIPv4Node(name, instance string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.NodeSpec{
			PodCIDR:    "192.168.1.0/24",
			ProviderID: "cce://i-QGUcXDdM",
		},
		Status: v1.NodeStatus{
			Phase: v1.NodeRunning,
			Capacity: v1.ResourceList{
				v1.ResourceCPU: *resource.NewQuantity(1, resource.DecimalSI),
			},
		},
	}
}

func mockIPv4IPv6Node(name, instance string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.NodeSpec{
			PodCIDRs:   []string{"192.168.1.0/24", "2002::1234:abcd:ffff:c0a8:101/64"},
			ProviderID: "cce://i-QGUcXDdM",
		},
		Status: v1.NodeStatus{
			Phase: v1.NodeRunning,
			Capacity: v1.ResourceList{
				v1.ResourceCPU: *resource.NewQuantity(1, resource.DecimalSI),
			},
		},
	}
}

func TestSyncNode(t *testing.T) {
	suite.Run(t, new(IPPoolTestCase))
}
