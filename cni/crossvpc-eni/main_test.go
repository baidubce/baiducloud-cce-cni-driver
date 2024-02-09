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
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	rpcdef "github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	mockcbclient "github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc/testing"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	mockutilnetwork "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network/testing"
	typeswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes"
	mocktypes "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes/testing"
	grpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/grpc"
	mockgrpc "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/grpc/testing"
	ipwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip"
	mockip "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip/testing"
	ipamwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam"
	mockipam "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam/testing"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	mocknetlink "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink/testing"
	mocknetns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netns/testing"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
	mockns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns/testing"
	rpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc"
	mockrpc "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc/testing"
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
	mocksysctl "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl/testing"
)

var (
	stdinData = `
 {
	 "cniVersion":"0.3.1",
	 "endpoint":"172.16.71.154:80",
	 "name":"cce-cni",
	 "prevResult":{
		 "cniVersion":"0.3.1",
		 "interfaces":[
			 {
				 "name":"vethcc21c2d7785",
				 "mac":"ba:93:2d:63:cf:22"
			 },
			 {
				 "name":"eth0",
				 "mac":"12:a6:63:62:fc:77",
				 "sandbox":"/proc/1417189/ns/net"
			 }
		 ],
		 "ips":[
			 {
				 "version":"4",
				 "interface":1,
				 "address":"172.31.2.10/24",
				 "gateway":"172.31.2.1"
			 }
		 ],
		 "routes":[
			 {
				 "dst":"0.0.0.0/0"
			 }
		 ],
		 "dns":{
 
		 }
	 },
	 "type":"crossvpc-eni"
 }`

	stdinDataMainPlugin = `
 {
	 "cniVersion":"0.3.1",
	 "endpoint":"172.16.71.154:80",
	 "name":"cce-cni",
	 "type":"crossvpc-eni"
 }`

	envArgs = `IgnoreUnknown=1;K8S_POD_NAMESPACE=default;K8S_POD_NAME=busybox;K8S_POD_INFRA_CONTAINER_ID=xxxxx`

	tenantEni = &netlink.Bond{
		LinkAttrs: netlink.LinkAttrs{
			Name: "eni0",
		},
	}
)

func setupEnv(ctrl *gomock.Controller) (
	*mocknetlink.MockInterface,
	*mockns.MockInterface,
	*mockipam.MockInterface,
	*mockip.MockInterface,
	*mocktypes.MockInterface,
	*mockutilnetwork.MockInterface,
	*mockrpc.MockInterface,
	*mockgrpc.MockInterface,
	*mocksysctl.MockInterface,
) {
	nlink := mocknetlink.NewMockInterface(ctrl)
	ns := mockns.NewMockInterface(ctrl)
	ipam := mockipam.NewMockInterface(ctrl)
	ip := mockip.NewMockInterface(ctrl)
	types := mocktypes.NewMockInterface(ctrl)
	netutil := mockutilnetwork.NewMockInterface(ctrl)
	rpc := mockrpc.NewMockInterface(ctrl)
	grpc := mockgrpc.NewMockInterface(ctrl)
	sysctl := mocksysctl.NewMockInterface(ctrl)

	return nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl
}

