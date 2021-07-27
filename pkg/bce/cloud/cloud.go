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
	"fmt"
	"os"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/baidubce/bce-sdk-go/services/bcc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	sdklog "github.com/baidubce/bce-sdk-go/util/log"
	"k8s.io/client-go/kubernetes"
)

const (
	BCCEndpointEnv = "BCC_ENDPOINT"
	BBCEndpointEnv = "BBC_ENDPOINT"

	connectionTimeoutSInSecond = 20
)

func newBCEClientConfig(ctx context.Context,
	region,
	endpointEnv string,
	preDefinedEndpoints map[string]string,
	auth Auth,
) *bce.BceClientConfiguration {
	endpoint, exist := os.LookupEnv(endpointEnv)
	if !exist || endpoint == "" {
		fmt.Printf("Env %v not set, using bce predefined endpoint config\n", endpointEnv)
		endpoint = preDefinedEndpoints[region]
	}

	return &bce.BceClientConfiguration{
		Endpoint:                  endpoint,
		Region:                    region,
		UserAgent:                 bce.DEFAULT_USER_AGENT,
		Credentials:               auth.GetCredentials(ctx),
		SignOption:                auth.GetSignOptions(ctx),
		Retry:                     bce.DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: bce.DEFAULT_CONNECTION_TIMEOUT_IN_MILLIS,
	}
}

func New(
	region string,
	clusterID string,
	accessKeyID string,
	secretAccessKey string,
	kubeClient kubernetes.Interface,
	debug bool,
) (Interface, error) {
	ctx := context.TODO()

	if debug {
		sdklog.SetLogHandler(sdklog.STDOUT)
	}

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

	bccClientConfig := newBCEClientConfig(ctx, region, BCCEndpointEnv, BCCEndpoints, auth)
	bbcClientConfig := newBCEClientConfig(ctx, region, BBCEndpointEnv, BBCEndpoints, auth)

	vpcClient := &vpc.Client{
		BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx)),
	}
	vpcClient.Config.ConnectionTimeoutInMillis = connectionTimeoutSInSecond * 1000

	bccClient := &bcc.Client{
		BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx)),
	}
	bccClient.Config.ConnectionTimeoutInMillis = connectionTimeoutSInSecond * 1000

	eniClient := &eni.Client{
		BceClient: bce.NewBceClient(bccClientConfig, auth.GetSigner(ctx)),
	}
	eniClient.Config.ConnectionTimeoutInMillis = connectionTimeoutSInSecond * 1000

	bbcClient := &bbc.Client{
		BceClient: bce.NewBceClient(bbcClientConfig, auth.GetSigner(ctx)),
	}
	bbcClient.Config.ConnectionTimeoutInMillis = connectionTimeoutSInSecond * 1000

	c := &Client{
		vpcClient: vpcClient,
		bccClient: bccClient,
		eniClient: eniClient,
		bbcClient: bbcClient,
	}
	return c, nil
}

func (c *Client) ListENIs(ctx context.Context, vpcID string) ([]eni.Eni, error) {
	var enis []eni.Eni

	isTruncated := true
	nextMarker := ""

	for isTruncated {
		listArgs := &eni.ListEniArgs{
			VpcId:  vpcID,
			Marker: nextMarker,
		}

		res, err := c.eniClient.ListEni(listArgs)
		if err != nil {
			return nil, err
		}

		enis = append(enis, res.Eni...)

		nextMarker = res.NextMarker
		isTruncated = res.IsTruncated
	}

	return enis, nil
}

func (c *Client) AddPrivateIP(ctx context.Context, privateIP string, eniID string) (string, error) {
	resp, err := c.eniClient.AddPrivateIp(&eni.EniPrivateIpArgs{
		EniId:            eniID,
		PrivateIpAddress: privateIP,
	})

	if err != nil {
		return "", err
	}

	return resp.PrivateIpAddress, nil
}

func (c *Client) DeletePrivateIP(ctx context.Context, privateIP string, eniID string) error {
	return c.eniClient.DeletePrivateIp(&eni.EniPrivateIpArgs{
		EniId:            eniID,
		PrivateIpAddress: privateIP,
	})
}

func (c *Client) CreateENI(ctx context.Context, args *eni.CreateEniArgs) (string, error) {
	resp, err := c.eniClient.CreateEni(args)
	if err != nil {
		return "", err
	}
	return resp.EniId, nil
}

func (c *Client) DeleteENI(ctx context.Context, eniID string) error {
	return c.eniClient.DeleteEni(&eni.DeleteEniArgs{
		EniId: eniID,
	})
}

func (c *Client) AttachENI(ctx context.Context, args *eni.EniInstance) error {
	return c.eniClient.AttachEniInstance(args)
}

func (c *Client) DetachENI(ctx context.Context, args *eni.EniInstance) error {
	return c.eniClient.DetachEniInstance(args)
}

func (c *Client) StatENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	return c.eniClient.GetEniDetail(eniID)
}

func (c *Client) ListRouteTable(ctx context.Context, vpcID, routeTableID string) ([]vpc.RouteRule, error) {
	resp, err := c.vpcClient.GetRouteTableDetail(routeTableID, vpcID)
	if err != nil {
		return nil, err
	}
	return resp.RouteRules, nil
}

func (c *Client) CreateRouteRule(ctx context.Context, args *vpc.CreateRouteRuleArgs) (string, error) {
	resp, err := c.vpcClient.CreateRouteRule(args)
	if err != nil {
		return "", err
	}
	return resp.RouteRuleId, nil
}

func (c *Client) DeleteRoute(ctx context.Context, routeID string) error {
	return c.vpcClient.DeleteRouteRule(routeID, "")
}

func (c *Client) DescribeSubnet(ctx context.Context, subnetID string) (*vpc.Subnet, error) {
	resp, err := c.vpcClient.GetSubnetDetail(subnetID)
	if err != nil {
		return nil, err
	}
	return &resp.Subnet, nil
}

func (c *Client) ListSubnets(ctx context.Context, args *vpc.ListSubnetArgs) ([]vpc.Subnet, error) {
	resp, err := c.vpcClient.ListSubnets(args)
	if err != nil {
		return nil, err
	}
	return resp.Subnets, nil
}

func (c *Client) DescribeInstance(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error) {
	resp, err := c.bccClient.GetInstanceDetail(instanceID)
	if err != nil {
		return nil, err
	}
	return &resp.Instance, nil
}

func (c *Client) BBCGetInstanceDetail(ctx context.Context, instanceID string) (*bbc.InstanceModel, error) {
	return c.bbcClient.GetInstanceDetail(instanceID)
}

func (c *Client) BBCGetInstanceENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error) {
	return c.bbcClient.GetInstanceEni(instanceID)
}

func (c *Client) BBCBatchAddIP(ctx context.Context, args *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error) {
	return c.bbcClient.BatchAddIP(args)
}

func (c *Client) BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error {
	return c.bbcClient.BatchDelIP(args)
}

func (c *Client) BBCBatchAddIPCrossSubnet(ctx context.Context, args *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error) {
	return c.bbcClient.BatchAddIPCrossSubnet(args)
}
