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

package node

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/byteorder"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/common"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/mac"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

const preferPublicIP bool = true

var (
	// addrsMu protects addrs. Outside the addresses struct
	// so that we can Uninitialize() without linter complaining
	// about lock copying.
	addrsMu lock.RWMutex
	addrs   addresses
)

type addresses struct {
	ipv4Loopback      net.IP
	ipv4Address       net.IP
	ipv4RouterAddress net.IP
	ipv4NodePortAddrs map[string]net.IP // iface name => ip addr
	ipv4MasqAddrs     map[string]net.IP // iface name => ip addr
	ipv6Address       net.IP
	ipv6RouterAddress net.IP
	ipv6NodePortAddrs map[string]net.IP // iface name => ip addr
	ipv4AllocRange    *cidr.CIDR
	ipv6AllocRange    *cidr.CIDR
	routerInfo        RouterInfo

	// k8s Node External IP
	ipv4ExternalAddress net.IP
	ipv6ExternalAddress net.IP

	// Addresses of the cce-health endpoint located on the node
	ipv4HealthIP net.IP
	ipv6HealthIP net.IP

	// Addresses of the CCE Ingress located on the node
	ipv4IngressIP net.IP
	ipv6IngressIP net.IP

	// k8s Node IP (either InternalIP or ExternalIP or nil; the former is preferred)
	k8sNodeIP net.IP

	ipsecKeyIdentity uint8

	wireguardPubKey string
}

type RouterInfo interface {
	GetIPv4CIDRs() []net.IPNet
	GetMac() mac.MAC
	GetInterfaceNumber() int
}

func makeIPv6HostIP() net.IP {
	ipstr := "fc00::10CA:1"
	ip := net.ParseIP(ipstr)
	if ip == nil {
		log.WithField(logfields.IPAddr, ipstr).Fatal("Unable to parse IP")
	}

	return ip
}

// InitDefaultPrefix initializes the node address and allocation prefixes with
// default values derived from the system. device can be set to the primary
// network device of the system in which case the first address with global
// scope will be regarded as the system's node address.
func InitDefaultPrefix(device string) {
	addrsMu.Lock()
	defer addrsMu.Unlock()

	if option.Config.EnableIPv4 {
		ip, err := firstGlobalV4Addr(device, addrs.ipv4RouterAddress, preferPublicIP)
		if err != nil {
			return
		}

		if addrs.ipv4Address == nil {
			addrs.ipv4Address = ip
		}

		if addrs.ipv4AllocRange == nil {
			// If the IPv6AllocRange is not nil then the IPv4 allocation should be
			// derived from the IPv6AllocRange.
			//                     vvvv vvvv
			// FD00:0000:0000:0000:0000:0000:0000:0000
			if addrs.ipv6AllocRange != nil {
				ip = net.IPv4(addrs.ipv6AllocRange.IP[8],
					addrs.ipv6AllocRange.IP[9],
					addrs.ipv6AllocRange.IP[10],
					addrs.ipv6AllocRange.IP[11])
			}
			v4range := fmt.Sprintf(defaults.DefaultIPv4Prefix+"/%d",
				ip.To4()[3], defaults.DefaultIPv4PrefixLen)
			_, ip4net, err := net.ParseCIDR(v4range)
			if err != nil {
				log.WithError(err).WithField(logfields.V4Prefix, v4range).Panic("BUG: Invalid default IPv4 prefix")
			}

			addrs.ipv4AllocRange = cidr.NewCIDR(ip4net)
			log.WithField(logfields.V4Prefix, addrs.ipv4AllocRange).Info("Using autogenerated IPv4 allocation range")
		}
	}

	if option.Config.EnableIPv6 {
		if addrs.ipv6Address == nil {
			// Find a IPv6 node address first
			addrs.ipv6Address, _ = firstGlobalV6Addr(device, addrs.ipv6RouterAddress, preferPublicIP)
			if addrs.ipv6Address == nil {
				addrs.ipv6Address = makeIPv6HostIP()
			}
		}

		if addrs.ipv6AllocRange == nil && addrs.ipv4AllocRange != nil {
			// The IPv6 allocation should be derived from the IPv4 allocation.
			ip := addrs.ipv4AllocRange.IP
			v6range := fmt.Sprintf("%s%02x%02x:%02x%02x:0:0/%d",
				option.Config.IPv6ClusterAllocCIDRBase, ip[0], ip[1], ip[2], ip[3], 96)

			_, ip6net, err := net.ParseCIDR(v6range)
			if err != nil {
				log.WithError(err).WithField(logfields.V6Prefix, v6range).Panic("BUG: Invalid default IPv6 prefix")
			}

			addrs.ipv6AllocRange = cidr.NewCIDR(ip6net)
			log.WithField(logfields.V6Prefix, addrs.ipv6AllocRange).Info("Using autogenerated IPv6 allocation range")
		}
	}
}

