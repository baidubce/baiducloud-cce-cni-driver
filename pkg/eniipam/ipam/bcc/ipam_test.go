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
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"reflect"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	mocksubnet "github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet/mock"
	datastorev1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/ipcache"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/juju/ratelimit"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
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
	kubeInformer := informers.NewSharedInformerFactory(kubeClient, time.Second)
	crdClient := crdfake.NewSimpleClientset()
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, time.Second)
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
	stopChan := wait.NeverStop
	kubeInformer.Core().V1().Nodes().Informer()
	kubeInformer.Core().V1().Pods().Informer()
	kubeInformer.Apps().V1().StatefulSets().Informer()
	crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()
	crdInformer.Cce().V1alpha1().IPPools().Informer()
	crdInformer.Cce().V1alpha1().Subnets().Informer()
	crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Informer()

	kubeInformer.Start(stopChan)
	crdInformer.Start(stopChan)
	__waitForCacheSync(kubeInformer, crdInformer, stopChan)
}

func __waitForCacheSync(kubeInformer informers.SharedInformerFactory, crdInformer crdinformers.SharedInformerFactory, stopChan <-chan struct{}) {
	time.Sleep(time.Microsecond)
	kubeInformer.WaitForCacheSync(stopChan)
	crdInformer.WaitForCacheSync(stopChan)
	time.Sleep(time.Microsecond)
}

