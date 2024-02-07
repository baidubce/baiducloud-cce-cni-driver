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

package network

import (
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"

	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	mocknetlink "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink/testing"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
	mockns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns/testing"
)

func Test_linuxNetwork_GetLinkByMacAddress(t *testing.T) {
	veth1 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			HardwareAddr: []byte{100, 100, 100, 100, 100, 100},
		},
	}
	veth2 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			HardwareAddr: []byte{238, 238, 238, 238, 238, 238},
		},
	}

	type fields struct {
		ctrl  *gomock.Controller
		nlink netlinkwrapper.Interface
		ns    nswrapper.Interface
	}
	type args struct {
		macAddress string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    netlink.Link
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)

				nlink := mocknetlink.NewMockInterface(ctrl)
				ns := mockns.NewMockInterface(ctrl)

				gomock.InOrder(
					nlink.EXPECT().LinkList().Return([]netlink.Link{veth1, veth2}, nil),
				)

				return fields{
					ctrl:  ctrl,
					nlink: nlink,
					ns:    ns,
				}
			}(),
			args: args{
				macAddress: "ee:ee:ee:ee:ee:ee",
			},
			want:    veth2,
			wantErr: false,
		},
		{
			name: "查找失败",
			fields: func() fields {
				ctrl := gomock.NewController(t)

				nlink := mocknetlink.NewMockInterface(ctrl)
				ns := mockns.NewMockInterface(ctrl)

				gomock.InOrder(
					nlink.EXPECT().LinkList().Return([]netlink.Link{veth1, veth2}, nil),
				)

				return fields{
					ctrl:  ctrl,
					nlink: nlink,
					ns:    ns,
				}
			}(),
			args: args{
				macAddress: "00:00:00:00:00:00",
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
			c := &linuxNetwork{
				nlink: tt.fields.nlink,
				ns:    tt.fields.ns,
			}
			got, err := c.GetLinkByMacAddress(tt.args.macAddress)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetLinkByMacAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetLinkByMacAddress() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_linuxNetwork_DetectInterfaceMTU(t *testing.T) {
	type fields struct {
		ctrl  *gomock.Controller
		nlink netlinkwrapper.Interface
		ns    nswrapper.Interface
	}
	type args struct {
		device string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    int
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)

				nlink := mocknetlink.NewMockInterface(ctrl)
				ns := mockns.NewMockInterface(ctrl)

				gomock.InOrder(
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Veth{
						LinkAttrs: netlink.LinkAttrs{
							MTU: 1500,
						},
					}, nil),
				)

				return fields{
					ctrl:  ctrl,
					nlink: nlink,
					ns:    ns,
				}
			}(),
			args: args{
				device: "",
			},
			want:    1500,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			c := &linuxNetwork{
				nlink: tt.fields.nlink,
				ns:    tt.fields.ns,
			}
			got, err := c.DetectInterfaceMTU(tt.args.device)
			if (err != nil) != tt.wantErr {
				t.Errorf("DetectInterfaceMTU() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DetectInterfaceMTU() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_linuxNetwork_DetectDefaultRouteInterfaceName(t *testing.T) {
	type fields struct {
		ctrl  *gomock.Controller
		nlink netlinkwrapper.Interface
		ns    nswrapper.Interface
	}
	tests := []struct {
		name    string
		fields  fields
		want    string
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)

				nlink := mocknetlink.NewMockInterface(ctrl)
				ns := mockns.NewMockInterface(ctrl)

				gomock.InOrder(
					nlink.EXPECT().RouteList(gomock.Any(), gomock.Any()).Return([]netlink.Route{{
						LinkIndex: 2,
					}}, nil),
					nlink.EXPECT().LinkByIndex(2).Return(&netlink.Veth{
						LinkAttrs: netlink.LinkAttrs{
							Name: "eth0",
						},
					}, nil),
				)

				return fields{
					ctrl:  ctrl,
					nlink: nlink,
					ns:    ns,
				}
			}(),
			want:    "eth0",
			wantErr: false,
		},
		{
			name: "无默认路由",
			fields: func() fields {
				ctrl := gomock.NewController(t)

				nlink := mocknetlink.NewMockInterface(ctrl)
				ns := mockns.NewMockInterface(ctrl)

				gomock.InOrder(
					nlink.EXPECT().RouteList(gomock.Any(), gomock.Any()).Return(nil, nil),
				)

				return fields{
					ctrl:  ctrl,
					nlink: nlink,
					ns:    ns,
				}
			}(),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			c := &linuxNetwork{
				nlink: tt.fields.nlink,
				ns:    tt.fields.ns,
			}
			got, err := c.DetectDefaultRouteInterfaceName()
			if (err != nil) != tt.wantErr {
				t.Errorf("DetectDefaultRouteInterfaceName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DetectDefaultRouteInterfaceName() got = %v, want %v", got, tt.want)
			}
		})
	}
}