// InitNodePortAddrs initializes NodePort IPv{4,6} addrs for the given devices.
// If inheritIPAddrFromDevice is non-empty, then the IP addr for the devices
// will be derived from it.
func InitNodePortAddrs(devices []string, inheritIPAddrFromDevice string) error {
	addrsMu.Lock()
	defer addrsMu.Unlock()

	var inheritedIP net.IP
	var err error

	if option.Config.EnableIPv4 {
		if inheritIPAddrFromDevice != "" {
			inheritedIP, err = firstGlobalV4Addr(inheritIPAddrFromDevice, addrs.k8sNodeIP, !preferPublicIP)
			if err != nil {
				return fmt.Errorf("failed to determine IPv4 of %s for NodePort", inheritIPAddrFromDevice)
			}
		}
		addrs.ipv4NodePortAddrs = make(map[string]net.IP, len(devices))
		for _, device := range devices {
			if inheritIPAddrFromDevice != "" {
				addrs.ipv4NodePortAddrs[device] = inheritedIP
			} else {
				ip, err := firstGlobalV4Addr(device, addrs.k8sNodeIP, !preferPublicIP)
				if err != nil {
					return fmt.Errorf("failed to determine IPv4 of %s for NodePort", device)
				}
				addrs.ipv4NodePortAddrs[device] = ip
			}
		}
	}

	if option.Config.EnableIPv6 {
		if inheritIPAddrFromDevice != "" {
			inheritedIP, err = firstGlobalV6Addr(inheritIPAddrFromDevice, addrs.k8sNodeIP, !preferPublicIP)
			if err != nil {
				return fmt.Errorf("Failed to determine IPv6 of %s for NodePort", inheritIPAddrFromDevice)
			}
		}
		addrs.ipv6NodePortAddrs = make(map[string]net.IP, len(devices))
		for _, device := range devices {
			if inheritIPAddrFromDevice != "" {
				addrs.ipv6NodePortAddrs[device] = inheritedIP
			} else {
				ip, err := firstGlobalV6Addr(device, addrs.k8sNodeIP, !preferPublicIP)
				if err != nil {
					return fmt.Errorf("Failed to determine IPv6 of %s for NodePort", device)
				}
				addrs.ipv6NodePortAddrs[device] = ip
			}
		}
	}

	return nil
}

func clone(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	dup := make(net.IP, len(ip))
	copy(dup, ip)
	return dup
}

// GetIPv4Loopback returns the loopback IPv4 address of this node.
func GetIPv4Loopback() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv4Loopback)
}

// SetIPv4Loopback sets the loopback IPv4 address of this node.
func SetIPv4Loopback(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv4Loopback = clone(ip)
	addrsMu.Unlock()
}

// GetIPv4AllocRange returns the IPv4 allocation prefix of this node
func GetIPv4AllocRange() *cidr.CIDR {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return addrs.ipv4AllocRange.DeepCopy()
}

// GetIPv6AllocRange returns the IPv6 allocation prefix of this node
func GetIPv6AllocRange() *cidr.CIDR {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return addrs.ipv6AllocRange.DeepCopy()
}