func Test_buildInstanceIdToNodeNameMap(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	type fields struct {
		ctrl                  *gomock.Controller
		eniCache              map[string][]*enisdk.Eni
		privateIPNumCache     map[string]int
		cacheHasSynced        bool
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
		datastore             *datastorev1.DataStore
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

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
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
			name: "create normal pod without allocate ip",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})

				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				// gomock.InOrder(
				// 	cloudClient.EXPECT().AddPrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return("10.1.1.1", nil),
				// )

				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("test-node", "i-xxx")
				ds.AddENIToStore("test-node", "eni-test")
				ds.AddPrivateIPToStore("test-node", "eni-test", "10.1.1.1", false)

				return fields{
					ctrl: ctrl,
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					privateIPNumCache: map[string]int{},
					datastore:         ds,
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
						ipamgeneric.WepLabelSubnetIDKey:     "",
						ipamgeneric.WepLabelInstanceTypeKey: "bcc",
					},
					Finalizers: []string{ipamgeneric.WepFinalizer},
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:       "10.1.1.1",
					Type:     ipamgeneric.WepTypePod,
					ENIID:    "eni-test",
					Node:     "test-node",
					UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
				},
			},
			wantErr: false,
		},
		{
			name: "create fix-ip pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				controllerRef := true
				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
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
				}, metav1.CreateOptions{})

				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(),
					&v1alpha1.WorkloadEndpoint{
						TypeMeta: metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo-0",
						},
						Spec: v1alpha1.WorkloadEndpointSpec{
							SubnetID: "sbn-test",
							IP:       "10.1.1.1",
							Type:     ipamgeneric.WepTypeSts,
						},
					}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
					cloudClient.EXPECT().BatchAddPrivateIP(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{"10.1.1.1"}, nil),
				)

				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("test-node", "i-xxx")
				ds.AddENIToStore("test-node", "eni-test")
				ds.AddPrivateIPToStore("test-node", "eni-test", "10.1.1.1", false)

				return fields{
					ctrl: ctrl,
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
					privateIPNumCache: map[string]int{},
					datastore:         ds,
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
						ipamgeneric.WepLabelSubnetIDKey:     "sbn-test",
						ipamgeneric.WepLabelStsOwnerKey:     "foo",
						ipamgeneric.WepLabelInstanceTypeKey: "bcc",
					},
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:          "10.1.1.1",
					SubnetID:    "sbn-test",
					Type:        ipamgeneric.WepTypeSts,
					ENIID:       "eni-test1",
					Node:        "test-node",
					UpdateAt:    metav1.Time{Time: time.Unix(0, 0)},
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
			eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
			for key, v := range tt.fields.eniCache {

				eniCache.Append(key, v...)
			}

			ipam := &IPAM{
				eniCache:              eniCache,
				privateIPNumCache:     tt.fields.privateIPNumCache,
				cacheHasSynced:        tt.fields.cacheHasSynced,
				allocated:             ipcache.NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
				addIPBackoffCache:     ipcache.NewCacheMap[*wait.Backoff](),
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
				datastore:             tt.fields.datastore,
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
	t.Parallel()
	type fields struct {
		ctrl                  *gomock.Controller
		eniCache              map[string][]*enisdk.Eni
		privateIPNumCache     map[string]int
		cacheHasSynced        bool
		allocated             map[string]*v1alpha1.WorkloadEndpoint
		datastore             *datastorev1.DataStore
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
		idleIPPoolMaxSize     int
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

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("test-node", "i-xxx")
				ds.AddENIToStore("test-node", "eni-test")

				gomock.InOrder(
					cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl: ctrl,
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
					datastore:         ds,
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
				Spec: v1alpha1.WorkloadEndpointSpec{
					UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
				},
			},
			wantErr: false,
		},
		{
			name: "delete a pod that has been deleted",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("test-node", "i-xxx")
				ds.AddENIToStore("test-node", "eni-test")

				return fields{
					ctrl: ctrl,
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					datastore:         ds,
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

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-0",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						Type:        ipamgeneric.WepTypeSts,
						EnableFixIP: EnableFixIPTrue,
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("test-node", "i-xxx")
				ds.AddENIToStore("test-node", "eni-test")

				return fields{
					ctrl: ctrl,
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					datastore:         ds,
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
					Type:        ipamgeneric.WepTypeSts,
					EnableFixIP: EnableFixIPTrue,
					UpdateAt:    metav1.Time{Time: time.Unix(0, 0)},
				},
			},
			wantErr: false,
		}, {
			name: "delete ip when pod cross subnet error",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("delete ip error")).AnyTimes()

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-0",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						Type:                    ipamgeneric.WepTypePod,
						EnableFixIP:             "false",
						SubnetTopologyReference: "psts-test",
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("test-node", "i-xxx")
				ds.AddENIToStore("test-node", "eni-test")

				return fields{
					ctrl: ctrl,
					eniCache: map[string][]*enisdk.Eni{
						"test-node": {{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					datastore:         ds,
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
			wantErr: true,
		}, {
			name: "realse ip to poll",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("delete ip error")).AnyTimes()

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-0",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						Type: ipamgeneric.WepTypePod,
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("test-node", "i-xxx")
				ds.AddENIToStore("test-node", "eni-test")

				return fields{
					ctrl: ctrl,
					eniCache: map[string][]*enisdk.Eni{
						"test-node": {{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.WorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					datastore:         ds,
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					bucket:            ratelimit.NewBucket(100, 100),
					idleIPPoolMaxSize: 100,
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
					Namespace: corev1.NamespaceDefault,
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					Type:     ipamgeneric.WepTypePod,
					UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
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
			ipam, _ := NewIPAM(tt.fields.kubeClient,
				tt.fields.crdClient,
				tt.fields.kubeInformer,
				tt.fields.crdInformer,
				tt.fields.cloud,
				tt.fields.cniMode,
				tt.fields.vpcID,
				tt.fields.clusterID,
				tt.fields.subnetSelectionPolicy,
				5,
				10,
				0,
				0,
				1,
				tt.fields.eniSyncPeriod,
				tt.fields.gcPeriod,
				true,
			)
			ipamServer := ipam.(*IPAM)
			ipamServer.cacheHasSynced = true
			eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
			for key, v := range tt.fields.eniCache {

				eniCache.Append(key, v...)
			}
			ipamServer.eniCache = eniCache
			ipamServer.clock = clock.NewFakeClock(time.Unix(0, 0))
			if tt.fields.idleIPPoolMaxSize > 0 {
				ipamServer.idleIPPoolMaxSize = tt.fields.idleIPPoolMaxSize
			}

			got, err := ipam.Release(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got != nil {
				got.Spec.UpdateAt = metav1.Time{Time: time.Unix(0, 0)}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Release() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func startInformers(kubeInformer informers.SharedInformerFactory, crdInformer crdinformers.SharedInformerFactory) {
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

func TestIPAM_gcLeakedIP(t *testing.T) {
	t.Parallel()
	type fields struct {
		ctrl                  *gomock.Controller
		datastore             *datastorev1.DataStore
		eniCache              map[string][]*enisdk.Eni
		possibleLeakedIPCache map[eniAndIPAddrKey]time.Time
		allocated             map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced        bool
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
		bucket                *ratelimit.Bucket
		batchAddIPNum         int
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
			name: "normal",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				// add two pod for test environment
				_, _ = kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
					Status: v1.PodStatus{
						PodIP: "10.1.1.2",
					},
				}, metav1.CreateOptions{})
				_, _ = kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox2",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})
				// add two wep for test environment
				_, _ = crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						IP:       "10.1.1.2",
						ENIID:    "test-node-id",
						Node:     "test-node",
						UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
					},
				}, metav1.CreateOptions{})
				_, _ = crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox2",
						Namespace: "default",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						IP:       "10.1.1.3",
						ENIID:    "test-node-id",
						Node:     "test-node",
						UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
					},
				}, metav1.CreateOptions{})
				startInformers(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), "10.1.1.4", "eni-test").Return(nil),
				)

				return fields{
					ctrl: ctrl,
					datastore: func() *datastorev1.DataStore {
						dt := datastorev1.NewDataStore()
						err := dt.AddNodeToStore("test-node", "test-node-id")
						if err != nil {
							return nil
						}
						err = dt.AddPrivateIPToStore("test-node", "test-node-id", "10.1.1.2", true)
						if err != nil {
							return nil
						}
						err = dt.AddPrivateIPToStore("test-node", "test-node-id", "10.1.1.3", true)
						if err != nil {
							return nil
						}
						err = dt.AddPrivateIPToStore("test-node", "test-node-id", "10.1.1.4", true)
						if err != nil {
							return nil
						}
						return dt
					}(),
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
							PrivateIpSet: []enisdk.PrivateIp{
								{
									PublicIpAddress:  "",
									Primary:          false,
									PrivateIpAddress: "10.1.1.2",
								},
								{
									PublicIpAddress:  "",
									Primary:          false,
									PrivateIpAddress: "10.1.1.3",
								},
								{
									PublicIpAddress:  "",
									Primary:          false,
									PrivateIpAddress: "10.1.1.4",
								},
							},
						}},
					},
					possibleLeakedIPCache: map[eniAndIPAddrKey]time.Time{},
					cacheHasSynced:        true,
					eventBroadcaster:      brdcaster,
					eventRecorder:         recorder,
					kubeInformer:          kubeInformer,
					kubeClient:            kubeClient,
					crdInformer:           crdInformer,
					crdClient:             crdClient,
					cloud:                 cloudClient,
					allocated:             map[string]*v1alpha1.WorkloadEndpoint{},
					bucket:                ratelimit.NewBucket(100, 100),
				}
			}(),
			args:    args{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
			for key, v := range tt.fields.eniCache {

				eniCache.Append(key, v...)
			}
			ipam := &IPAM{
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
				datastore:             tt.fields.datastore,
				eniCache:              eniCache,
				possibleLeakedIPCache: tt.fields.possibleLeakedIPCache,
				allocated:             ipcache.NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
				bucket:                tt.fields.bucket,
				batchAddIPNum:         tt.fields.batchAddIPNum,
				cacheHasSynced:        tt.fields.cacheHasSynced,
				gcPeriod:              tt.fields.gcPeriod,
				clock:                 clock.NewFakeClock(time.Unix(0, 0)),
				privateIPNumCache: map[string]int{
					"eni-test": 1,
				},
			}
			// build leaked ip cache
			err := ipam.gcLeakedIP(tt.args.ctx)
			// mock timeout
			gcPruneExpiredDuration := ipamgeneric.LeakedPrivateIPExpiredTimeout + (1 * time.Minute)
			for key, recordTime := range ipam.possibleLeakedIPCache {
				ipam.possibleLeakedIPCache[key] = recordTime.Add(-gcPruneExpiredDuration)
			}
			// prune leaked ip
			err = ipam.gcLeakedIP(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("gcLeakedIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_buildAllocatedCache(t *testing.T) {
	t.Parallel()
	type fields struct {
		ctrl                  *gomock.Controller
		eniCache              map[string][]*enisdk.Eni
		privateIPNumCache     map[string]int
		cacheHasSynced        bool
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

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl: ctrl,
					eniCache: map[string][]*enisdk.Eni{
						"test-node": []*enisdk.Eni{{
							EniId: "eni-test",
						}},
					},
					cacheHasSynced:    true,
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
			eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
			for key, v := range tt.fields.eniCache {

				eniCache.Append(key, v...)
			}
			ipam := &IPAM{
				eniCache:              eniCache,
				privateIPNumCache:     tt.fields.privateIPNumCache,
				cacheHasSynced:        tt.fields.cacheHasSynced,
				allocated:             ipcache.NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
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
			if err := ipam.buildAllocatedCache(context.TODO()); (err != nil) != tt.wantErr {
				t.Errorf("buildAllocatedCache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPAM_buildInuseENICache(t *testing.T) {
	t.Parallel()
	type fields struct {
		eniCache              map[string][]*enisdk.Eni
		privateIPNumCache     map[string]int
		cacheHasSynced        bool
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
			eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
			for key, v := range tt.fields.eniCache {

				eniCache.Append(key, v...)
			}
			ipam := &IPAM{
				eniCache:              eniCache,
				privateIPNumCache:     tt.fields.privateIPNumCache,
				cacheHasSynced:        tt.fields.cacheHasSynced,
				allocated:             ipcache.NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
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
			if err := ipam.buildInuseENICache(tt.args.ctx, tt.args.nodes, tt.args.enis); (err != nil) != tt.wantErr {
				t.Errorf("buildInuseENICache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPAM_updateIPPoolStatus(t *testing.T) {
	t.Parallel()
	type fields struct {
		ctrl                  *gomock.Controller
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
		ctx        context.Context
		node       *v1.Node
		instanceID string
		enis       []enisdk.Eni
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

				crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.IPPool{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ippool-test",
					},
					Spec:   v1alpha1.IPPoolSpec{},
					Status: v1alpha1.IPPoolStatus{},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:              ctrl,
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
				instanceID: "i-xxxxx",
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
			eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
			for key, v := range tt.fields.eniCache {

				eniCache.Append(key, v...)
			}
			ipam := &IPAM{
				eniCache:              eniCache,
				privateIPNumCache:     tt.fields.privateIPNumCache,
				cacheHasSynced:        tt.fields.cacheHasSynced,
				allocated:             ipcache.NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
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
			if err := ipam.updateIPPoolStatus(tt.args.ctx, tt.args.node, tt.args.instanceID, tt.args.enis); (err != nil) != tt.wantErr {
				t.Errorf("updateIPPoolStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPAM_canAllocateIP(t *testing.T) {
	t.Parallel()
	type fields struct {
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
		ctx      context.Context
		nodeName string
		enis     []*enisdk.Eni
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "node with no eni",
			fields: fields{
				datastore: &datastorev1.DataStore{},
			},
			args: args{
				ctx:      context.TODO(),
				nodeName: "xxx",
				enis:     []*enisdk.Eni{},
			},
			want: false,
		},
		{
			name: "node with enis",
			fields: func() fields {
				ds := datastorev1.NewDataStore()
				ds.AddNodeToStore("xxx", "i-xxx")
				ds.AddENIToStore("xxx", "eni-xxx")
				ds.AddPrivateIPToStore("xxx", "eni-xxx", "1.1.1.1", false)

				return fields{
					datastore: ds,
				}
			}(),
			args: args{
				ctx:      context.TODO(),
				nodeName: "xxx",
				enis:     []*enisdk.Eni{{EniId: "eni-xxx"}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
			for key, v := range tt.fields.eniCache {

				eniCache.Append(key, v...)
			}
			ipam := &IPAM{
				eniCache:                eniCache,
				privateIPNumCache:       tt.fields.privateIPNumCache,
				possibleLeakedIPCache:   tt.fields.possibleLeakedIPCache,
				addIPBackoffCache:       ipcache.NewCacheMap[*wait.Backoff](),
				cacheHasSynced:          tt.fields.cacheHasSynced,
				allocated:               ipcache.NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
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
			if got := ipam.canAllocateIP(tt.args.ctx, tt.args.nodeName, tt.args.enis); got != tt.want {
				t.Errorf("IPAM.canAllocateIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

// mock a ipam server
func mockIPAM(t *testing.T, stopChan chan struct{}) *IPAM {
	ctrl := gomock.NewController(t)
	kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, _, _ := setupEnv(ctrl)
	ipam, _ := NewIPAM(kubeClient,
		crdClient,
		kubeInformer,
		crdInformer,
		cloudClient,
		"vpc-cni-secondry-ip-veth",
		"vpcID",
		"clusterID",
		SubnetSelectionPolicyMostFreeIP,
		5,
		10,
		0,
		0,
		1,
		300*time.Second,
		300*time.Second,
		true,
	)
	ipamServer := ipam.(*IPAM)
	ipamServer.cacheHasSynced = true
	eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
	eniCache.Append("test-node", &enisdk.Eni{
		EniId: "eni-test",
	})
	ipamServer.eniCache = eniCache
	ipamServer.clock = clock.NewFakeClock(time.Unix(0, 0))
	return ipam.(*IPAM)
}

// mock bceclient
func MockClinet(c cloud.Interface, f func(*mockcloud.MockInterface)) {
	f(c.(*mockcloud.MockInterface))
}

type IPAMTest struct {
	suite.Suite
	ipam        *IPAM
	wantErr     bool
	want        *networkingv1alpha1.WorkloadEndpoint
	ctx         context.Context
	name        string
	namespace   string
	containerID string
	podLabel    labels.Set
	stopChan    chan struct{}
}

// 每次测试前设置上下文
func (suite *IPAMTest) SetupTest() {
	suite.stopChan = make(chan struct{})
	suite.ipam = mockIPAM(suite.T(), suite.stopChan)
	suite.ctx = context.TODO()
	suite.name = "busybox"
	suite.namespace = corev1.NamespaceDefault
	suite.podLabel = labels.Set{
		"k8s.io/app": "busybox",
	}

	runtime.ReallyCrash = false
}

func (suite *IPAMTest) TearDownTest() {
	close(suite.stopChan)
}

func (suite *IPAMTest) TestIPAMRun() {
	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.ListENIs(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("list error")).AnyTimes()
	go func() {
		err := suite.ipam.Run(suite.ctx, suite.stopChan)
		suite.Assert().Nil(err)
	}()
	time.Sleep(3 * time.Second)
}

func (suite *IPAMTest) Test_tryToGetIPFromCache() {
	var (
		nodeName   = "test-node"
		instanceID = "i-xxx"
		eniID      = "test-eni"
		subnetID   = "test-subnet"
		ip         = "192.168.1.109"
		subnetInfo = &vpc.Subnet{
			AvailableIp: 10,
		}
		eniInfo = &enisdk.Eni{
			PrivateIpSet: []enisdk.PrivateIp{
				{
					Primary:          true,
					PrivateIpAddress: "192.168.1.100",
				},
				{
					Primary:          false,
					PrivateIpAddress: "192.168.1.107",
				},
			},
		}
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}
		enis = []*enisdk.Eni{
			{
				EniId:    eniID,
				SubnetId: subnetID,
				ZoneName: "zoneF",
			},
		}
	)

	storeErr := suite.ipam.datastore.AddNodeToStore(nodeName, instanceID)
	suite.Assert().Nil(storeErr)
	storeErr = suite.ipam.datastore.AddENIToStore(node.Name, eniID)
	suite.Assert().Nil(storeErr)

	// assertEniCanIncreasePool
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(suite.ctx, subnetID).Return(subnetInfo, nil).Times(2)

	_, nodeErr := suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				"cce.io/max-ip-per-eni": "8",
			},
		},
	}, metav1.CreateOptions{})
	suite.Assert().Nil(nodeErr)
	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().StatENI(suite.ctx, eniID).Return(eniInfo, nil).Times(2)
	// end of assertEniCanIncreasePool

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIP(gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any()).Return([]string{"192.168.1.109"}, nil)

	allocatedIP, allocatedENI, err := suite.ipam.tryToAllocateIPFromCache(suite.ctx, node, enis, getDeadline())
	suite.Assert().Equal(ip, allocatedIP)
	suite.Assert().Equal(eniID, allocatedENI.EniId)
	suite.Assert().Nil(err)
}

func (suite *IPAMTest) Test_tryToGetIPFromCache_error() {
	var (
		nodeName   = "test-node"
		instanceID = "i-xxx"
		eniID      = "test-eni"
		subnetID   = "test-subnet"
		subnetInfo = &vpc.Subnet{
			AvailableIp: 0,
		}
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}
		enis = []*enisdk.Eni{
			{
				EniId:    eniID,
				SubnetId: subnetID,
				ZoneName: "zoneF",
			},
		}
	)

	storeErr := suite.ipam.datastore.AddNodeToStore(nodeName, instanceID)
	suite.Assert().Nil(storeErr)
	storeErr = suite.ipam.datastore.AddENIToStore(node.Name, eniID)
	suite.Assert().Nil(storeErr)

	// assertEniCanIncreasePool
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(suite.ctx, subnetID).Return(subnetInfo, nil)
	// end of assertEniCanIncreasePool

	_, _, err := suite.ipam.tryToAllocateIPFromCache(suite.ctx, node, enis, getDeadline())
	suite.Assert().ErrorContains(err, "has no available ip")
}

func (suite *IPAMTest) Test_tryAllocateIPForFixIPPodFailRateLimit() {
	var (
		eni = &enisdk.Eni{
			EniId: "eni-test",
		}
		wep  = data.MockFixedWorkloadEndpoint()
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
			},
		}
	)
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("RateLimit"))
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIP(gomock.Any(), gomock.Len(1), 0, "eni-test").Return([]string{}, fmt.Errorf("RateLimit"))

	_, _, err := suite.ipam.allocateFixedIPFromCloud(suite.ctx, node, []*enisdk.Eni{eni}, wep, getDeadline())
	suite.Error(err, "allocation ip error")

}

func (suite *IPAMTest) Test_tryAllocateIPForFixIPPodFailSubnetHasNoMoreIpException() {
	var (
		nodeName = "test-node"
		eniID    = "test-eni"
		subnetID = "test-subnet"
		eni      = &enisdk.Eni{
			EniId:    eniID,
			SubnetId: subnetID,
		}
		wep  = data.MockFixedWorkloadEndpoint()
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}
	)
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("SubnetHasNoMoreIpException"))
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIP(gomock.Any(), gomock.Len(1), 0,
		eniID).Return([]string{}, fmt.Errorf("SubnetHasNoMoreIpException"))
	// I don't know how to mock sbnCtl, because it implements multi interface
	oldSbnCtl := suite.ipam.sbnCtl
	suite.ipam.sbnCtl = mocksubnet.NewMockSubnetControl(gomock.NewController(suite.T()))
	defer func() {
		suite.ipam.sbnCtl = oldSbnCtl
	}()
	suite.ipam.sbnCtl.(*mocksubnet.MockSubnetControl).EXPECT().DeclareSubnetHasNoMoreIP(suite.ctx, subnetID,
		true).Return(nil)
	_, _, err := suite.ipam.allocateFixedIPFromCloud(suite.ctx, node, []*enisdk.Eni{eni}, wep, getDeadline())
	suite.Error(err, "allocation ip error")
}

func (suite *IPAMTest) Test_tryAllocateIPForFixIPPodFailPrivateIpInUseException() {
	var (
		eni = &enisdk.Eni{
			EniId: "eni-test",
		}
		wep  = data.MockFixedWorkloadEndpoint()
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
			},
		}
	)
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("RateLimit"))
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIP(gomock.Any(), gomock.Len(1), 0, "eni-test").Return([]string{}, fmt.Errorf("PrivateIpInUseException")).AnyTimes()

	_, _, err := suite.ipam.allocateFixedIPFromCloud(suite.ctx, node, []*enisdk.Eni{eni}, wep, getDeadline())
	suite.Error(err, "allocation ip error")

}

// normal case
func (suite *IPAMTest) Test_handleIncreasePoolEvent() {
	var (
		nodeName   = "test-node"
		instanceID = "i-xxx"
		eniID      = "test-eni"
		subnetID   = "test-subnet"
		e          = &event{
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			},
			enis: []*enisdk.Eni{{
				EniId:    eniID,
				SubnetId: subnetID,
			}},
			passive: false,
			ctx:     suite.ctx,
		}
		subnetInfo = &vpc.Subnet{
			AvailableIp: 10,
		}
		eniInfo = &enisdk.Eni{
			PrivateIpSet: []enisdk.PrivateIp{
				{
					Primary:          true,
					PrivateIpAddress: "192.168.1.100",
				},
				{
					Primary:          false,
					PrivateIpAddress: "192.168.1.107",
				},
			},
		}
		ipam = suite.ipam
	)

	ipam.batchAddIPNum = 1
	ipam.idleIPPoolMinSize = 2

	err := suite.ipam.datastore.AddNodeToStore(nodeName, instanceID)
	suite.Assert().Nil(err)
	err = suite.ipam.datastore.AddENIToStore(nodeName, eniID)
	suite.Assert().Nil(err)
	err = suite.ipam.datastore.AddPrivateIPToStore(nodeName, eniID, "192.168.1.107", false)
	suite.Assert().Nil(err)

	// assertEniCanIncreasePool
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(suite.ctx, subnetID).Return(subnetInfo, nil)

	_, nodeErr := suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				"cce.io/max-ip-per-eni": "8",
			},
		},
	}, metav1.CreateOptions{})
	suite.Assert().Nil(nodeErr)

	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().StatENI(suite.ctx, eniID).Return(eniInfo, nil)
	// end of assertEniCanIncreasePool

	// batchAllocateIPWithBackoff
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().
		BatchAddPrivateIP(suite.ctx, gomock.Len(0), ipam.batchAddIPNum, eniID).
		Return([]string{"192.168.1.108"}, nil).AnyTimes()

	ch := make(chan *event, 1)
	ch <- e
	close(ch)

	ips, err := suite.ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
	suite.Assert().Nil(err)
	suite.Assert().Equal(1, len(ips))

	ipam.handleIncreasePoolEvent(suite.ctx, nodeName, ch)

	ips, err = suite.ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
	suite.Assert().Nil(err)
	suite.Assert().Equal(2, len(ips))
}

// exception case
func (suite *IPAMTest) Test_handleIncreasePoolEvent_canIgnoreEvent() {
	var (
		nodeName   = "test-node"
		instanceID = "i-xxx"
		eniID      = "test-eni"
		subnetID   = "test-subnet"
		activeEvt  = &event{
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			},
			enis: []*enisdk.Eni{{
				EniId:    eniID,
				SubnetId: subnetID,
			}},
			passive: false,
			ctx:     suite.ctx,
		}
		passiveEvt = &event{
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			},
			enis: []*enisdk.Eni{{
				EniId:    eniID,
				SubnetId: subnetID,
			}},
			passive: true,
			ctx:     suite.ctx,
		}
		ipam = suite.ipam
	)

	ipam.batchAddIPNum = 1
	ipam.idleIPPoolMinSize = 1

	err := suite.ipam.datastore.AddNodeToStore(nodeName, instanceID)
	suite.Assert().Nil(err)
	err = suite.ipam.datastore.AddENIToStore(nodeName, eniID)
	suite.Assert().Nil(err)
	err = suite.ipam.datastore.AddPrivateIPToStore(nodeName, eniID, "192.168.1.107", false)
	suite.Assert().Nil(err)

	ch := make(chan *event, 10)
	ch <- passiveEvt
	ch <- activeEvt
	close(ch)

	ips, err := suite.ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
	suite.Assert().Nil(err)
	suite.Assert().Equal(1, len(ips))

	ipam.handleIncreasePoolEvent(suite.ctx, nodeName, ch)

	ips, err = suite.ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
	suite.Assert().Nil(err)
	suite.Assert().Equal(1, len(ips))
}

