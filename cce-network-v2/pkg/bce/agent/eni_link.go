package agent

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/os"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/sysctl"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	k8serrors "k8s.io/apimachinery/pkg/util/errors"
)

const (
	fromENIPrimaryIPRulePriority = 1024
	rpFilterSysctlTemplate       = "net.ipv4.conf.%s.rp_filter"

	// ENINamePrefix ENI device name prefix
	ENINamePrefix = "cce-eni"
	ERINamePrefix = "cce-eri"

	// maxENIIndex max ENI index, this is the max number of ENI that can be attached to a node
	// it is also the max route table for a node
	maxENIIndex = 252
)

var (
	// eniIndexMutex protects the ENI index allocation to prevent race conditions
	// when multiple goroutines are trying to find available ENI indices simultaneously
	eniIndexMutex sync.Mutex
)

// findAvailableENIIndex finds the first available ENI index in a thread-safe manner
// This function prevents race conditions when multiple goroutines are trying to
// allocate ENI indices simultaneously
func findAvailableENIIndex() (int, error) {
	eniIndexMutex.Lock()
	defer eniIndexMutex.Unlock()

	linkList, err := netlink.LinkList()
	if err != nil {
		return -1, fmt.Errorf("failed to list links: %v", err)
	}

	// Build a map of used indices
	used := make(map[int]bool)
	for _, link := range linkList {
		name := link.Attrs().Name
		if strings.HasPrefix(name, ENINamePrefix+"-") || strings.HasPrefix(name, ERINamePrefix+"-") {
			parts := strings.Split(name, "-")
			if len(parts) == 3 {
				if idx, err := strconv.Atoi(parts[2]); err == nil {
					used[idx] = true
				}
			}
		}
	}

	// Find the first available index
	for i := 0; i < maxENIIndex; i++ {
		if !used[i] {
			return i, nil
		}
	}

	return -1, fmt.Errorf("no available ENI index found, all %d indices are in use", maxENIIndex)
}

type eniLink struct {
	eni     *ccev2.ENI
	eniID   string
	macAddr string

	link      netlink.Link
	linkIndex int
	eniIndex  int
	linkName  string

	// eni processing
	log *logrus.Entry

	ipv4Gateway string
	ipv6Gateway string

	release *os.OSRelease
}

// newENILink create a new link config from ccev2 ENI mac
// this method will rename link to cce-eni-{index}
func newENILink(eni *ccev2.ENI, release *os.OSRelease) (*eniLink, error) {
	var ec = &eniLink{}

	ec.macAddr = eni.Spec.ENI.MacAddress
	ec.eni = eni
	elink, err := link.FindENILinkByMac(ec.macAddr)
	if err != nil {
		return ec, err
	}
	linkIndex := elink.Attrs().Index

	ec.eniID = eni.Spec.ENI.ID
	ec.linkIndex = linkIndex
	ec.linkName = elink.Attrs().Name
	ec.link = elink
	ec.log = initLog.WithFields(logrus.Fields{
		"eni":       ec.eniID,
		"linkIndex": ec.linkIndex,
		"linkName":  ec.linkName,
	})
	ec.release = release
	return ec, nil
}

