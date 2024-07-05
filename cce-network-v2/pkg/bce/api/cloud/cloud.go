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
	"os"
	"time"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/baidubce/bce-sdk-go/services/bcc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/eip"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/esg"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	sdklog "github.com/baidubce/bce-sdk-go/util/log"
	"k8s.io/client-go/kubernetes"

	eniExt "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/hpc"
)

const (
	BCCEndpointEnv = "BCC_ENDPOINT"
	BBCEndpointEnv = "BBC_ENDPOINT"
	EIPEndpointEnv = "EIP_ENDPOINT"
)

func newBCEClientConfig(ctx context.Context,
	region,
	endpointEnv string,
	preDefinedEndpoints map[string]string,
	auth Auth,
	timeout time.Duration,
) *bce.BceClientConfiguration {
	endpoint, exist := os.LookupEnv(endpointEnv)
	if !exist || endpoint == "" {
		endpoint = preDefinedEndpoints[region]
	}

	return &bce.BceClientConfiguration{
		Endpoint:                  endpoint,
		Region:                    region,
		UserAgent:                 bce.DEFAULT_USER_AGENT,
		Credentials:               auth.GetCredentials(ctx),
		SignOption:                auth.GetSignOptions(ctx),
		Retry:                     bce.NewNoRetryPolicy(),
		ConnectionTimeoutInMillis: int(timeout.Milliseconds()),
	}
}

func New(
	region string,
	clusterID string,
	accessKeyID string,
	secretAccessKey string,
	kubeClient kubernetes.Interface,
	debug bool,
	timeout time.Duration,
) (Interface, error) {
	ctx := context.TODO()
	// set logrus as bce sdk default logger
	sdklog.SetLogger(&bceLogger{})

	var auth Auth
	var err error
	if accessKeyID != "" && secretAccessKey != "" {
		auth, err = NewAccessKeyPairAuth(accessKeyID, secretAccessKey, "")
	} else {
		auth, err = NewCCEGatewayAuth(region, clusterID, kubeClient)
	}

	if err != nil {
		return nil, err
	}

	bccClientConfig := newBCEClientConfig(ctx, region, BCCEndpointEnv, BCCEndpoints, auth, timeout)
	bbcClientConfig := newBCEClientConfig(ctx, region, BBCEndpointEnv, BBCEndpoints, auth, timeout)
	eipClientConfig := newBCEClientConfig(ctx, region, EIPEndpointEnv, EIPEndpoints, auth, timeout)

	vpcClient := &vpc.Client{
		BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx)),
	}

	bccClient := &bcc.Client{
		BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx)),
	}

	eipClient := &eip.Client{
		BceClient: bce.NewBceClient(eipClientConfig, auth.GetSigner(ctx)),
	}

	// todo iaas sdk 暂未支持过滤 eri 和 eni，暂时自行封装一层支持，待后续 sdk 支持过滤 eri 和 eni 后，去除这部分封装
	eniClient := &eniExt.Client{
		Client: &eni.Client{BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx))},
	}

	bbcClient := &bbc.Client{
		BceClient: bce.NewBceClient(bbcClientConfig, auth.GetSigner(ctx)),
	}

	hpcClient := &hpc.Client{
		BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx)),
	}

	esgClient := &esg.Client{
		BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx)),
	}
	c := &Client{
		vpcClient: vpcClient,
		hpcClient: hpcClient,
		bccClient: bccClient,
		eipClient: eipClient,
		eniClient: eniClient,
		bbcClient: bbcClient,
		esgClient: esgClient,
	}
	return c, nil
}

func (c *Client) ListENIs(_ context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	var enis []eni.Eni

	isTruncated := true
	nextMarker := ""

	for isTruncated {
		t := time.Now()

		listArgs := &eni.ListEniArgs{
			VpcId:      args.VpcId,
			Name:       args.Name,
			InstanceId: args.InstanceId,
			Marker:     nextMarker,
		}

		res, err := c.eniClient.ListEnis(listArgs)
		exportMetric("ListENI", t, err)
		if err != nil {
			return nil, err
		}

		enis = append(enis, res.Eni...)

		nextMarker = res.NextMarker
		isTruncated = res.IsTruncated
	}

	return enis, nil
}

func (c *Client) ListERIs(_ context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	var enis []eni.Eni

	isTruncated := true
	nextMarker := ""

	for isTruncated {
		t := time.Now()

		listArgs := &eni.ListEniArgs{
			VpcId:      args.VpcId,
			Name:       args.Name,
			InstanceId: args.InstanceId,
			Marker:     nextMarker,
		}

		res, err := c.eniClient.ListEris(listArgs)
		exportMetric("ListERI", t, err)
		if err != nil {
			return nil, err
		}

		enis = append(enis, res.Eni...)

		nextMarker = res.NextMarker
		isTruncated = res.IsTruncated
	}

	return enis, nil
}