func (suite *IPAMTest) Test_handleIncreasePoolEvent_eniCannotIncreasePool() {
	var (
		nodeName   = "test-node"
		instanceID = "i-xxx"
		eniID      = "test-eni"
		subnetID   = "test-subnet"
		e          = &event{
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			},
			enis: []*enisdk.Eni{{
				EniId:    eniID,
				SubnetId: subnetID,
			}},
			passive: false,
			ctx:     suite.ctx,
		}
		subnetInfo = &vpc.Subnet{
			AvailableIp: 0,
		}
		eniInfo = &enisdk.Eni{
			PrivateIpSet: []enisdk.PrivateIp{
				{
					Primary:          true,
					PrivateIpAddress: "192.168.1.100",
				},
				{
					Primary:          false,
					PrivateIpAddress: "192.168.1.107",
				},
			},
		}
		ipam = suite.ipam
	)

	ipam.batchAddIPNum = 1
	ipam.idleIPPoolMinSize = 2

	err := suite.ipam.datastore.AddNodeToStore(nodeName, instanceID)
	suite.Assert().Nil(err)
	err = suite.ipam.datastore.AddENIToStore(nodeName, eniID)
	suite.Assert().Nil(err)
	err = suite.ipam.datastore.AddPrivateIPToStore(nodeName, eniID, "192.168.1.107", false)
	suite.Assert().Nil(err)

	ch := make(chan *event)
	go ipam.handleIncreasePoolEvent(suite.ctx, nodeName, ch)

	// 1. subnet has no available ip
	// 1.1 prepare
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(suite.ctx, subnetID).Return(subnetInfo, nil)

	// 1.2 start
	ch <- e

	// 1.3 wait then check
	time.Sleep(10 * time.Millisecond)
	ips, err := suite.ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
	suite.Assert().Nil(err)
	suite.Assert().Equal(1, len(ips))

	// 2. node cannot attach more ip due to memory
	// 2.1 prepare
	subnetInfo.AvailableIp = 2
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(suite.ctx, subnetID).Return(subnetInfo, nil)
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().StatENI(suite.ctx, eniID).Return(eniInfo, nil)

	suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				"cce.io/max-ip-per-eni": "1",
			},
		},
	}, metav1.CreateOptions{})

	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)

	// 2.2 start
	ch <- e

	// 2.3 wait then check
	time.Sleep(10 * time.Millisecond)
	ips, err = suite.ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
	suite.Assert().Nil(err)
	suite.Assert().Equal(1, len(ips))

	close(ch)
}