func (ec *eniLink) rename(isPrimary bool) error {
	elink := ec.link
	eniIndex := elink.Attrs().Index
	var err error

	udevName := elink.Attrs().Name

	namePrefix := ENINamePrefix
	if ec.eni != nil && ec.eni.Spec.Type == ccev2.ENIForERI {
		namePrefix = ERINamePrefix
	}

	// rename link to cce-eni-{index} or cce-eri-{index}
	if !strings.HasPrefix(udevName, namePrefix) {
		var cceName string
		if !isPrimary {
			// find the first unused index using thread-safe function
			availableIndex, err := findAvailableENIIndex()
			if err != nil {
				return fmt.Errorf("failed to find available ENI index: %v", err)
			}
			eniIndex = availableIndex
		}
		cceName = fmt.Sprintf("%s-%d", namePrefix, eniIndex)

		err = ec.release.HostOS().DisableDHCPv6(udevName, cceName)
		if err != nil {
			return err
		}
		ec.log.WithField("ifname", cceName).Info("generate ifcfg file")

		// Devices can be renamed only when down
		if err = netlink.LinkSetDown(elink); err != nil {
			return fmt.Errorf("failed to set %q down: %v", elink.Attrs().Name, err)
		}

		// Rename container device to respect args.IfName
		if err := netlink.LinkSetName(elink, cceName); err != nil {
			return fmt.Errorf("failed to rename device %q to %q: %v", elink.Attrs().Name, cceName, err)
		}
		elink, err = netlink.LinkByName(cceName)
		if err != nil {
			return fmt.Errorf("failed to find device %q: %v", cceName, err)
		}

		// Ensure NetworkManager stops managing the renamed interface
		err = ec.release.HostOS().EnsureNetworkManagerUnmanaged(cceName)
		if err != nil {
			ec.log.WithError(err).Warnf("failed to ensure NetworkManager stops managing %s", cceName)
		}

		// Disable IPv6 for the interface if IPv6 is not enabled in configuration
		if !option.Config.EnableIPv6 {
			if err = sysctl.Write(fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6", cceName), "1"); err != nil {
				ec.log.WithError(err).Errorf("failed to disable IPv6 for interface %s", cceName)
				return err
			}
			ec.log.Infof("successfully disabled IPv6 for interface %s", cceName)
		}

	}

	if strings.HasPrefix(elink.Attrs().Name, namePrefix) {
		nameAndIndex := strings.Split(elink.Attrs().Name, namePrefix+"-")
		if len(nameAndIndex) == 2 {
			eniIndex, _ = strconv.Atoi(nameAndIndex[1])
		}
	}

	ec.linkName = elink.Attrs().Name
	ec.eniIndex = eniIndex
	ec.link = elink
	return nil
}

// setupENILink set link addr if need
// return:
// error: error when opening the link
func (ec *eniLink) ensureLinkConfig() (err error) {
	// 1. set link up
	err = ec.ensureLinkUp()
	if err != nil {
		return err
	}

	// 2. add primary IP
	err = ec.ensureENIAddr()
	if err != nil {
		return err
	}

	// 3. disable rtfilter
	err = ec.disableRPFCheck()
	if err != nil {
		return err
	}

	// 4. ensure IP address again before route configuration to handle timing issues
	err = ec.ensureENIAddr()
	if err != nil {
		return err
	}

	// 5. set from primary ip rule route with table 127 + index
	err = ec.ensureFromPrimaryRoute()
	if err != nil {
		return err
	}

	// 6. check and adjust ring buffer settings for secondary ENI interfaces
	err = ec.checkAndAdjustRingBuffer()
	if err != nil {
		return err
	}

	return nil
}

func (ec *eniLink) disableRPFCheck() error {
	var errs []error
	defaultRouteInterface, err := link.DetectDefaultRouteInterfaceName()
	errs = append(errs, err)
	for _, intf := range []string{"all", defaultRouteInterface, ec.linkName} {
		if intf != "" {
			err = link.DisableRpFilter(intf)
			errs = append(errs, err)
		}
	}
	return k8serrors.NewAggregate(errs)
}

func (ec *eniLink) disableDad() error {
	if !option.Config.EnableIPv6 {
		return nil
	}

	var errs []error
	err := sysctl.CompareAndSet(fmt.Sprintf("net.ipv6.conf.%s.accept_dad", ec.linkName), "1", "0")
	if err != nil {
		errs = append(errs, err)
	}
	err = sysctl.CompareAndSet(fmt.Sprintf("net.ipv6.conf.%s.dad_transmits", ec.linkName), "1", "0")
	if err != nil {
		errs = append(errs, err)
	}
	err = sysctl.CompareAndSet(fmt.Sprintf("net.ipv6.conf.%s.enhanced_dad", ec.linkName), "1", "0")
	if err != nil {
		errs = append(errs, err)
	}
	err = sysctl.CompareAndSet(fmt.Sprintf("net.ipv6.conf.%s.autoconf", ec.linkName), "1", "0")
	if err != nil {
		errs = append(errs, err)
	}
	err = sysctl.CompareAndSet(fmt.Sprintf("net.ipv6.conf.%s.keep_addr_on_down", ec.linkName), "0", "1")
	if err != nil {
		errs = append(errs, err)
	}
	return k8serrors.NewAggregate(errs)
}

