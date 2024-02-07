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

package agent

import (
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	NatGreLinkName = "natgre"
)

func bigNatLinkExists() (netlink.Link, bool) {
	link, err := netlink.LinkByName(NatGreLinkName)
	if err != nil {
		return nil, false
	}
	return link, link.Type() == "gre" && link.Attrs().Flags&net.FlagUp != 0
}

func ensureBigNatRoutes(log *logrus.Entry, natgreLink, eniLink netlink.Link, eniGw net.IP, table int) error {
	var (
		cidrs = []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"100.64.0.0/10",
		}
	)
	if eniGw.To4() == nil {
		return nil
	}

	// default dev natgre
	defaultRoute := netlink.Route{
		LinkIndex: natgreLink.Attrs().Index,
		Dst:       ip.IPv4ZeroCIDR,
		Table:     table,
		Scope:     netlink.SCOPE_LINK,
	}
	defaultRoute.SetFlag(netlink.FLAG_ONLINK)

	err := link.ReplaceRoute([]netlink.Route{defaultRoute})
	if err != nil {
		log.WithError(err).Errorf("failed to replace default route in table %v", table)
	}

	// other cidrs dev eni
	for _, cidr := range cidrs {
		_, dst, _ := net.ParseCIDR(cidr)
		err = replaceDefaultRoute(dst, eniGw, natgreLink, table)
		if err != nil {
			log.WithError(err).Errorf("failed to replace route in table %v", table)
		}
	}

	return nil
}