func TestIPAM(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(IPAMTest))
}

func getDeadline() time.Time {
	return time.Now().Add(allocateIPTimeout)
}

func (suite *IPAMTest) Test_allocateIPForFixedIPPod() {
	var (
		nodeName    = "test-node"
		instanceID  = "i-xxxx"
		containerID = "test-con"
		oldEniID    = "test-eni-1"
		newEniID    = "test-eni-2"
		subnetID    = "test-subnet"
		ip          = "111.111.222.222"
		wepName     = "test-pod"
		node        = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}
		pod  = &corev1.Pod{}
		enis = []*enisdk.Eni{
			{
				EniId:    newEniID,
				SubnetId: subnetID,
				ZoneName: "zoneF",
			},
		}
		wep = &v1alpha1.WorkloadEndpoint{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      wepName,
				Namespace: v1.NamespaceDefault,
			},
			Spec: networkingv1alpha1.WorkloadEndpointSpec{
				Node:  nodeName,
				ENIID: oldEniID,
			},
		}
		subnetInfo = &vpc.Subnet{
			AvailableIp: 10,
		}
	)
	// init datastore
	storeErr := suite.ipam.datastore.AddNodeToStore(nodeName, instanceID)
	suite.Assert().Nil(storeErr)

	// allocateFixedIPFromCloud
	gomock.InOrder(
		suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().
			DeletePrivateIP(suite.ctx, gomock.Any(), oldEniID).Return(fmt.Errorf("RateLimit")),

		suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().
			BatchAddPrivateIP(gomock.Any(), gomock.Len(1), 0, newEniID).
			Return([]string{}, fmt.Errorf("RateLimit")),
	)

	// assertEniCanIncreasePool
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(suite.ctx, subnetID).Return(subnetInfo, nil).Times(2)

	_, nodeErr := suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				"cce.io/max-ip-per-eni": "8",
			},
		},
	}, metav1.CreateOptions{})
	suite.Assert().Nil(nodeErr)

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().StatENI(suite.ctx, newEniID).Return(enis[0], nil).Times(2)
	// end of assertEniCanIncreasePool

	// handleIncreasePoolEvent
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIP(suite.ctx, gomock.Len(0), 1, newEniID).
		Return([]string{ip}, nil)

	_, wepErr := suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(),
		&v1alpha1.WorkloadEndpoint{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: wepName,
			},
			Spec: v1alpha1.WorkloadEndpointSpec{
				SubnetID: subnetID,
				IP:       "10.1.1.1",
				Type:     ipamgeneric.WepTypeSts,
			},
		}, metav1.CreateOptions{})
	suite.Assert().Nil(wepErr)

	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)
	newWep, err := suite.ipam.allocateIPForFixedIPPod(suite.ctx, node, pod, containerID, enis, wep, getDeadline())
	suite.Assert().Nil(err)
	suite.Assert().Equal(ip, newWep.Spec.IP)
	suite.Assert().Equal(newEniID, newWep.Spec.ENIID)
}

