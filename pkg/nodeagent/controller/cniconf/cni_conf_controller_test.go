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

package cniconf

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	mockmetadata "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	v1alpha1network "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs"
	fsutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs"
	mockfs "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/kernel"
	mockkernel "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/kernel/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	mockutilnetwork "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network/testing"
)

func Test_controllerTools(t *testing.T) {
	var (
		templateFilePath  = "/tmp/cce-cni-secondary-ip-veth.tmpl"
		cniFilePath       = "/tmp/"
		cniConfigFileName = "cce-cni-secondary-ip-veth"
	)

	type fields struct {
		ctrl          *gomock.Controller
		kubeClient    kubernetes.Interface
		ippoolLister  v1alpha1network.IPPoolLister
		nodeLister    corelisters.NodeLister
		metaClient    metadata.Interface
		cniMode       types.ContainerNetworkMode
		nodeName      string
		config        *v1alpha1.CNIConfigControllerConfiguration
		netutil       network.Interface
		kernelhandler kernel.Interface
		filesystem    fs.FileSystem
	}
	type args struct {
		ctx     context.Context
		nodeKey string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *CNIConfigData
		wantErr bool
	}{
		{
			fields: func() fields {
				config := &v1alpha1.CNIConfigControllerConfiguration{
					CNINetworkName:               "",
					CNIConfigFileName:            cniConfigFileName,
					CNIConfigDir:                 cniFilePath,
					CNIConfigTemplateFile:        templateFilePath,
					AutoDetectConfigTemplateFile: false,
				}
				filesystem := fsutil.DefaultFS{}
				filesystem.WriteFile(templateFilePath, []byte("cni-template"), 0755)
				ctrl := gomock.NewController(t)
				kernelhandler := mockkernel.NewMockInterface(ctrl)
				netutil := mockutilnetwork.NewMockInterface(ctrl)
				kubeClient := kubefake.NewSimpleClientset()
				metaClient := mockmetadata.NewMockInterface(ctrl)
				crdClient := crdfake.NewSimpleClientset()
				informerResyncPeriod := time.Duration(time.Duration(1))
				crdInformerFactory := crdinformers.NewSharedInformerFactoryWithOptions(crdClient, informerResyncPeriod,
					crdinformers.WithNamespace(v1.NamespaceDefault),
					crdinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
						options.Kind = "IPPool"
						options.APIVersion = "v1alpha1"
					}),
				)
				ippoolLister := crdInformerFactory.Cce().V1alpha1().IPPools().Lister()

				kubeClient.CoreV1().Services(IPAMServiceNamespace).Create(context.TODO(), &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: IPAMServiceName,
					},
					Spec: v1.ServiceSpec{
						ClusterIP: "10.0.0.2",
						Ports: []v1.ServicePort{
							{
								Port: 80,
							},
						},
					},
					Status: v1.ServiceStatus{},
				}, metav1.CreateOptions{})
				return fields{
					filesystem:    filesystem,
					ctrl:          ctrl,
					kubeClient:    kubeClient,
					metaClient:    metaClient,
					ippoolLister:  ippoolLister,
					cniMode:       types.CCEModeSecondaryIPAutoDetect,
					nodeName:      "test-node",
					config:        config,
					netutil:       netutil,
					kernelhandler: kernelhandler,
				}
			}(),
			args: args{
				ctx:     context.TODO(),
				nodeKey: "test-node",
			},
			want: &CNIConfigData{
				IPAMEndPoint:    "10.0.0.2:80",
				VethMTU:         1400,
				MasterInterface: "eth0",
				InstanceType:    "bcc",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		defer func() {
			tt.fields.filesystem.Remove(templateFilePath)
			tt.fields.filesystem.Remove(cniFilePath + cniConfigFileName)
		}()
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			c := New(tt.fields.kubeClient, tt.fields.ippoolLister, tt.fields.cniMode, tt.fields.nodeName, tt.fields.config)
			c.metaClient = tt.fields.metaClient
			c.config = tt.fields.config
			c.netutil = tt.fields.netutil
			c.kernelhandler = tt.fields.kernelhandler
			c.filesystem = tt.fields.filesystem

			if err := c.SyncNode(tt.args.nodeKey, tt.fields.nodeLister); (err != nil) != tt.wantErr {
				t.Errorf("SyncNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestOptions_getNodeInstanceTypeEx(t *testing.T) {
	var (
		nodeNameEnvKey = "NODE_NAME"
		originNodeName = os.Getenv(nodeNameEnvKey)
	)

	type fields struct {
		ctrl          *gomock.Controller
		kubeClient    kubernetes.Interface
		metaClient    metadata.Interface
		cniMode       types.ContainerNetworkMode
		nodeName      string
		config        *v1alpha1.CNIConfigControllerConfiguration
		netutil       network.Interface
		kernelhandler kernel.Interface
		filesystem    fs.FileSystem
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    metadata.InstanceTypeEx
		wantErr bool
	}{
		{
			name: "normal case",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				metaClient := mockmetadata.NewMockInterface(ctrl)

				gomock.InOrder(
					metaClient.EXPECT().GetInstanceTypeEx().Return(metadata.InstanceTypeExBCC, nil),
				)

				return fields{
					metaClient: metaClient,
					kubeClient: nil,
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    metadata.InstanceTypeExBCC,
			wantErr: false,
		},
		{
			name: "fallback to get instanceTypeEx from options node instanceTypeEx",
			fields: func() fields {
				nodeName := "test-node"
				os.Setenv(nodeNameEnvKey, nodeName)
				ctrl := gomock.NewController(t)
				metaClient := mockmetadata.NewMockInterface(ctrl)
				kubeClient := kubefake.NewSimpleClientset()
				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName,
						Labels: map[string]string{
							v1.LabelInstanceType: "BCC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-QwUTCSuA",
					},
				}, metav1.CreateOptions{})

				gomock.InOrder(
					metaClient.EXPECT().GetInstanceTypeEx().Return(metadata.InstanceTypeExUnknown, errors.New("not found")),
				)

				return fields{
					metaClient: metaClient,
					kubeClient: kubeClient,
					nodeName:   nodeName,
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    metadata.InstanceTypeExBCC,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		defer func() {
			os.Setenv(nodeNameEnvKey, originNodeName)
		}()
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{
				kubeClient:    tt.fields.kubeClient,
				metaClient:    tt.fields.metaClient,
				cniMode:       tt.fields.cniMode,
				nodeName:      tt.fields.nodeName,
				config:        tt.fields.config,
				netutil:       tt.fields.netutil,
				kernelhandler: tt.fields.kernelhandler,
				filesystem:    tt.fields.filesystem,
			}
			got, err := c.getNodeInstanceTypeEx(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Options.getNodeInstanceTypeEx() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Options.getNodeInstanceTypeEx() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_canUseIPVlan(t *testing.T) {
	type args struct {
		kernelVersion *version.Version
		kernelModules []string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			args: args{
				kernelVersion: version.MustParseGeneric("4.18.0-240.1.1.el8_3.x86_64"),
				kernelModules: []string{"nf_nat_masquerade_ipv4", "ipvlan"},
			},
			want: true,
		},
		{
			args: args{
				kernelVersion: version.MustParseGeneric("3.10.0-1160.6.1.el7.x86_64"),
				kernelModules: []string{"nf_nat_masquerade_ipv4"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canUseIPVlan(tt.args.kernelVersion, tt.args.kernelModules); got != tt.want {
				t.Errorf("canUseIPVlan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestController_getCNIConfigTemplateFilePath(t *testing.T) {
	type fields struct {
		kubeClient    kubernetes.Interface
		cniMode       types.ContainerNetworkMode
		nodeName      string
		config        *v1alpha1.CNIConfigControllerConfiguration
		netutil       network.Interface
		kernelhandler kernel.Interface
		filesystem    fs.FileSystem
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr bool
	}{
		{
			fields: fields{
				cniMode: types.CCEModeSecondaryIPVeth,
				config: &v1alpha1.CNIConfigControllerConfiguration{
					AutoDetectConfigTemplateFile: true,
				},
			},
			args: args{
				ctx: context.TODO(),
			},
			want:    "/etc/kubernetes/cni/cce-cni-secondary-ip-veth.tmpl",
			wantErr: false,
		},
		{
			fields: fields{
				cniMode: types.CCEModeSecondaryIPVeth,
				config: &v1alpha1.CNIConfigControllerConfiguration{
					AutoDetectConfigTemplateFile: false,
					CNIConfigTemplateFile:        "./template",
				},
			},
			args: args{
				ctx: context.TODO(),
			},
			want:    "./template",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{
				kubeClient:    tt.fields.kubeClient,
				cniMode:       tt.fields.cniMode,
				nodeName:      tt.fields.nodeName,
				config:        tt.fields.config,
				netutil:       tt.fields.netutil,
				kernelhandler: tt.fields.kernelhandler,
				filesystem:    tt.fields.filesystem,
			}
			got, err := c.getCNIConfigTemplateFilePath(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCNIConfigTemplateFilePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCNIConfigTemplateFilePath() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestController_fillCNIConfigData(t *testing.T) {
	type fields struct {
		ctrl          *gomock.Controller
		kubeClient    kubernetes.Interface
		metaClient    metadata.Interface
		cniMode       types.ContainerNetworkMode
		nodeName      string
		config        *v1alpha1.CNIConfigControllerConfiguration
		netutil       network.Interface
		kernelhandler kernel.Interface
		filesystem    fs.FileSystem
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *CNIConfigData
		wantErr bool
	}{
		{
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kernelhandler := mockkernel.NewMockInterface(ctrl)
				netutil := mockutilnetwork.NewMockInterface(ctrl)
				kubeClient := kubefake.NewSimpleClientset()
				metaClient := mockmetadata.NewMockInterface(ctrl)

				kubeClient.CoreV1().Services(IPAMServiceNamespace).Create(context.TODO(), &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: IPAMServiceName,
					},
					Spec: v1.ServiceSpec{
						ClusterIP: "10.0.0.2",
						Ports: []v1.ServicePort{
							{
								Port: 80,
							},
						},
					},
					Status: v1.ServiceStatus{},
				}, metav1.CreateOptions{})

				gomock.InOrder(
					kernelhandler.EXPECT().DetectKernelVersion(gomock.Any()).Return("4.18.0-240.1.1.el8_3.x86_64", nil),
					kernelhandler.EXPECT().GetModules(gomock.Any(), gomock.Any()).Return([]string{"ipvlan"}, nil),
					metaClient.EXPECT().GetInstanceTypeEx().Return(metadata.InstanceTypeExBCC, nil),
					netutil.EXPECT().DetectDefaultRouteInterfaceName().Return("eth0", nil),
					netutil.EXPECT().DetectInterfaceMTU(gomock.Any()).Return(1400, nil),
				)

				return fields{
					ctrl:       ctrl,
					kubeClient: kubeClient,
					metaClient: metaClient,
					cniMode:    types.CCEModeSecondaryIPAutoDetect,
					nodeName:   "",
					config: &v1alpha1.CNIConfigControllerConfiguration{
						CNINetworkName:               "",
						CNIConfigFileName:            "",
						CNIConfigDir:                 "",
						CNIConfigTemplateFile:        "",
						AutoDetectConfigTemplateFile: true,
					},
					netutil:       netutil,
					kernelhandler: kernelhandler,
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want: &CNIConfigData{
				IPAMEndPoint:    "10.0.0.2:80",
				VethMTU:         1400,
				MasterInterface: "eth0",
				InstanceType:    "bcc",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			c := &Controller{
				kubeClient:    tt.fields.kubeClient,
				metaClient:    tt.fields.metaClient,
				cniMode:       tt.fields.cniMode,
				nodeName:      tt.fields.nodeName,
				config:        tt.fields.config,
				netutil:       tt.fields.netutil,
				kernelhandler: tt.fields.kernelhandler,
				filesystem:    tt.fields.filesystem,
			}
			got, err := c.fillCNIConfigData(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("fillCNIConfigData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fillCNIConfigData() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestController_createOrUpdateCNIConfigFileContent(t *testing.T) {
	type fields struct {
		ctrl          *gomock.Controller
		kubeClient    kubernetes.Interface
		cniMode       types.ContainerNetworkMode
		nodeName      string
		config        *v1alpha1.CNIConfigControllerConfiguration
		netutil       network.Interface
		kernelhandler kernel.Interface
		filesystem    fs.FileSystem
	}
	type args struct {
		ctx              context.Context
		cniConfigContent string
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
				filesystem := mockfs.NewMockFileSystem(ctrl)

				gomock.InOrder(
					filesystem.EXPECT().WriteFile(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
					filesystem.EXPECT().MD5Sum(gomock.Any()).Return("md5xxx", nil),
					filesystem.EXPECT().MD5Sum(gomock.Any()).Return("md5xxx", nil),
					filesystem.EXPECT().Remove(gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:       ctrl,
					config:     &v1alpha1.CNIConfigControllerConfiguration{},
					filesystem: filesystem,
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: false,
		},

		{
			fields: func() fields {
				ctrl := gomock.NewController(t)
				filesystem := mockfs.NewMockFileSystem(ctrl)

				gomock.InOrder(
					filesystem.EXPECT().WriteFile(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
					filesystem.EXPECT().MD5Sum(gomock.Any()).Return("md5xxx", nil),
					filesystem.EXPECT().MD5Sum(gomock.Any()).Return("md5yyy", nil),
					filesystem.EXPECT().Rename(gomock.Any(), gomock.Any()).Return(nil),
					filesystem.EXPECT().Remove(gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:       ctrl,
					config:     &v1alpha1.CNIConfigControllerConfiguration{},
					filesystem: filesystem,
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: false,
		},

		{
			fields: func() fields {
				ctrl := gomock.NewController(t)
				filesystem := mockfs.NewMockFileSystem(ctrl)

				gomock.InOrder(
					filesystem.EXPECT().WriteFile(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
					filesystem.EXPECT().MD5Sum(gomock.Any()).Return("md5xxx", nil),
					filesystem.EXPECT().MD5Sum(gomock.Any()).Return("", os.ErrNotExist),
					filesystem.EXPECT().Rename(gomock.Any(), gomock.Any()).Return(nil),
					filesystem.EXPECT().Remove(gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:       ctrl,
					config:     &v1alpha1.CNIConfigControllerConfiguration{},
					filesystem: filesystem,
				}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			c := &Controller{
				kubeClient:    tt.fields.kubeClient,
				cniMode:       tt.fields.cniMode,
				nodeName:      tt.fields.nodeName,
				config:        tt.fields.config,
				netutil:       tt.fields.netutil,
				kernelhandler: tt.fields.kernelhandler,
				filesystem:    tt.fields.filesystem,
			}
			if err := c.createOrUpdateCNIConfigFileContent(tt.args.ctx, tt.args.cniConfigContent); (err != nil) != tt.wantErr {
				t.Errorf("createOrUpdateCNIConfigFileContent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_renderTemplate(t *testing.T) {
	type args struct {
		ctx        context.Context
		tplContent string
		dataObject *CNIConfigData
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			args: args{
				ctx:        context.TODO(),
				tplContent: `"endpoint":"{{ .IPAMEndPoint }}"`,
				dataObject: &CNIConfigData{
					NetworkName:     "cce-cni",
					IPAMEndPoint:    "10.0.0.2:80",
					VethMTU:         1500,
					MasterInterface: "eth0",
				},
			},
			want:    `"endpoint":"10.0.0.2:80"`,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderTemplate(tt.args.ctx, tt.args.tplContent, tt.args.dataObject)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("renderTemplate() got = %v, want %v", got, tt.want)
			}
		})
	}
}