func (c *Client) AddPrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) (string, error) {
	t := time.Now()
	resp, err := c.eniClient.AddPrivateIp(&eni.EniPrivateIpArgs{
		EniId:            eniID,
		PrivateIpAddress: privateIP,
		IsIpv6:           isIpv6,
	})
	exportMetricAndLog(ctx, "AddPrivateIP", t, err)

	if err != nil {
		return "", err
	}

	return resp.PrivateIpAddress, nil
}

func (c *Client) DeletePrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) error {
	t := time.Now()
	err := c.eniClient.DeletePrivateIp(&eni.EniPrivateIpArgs{
		EniId:            eniID,
		PrivateIpAddress: privateIP,
		IsIpv6:           isIpv6,
	})
	exportMetricAndLog(ctx, "DeletePrivateIP", t, err)
	return err
}

func (c *Client) BindENIPublicIP(ctx context.Context, privateIP string, publicIP string, eniID string) error {
	t := time.Now()
	err := c.eniClient.BindEniPublicIp(&eni.BindEniPublicIpArgs{
		EniId:            eniID,
		PrivateIpAddress: privateIP,
		PublicIpAddress:  publicIP,
	})
	exportMetricAndLog(ctx, "BindENIPublicIP", t, err)
	return err
}

func (c *Client) UnBindENIPublicIP(ctx context.Context, publicIP string, eniID string) error {
	t := time.Now()
	err := c.eniClient.UnBindEniPublicIp(&eni.UnBindEniPublicIpArgs{
		EniId:           eniID,
		PublicIpAddress: publicIP,
	})
	exportMetricAndLog(ctx, "UnBindENIPublicIP", t, err)
	return err
}

func (c *Client) DirectEIP(ctx context.Context, eip string) error {
	t := time.Now()
	err := c.eipClient.DirectEip(eip, "")
	exportMetricAndLog(ctx, "DirectEIP", t, err)
	return err
}

func (c *Client) UnDirectEIP(ctx context.Context, eip string) error {
	t := time.Now()
	err := c.eipClient.UnDirectEip(eip, "")
	exportMetricAndLog(ctx, "UnDirectEIP", t, err)
	return err
}

func (c *Client) ListEIPs(_ context.Context, args eip.ListEipArgs) ([]eip.EipModel, error) {
	var eips []eip.EipModel

	isTruncated := true
	nextMarker := ""

	for isTruncated {
		t := time.Now()

		args.Marker = nextMarker

		res, err := c.eipClient.ListEip(&args)
		exportMetric("ListEIP", t, err)
		if err != nil {
			return nil, err
		}

		eips = append(eips, res.EipList...)

		nextMarker = res.NextMarker
		isTruncated = res.IsTruncated
	}

	return eips, nil
}

func (c *Client) BatchAddPrivateIP(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool) ([]string, error) {
	t := time.Now()

	resp, err := c.eniClient.BatchAddPrivateIp(&eni.EniBatchPrivateIpArgs{
		EniId:                 eniID,
		PrivateIpAddresses:    privateIPs,
		PrivateIpAddressCount: count,
		IsIpv6:                isIpv6,
	})

	exportMetricAndLog(ctx, "BatchAddPrivateIP", t, err)

	return resp.PrivateIpAddresses, err
}

func (c *Client) BatchAddPrivateIpCrossSubnet(ctx context.Context, eniID, subnetID string, privateIPs []string, count int, isIpv6 bool) ([]string, error) {
	t := time.Now()

	var ips []eni.PrivateIpArgs
	arg := &eni.EniBatchAddPrivateIpCrossSubnetArgs{
		EniId:  eniID,
		IsIpv6: isIpv6,
	}
	if len(privateIPs) != 0 {
		for _, ip := range privateIPs {
			ips = append(ips, eni.PrivateIpArgs{PrivateIpAddress: ip, SubnetId: subnetID})
		}
		arg.PrivateIps = ips
	} else {
		arg.SubnetId = subnetID
		arg.PrivateIpAddressCount = count

	}

	resp, err := c.eniClient.BatchAddPrivateIpCrossSubnet(arg)

	exportMetricAndLog(ctx, "BatchAddPrivateIpCrossSubnet", t, err)

	return resp.PrivateIpAddresses, err
}

func (c *Client) BatchDeletePrivateIP(ctx context.Context, privateIPs []string, eniID string, isIpv6 bool) error {
	t := time.Now()

	err := c.eniClient.BatchDeletePrivateIp(&eni.EniBatchPrivateIpArgs{
		EniId:              eniID,
		PrivateIpAddresses: privateIPs,
		IsIpv6:             isIpv6,
	})

	exportMetricAndLog(ctx, "BatchDeletePrivateIP", t, err)

	return err
}

