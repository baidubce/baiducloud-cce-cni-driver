/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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
	"fmt"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/hpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/eip"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	grate "golang.org/x/time/rate"
)

const (
	AddPrivateIP                 = "bcecloud/apis/v1/AddPrivateIP"
	DeletePrivateIP              = "bcecloud/apis/v1/DeletePrivateIP"
	BindENIPublicIP              = "bcecloud/apis/v1/BindENIPulblicIP"
	UnBindENIPublicIP            = "bcecloud/apis/v1/UnbindENIPulblicIP"
	DirectEIP                    = "bcecloud/apis/v1/DirectEIP"
	UnDirectEIP                  = "bcecloud/apis/v1/UnDirectEIP"
	ListEIPs                     = "bcecloud/apis/v1/ListEIPs"
	BatchAddPrivateIpCrossSubnet = "bcecloud/apis/v1/BatchAddPrivateIpCrossSubnet"
	BatchAddPrivateIP            = "bcecloud/apis/v1/BatchAddPrivateIP"
	BatchDeletePrivateIP         = "bcecloud/apis/v1/BatchDeletePrivateIP"

	CreateENI = "bcecloud/apis/v1/CreateENI"
	DeleteENI = "bcecloud/apis/v1/DeleteENI"
	AttachENI = "bcecloud/apis/v1/AttachENI"
	DetachENI = "bcecloud/apis/v1/DetachENI"
	StatENI   = "bcecloud/apis/v1/StatENI"
	ListENIs  = "bcecloud/apis/v1/ListENIs"
	ListERIs  = "bcecloud/apis/v1/ListERIs"

	ListRouteTable  = "bcecloud/apis/v1/ListRouteTable"
	CreateRouteRule = "bcecloud/apis/v1/CreateRouteRule"
	DeleteRouteRule = "bcecloud/apis/v1/DeleteRouteRule"

	DescribeSubnet = "bcecloud/apis/v1/DescribeSubnet"
	ListSubnets    = "bcecloud/apis/v1/ListSubnets"

	ListSecurityGroup = "bcecloud/apis/v1/ListSecurityGroup"

	GetBCCInstanceDetail = "bcecloud/apis/v1/GetBCCInstanceDetail"
	GetBBCInstanceDetail = "bcecloud/apis/v1/GetBBCInstanceDetail"
	GetBBCInstanceENI    = "bcecloud/apis/v1/GetBBCInstanceENI"

	BBCBatchAddIP            = "bcecloud/apis/v1/BBCBatchAddIP"
	BBCBatchDelIP            = "bcecloud/apis/v1/BBCBatchDelIP"
	BBCBatchAddIPCrossSubnet = "bcecloud/apis/v1/BBCBatchAddIPCrossSubnet"

	GetHPCEniID                = "bcecloud/apis/v1/GetHPCEniID"
	BatchDeleteHpcEniPrivateIP = "bcecloud/apis/v1/BatchDeleteHpcEniPrivateIP"
	BatchAddHpcEniPrivateIP    = "bcecloud/apis/v1/BatchAddHpcEniPrivateIP"
)

var apiRateLimitDefaults = map[string]rate.APILimiterParameters{
	BatchAddPrivateIP: {
		RateLimit:        5,
		RateBurst:        10,
		ParallelRequests: 5,
		MaxWaitDuration:  30 * time.Second,
		Log:              true,
	}, BatchDeletePrivateIP: {
		RateLimit:        5,
		RateBurst:        10,
		ParallelRequests: 5,
		MaxWaitDuration:  30 * time.Second,
		Log:              true,
	},
	// for eni
	CreateENI: {
		RateLimit:        5,
		RateBurst:        5,
		ParallelRequests: 5,
		MaxWaitDuration:  30 * time.Second,
		Log:              true,
	}, DeleteENI: {
		RateLimit:        1,
		RateBurst:        1,
		ParallelRequests: 1,
		MaxWaitDuration:  30 * time.Second,
		Log:              true,
	}, AttachENI: {
		RateLimit:        5,
		RateBurst:        5,
		ParallelRequests: 5,
		MaxWaitDuration:  30 * time.Second,
		Log:              true,
	}, StatENI: {
		RateLimit:        5,
		RateBurst:        5,
		ParallelRequests: 5,
		MaxWaitDuration:  30 * time.Second,
		Log:              false,
	}, ListENIs: {
		RateLimit:        1,
		RateBurst:        1,
		ParallelRequests: 1,
		MaxWaitDuration:  30 * time.Second,
		Log:              false,
	}, ListERIs: {
		RateLimit:        1,
		RateBurst:        1,
		ParallelRequests: 1,
		MaxWaitDuration:  30 * time.Second,
		Log:              false,
	},

	// for route table
	ListRouteTable: {
		RateLimit:        1,
		RateBurst:        1,
		ParallelRequests: 1,
		MaxWaitDuration:  30 * time.Second,
		Log:              false,
	}, CreateRouteRule: {
		RateLimit:        5,
		RateBurst:        10,
		ParallelRequests: 5,
		MaxWaitDuration:  30 * time.Second,
		Log:              true,
	}, DeleteRouteRule: {
		RateLimit:        5,
		RateBurst:        10,
		ParallelRequests: 5,
		MaxWaitDuration:  30 * time.Second,
		Log:              true,
	},
	// for subnet
	DescribeSubnet: {
		RateLimit:        5,
		RateBurst:        10,
		ParallelRequests: 5,
		MaxWaitDuration:  5 * time.Second,
		Log:              false,
	}, ListSubnets: {
		RateLimit:        1,
		RateBurst:        1,
		ParallelRequests: 1,
		MaxWaitDuration:  30 * time.Second,
		Log:              false,
	},
	// for instance
	GetBCCInstanceDetail: {
		RateLimit:        1,
		RateBurst:        1,
		ParallelRequests: 1,
		MaxWaitDuration:  30 * time.Second,
		Log:              false,
	}, GetBBCInstanceDetail: {
		RateLimit:        1,
		RateBurst:        1,
		ParallelRequests: 1,
		MaxWaitDuration:  30 * time.Second,
		Log:              false,
	},
}

