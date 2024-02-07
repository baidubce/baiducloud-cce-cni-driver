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

package ip

import (
	"net"

	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

// Interface is the wrapper interface for the containernetworking ip
type Interface interface {
	// link
	RandomVethName() (string, error)
	RenameLink(curName, newName string) error
	SetupVethWithName(contVethName, hostVethName string, mtu int, hostNS ns.NetNS) (net.Interface, net.Interface, error)
	SetupVeth(contVethName string, mtu int, hostNS ns.NetNS) (net.Interface, net.Interface, error)
	DelLinkByName(ifName string) error
	DelLinkByNameAddr(ifName string) ([]*net.IPNet, error)
	GetVethPeerIfindex(ifName string) (netlink.Link, int, error)

	// forward
	EnableIP4Forward() error
	EnableIP6Forward() error
	EnableForward(ips []*current.IPConfig) error

	// masq
	SetupIPMasq(ipn *net.IPNet, chain string, comment string) error
	TeardownIPMasq(ipn *net.IPNet, chain string, comment string) error

	// route
	AddRoute(ipn *net.IPNet, gw net.IP, dev netlink.Link) error
	AddHostRoute(ipn *net.IPNet, gw net.IP, dev netlink.Link) error
	AddDefaultRoute(gw net.IP, dev netlink.Link) error
}

type ipType struct {
}

// New returns a new IP
func New() Interface {
	return &ipType{}
}

func (*ipType) RandomVethName() (string, error) {
	return ip.RandomVethName()
}

func (*ipType) RenameLink(curName, newName string) error {
	return ip.RenameLink(curName, newName)
}

func (*ipType) SetupVethWithName(contVethName, hostVethName string, mtu int, hostNS ns.NetNS) (net.Interface, net.Interface, error) {
	return ip.SetupVethWithName(contVethName, hostVethName, mtu, hostNS)
}

func (*ipType) SetupVeth(contVethName string, mtu int, hostNS ns.NetNS) (net.Interface, net.Interface, error) {
	return ip.SetupVeth(contVethName, mtu, hostNS)
}

func (*ipType) DelLinkByName(ifName string) error {
	return ip.DelLinkByName(ifName)
}

func (*ipType) DelLinkByNameAddr(ifName string) ([]*net.IPNet, error) {
	return ip.DelLinkByNameAddr(ifName)
}

func (*ipType) GetVethPeerIfindex(ifName string) (netlink.Link, int, error) {
	return ip.GetVethPeerIfindex(ifName)
}

func (*ipType) EnableIP4Forward() error {
	return ip.EnableIP4Forward()
}

func (*ipType) EnableIP6Forward() error {
	return ip.EnableIP6Forward()
}

func (*ipType) EnableForward(ips []*current.IPConfig) error {
	return ip.EnableForward(ips)
}

func (*ipType) SetupIPMasq(ipn *net.IPNet, chain string, comment string) error {
	return ip.SetupIPMasq(ipn, chain, comment)
}

func (*ipType) TeardownIPMasq(ipn *net.IPNet, chain string, comment string) error {
	return ip.TeardownIPMasq(ipn, chain, comment)
}

func (*ipType) AddRoute(ipn *net.IPNet, gw net.IP, dev netlink.Link) error {
	return ip.AddRoute(ipn, gw, dev)
}

func (*ipType) AddHostRoute(ipn *net.IPNet, gw net.IP, dev netlink.Link) error {
	return ip.AddHostRoute(ipn, gw, dev)
}

func (*ipType) AddDefaultRoute(gw net.IP, dev netlink.Link) error {
	return ip.AddDefaultRoute(gw, dev)
}
