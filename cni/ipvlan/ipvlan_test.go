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

package main

import (
	"errors"
	"net"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	utilnetwork "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	mockutilnetwork "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network/testing"
	typeswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes"
	mocktypes "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes/testing"
	ipwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip"
	mockip "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip/testing"
	ipamwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam"
	mockipam "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam/testing"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	mocknetlink "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink/testing"
	mocknetns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netns/testing"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
	mockns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns/testing"
)

var (
	stdinData = `{
    "name":"cce-cni",
    "cniVersion":"0.3.1",
    "type":"ipvlan",
    "ipam":{
        "endpoint":"172.25.66.38:80",
        "type":"eni-ipam"
    },
    "master":"eth0",
    "mode":"l3",
    "omitGateway":true
}`
	envArgs = `IgnoreUnknown=1;K8S_POD_NAMESPACE=default;K8S_POD_NAME=busybox;K8S_POD_INFRA_CONTAINER_ID=xxxxx`
)

func setupEnv(ctrl *gomock.Controller) (
	*mocknetlink.MockInterface,
	*mockns.MockInterface,
	*mockipam.MockInterface,
	*mockip.MockInterface,
	*mocktypes.MockInterface,
	*mockutilnetwork.MockInterface,
	versioned.Interface,
) {
	nlink := mocknetlink.NewMockInterface(ctrl)
	ns := mockns.NewMockInterface(ctrl)
	ipam := mockipam.NewMockInterface(ctrl)
	ip := mockip.NewMockInterface(ctrl)
	types := mocktypes.NewMockInterface(ctrl)
	netutil := mockutilnetwork.NewMockInterface(ctrl)
	crdClient := crdfake.NewSimpleClientset()

	return nlink, ns, ipam, ip, types, netutil, crdClient
}

func Test_ipvlanPlugin_cmdAdd(t *testing.T) {
	type fields struct {
		ctrl      *gomock.Controller
		nlink     netlinkwrapper.Interface
		ns        nswrapper.Interface
		ipam      ipamwrapper.Interface
		ip        ipwrapper.Interface
		types     typeswrapper.Interface
		netutil   utilnetwork.Interface
		crdClient versioned.Interface
	}
	type args struct {
		args *skel.CmdArgs
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
				nlink, ns, ipam, ip, types, netutil, crdClient := setupEnv(ctrl)

				netns := mocknetns.NewMockNetNS(ctrl)
				ipamResult := &current.Result{
					CNIVersion: "0.3.1",
					IPs: []*current.IPConfig{{
						Version: "4",
						Address: net.IPNet{
							IP:   net.ParseIP("192.168.100.100"),
							Mask: net.CIDRMask(32, 32),
						},
					}},
				}

				gomock.InOrder(
					types.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					ipam.EXPECT().ExecAdd(gomock.Any(), gomock.Any()).Return(ipamResult, nil),
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{netlink.LinkAttrs{Name: "eth0"}}, nil),
					ip.EXPECT().RandomVethName().Return("vethxxxxxx", nil),
					netns.EXPECT().Fd().Return(uintptr(100)),
					nlink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
					types.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil),
					netns.EXPECT().Close().Return(nil),
				)

				return fields{
					ctrl:      ctrl,
					nlink:     nlink,
					ns:        ns,
					ipam:      ipam,
					ip:        ip,
					types:     types,
					netutil:   netutil,
					crdClient: crdClient,
				}

			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinData),
				},
			},
			wantErr: false,
		},
		{
			name: "IPAM 分配失败",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, crdClient := setupEnv(ctrl)

				netns := mocknetns.NewMockNetNS(ctrl)

				gomock.InOrder(
					types.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					ipam.EXPECT().ExecAdd(gomock.Any(), gomock.Any()).Return(nil, errors.New("ipam failed")),
					netns.EXPECT().Close().Return(nil),
				)

				return fields{
					ctrl:      ctrl,
					nlink:     nlink,
					ns:        ns,
					ipam:      ipam,
					ip:        ip,
					types:     types,
					netutil:   netutil,
					crdClient: crdClient,
				}

			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinData),
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			p := &ipvlanPlugin{
				nlink:     tt.fields.nlink,
				ns:        tt.fields.ns,
				ipam:      tt.fields.ipam,
				ip:        tt.fields.ip,
				types:     tt.fields.types,
				netutil:   tt.fields.netutil,
				crdClient: tt.fields.crdClient,
			}
			if err := p.cmdAdd(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("cmdAdd() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_ipvlanPlugin_cmdDel(t *testing.T) {
	type fields struct {
		ctrl      *gomock.Controller
		nlink     netlinkwrapper.Interface
		ns        nswrapper.Interface
		ipam      ipamwrapper.Interface
		ip        ipwrapper.Interface
		types     typeswrapper.Interface
		netutil   utilnetwork.Interface
		crdClient versioned.Interface
	}
	type args struct {
		args *skel.CmdArgs
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
				nlink, ns, ipam, ip, types, netutil, crdClient := setupEnv(ctrl)

				gomock.InOrder(
					ipam.EXPECT().ExecDel(gomock.Any(), gomock.Any()).Return(nil),
					ns.EXPECT().WithNetNSPath(gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:      ctrl,
					nlink:     nlink,
					ns:        ns,
					ipam:      ipam,
					ip:        ip,
					types:     types,
					netutil:   netutil,
					crdClient: crdClient,
				}

			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinData),
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
			p := &ipvlanPlugin{
				nlink:     tt.fields.nlink,
				ns:        tt.fields.ns,
				ipam:      tt.fields.ipam,
				ip:        tt.fields.ip,
				types:     tt.fields.types,
				netutil:   tt.fields.netutil,
				crdClient: tt.fields.crdClient,
			}
			if err := p.cmdDel(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("cmdDel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_ipvlanPlugin_updateMasterFromWep(t *testing.T) {
	type fields struct {
		ctrl      *gomock.Controller
		nlink     netlinkwrapper.Interface
		ns        nswrapper.Interface
		ipam      ipamwrapper.Interface
		ip        ipwrapper.Interface
		types     typeswrapper.Interface
		netutil   utilnetwork.Interface
		crdClient versioned.Interface
	}
	type args struct {
		k8sArgs *cni.K8SArgs
		n       *NetConf
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
				nlink, ns, ipam, ip, types, netutil, crdClient := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(&v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{Name: "busybox"},
				})

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(&netlink.Device{netlink.LinkAttrs{Name: "eth0"}}, nil),
				)

				return fields{
					ctrl:      ctrl,
					nlink:     nlink,
					ns:        ns,
					ipam:      ipam,
					ip:        ip,
					types:     types,
					netutil:   netutil,
					crdClient: crdClient,
				}

			}(),
			args: args{
				k8sArgs: &cni.K8SArgs{
					K8S_POD_NAME:      "busybox",
					K8S_POD_NAMESPACE: "default",
				},
				n: &NetConf{},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			p := &ipvlanPlugin{
				nlink:     tt.fields.nlink,
				ns:        tt.fields.ns,
				ipam:      tt.fields.ipam,
				ip:        tt.fields.ip,
				types:     tt.fields.types,
				netutil:   tt.fields.netutil,
				crdClient: tt.fields.crdClient,
			}
			if err := p.updateMasterFromWep(tt.args.k8sArgs, tt.args.n); (err != nil) != tt.wantErr {
				t.Errorf("updateMasterFromWep() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