// SetIPv4 sets the IPv4 node address. It must be reachable on the network.
// It is set based on the following priority:
// - NodeInternalIP
// - NodeExternalIP
// - other IP address type
func SetIPv4(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv4Address = clone(ip)
	addrsMu.Unlock()
}

// GetIPv4 returns one of the IPv4 node address available with the following
// priority:
// - NodeInternalIP
// - NodeExternalIP
// - other IP address type.
// It must be reachable on the network.
func GetIPv4() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv4Address)
}

// SetInternalIPv4Router sets the cce internal IPv4 node address, it is allocated from the node prefix.
// This must not be conflated with k8s internal IP as this IP address is only relevant within the
// CCE-managed network (this means within the node for direct routing mode and on the overlay
// for tunnel mode).
func SetInternalIPv4Router(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv4RouterAddress = clone(ip)
	addrsMu.Unlock()
}

// GetInternalIPv4Router returns the cce internal IPv4 node address. This must not be conflated with
// k8s internal IP as this IP address is only relevant within the CCE-managed network (this means
// within the node for direct routing mode and on the overlay for tunnel mode).
func GetInternalIPv4Router() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv4RouterAddress)
}

// SetK8sExternalIPv4 sets the external IPv4 node address. It must be a public IP that is routable
// on the network as well as the internet.
func SetK8sExternalIPv4(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv4ExternalAddress = clone(ip)
	addrsMu.Unlock()
}

// GetK8sExternalIPv4 returns the external IPv4 node address. It must be a public IP that is routable
// on the network as well as the internet. It can return nil if no External IPv4 address is assigned.
func GetK8sExternalIPv4() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv4ExternalAddress)
}

// GetRouterInfo returns additional information for the router, the cce_host interface.
func GetRouterInfo() RouterInfo {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return addrs.routerInfo
}

// SetRouterInfo sets additional information for the router, the cce_host interface.
func SetRouterInfo(info RouterInfo) {
	addrsMu.Lock()
	addrs.routerInfo = info
	addrsMu.Unlock()
}

// GetHostMasqueradeIPv4 returns the IPv4 address to be used for masquerading
// any traffic that is being forwarded from the host into the CCE cluster.
func GetHostMasqueradeIPv4() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv4RouterAddress)
}

// SetIPv4AllocRange sets the IPv4 address pool to use when allocating
// addresses for local endpoints
func SetIPv4AllocRange(net *cidr.CIDR) {
	addrsMu.Lock()
	addrs.ipv4AllocRange = net.DeepCopy()
	addrsMu.Unlock()
}

// Uninitialize resets this package to the default state, for use in
// testsuite code.
func Uninitialize() {
	addrsMu.Lock()
	addrs = addresses{}
	addrsMu.Unlock()
}

// GetNodePortIPv4Addrs returns the node-port IPv4 address for NAT
func GetNodePortIPv4Addrs() []net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	addrs4 := make([]net.IP, 0, len(addrs.ipv4NodePortAddrs))
	for _, addr := range addrs.ipv4NodePortAddrs {
		addrs4 = append(addrs4, clone(addr))
	}
	return addrs4
}

// GetNodePortIPv4AddrsWithDevices returns the map iface => NodePort IPv4.
func GetNodePortIPv4AddrsWithDevices() map[string]net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return copyStringToNetIPMap(addrs.ipv4NodePortAddrs)
}

// GetNodePortIPv6Addrs returns the node-port IPv6 address for NAT
func GetNodePortIPv6Addrs() []net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	addrs6 := make([]net.IP, 0, len(addrs.ipv6NodePortAddrs))
	for _, addr := range addrs.ipv6NodePortAddrs {
		addrs6 = append(addrs6, clone(addr))
	}
	return addrs6
}

// GetNodePortIPv6AddrsWithDevices returns the map iface => NodePort IPv6.
func GetNodePortIPv6AddrsWithDevices() map[string]net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return copyStringToNetIPMap(addrs.ipv6NodePortAddrs)
}

