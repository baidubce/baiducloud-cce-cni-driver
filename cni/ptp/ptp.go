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
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils"
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
	proxyARPSysctlTemplate    = "net.ipv4.conf.%s.proxy_arp"
	proxyDelaySysctlTemplate  = "net.ipv4.neigh.%s.proxy_delay"
	ndpProxySysctlTemplate    = "net.ipv6.conf.%s.proxy_ndp"
	disableIPv6SysctlTemplate = "net.ipv6.conf.%s.disable_ipv6"
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

type NetConf struct {
	types.NetConf
	IPMasq         bool `json:"ipMasq"`
	MTU            int  `json:"mtu"`
	EnableARPProxy bool `json:"enableARPProxy"`
	// HostVethPrefix is the veth for container prefix on host
	HostVethPrefix string `json:"vethPrefix"`
}

func (p *ptpPlugin) loadConf(bytes []byte) (*NetConf, string, error) {
	n := &NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}

	if n.HostVethPrefix == "" {
		n.HostVethPrefix = "veth"
	}

	return n, n.CNIVersion, nil
}

func (p *ptpPlugin) loadK8SArgs(envArgs string) (*cni.K8SArgs, error) {
	k8sArgs := cni.K8SArgs{}
	if envArgs != "" {
		err := p.types.LoadArgs(envArgs, &k8sArgs)
		if err != nil {
			return nil, err
		}
	}
	return &k8sArgs, nil
}

func (p *ptpPlugin) setupVeth(contVethName, hostVethName string, mtu int, hostNS ns.NetNS) (net.Interface, net.Interface, error) {
	return p.ip.SetupVethWithName(contVethName, hostVethName, mtu, hostNS)
}

func (p *ptpPlugin) setupContainerVeth(netns ns.NetNS, ifName string, hostVethName string, mtu int, pr *current.Result, enableARPProxy bool) (*current.Interface, *current.Interface, error) {
	hostInterface := &current.Interface{}
	containerInterface := &current.Interface{}

	err := netns.Do(func(hostNS ns.NetNS) error {
		return p.setupContainerNetNSVeth(
			hostInterface,
			containerInterface,
			netns,
			hostNS,
			ifName,
			hostVethName,
			mtu,
			pr,
			enableARPProxy)
	})
	if err != nil {
		return nil, nil, err
	}
	return hostInterface, containerInterface, nil
}

