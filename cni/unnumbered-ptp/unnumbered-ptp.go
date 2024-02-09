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

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/vishvananda/netlink"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	typeswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes"
	ipwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip"
	ipamwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
)

const (
	rpFilterSysctlTemplate = "net.ipv4.conf.%s.rp_filter"
	proxyARPSysctlTemplate = "net.ipv4.conf.%s.proxy_arp"

	defaultContainerInterfaceName = "eth1"
)

type ptpPlugin struct {
	nlink   netlinkwrapper.Interface
	ns      nswrapper.Interface
	ipam    ipamwrapper.Interface
	ip      ipwrapper.Interface
	types   typeswrapper.Interface
	sysctl  sysctlwrapper.Interface
	netutil networkutil.Interface
}

func newPlugin() *ptpPlugin {
	return &ptpPlugin{
		nlink:   netlinkwrapper.New(),
		ns:      nswrapper.New(),
		ipam:    ipamwrapper.New(),
		ip:      ipwrapper.New(),
		types:   typeswrapper.New(),
		sysctl:  sysctlwrapper.New(),
		netutil: networkutil.New(),
	}
}

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

type PluginConf struct {
	types.NetConf
	RawPrevResult      *map[string]interface{} `json:"prevResult"`
	PrevResult         *current.Result         `json:"-"`
	HostInterface      string                  `json:"hostInterface"`
	ContainerInterface string                  `json:"containerInterface"`
	MTU                int                     `json:"mtu"`
	ServiceCIDR        string                  `json:"serviceCIDR"`
	LocalDNSAddr       string                  `json:"localDNSAddr"`
}

func loadConf(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// Parse previous result
	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %v", err)
		}
		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}

	if conf.HostInterface == "" {
		return nil, fmt.Errorf("hostInterface must be specified")
	}

	if conf.ContainerInterface == "" {
		conf.ContainerInterface = defaultContainerInterfaceName
	}

	return &conf, nil
}

func (p *ptpPlugin) enableForwarding(ipv4 bool, ipv6 bool) error {
	if ipv4 {
		err := p.ip.EnableIP4Forward()
		if err != nil {
			return fmt.Errorf("Could not enable IPv4 forwarding: %v", err)
		}
	}
	if ipv6 {
		err := p.ip.EnableIP6Forward()
		if err != nil {
			return fmt.Errorf("Could not enable IPv6 forwarding: %v", err)
		}
	}
	return nil
}

