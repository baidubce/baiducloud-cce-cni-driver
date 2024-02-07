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
	"net"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"

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
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
	mocksysctl "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl/testing"
)

var (
	stdinDataIPv4 = `{
    "cniVersion":"0.3.1",
    "name":"cce-cni",
    "type":"ptp",
    "mtu":1400,
    "ipMasq":true,
    "enableARPProxy":true,
    "ipam":{
        "type":"eni-ipam"
    }
}`

	envArgs = `IgnoreUnknown=1;K8S_POD_NAMESPACE=default;K8S_POD_NAME=busybox;K8S_POD_INFRA_CONTAINER_ID=xxxxx`
)

func setupEnv(ctrl *gomock.Controller) (
	*mocknetlink.MockInterface,
	*mockns.MockInterface,
	*mockipam.MockInterface,
	*mockip.MockInterface,
	*mocktypes.MockInterface,
	*mocksysctl.MockInterface,
	*mockutilnetwork.MockInterface,
) {
	nlink := mocknetlink.NewMockInterface(ctrl)
	ns := mockns.NewMockInterface(ctrl)
	ipam := mockipam.NewMockInterface(ctrl)
	ip := mockip.NewMockInterface(ctrl)
	types := mocktypes.NewMockInterface(ctrl)
	sysctl := mocksysctl.NewMockInterface(ctrl)
	netutil := mockutilnetwork.NewMockInterface(ctrl)

	return nlink, ns, ipam, ip, types, sysctl, netutil
}