func (ec *eniLink) ensureLinkUp() error {
	if ec.link.Attrs().Flags&net.FlagUp == 0 {
		ec.log.Infof("eni link is down, will bring it up")
		return netlink.LinkSetUp(ec.link)
	}
	return nil
}

// set eni addr to link
func (ec *eniLink) ensureENIAddr() error {
	subnet, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(ec.eni.Spec.ENI.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of eni: %v", err)
	}

	setAddr := func(cidr string, ips []*models.PrivateIP, family int) error {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("failed to parse cidr(%s) from subnet(%s): %w", cidr, subnet.Name, err)
		}
		primaryIPs := ccev2.FiltePrimaryAddress(ips)
		if len(primaryIPs) != 1 {
			return fmt.Errorf("no primary address found in eni(%s) with primary ips [%v]", ec.eni.Name, primaryIPs)
		}
		for _, primaryIP := range primaryIPs {
			addr := &netlink.Addr{
				IPNet: &net.IPNet{
					IP:   net.ParseIP(primaryIP.PrivateIPAddress),
					Mask: ipnet.Mask,
				},
			}
			err := link.EnsureLinkAddr(addr, ec.link)
			if err != nil {
				return fmt.Errorf("failed to set IP address %s on device %s: %v",
					primaryIP.PrivateIPAddress, ec.link.Attrs().Name, err)
			}
		}
		return nil
	}

	// 1. set ipv4 addrs to eni link
	if option.Config.EnableIPv4 {
		if err = setAddr(subnet.Spec.CIDR, ec.eni.Spec.ENI.PrivateIPSet, netlink.FAMILY_V4); err != nil {
			return err
		}
	}

	// 2. set ipv6 addrs to elink
	if option.Config.EnableIPv6 {
		// Ensure IPv6 link-local address exists on the interface
		if err = ec.ensureIPv6LinkLocalAddr(); err != nil {
			return err
		}
		if err = setAddr(subnet.Spec.IPv6CIDR, ec.eni.Spec.ENI.IPV6PrivateIPSet, netlink.FAMILY_V6); err != nil {
			// 2.12.17 -> 2.12.18 have a bug, when ipv6 addr set failed, the eni will not be ready on node forever
			// Therefore, skip the error of ipv6 addr set, and do not return error.
			ec.log.Warningf("ipv6 failed to set addr for eni: %v", err)
			return nil
		}
	}
	return nil
}

func (ec *eniLink) ensureFromPrimaryRoute() (err error) {
	if ec.eni.Spec.RouteTableOffset <= 0 {
		ec.eni.Spec.RouteTableOffset = defaultRouteTableIDOffset
	}
	rtTable := ec.eni.Spec.RouteTableOffset + ec.eniIndex

	// ip route show dev ethX
	// save gateway to ec
	// ip route replace default via {eniGW} dev ethX table {rtTable} onlink
	if option.Config.EnableIPv4 {
		gateway, err := EnsureRoute(ec.log, ec.link, netlink.FAMILY_V4, rtTable, ip.IPv4ZeroCIDR)
		if err != nil {
			ec.log.Errorf("ipv4 failed to ensure route for eni from primary route: %v", err)
			return err
		}
		ec.ipv4Gateway = gateway
	}
	if option.Config.EnableIPv6 {
		gateway, err := EnsureRoute(ec.log, ec.link, netlink.FAMILY_V6, rtTable, ip.IPv6ZeroCIDR)
		if err != nil {
			ec.log.Warningf("ipv6 failed to ensure route for eni from primary route: %v", err)
			// 2.12.17 -> 2.12.18 have a bug, when ipv6 addr set failed, the eni will not be ready on node forever
			// Therefore, skip the error of ipv6 addr set, and do not return error.
			return nil
		}
		ec.ipv6Gateway = gateway
	}
	return nil
}