func (p *ptpPlugin) setupContainerNetNSVeth(
	hostInterface *current.Interface,
	containerInterface *current.Interface,
	netns ns.NetNS,
	hostNS ns.NetNS,
	ifName string,
	hostVethName string,
	mtu int,
	pr *current.Result,
	enableARPProxy bool) error {
	hostVeth, contVeth0, err := p.setupVeth(ifName, hostVethName, mtu, hostNS)
	if err != nil {
		return err
	}
	hostInterface.Name = hostVeth.Name
	hostInterface.Mac = hostVeth.HardwareAddr.String()
	containerInterface.Name = contVeth0.Name
	containerInterface.Mac = contVeth0.HardwareAddr.String()
	containerInterface.Sandbox = netns.Path()

	for _, ipc := range pr.IPs {
		// All addresses apply to the container veth interface
		ipc.Interface = current.Int(1)
	}

	pr.Interfaces = []*current.Interface{hostInterface, containerInterface}

	if err = p.ipam.ConfigureIface(ifName, pr); err != nil {
		return err
	}

	contVeth, err := p.netutil.InterfaceByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to look up %q: %v", ifName, err)
	}

	for _, ipc := range pr.IPs {
		// Delete the route that was automatically added
		route := netlink.Route{
			LinkIndex: contVeth.Index,
			Dst: &net.IPNet{
				IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
				Mask: ipc.Address.Mask,
			},
			Scope: netlink.SCOPE_NOWHERE,
		}

		if err := p.nlink.RouteDel(&route); err != nil && !netlinkwrapper.IsNotExistError(err) {
			return fmt.Errorf("failed to delete route %v: %v", route, err)
		}

		isIPv6 := false
		addrBits := 32
		if ipc.Version == "6" {
			isIPv6 = true
			addrBits = 128
		}
		if !enableARPProxy {
			for _, r := range []netlink.Route{
				{
					LinkIndex: contVeth.Index,
					Dst: &net.IPNet{
						IP:   ipc.Gateway,
						Mask: net.CIDRMask(addrBits, addrBits),
					},
					Scope: netlink.SCOPE_LINK,
					Src:   ipc.Address.IP,
				},
				{
					LinkIndex: contVeth.Index,
					Dst: &net.IPNet{
						IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
						Mask: ipc.Address.Mask,
					},
					Scope: netlink.SCOPE_UNIVERSE,
					Gw:    ipc.Gateway,
					Src:   ipc.Address.IP,
				},
			} {
				if err := p.nlink.RouteAdd(&r); err != nil {
					return fmt.Errorf("failed to add route %v: %v", r, err)
				}
			}
		} else { // arp/ndp proxy 模式
			if !isIPv6 {
				// ip route add 169.254.1.1 dev eth0 scope link
				gw := net.IPv4(169, 254, 1, 1)
				gwNet := &net.IPNet{IP: gw, Mask: net.CIDRMask(32, 32)}
				if err := p.nlink.RouteReplace(
					&netlink.Route{
						LinkIndex: contVeth.Index,
						Scope:     netlink.SCOPE_LINK,
						Dst:       gwNet,
					},
				); err != nil {
					return fmt.Errorf("failed to add gw route inside the container: %v", err)
				}
				// ip route add default via 169.254.1.1 dev eth0
				if err := p.nlink.RouteReplace(
					&netlink.Route{
						LinkIndex: contVeth.Index,
						Scope:     netlink.SCOPE_UNIVERSE,
						Dst:       &net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: net.CIDRMask(0, 32)},
						Gw:        gw,
					},
				); err != nil {
					return fmt.Errorf("failed to add default route inside the container: %v", err)
				}

			}
			if isIPv6 {
				// Make sure ipv6 is enabled in the container/pod network namespace.
				// Without these sysctls enabled, interfaces will come up but they won't get a link local IPv6 address
				// which is required to add the default IPv6 route.

				for _, o := range []string{"all", "default", "lo"} {
					if _, err := p.sysctl.Sysctl(fmt.Sprintf(disableIPv6SysctlTemplate, o), "0"); err != nil {
						return fmt.Errorf("failed to set net.ipv6.conf.%s.disable_ipv6=0: %s", o, err)
					}
				}

				// Retry several times as the LL can take a several micro/miliseconds to initialize and we may be too fast after these sysctls
				var hostVethIPv6Addr net.IP
				hostNSErr := hostNS.Do(func(_ ns.NetNS) error {
					return p.getIPv6LinkLocalAddress(hostInterface, &hostVethIPv6Addr)
				})
				if hostNSErr != nil {
					return fmt.Errorf("failed to get veth info of host side")
				}

				// ip route add hostveth dev eth0 scope link
				gwNet := &net.IPNet{IP: hostVethIPv6Addr, Mask: net.CIDRMask(128, 128)}
				if err := p.nlink.RouteReplace(
					&netlink.Route{
						LinkIndex: contVeth.Index,
						Scope:     netlink.SCOPE_LINK,
						Dst:       gwNet,
					},
				); err != nil {
					return fmt.Errorf("failed to add gw route inside the container: %v", err)
				}
				// ip route add default via 169.254.1.1 dev eth0
				if err := p.nlink.RouteReplace(
					&netlink.Route{
						LinkIndex: contVeth.Index,
						Scope:     netlink.SCOPE_UNIVERSE,
						Dst:       &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
						Gw:        hostVethIPv6Addr,
					},
				); err != nil {
					return fmt.Errorf("failed to add default route inside the container: %v", err)
				}
			}
		}
	}

	// Send a gratuitous arp for all v4 addresses
	for _, ipc := range pr.IPs {
		if ipc.Version == "4" {
			_ = p.netutil.GratuitousArpOverIface(ipc.Address.IP, *contVeth)
		}
	}

	return nil
}