func Test_ptpPlugin_cmdAdd(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		sysctl  sysctlwrapper.Interface
		netutil utilnetwork.Interface
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
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

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
					ip.EXPECT().DelLinkByName(gomock.Any()).Return(nil),
					ipam.EXPECT().ExecAdd(gomock.Any(), gomock.Any()).Return(ipamResult, nil),
					ip.EXPECT().EnableForward(gomock.Any()).Return(nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Veth{}, nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
					ip.EXPECT().SetupIPMasq(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
					types.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil),
					netns.EXPECT().Close().Return(nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					sysctl:  sysctl,
					netutil: netutil,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinDataIPv4),
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
			p := &ptpPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				sysctl:  tt.fields.sysctl,
				netutil: tt.fields.netutil,
			}
			if err := p.cmdAdd(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("cmdAdd() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_ptpPlugin_cmdDel(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		sysctl  sysctlwrapper.Interface
		netutil utilnetwork.Interface
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
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

				gomock.InOrder(
					ipam.EXPECT().ExecDel(gomock.Any(), gomock.Any()).Return(nil),
					ns.EXPECT().WithNetNSPath(gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					sysctl:  sysctl,
					netutil: netutil,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinDataIPv4),
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
			p := &ptpPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				sysctl:  tt.fields.sysctl,
				netutil: tt.fields.netutil,
			}
			if err := p.cmdDel(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("cmdDel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_ptpPlugin_setupContainerNetNSVeth(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		sysctl  sysctlwrapper.Interface
		netutil utilnetwork.Interface
	}
	type args struct {
		ctrl               *gomock.Controller
		hostInterface      *current.Interface
		containerInterface *current.Interface
		netns              ns.NetNS
		hostNS             ns.NetNS
		ifName             string
		hostVethName       string
		mtu                int
		pr                 *current.Result
		enableARPProxy     bool
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "enableARPProxy = true 正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

				gomock.InOrder(
					ip.EXPECT().SetupVethWithName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(net.Interface{}, net.Interface{}, nil),
					ipam.EXPECT().ConfigureIface(gomock.Any(), gomock.Any()).Return(nil),
					netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, nil),
					nlink.EXPECT().RouteDel(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					netutil.EXPECT().GratuitousArpOverIface(gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					sysctl:  sysctl,
					netutil: netutil,
				}
			}(),
			args: func() args {
				ctrl := gomock.NewController(t)

				netns := mocknetns.NewMockNetNS(ctrl)
				gomock.InOrder(
					netns.EXPECT().Path().Return(""),
				)

				return args{
					ctrl:               ctrl,
					hostInterface:      &current.Interface{},
					containerInterface: &current.Interface{},
					netns:              netns,
					ifName:             "eth0",
					hostVethName:       "vethxxxxx",
					pr: &current.Result{
						IPs: []*current.IPConfig{
							{
								Version: "4",
								Address: net.IPNet{
									IP:   net.ParseIP("192.168.100.100"),
									Mask: net.CIDRMask(32, 32),
								},
							},
						},
					},
					enableARPProxy: true,
				}
			}(),
			wantErr: false,
		},
		{
			name: "enableARPProxy = false 正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

				gomock.InOrder(
					ip.EXPECT().SetupVethWithName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(net.Interface{}, net.Interface{}, nil),
					ipam.EXPECT().ConfigureIface(gomock.Any(), gomock.Any()).Return(nil),
					netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, nil),
					nlink.EXPECT().RouteDel(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					netutil.EXPECT().GratuitousArpOverIface(gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					sysctl:  sysctl,
					netutil: netutil,
				}
			}(),
			args: func() args {
				ctrl := gomock.NewController(t)

				netns := mocknetns.NewMockNetNS(ctrl)
				gomock.InOrder(
					netns.EXPECT().Path().Return(""),
				)

				return args{
					ctrl:               ctrl,
					hostInterface:      &current.Interface{},
					containerInterface: &current.Interface{},
					netns:              netns,
					ifName:             "eth0",
					hostVethName:       "vethxxxxx",
					pr: &current.Result{
						IPs: []*current.IPConfig{
							{
								Version: "4",
								Address: net.IPNet{
									IP:   net.ParseIP("192.168.100.100"),
									Mask: net.CIDRMask(32, 32),
								},
							},
						},
					},
					enableARPProxy: false,
				}
			}(),
			wantErr: false,
		},
		{
			name: "enableARPProxy = true, IPv6 正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

				gomock.InOrder(
					ip.EXPECT().SetupVethWithName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(net.Interface{}, net.Interface{}, nil),
					ipam.EXPECT().ConfigureIface(gomock.Any(), gomock.Any()).Return(nil),
					netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, nil),
					nlink.EXPECT().RouteDel(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					sysctl:  sysctl,
					netutil: netutil,
				}
			}(),
			args: func() args {
				ctrl := gomock.NewController(t)

				hostNS := mocknetns.NewMockNetNS(ctrl)
				netns := mocknetns.NewMockNetNS(ctrl)
				gomock.InOrder(
					netns.EXPECT().Path().Return(""),
					hostNS.EXPECT().Do(gomock.Any()).Return(nil),
				)

				return args{
					ctrl:               ctrl,
					hostInterface:      &current.Interface{},
					containerInterface: &current.Interface{},
					hostNS:             hostNS,
					netns:              netns,
					ifName:             "eth0",
					hostVethName:       "vethxxxxx",
					pr: &current.Result{
						IPs: []*current.IPConfig{
							{
								Version: "6",
								Address: net.IPNet{
									IP:   net.ParseIP("fe80::f827:ff:fe05:83f"),
									Mask: net.CIDRMask(128, 128),
								},
							},
						},
					},
					enableARPProxy: true,
				}
			}(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		if tt.fields.ctrl != nil {
			defer tt.fields.ctrl.Finish()
		}
		if tt.args.ctrl != nil {
			defer tt.args.ctrl.Finish()
		}
		t.Run(tt.name, func(t *testing.T) {
			p := &ptpPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				sysctl:  tt.fields.sysctl,
				netutil: tt.fields.netutil,
			}
			if err := p.setupContainerNetNSVeth(tt.args.hostInterface, tt.args.containerInterface, tt.args.netns, tt.args.hostNS, tt.args.ifName, tt.args.hostVethName, tt.args.mtu, tt.args.pr, tt.args.enableARPProxy); (err != nil) != tt.wantErr {
				t.Errorf("setupContainerNetNSVeth() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_ptpPlugin_getIPv6LinkLocalAddress(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		sysctl  sysctlwrapper.Interface
		netutil utilnetwork.Interface
	}
	type args struct {
		hostInterface         *current.Interface
		hostInterfaceIPv6Addr *net.IP
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
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

				gomock.InOrder(
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Veth{}, nil),
					nlink.EXPECT().AddrList(gomock.Any(), gomock.Any()).Return([]netlink.Addr{
						{
							IPNet: &net.IPNet{
								IP: net.ParseIP("::1"),
							},
						},
					}, nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					sysctl:  sysctl,
					netutil: netutil,
				}
			}(),
			args: args{
				hostInterface:         &current.Interface{},
				hostInterfaceIPv6Addr: &net.IP{},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &ptpPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				sysctl:  tt.fields.sysctl,
				netutil: tt.fields.netutil,
			}
			if err := p.getIPv6LinkLocalAddress(tt.args.hostInterface, tt.args.hostInterfaceIPv6Addr); (err != nil) != tt.wantErr {
				t.Errorf("getIPv6LinkLocalAddress() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