func (c *Client) CreateENI(ctx context.Context, args *eni.CreateEniArgs) (string, error) {
	t := time.Now()
	resp, err := c.eniClient.CreateEni(args)
	exportMetric("CreateENI", t, err)
	if err != nil {
		return "", err
	}
	return resp.EniId, nil
}

func (c *Client) DeleteENI(ctx context.Context, eniID string) error {
	t := time.Now()
	err := c.eniClient.DeleteEni(&eni.DeleteEniArgs{
		EniId: eniID,
	})
	exportMetric("DeleteENI", t, err)
	return err
}

func (c *Client) AttachENI(ctx context.Context, args *eni.EniInstance) error {
	t := time.Now()
	err := c.eniClient.AttachEniInstance(args)
	exportMetric("AttachENI", t, err)
	return err
}

func (c *Client) DetachENI(ctx context.Context, args *eni.EniInstance) error {
	t := time.Now()
	err := c.eniClient.DetachEniInstance(args)
	exportMetric("DetachENI", t, err)
	return err
}

func (c *Client) StatENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	t := time.Now()
	resp, err := c.eniClient.GetEniDetail(eniID)
	exportMetric("StatENI", t, err)
	return resp, err
}

// GetENIQuota implements Interface.
func (c *Client) GetENIQuota(ctx context.Context, instanceID string) (*eni.EniQuoteInfo, error) {
	t := time.Now()
	resp, err := c.eniClient.GetEniQuota(&eni.EniQuoteArgs{
		InstanceId: instanceID,
	})
	exportMetric("GET /v1/eni/quota", t, err)
	return resp, err
}

func (c *Client) ListRouteTable(ctx context.Context, vpcID, routeTableID string) ([]vpc.RouteRule, error) {
	t := time.Now()
	resp, err := c.vpcClient.GetRouteTableDetail(routeTableID, vpcID)
	exportMetric("ListRouteTable", t, err)
	if err != nil {
		return nil, err
	}
	return resp.RouteRules, nil
}

func (c *Client) CreateRouteRule(ctx context.Context, args *vpc.CreateRouteRuleArgs) (string, error) {
	t := time.Now()
	resp, err := c.vpcClient.CreateRouteRule(args)
	exportMetric("CreateRouteRule", t, err)
	if err != nil {
		return "", err
	}
	return resp.RouteRuleId, nil
}

func (c *Client) DeleteRouteRule(ctx context.Context, routeID string) error {
	t := time.Now()
	err := c.vpcClient.DeleteRouteRule(routeID, "")
	exportMetric("DeleteRouteRule", t, err)
	return err
}

func (c *Client) DescribeSubnet(ctx context.Context, subnetID string) (*vpc.Subnet, error) {
	t := time.Now()
	resp, err := c.vpcClient.GetSubnetDetail(subnetID)
	exportMetric("DescribeSubnet", t, err)
	if err != nil {
		return nil, err
	}
	return &resp.Subnet, nil
}

func (c *Client) ListSubnets(ctx context.Context, args *vpc.ListSubnetArgs) ([]vpc.Subnet, error) {
	t := time.Now()
	resp, err := c.vpcClient.ListSubnets(args)
	exportMetric("ListSubnets", t, err)
	if err != nil {
		return nil, err
	}
	return resp.Subnets, nil
}

func (c *Client) GetBCCInstanceDetail(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error) {
	t := time.Now()
	resp, err := c.bccClient.GetInstanceDetail(instanceID)
	exportMetric("GetBCCInstanceDetail", t, err)
	if err != nil {
		return nil, err
	}
	return &resp.Instance, nil
}

func (c *Client) ListSecurityGroup(ctx context.Context, vpcID, instanceID string) ([]bccapi.SecurityGroupModel, error) {
	var securityGroups []bccapi.SecurityGroupModel

	isTruncated := true
	nextMarker := ""

	for isTruncated {
		t := time.Now()

		args := bccapi.ListSecurityGroupArgs{
			Marker:     nextMarker,
			InstanceId: instanceID,
			VpcId:      vpcID,
		}

		res, err := c.bccClient.ListSecurityGroup(&args)
		exportMetricAndLog(ctx, "ListSecurityGroup", t, err)
		if err != nil {
			return nil, err
		}

		securityGroups = append(securityGroups, res.SecurityGroups...)

		nextMarker = res.NextMarker
		isTruncated = res.IsTruncated
	}

	return securityGroups, nil
}

func (c *Client) ListAclEntrys(ctx context.Context, vpcID string) ([]vpc.AclEntry, error) {
	t := time.Now()
	result, err := c.vpcClient.ListAclEntrys(vpcID)
	exportMetric("ListAclEntrys", t, err)
	if err != nil {
		return nil, err
	}
	return result.AclEntrys, nil
}