func (p *ptpPlugin) setupContainerNetNSVeth(
	hostInterface *current.Interface,
	hostNS ns.NetNS,
	ifName string,
	mtu int,
	hostAddrs []netlink.Addr,
	pr *current.Result,
	serviceCIDR string,
	localDNSAddr string) error {
	hostVeth, _, err := p.ip.SetupVeth(ifName, mtu, hostNS)
	if err != nil {
		return err
	}
	hostInterface.Name = hostVeth.Name
	hostInterface.Mac = hostVeth.HardwareAddr.String()

	contVeth, err := p.netutil.InterfaceByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to look up %q: %v", ifName, err)
	}

	contVethLink, err := p.nlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to look up %q: %v", ifName, err)
	}

	for _, ipc := range pr.IPs {
		var err error
		if ipc.Version == "4" {
			err = p.nlink.AddrAdd(contVethLink, &netlink.Addr{
				IPNet: &net.IPNet{
					IP:   ipc.Address.IP,
					Mask: net.CIDRMask(32, 32),
				},
			})
		} else if ipc.Version == "6" {
			err = p.nlink.AddrAdd(contVethLink, &netlink.Addr{
				IPNet: &net.IPNet{
					IP:   ipc.Address.IP,
					Mask: net.CIDRMask(128, 128),
				},
			})
		}
		if err != nil {
			return fmt.Errorf("failed to add container primary ip %v to %s: %v", ipc.Address.String(), ifName, err)
		}
	}

	// add route to host through veth
	for _, ipc := range hostAddrs {
		addrBits := 128
		if ipc.IP.To4() != nil {
			addrBits = 32
		}

		err := p.nlink.RouteAdd(&netlink.Route{
			LinkIndex: contVeth.Index,
			Scope:     netlink.SCOPE_LINK,
			Dst: &net.IPNet{
				IP:   ipc.IP,
				Mask: net.CIDRMask(addrBits, addrBits),
			},
		})

		if err != nil {
			return fmt.Errorf("failed to add host route dst %v: %v", ipc.IP, err)
		}
	}

	// add route to cluster ip through veth
	if serviceCIDR != "" {
		_, svcCIDR, err := net.ParseCIDR(serviceCIDR)
		err = p.nlink.RouteAdd(&netlink.Route{
			LinkIndex: contVeth.Index,
			Scope:     netlink.SCOPE_LINK,
			Dst:       svcCIDR,
		})
		if err != nil {
			return fmt.Errorf("failed to add clusterIP route dst %v: %v", svcCIDR, err)
		}
	}

	// add route to node local dns through veth
	if localDNSAddr != "" {
		dnsIP := net.ParseIP(localDNSAddr)
		if dnsIP == nil {
			return fmt.Errorf("invalid dns address: %v", localDNSAddr)
		}
		addrBits := 128
		if dnsIP.To4() != nil {
			addrBits = 32
		}
		dnsCIDR := &net.IPNet{
			IP:   dnsIP,
			Mask: net.CIDRMask(addrBits, addrBits),
		}
		err = p.nlink.RouteAdd(&netlink.Route{
			LinkIndex: contVeth.Index,
			Scope:     netlink.SCOPE_LINK,
			Dst:       dnsCIDR,
		})
		if err != nil {
			return fmt.Errorf("failed to add local dns route dst %v: %v", dnsCIDR, err)
		}
	}

	// Send a gratuitous arp for all borrowed v4 addresses
	for _, ipc := range pr.IPs {
		if ipc.Version == "4" {
			_ = p.netutil.GratuitousArpOverIface(ipc.Address.IP, *contVeth)
		}
	}

	if err := p.disableRPFCheck(ifName); err != nil {
		return fmt.Errorf("failed to disable rpf check for %v: %v", ifName, err)
	}

	return nil
}

func (p *ptpPlugin) setupContainerVeth(
	netns ns.NetNS,
	ifName string,
	mtu int,
	hostAddrs []netlink.Addr,
	pr *current.Result,
	serviceCIDR string,
	localDNSAddr string,
) (*current.Interface, error) {
	hostInterface := &current.Interface{}

	err := netns.Do(func(hostNS ns.NetNS) error {
		return p.setupContainerNetNSVeth(
			hostInterface,
			hostNS,
			ifName,
			mtu,
			hostAddrs,
			pr,
			serviceCIDR,
			localDNSAddr,
		)
	})
	if err != nil {
		return nil, err
	}
	return hostInterface, nil
}

func (p *ptpPlugin) setupHostVeth(vethName string, hostAddrs []netlink.Addr, result *current.Result) error {
	// no IPs to route
	if len(result.IPs) == 0 {
		return nil
	}

	// lookup by name as interface ids might have changed
	veth, err := p.netutil.InterfaceByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", vethName, err)
	}

	// add destination routes to Pod IPs
	for _, ipc := range result.IPs {
		addrBits := 128
		if ipc.Address.IP.To4() != nil {
			addrBits = 32
		}

		var src net.IP
		if len(hostAddrs) > 0 {
			src = hostAddrs[0].IP
		}

		err := p.nlink.RouteAdd(&netlink.Route{
			LinkIndex: veth.Index,
			Scope:     netlink.SCOPE_LINK,
			Dst: &net.IPNet{
				IP:   ipc.Address.IP,
				Mask: net.CIDRMask(addrBits, addrBits),
			},
			Src: src,
		})

		if err != nil {
			return fmt.Errorf("failed to add host route dst %v: %v", ipc.Address.IP, err)
		}
	}

	// For centos 8, sysctl -w net.ipv4.conf.${hostveth}.proxy_arp=1
	if _, err := p.sysctl.Sysctl(fmt.Sprintf(proxyARPSysctlTemplate, vethName), "1"); err != nil {
		return fmt.Errorf("failed to set net.ipv4.conf.%s.proxy_arp=1: %s", vethName, err)
	}

	// Send a gratuitous arp for all borrowed v4 addresses
	for _, ipc := range hostAddrs {
		if ipc.IP.To4() != nil {
			_ = p.netutil.GratuitousArpOverIface(ipc.IP, *veth)
		}
	}

	return nil
}