// flowControlClient implement `Interface` is a client with flow control.
// It will proxy all the method call to the underlying client, and wrapper with:
// limiter.Wait(ctx) to control the rate of the method call.
type flowControlClient struct {
	client  Interface
	limiter rate.ServiceLimiterManager
}

// NewFlowControlClient returns a client with flow control.
func NewFlowControlClient(client Interface, qps float64, burst int, timeout time.Duration) (Interface, error) {
	apiLimiterSet, err := rate.NewAPILimiterSet(option.Config.APIRateLimit, apiRateLimitDefaults, rate.SimpleMetricsObserver)
	if err != nil {
		log.WithError(err).Error("unable to configure API rate limiting")
		return nil, fmt.Errorf("unable to configure API rate limiting: %w", err)
	}
	return &flowControlClient{
		client: client,
		limiter: rate.APILimiterSetWrapDefault(apiLimiterSet, &rate.APILimiterParameters{
			RateLimit:       grate.Limit(qps),
			RateBurst:       burst,
			MaxWaitDuration: timeout,
		}),
	}, nil
}

// AddPrivateIP implements Interface
func (fc *flowControlClient) AddPrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) (string, error) {
	req, err := fc.limiter.Wait(ctx, AddPrivateIP)
	if err != nil {
		return "", err
	}

	ret, err := fc.client.AddPrivateIP(ctx, privateIP, eniID, isIpv6)

	req.Error(err)
	return ret, err
}

// AttachENI implements Interface
func (fc *flowControlClient) AttachENI(ctx context.Context, args *eni.EniInstance) error {
	req, err := fc.limiter.Wait(ctx, AttachENI)
	if err != nil {
		return err
	}

	err = fc.client.AttachENI(ctx, args)
	req.Error(err)
	return err
}

// BBCBatchAddIP implements Interface
func (fc *flowControlClient) BBCBatchAddIP(ctx context.Context, args *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error) {
	req, err := fc.limiter.Wait(ctx, BBCBatchAddIP)
	if err != nil {
		return nil, err
	}

	ret, err := fc.client.BBCBatchAddIP(ctx, args)
	req.Error(err)
	return ret, err
}

// BBCBatchAddIPCrossSubnet implements Interface
func (fc *flowControlClient) BBCBatchAddIPCrossSubnet(ctx context.Context, args *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error) {
	req, err := fc.limiter.Wait(ctx, BBCBatchAddIPCrossSubnet)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.BBCBatchAddIPCrossSubnet(ctx, args)
	req.Error(err)
	return ret, err
}

// BBCBatchDelIP implements Interface
func (fc *flowControlClient) BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error {
	req, err := fc.limiter.Wait(ctx, BBCBatchDelIP)
	if err != nil {
		return err
	}
	err = fc.client.BBCBatchDelIP(ctx, args)
	req.Error(err)
	return err
}

// BatchAddHpcEniPrivateIP implements Interface
func (fc *flowControlClient) BatchAddHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchPrivateIPArgs) (*hpc.BatchAddPrivateIPResult, error) {
	req, err := fc.limiter.Wait(ctx, BatchAddHpcEniPrivateIP)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.BatchAddHpcEniPrivateIP(ctx, args)
	req.Error(err)
	return ret, err
}

