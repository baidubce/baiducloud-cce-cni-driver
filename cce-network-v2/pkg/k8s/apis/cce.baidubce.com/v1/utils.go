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
package v1

import (
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
)

// IsInSubnet checks if the privateIPStr is in the subnet
func IsInSubnet(sbn *Subnet, privateIPStr string) bool {
	ip := net.ParseIP(privateIPStr)
	if ip == nil {
		return false
	}
	var cidrs string
	if ip.To4() != nil {
		cidrs = sbn.Spec.CIDR
	} else {
		cidrs = sbn.Spec.IPv6CIDR
	}
	return cidr.CIDRStringContainsIP(cidrs, privateIPStr)
}
