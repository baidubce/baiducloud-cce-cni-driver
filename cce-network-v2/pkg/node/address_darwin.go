//go:build darwin

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

package node

import (
	"net"
)

func firstGlobalV4Addr(intf string, preferredIP net.IP, preferPublic bool) (net.IP, error) {
	return net.IP{}, nil
}

func firstGlobalV6Addr(intf string, preferredIP net.IP, preferPublic bool) (net.IP, error) {
	return net.IP{}, nil
}

// getCCEHostIPsFromNetDev returns the first IPv4 link local and returns
// it
func getCCEHostIPsFromNetDev(devName string) (ipv4GW, ipv6Router net.IP) {
	return net.IP{}, net.IP{}
}
