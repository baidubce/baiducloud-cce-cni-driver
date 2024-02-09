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
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/utils/exec"
	utilexec "k8s.io/utils/exec"
	fakeexec "k8s.io/utils/exec/testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	rpcdef "github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	mockcbclient "github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc/testing"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
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
    "name":"cce-cni",
    "type":"rdma",
	"ipam":{
        "endpoint":"172.25.66.38:80"
    },
	"prevResult": {
		"ips": [
			{
			  "address": "10.1.0.5/16",
			  "gateway": "10.1.0.1",
			  "interface": 2
			}
		],
		"routes": [
		  {
			"dst": "0.0.0.0/0"
		  }
		],
		"interfaces": [
			{
				"name": "cni0",
				"mac": "00:11:22:33:44:55"
			},
			{
				"name": "veth3243",
				"mac": "55:44:33:22:11:11"
			},
			{
				"name": "eth0",
				"mac": "00:11:22:33:44:66",
				"sandbox": "/var/run/netns/blue"
			}
		],
		"dns": {
		  "nameservers": [ "10.1.0.1" ]
		}
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

func Test_cmdDel(t *testing.T) {
	t.Log("test cmd del")
	SetUPK8SClientEnv()
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
		exec    utilexec.Interface
		sysctl  sysctlwrapper.Interface
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
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				allocReply := rpcdef.ReleaseIPReply{
					IsSuccess: true,
					ErrMsg:    "",
				}
				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				ns.EXPECT().WithNetNSPath(gomock.Any(), gomock.Any()).Return(nil)
				grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
				rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient)
				cniBackendClient.EXPECT().ReleaseIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil)

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
			name: "异常流程1",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				allocReply := rpcdef.ReleaseIPReply{
					IsSuccess: true,
					ErrMsg:    "",
				}
				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				ns.EXPECT().WithNetNSPath(gomock.Any(), gomock.Any()).Return(nil)
				grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
				rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient)
				cniBackendClient.EXPECT().ReleaseIP(gomock.Any(), gomock.Any()).Return(&allocReply, errors.New("release ip error"))

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
			name: "异常流程2",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				allocReply := rpcdef.ReleaseIPReply{
					IsSuccess: true,
					ErrMsg:    "",
				}
				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)
				ns.EXPECT().WithNetNSPath(gomock.Any(), gomock.Any()).Return(errors.New("nspath error for cmd del unit testrelease"))
				grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
				rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient)
				cniBackendClient.EXPECT().ReleaseIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil)

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			p := &rdmaPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
				exec:    tt.fields.exec,
				sysctl:  tt.fields.sysctl,
			}
			if err := p.cmdDel(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("rdmaPlugin.cmdDel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_cmdAdd(t *testing.T) {
	t.Log("test cmd add")
	SetUPK8SClientEnv()
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
		exec    utilexec.Interface
		sysctl  sysctlwrapper.Interface
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
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				fakeCmd := fakeexec.FakeCmd{
					CombinedOutputScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) { return []byte("ens11"), nil, nil },
					},
				}
				fakeExec := getFakeExecTemplate(&fakeCmd)
				netns := mocknetns.NewMockNetNS(ctrl)

				nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "ens11"}}, nil).AnyTimes()
				nlink.EXPECT().LinkAdd(gomock.Any()).Return(nil)
				nlink.EXPECT().AddrList(gomock.Any(), gomock.Any()).Return([]netlink.Addr{
					{
						IPNet: &net.IPNet{
							IP:   net.IPv4(25, 0, 0, 45),
							Mask: net.CIDRMask(24, 32),
						},
					},
				}, nil).AnyTimes()

				nlink.EXPECT().RuleList(gomock.Any()).Return([]netlink.Rule{
					{
						Src: &net.IPNet{
							IP:   net.IPv4(25, 0, 0, 45),
							Mask: net.CIDRMask(32, 32),
						},
					},
				}, nil)

				nlink.EXPECT().RouteListFiltered(gomock.Any(), gomock.Any(), gomock.Any()).Return([]netlink.Route{
					{
						Gw: net.IPv4(25, 0, 0, 1),
					},
				}, nil)
				//nlink.EXPECT().RuleDel(gomock.Any()).Return(nil)
				ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil)
				netns.EXPECT().Fd().Return(uintptr(10))
				netns.EXPECT().Do(gomock.Any()).Return(nil).AnyTimes()
				netns.EXPECT().Close().Return(nil)

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
					exec:    &fakeExec,
					sysctl:  sysctl,
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
			name: "异常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				fakeCmd := fakeexec.FakeCmd{
					CombinedOutputScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) {
							return []byte("ens11"), nil, errors.New("get roce device error for unit test")
						},
					},
				}
				fakeExec := getFakeExecTemplate(&fakeCmd)
				netns := mocknetns.NewMockNetNS(ctrl)

				nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "ens11"}}, nil).AnyTimes()
				nlink.EXPECT().AddrList(gomock.Any(), gomock.Any()).Return([]netlink.Addr{
					{
						IPNet: &net.IPNet{
							IP:   net.IPv4(25, 0, 0, 45),
							Mask: net.CIDRMask(24, 32),
						},
					},
				}, nil).AnyTimes()

				//nlink.EXPECT().RuleDel(gomock.Any()).Return(nil)
				ns.EXPECT().GetNS(gomock.Any()).Return(netns, nil)
				netns.EXPECT().Do(gomock.Any()).Return(nil).AnyTimes()
				netns.EXPECT().Close().Return(nil)

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
					exec:    &fakeExec,
					sysctl:  sysctl,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			p := &rdmaPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
				exec:    tt.fields.exec,
				sysctl:  tt.fields.sysctl,
			}
			if err := p.cmdAdd(tt.args.args); (err != nil) != tt.wantErr {
				t.Errorf("rdmaPlugin.cmdAdd() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func getFakeExecTemplate(fakeCmd *fakeexec.FakeCmd) fakeexec.FakeExec {
	var fakeTemplate []fakeexec.FakeCommandAction
	for i := 0; i < len(fakeCmd.CombinedOutputScript); i++ {
		fakeTemplate = append(fakeTemplate, func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(fakeCmd, cmd, args...) })
	}
	return fakeexec.FakeExec{
		CommandScript: fakeTemplate,
	}
}