func (suite *IPAMTest) Test_checkIdleIPPool() {
	var (
		nodeWithEni    = "test-node-1"
		instanceID1    = "test-inst-1"
		nodeWithoutEni = "test-node-2"
		instanceID2    = "test-inst-2"
		subnetID       = "test-subnet"
		subnetInfo     = &vpc.Subnet{
			AvailableIp: 0,
		}
	)
	// init node
	_, nodeErr1 := suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				v1.LabelInstanceType: "BCC",
			},
			Name: nodeWithEni,
			Annotations: map[string]string{
				"cce.io/max-ip-per-eni": "8",
			},
		},
	}, metav1.CreateOptions{})
	suite.Assert().Nil(nodeErr1)

	_, nodeErr2 := suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				v1.LabelInstanceType: "BCC",
			},
			Name: nodeWithoutEni,
			Annotations: map[string]string{
				"cce.io/max-ip-per-eni": "8",
			},
		},
	}, metav1.CreateOptions{})
	suite.Assert().Nil(nodeErr2)

	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)
	// init datastore
	storeErr := suite.ipam.datastore.AddNodeToStore(nodeWithEni, instanceID1)
	suite.Assert().Nil(storeErr)
	storeErr2 := suite.ipam.datastore.AddNodeToStore(nodeWithoutEni, instanceID2)
	suite.Assert().Nil(storeErr2)
	// init eni cache
	suite.ipam.eniCache.Append(nodeWithEni, &enisdk.Eni{
		EniId:    "eni-test",
		SubnetId: subnetID,
	})
	// mock cloud
	// assertEniCanIncreasePool
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DescribeSubnet(gomock.Any(), subnetID).
		Return(subnetInfo, nil).AnyTimes()

	_, err := suite.ipam.checkIdleIPPool()
	suite.Assert().Nil(err)
	// wait handleIncreasePoolEvent finish
	time.Sleep(10 * time.Millisecond)
	_, ok1 := suite.ipam.increasePoolEventChan[nodeWithEni]
	suite.Assert().True(ok1)
	_, ok2 := suite.ipam.increasePoolEventChan[nodeWithoutEni]
	suite.Assert().False(ok2)
}
