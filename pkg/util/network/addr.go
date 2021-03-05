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

package network

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"

	"github.com/containernetworking/plugins/pkg/ns"
)

func (c *linuxNetwork) GetIPFromPodNetNS(podNSPath string, ifName string, ipFamily int) (net.IP, error) {
	var ip net.IP

	netns, err := c.ns.GetNS(podNSPath)
	if err != nil {
		return nil, err
	}
	defer netns.Close()

	err = netns.Do(func(hostNS ns.NetNS) error {
		intf, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}
		addrs, err := netlink.AddrList(intf, ipFamily)
		if err != nil {
			return err
		}
		if len(addrs) == 0 {
			return fmt.Errorf("no ip found on %v", ifName)
		}
		ip = addrs[0].IP
		return nil
	})
	if err != nil {
		return nil, err
	}

	return ip, nil
}
