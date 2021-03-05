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
	"fmt"
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
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
)

func setupEnv(ctrl *gomock.Controller) (
	kubernetes.Interface,
	informers.SharedInformerFactory,
	versioned.Interface,
	crdinformers.SharedInformerFactory,
	*mockcloud.MockInterface,
	record.EventBroadcaster,
	record.EventRecorder,
) {
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformer := informers.NewSharedInformerFactory(kubeClient, time.Minute)
	crdClient := crdfake.NewSimpleClientset()
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, time.Minute)
	cloudClient := mockcloud.NewMockInterface(ctrl)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	return kubeClient, kubeInformer,
		crdClient, crdInformer, cloudClient,
		eventBroadcaster, recorder
}

func waitForCacheSync(kubeInformer informers.SharedInformerFactory, crdInformer crdinformers.SharedInformerFactory) {
	nodeInformer := kubeInformer.Core().V1().Nodes().Informer()
	podInformer := kubeInformer.Core().V1().Pods().Informer()
	stsInformer := kubeInformer.Apps().V1().StatefulSets().Informer()
	wepInformer := crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()
	ippoolInformer := crdInformer.Cce().V1alpha1().IPPools().Informer()
	subnetInformer := crdInformer.Cce().V1alpha1().Subnets().Informer()

	kubeInformer.Start(wait.NeverStop)
	crdInformer.Start(wait.NeverStop)

	cache.WaitForNamedCacheSync(
		"cce-ipam",
		wait.NeverStop,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		stsInformer.HasSynced,
		wepInformer.HasSynced,
		ippoolInformer.HasSynced,
		subnetInformer.HasSynced,
	)
}