// GetMasqIPv4AddrsWithDevices returns the map iface => BPF masquerade IPv4.
func GetMasqIPv4AddrsWithDevices() map[string]net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return copyStringToNetIPMap(addrs.ipv4MasqAddrs)
}

// SetIPv6NodeRange sets the IPv6 address pool to be used on this node
func SetIPv6NodeRange(net *cidr.CIDR) {
	addrsMu.Lock()
	addrs.ipv6AllocRange = net.DeepCopy()
	addrsMu.Unlock()
}

// AutoComplete completes the parts of addressing that can be auto derived
func AutoComplete() error {

	InitDefaultPrefix("veth")

	if option.Config.EnableIPv6 && addrs.ipv6AllocRange == nil {
		return fmt.Errorf("IPv6 allocation CIDR is not configured. Please specificy --%s", option.IPv6Range)
	}

	if option.Config.EnableIPv4 && addrs.ipv4AllocRange == nil {
		return fmt.Errorf("IPv4 allocation CIDR is not configured. Please specificy --%s", option.IPv4Range)
	}

	return nil
}

func chooseHostIPsToRestore(ipv6 bool, fromK8s, fromFS net.IP, cidrs []*cidr.CIDR) (ip net.IP, err error) {
	switch {
	// If both IPs are available, then check both for validity. We prefer the
	// local IP from the FS over the K8s IP.
	case fromK8s != nil && fromFS != nil:
		if fromK8s.Equal(fromFS) {
			ip = fromK8s
		} else {
			ip = fromFS
			err = errMismatch

			// Check if we need to fallback to using the fromK8s IP, in the
			// case that the IP from the FS is not within the CIDR. If we
			// fallback, then we also need to check the fromK8s IP is also
			// within the CIDR.
			for _, cidr := range cidrs {
				if cidr != nil && cidr.Contains(ip) {
					return
				} else if cidr != nil && cidr.Contains(fromK8s) {
					ip = fromK8s
					return
				}
			}
		}
	case fromK8s == nil && fromFS != nil:
		ip = fromFS
	case fromK8s != nil && fromFS == nil:
		ip = fromK8s
	case fromK8s == nil && fromFS == nil:
		// We do nothing in this case because there are no router IPs to
		// restore.
		return
	}

	for _, cidr := range cidrs {
		if cidr != nil && cidr.Contains(ip) {
			return
		}
	}

	err = errDoesNotBelong
	return
}

// restoreCCEHostIPsFromFS restores the router IPs (`cce_host`) from a
// previous CCE run. The IPs are restored from the filesystem. This is part
// 1/2 of the restoration.
func restoreCCEHostIPsFromFS() {
	// Read the previous cce_host IPs from node_config.h for backward
	// compatibility.
	router4, router6 := getCCEHostIPs()
	if option.Config.EnableIPv4 {
		SetInternalIPv4Router(router4)
	}
	if option.Config.EnableIPv6 {
		SetIPv6Router(router6)
	}
}

var (
	errMismatch      = errors.New("mismatched IPs")
	errDoesNotBelong = errors.New("IP does not belong to CIDR")
)

const mismatchRouterIPsMsg = "Mismatch of router IPs found during restoration. The Kubernetes resource contained %s, while the filesystem contained %s. Using the router IP from the filesystem. To change the router IP, specify --%s and/or --%s."

// ValidatePostInit validates the entire addressing setup and completes it as
// required
func ValidatePostInit() error {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	if option.Config.EnableIPv4 {
		if addrs.ipv4Address == nil {
			return fmt.Errorf("external IPv4 node address could not be derived, please configure via --ipv4-node")
		}
	}

	if option.Config.EnableIPv4 && addrs.ipv4RouterAddress == nil {
		return fmt.Errorf("BUG: Internal IPv4 node address was not configured")
	}

	return nil
}

// SetIPv6 sets the IPv6 address of the node
func SetIPv6(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv6Address = clone(ip)
	addrsMu.Unlock()
}

// GetIPv6 returns the IPv6 address of the node
func GetIPv6() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv6Address)
}

