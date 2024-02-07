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
	"net"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"

	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	mocknetlink "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink/testing"
	mocknetns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netns/testing"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
	mockns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns/testing"
)

func Test_linuxNetwork_GetIPFromPodNetNS(t *testing.T) {
	type fields struct {
		ctrl  *gomock.Controller
		nlink netlinkwrapper.Interface
		ns    nswrapper.Interface
	}
	type args struct {
		podNSPath string
		ifName    string
		ipFamily  int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    net.IP
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink := mocknetlink.NewMockInterface(ctrl)
				ns := mockns.NewMockInterface(ctrl)
				netns := mocknetns.NewMockNetNS(ctrl)

				gomock.InOrder(
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
					netns.EXPECT().Close().Return(nil),
				)

				return fields{
					ctrl:  ctrl,
					nlink: nlink,
					ns:    ns,
				}
			}(),
			args: args{
				podNSPath: "/proc/1/ns/net",
				ifName:    "eth0",
				ipFamily:  netlink.FAMILY_V4,
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "获取 IP 失败",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink := mocknetlink.NewMockInterface(ctrl)
				ns := mockns.NewMockInterface(ctrl)
				netns := mocknetns.NewMockNetNS(ctrl)

				gomock.InOrder(
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netns.EXPECT().Do(gomock.Any()).Return(netlink.LinkNotFoundError{}),
					netns.EXPECT().Close().Return(nil),
				)

				return fields{
					ctrl:  ctrl,
					nlink: nlink,
					ns:    ns,
				}
			}(),
			args: args{
				podNSPath: "/proc/1/ns/net",
				ifName:    "eth0",
				ipFamily:  netlink.FAMILY_V4,
			},
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
			got, err := c.GetIPFromPodNetNS(tt.args.podNSPath, tt.args.ifName, tt.args.ipFamily)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetIPFromPodNetNS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetIPFromPodNetNS() got = %v, want %v", got, tt.want)
			}
		})
	}
}
