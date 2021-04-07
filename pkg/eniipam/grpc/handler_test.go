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

package grpc

import (
	"context"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	mockipam "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
)

func TestENIIPAMGrpcServer_AllocateIP(t *testing.T) {
	type fields struct {
		ctrl   *gomock.Controller
		ipamd  ipam.Interface
		port   int
		ipType rpc.IPType
	}
	type args struct {
		ctx context.Context
		req *rpc.AllocateIPRequest
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *rpc.AllocateIPReply
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)

				ipamd := mockipam.NewMockInterface(ctrl)
				gomock.InOrder(
					ipamd.EXPECT().Allocate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&v1alpha1.WorkloadEndpoint{
						Spec: v1alpha1.WorkloadEndpointSpec{
							IP: "192.168.100.100",
						},
					}, nil),
				)

				return fields{
					ctrl:  ctrl,
					ipamd: ipamd,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: &rpc.AllocateIPRequest{
					K8SPodName:             "busybox",
					K8SPodNamespace:        "default",
					K8SPodInfraContainerID: "xxxxx",
				},
			},
			want: &rpc.AllocateIPReply{
				IsSuccess: true,
				IPType:    0,
				NetworkInfo: &rpc.AllocateIPReply_ENIMultiIP{
					ENIMultiIP: &rpc.ENIMultiIPReply{
						IP: "192.168.100.100",
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
			cb := &ENIIPAMGrpcServer{
				bccipamd: tt.fields.ipamd,
				port:     tt.fields.port,
			}
			got, err := cb.AllocateIP(tt.args.ctx, tt.args.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("AllocateIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AllocateIP() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestENIIPAMGrpcServer_ReleaseIP(t *testing.T) {
	type fields struct {
		ctrl   *gomock.Controller
		ipamd  ipam.Interface
		port   int
		ipType rpc.IPType
	}
	type args struct {
		ctx context.Context
		req *rpc.ReleaseIPRequest
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *rpc.ReleaseIPReply
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)

				ipamd := mockipam.NewMockInterface(ctrl)
				gomock.InOrder(
					ipamd.EXPECT().Release(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&v1alpha1.WorkloadEndpoint{
						Spec: v1alpha1.WorkloadEndpointSpec{
							IP: "192.168.100.100",
						},
					}, nil),
				)

				return fields{
					ctrl:  ctrl,
					ipamd: ipamd,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: &rpc.ReleaseIPRequest{
					K8SPodName:             "busybox",
					K8SPodNamespace:        "default",
					K8SPodInfraContainerID: "xxxxx",
				},
			},
			want: &rpc.ReleaseIPReply{
				IsSuccess: true,
				NetworkInfo: &rpc.ReleaseIPReply_ENIMultiIP{
					ENIMultiIP: &rpc.ENIMultiIPReply{
						IP: "192.168.100.100",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		if tt.fields.ctrl != nil {
			defer tt.fields.ctrl.Finish()
		}
		t.Run(tt.name, func(t *testing.T) {
			cb := &ENIIPAMGrpcServer{
				bccipamd: tt.fields.ipamd,
				port:     tt.fields.port,
			}
			got, err := cb.ReleaseIP(tt.args.ctx, tt.args.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReleaseIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReleaseIP() got = %v, want %v", got, tt.want)
			}
		})
	}
}
