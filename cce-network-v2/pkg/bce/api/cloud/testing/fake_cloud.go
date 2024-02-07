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

package testing

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/hpc"

	k8snet "k8s.io/utils/net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/eip"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"
)

// Ensure, that FakeBceCloud does implement Interface.
var _ cloud.Interface = &FakeBceCloud{}

type apiConfig struct {
	ipRange         string
	ipRangeStartIdx int

	addIPLatency time.Duration
	delIPLatency time.Duration

	lock sync.Mutex
}

type FakeBceCloud struct {
	bbcConfig *apiConfig
}

// BCCBatchAddIP implements cloud.Interface.
func (*FakeBceCloud) BCCBatchAddIP(ctx context.Context, args *bccapi.BatchAddIpArgs) (*bccapi.BatchAddIpResponse, error) {
	panic("unimplemented")
}

// BCCBatchDelIP implements cloud.Interface.
func (*FakeBceCloud) BCCBatchDelIP(ctx context.Context, args *bccapi.BatchDelIpArgs) error {
	panic("unimplemented")
}

// ListBCCInstanceEni implements cloud.Interface.
func (*FakeBceCloud) ListBCCInstanceEni(ctx context.Context, instanceID string) ([]bccapi.Eni, error) {
	panic("unimplemented")
}

// GetENIQuota implements cloud.Interface.
func (*FakeBceCloud) GetENIQuota(ctx context.Context, instanceID string) (*eni.EniQuoteInfo, error) {
	panic("unimplemented")
}

// BindENIPublicIP implements cloud.Interface
func (*FakeBceCloud) BindENIPublicIP(ctx context.Context, privateIP string, publicIP string, eniID string) error {
	panic("unimplemented")
}

// DirectEIP implements cloud.Interface
func (*FakeBceCloud) DirectEIP(ctx context.Context, eip string) error {
	panic("unimplemented")
}

// ListEIPs implements cloud.Interface
func (*FakeBceCloud) ListEIPs(ctx context.Context, args eip.ListEipArgs) ([]eip.EipModel, error) {
	panic("unimplemented")
}

// UnBindENIPublicIP implements cloud.Interface
func (*FakeBceCloud) UnBindENIPublicIP(ctx context.Context, publicIP string, eniID string) error {
	panic("unimplemented")
}

// UnDirectEIP implements cloud.Interface
func (*FakeBceCloud) UnDirectEIP(ctx context.Context, eip string) error {
	panic("unimplemented")
}

func (fake *FakeBceCloud) GetHPCEniID(ctx context.Context, instanceID string) (*hpc.EniList, error) {
	return &hpc.EniList{}, nil
}

func (fake *FakeBceCloud) BatchDeleteHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchDeleteIPArgs) error {
	return nil
}

func (fake *FakeBceCloud) BatchAddHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchPrivateIPArgs) (*hpc.BatchAddPrivateIPResult, error) {
	return &hpc.BatchAddPrivateIPResult{}, nil
}

func NewFakeBceCloud() cloud.Interface {
	bbcConfig := &apiConfig{
		ipRange:         "10.255.0.0/16",
		ipRangeStartIdx: 10,
		addIPLatency:    0,
		delIPLatency:    0,
	}

	if ipRange, ok := os.LookupEnv("FAKE_CLOUD_BBC_IP_RANGE"); ok {
		bbcConfig.ipRange = ipRange
	}

	if latency, ok := os.LookupEnv("FAKE_CLOUD_BBC_ADD_IP_LATENCY"); ok {
		lat, err := strconv.Atoi(latency)
		if err == nil {
			bbcConfig.addIPLatency = time.Millisecond * time.Duration(lat)
		}
	}

	if latency, ok := os.LookupEnv("FAKE_CLOUD_BBC_DEL_IP_LATENCY"); ok {
		lat, err := strconv.Atoi(latency)
		if err == nil {
			bbcConfig.delIPLatency = time.Millisecond * time.Duration(lat)
		}
	}

	return &FakeBceCloud{
		bbcConfig: bbcConfig,
	}
}

func (fake *FakeBceCloud) AddPrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) (string, error) {
	return "", nil
}

func (fake *FakeBceCloud) AttachENI(ctx context.Context, args *eni.EniInstance) error {
	return nil
}

