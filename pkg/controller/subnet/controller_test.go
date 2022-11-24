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

package subnet

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/golang/mock/gomock"
	"github.com/juju/ratelimit"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
)

func MockSubnetController(t *testing.T) *SubnetController {
	ctrl := gomock.NewController(t)
	_, _, crdClient, crdInformer, bceClient, eventBroadcaster, _ := data.NewMockEnv(ctrl)

	return NewSubnetController(crdInformer, crdClient, bceClient, eventBroadcaster)
}

func TestIPAM_updateSubnetStatus(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		eniCache             map[string][]*enisdk.Eni
		privateIPNumCache    map[string]int
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.WorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          externalversions.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		cniMode              types.ContainerNetworkMode
		vpcID                string
		clusterID            string
		bucket               *ratelimit.Bucket
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, _, crdClient, crdInformer, bceClient, eventBroadcaster, _ := data.NewMockEnv(ctrl)

				crdClient.CceV1alpha1().Subnets(metav1.NamespaceDefault).Create(context.TODO(), &v1alpha1.Subnet{
					Spec: v1alpha1.SubnetSpec{
						ID: "sbn-test",
					},
					Status: v1alpha1.SubnetStatus{
						AvailableIPNum: 200,
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(crdInformer)

				gomock.InOrder(
					bceClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-test").Return(&vpc.Subnet{
						SubnetId:    "sbn-test",
						AvailableIp: 100,
					}, nil),
				)

				return fields{
					ctrl:             ctrl,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            bceClient,
					eventBroadcaster: eventBroadcaster,
				}
			}(),
			args: args{
				ctx: nil,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			sbnController := NewSubnetController(
				tt.fields.crdInformer,
				tt.fields.crdClient,
				tt.fields.cloud,
				tt.fields.eventBroadcaster,
			)

			sbnController.updateSubnetStatus()
		})
	}
}

func TestIPAM_ensureSubnetCRDExists(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		eniCache             map[string][]*enisdk.Eni
		privateIPNumCache    map[string]int
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.WorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		cniMode              types.ContainerNetworkMode
		vpcID                string
		clusterID            string
		bucket               *ratelimit.Bucket
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
	}
	type args struct {
		ctx  context.Context
		name string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "first time to create subnet",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, _, crdClient, crdInformer, bceClient, eventBroadcaster, _ := data.NewMockEnv(ctrl)

				gomock.InOrder(
					bceClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-test").Return(&vpc.Subnet{
						SubnetId:    "sbn-test",
						AvailableIp: 100,
					}, nil),
				)

				return fields{
					ctrl:             ctrl,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            bceClient,
					eventBroadcaster: eventBroadcaster,
				}
			}(),
			args: args{
				ctx:  context.TODO(),
				name: "sbn-test",
			},
			wantErr: false,
		},
		{
			name: "subnet has no more ip",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, _, crdClient, crdInformer, bceClient, eventBroadcaster, recorder := data.NewMockEnv(ctrl)

				crdClient.CceV1alpha1().Subnets(metav1.NamespaceDefault).Create(context.TODO(), &v1alpha1.Subnet{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sbn-test",
					},
					Status: v1alpha1.SubnetStatus{
						AvailableIPNum: 0,
						Enable:         false,
						HasNoMoreIP:    true,
					},
				}, metav1.CreateOptions{})

				subnetInformer := crdInformer.Cce().V1alpha1().Subnets().Informer()
				crdInformer.Start(wait.NeverStop)
				cache.WaitForNamedCacheSync(
					"cce-ipam",
					wait.NeverStop,
					subnetInformer.HasSynced,
				)

				return fields{
					ctrl:             ctrl,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            bceClient,
					eventRecorder:    recorder,
					eventBroadcaster: eventBroadcaster,
				}
			}(),
			args: args{
				ctx:  context.TODO(),
				name: "sbn-test",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			sbnController := NewSubnetController(
				tt.fields.crdInformer,
				tt.fields.crdClient,
				tt.fields.cloud,
				tt.fields.eventBroadcaster,
			)

			if err := sbnController.EnsureSubnetCRExists(tt.args.ctx, tt.args.name); (err != nil) != tt.wantErr {
				t.Errorf("ensureSubnetCRExists() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func waitForCacheSync(crdInformer crdinformers.SharedInformerFactory) {
	wepInformer := crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()
	ippoolInformer := crdInformer.Cce().V1alpha1().IPPools().Informer()
	subnetInformer := crdInformer.Cce().V1alpha1().Subnets().Informer()

	crdInformer.Start(wait.NeverStop)

	cache.WaitForNamedCacheSync(
		"cce-ipam",
		wait.NeverStop,
		wepInformer.HasSynced,
		ippoolInformer.HasSynced,
		subnetInformer.HasSynced,
	)
}

type SubnetControllerTester struct {
	suite.Suite
	tsc        *SubnetController
	kubeClient kubernetes.Interface
	key        string
	wantErr    bool
}

func (suite *SubnetControllerTester) SetupTest() {
	suite.tsc = MockSubnetController(suite.T())
	suite.key = "default/sbn-test"
	suite.wantErr = false
}

func (suite *SubnetControllerTester) assert() {
	waitForCacheSync(suite.tsc.crdInformer)
	err := suite.tsc.SyncIPPool(suite.key, suite.tsc.crdInformer.Cce().V1alpha1().IPPools().Lister())
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
}

func (suite *SubnetControllerTester) TestRun() {
	stopCh := make(chan struct{})
	close(stopCh)
	suite.tsc.Run(stopCh)
}

func (suite *SubnetControllerTester) TestIPPoolNotExists() {
	suite.assert()
}

func (suite *SubnetControllerTester) TestSubnetNotExists() {
	suite.key = "default/ippool-test"
	suite.wantErr = true
	pool := data.MockIPPool()
	suite.tsc.crdClient.CceV1alpha1().IPPools("default").Create(context.TODO(), pool, metav1.CreateOptions{})

	suite.tsc.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(gomock.Any(), "sbn-test").Return(nil, fmt.Errorf("NotFound"))

	suite.assert()
}

func TestSubnetController_SyncIPPool(t *testing.T) {
	suite.Run(t, new(SubnetControllerTester))
}