func Test_rocePlugin_setupMacvlanInterface(t *testing.T) {
	type fields struct {
		ctrl    *gomock.Controller
		nlink   netlinkwrapper.Interface
		ns      nswrapper.Interface
		ipam    ipamwrapper.Interface
		ip      ipwrapper.Interface
		types   typeswrapper.Interface
		netutil networkutil.Interface
		exec    utilexec.Interface
		sysctl  sysctlwrapper.Interface
		netns   *mocknetns.MockNetNS
	}

	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, _, _, sysctl := setupEnv(ctrl)

				ip.EXPECT().RenameLink(gomock.Any(), gomock.Any()).Return(nil)
				nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "roce0"}}, nil).AnyTimes()
				netns := mocknetns.NewMockNetNS(ctrl)
				netns.EXPECT().Path().Return("")

				return fields{
					ctrl:    ctrl,
					nlink:   nlink,
					ns:      ns,
					ipam:    ipam,
					ip:      ip,
					types:   types,
					netutil: netutil,
					sysctl:  sysctl,
					netns:   netns,
				}
			}(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			p := &rdmaPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				sysctl:  tt.fields.sysctl,
			}

			macvlan := &current.Interface{}
			mv := &netlink.Macvlan{
				Mode: netlink.MACVLAN_MODE_BRIDGE,
			}
			if err := p.setupMacvlanInterface(macvlan, mv, "roce0", "roce0", tt.fields.netns); (err != nil) != tt.wantErr {
				t.Errorf("rdmaPlugin.setupMacvlanInterface() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_rdmaPlugin_setupMacvlanNetworkInfo(t *testing.T) {
	t.Log("test rdmaPlugin setupMacvlanNetworkInfo")

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
		exec    utilexec.Interface
		sysctl  sysctlwrapper.Interface
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
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)

				fakeCmd := fakeexec.FakeCmd{
					CombinedOutputScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) { return []byte("ens11"), nil, nil },
						func() ([]byte, []byte, error) { return []byte("ens12"), nil, nil },
					},
					RunScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) { return nil, nil, nil },
						func() ([]byte, []byte, error) { return nil, nil, nil },
					},
				}
				fakeExec := getFakeExecTemplate(&fakeCmd)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_ENIMultiIP{
						ENIMultiIP: &rpcdef.ENIMultiIPReply{
							IP:  "172.168.172.168",
							Mac: "a2:37:b9:e8:ee:8f",
							Gw:  "172.168.172.1",
						},
					},
				}

				grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
				rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient)
				cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil)
				nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "ens11"}}, nil).AnyTimes()
				nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil)
				nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil)
				nlink.EXPECT().RuleDel(gomock.Any()).Return(nil).AnyTimes()
				nlink.EXPECT().RuleAdd(gomock.Any()).Return(nil).AnyTimes()
				netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, nil)
				netutil.EXPECT().GratuitousArpOverIface(gomock.Any(), gomock.Any()).Return(nil)

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
					exec:    &fakeExec,
					sysctl:  sysctl,
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
			name: "异常流程1",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)

				fakeCmd := fakeexec.FakeCmd{
					CombinedOutputScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) { return []byte("ens11"), nil, nil },
					},
				}
				fakeExec := getFakeExecTemplate(&fakeCmd)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_ENIMultiIP{
						ENIMultiIP: &rpcdef.ENIMultiIPReply{
							IP:  "172.168.172.168",
							Mac: "a2:37:b9:e8:ee:8f",
							Gw:  "172.168.172.1",
						},
					},
				}

				releaseReply := rpcdef.ReleaseIPReply{
					IsSuccess:   true,
					NetworkInfo: &rpcdef.ReleaseIPReply_ENIMultiIP{},
				}

				grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient).AnyTimes()
				cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil)
				cniBackendClient.EXPECT().ReleaseIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(&releaseReply, nil)
				nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "ens11"}}, nil).AnyTimes()
				nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil)
				nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(errors.New("add addr failed"))

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
					exec:    &fakeExec,
					sysctl:  sysctl,
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
			name: "异常流程2",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)

				fakeCmd := fakeexec.FakeCmd{
					CombinedOutputScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) { return []byte("ens11"), nil, nil },
					},
				}
				fakeExec := getFakeExecTemplate(&fakeCmd)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: false,
					NetworkInfo: &rpcdef.AllocateIPReply_ENIMultiIP{
						ENIMultiIP: &rpcdef.ENIMultiIPReply{
							IP:  "172.168.172.168",
							Mac: "a2:37:b9:e8:ee:8f",
							Gw:  "172.168.172.1",
						},
					},
				}

				grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient).AnyTimes()
				cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, errors.New("allocate ip error")).AnyTimes()
				nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "ens11"}}, nil).AnyTimes()
				nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil)

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
					exec:    &fakeExec,
					sysctl:  sysctl,
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
			name: "异常流程3",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				cniBackendClient := mockcbclient.NewMockCNIBackendClient(ctrl)

				fakeCmd := fakeexec.FakeCmd{
					CombinedOutputScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) { return []byte("ens11"), nil, nil },
						func() ([]byte, []byte, error) { return []byte("ens12"), nil, nil },
					},
					RunScript: []fakeexec.FakeAction{
						func() ([]byte, []byte, error) { return nil, nil, nil },
						func() ([]byte, []byte, error) { return nil, nil, nil },
					},
				}
				fakeExec := getFakeExecTemplate(&fakeCmd)
				allocReply := rpcdef.AllocateIPReply{
					IsSuccess: true,
					NetworkInfo: &rpcdef.AllocateIPReply_ENIMultiIP{
						ENIMultiIP: &rpcdef.ENIMultiIPReply{
							IP:  "172.168.172.168",
							Mac: "a2:37:b9:e8:ee:8f",
							Gw:  "172.168.172.1",
						},
					},
				}

				releaseReply := rpcdef.ReleaseIPReply{
					IsSuccess:   true,
					NetworkInfo: &rpcdef.ReleaseIPReply_ENIMultiIP{},
				}
				grpc.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				rpc.EXPECT().NewCNIBackendClient(gomock.Any()).Return(cniBackendClient).AnyTimes()
				cniBackendClient.EXPECT().AllocateIP(gomock.Any(), gomock.Any()).Return(&allocReply, nil)
				cniBackendClient.EXPECT().ReleaseIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(&releaseReply, nil)
				nlink.EXPECT().LinkByName(gomock.Any()).Return(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "ens11"}}, nil).AnyTimes()
				nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil)
				nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil)
				nlink.EXPECT().RuleDel(gomock.Any()).Return(nil).AnyTimes()
				nlink.EXPECT().RuleAdd(gomock.Any()).Return(nil).AnyTimes()
				netutil.EXPECT().InterfaceByName(gomock.Any()).Return(&net.Interface{}, errors.New("get interface by name error"))
				//netutil.EXPECT().GratuitousArpOverIface(gomock.Any(), gomock.Any()).Return(nil)

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
					exec:    &fakeExec,
					sysctl:  sysctl,
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
			p := &rdmaPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
				exec:    tt.fields.exec,
				sysctl:  tt.fields.sysctl,
			}
			ctx := log.NewContext()
			n, _, err := loadConf(tt.args.args.StdinData)
			if err != nil {
				t.Errorf("loadConf error = %v", err)
			}
			macvlan := &current.Interface{}
			k8sArgs, err := p.loadK8SArgs(tt.args.args.Args)
			if err != nil {
				t.Errorf("loadK8SArgs error = %v", err)
			}
			roceIpam := NewRoceIPAM(tt.fields.grpc, tt.fields.rpc)
			masterMask := net.CIDRMask(24, 32)
			gw := net.IPv4(25, 0, 0, 1)
			if err := p.setupMacvlanNetworkInfo(ctx, n, "a2:37:b9:e8:ee:8f", masterMask, gw, "roce0", macvlan, k8sArgs, roceIpam); (err != nil) != tt.wantErr {
				t.Errorf("rdmaPlugin.cmdAdd() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_rdmaPlugin_delAllMacVlanDevices(t *testing.T) {
	t.Log("test rdmaPlugin setupMacvlanNetworkInfo")

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
		exec    utilexec.Interface
		sysctl  sysctlwrapper.Interface
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
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				macVlanDev := &netlink.Macvlan{
					LinkAttrs: netlink.LinkAttrs{
						HardwareAddr: []byte{100, 100, 100, 100, 100, 100},
					},
				}

				ip.EXPECT().DelLinkByName(gomock.Any()).Return(nil)
				nlink.EXPECT().LinkList().Return([]netlink.Link{macVlanDev}, nil)

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
			name: "异常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				macVlanDev := &netlink.Macvlan{
					LinkAttrs: netlink.LinkAttrs{
						HardwareAddr: []byte{100, 100, 100, 100, 100, 100},
					},
				}

				ip.EXPECT().DelLinkByName(gomock.Any()).Return(errors.New("Delete Link By Name Error"))
				nlink.EXPECT().LinkList().Return([]netlink.Link{macVlanDev}, nil)

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
			p := &rdmaPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
				exec:    tt.fields.exec,
				sysctl:  tt.fields.sysctl,
			}

			if err := p.delAllMacVlanDevices(); (err != nil) != tt.wantErr {
				t.Errorf("rdmaPlugin.delAllMacVlanDevices() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_rdmaPlugin_AddRoute(t *testing.T) {
	t.Log("test rdmaPlugin AddRoute")

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
		exec    utilexec.Interface
		sysctl  sysctlwrapper.Interface
	}
	type args struct {
		args *skel.CmdArgs
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				nlink.EXPECT().RouteAdd(gomock.Any()).Return(nil)

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
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			p := &rdmaPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
				exec:    tt.fields.exec,
				sysctl:  tt.fields.sysctl,
			}
			gw := net.IPv4(10, 10, 0, 1)
			src := net.IPv4(10, 10, 0, 45)
			dst := &net.IPNet{IP: src, Mask: net.CIDRMask(24, 32)}
			if err := p.addRoute(1000, 1000, gw, src, dst); (err != nil) != tt.wantErr {
				t.Errorf("rdmaPlugin.addRoute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_rdmaPlugin_disableRPFCheck(t *testing.T) {
	t.Log("test rdmaPlugin disableRPFCheck")

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
		exec    utilexec.Interface
		sysctl  sysctlwrapper.Interface
	}
	type args struct {
		args *skel.CmdArgs
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "正常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

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
			wantErr: false,
		},
		{
			name: "异常流程",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				nlink, ns, ipam, ip, types, netutil, rpc, grpc, sysctl := setupEnv(ctrl)

				sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", errors.New("apply sysctl error")).AnyTimes()

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
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			p := &rdmaPlugin{
				nlink:   tt.fields.nlink,
				ns:      tt.fields.ns,
				ipam:    tt.fields.ipam,
				ip:      tt.fields.ip,
				types:   tt.fields.types,
				netutil: tt.fields.netutil,
				rpc:     tt.fields.rpc,
				grpc:    tt.fields.grpc,
				exec:    tt.fields.exec,
				sysctl:  tt.fields.sysctl,
			}
			ctx := log.NewContext()
			if err := p.disableRPFCheck(ctx, 1); (err != nil) != tt.wantErr {
				t.Errorf("rdmaPlugin.disableRPFCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_modeFromString(t *testing.T) {
	t.Log("test cmd modeFromString")

	_, err := modeFromString("private")
	if err != nil {
		t.Error("modeFromString failed")
	}
	_, err = modeFromString("vepa")
	if err != nil {
		t.Error("modeFromString failed")
	}
	_, err = modeFromString("passthru")
	if err != nil {
		t.Error("modeFromString failed")
	}
	_, err = modeFromString("error")
	if err == nil {
		t.Error("modeFromString failed")
	}
}

func Test_cmdCheck(t *testing.T) {
	t.Log("test cmd check")
	p := newRdmaPlugin()
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

func TestNewRdmaPlugin(t *testing.T) {
	t.Log("test cmd rdma plugin")
	cni.InitFlags(filepath.Join(os.TempDir(), "rdma.log"))
	p := newRdmaPlugin()
	if p == nil {
		t.Error("newRdmaPlugin returns nil")
	}
}

func SetUPK8SClientEnv() {
	testConfig := &rest.Config{
		Host:            "testHost",
		APIPath:         "api",
		ContentConfig:   rest.ContentConfig{},
		Impersonate:     rest.ImpersonationConfig{},
		TLSClientConfig: rest.TLSClientConfig{},
	}

	//defer func() { buildConfigFromFlags = origNewKubernetesClientSet }()
	buildConfigFromFlags = func(masterUrl, kubeconfigPath string) (config *rest.Config, err error) {
		return testConfig, nil
	}

	k8sClientSet = func(c *rest.Config) (kubernetes.Interface, error) {
		kubeClient := k8sfake.NewSimpleClientset()
		_, _ = kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: "busybox",
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					v1.Container{
						Name:      "main",
						Image:     "python:3.8",
						Command:   []string{"python"},
						Args:      []string{"-c", "print('hello world')"},
						Resources: getResourceRequirements(),
					},
				},
			},
		}, metav1.CreateOptions{})
		return kubeClient, nil
	}
}

func getResourceRequirements() v1.ResourceRequirements {
	res := v1.ResourceRequirements{}
	requests := v1.ResourceList{}
	limits := v1.ResourceList{}
	requests["rdma/roce"] = resource.MustParse("1")
	limits["rdma/roce"] = resource.MustParse("1")
	res.Requests = requests
	res.Limits = limits
	return res
}
