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
package main

import (
	"fmt"
	"net"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/vishvananda/netlink"
)

// containerSet sets up the container interface
// this method is called at container network namespace
func containerSet(hostVeth, contVeth *net.Interface, pr *current.Result) error {
	var err error
	for _, ipc := range pr.IPs {
		// Add a permanent ARP entry for the gateway
		err = netlink.NeighAdd(&netlink.Neigh{
			LinkIndex: contVeth.Index,
			State:     netlink.NUD_PERMANENT,
			IP:        ipc.Gateway,
			HardwareAddr: func() net.HardwareAddr {
				return hostVeth.HardwareAddr
			}(),
		})
		if err != nil {
			logger.WithError(err).Errorf("failed to add permanent ARP entry for the gateway %q", ipc.Gateway)
			return fmt.Errorf("failed to add permanent ARP entry for the gateway %q: %v", ipc.Gateway, err)
		}
	}

	return err
}

// AddLinkRoute adds a link-scoped route to a device.
func AddLinkRoute(ipn *net.IPNet, gw net.IP, dev netlink.Link) error {
	return netlink.RouteAdd(&netlink.Route{
		LinkIndex: dev.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       ipn,
		Gw:        gw,
	})
}