// BatchAddPrivateIP implements Interface
func (fc *flowControlClient) BatchAddPrivateIP(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool) ([]string, error) {
	req, err := fc.limiter.Wait(ctx, BatchAddPrivateIP)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.BatchAddPrivateIP(ctx, privateIPs, count, eniID, isIpv6)
	req.Error(err)
	return ret, err
}

// BatchAddPrivateIpCrossSubnet implements Interface
func (fc *flowControlClient) BatchAddPrivateIpCrossSubnet(ctx context.Context, eniID string, subnetID string, privateIPs []string, count int, isIpv6 bool) ([]string, error) {
	req, err := fc.limiter.Wait(ctx, BatchAddPrivateIpCrossSubnet)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.BatchAddPrivateIpCrossSubnet(ctx, eniID, subnetID, privateIPs, count, isIpv6)
	req.Error(err)
	return ret, err
}

// BatchDeleteHpcEniPrivateIP implements Interface
func (fc *flowControlClient) BatchDeleteHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchDeleteIPArgs) error {
	req, err := fc.limiter.Wait(ctx, BatchDeleteHpcEniPrivateIP)
	if err != nil {
		return err
	}
	err = fc.client.BatchDeleteHpcEniPrivateIP(ctx, args)
	req.Error(err)
	return err
}

// BatchDeletePrivateIP implements Interface
func (fc *flowControlClient) BatchDeletePrivateIP(ctx context.Context, privateIPs []string, eniID string, isIpv6 bool) error {
	req, err := fc.limiter.Wait(ctx, BatchDeletePrivateIP)
	if err != nil {
		return err
	}
	err = fc.client.BatchDeletePrivateIP(ctx, privateIPs, eniID, isIpv6)
	req.Error(err)
	return err
}

// CreateENI implements Interface
func (fc *flowControlClient) CreateENI(ctx context.Context, args *eni.CreateEniArgs) (string, error) {
	req, err := fc.limiter.Wait(ctx, CreateENI)
	if err != nil {
		return "", err
	}
	ret, err := fc.client.CreateENI(ctx, args)
	req.Error(err)
	return ret, err
}

// CreateRouteRule implements Interface
func (fc *flowControlClient) CreateRouteRule(ctx context.Context, args *vpc.CreateRouteRuleArgs) (string, error) {
	req, err := fc.limiter.Wait(ctx, CreateRouteRule)
	if err != nil {
		return "", err
	}
	ret, err := fc.client.CreateRouteRule(ctx, args)
	req.Error(err)
	return ret, err
}

// DeleteENI implements Interface
func (fc *flowControlClient) DeleteENI(ctx context.Context, eniID string) error {
	req, err := fc.limiter.Wait(ctx, DeleteENI)
	if err != nil {
		return err
	}
	err = fc.client.DeleteENI(ctx, eniID)
	req.Error(err)
	return err
}

// DeletePrivateIP implements Interface
func (fc *flowControlClient) DeletePrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) error {
	req, err := fc.limiter.Wait(ctx, DeletePrivateIP)
	if err != nil {
		return err
	}
	err = fc.client.DeletePrivateIP(ctx, privateIP, eniID, isIpv6)
	req.Error(err)
	return err
}

// BindENIPublicIP implements Interface
func (fc *flowControlClient) BindENIPublicIP(ctx context.Context, privateIP string, publicIP string, eniID string) error {
	req, err := fc.limiter.Wait(ctx, UnBindENIPublicIP)
	if err != nil {
		return err
	}
	err = fc.client.BindENIPublicIP(ctx, privateIP, publicIP, eniID)
	req.Error(err)
	return err
}

// UnBindENIPublicIP implements Interface
func (fc *flowControlClient) UnBindENIPublicIP(ctx context.Context, publicIP string, eniID string) error {
	req, err := fc.limiter.Wait(ctx, UnBindENIPublicIP)
	if err != nil {
		return err
	}
	err = fc.client.UnBindENIPublicIP(ctx, publicIP, eniID)
	req.Error(err)
	return err
}

// DirectEIP implements Interface
func (fc *flowControlClient) DirectEIP(ctx context.Context, eip string) error {
	req, err := fc.limiter.Wait(ctx, DirectEIP)
	if err != nil {
		return err
	}
	err = fc.client.DirectEIP(ctx, eip)
	req.Error(err)
	return err
}

// UnDirectEIP implements Interface
func (fc *flowControlClient) UnDirectEIP(ctx context.Context, eip string) error {
	req, err := fc.limiter.Wait(ctx, UnDirectEIP)
	if err != nil {
		return err
	}
	err = fc.client.UnDirectEIP(ctx, eip)
	req.Error(err)
	return err
}