// GetHostMasqueradeIPv6 returns the IPv6 address to be used for masquerading
// any traffic that is being forwarded from the host into the CCE cluster.
func GetHostMasqueradeIPv6() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv6Address)
}

// GetIPv6Router returns the IPv6 address of the node
func GetIPv6Router() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv6RouterAddress)
}

// SetIPv6Router returns the IPv6 address of the node
func SetIPv6Router(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv6RouterAddress = clone(ip)
	addrsMu.Unlock()
}

// SetK8sExternalIPv6 sets the external IPv6 node address. It must be a public IP.
func SetK8sExternalIPv6(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv6ExternalAddress = clone(ip)
	addrsMu.Unlock()
}

// GetK8sExternalIPv6 returns the external IPv6 node address.
func GetK8sExternalIPv6() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv6ExternalAddress)
}

// GetNodeAddressing returns the NodeAddressing model for the local IPs.
func GetNodeAddressing() *models.NodeAddressing {
	a := &models.NodeAddressing{}

	if option.Config.EnableIPv6 {
		a.IPV6 = &models.NodeAddressingElement{
			Enabled:    option.Config.EnableIPv6,
			IP:         GetIPv6Router().String(),
			AllocRange: GetIPv6AllocRange().String(),
		}
	}

	if option.Config.EnableIPv4 {
		a.IPV4 = &models.NodeAddressingElement{
			Enabled:    option.Config.EnableIPv4,
			IP:         GetInternalIPv4Router().String(),
			AllocRange: GetIPv4AllocRange().String(),
		}
	}

	return a
}

func getCCEHostIPsFromFile(nodeConfig string) (ipv4GW, ipv6Router net.IP) {
	// ipLen is the length of the IP address stored in the node_config.h
	// it has the same length for both IPv4 and IPv6.
	const ipLen = net.IPv6len

	var hasIPv4, hasIPv6 bool
	f, err := os.Open(nodeConfig)
	switch {
	case err != nil:
	default:
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			txt := scanner.Text()
			switch {
			case !hasIPv6 && strings.Contains(txt, defaults.RestoreV6Addr):
				defineLine := strings.Split(txt, defaults.RestoreV6Addr)
				if len(defineLine) != 2 {
					continue
				}
				ipv6 := common.C2GoArray(defineLine[1])
				if len(ipv6) != ipLen {
					continue
				}
				ipv6Router = net.IP(ipv6)
				hasIPv6 = true
			case !hasIPv4 && strings.Contains(txt, defaults.RestoreV4Addr):
				defineLine := strings.Split(txt, defaults.RestoreV4Addr)
				if len(defineLine) != 2 {
					continue
				}
				ipv4 := common.C2GoArray(defineLine[1])
				if len(ipv4) != ipLen {
					continue
				}
				ipv4GW = net.IP(ipv4)
				hasIPv4 = true

			// Legacy cases based on the header defines:
			case !hasIPv4 && strings.Contains(txt, "IPV4_GATEWAY"):
				// #define IPV4_GATEWAY 0xee1c000a
				defineLine := strings.Split(txt, " ")
				if len(defineLine) != 3 {
					continue
				}
				ipv4GWHex := strings.TrimPrefix(defineLine[2], "0x")
				ipv4GWUint64, err := strconv.ParseUint(ipv4GWHex, 16, 32)
				if err != nil {
					continue
				}
				if ipv4GWUint64 != 0 {
					bs := make([]byte, net.IPv4len)
					byteorder.Native.PutUint32(bs, uint32(ipv4GWUint64))
					ipv4GW = net.IPv4(bs[0], bs[1], bs[2], bs[3])
					hasIPv4 = true
				}
			case !hasIPv6 && strings.Contains(txt, " ROUTER_IP "):
				// #define ROUTER_IP 0xf0, 0xd, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xa, 0x0, 0x0, 0x0, 0x0, 0x0, 0x8a, 0xd6
				defineLine := strings.Split(txt, " ROUTER_IP ")
				if len(defineLine) != 2 {
					continue
				}
				ipv6 := common.C2GoArray(defineLine[1])
				if len(ipv6) != net.IPv6len {
					continue
				}
				ipv6Router = net.IP(ipv6)
				hasIPv6 = true
			}
		}
	}
	return ipv4GW, ipv6Router
}

