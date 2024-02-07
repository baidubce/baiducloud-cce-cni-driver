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

package ip

import (
	"net"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

var (
	_, IPv4ZeroCIDR, _ = net.ParseCIDR("0.0.0.0/0")
	_, IPv6ZeroCIDR, _ = net.ParseCIDR("::/0")

	IPv4Mask32  = net.CIDRMask(32, 32)
	IPv6Mask128 = net.CIDRMask(128, 128)
)

// ParseCIDRs fetches all CIDRs referred to by the specified slice and returns
// them as regular golang CIDR objects.
func ParseCIDRs(cidrs []string) (valid []*net.IPNet, invalid []string) {
	valid = make([]*net.IPNet, 0, len(cidrs))
	invalid = make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, prefix, err := net.ParseCIDR(cidr)
		if err != nil {
			// Likely the CIDR is specified in host format.
			ip := net.ParseIP(cidr)
			if ip == nil {
				invalid = append(invalid, cidr)
				continue
			} else {
				prefix = IPToPrefix(ip)
			}
		}
		if prefix != nil {
			valid = append(valid, prefix)
		}
	}
	return valid, invalid
}

func ConvertIPPairToIPNet(addrPair *ccev2.AddressPair) *net.IPNet {
	ipnet := &net.IPNet{
		IP:   net.ParseIP(addrPair.IP),
		Mask: IPv4Mask32,
	}
	if ipnet.IP.To4() == nil {
		ipnet.Mask = IPv6Mask128
	}
	return ipnet
}
