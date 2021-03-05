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
    "hostInterface":"enp216s0",
    "localDNSAddr":"",
    "mtu":1400,
    "name":"cce-cni",
    "prevResult":{
        "cniVersion":"0.3.1",
        "interfaces":[
            {
                "name":"eth0",
                "mac":"fa:20:20:0b:14:36",
                "sandbox":"/proc/100/ns/net"
            }
        ],
        "ips":[
            {
                "version":"4",
                "interface":0,
                "address":"192.168.100.100/32"
            }
        ],
        "dns":{

        }
    },
    "serviceCIDR":"172.25.0.0/16",
    "type":"unnumbered-ptp"
}`

	stdinDataDualStack = `{
    "cniVersion":"0.3.1",
    "hostInterface":"enp216s0",
    "localDNSAddr":"",
    "mtu":1400,
    "name":"cce-cni",
    "prevResult":{
        "cniVersion":"0.3.1",
        "interfaces":[
            {
                "name":"eth0",
                "mac":"fa:20:20:0b:14:36",
                "sandbox":"/proc/100/ns/net"
            }
        ],
        "ips":[
            {
                "version":"4",
                "interface":0,
                "address":"192.168.100.100/32"
            },
            {
                "version":"6",
                "interface":0,
                "address":"fc80::f820:20ff:fe0b:1436/128"
            }
        ],
        "dns":{

        }
    },
    "serviceCIDR":"172.25.0.0/16",
    "type":"unnumbered-ptp"
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
				hostIP := netlink.Addr{
					IPNet: &net.IPNet{
						IP:   net.ParseIP("fe80::787e:9eff:fe13:646e"),
						Mask: net.CIDRMask(128, 128),
					},
				}

				gomock.InOrder(
					ip.EXPECT().EnableIP4Forward().Return(nil),
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{netlink.LinkAttrs{Name: "eth0"}}, nil),
					nlink.EXPECT().AddrList(gomock.Any(), gomock.Any()).Return([]netlink.Addr{hostIP}, nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
					netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
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
		{
			name: "双栈正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

				netns := mocknetns.NewMockNetNS(ctrl)
				hostIP := netlink.Addr{
					IPNet: &net.IPNet{
						IP:   net.ParseIP("192.168.100.3"),
						Mask: net.CIDRMask(32, 32),
					},
				}

				gomock.InOrder(
					ip.EXPECT().EnableIP4Forward().Return(nil),
					ip.EXPECT().EnableIP6Forward().Return(nil),
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{netlink.LinkAttrs{Name: "eth0"}}, nil),
					nlink.EXPECT().AddrList(gomock.Any(), gomock.Any()).Return(nil, nil),
					nlink.EXPECT().AddrList(gomock.Any(), gomock.Any()).Return([]netlink.Addr{hostIP}, nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
					netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
					netutil.EXPECT().GratuitousArpOverIface(gomock.Any(), gomock.Any()).Return(nil),
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
					StdinData:   []byte(stdinDataDualStack),
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
		{
			name: "删除失败",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, sysctl, netutil := setupEnv(ctrl)

				gomock.InOrder(
					ns.EXPECT().WithNetNSPath(gomock.Any(), gomock.Any()).Return(net.UnknownNetworkError("unknown")),
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
			wantErr: true,
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
		hostInterface *current.Interface
		hostNS        ns.NetNS
		ifName        string
		mtu           int
		hostAddrs     []netlink.Addr
		pr            *current.Result
		serviceCIDR   string
		localDNSAddr  string
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
					ip.EXPECT().SetupVeth(gomock.Any(), gomock.Any(), gomock.Any()).Return(net.Interface{}, net.Interface{}, nil),
					netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
					netutil.EXPECT().GratuitousArpOverIface(gomock.Any(), gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
					sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil),
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
				hostInterface: &current.Interface{},
				ifName:        "eth1",
				hostAddrs: []netlink.Addr{{
					IPNet: &net.IPNet{
						IP:   net.ParseIP("192.168.100.3"),
						Mask: net.CIDRMask(32, 32),
					},
				}},
				pr: &current.Result{
					IPs: []*current.IPConfig{{Version: "4"}},
				},
				serviceCIDR:  "172.16.10.0/24",
				localDNSAddr: "169.254.100.10",
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
			if err := p.setupContainerNetNSVeth(tt.args.hostInterface, tt.args.hostNS, tt.args.ifName, tt.args.mtu, tt.args.hostAddrs, tt.args.pr, tt.args.serviceCIDR, tt.args.localDNSAddr); (err != nil) != tt.wantErr {
				t.Errorf("setupContainerNetNSVeth() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