func (ec *eniLink) ensureENINeigh() error {
	// set proxy neigh
	err := ensureENIArpProxy(ec.log, ec.macAddr)
	if err != nil {
		ec.log.WithError(err).Error("set arp proxy falied")
		return err
	}
	err = ensureENINDPProxy(ec.log, ec.eni)
	if err != nil {
		ec.log.WithError(err).Error("set ndp proxy falied")
		return err
	}
	// 2. disable dad
	return ec.disableDad()
}

func EnsureRoute(log *logrus.Entry, eniLink netlink.Link, family int, rtTable int, routeDst *net.IPNet) (string, error) {
	addrs, err := netlink.AddrList(eniLink, family)
	if err != nil {
		return "", err
	}

	if len(addrs) == 0 {
		return "", fmt.Errorf("no address found in dev(%s) with family %d", eniLink.Attrs().Name, family)
	}

	routes, err := netlink.RouteList(eniLink, family)
	if err != nil {
		return "", fmt.Errorf("failed to list dev %v routes: %v", eniLink.Attrs().Name, err)
	}

	var gateway net.IP
	for _, addr := range addrs {
		if !addr.IP.IsGlobalUnicast() {
			continue
		}
		gateway, err = metadataLinkGateway(eniLink.Attrs().HardwareAddr.String(), addr.IP)
		if err != nil {
			return "", fmt.Errorf("failed to get gateway of eni from meta-data")
		}
		break
	}

	// clean up old route
	for _, route := range routes {
		if route.Table == rtTable || route.Table == syscall.RT_TABLE_LOCAL {
			continue
		}

		err = netlink.RouteDel(&route)
		if err != nil {
			log.Warnf("failed to delete route %v: %v", route, err)
		}
		log.Infof("delete route %v", route)
	}

	routes, _ = netlink.RouteList(eniLink, family)
	for _, route := range routes {
		// default route is always returned
		if route.Dst.IP.Equal(routeDst.IP) && route.Gw.Equal(gateway) {
			return route.Gw.String(), nil
		}
	}

	if family == netlink.FAMILY_V4 {
		natgreLink, useBigNat := bigNatLinkExists()
		if useBigNat {
			log.Infof("detect bignat used...")
			return gateway.String(), ensureBigNatRoutes(log, natgreLink, eniLink, gateway, rtTable)
		}
	}

	return gateway.String(), replaceDefaultRoute(routeDst, gateway, eniLink, rtTable)
}

func metadataLinkGateway(mac string, ip net.IP) (net.IP, error) {
	gateway, err := defaultMetaClient.GetLinkGateway(mac, ip.String())
	if err != nil {
		return nil, err
	}

	gw := net.ParseIP(gateway)
	if gw == nil {
		return nil, fmt.Errorf("error parsing gateway IP address: %v", gateway)
	}
	return gw, nil
}

func replaceDefaultRoute(dst *net.IPNet, gw net.IP, dev netlink.Link, table int) error {
	var mask net.IPMask = net.CIDRMask(32, 32)
	if dst.IP.To4() == nil {
		mask = net.CIDRMask(128, 128)
	}

	routes := []netlink.Route{
		{
			LinkIndex: dev.Attrs().Index,
			Dst: &net.IPNet{
				IP:   gw,
				Mask: mask,
			},
			Table: table,
			Scope: netlink.SCOPE_LINK,
		}, {
			LinkIndex: dev.Attrs().Index,
			Dst:       dst,
			Table:     table,
			Gw:        gw,
		},
	}
	return link.ReplaceRoute(routes)
}

// EthtoolRingSettings represents the ring buffer settings
type EthtoolRingSettings struct {
	RxCurrent int
	RxMax     int
	TxCurrent int
	TxMax     int
}

