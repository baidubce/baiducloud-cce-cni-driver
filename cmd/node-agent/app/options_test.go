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

package app

import (
	"context"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	agentconfig "github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	nodeagentconfig "github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
)

func TestOptions_complete(t *testing.T) {
	var (
		configFile = "/tmp/cce-cni-conf"
	)

	type fields struct {
		configFile   string
		config       *nodeagentconfig.NodeAgentConfiguration
		hostName     string
		instanceID   string
		instanceType metadata.InstanceTypeEx
		subnetID     string
		node         *v1.Node
		errCh        chan error
		metaClient   metadata.Interface
		kubeClient   kubernetes.Interface
		bceClient    cloud.Interface
	}
	type args struct {
		ctx  context.Context
		args []string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "parse config error",
			fields: fields{
				configFile: "xxxx",
				errCh:      make(chan error),
			},
			args: args{
				ctx:  context.TODO(),
				args: []string{},
			},
			wantErr: true,
		},
		{
			name: "get patch config name error",
			fields: func() fields {
				ioutil.WriteFile(configFile, []byte("cniMode: vpc-secondary-ip-veth"), 0755)
				return fields{
					configFile: configFile,
					errCh:      make(chan error),
					kubeClient: nil,
				}
			}(),
			args: args{
				ctx:  context.TODO(),
				args: []string{},
			},
			wantErr: true,
		},
		{
			name: "node specify no patch config",
			fields: func() fields {
				ioutil.WriteFile(configFile, []byte("cniMode: vpc-secondary-ip-veth"), 0755)
				return fields{
					configFile: configFile,
					errCh:      make(chan error),
					kubeClient: nil,
					node:       &v1.Node{},
				}
			}(),
			args: args{
				ctx:  context.TODO(),
				args: []string{},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		defer func() {
			os.Remove(configFile)
		}()
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				configFile:   tt.fields.configFile,
				config:       tt.fields.config,
				hostName:     tt.fields.hostName,
				instanceID:   tt.fields.instanceID,
				instanceType: tt.fields.instanceType,
				subnetID:     tt.fields.subnetID,
				node:         tt.fields.node,
				errCh:        tt.fields.errCh,
				metaClient:   tt.fields.metaClient,
				kubeClient:   tt.fields.kubeClient,
				bceClient:    tt.fields.bceClient,
			}
			if err := o.complete(tt.args.ctx, tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("Options.complete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOptions_getPatchConfigName(t *testing.T) {
	type fields struct {
		configFile   string
		config       *agentconfig.NodeAgentConfiguration
		hostName     string
		instanceID   string
		instanceType metadata.InstanceTypeEx
		subnetID     string
		node         *v1.Node
		errCh        chan error
		metaClient   metadata.Interface
		kubeClient   kubernetes.Interface
		bceClient    cloud.Interface
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		want1   string
		wantErr bool
	}{
		{
			name: "node with no label",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{},
				},
				errCh: make(chan error),
			},
			args: args{
				ctx: nil,
			},
			want:    false,
			want1:   "",
			wantErr: false,
		},
		{
			name: "node with cni config label",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							CNIPatchConfigLabel: "test",
						},
					},
				},
				errCh: make(chan error),
			},
			args: args{
				ctx: nil,
			},
			want:    true,
			want1:   "test",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				configFile:   tt.fields.configFile,
				config:       tt.fields.config,
				hostName:     tt.fields.hostName,
				instanceID:   tt.fields.instanceID,
				instanceType: tt.fields.instanceType,
				subnetID:     tt.fields.subnetID,
				node:         tt.fields.node,
				errCh:        tt.fields.errCh,
				metaClient:   tt.fields.metaClient,
				kubeClient:   tt.fields.kubeClient,
				bceClient:    tt.fields.bceClient,
			}
			got, got1, err := o.getPatchConfigName(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Options.getPatchConfigName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Options.getPatchConfigName() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Options.getPatchConfigName() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestOptions_getPatchConfigData(t *testing.T) {
	type fields struct {
		configFile   string
		config       *nodeagentconfig.NodeAgentConfiguration
		hostName     string
		instanceID   string
		instanceType metadata.InstanceTypeEx
		subnetID     string
		node         *v1.Node
		errCh        chan error
		metaClient   metadata.Interface
		kubeClient   kubernetes.Interface
		bceClient    cloud.Interface
	}
	type args struct {
		ctx  context.Context
		name string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "normal case",
			fields: func() fields {
				kubeClient := fake.NewSimpleClientset()
				kubeClient.CoreV1().ConfigMaps("kube-system").Create(context.TODO(), &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name: "patch",
					},
					Data: map[string]string{"config": "data"},
				}, metav1.CreateOptions{})
				return fields{
					errCh:      make(chan error),
					metaClient: nil,
					kubeClient: kubeClient,
					bceClient:  nil,
				}
			}(),
			args: args{
				ctx:  context.TODO(),
				name: "patch",
			},
			want:    "\"data\"",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				configFile:   tt.fields.configFile,
				config:       tt.fields.config,
				hostName:     tt.fields.hostName,
				instanceID:   tt.fields.instanceID,
				instanceType: tt.fields.instanceType,
				subnetID:     tt.fields.subnetID,
				node:         tt.fields.node,
				errCh:        tt.fields.errCh,
				metaClient:   tt.fields.metaClient,
				kubeClient:   tt.fields.kubeClient,
				bceClient:    tt.fields.bceClient,
			}
			got, err := o.getPatchConfigData(tt.args.ctx, tt.args.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("Options.getPatchConfigData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Options.getPatchConfigData() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOptions_mergeConfigAndUnmarshal(t *testing.T) {
	type fields struct {
		configFile   string
		config       *nodeagentconfig.NodeAgentConfiguration
		hostName     string
		instanceID   string
		instanceType metadata.InstanceTypeEx
		subnetID     string
		node         *v1.Node
		errCh        chan error
		metaClient   metadata.Interface
		kubeClient   kubernetes.Interface
		bceClient    cloud.Interface
	}
	type args struct {
		ctx    context.Context
		config []byte
		patch  []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *agentconfig.NodeAgentConfiguration
		wantErr bool
	}{
		{
			name:   "normal case",
			fields: fields{},
			args: args{
				ctx:    context.TODO(),
				config: []byte(`{"cniMode":"unknown"}`),
				patch:  []byte(`{"cniMode":"kubenet"}`),
			},
			want: &nodeagentconfig.NodeAgentConfiguration{
				CNIMode: types.K8sNetworkModeKubenet,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				configFile:   tt.fields.configFile,
				config:       tt.fields.config,
				hostName:     tt.fields.hostName,
				instanceID:   tt.fields.instanceID,
				instanceType: tt.fields.instanceType,
				subnetID:     tt.fields.subnetID,
				node:         tt.fields.node,
				errCh:        tt.fields.errCh,
				metaClient:   tt.fields.metaClient,
				kubeClient:   tt.fields.kubeClient,
				bceClient:    tt.fields.bceClient,
			}
			got, err := o.mergeConfigAndUnmarshal(tt.args.ctx, tt.args.config, tt.args.patch)
			if (err != nil) != tt.wantErr {
				t.Errorf("Options.mergeConfigAndUnmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Options.mergeConfigAndUnmarshal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOptions_validateCCE(t *testing.T) {
	type fields struct {
		configFile   string
		config       *nodeagentconfig.NodeAgentConfiguration
		hostName     string
		instanceID   string
		instanceType metadata.InstanceTypeEx
		subnetID     string
		node         *v1.Node
		errCh        chan error
		metaClient   metadata.Interface
		kubeClient   kubernetes.Interface
		bceClient    cloud.Interface
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "valid security groups",
			fields: fields{
				config: &nodeagentconfig.NodeAgentConfiguration{
					CNIMode: types.CCEModeBBCSecondaryIPAutoDetect,
					CCE: nodeagentconfig.CCEConfiguration{
						ENIController: nodeagentconfig.ENIControllerConfiguration{
							SecurityGroupList:           []string{"g-xxxx"},
							EnterpriseSecurityGroupList: []string{"esg-xxx"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid security groups",
			fields: fields{
				config: &nodeagentconfig.NodeAgentConfiguration{
					CNIMode: types.CCEModeBBCSecondaryIPAutoDetect,
					CCE: nodeagentconfig.CCEConfiguration{
						ENIController: nodeagentconfig.ENIControllerConfiguration{
							SecurityGroupList:           []string{},
							EnterpriseSecurityGroupList: []string{"eeesg-xxx"},
						},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				configFile:   tt.fields.configFile,
				config:       tt.fields.config,
				hostName:     tt.fields.hostName,
				instanceID:   tt.fields.instanceID,
				instanceType: tt.fields.instanceType,
				subnetID:     tt.fields.subnetID,
				node:         tt.fields.node,
				errCh:        tt.fields.errCh,
				metaClient:   tt.fields.metaClient,
				kubeClient:   tt.fields.kubeClient,
				bceClient:    tt.fields.bceClient,
			}
			if err := o.validateCCE(); (err != nil) != tt.wantErr {
				t.Errorf("Options.validateCCE() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
