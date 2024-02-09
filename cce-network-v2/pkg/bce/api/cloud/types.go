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

package cloud

import (
	"context"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/baidubce/bce-sdk-go/services/bcc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/eip"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"

	eniExt "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/hpc"
)

type Interface interface {
	ListENIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error)
	ListERIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error)
	AddPrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) (string, error)
	DeletePrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) error

	BindENIPublicIP(ctx context.Context, privateIP string, publicIP string, eniID string) error
	UnBindENIPublicIP(ctx context.Context, publicIP string, eniID string) error
	DirectEIP(ctx context.Context, eip string) error
	UnDirectEIP(ctx context.Context, eip string) error
	ListEIPs(ctx context.Context, args eip.ListEipArgs) ([]eip.EipModel, error)

	// BatchAddPrivateIpCrossSubnet
	// Assign IP addresses to Eni across subnets.
	// Note that this feature needs to initiate a work order in advance to
	// enable the cross subnet IP allocation function
	BatchAddPrivateIpCrossSubnet(ctx context.Context, eniID, subnetID string, privateIPs []string, count int, isIpv6 bool) ([]string, error)
	BatchAddPrivateIP(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool) ([]string, error)
	BatchDeletePrivateIP(ctx context.Context, privateIPs []string, eniID string, isIpv6 bool) error
	CreateENI(ctx context.Context, args *eni.CreateEniArgs) (string, error)
	DeleteENI(ctx context.Context, eniID string) error
	AttachENI(ctx context.Context, args *eni.EniInstance) error
	DetachENI(ctx context.Context, args *eni.EniInstance) error
	StatENI(ctx context.Context, eniID string) (*eni.Eni, error)
	GetENIQuota(ctx context.Context, instanceID string) (*eni.EniQuoteInfo, error)

	// ListBCCInstanceEni Query the list of BCC eni network interface.
	// Unlike the VPC interface, this interface can query the primaty network interface of BCC/EBC
	// However, the `ListENIs`` and `StatENI`` interfaces of VPC cannot retrieve relevant information
	// about the primary network interface of BCC/EBC.
	ListBCCInstanceEni(ctx context.Context, instanceID string) ([]bccapi.Eni, error)

	// BCCBatchAddIP batch add secondary IP to primary interface of BCC/EBC
	BCCBatchAddIP(ctx context.Context, args *bccapi.BatchAddIpArgs) (*bccapi.BatchAddIpResponse, error)

	// BCCBatchDelIP batch delete secondary IP to primary interface of BCC/EBC
	// Waring: Do not mistakenly delete the primary IP address of the main network card
	BCCBatchDelIP(ctx context.Context, args *bccapi.BatchDelIpArgs) error

	ListRouteTable(ctx context.Context, vpcID, routeTableID string) ([]vpc.RouteRule, error)
	CreateRouteRule(ctx context.Context, args *vpc.CreateRouteRuleArgs) (string, error)
	DeleteRouteRule(ctx context.Context, routeID string) error

	DescribeSubnet(ctx context.Context, subnetID string) (*vpc.Subnet, error)
	ListSubnets(ctx context.Context, args *vpc.ListSubnetArgs) ([]vpc.Subnet, error)

	ListSecurityGroup(ctx context.Context, vpcID, instanceID string) ([]bccapi.SecurityGroupModel, error)

	GetBCCInstanceDetail(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error)

	GetBBCInstanceDetail(ctx context.Context, instanceID string) (*bbc.InstanceModel, error)
	GetBBCInstanceENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error)
	BBCBatchAddIP(ctx context.Context, args *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error)
	BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error
	BBCBatchAddIPCrossSubnet(ctx context.Context, args *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error)

	GetHPCEniID(ctx context.Context, instanceID string) (*hpc.EniList, error)
	BatchDeleteHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchDeleteIPArgs) error
	BatchAddHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchPrivateIPArgs) (*hpc.BatchAddPrivateIPResult, error)
}

type Client struct {
	bccClient *bcc.Client
	eipClient *eip.Client
	eniClient *eniExt.Client
	vpcClient *vpc.Client
	hpcClient *hpc.Client
	bbcClient *bbc.Client
}

var (
	BCCEndpoints = map[string]string{
		"bj":  "bcc.bj.baidubce.com",
		"gz":  "bcc.gz.baidubce.com",
		"su":  "bcc.su.baidubce.com",
		"hkg": "bcc.hkg.baidubce.com",
		"fwh": "bcc.fwh.baidubce.com",
		"bd":  "bcc.bd.baidubce.com",
		// 新加坡
		"sin": "bcc.sin.baidubce.com",
		// 上海
		"fsh":     "bcc.fsh.baidubce.com",
		"yq":      "bcc.yq.baidubce.com",
		"nj":      "bcc.nj.baidubce.com",
		"cd":      "bcc.cd.baidubce.com",
		"sandbox": "bcc.bj.qasandbox.baidu-int.com",
	}

	BBCEndpoints = map[string]string{
		"bj":  "bbc.bj.baidubce.com",
		"gz":  "bbc.gz.baidubce.com",
		"su":  "bbc.su.baidubce.com",
		"hkg": "bbc.hkg.baidubce.com",
		"fwh": "bbc.fwh.baidubce.com",
		"bd":  "bbc.bd.baidubce.com",
		// 新加坡
		"sin": "bbc.sin.baidubce.com",
		// 上海
		"fsh":     "bbc.fsh.baidubce.com",
		"yq":      "bbc.yq.baidubce.com",
		"nj":      "bbc.nj.baidubce.com",
		"cd":      "bbc.cd.baidubce.com",
		"sandbox": "bbc.bj.qasandbox.baidu-int.com",
	}

	EIPEndpoints = map[string]string{
		"bj":      "eip.bj.baidubce.com",
		"gz":      "eip.gz.baidubce.com",
		"su":      "eip.su.baidubce.com",
		"hkg":     "eip.hkg.baidubce.com",
		"fwh":     "eip.fwh.baidubce.com",
		"bd":      "eip.bd.baidubce.com",
		"sin":     "eip.sin.baidubce.com",
		"fsh":     "eip.fsh.baidubce.com",
		"yq":      "eip.yq.baidubce.com",
		"nj":      "eip.nj.baidubce.com",
		"cd":      "eip.cd.baidubce.com",
		"sandbox": "eip.bj.qasandbox.baidu-int.com",
	}
)

var _ Interface = &Client{}
