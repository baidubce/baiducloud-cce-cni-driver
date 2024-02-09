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

package limit

import (
	"context"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/testing"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func Test_crossVPCEniResourceManager_SyncCapacity(t *testing.T) {
	type fields struct {
		ctrl                    *gomock.Controller
		simpleIPResourceManager *simpleIPResourceManager
		bccInstance             *bccapi.InstanceModel
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
			name: "patch 失败流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, _, _, _, _ := setupEnv(ctrl)

				return fields{
					ctrl: ctrl,
					simpleIPResourceManager: &simpleIPResourceManager{
						kubeClient:        kubeClient,
						preAttachedENINum: 1,
						node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "6.0.16.4",
							},
						},
					},
					bccInstance: &bccapi.InstanceModel{
						CpuCount: 8,
					},
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: true,
		},
		{
			name: "正常新增 resource 流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, _, _, _, _ := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "6.0.16.4",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							"cross-vpc-eni.cce.io/eni": resource.Quantity{},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl: ctrl,
					simpleIPResourceManager: &simpleIPResourceManager{
						kubeClient:        kubeClient,
						preAttachedENINum: 1,
						node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "6.0.16.4",
							},
							Status: v1.NodeStatus{
								Capacity: v1.ResourceList{},
							},
						},
					},
					bccInstance: &bccapi.InstanceModel{
						CpuCount: 8,
					},
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: false,
		},
		{
			name: "正常修改 resource 流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, _, _, _, _ := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "6.0.16.4",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							"cross-vpc-eni.cce.io/eni": resource.Quantity{},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl: ctrl,
					simpleIPResourceManager: &simpleIPResourceManager{
						kubeClient:        kubeClient,
						preAttachedENINum: 1,
						node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "6.0.16.4",
							},
							Status: v1.NodeStatus{
								Capacity: v1.ResourceList{
									"cross-vpc-eni.cce.io/eni": resource.Quantity{},
								},
							},
						},
					},
					bccInstance: &bccapi.InstanceModel{
						CpuCount: 8,
					},
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: false,
		},
		{
			name: "正常新增 resource 流程，node anno 自定义最大 eni 数量为 3",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, _, _, _, _ := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "6.0.16.4",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							"cross-vpc-eni.cce.io/eni": resource.Quantity{},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl: ctrl,
					simpleIPResourceManager: &simpleIPResourceManager{
						kubeClient:        kubeClient,
						preAttachedENINum: 1,
						node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "6.0.16.4",
								Annotations: map[string]string{
									"cross-vpc-eni.cce.io/maxEniNumber": "3",
								},
							},
							Status: v1.NodeStatus{
								Capacity: v1.ResourceList{
									"cross-vpc-eni.cce.io/eni": resource.MustParse("3"),
								},
							},
						},
					},
					bccInstance: &bccapi.InstanceModel{
						CpuCount: 8,
					},
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: false,
		},
		{
			name: "正常新增 resource 流程，node anno 自定义最大 eni 错误",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, _, _, _, _ := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "6.0.16.4",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							"cross-vpc-eni.cce.io/eni": resource.Quantity{},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl: ctrl,
					simpleIPResourceManager: &simpleIPResourceManager{
						kubeClient:        kubeClient,
						preAttachedENINum: 1,
						node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "6.0.16.4",
								Annotations: map[string]string{
									"cross-vpc-eni.cce.io/maxEniNumber": "xxx",
								},
							},
							Status: v1.NodeStatus{
								Capacity: v1.ResourceList{
									"cross-vpc-eni.cce.io/eni": resource.MustParse("3"),
								},
							},
						},
					},
					bccInstance: &bccapi.InstanceModel{
						CpuCount: 8,
					},
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: true,
		},
		{
			name: "正常新增 resource 流程，node label 自定义最大 eni 数量为 3",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, _, _, _, _ := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "6.0.16.4",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							"cross-vpc-eni.cce.io/eni": resource.Quantity{},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl: ctrl,
					simpleIPResourceManager: &simpleIPResourceManager{
						kubeClient:        kubeClient,
						preAttachedENINum: 1,
						node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "6.0.16.4",
								Labels: map[string]string{
									"cross-vpc-eni.cce.io/max-eni-number": "3",
								},
							},
							Status: v1.NodeStatus{
								Capacity: v1.ResourceList{
									"cross-vpc-eni.cce.io/eni": resource.MustParse("3"),
								},
							},
						},
					},
					bccInstance: &bccapi.InstanceModel{
						CpuCount: 8,
					},
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: false,
		},
		{
			name: "正常新增 resource 流程，node label 自定义最大 eni 错误",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, _, _, _, _ := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "6.0.16.4",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							"cross-vpc-eni.cce.io/eni": resource.Quantity{},
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl: ctrl,
					simpleIPResourceManager: &simpleIPResourceManager{
						kubeClient:        kubeClient,
						preAttachedENINum: 1,
						node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "6.0.16.4",
								Labels: map[string]string{
									"cross-vpc-eni.cce.io/max-eni-number": "xxx",
								},
							},
							Status: v1.NodeStatus{
								Capacity: v1.ResourceList{
									"cross-vpc-eni.cce.io/eni": resource.MustParse("3"),
								},
							},
						},
					},
					bccInstance: &bccapi.InstanceModel{
						CpuCount: 8,
					},
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		if tt.fields.ctrl != nil {
			defer tt.fields.ctrl.Finish()
		}
		t.Run(tt.name, func(t *testing.T) {
			manager := &crossVPCEniResourceManager{
				simpleIPResourceManager: tt.fields.simpleIPResourceManager,
				bccInstance:             tt.fields.bccInstance,
			}
			if err := manager.SyncCapacity(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("crossVPCEniResourceManager.SyncCapacity() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewCrossVPCEniResourceManager(t *testing.T) {
	type args struct {
		kubeClient  kubernetes.Interface
		node        *corev1.Node
		bccInstance *bccapi.InstanceModel
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "正常流程",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewCrossVPCEniResourceManager(tt.args.kubeClient, tt.args.node, tt.args.bccInstance); got == nil {
				t.Errorf("NewCrossVPCEniResourceManager() = %v", got)
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
