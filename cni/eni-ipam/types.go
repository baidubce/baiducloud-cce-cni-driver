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

	"github.com/containernetworking/cni/pkg/types/current"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
)

var (
	eniIPAM = NewENIIPAM()
)

type NetConf struct {
	CNIVersion   string          `json:"cniVersion,omitempty"`
	Name         string          `json:"name,omitempty"`
	Type         string          `json:"type,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
	IPAM         *IPAMConf       `json:"ipam,omitempty"`
}

type IPAMConf struct {
	Type                    string `json:"type,omitempty"`
	Endpoint                string `json:"endpoint"`
	InstanceType            string `json:"instanceType"`
	DeleteENIScopeLinkRoute bool   `json:"deleteENIScopeLinkRoute"`
	RouteTableIDOffset      int    `json:"routeTableIDOffset"` // default 127, eth1 use table 128, eth2 use table 129 etc.
	ENILinkPrefix           string `json:"eniLinkPrefix"`      // default "eth"
}

type networkClient interface {
	SetupNetwork(ctx context.Context, result *current.Result, ipamConf *IPAMConf, resp *rpc.AllocateIPReply) error
	TeardownNetwork(ctx context.Context, ipamConf *IPAMConf, resp *rpc.ReleaseIPReply) error
}