// checkAndAdjustRingBuffer checks the ring buffer settings for ENI interfaces
// and adjusts them if current values don't match the maximum values
func (ec *eniLink) checkAndAdjustRingBuffer() error {
	scopedLog := ec.log.WithField("interface", ec.linkName)
	scopedLog.Debug("checking ring buffer settings for ENI interface")

	// Get current ring buffer settings
	ringSettings, err := getRingBufferSettings(ec.linkName)
	if err != nil {
		scopedLog.WithError(err).Debug("failed to get ring buffer settings, skipping adjustment")
		return nil // Don't fail the entire process if we can't get ring buffer settings
	}

	scopedLog.WithFields(logrus.Fields{
		"rx_current": ringSettings.RxCurrent,
		"rx_max":     ringSettings.RxMax,
		"tx_current": ringSettings.TxCurrent,
		"tx_max":     ringSettings.TxMax,
	}).Debug("current ring buffer settings")

	// Check if adjustment is needed
	needsAdjustment := false
	if ringSettings.RxCurrent != ringSettings.RxMax {
		scopedLog.Infof("RX ring buffer current (%d) differs from max (%d), adjustment needed",
			ringSettings.RxCurrent, ringSettings.RxMax)
		needsAdjustment = true
	}
	if ringSettings.TxCurrent != ringSettings.TxMax {
		scopedLog.Infof("TX ring buffer current (%d) differs from max (%d), adjustment needed",
			ringSettings.TxCurrent, ringSettings.TxMax)
		needsAdjustment = true
	}

	if !needsAdjustment {
		scopedLog.Debug("ring buffer settings are already optimal, no adjustment needed")
		return nil
	}

	// Adjust ring buffer settings
	err = adjustRingBufferSettings(ec.linkName, ringSettings)
	if err != nil {
		scopedLog.WithError(err).Warning("failed to adjust ring buffer settings, but continuing")
		return nil // Don't fail the entire process if ring buffer adjustment fails
	}

	scopedLog.WithFields(logrus.Fields{
		"interface": ec.linkName,
		"rx_from":   ringSettings.RxCurrent,
		"rx_to":     ringSettings.RxMax,
		"tx_from":   ringSettings.TxCurrent,
		"tx_to":     ringSettings.TxMax,
	}).Info("successfully adjusted ring buffer settings to maximum values")
	return nil
}

// getRingBufferSettings gets the current ring buffer settings using ethtool
func getRingBufferSettings(interfaceName string) (*EthtoolRingSettings, error) {
	cmd := exec.Command("ethtool", "-g", interfaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to execute ethtool -g %s: %w", interfaceName, err)
	}

	return parseRingBufferOutput(string(output))
}

// parseRingBufferOutput parses the output of ethtool -g command
func parseRingBufferOutput(output string) (*EthtoolRingSettings, error) {
	lines := strings.Split(output, "\n")
	settings := &EthtoolRingSettings{}

	// Flag to track if we're in the "Current hardware settings" section
	inCurrentSection := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "Pre-set maximums:") {
			inCurrentSection = false
			continue
		}
		if strings.Contains(line, "Current hardware settings:") {
			inCurrentSection = true
			continue
		}

		if !inCurrentSection {
			// Parse maximum values
			if strings.HasPrefix(line, "RX:") {
				if val, err := extractNumberFromLine(line); err == nil {
					settings.RxMax = val
				}
			} else if strings.HasPrefix(line, "TX:") {
				if val, err := extractNumberFromLine(line); err == nil {
					settings.TxMax = val
				}
			}
		} else {
			// Parse current values
			if strings.HasPrefix(line, "RX:") {
				if val, err := extractNumberFromLine(line); err == nil {
					settings.RxCurrent = val
				}
			} else if strings.HasPrefix(line, "TX:") {
				if val, err := extractNumberFromLine(line); err == nil {
					settings.TxCurrent = val
				}
			}
		}
	}

	return settings, nil
}