// ListEsg implements Interface.
func (c *Client) ListEsg(ctx context.Context, instanceID string) ([]esg.EnterpriseSecurityGroup, error) {
	var result []esg.EnterpriseSecurityGroup
	isTruncated := true
	nextMarker := ""
	for isTruncated {
		t := time.Now()
		args := esg.ListEsgArgs{
			Marker:     nextMarker,
			InstanceId: instanceID,
		}
		res, err := c.esgClient.ListEsg(&args)
		exportMetricAndLog(ctx, "ListEsg", t, err)
		if err != nil {
			return nil, err
		}
		nextMarker = res.NextMarker
		isTruncated = res.IsTruncated
		result = append(result, res.EnterpriseSecurityGroups...)
	}
	return result, nil
}

func (c *Client) GetBBCInstanceDetail(ctx context.Context, instanceID string) (*bbc.InstanceModel, error) {
	t := time.Now()
	resp, err := c.bbcClient.GetInstanceDetail(instanceID)
	exportMetric("GetBBCInstanceDetail", t, err)
	return resp, err
}

func (c *Client) GetBBCInstanceENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error) {
	t := time.Now()
	resp, err := c.bbcClient.GetInstanceEni(instanceID)
	exportMetric("GetBBCInstanceENI", t, err)
	return resp, err
}

func (c *Client) BBCBatchAddIP(ctx context.Context, args *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error) {
	t := time.Now()
	resp, err := c.bbcClient.BatchAddIP(args)
	exportMetricAndLog(ctx, "BBCBatchAddIP", t, err)
	return resp, err
}

func (c *Client) BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error {
	t := time.Now()
	err := c.bbcClient.BatchDelIP(args)
	exportMetricAndLog(ctx, "BBCBatchDelIP", t, err)
	return err
}

func (c *Client) BBCBatchAddIPCrossSubnet(ctx context.Context, args *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error) {
	t := time.Now()
	resp, err := c.bbcClient.BatchAddIPCrossSubnet(args)
	exportMetricAndLog(ctx, "BBCBatchAddIPCrossSubnet", t, err)
	return resp, err
}

func (c *Client) GetHPCEniID(ctx context.Context, instanceID string) (*hpc.EniList, error) {
	t := time.Now()
	resp, err := c.hpcClient.GetHPCEniID(instanceID)
	exportMetricAndLog(ctx, "GetHPCEniID", t, err)
	return resp, err
}

func (c *Client) BatchDeleteHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchDeleteIPArgs) error {
	t := time.Now()
	err := c.hpcClient.BatchDeletePrivateIPByHpc(args)
	exportMetricAndLog(ctx, "BatchDeleteHpcEniPrivateIP", t, err)
	return err
}

func (c *Client) BatchAddHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchPrivateIPArgs) (*hpc.BatchAddPrivateIPResult, error) {
	t := time.Now()
	resp, err := c.hpcClient.BatchAddPrivateIPByHpc(args)
	exportMetricAndLog(ctx, "BatchAddHpcEniPrivateIP", t, err)
	return resp, err
}

// BCCBatchAddIP implements Interface.
func (c *Client) BCCBatchAddIP(ctx context.Context, args *bccapi.BatchAddIpArgs) (*bccapi.BatchAddIpResponse, error) {
	t := time.Now()
	resp, err := c.bccClient.BatchAddIP(args)
	exportMetricAndLog(ctx, BCCBatchAddIP, t, err)
	return resp, err
}

// BCCBatchDelIP implements Interface.
func (c *Client) BCCBatchDelIP(ctx context.Context, args *bccapi.BatchDelIpArgs) error {
	t := time.Now()
	err := c.bccClient.BatchDelIP(args)
	exportMetricAndLog(ctx, BCCBatchDelIP, t, err)
	return err
}

// ListBCCInstanceEni implements Interface.
func (c *Client) ListBCCInstanceEni(ctx context.Context, instanceID string) ([]bccapi.Eni, error) {
	t := time.Now()
	resp, err := c.bccClient.ListInstanceEnis(instanceID)
	exportMetricAndLog(ctx, BCCListENIs, t, err)
	if err != nil {
		return nil, err
	}
	return resp.EniList, err
}

// DescribeVPC implements Interface.
func (c *Client) DescribeVPC(ctx context.Context, vpcID string) (*vpc.ShowVPCModel, error) {
	t := time.Now()
	resp, err := c.vpcClient.GetVPCDetail(vpcID)
	exportMetricAndLog(ctx, DescribeVPC, t, err)
	if err != nil {
		return nil, err
	}
	return &resp.VPC, nil
}
