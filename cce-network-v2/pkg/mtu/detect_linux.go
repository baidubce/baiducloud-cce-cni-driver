//go:build linux

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

package mtu

import (
	"fmt"
	"net"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

const (
	externalProbeIPv4 = "1.1.1.1"
	externalProbeIPv6 = "2606:4700:4700::1111"
)

func getRoute(externalProbe string) ([]netlink.Route, error) {
	ip := net.ParseIP(externalProbe)
	if ip == nil {
		return nil, fmt.Errorf("unable to parse IP %s", externalProbe)
	}

	routes, err := netlink.RouteGet(ip)
	if err != nil {
		return nil, fmt.Errorf("unable to lookup route to %s: %w", externalProbe, err)
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no route to %s", externalProbe)
	}

	return routes, nil
}

func autoDetect() (int, error) {
	var routes []netlink.Route
	var err error

	routes, err = getRoute(externalProbeIPv4)
	if err != nil {
		prevErr := err
		routes, err = getRoute(externalProbeIPv6)
		if err != nil {
			return 0, fmt.Errorf("%v, %v", err.Error(), prevErr.Error())
		}
	}

	if routes[0].Gw == nil {
		return 0, fmt.Errorf("unable to find default gateway from the routes: %s", routes)
	}

	link, err := netlink.LinkByIndex(routes[0].LinkIndex)
	if err != nil {
		return 0, fmt.Errorf("unable to find interface of default route: %w", err)
	}

	if mtu := link.Attrs().MTU; mtu != 0 {
		log.Infof("Detected MTU %d", mtu)
		return mtu, nil
	}

	return EthernetMTU, nil
}

// getMTUFromIf finds the interface that holds the ip and returns its mtu
func getMTUFromIf(ip net.IP) (int, error) {
	ifaces, err := netlink.LinkList()
	if err != nil {
		return 0, fmt.Errorf("unable to list interfaces: %w", err)
	}

	for _, iface := range ifaces {
		addrs, err := netlink.AddrList(iface, netlink.FAMILY_ALL)
		if err != nil {
			log.WithFields(logrus.Fields{
				logfields.Device: iface.Attrs().Name,
			}).Warning("Unable to list all addresses")
			continue
		}

		for _, addr := range addrs {
			if addr.IPNet.IP.Equal(ip) == true {
				myMTU := iface.Attrs().MTU
				log.WithFields(logrus.Fields{
					logfields.Device: iface.Attrs().Name,
					logfields.IPAddr: ip,
					logfields.MTU:    myMTU,
				}).Info("Inheriting MTU from external network interface")
				return myMTU, nil
			}
		}
	}
	return 0, fmt.Errorf("No interface contains the provided ip: %v", ip)
}