func Test_crossVpcEniPlugin_cmdAdd(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		netutil networkutil.Interface
		rpc     rpcwrapper.Interface
		grpc    grpcwrapper.Interface
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
			name: "Chained Plugin 正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				netns := mocknetns.NewMockNetNS(ctrl)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					netns.EXPECT().Fd().Return(uintptr(10)),
					nlink.EXPECT().LinkSetNsFd(gomock.Any(), gomock.Any()).Return(nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
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
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "Main plugin 正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				netns := mocknetns.NewMockNetNS(ctrl)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					netns.EXPECT().Fd().Return(uintptr(10)),
					nlink.EXPECT().LinkSetNsFd(gomock.Any(), gomock.Any()).Return(nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
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
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinDataMainPlugin),
				},
			},
			wantErr: false,
		},
		{
			name: "分配 ENI 失败流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				// netns := mocknetns.NewMockNetNS(ctrl)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: false,
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinData),
				},
			},
			wantErr: true,
		},
		{
			name: "Pod 无需申请 ENI ",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				// netns := mocknetns.NewMockNetNS(ctrl)
				allocReply := &rpcdef.AllocateIPReply{
					IsSuccess: true,
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(allocReply, nil),
					types.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "Main plugin ，查找 eni 初次失败",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				netns := mocknetns.NewMockNetNS(ctrl)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(nil, fmt.Errorf("link with mac ff:ff:ff:ff:ff:ff not found")),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					netns.EXPECT().Fd().Return(uintptr(10)),
					nlink.EXPECT().LinkSetNsFd(gomock.Any(), gomock.Any()).Return(nil),
					netns.EXPECT().Do(gomock.Any()).Return(nil),
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
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinDataMainPlugin),
				},
			},
			wantErr: false,
		},
		{
			name: "Main plugin ，查找 eni 一直失败",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				netns := mocknetns.NewMockNetNS(ctrl)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil),
					ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(nil, fmt.Errorf("link with mac ff:ff:ff:ff:ff:ff not found")),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(nil, fmt.Errorf("link with mac ff:ff:ff:ff:ff:ff not found")),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(nil, fmt.Errorf("link with mac ff:ff:ff:ff:ff:ff not found")),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(nil, fmt.Errorf("link with mac ff:ff:ff:ff:ff:ff not found")),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(nil, fmt.Errorf("link with mac ff:ff:ff:ff:ff:ff not found")),
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(nil, fmt.Errorf("link with mac ff:ff:ff:ff:ff:ff not found")),
					netns.EXPECT().Close().Return(nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
					Netns:       "/proc/100/ns/net",
					IfName:      "eth0",
					Args:        envArgs,
					Path:        "/opt/cin/bin",
					StdinData:   []byte(stdinDataMainPlugin),
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
			p := &crossVpcEniPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
			}
			if err := p.cmdAdd(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("crossVpcEniPlugin.cmdAdd() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_crossVpcEniPlugin_setupEni(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		netutil networkutil.Interface
		rpc     rpcwrapper.Interface
		grpc    grpcwrapper.Interface
		sysctl  sysctlwrapper.Interface
	}
	type args struct {
		resp   *rpc.AllocateIPReply
		ifName string
		args   *skel.CmdArgs
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "正常流程, 不指定网卡名字",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkByName("eth0").Return(&netlink.Veth{}, nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("0", nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				},
				ifName: "",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "正常流程, 指定网卡名字",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkSetDown(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetName(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkByName("eth0").Return(&netlink.Veth{}, nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("0", nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				},
				ifName: "eni0",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "正常流程, 指定网卡名字 eth0",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Veth{}, nil),
					nlink.EXPECT().LinkDel(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetDown(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetName(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("0", nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				},
				ifName: "eth0",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "正常流程, 指定网卡名字，使用 ENI 作为默认路由网卡",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkSetDown(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetName(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkByName("eth0").Return(&netlink.Veth{}, nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("0", nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:                              "10.10.10.10",
							Mac:                             "ff:ff:ff:ff:ff:ff",
							VPCCIDR:                         "10.0.0.0/8",
							DefaultRouteInterfaceDelegation: "eni",
						},
					},
				},
				ifName: "eni0",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "正常流程, 指定网卡名字，ENI 添加自定义网段路由",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkSetDown(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetName(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkByName("eth0").Return(&netlink.Veth{}, nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("0", nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:                        "10.10.10.10",
							Mac:                       "ff:ff:ff:ff:ff:ff",
							VPCCIDR:                   "10.0.0.0/8",
							DefaultRouteExcludedCidrs: []string{"100.0.0.0/8", "180.0.0.0/16"},
						},
					},
				},
				ifName: "eni0",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "正常流程, 指定网卡名字，使用 ENI 作为默认路由网卡，容器默认网卡指定自定义路由",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkSetDown(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetName(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkByName("eth0").Return(&netlink.Veth{}, nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("0", nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:                              "10.10.10.10",
							Mac:                             "ff:ff:ff:ff:ff:ff",
							VPCCIDR:                         "10.0.0.0/8",
							DefaultRouteInterfaceDelegation: "eni",
							DefaultRouteExcludedCidrs:       []string{"100.0.0.0/8", "180.0.0.0/16"},
						},
					},
				},
				ifName: "eni0",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "正常流程, 指定网卡名字 eth0，使用 ENI 作为默认路由网卡，容器默认网卡指定自定义路由",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Veth{}, nil),
					nlink.EXPECT().LinkDel(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetDown(gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetName(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("0", nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:                              "10.10.10.10",
							Mac:                             "ff:ff:ff:ff:ff:ff",
							VPCCIDR:                         "10.0.0.0/8",
							DefaultRouteInterfaceDelegation: "eni",
							DefaultRouteExcludedCidrs:       []string{"100.0.0.0/8", "180.0.0.0/16"},
						},
					},
				},
				ifName: "eth0",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "正常流程, 不指定网卡名字，关闭 ip_forward 出错",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				gomock.InOrder(
					netutil.EXPECT().GetLinkByMacAddress(gomock.Any()).Return(tenantEni, nil),
					nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil),
					nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil),
					nlink.EXPECT().LinkByName("eth0").Return(&netlink.Veth{}, nil),
					sysctl.EXPECT().Sysctl("net/ipv4/ip_forward", "0").Return("", syscall.EPERM),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
					sysctl:  sysctl,
				}
			}(),
			args: args{
				resp: &rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				},
				ifName: "",
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			p := &crossVpcEniPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
				sysctl:  tt.fields.sysctl,
			}
			if err := p.setupEni(tt.args.resp, tt.args.ifName, tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("crossVpcEniPlugin.setupEni() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_crossVpcEniPlugin_cmdDel(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		netutil networkutil.Interface
		rpc     rpcwrapper.Interface
		grpc    grpcwrapper.Interface
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
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)

				releaseReply := rpcdef.ReleaseIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.ReleaseIPReply_CrossVPCENI{
						CrossVPCENI: &rpcdef.CrossVPCENIReply{
							IP:      "10.10.10.10",
							Mac:     "ff:ff:ff:ff:ff:ff",
							VPCCIDR: "10.0.0.0/8",
						},
					},
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().ReleaseIP(gomock.Any(), gomock.Any()).Return(&releaseReply, nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			name: "释放 ENI 失败流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, _ := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)

				releaseReply := rpcdef.ReleaseIPReply{
					IsSuccess: false,
				}

				gomock.InOrder(
					grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil),
					rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient),
					cniBackendClient.EXPECT().ReleaseIP(gomock.Any(), gomock.Any()).Return(&releaseReply, nil),
				)

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					rpc:     rpc,
					grpc:    grpc,
				}
			}(),
			args: args{
				args: &skel.CmdArgs{
					ContainerID: "xxxx",
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
			p := &crossVpcEniPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
			}
			if err := p.cmdDel(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("crossVpcEniPlugin.cmdDel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_cmdCheck(t *testing.T) {
	p := newCrossVpcEniPlugin()
	if err := p.cmdCheck(&skel.CmdArgs{
		ContainerID: "xxxx",
		Netns:       "/proc/100/ns/net",
		IfName:      "eth0",
		Args:        envArgs,
		Path:        "/opt/cin/bin",
		StdinData:   []byte(stdinData),
	}); err != nil {
		t.Error("cmdCheck failed")
	}
}

func TestNewCrossVpcEniPlugin(t *testing.T) {
	cni.InitFlags(filepath.Join(os.TempDir(), "crossvpc-eni.log"))
	p := newCrossVpcEniPlugin()
	if p == nil {
		t.Error("newCrossVpcEniPlugin returns nil")
	}
}