func (fake *FakeBceCloud) BBCBatchAddIP(ctx context.Context, args *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error) {
	return &bbc.BatchAddIpResponse{}, nil
}

func (fake *FakeBceCloud) BBCBatchAddIPCrossSubnet(ctx context.Context, args *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error) {
	var (
		privateIPs  = make([]string, 0)
		totalAddNum = 0
	)

	if fake.bbcConfig.addIPLatency != 0 {
		time.Sleep(fake.bbcConfig.addIPLatency)
	}

	for _, x := range args.SingleEniAndSubentIps {
		totalAddNum += x.SecondaryPrivateIpAddressCount
	}

	_, ipnet, err := net.ParseCIDR(fake.bbcConfig.ipRange)
	if err != nil {
		return nil, err
	}

	fake.bbcConfig.lock.Lock()
	defer fake.bbcConfig.lock.Unlock()

	for i := 0; i < totalAddNum; i++ {
		idx := fake.bbcConfig.ipRangeStartIdx
		fake.bbcConfig.ipRangeStartIdx++
		ip, err := k8snet.GetIndexedIP(ipnet, idx)
		if err != nil {
			return nil, err
		}
		privateIPs = append(privateIPs, ip.String())
	}

	return &bbc.BatchAddIpResponse{
		PrivateIps: privateIPs,
	}, nil
}

func (fake *FakeBceCloud) BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error {

	if fake.bbcConfig.delIPLatency != 0 {
		time.Sleep(fake.bbcConfig.delIPLatency)
	}
	return nil
}

func (fake *FakeBceCloud) GetBBCInstanceDetail(ctx context.Context, instanceID string) (*bbc.InstanceModel, error) {
	return &bbc.InstanceModel{}, nil
}

func (fake *FakeBceCloud) GetBBCInstanceENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error) {
	return &bbc.GetInstanceEniResult{
		Id: "eni-" + instanceID,
	}, nil
}

func (fake *FakeBceCloud) CreateENI(ctx context.Context, args *eni.CreateEniArgs) (string, error) {
	return "", nil
}

func (fake *FakeBceCloud) CreateRouteRule(ctx context.Context, args *vpc.CreateRouteRuleArgs) (string, error) {
	return "", nil
}

func (fake *FakeBceCloud) DeleteENI(ctx context.Context, eniID string) error {
	return nil
}

func (fake *FakeBceCloud) DeletePrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) error {
	return nil
}

func (fake *FakeBceCloud) DeleteRouteRule(ctx context.Context, routeID string) error {
	return nil
}

func (fake *FakeBceCloud) GetBCCInstanceDetail(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error) {
	return &bccapi.InstanceModel{}, nil
}

func (fake *FakeBceCloud) DescribeSubnet(ctx context.Context, subnetID string) (*vpc.Subnet, error) {
	return &vpc.Subnet{}, nil
}

func (fake *FakeBceCloud) DetachENI(ctx context.Context, args *eni.EniInstance) error {
	return nil
}

func (fake *FakeBceCloud) ListENIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	return nil, nil
}

func (fake *FakeBceCloud) ListERIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	return nil, nil
}

func (fake *FakeBceCloud) ListRouteTable(ctx context.Context, vpcID string, routeTableID string) ([]vpc.RouteRule, error) {
	return nil, nil
}

func (fake *FakeBceCloud) ListSubnets(ctx context.Context, args *vpc.ListSubnetArgs) ([]vpc.Subnet, error) {
	return nil, nil
}

func (fake *FakeBceCloud) ListSecurityGroup(ctx context.Context, vpcID, instanceID string) ([]bccapi.SecurityGroupModel, error) {
	return nil, nil
}

func (fake *FakeBceCloud) StatENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	return &eni.Eni{}, nil
}

func (fake *FakeBceCloud) BatchAddPrivateIP(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool) ([]string, error) {
	return nil, nil

}

func (fake *FakeBceCloud) BatchDeletePrivateIP(ctx context.Context, privateIPs []string, eniID string, isIpv6 bool) error {
	return nil
}

func (fake *FakeBceCloud) BatchAddPrivateIpCrossSubnet(ctx context.Context, eniID, subnetID string, privateIPs []string, count int, isIpv6 bool) ([]string, error) {
	return nil, nil
}