// extractNumberFromLine extracts the number from a line like "RX:		256"
func extractNumberFromLine(line string) (int, error) {
	re := regexp.MustCompile(`\d+`)
	match := re.FindString(line)
	if match == "" {
		return 0, fmt.Errorf("no number found in line: %s", line)
	}
	return strconv.Atoi(match)
}

// adjustRingBufferSettings adjusts the ring buffer settings to maximum values
func adjustRingBufferSettings(interfaceName string, settings *EthtoolRingSettings) error {
	// Build ethtool command arguments
	args := []string{"-G", interfaceName}

	if settings.RxCurrent != settings.RxMax {
		args = append(args, "rx", strconv.Itoa(settings.RxMax))
	}
	if settings.TxCurrent != settings.TxMax {
		args = append(args, "tx", strconv.Itoa(settings.TxMax))
	}

	cmd := exec.Command("ethtool", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("failed to execute ethtool %s: %w, output: %s",
			strings.Join(args, " "), err, string(output))
	}

	return nil
}

// NetlinkInterface
type NetlinkInterface interface {
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
}

// DefaultNetlinkImpl
type DefaultNetlinkImpl struct{}

func (d *DefaultNetlinkImpl) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (d *DefaultNetlinkImpl) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

var defaultNetlinkImpl NetlinkInterface = &DefaultNetlinkImpl{}

// ensureIPv6LinkLocalAddr checks if the interface has an IPv6 link-local address, and adds one if not present.
func (ec *eniLink) ensureIPv6LinkLocalAddr() error {
	return ec.ensureIPv6LinkLocalAddrWithNetlink(defaultNetlinkImpl)
}

// ensureIPv6LinkLocalAddrWithNetlink
func (ec *eniLink) ensureIPv6LinkLocalAddrWithNetlink(nl NetlinkInterface) error {
	addrs, err := nl.AddrList(ec.link, netlink.FAMILY_V6)
	if err != nil {
		return fmt.Errorf("failed to list IPv6 addresses on %s: %v", ec.link.Attrs().Name, err)
	}
	hasLinkLocal := false
	for _, addr := range addrs {
		if addr.IP.IsLinkLocalUnicast() {
			hasLinkLocal = true
			break
		}
	}
	if !hasLinkLocal {
		mac := ec.link.Attrs().HardwareAddr
		if mac == nil || len(mac) != 6 {
			return fmt.Errorf("invalid MAC address for interface %s", ec.link.Attrs().Name)
		}
		// Generate a standard EUI-64 link-local address from MAC address
		ip := macToEUI64LinkLocal(mac)
		if ip == nil {
			return fmt.Errorf("failed to generate EUI-64 link-local address for %s", ec.link.Attrs().Name)
		}
		linkLocal := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   ip,
				Mask: net.CIDRMask(64, 128),
			},
			Scope: int(netlink.SCOPE_LINK),
		}
		if err := nl.AddrAdd(ec.link, linkLocal); err != nil {
			ec.log.WithError(err).Warnf("failed to add IPv6 link-local address to %s", ec.link.Attrs().Name)
			return err
		}
		ec.log.WithField("address", ip.String()).Infof("successfully added IPv6 link-local address to %s", ec.link.Attrs().Name)
	}
	return nil
}

// macToEUI64LinkLocal generates a standard IPv6 link-local address (fe80::/64) using EUI-64 from a MAC address.
func macToEUI64LinkLocal(mac net.HardwareAddr) net.IP {
	if len(mac) != 6 {
		return nil
	}
	// EUI-64: flip the 7th bit (Universal/Local bit)
	eui := make([]byte, 8)
	eui[0] = mac[0] ^ 0x02
	eui[1] = mac[1]
	eui[2] = mac[2]
	eui[3] = 0xff
	eui[4] = 0xfe
	eui[5] = mac[3]
	eui[6] = mac[4]
	eui[7] = mac[5]
	// Compose the full IPv6 address: fe80::eui-64
	ip := make(net.IP, net.IPv6len)
	copy(ip, []byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0})
	copy(ip[8:], eui)
	return ip
}