func (p *ptpPlugin) getIPv6LinkLocalAddress(hostInterface *current.Interface, hostInterfaceIPv6Addr *net.IP) error {
	// No need to add a dummy next hop route as the host veth device will already have an IPv6
	// link local address that can be used as a next hop.
	// Just fetch the address of the host end of the veth and use it as the next hop.
	var err error
	var hostVeth netlink.Link
	var addresses []netlink.Addr

	for i := 0; i < 10; i++ {
		hostVeth, err = p.nlink.LinkByName(hostInterface.Name)
		if err != nil {
			err = fmt.Errorf("failed to get host veth link %v: %v", hostInterface.Name, err)
		}
		addresses, err = p.nlink.AddrList(hostVeth, netlink.FAMILY_V6)
		if err != nil {
			err = fmt.Errorf("error listing IPv6 addresses for the host side of the veth pair: %s", err)
		}
		if len(addresses) < 1 {
			err = fmt.Errorf("failed to get IPv6 addresses for host side of the veth pair")
		}
		if err == nil {
			*hostInterfaceIPv6Addr = addresses[0].IP
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	return nil
}

func (p *ptpPlugin) setupHostVeth(vethName string, result *current.Result, enableARPProxy bool) error {
	var hasIPv4, hasIPv6 bool

	// hostVeth moved namespaces and may have a new ifindex
	veth, err := p.nlink.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", vethName, err)
	}

	for _, ipc := range result.IPs {
		maskLen := 128
		if ipc.Address.IP.To4() != nil {
			maskLen = 32
		}

		if ipc.Version == "4" {
			hasIPv4 = true
		} else if ipc.Version == "6" {
			hasIPv6 = true
		}

		if !enableARPProxy {
			ipn := &net.IPNet{
				IP:   ipc.Gateway,
				Mask: net.CIDRMask(maskLen, maskLen),
			}
			addr := &netlink.Addr{IPNet: ipn, Label: ""}
			if err = p.nlink.AddrAdd(veth, addr); err != nil {
				return fmt.Errorf("failed to add IP addr (%#v) to veth: %v", ipn, err)
			}
		}

		ipn := &net.IPNet{
			IP:   ipc.Address.IP,
			Mask: net.CIDRMask(maskLen, maskLen),
		}
		// dst happens to be the same as IP/net of host veth
		if err = p.nlink.RouteReplace(&netlink.Route{
			LinkIndex: veth.Attrs().Index,
			Scope:     netlink.SCOPE_LINK,
			Dst:       ipn,
		}); err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to add route on host: %v", err)
		}
	}

	if enableARPProxy {
		if err = p.configureSysctls(vethName, hasIPv4, hasIPv6); err != nil {
			return err
		}
	}

	return nil
}

func (p *ptpPlugin) configureSysctls(hostVethName string, hasIPv4, hasIPv6 bool) error {
	if hasIPv4 {
		if _, err := p.sysctl.Sysctl(fmt.Sprintf(proxyDelaySysctlTemplate, hostVethName), "0"); err != nil {
			return fmt.Errorf("failed to set net.ipv4.neigh.%s.proxy_delay=0: %s", hostVethName, err)
		}

		if _, err := p.sysctl.Sysctl(fmt.Sprintf(proxyARPSysctlTemplate, hostVethName), "1"); err != nil {
			return fmt.Errorf("failed to set net.ipv4.conf.%s.proxy_arp=1: %s", hostVethName, err)
		}
	}

	if hasIPv6 {
		if _, err := p.sysctl.Sysctl(fmt.Sprintf(disableIPv6SysctlTemplate, hostVethName), "0"); err != nil {
			return fmt.Errorf("failed to set net.ipv6.conf.%s.disable_ipv6=0: %s", hostVethName, err)
		}

		if _, err := p.sysctl.Sysctl(fmt.Sprintf(ndpProxySysctlTemplate, hostVethName), "1"); err != nil {
			return fmt.Errorf("failed to set net.ipv6.conf.%s.proxy_ndp=1: %s", hostVethName, err)
		}
	}

	return nil
}

