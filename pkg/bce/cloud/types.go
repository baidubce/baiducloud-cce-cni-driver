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
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"
)

type Interface interface {
	ListENIs(ctx context.Context, vpcID string) ([]eni.Eni, error)
	AddPrivateIP(ctx context.Context, privateIP string, eniID string) (string, error)
	DeletePrivateIP(ctx context.Context, privateIP string, eniID string) error
	CreateENI(ctx context.Context, args *eni.CreateEniArgs) (string, error)
	DeleteENI(ctx context.Context, eniID string) error
	AttachENI(ctx context.Context, args *eni.EniInstance) error
	DetachENI(ctx context.Context, args *eni.EniInstance) error
	StatENI(ctx context.Context, eniID string) (*eni.Eni, error)

	ListRouteTable(ctx context.Context, vpcID, routeTableID string) ([]vpc.RouteRule, error)
	CreateRouteRule(ctx context.Context, args *vpc.CreateRouteRuleArgs) (string, error)
	DeleteRoute(ctx context.Context, routeID string) error

	DescribeSubnet(ctx context.Context, subnetID string) (*vpc.Subnet, error)
	ListSubnets(ctx context.Context, args *vpc.ListSubnetArgs) ([]vpc.Subnet, error)

	DescribeInstance(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error)

	BBCGetInstanceDetail(ctx context.Context, instanceID string) (*bbc.InstanceModel, error)
	BBCGetInstanceENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error)
	BBCBatchAddIP(ctx context.Context, args *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error)
	BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error
}

type Client struct {
	bccClient *bcc.Client
	eniClient *eni.Client
	vpcClient *vpc.Client
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
	}

	BBCEndpoints = map[string]string{
		"bj":  "bbc.bj.baidubce.com",
		"gz":  "bbc.gz.baidubce.com",
		"su":  "bbc.su.baidubce.com",
		"hkg": "bbc.hkg.baidubce.com",
		"fwh": "bbc.fwh.baidubce.com",
		"bd":  "bbc.bd.baidubce.com",
	}
)

var _ Interface = &Client{}