// ListEIPs implements Interface
func (fc *flowControlClient) ListEIPs(ctx context.Context, args eip.ListEipArgs) ([]eip.EipModel, error) {
	req, err := fc.limiter.Wait(ctx, ListEIPs)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.ListEIPs(ctx, args)
	req.Error(err)
	return ret, err
}

// DeleteRouteRule implements Interface
func (fc *flowControlClient) DeleteRouteRule(ctx context.Context, routeID string) error {
	req, err := fc.limiter.Wait(ctx, DeleteRouteRule)
	if err != nil {
		return err
	}
	err = fc.client.DeleteRouteRule(ctx, routeID)
	req.Error(err)
	return err
}

// DescribeSubnet implements Interface
func (fc *flowControlClient) DescribeSubnet(ctx context.Context, subnetID string) (*vpc.Subnet, error) {
	req, err := fc.limiter.Wait(ctx, DescribeSubnet)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.DescribeSubnet(ctx, subnetID)
	req.Error(err)
	return ret, err
}

// DetachENI implements Interface
func (fc *flowControlClient) DetachENI(ctx context.Context, args *eni.EniInstance) error {
	req, err := fc.limiter.Wait(ctx, DetachENI)
	if err != nil {
		return err
	}
	err = fc.client.DetachENI(ctx, args)
	req.Error(err)
	return err
}

// GetBBCInstanceDetail implements Interface
func (fc *flowControlClient) GetBBCInstanceDetail(ctx context.Context, instanceID string) (*bbc.InstanceModel, error) {
	req, err := fc.limiter.Wait(ctx, GetBBCInstanceDetail)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.GetBBCInstanceDetail(ctx, instanceID)
	req.Error(err)
	return ret, err
}

// GetBBCInstanceENI implements Interface
func (fc *flowControlClient) GetBBCInstanceENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error) {
	req, err := fc.limiter.Wait(ctx, GetBBCInstanceENI)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.GetBBCInstanceENI(ctx, instanceID)
	req.Error(err)
	return ret, err
}

// GetBCCInstanceDetail implements Interface
func (fc *flowControlClient) GetBCCInstanceDetail(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error) {
	req, err := fc.limiter.Wait(ctx, GetBCCInstanceDetail)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.GetBCCInstanceDetail(ctx, instanceID)
	req.Error(err)
	return ret, err
}

// GetHPCEniID implements Interface
func (fc *flowControlClient) GetHPCEniID(ctx context.Context, instanceID string) (*hpc.EniList, error) {
	req, err := fc.limiter.Wait(ctx, GetHPCEniID)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.GetHPCEniID(ctx, instanceID)
	req.Error(err)
	return ret, err
}

// ListENIs implements Interface
func (fc *flowControlClient) ListENIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	req, err := fc.limiter.Wait(ctx, ListENIs)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.ListENIs(ctx, args)
	req.Error(err)
	return ret, err
}

// ListERIs implements Interface
func (fc *flowControlClient) ListERIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	req, err := fc.limiter.Wait(ctx, ListERIs)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.ListERIs(ctx, args)
	req.Error(err)
	return ret, err
}

// ListRouteTable implements Interface
func (fc *flowControlClient) ListRouteTable(ctx context.Context, vpcID string, routeTableID string) ([]vpc.RouteRule, error) {
	req, err := fc.limiter.Wait(ctx, ListRouteTable)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.ListRouteTable(ctx, vpcID, routeTableID)
	req.Error(err)
	return ret, err
}

// ListSecurityGroup implements Interface
func (fc *flowControlClient) ListSecurityGroup(ctx context.Context, vpcID string, instanceID string) ([]bccapi.SecurityGroupModel, error) {
	req, err := fc.limiter.Wait(ctx, ListSecurityGroup)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.ListSecurityGroup(ctx, vpcID, instanceID)
	req.Error(err)
	return ret, err
}

// ListSubnets implements Interface
func (fc *flowControlClient) ListSubnets(ctx context.Context, args *vpc.ListSubnetArgs) ([]vpc.Subnet, error) {
	req, err := fc.limiter.Wait(ctx, ListSubnets)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.ListSubnets(ctx, args)
	req.Error(err)
	return ret, err
}

// StatENI implements Interface
func (fc *flowControlClient) StatENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	req, err := fc.limiter.Wait(ctx, StatENI)
	if err != nil {
		return nil, err
	}
	ret, err := fc.client.StatENI(ctx, eniID)
	req.Error(err)
	return ret, err
}

var _ Interface = &flowControlClient{}