func (p *ptpPlugin) cmdAdd(args *skel.CmdArgs) error {
	conf, _, err := p.loadConf(args.StdinData)
	if err != nil {
		return err
	}

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	hostVethName := VethNameForPod(string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), conf.HostVethPrefix)
	// clean up old veth
	_ = p.ip.DelLinkByName(hostVethName)

	// run the IPAM plugin and get back the config to apply
	r, err := p.ipam.ExecAdd(conf.IPAM.Type, args.StdinData)
	if err != nil {
		return err
	}

	// Invoke ipam del if err to avoid ip leak
	defer func() {
		if err != nil {
			p.ipam.ExecDel(conf.IPAM.Type, args.StdinData)
		}
	}()

	// Convert whatever the IPAM result was into the current Result type
	result, err := current.NewResultFromResult(r)
	if err != nil {
		return err
	}

	if len(result.IPs) == 0 {
		return errors.New("IPAM plugin returned missing IP config")
	}

	if err := p.ip.EnableForward(result.IPs); err != nil {
		return fmt.Errorf("Could not enable IP forwarding: %v", err)
	}

	netns, err := p.ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	hostInterface, _, err := p.setupContainerVeth(netns, args.IfName, hostVethName, conf.MTU, result, conf.EnableARPProxy)
	if err != nil {
		return err
	}

	if err = p.setupHostVeth(hostInterface.Name, result, conf.EnableARPProxy); err != nil {
		return err
	}

	if conf.IPMasq {
		chain := utils.FormatChainName(conf.Name, args.ContainerID)
		comment := utils.FormatComment(conf.Name, args.ContainerID)
		for _, ipc := range result.IPs {
			if err = p.ip.SetupIPMasq(&ipc.Address, chain, comment); err != nil {
				return err
			}
		}
	}

	// Only override the DNS settings in the previous result if any DNS fields
	// were provided to the ptp plugin. This allows, for example, IPAM plugins
	// to specify the DNS settings instead of the ptp plugin.
	if dnsConfSet(conf.DNS) {
		result.DNS = conf.DNS
	}

	return p.types.PrintResult(result, conf.CNIVersion)
}

func dnsConfSet(dnsConf types.DNS) bool {
	return dnsConf.Nameservers != nil ||
		dnsConf.Search != nil ||
		dnsConf.Options != nil ||
		dnsConf.Domain != ""
}

func (p *ptpPlugin) cmdDel(args *skel.CmdArgs) error {
	conf, _, err := p.loadConf(args.StdinData)
	if err != nil {
		return err
	}

	if err := p.ipam.ExecDel(conf.IPAM.Type, args.StdinData); err != nil {
		return err
	}

	if args.Netns == "" {
		return nil
	}

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	// If the device isn't there then don't try to clean up IP masq either.
	var ipnets []*net.IPNet
	err = p.ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		var err error
		ipnets, err = p.ip.DelLinkByNameAddr(args.IfName)
		if err != nil && err == ip.ErrLinkNotFound {
			return nil
		}
		return err
	})

	if err != nil {
		return err
	}

	if len(ipnets) != 0 && conf.IPMasq {
		chain := utils.FormatChainName(conf.Name, args.ContainerID)
		comment := utils.FormatComment(conf.Name, args.ContainerID)
		for _, ipn := range ipnets {
			err = p.ip.TeardownIPMasq(ipn, chain, comment)
		}
	}

	return err
}

func main() {
	p := newPlugin()
	skel.PluginMain(p.cmdAdd, p.cmdCheck, p.cmdDel, cni.PluginSupportedVersions, bv.BuildString("ptp"))
}

func (p *ptpPlugin) cmdCheck(args *skel.CmdArgs) error {
	return nil
}

// VethNameForPod return host-side veth name for pod
// max veth length is 15
func VethNameForPod(name, namespace, prefix string) string {
	// A SHA1 is always 20 bytes long, and so is sufficient for generating the
	// veth name and mac addr.
	h := sha1.New()
	h.Write([]byte(namespace + "." + name))
	return fmt.Sprintf("%s%s", prefix, hex.EncodeToString(h.Sum(nil))[:11])
}