// disableRPFCheck set /proc/sys/net/ipv4/conf/*/rp_filter to 0
// xref https://www.kernel.org/doc/Documentation/networking/ip-sysctl.txt
// The max value from conf/{all,interface}/rp_filter is used
//	when doing source validation on the {interface}.
func (p *ptpPlugin) disableRPFCheck(ifName string) error {
	_, err := p.sysctl.Sysctl(fmt.Sprintf(rpFilterSysctlTemplate, "all"), "0")
	if err != nil {
		return fmt.Errorf("failed to disable RP filter for interface %v: %v", "all", err)
	}
	_, err = p.sysctl.Sysctl(fmt.Sprintf(rpFilterSysctlTemplate, ifName), "0")
	if err != nil {
		return fmt.Errorf("failed to disable RP filter for interface %v: %v", ifName, err)
	}
	return nil
}

func (p *ptpPlugin) getHostInterfaceAddress(ifName string, ipv4 bool, ipv6 bool) ([]netlink.Addr, error) {
	iface, err := p.nlink.LinkByName(ifName)
	if err != nil {
		return nil, fmt.Errorf("failed to find link %q: %v", ifName, err)
	}

	var hostAddrs []netlink.Addr

	if ipv4 {
		addrs, err := p.nlink.AddrList(iface, netlink.FAMILY_V4)
		if err != nil {
			return nil, fmt.Errorf("failed to get host %q IPv4 addresses: %v", iface, err)
		}
		hostAddrs = append(hostAddrs, addrs...)
	}

	if ipv6 {
		addrs, err := p.nlink.AddrList(iface, netlink.FAMILY_V6)
		if err != nil {
			return nil, fmt.Errorf("failed to get host %q IPv6 addresses: %v", iface, err)
		}
		hostAddrs = append(hostAddrs, addrs...)
	}

	return hostAddrs, nil
}

func (p *ptpPlugin) cmdAdd(args *skel.CmdArgs) error {
	conf, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	if conf.PrevResult == nil {
		return fmt.Errorf("must be called as chained plugin")
	}

	containerIPs := make([]net.IP, 0, len(conf.PrevResult.IPs))
	for _, cip := range conf.PrevResult.IPs {
		containerIPs = append(containerIPs, cip.Address.IP)
	}

	if len(containerIPs) == 0 {
		return fmt.Errorf("got no container IPs")
	}

	containerIPv4 := false
	containerIPv6 := false
	for _, ipc := range containerIPs {
		if ipc.To4() != nil {
			containerIPv4 = true
		} else {
			containerIPv6 = true
		}
	}

	if err := p.enableForwarding(containerIPv4, containerIPv6); err != nil {
		return err
	}

	hostAddrs, err := p.getHostInterfaceAddress(conf.HostInterface, containerIPv4, containerIPv6)
	if err != nil {
		return err
	}

	netns, err := p.ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	hostInterface, err := p.setupContainerVeth(netns, conf.ContainerInterface, conf.MTU,
		hostAddrs, conf.PrevResult, conf.ServiceCIDR, conf.LocalDNSAddr)
	if err != nil {
		return err
	}

	if err = p.setupHostVeth(hostInterface.Name, hostAddrs, conf.PrevResult); err != nil {
		return err
	}

	// Pass through the result for the next plugin
	return p.types.PrintResult(conf.PrevResult, conf.CNIVersion)
}

func (p *ptpPlugin) cmdDel(args *skel.CmdArgs) error {
	if args.Netns == "" {
		return nil
	}

	err := p.ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		var err error
		err = p.ip.DelLinkByName(args.IfName)
		if err != nil && err == ip.ErrLinkNotFound {
			return nil
		}
		return err
	})

	if err != nil {
		return err
	}

	return nil
}

func (p *ptpPlugin) cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func main() {
	p := newPlugin()
	skel.PluginMain(p.cmdAdd, p.cmdDel, p.cmdCheck, cni.PluginSupportedVersions, bv.BuildString("unnumbered-ptp"))
}