// getCCEHostIPs returns the CCE IPv4 gateway and router IPv6 address from
// the node_config.h file if is present; or by deriving it from
// defaults.HostDevice interface, on which only the IPv4 is possible to derive.
func getCCEHostIPs() (ipv4GW, ipv6Router net.IP) {
	nodeConfig := option.Config.GetNodeConfigPath()
	ipv4GW, ipv6Router = getCCEHostIPsFromFile(nodeConfig)
	if ipv4GW != nil || ipv6Router != nil {
		log.WithFields(logrus.Fields{
			"ipv4": ipv4GW,
			"ipv6": ipv6Router,
			"file": nodeConfig,
		}).Info("Restored router address from node_config")
		return ipv4GW, ipv6Router
	}
	return getCCEHostIPsFromNetDev(defaults.HostDevice)
}

// SetIPsecKeyIdentity sets the IPsec key identity an opaque value used to
// identity encryption keys used on the node.
func SetIPsecKeyIdentity(id uint8) {
	addrsMu.Lock()
	addrs.ipsecKeyIdentity = id
	addrsMu.Unlock()
}

// GetIPsecKeyIdentity returns the IPsec key identity of the node
func GetIPsecKeyIdentity() uint8 {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return addrs.ipsecKeyIdentity
}

// GetK8sNodeIPs returns k8s Node IP addr.
func GetK8sNodeIP() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.k8sNodeIP)
}

// SetK8sNodeIP sets k8s Node IP addr.
func SetK8sNodeIP(ip net.IP) {
	addrsMu.Lock()
	addrs.k8sNodeIP = clone(ip)
	addrsMu.Unlock()
}

func SetWireguardPubKey(key string) {
	addrsMu.Lock()
	addrs.wireguardPubKey = key
	addrsMu.Unlock()
}

func GetWireguardPubKey() string {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return addrs.wireguardPubKey
}

// SetEndpointHealthIPv4 sets the IPv4 cce-health endpoint address.
func SetEndpointHealthIPv4(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv4HealthIP = clone(ip)
	addrsMu.Unlock()
}

// GetEndpointHealthIPv4 returns the IPv4 cce-health endpoint address.
func GetEndpointHealthIPv4() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv4HealthIP)
}

// SetEndpointHealthIPv6 sets the IPv6 cce-health endpoint address.
func SetEndpointHealthIPv6(ip net.IP) {
	addrsMu.Lock()
	addrs.ipv6HealthIP = clone(ip)
	addrsMu.Unlock()
}

// GetEndpointHealthIPv6 returns the IPv6 cce-health endpoint address.
func GetEndpointHealthIPv6() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv6HealthIP)
}

// SetIngressIPv4 sets the local IPv4 source address for CCE Ingress.
func SetIngressIPv4(ip net.IP) {
	addrsMu.RLock()
	addrs.ipv4IngressIP = clone(ip)
	addrsMu.RUnlock()
}

// GetIngressIPv4 returns the local IPv4 source address for CCE Ingress.
func GetIngressIPv4() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv4IngressIP)
}

// SetIngressIPv6 sets the local IPv6 source address for CCE Ingress.
func SetIngressIPv6(ip net.IP) {
	addrsMu.RLock()
	addrs.ipv6IngressIP = clone(ip)
	addrsMu.RUnlock()
}

// GetIngressIPv6 returns the local IPv6 source address for CCE Ingress.
func GetIngressIPv6() net.IP {
	addrsMu.RLock()
	defer addrsMu.RUnlock()
	return clone(addrs.ipv6IngressIP)
}

func copyStringToNetIPMap(in map[string]net.IP) map[string]net.IP {
	out := make(map[string]net.IP, len(in))
	for iface, ip := range in {
		dup := make(net.IP, len(ip))
		copy(dup, ip)
		out[iface] = dup
	}
	return out
}
