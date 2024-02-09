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

package crossvpceni

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/scheme"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func setupEnv(ctrl *gomock.Controller) (
	kubernetes.Interface,
	informers.SharedInformerFactory,
	versioned.Interface,
	crdinformers.SharedInformerFactory,
	record.EventBroadcaster,
	record.EventRecorder,
) {
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformer := informers.NewSharedInformerFactory(kubeClient, time.Minute)
	crdClient := crdfake.NewSimpleClientset()
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, time.Minute)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events("default"),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	return kubeClient, kubeInformer,
		crdClient, crdInformer,
		eventBroadcaster, recorder
}

func waitForCacheSync(kubeInformer informers.SharedInformerFactory, crdInformer crdinformers.SharedInformerFactory) {
	nodeInformer := kubeInformer.Core().V1().Nodes().Informer()
	podInformer := kubeInformer.Core().V1().Pods().Informer()
	cveniInformer := crdInformer.Cce().V1alpha1().CrossVPCEnis().Informer()

	kubeInformer.Start(wait.NeverStop)
	crdInformer.Start(wait.NeverStop)

	cache.WaitForNamedCacheSync(
		"cce-ipam",
		wait.NeverStop,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		cveniInformer.HasSynced,
	)
}

func TestRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	timer := time.NewTimer(time.Second)
	ch := make(chan struct{})
	kubeClient, _, crdClient, _, _, _ := setupEnv(ctrl)

	ipam, err := NewIPAM(
		kubeClient,
		crdClient,
		types.CCEModeCrossVPCEni,
		"vpc-xxx", "cce-xxx",
		20*time.Second, 120*time.Second,
		true,
	)
	if err != nil {
		t.Error("NewIPAM failed")
	}

	go func() {
		<-timer.C
		for {
			ch <- struct{}{}
		}
	}()

	err = ipam.Run(context.TODO(), ch)
	if err != nil {
		t.Error("Run failed")
	}

}

