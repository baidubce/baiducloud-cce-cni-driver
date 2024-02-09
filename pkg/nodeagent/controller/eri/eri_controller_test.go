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

package eri

import (
	"context"
	"net"

	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"

	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	mockmetadata "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/rdma/client"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	mockutilnetwork "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network/testing"
	mocktypes "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes/testing"
	mockip "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip/testing"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	mocknetlink "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink/testing"
	mockns "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns/testing"
	mockrpc "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc/testing"
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
	mocksysctl "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl/testing"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
)

func setupEnv(ctrl *gomock.Controller) (
	*mocknetlink.MockInterface,
	*mockns.MockInterface,
	*mockip.MockInterface,
	*mocktypes.MockInterface,
	*mockutilnetwork.MockInterface,
	*mockrpc.MockInterface,
	*mocksysctl.MockInterface,
) {
	nlink := mocknetlink.NewMockInterface(ctrl)
	ns := mockns.NewMockInterface(ctrl)
	ip := mockip.NewMockInterface(ctrl)
	types := mocktypes.NewMockInterface(ctrl)
	netutil := mockutilnetwork.NewMockInterface(ctrl)
	rpc := mockrpc.NewMockInterface(ctrl)
	sysctl := mocksysctl.NewMockInterface(ctrl)
	return nlink, ns, ip, types, netutil, rpc, sysctl
}

func Test_ReconcileERIs(t *testing.T) {
	type fields struct {
		metaClient    metadata.Interface
		eriClient     *client.EriClient
		netlink       netlinkwrapper.Interface
		sysctl        sysctlwrapper.Interface
		netutil       networkutil.Interface
		clusterID     string
		nodeName      string
		instanceID    string
		vpcID         string
		eniSyncPeriod time.Duration
	}
	type args struct {
		ctx     context.Context
		nodeKey string
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
				nlink, _, _, _, netutil, _, sysctl := setupEnv(ctrl)
				metaClient := mockmetadata.NewMockInterface(ctrl)

				metaClient.EXPECT().GetLinkMask(gomock.Any(), gomock.Any()).Return("24", nil).AnyTimes()
				bceclound := mockcloud.NewMockInterface(ctrl)
				eriClient := client.NewEriClient(bceclound)

				bceclound.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]enisdk.Eni{
					//eriClient.cloud.(*mockcloud.MockInterface).EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]enisdk.Eni{
					{
						MacAddress: "01:01:01:01:01:01",
						PrivateIpSet: []enisdk.PrivateIp{
							{
								Primary:          true,
								PrivateIpAddress: "1.1.1.1",
							},
							{
								Primary:          false,
								PrivateIpAddress: "2.2.2.2",
							},
						},
					},
				}, nil).AnyTimes()

				macVlanDev := &netlink.Macvlan{
					LinkAttrs: netlink.LinkAttrs{
						HardwareAddr: []byte{1, 1, 1, 1, 1, 1},
					},
				}
				nlink.EXPECT().LinkList().Return([]netlink.Link{macVlanDev}, nil).AnyTimes()
				nlink.EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				nlink.EXPECT().AddrList(gomock.Any(), gomock.Any()).Return([]netlink.Addr{
					{
						IPNet: &net.IPNet{
							IP:   net.IPv4(25, 0, 0, 45),
							Mask: net.CIDRMask(24, 32),
						},
					},
				}, nil).AnyTimes()
				nlink.EXPECT().LinkSetUp(gomock.Any()).Return(nil).AnyTimes()
				nlink.EXPECT().LinkSetMTU(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				netutil.EXPECT().DetectDefaultRouteInterfaceName().Return("eth0", nil).AnyTimes()
				sysctl.EXPECT().Sysctl(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()
				return fields{
					metaClient:    metaClient,
					eriClient:     eriClient,
					netlink:       nlink,
					sysctl:        sysctl,
					netutil:       netutil,
					clusterID:     "cluster1",
					nodeName:      "node1",
					instanceID:    "ins1",
					vpcID:         "vpcid1",
					eniSyncPeriod: 5 * time.Second,
				}
			}(),
			args: args{
				ctx:     context.TODO(),
				nodeKey: "test-node",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		noforever = true

		t.Run(tt.name, func(t *testing.T) {
			c := New(tt.fields.metaClient,
				tt.fields.eriClient,
				tt.fields.netlink,
				tt.fields.sysctl,
				tt.fields.netutil,
				tt.fields.clusterID,
				tt.fields.nodeName,
				tt.fields.instanceID,
				tt.fields.vpcID,
				tt.fields.eniSyncPeriod)
			c.ReconcileERIs()
		})
	}
}

func Test_getInterfaceIndex(t *testing.T) {
	macVlanDev := &netlink.Macvlan{
		LinkAttrs: netlink.LinkAttrs{
			HardwareAddr: []byte{1, 1, 1, 1, 1, 1},
		},
	}
	macVlanDev.Name = "eth2"
	_, _ = getInterfaceIndex(netlink.Link(macVlanDev))

	macVlanDev.Name = "e"
	_, _ = getInterfaceIndex(netlink.Link(macVlanDev))

	macVlanDev.Name = "eths"
	_, _ = getInterfaceIndex(netlink.Link(macVlanDev))
}
