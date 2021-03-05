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
	"github.com/j-keck/arping"
	"net"

	"github.com/vishvananda/netlink"
)

func (c *linuxNetwork) GetLinkByMacAddress(macAddress string) (netlink.Link, error) {
	interfaces, err := c.nlink.LinkList()
	if err != nil {
		return nil, err
	}

	var link netlink.Link

	for _, intf := range interfaces {
		if intf.Attrs().HardwareAddr.String() == macAddress {
			link = intf
			break
		}
	}

	if link == nil {
		return nil, fmt.Errorf("link with MAC %s not found", macAddress)
	}

	return link, nil
}

func (c *linuxNetwork) DetectInterfaceMTU(device string) (int, error) {
	intf, err := c.nlink.LinkByName(device)
	if err != nil {
		return 0, err
	}

	return intf.Attrs().MTU, nil
}

func (c *linuxNetwork) DetectDefaultRouteInterfaceName() (string, error) {
	routeToDstIP, err := c.nlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return "", err
	}

	for _, v := range routeToDstIP {
		if v.Dst == nil {
			l, err := c.nlink.LinkByIndex(v.LinkIndex)
			if err != nil {
				return "", err
			}
			return l.Attrs().Name, nil
		}
	}

	return "", fmt.Errorf("no default route interface found")
}

func (c *linuxNetwork) InterfaceByName(name string) (*net.Interface, error) {
	return net.InterfaceByName(name)
}

func (c *linuxNetwork) GratuitousArpOverIface(srcIP net.IP, iface net.Interface) error {
	return arping.GratuitousArpOverIface(srcIP, iface)
}