func Test_buildInstanceIdToNodeNameMap(t *testing.T) {
	type args struct {
		ctx   context.Context
		nodes []*v1.Node
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "",
			args: args{
				ctx: context.TODO(),
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
						Spec: v1.NodeSpec{
							ProviderID: "cce://i-xx",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
						},
						Spec: v1.NodeSpec{
							ProviderID: "cce://i-yy",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node3",
						},
					},
				},
			},
			want: map[string]string{
				"i-xx": "node1",
				"i-yy": "node2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildInstanceIdToNodeNameMap(tt.args.ctx, tt.args.nodes); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildInstanceIdToNodeNameMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_Allocate(t *testing.T) {
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
		clock                 clock.Clock
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
		ctx         context.Context
		name        string
		namespace   string
		containerID string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *v1alpha1.WorkloadEndpoint
		wantErr bool
	}{
		{
			name: "ipam has not synced cache",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				return fields{
					ctrl:           ctrl,
					cacheHasSynced: false,
				}
			}(),
			args:    args{},
			want:    nil,
			wantErr: true,
		},
		{
			name: "node has no eni",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(&v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					eniCache:         map[string][]*enisdk.Eni{},
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					bucket:           ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "create normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(&v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().AddPrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return("10.1.1.1", nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: "default",
					Labels: map[string]string{
						SubnetKey: "",
					},
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:       "10.1.1.1",
					Type:     PodType,
					ENIID:    "eni-test",
					Node:     "test-node",
					UpdateAt: metav1.Time{time.Unix(0, 0)},
				},
			},
			wantErr: false,
		},
		{
			name: "create normal pod failed",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(&v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().AddPrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return("", fmt.Errorf("unknown error")),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			wantErr: true,
		},
		{
			name: "create fix-ip pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				controllerRef := true
				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(&v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-0",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "apps/v1",
								Kind:       "StatefulSet",
								Name:       "foo",
								Controller: &controllerRef,
							},
						},
						Annotations: map[string]string{
							StsPodAnnotationEnableFixIP: EnableFixIPTrue,
						},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				})

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(&v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-0",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						SubnetID: "sbn-test",
						IP:       "10.1.1.1",
						Type:     StsType,
					},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
					cloudClient.EXPECT().AddPrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return("10.1.1.1", nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					eniCache: map[string][]*enisdk.Eni{
						"test-node": {
							{
								EniId:    "eni-test1",
								SubnetId: "sbn-test",
							},
							{
								EniId:        "eni-test2",
								SubnetId:     "sbn-test",
								PrivateIpSet: []enisdk.PrivateIp{{PrivateIpAddress: "10.1.1.2"}},
							},
						},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "foo-0",
				namespace: "default",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo-0",
					Namespace: "default",
					Labels: map[string]string{
						SubnetKey: "sbn-test",
						OwnerKey:  "foo",
					},
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:          "10.1.1.1",
					SubnetID:    "sbn-test",
					Type:        StsType,
					ENIID:       "eni-test1",
					Node:        "test-node",
					UpdateAt:    metav1.Time{time.Unix(0, 0)},
					EnableFixIP: EnableFixIPTrue,
				},
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
				clock:                 tt.fields.clock,
				cniMode:               tt.fields.cniMode,
				vpcID:                 tt.fields.vpcID,
				clusterID:             tt.fields.clusterID,
				subnetSelectionPolicy: tt.fields.subnetSelectionPolicy,
				bucket:                tt.fields.bucket,
				eniSyncPeriod:         tt.fields.eniSyncPeriod,
				informerResyncPeriod:  tt.fields.informerResyncPeriod,
				gcPeriod:              tt.fields.gcPeriod,
			}
			got, err := ipam.Allocate(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Allocate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Allocate() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_Release(t *testing.T) {
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
		clock                 clock.Clock
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
		ctx         context.Context
		name        string
		namespace   string
		containerID string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *v1alpha1.WorkloadEndpoint
		wantErr bool
	}{
		{
			name: "delete normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(&v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want: &v1alpha1.WorkloadEndpoint{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: "default",
				},
				Spec: v1alpha1.WorkloadEndpointSpec{},
			},
			wantErr: false,
		},
		{
			name: "delete a pod that has been deleted",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "delete sts fix-ip pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(&v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-0",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						Type:        StsType,
						EnableFixIP: EnableFixIPTrue,
					},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "foo-0",
				namespace: v1.NamespaceDefault,
			},
			want: &v1alpha1.WorkloadEndpoint{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo-0",
					Namespace: "default",
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					Type:        StsType,
					EnableFixIP: EnableFixIPTrue,
					UpdateAt:    metav1.Time{time.Unix(0, 0)},
				},
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
				clock:                 tt.fields.clock,
				cniMode:               tt.fields.cniMode,
				vpcID:                 tt.fields.vpcID,
				clusterID:             tt.fields.clusterID,
				subnetSelectionPolicy: tt.fields.subnetSelectionPolicy,
				bucket:                tt.fields.bucket,
				eniSyncPeriod:         tt.fields.eniSyncPeriod,
				informerResyncPeriod:  tt.fields.informerResyncPeriod,
				gcPeriod:              tt.fields.gcPeriod,
			}
			got, err := ipam.Release(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Release() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_buildAllocatedCache(t *testing.T) {
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
		clock                 clock.Clock
		cniMode               types.ContainerNetworkMode
		vpcID                 string
		clusterID             string
		subnetSelectionPolicy SubnetSelectionPolicy
		bucket                *ratelimit.Bucket
		eniSyncPeriod         time.Duration
		informerResyncPeriod  time.Duration
		gcPeriod              time.Duration
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(&v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
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
				clock:                 tt.fields.clock,
				cniMode:               tt.fields.cniMode,
				vpcID:                 tt.fields.vpcID,
				clusterID:             tt.fields.clusterID,
				subnetSelectionPolicy: tt.fields.subnetSelectionPolicy,
				bucket:                tt.fields.bucket,
				eniSyncPeriod:         tt.fields.eniSyncPeriod,
				informerResyncPeriod:  tt.fields.informerResyncPeriod,
				gcPeriod:              tt.fields.gcPeriod,
			}
			if err := ipam.buildAllocatedCache(); (err != nil) != tt.wantErr {
				t.Errorf("buildAllocatedCache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPAM_buildENICache(t *testing.T) {
	type fields struct {
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
		clock                 clock.Clock
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
		ctx   context.Context
		nodes []*v1.Node
		enis  []enisdk.Eni
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			fields: fields{},
			args: args{
				ctx: context.TODO(),
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node",
						},
						Spec: v1.NodeSpec{
							ProviderID: "cce://i-xxxxx",
						},
					},
				},
				enis: []enisdk.Eni{
					{
						EniId:      "eni-1",
						Name:       "cce-xxxxx/i-xxxxx/172.19.11.48/adb9f7",
						InstanceId: "i-xxxxx",
						Status:     utileni.ENIStatusInuse,
					},
					{
						EniId:      "eni-2",
						Name:       "test",
						InstanceId: "i-xxxxx",
						Status:     utileni.ENIStatusInuse,
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				clock:                 tt.fields.clock,
				cniMode:               tt.fields.cniMode,
				vpcID:                 tt.fields.vpcID,
				clusterID:             tt.fields.clusterID,
				subnetSelectionPolicy: tt.fields.subnetSelectionPolicy,
				bucket:                tt.fields.bucket,
				eniSyncPeriod:         tt.fields.eniSyncPeriod,
				informerResyncPeriod:  tt.fields.informerResyncPeriod,
				gcPeriod:              tt.fields.gcPeriod,
			}
			if err := ipam.buildENICache(tt.args.ctx, tt.args.nodes, tt.args.enis); (err != nil) != tt.wantErr {
				t.Errorf("buildENICache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPAM_updateIPPoolStatus(t *testing.T) {
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
		clock                 clock.Clock
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
		node *v1.Node
		enis []enisdk.Eni
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
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(&v1alpha1.IPPool{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ippool-test",
					},
					Spec:   v1alpha1.IPPoolSpec{},
					Status: v1alpha1.IPPoolStatus{},
				})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:              ctrl,
					lock:              sync.RWMutex{},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clusterID:         "cce-xxxxx",
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx: context.TODO(),
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				},
				enis: []enisdk.Eni{
					{
						EniId:      "eni-1",
						Name:       "cce-xxxxx/i-xxxxx/172.19.11.48/adb9f7",
						InstanceId: "i-xxxxx",
						Status:     utileni.ENIStatusInuse,
					},
					{
						EniId:      "eni-2",
						Name:       "test",
						InstanceId: "i-xxxxx",
						Status:     utileni.ENIStatusInuse,
					},
				},
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
				clock:                 tt.fields.clock,
				cniMode:               tt.fields.cniMode,
				vpcID:                 tt.fields.vpcID,
				clusterID:             tt.fields.clusterID,
				subnetSelectionPolicy: tt.fields.subnetSelectionPolicy,
				bucket:                tt.fields.bucket,
				eniSyncPeriod:         tt.fields.eniSyncPeriod,
				informerResyncPeriod:  tt.fields.informerResyncPeriod,
				gcPeriod:              tt.fields.gcPeriod,
			}
			if err := ipam.updateIPPoolStatus(tt.args.ctx, tt.args.node, tt.args.enis); (err != nil) != tt.wantErr {
				t.Errorf("updateIPPoolStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
