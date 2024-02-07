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
package utils

import (
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// ConvertPrivateIP2AddressPair converts private ip to address pair
func ConvertPrivateIP2AddressPair(privateIP *models.PrivateIP, subnet *ccev1.Subnet) (*ccev2.AddressPair, error) {
	addrResult := &ccev2.AddressPair{
		IP:     privateIP.PrivateIPAddress,
		Subnet: subnet.Name,
		VPCID:  subnet.Spec.VPCID,
	}
	family := ccev2.IPv4Family
	tmpip := net.ParseIP(privateIP.PrivateIPAddress)

	cidrstr := subnet.Spec.CIDR
	if tmpip != nil && tmpip.To4() == nil {
		family = ccev2.IPv6Family
		cidrstr = subnet.Spec.IPv6CIDR
	}
	addrResult.Family = family
	addrResult.CIDRs = []string{cidrstr}

	_, cidrNet, err := net.ParseCIDR(cidrstr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CIDR (%s) of subnet by subnet ID: %s", cidrstr, addrResult.Subnet)
	}

	// gateway address usually the first IP is IPv4 address
	gw := ip.GetIPAtIndex(*cidrNet, int64(1))
	if gw == nil {
		return nil, fmt.Errorf("failed to generate gateway by CIDR (%s) of subnet (%s)", cidrstr, addrResult.Subnet)
	}
	addrResult.Gateway = gw.String()
	return addrResult, nil
}
