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

package eni

import (
	"context"
	"net"

	"github.com/vishvananda/netlink"

	cidrutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/cidr"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	NatGreLinkName = "natgre"
)

func (c *Controller) bigNatLinkExists(ctx context.Context) (netlink.Link, bool) {
	link, err := c.netlink.LinkByName(NatGreLinkName)
	if err != nil {
		return nil, false
	}
	return link, link.Type() == "gre" && link.Attrs().Flags&net.FlagUp != 0
}

func (c *Controller) addBigNatRoutes(ctx context.Context, natgreLink, eniLink netlink.Link, eniGw net.IP, table int) error {
	var (
		cidrs = []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"100.64.0.0/10",
		}
	)

	// default dev natgre
	rt, err := c.replaceRoute(cidrutil.IPv4ZeroCIDR, nil, natgreLink, table, false)
	if err != nil {
		log.Errorf(ctx, "failed to replace default route %+v in table %v: %v", *rt, table, err)
	}

	// other cidrs dev eni
	for _, cidr := range cidrs {
		_, dst, _ := net.ParseCIDR(cidr)
		rt, err = c.replaceRoute(dst, eniGw, eniLink, table, true)
		if err != nil {
			log.Errorf(ctx, "failed to replace route %+v in table %v: %v", *rt, table, err)
		}
	}

	return nil
}
