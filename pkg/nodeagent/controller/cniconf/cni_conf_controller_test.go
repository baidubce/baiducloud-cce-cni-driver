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
	"os"

	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs"
	mockfs "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/kernel"
	mockkernel "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/kernel/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	mockutilnetwork "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network/testing"
)

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

				kubeClient.CoreV1().Services(IPAMServiceNamespace).Create(&v1.Service{
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
				})

				gomock.InOrder(
					kernelhandler.EXPECT().DetectKernelVersion(gomock.Any()).Return("4.18.0-240.1.1.el8_3.x86_64", nil),
					kernelhandler.EXPECT().GetModules(gomock.Any(), gomock.Any()).Return([]string{"ipvlan"}, nil),
					netutil.EXPECT().DetectDefaultRouteInterfaceName().Return("eth0", nil),
					netutil.EXPECT().DetectInterfaceMTU(gomock.Any()).Return(1400, nil),
				)

				return fields{
					ctrl:       ctrl,
					kubeClient: kubeClient,
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
