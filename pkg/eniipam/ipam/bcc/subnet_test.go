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

package bcc

import (
	"context"
	"sync"
	"testing"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/golang/mock/gomock"
	"github.com/juju/ratelimit"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
)

func TestIPAM_updateSubnetStatus(t *testing.T) {
	type fields struct {
		ctrl                  *gomock.Controller
		lock                  sync.RWMutex
		eniCache              map[string][]*enisdk.Eni
		privateIPNumCache     map[string]int
		cacheHasSynced        bool
		allocated             map[string]*v1alpha1.WorkloadEndpoint
		eventBroadcaster      record.EventBroadcaster
		eventRecorder         record.EventRecorder
		kubeInformer          informers.SharedInformerFactory
		kubeClient            kubernetes.Interface
		crdInformer           externalversions.SharedInformerFactory
		crdClient             versioned.Interface
		cloud                 cloud.Interface
		cniMode               types.ContainerNetworkMode
		vpcID                 string
		clusterID             string
		subnetSelectionPolicy SubnetSelectionPolicy
		bucket                *ratelimit.Bucket
		eniSyncPeriod         time.Duration
		informerResyncPeriod  time.Duration
		gcPeriod              time.Duration
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
				_, kubeInformer, crdClient, crdInformer, bceClient, _, _ := setupEnv(ctrl)

				crdClient.CceV1alpha1().Subnets(metav1.NamespaceDefault).Create(&v1alpha1.Subnet{
					Spec: v1alpha1.SubnetSpec{
						ID: "sbn-test",
					},
					Status: v1alpha1.SubnetStatus{
						AvailableIPNum: 200,
					},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					bceClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-test").Return(&vpc.Subnet{
						SubnetId:    "sbn-test",
						AvailableIp: 100,
					}, nil),
				)

				return fields{
					ctrl:        ctrl,
					crdInformer: crdInformer,
					crdClient:   crdClient,
					cloud:       bceClient,
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
			ipam := &IPAM{
				lock:                  tt.fields.lock,
				eniCache:              tt.fields.eniCache,
				privateIPNumCache:     tt.fields.privateIPNumCache,
				cacheHasSynced:        tt.fields.cacheHasSynced,
				allocated:             tt.fields.allocated,
				eventBroadcaster:      tt.fields.eventBroadcaster,
				eventRecorder:         tt.fields.eventRecorder,
				kubeInformer:          tt.fields.kubeInformer,
				kubeClient:            tt.fields.kubeClient,
				crdInformer:           tt.fields.crdInformer,
				crdClient:             tt.fields.crdClient,
				cloud:                 tt.fields.cloud,
				cniMode:               tt.fields.cniMode,
				vpcID:                 tt.fields.vpcID,
				clusterID:             tt.fields.clusterID,
				subnetSelectionPolicy: tt.fields.subnetSelectionPolicy,
				bucket:                tt.fields.bucket,
				eniSyncPeriod:         tt.fields.eniSyncPeriod,
				informerResyncPeriod:  tt.fields.informerResyncPeriod,
				gcPeriod:              tt.fields.gcPeriod,
			}
			if err := ipam.updateSubnetStatus(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("updateSubnetStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPAM_ensureSubnetCRDExists(t *testing.T) {
	type fields struct {
		ctrl                  *gomock.Controller
		lock                  sync.RWMutex
		eniCache              map[string][]*enisdk.Eni
		privateIPNumCache     map[string]int
		cacheHasSynced        bool
		allocated             map[string]*v1alpha1.WorkloadEndpoint
		eventBroadcaster      record.EventBroadcaster
		eventRecorder         record.EventRecorder
		kubeInformer          informers.SharedInformerFactory
		kubeClient            kubernetes.Interface
		crdInformer           crdinformers.SharedInformerFactory
		crdClient             versioned.Interface
		cloud                 cloud.Interface
		cniMode               types.ContainerNetworkMode
		vpcID                 string
		clusterID             string
		subnetSelectionPolicy SubnetSelectionPolicy
		bucket                *ratelimit.Bucket
		eniSyncPeriod         time.Duration
		informerResyncPeriod  time.Duration
		gcPeriod              time.Duration
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
				_, _, crdClient, crdInformer, bceClient, _, _ := setupEnv(ctrl)

				gomock.InOrder(
					bceClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-test").Return(&vpc.Subnet{
						SubnetId:    "sbn-test",
						AvailableIp: 100,
					}, nil),
				)

				return fields{
					ctrl:        ctrl,
					crdInformer: crdInformer,
					crdClient:   crdClient,
					cloud:       bceClient,
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
				_, _, crdClient, crdInformer, bceClient, _, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().Subnets(metav1.NamespaceDefault).Create(&v1alpha1.Subnet{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sbn-test",
					},
					Status: v1alpha1.SubnetStatus{
						AvailableIPNum: 0,
						Enable:         false,
						HasNoMoreIP:    true,
					},
				})

				subnetInformer := crdInformer.Cce().V1alpha1().Subnets().Informer()
				crdInformer.Start(wait.NeverStop)
				cache.WaitForNamedCacheSync(
					"cce-ipam",
					wait.NeverStop,
					subnetInformer.HasSynced,
				)

				return fields{
					ctrl:          ctrl,
					crdInformer:   crdInformer,
					crdClient:     crdClient,
					cloud:         bceClient,
					eventRecorder: recorder,
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
			ipam := &IPAM{
				lock:                  tt.fields.lock,
				eniCache:              tt.fields.eniCache,
				privateIPNumCache:     tt.fields.privateIPNumCache,
				cacheHasSynced:        tt.fields.cacheHasSynced,
				allocated:             tt.fields.allocated,
				eventBroadcaster:      tt.fields.eventBroadcaster,
				eventRecorder:         tt.fields.eventRecorder,
				kubeInformer:          tt.fields.kubeInformer,
				kubeClient:            tt.fields.kubeClient,
				crdInformer:           tt.fields.crdInformer,
				crdClient:             tt.fields.crdClient,
				cloud:                 tt.fields.cloud,
				cniMode:               tt.fields.cniMode,
				vpcID:                 tt.fields.vpcID,
				clusterID:             tt.fields.clusterID,
				subnetSelectionPolicy: tt.fields.subnetSelectionPolicy,
				bucket:                tt.fields.bucket,
				eniSyncPeriod:         tt.fields.eniSyncPeriod,
				informerResyncPeriod:  tt.fields.informerResyncPeriod,
				gcPeriod:              tt.fields.gcPeriod,
			}
			if err := ipam.ensureSubnetCRExists(tt.args.ctx, tt.args.name); (err != nil) != tt.wantErr {
				t.Errorf("ensureSubnetCRExists() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