func TestIPAM_Allocate(t *testing.T) {
	var (
		containerID = "xxxxx"
	)
	type fields struct {
		ctrl             *gomock.Controller
		debug            bool
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		gcPeriod         time.Duration
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
		want    *v1alpha1.CrossVPCEni
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods("default").Create(context.TODO(), &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:           "userid",
							PodAnnotationCrossVPCEniSubnetID:         "sbn-bbb",
							PodAnnotationCrossVPCEniSecurityGroupIDs: "g-xxx",
							PodAnnotationCrossVPCEniVPCCIDR:          "10.0.0.0/8",
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
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-xxx",
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				go func() {
					var (
						ctx = log.NewContext()
					)

					time.Sleep(3 * time.Second)
					cveni, err := crdClient.CceV1alpha1().CrossVPCEnis().Get(context.TODO(), containerID, metav1.GetOptions{})
					if err != nil {
						log.Fatalf(ctx, "Get cveni %v failed: %v", containerID, err)
					}
					cveni.Status.EniStatus = v1alpha1.EniStatusInuse

					_, err = crdClient.CceV1alpha1().CrossVPCEnis().UpdateStatus(context.TODO(), cveni, metav1.UpdateOptions{})
					if err != nil {
						log.Fatalf(ctx, "UpdateStatus cveni %v failed: %v", containerID, err)
					}
				}()

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:         context.TODO(),
				name:        "busybox",
				namespace:   "default",
				containerID: containerID,
			},
			want: &v1alpha1.CrossVPCEni{
				ObjectMeta: metav1.ObjectMeta{
					Name: containerID,
					Labels: map[string]string{
						PodLabelOwnerName:      "busybox",
						PodLabelOwnerNamespace: "default",
						PodLabelOwnerNode:      "test-node",
						PodLabelOwnerInstance:  "i-xxx",
					},
					Finalizers: []string{ipamgeneric.WepFinalizer},
				},
				Spec: v1alpha1.CrossVPCEniSpec{
					UserID:                    "userid",
					SubnetID:                  "sbn-bbb",
					SecurityGroupIDs:          []string{"g-xxx"},
					VPCCIDR:                   "10.0.0.0/8",
					PrivateIPAddress:          "",
					BoundInstanceID:           "i-xxx",
					DefaultRouteExcludedCidrs: make([]string, 0),
				},
				Status: v1alpha1.CrossVPCEniStatus{
					EniID:               "",
					EniStatus:           v1alpha1.EniStatusInuse,
					PrimaryIPAddress:    "",
					MacAddress:          "",
					InvolvedContainerID: containerID,
				},
			},
			wantErr: false,
		},
		{
			name: "node 存在 unstable eni",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods("default").Create(context.TODO(), &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:           "userid",
							PodAnnotationCrossVPCEniSubnetID:         "sbn-bbb",
							PodAnnotationCrossVPCEniSecurityGroupIDs: "g-xxx",
							PodAnnotationCrossVPCEniVPCCIDR:          "10.0.0.0/8",
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
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-xxx",
					},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().CrossVPCEnis().Create(context.TODO(), &v1alpha1.CrossVPCEni{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ooops",
						Labels: map[string]string{
							PodLabelOwnerNode:     "test-node",
							PodLabelOwnerInstance: "i-xxx",
						},
					},
					Status: v1alpha1.CrossVPCEniStatus{
						EniStatus: v1alpha1.EniStatusAttaching,
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:         context.TODO(),
				name:        "busybox",
				namespace:   "default",
				containerID: containerID,
			},
			wantErr: true,
		},
		{
			name: "node 存在 unstable eni，存量 cveni 没有 ownnerinstance label",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods("default").Create(context.TODO(), &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:           "userid",
							PodAnnotationCrossVPCEniSubnetID:         "sbn-bbb",
							PodAnnotationCrossVPCEniSecurityGroupIDs: "g-xxx",
							PodAnnotationCrossVPCEniVPCCIDR:          "10.0.0.0/8",
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
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-xxx",
					},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().CrossVPCEnis().Create(context.TODO(), &v1alpha1.CrossVPCEni{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ooops",
						Labels: map[string]string{
							PodLabelOwnerNode: "test-node",
						},
					},
					Status: v1alpha1.CrossVPCEniStatus{
						EniStatus: v1alpha1.EniStatusAttaching,
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				go func() {
					var (
						ctx = log.NewContext()
					)

					time.Sleep(6 * time.Second)
					cveni, err := crdClient.CceV1alpha1().CrossVPCEnis().Get(context.TODO(), containerID, metav1.GetOptions{})
					if err != nil {
						log.Fatalf(ctx, "Get cveni %v failed: %v", containerID, err)
					}
					cveni.Status.EniStatus = v1alpha1.EniStatusInuse

					_, err = crdClient.CceV1alpha1().CrossVPCEnis().UpdateStatus(context.TODO(), cveni, metav1.UpdateOptions{})
					if err != nil {
						log.Fatalf(ctx, "UpdateStatus cveni %v failed: %v", containerID, err)
					}
				}()

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:         context.TODO(),
				name:        "busybox",
				namespace:   "default",
				containerID: containerID,
			},
			want: &v1alpha1.CrossVPCEni{
				ObjectMeta: metav1.ObjectMeta{
					Name: containerID,
					Labels: map[string]string{
						PodLabelOwnerName:      "busybox",
						PodLabelOwnerNamespace: "default",
						PodLabelOwnerNode:      "test-node",
						PodLabelOwnerInstance:  "i-xxx",
					},
					Finalizers: []string{ipamgeneric.WepFinalizer},
				},
				Spec: v1alpha1.CrossVPCEniSpec{
					UserID:                    "userid",
					SubnetID:                  "sbn-bbb",
					SecurityGroupIDs:          []string{"g-xxx"},
					VPCCIDR:                   "10.0.0.0/8",
					PrivateIPAddress:          "",
					BoundInstanceID:           "i-xxx",
					DefaultRouteExcludedCidrs: make([]string, 0),
				},
				Status: v1alpha1.CrossVPCEniStatus{
					EniID:               "",
					EniStatus:           v1alpha1.EniStatusInuse,
					PrimaryIPAddress:    "",
					MacAddress:          "",
					InvolvedContainerID: containerID,
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
				debug:            tt.fields.debug,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				clock:            tt.fields.clock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				gcPeriod:         tt.fields.gcPeriod,
			}
			got, err := ipam.Allocate(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("IPAM.Allocate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IPAM.Allocate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_Release(t *testing.T) {
	var (
		containerID = "xxxxx"
	)

	type fields struct {
		ctrl             *gomock.Controller
		debug            bool
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		gcPeriod         time.Duration
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
		want    *v1alpha1.CrossVPCEni
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().CrossVPCEnis().Create(context.TODO(), &v1alpha1.CrossVPCEni{
					ObjectMeta: metav1.ObjectMeta{
						Name: containerID,
					},
					Status: v1alpha1.CrossVPCEniStatus{
						EniStatus: v1alpha1.EniStatusDeleted,
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:         context.TODO(),
				name:        "busybox",
				namespace:   "default",
				containerID: containerID,
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "非独占 ENI Pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:         context.TODO(),
				name:        "busybox",
				namespace:   "default",
				containerID: containerID,
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "node 存在 unstable eni",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().CrossVPCEnis().Create(context.TODO(), &v1alpha1.CrossVPCEni{
					ObjectMeta: metav1.ObjectMeta{
						Name: containerID,
						Labels: map[string]string{
							PodLabelOwnerNode:     "test-node",
							PodLabelOwnerInstance: "i-xxx",
						},
					},
					Status: v1alpha1.CrossVPCEniStatus{
						EniStatus: v1alpha1.EniStatusDeleted,
					},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().CrossVPCEnis().Create(context.TODO(), &v1alpha1.CrossVPCEni{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ooops",
						Labels: map[string]string{
							PodLabelOwnerNode:     "test-node",
							PodLabelOwnerInstance: "i-xxx",
						},
					},
					Status: v1alpha1.CrossVPCEniStatus{
						EniStatus: v1alpha1.EniStatusAttaching,
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:         context.TODO(),
				name:        "busybox",
				namespace:   "default",
				containerID: containerID,
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				debug:            tt.fields.debug,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				cloud:            tt.fields.cloud,
				clock:            tt.fields.clock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				gcPeriod:         tt.fields.gcPeriod,
			}
			got, err := ipam.Release(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("IPAM.Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IPAM.Release() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_gcLeakedCrossVPCEni(t *testing.T) {
	type fields struct {
		ctrl             *gomock.Controller
		debug            bool
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		gcPeriod         time.Duration
	}
	type args struct {
		ctx     context.Context
		eniList []*v1alpha1.CrossVPCEni
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx: context.TODO(),
				eniList: []*v1alpha1.CrossVPCEni{&v1alpha1.CrossVPCEni{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxxxx",
						Labels: map[string]string{
							PodLabelOwnerNamespace: "default",
							PodLabelOwnerName:      "busybox",
						},
					},
				}},
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
				debug:            tt.fields.debug,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				cloud:            tt.fields.cloud,
				clock:            tt.fields.clock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				gcPeriod:         tt.fields.gcPeriod,
			}
			if err := ipam.gcLeakedCrossVPCEni(tt.args.ctx, tt.args.eniList); (err != nil) != tt.wantErr {
				t.Errorf("IPAM.gcLeakedCrossVPCEni() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIPAM_searchCrossVPCEniEvents(t *testing.T) {
	var (
		eni = &v1alpha1.CrossVPCEni{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CrossVPCEni",
				APIVersion: "cce.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "f69ed9c86fc4b7fb5ce8635fab94",
				Namespace: "default",
				UID:       "75286a42-1f64-4ff1-b696-7dbb751ac920",
			},
			Spec:   v1alpha1.CrossVPCEniSpec{},
			Status: v1alpha1.CrossVPCEniStatus{},
		}
	)

	type fields struct {
		ctrl             *gomock.Controller
		lock             sync.RWMutex
		debug            bool
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		gcPeriod         time.Duration
	}
	type args struct {
		ctx context.Context
		eni *v1alpha1.CrossVPCEni
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *v1.EventList
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, eventBroadcaster, recorder := setupEnv(ctrl)
				waitForCacheSync(kubeInformer, crdInformer)

				// recorder.Event(eni, v1.EventTypeWarning, "CreateEni", "Got Error")

				return fields{
					ctrl:             ctrl,
					debug:            false,
					cacheHasSynced:   true,
					eventBroadcaster: eventBroadcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx: context.TODO(),
				eni: eni,
			},
			want: &v1.EventList{
				TypeMeta: metav1.TypeMeta{},
				ListMeta: metav1.ListMeta{},
				Items:    nil,
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
				lock:             tt.fields.lock,
				debug:            tt.fields.debug,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				cloud:            tt.fields.cloud,
				clock:            tt.fields.clock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				gcPeriod:         tt.fields.gcPeriod,
			}
			got, err := ipam.searchCrossVPCEniEvents(tt.args.ctx, tt.args.eni)
			if (err != nil) != tt.wantErr {
				t.Errorf("IPAM.searchCrossVPCEniEvents() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IPAM.searchCrossVPCEniEvents() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_eventsToErrorMsg(t *testing.T) {
	type args struct {
		events *v1.EventList
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty list",
			args: args{
				events: &v1.EventList{
					Items: []v1.Event{},
				},
			},
			want: "",
		},
		{
			name: "normal list",
			args: args{
				events: &v1.EventList{
					Items: []v1.Event{
						{
							Reason:  "CreateEni",
							Message: "aaa",
							Type:    v1.EventTypeWarning,
						},
						{
							Reason:  "CreateEni",
							Message: "bbb",
							Type:    v1.EventTypeWarning,
						},
						{
							Reason:  "AttachEni",
							Message: "ccc",
							Type:    v1.EventTypeWarning,
						},
					},
				},
			},
			want: `[CreateEni: aaa, AttachEni: ccc]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventsToErrorMsg(tt.args.events); got != tt.want {
				t.Errorf("eventsToErrorMsg() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getEniSpecFromPodAnnotations(t *testing.T) {

	var (
		userID     = "abcdef"
		subnetID   = "sbn-xxx"
		secGroupID = "g-xxx"
		vpcCIDR    = "7.0.0.0/16"
	)

	type args struct {
		ctx context.Context
		pod *v1.Pod
	}
	tests := []struct {
		name    string
		args    args
		want    *v1alpha1.CrossVPCEniSpec
		wantErr bool
	}{
		{
			name: "eni without route anno",
			args: args{
				ctx: context.TODO(),
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:           userID,
							PodAnnotationCrossVPCEniSubnetID:         subnetID,
							PodAnnotationCrossVPCEniSecurityGroupIDs: secGroupID,
							PodAnnotationCrossVPCEniVPCCIDR:          vpcCIDR,
						},
					},
				},
			},
			want: &v1alpha1.CrossVPCEniSpec{
				UserID:                          userID,
				SubnetID:                        subnetID,
				SecurityGroupIDs:                []string{secGroupID},
				VPCCIDR:                         vpcCIDR,
				PrivateIPAddress:                "",
				BoundInstanceID:                 "",
				DefaultRouteInterfaceDelegation: "",
				DefaultRouteExcludedCidrs:       []string{},
			},
			wantErr: false,
		},
		{
			name: "eni with eni delegation anno",
			args: args{
				ctx: context.TODO(),
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:                          userID,
							PodAnnotationCrossVPCEniSubnetID:                        subnetID,
							PodAnnotationCrossVPCEniSecurityGroupIDs:                secGroupID,
							PodAnnotationCrossVPCEniVPCCIDR:                         vpcCIDR,
							PodAnnotationCrossVPCEniDefaultRouteInterfaceDelegation: "eni",
						},
					},
				},
			},
			want: &v1alpha1.CrossVPCEniSpec{
				UserID:                          userID,
				SubnetID:                        subnetID,
				SecurityGroupIDs:                []string{secGroupID},
				VPCCIDR:                         vpcCIDR,
				PrivateIPAddress:                "",
				BoundInstanceID:                 "",
				DefaultRouteInterfaceDelegation: "eni",
				DefaultRouteExcludedCidrs:       []string{},
			},
			wantErr: false,
		},
		{
			name: "eni with bad eni delegation anno",
			args: args{
				ctx: context.TODO(),
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:                          userID,
							PodAnnotationCrossVPCEniSubnetID:                        subnetID,
							PodAnnotationCrossVPCEniSecurityGroupIDs:                secGroupID,
							PodAnnotationCrossVPCEniVPCCIDR:                         vpcCIDR,
							PodAnnotationCrossVPCEniDefaultRouteInterfaceDelegation: "emm",
						},
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "eni with eni delegation anno and excluded cidr",
			args: args{
				ctx: context.TODO(),
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:                          userID,
							PodAnnotationCrossVPCEniSubnetID:                        subnetID,
							PodAnnotationCrossVPCEniSecurityGroupIDs:                secGroupID,
							PodAnnotationCrossVPCEniVPCCIDR:                         vpcCIDR,
							PodAnnotationCrossVPCEniDefaultRouteInterfaceDelegation: "eni",
							PodAnnotationCrossVPCEniDefaultRouteExcludedCidrs:       "8.0.0.0/8,9.0.0.0/8",
						},
					},
				},
			},
			want: &v1alpha1.CrossVPCEniSpec{
				UserID:                          userID,
				SubnetID:                        subnetID,
				SecurityGroupIDs:                []string{secGroupID},
				VPCCIDR:                         vpcCIDR,
				PrivateIPAddress:                "",
				BoundInstanceID:                 "",
				DefaultRouteInterfaceDelegation: "eni",
				DefaultRouteExcludedCidrs:       []string{"8.0.0.0/8", "9.0.0.0/8"},
			},
			wantErr: false,
		},
		{
			name: "eni with eni delegation anno and excluded cidr",
			args: args{
				ctx: context.TODO(),
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
						Annotations: map[string]string{
							PodAnnotationCrossVPCEniUserID:                          userID,
							PodAnnotationCrossVPCEniSubnetID:                        subnetID,
							PodAnnotationCrossVPCEniSecurityGroupIDs:                secGroupID,
							PodAnnotationCrossVPCEniVPCCIDR:                         vpcCIDR,
							PodAnnotationCrossVPCEniDefaultRouteInterfaceDelegation: "eni",
							PodAnnotationCrossVPCEniDefaultRouteExcludedCidrs:       "8.0.0.0/1,",
						},
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getEniSpecFromPodAnnotations(tt.args.ctx, tt.args.pod)
			if (err != nil) != tt.wantErr {
				t.Errorf("getEniSpecFromPodAnnotations() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getEniSpecFromPodAnnotations() = %v, want %v", got, tt.want)
			}
		})
	}
}
