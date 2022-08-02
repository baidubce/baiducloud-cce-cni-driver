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
	"reflect"
	"sync"
	"testing"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/juju/ratelimit"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	datastorev1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func Test_buildENICache(t *testing.T) {
	var (
		enis = []enisdk.Eni{
			enisdk.Eni{Name: "cce-xxx/i-aaa/nodeA/1234"},
			enisdk.Eni{Name: "cce-xxx/i-bbb/nodeB/1234"},
			enisdk.Eni{Name: "cce-xxx/i-aaa/nodeA/1234"},
			enisdk.Eni{Name: "xxxx"},
		}
	)
	type args struct {
		ctx  context.Context
		enis []enisdk.Eni
	}
	tests := []struct {
		name string
		args args
		want map[string][]*enisdk.Eni
	}{
		{
			name: "normal case",
			args: args{
				ctx:  context.TODO(),
				enis: enis,
			},
			want: map[string][]*enisdk.Eni{
				"nodeA": []*enisdk.Eni{&enis[0], &enis[2]},
				"nodeB": []*enisdk.Eni{&enis[1]},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildENICache(tt.args.ctx, tt.args.enis); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildENICache() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_listAttachedENIs(t *testing.T) {
	var (
		enis = []enisdk.Eni{
			enisdk.Eni{Name: "cce-xxx/i-aaa/nodeA/1234", Status: utileni.ENIStatusInuse, InstanceId: "i-aaa"},
			enisdk.Eni{Name: "cce-xxx/i-bbb/nodeB/1234", Status: utileni.ENIStatusAttaching},
			enisdk.Eni{Name: "cce-xxx/i-aaa/nodeA/1234", Status: utileni.ENIStatusAttaching},
		}
	)
	type args struct {
		ctx       context.Context
		clusterID string
		node      *v1.Node
		eniCache  map[string][]*enisdk.Eni
	}
	tests := []struct {
		name    string
		args    args
		want    []*enisdk.Eni
		wantErr bool
	}{
		{
			name: "normal case",
			args: args{
				ctx:       nil,
				clusterID: "cce-xxx",
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "nodeA",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-aaa",
					},
				},
				eniCache: map[string][]*enisdk.Eni{
					"nodeA": []*enisdk.Eni{&enis[0], &enis[2]},
					"nodeB": []*enisdk.Eni{&enis[1]},
				},
			},
			want:    []*enisdk.Eni{&enis[0]},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := listAttachedENIs(tt.args.ctx, tt.args.clusterID, tt.args.node, tt.args.eniCache)
			if (err != nil) != tt.wantErr {
				t.Errorf("listAttachedENIs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("listAttachedENIs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_getSecurityGroupsFromDefaultIPPool(t *testing.T) {
	type fields struct {
		ctrl                    *gomock.Controller
		lock                    sync.RWMutex
		eniCache                map[string][]*enisdk.Eni
		privateIPNumCache       map[string]int
		possibleLeakedIPCache   map[eniAndIPAddrKey]time.Time
		addIPBackoffCache       map[string]*wait.Backoff
		cacheHasSynced          bool
		allocated               map[string]*v1alpha1.WorkloadEndpoint
		datastore               *datastorev1.DataStore
		idleIPPoolMinSize       int
		idleIPPoolMaxSize       int
		batchAddIPNum           int
		eventBroadcaster        record.EventBroadcaster
		eventRecorder           record.EventRecorder
		kubeInformer            informers.SharedInformerFactory
		kubeClient              kubernetes.Interface
		crdInformer             crdinformers.SharedInformerFactory
		crdClient               versioned.Interface
		cloud                   cloud.Interface
		clock                   clock.Clock
		cniMode                 types.ContainerNetworkMode
		vpcID                   string
		clusterID               string
		subnetSelectionPolicy   SubnetSelectionPolicy
		bucket                  *ratelimit.Bucket
		eniSyncPeriod           time.Duration
		informerResyncPeriod    time.Duration
		gcPeriod                time.Duration
		buildDataStoreEventChan map[string]chan *event
		increasePoolEventChan   map[string]chan *event
	}
	type args struct {
		ctx  context.Context
		node *v1.Node
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []string
		want1   []string
		wantErr bool
	}{
		{
			name: "no ippool found",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, _, crdClient, _, _, _, _ := setupEnv(ctrl)

				return fields{
					ctrl:      ctrl,
					lock:      sync.RWMutex{},
					crdClient: crdClient,
				}
			}(),
			args: args{
				ctx: log.NewContext(),
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
					},
				},
			},
			want:    nil,
			want1:   nil,
			wantErr: true,
		},
		{
			name: "neither sg or esg specified",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, _, crdClient, _, _, _, _ := setupEnv(ctrl)

				ctx := log.NewContext()
				crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(ctx, &v1alpha1.IPPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ippool-node",
						Namespace: v1.NamespaceDefault,
					},
					Spec: v1alpha1.IPPoolSpec{
						ENI: v1alpha1.ENISpec{
							VPCID:                    "",
							AvailabilityZone:         "",
							Subnets:                  []string{},
							SecurityGroups:           []string{},
							EnterpriseSecurityGroups: []string{},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl:      ctrl,
					lock:      sync.RWMutex{},
					crdClient: crdClient,
				}
			}(),
			args: args{
				ctx: log.NewContext(),
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
					},
				},
			},
			want:    nil,
			want1:   nil,
			wantErr: true,
		},
		{
			name: "both sg and esg specified",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, _, crdClient, _, _, _, _ := setupEnv(ctrl)

				ctx := log.NewContext()
				crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(ctx, &v1alpha1.IPPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ippool-node",
						Namespace: v1.NamespaceDefault,
					},
					Spec: v1alpha1.IPPoolSpec{
						ENI: v1alpha1.ENISpec{
							SecurityGroups:           []string{"g-xxx"},
							EnterpriseSecurityGroups: []string{"esg-xxx"},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl:      ctrl,
					lock:      sync.RWMutex{},
					crdClient: crdClient,
				}
			}(),
			args: args{
				ctx: log.NewContext(),
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
					},
				},
			},
			want:    nil,
			want1:   nil,
			wantErr: true,
		},
		{
			name: "normal case",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, _, crdClient, _, _, _, _ := setupEnv(ctrl)

				ctx := log.NewContext()
				crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(ctx, &v1alpha1.IPPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ippool-node",
						Namespace: v1.NamespaceDefault,
					},
					Spec: v1alpha1.IPPoolSpec{
						ENI: v1alpha1.ENISpec{
							SecurityGroups:           nil,
							EnterpriseSecurityGroups: []string{"esg-xxx"},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl:      ctrl,
					lock:      sync.RWMutex{},
					crdClient: crdClient,
				}
			}(),
			args: args{
				ctx: log.NewContext(),
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
					},
				},
			},
			want:    nil,
			want1:   []string{"esg-xxx"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		if tt.fields.ctrl != nil {
			defer tt.fields.ctrl.Finish()
		}
		t.Run(tt.name, func(t *testing.T) {
			ipam := &IPAM{
				lock:                    tt.fields.lock,
				eniCache:                tt.fields.eniCache,
				privateIPNumCache:       tt.fields.privateIPNumCache,
				possibleLeakedIPCache:   tt.fields.possibleLeakedIPCache,
				addIPBackoffCache:       tt.fields.addIPBackoffCache,
				cacheHasSynced:          tt.fields.cacheHasSynced,
				allocated:               tt.fields.allocated,
				datastore:               tt.fields.datastore,
				idleIPPoolMinSize:       tt.fields.idleIPPoolMinSize,
				idleIPPoolMaxSize:       tt.fields.idleIPPoolMaxSize,
				batchAddIPNum:           tt.fields.batchAddIPNum,
				eventBroadcaster:        tt.fields.eventBroadcaster,
				eventRecorder:           tt.fields.eventRecorder,
				kubeInformer:            tt.fields.kubeInformer,
				kubeClient:              tt.fields.kubeClient,
				crdInformer:             tt.fields.crdInformer,
				crdClient:               tt.fields.crdClient,
				cloud:                   tt.fields.cloud,
				clock:                   tt.fields.clock,
				cniMode:                 tt.fields.cniMode,
				vpcID:                   tt.fields.vpcID,
				clusterID:               tt.fields.clusterID,
				subnetSelectionPolicy:   tt.fields.subnetSelectionPolicy,
				bucket:                  tt.fields.bucket,
				eniSyncPeriod:           tt.fields.eniSyncPeriod,
				informerResyncPeriod:    tt.fields.informerResyncPeriod,
				gcPeriod:                tt.fields.gcPeriod,
				buildDataStoreEventChan: tt.fields.buildDataStoreEventChan,
				increasePoolEventChan:   tt.fields.increasePoolEventChan,
			}
			got, got1, err := ipam.getSecurityGroupsFromDefaultIPPool(tt.args.ctx, tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("IPAM.getSecurityGroupsFromDefaultIPPool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IPAM.getSecurityGroupsFromDefaultIPPool() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("IPAM.getSecurityGroupsFromDefaultIPPool() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
