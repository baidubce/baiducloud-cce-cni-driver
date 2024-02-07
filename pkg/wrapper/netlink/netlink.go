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

package netlink

import (
	"github.com/vishvananda/netlink"
)

// Interface wraps methods used from the vishvananda/netlink package
type Interface interface {
	// LinkByName gets a link object given the device name
	LinkByName(name string) (netlink.Link, error)
	// LinkByName gets a link object given the device name
	LinkSetName(link netlink.Link, name string) error
	// LinkSetNsFd is equivalent to `ip link set $link netns $ns`
	LinkSetNsFd(link netlink.Link, fd int) error
	// ParseAddr parses an address string
	ParseAddr(s string) (*netlink.Addr, error)
	// AddrAdd is equivalent to `ip addr add $addr dev $link`
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	// AddrDel is equivalent to `ip addr del $addr dev $link`
	AddrDel(link netlink.Link, addr *netlink.Addr) error
	// AddrList is equivalent to `ip addr show `
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	// LinkAdd is equivalent to `ip link add`
	LinkAdd(link netlink.Link) error
	// LinkSetUp is equivalent to `ip link set $link up`
	LinkSetUp(link netlink.Link) error
	// LinkList is equivalent to: `ip link show`
	LinkList() ([]netlink.Link, error)
	// LinkSetDown is equivalent to: `ip link set $link down`
	LinkSetDown(link netlink.Link) error
	// LinkByIndex finds a link by index and returns a pointer to the object.
	LinkByIndex(index int) (netlink.Link, error)
	// RouteList gets a list of routes in the system.
	RouteList(link netlink.Link, family int) ([]netlink.Route, error)
	// RouteAdd will add a route to the route table
	RouteAdd(route *netlink.Route) error
	// RouteReplace will replace the route in the route table
	RouteReplace(route *netlink.Route) error
	// RouteDel is equivalent to `ip route del`
	RouteDel(route *netlink.Route) error
	// RouteListFiltered gets a list of routes in the system filtered with specified rules.
	RouteListFiltered(family int, filter *netlink.Route, filterMask uint64) ([]netlink.Route, error)
	// NeighAdd equivalent to: `ip neigh add ....`
	NeighAdd(neigh *netlink.Neigh) error
	// LinkDel equivalent to: `ip link del $link`
	LinkDel(link netlink.Link) error
	// NewRule creates a new empty rule
	NewRule() *netlink.Rule
	// RuleAdd is equivalent to: ip rule add
	RuleAdd(rule *netlink.Rule) error
	// RuleDel is equivalent to: ip rule del
	RuleDel(rule *netlink.Rule) error
	// RuleList is equivalent to: ip rule list
	RuleList(family int) ([]netlink.Rule, error)
	// LinkSetMTU is equivalent to `ip link set dev $link mtu $mtu`
	LinkSetMTU(link netlink.Link, mtu int) error

	NeighDel(neigh *netlink.Neigh) error
	NeighList(linkIndex, family int) ([]netlink.Neigh, error)
}

// client is the implementation of Interface
type client struct {
}

// New return &client
func New() Interface {
	return &client{}
}

func (*client) LinkAdd(link netlink.Link) error {
	return netlink.LinkAdd(link)
}

func (*client) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (*client) LinkSetName(link netlink.Link, name string) error {
	return netlink.LinkSetName(link, name)
}

func (*client) LinkSetNsFd(link netlink.Link, fd int) error {
	return netlink.LinkSetNsFd(link, fd)
}

func (*client) ParseAddr(s string) (*netlink.Addr, error) {
	return netlink.ParseAddr(s)
}

func (*client) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

func (*client) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}

func (*client) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

func (*client) LinkList() ([]netlink.Link, error) {
	return netlink.LinkList()
}

func (*client) LinkSetDown(link netlink.Link) error {
	return netlink.LinkSetDown(link)
}

func (*client) LinkByIndex(index int) (netlink.Link, error) {
	return netlink.LinkByIndex(index)
}

func (*client) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return netlink.RouteList(link, family)
}

func (*client) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

func (*client) RouteReplace(route *netlink.Route) error {
	return netlink.RouteReplace(route)
}

func (*client) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

func (*client) RouteListFiltered(family int, filter *netlink.Route, filterMask uint64) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(family, filter, filterMask)
}

func (*client) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (*client) NeighAdd(neigh *netlink.Neigh) error {
	return netlink.NeighAdd(neigh)
}

func (*client) LinkDel(link netlink.Link) error {
	return netlink.LinkDel(link)
}

func (*client) NewRule() *netlink.Rule {
	return netlink.NewRule()
}

func (*client) RuleAdd(rule *netlink.Rule) error {
	return netlink.RuleAdd(rule)
}

func (*client) RuleDel(rule *netlink.Rule) error {
	return netlink.RuleDel(rule)
}

func (*client) RuleList(family int) ([]netlink.Rule, error) {
	return netlink.RuleList(family)
}

func (*client) LinkSetMTU(link netlink.Link, mtu int) error {
	return netlink.LinkSetMTU(link, mtu)
}

func (*client) NeighDel(neigh *netlink.Neigh) error {
	return netlink.NeighDel(neigh)
}
func (*client) NeighList(linkIndex, family int) ([]netlink.Neigh, error) {
	return netlink.NeighList(linkIndex, family)
}
