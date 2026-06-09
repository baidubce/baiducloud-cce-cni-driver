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
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
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
	defaultMTUforENI             = 1500

	// ENINamePrefix ENI device name prefix
	ENINamePrefix = "cce-eni"
	ERINamePrefix = "cce-eri"

	// maxENIIndex max ENI index, this is the max number of ENI that can be attached to a node
	// it is also the max route table for a node
	maxENIIndex = 252
)

// ENI configuration state tracking (memory-only)
type eniConfigState struct {
	ENIID       string    `json:"eni_id"`
	MACAddress  string    `json:"mac_address"` // Primary identifier - MAC is unique and persistent
	ConfigSaved bool      `json:"config_saved"`
	LastUpdated time.Time `json:"last_updated"` // When configuration was last fetched from MetaAPI

	// ENI correct configuration from metadata API
	CorrectConfig *eniCorrectConfig `json:"correct_config,omitempty"`
}

// eniCorrectConfig stores the correct ENI configuration from metadata API
// Only stores primary IP configuration, secondary IPs are managed by IPAM
type eniCorrectConfig struct {
	// Primary IP configuration (only these are managed by ENI agent)
	PrimaryIPv4 string `json:"primary_ipv4"`
	PrimaryIPv6 string `json:"primary_ipv6"`

	// Network configuration
	Gateway     string `json:"gateway"`
	IPv6Gateway string `json:"ipv6_gateway"`

	// Interface configuration
	MTU int `json:"mtu"`
}

var (
	// eniConfigStates uses sync.Map for better concurrent performance
	// Key: macAddress (string), Value: *eniConfigState
	eniConfigStates sync.Map
)

// getOrCreateENIConfigState gets or creates ENI configuration state in memory
// Uses MAC address as the primary identifier since it's unique and persistent across renames/reboots
// Returns a complete and valid ENI config, refreshing from MetaAPI if needed
func getOrCreateENIConfigState(eniID, macAddress string, eni *ccev2.ENI) (*eniConfigState, error) {
	// Check memory cache first using MAC address as key
	value, exists := eniConfigStates.Load(macAddress)

	var state *eniConfigState
	if exists {
		state = value.(*eniConfigState)
	}

	// Determine if we need to refresh from MetaAPI
	needsRefresh := false
	reason := ""

	if !exists || state == nil {
		// No cached config (first time or restart)
		needsRefresh = true
		reason = "no cached config"

		// Create new state if not found
		state = &eniConfigState{
			ENIID:       eniID,
			MACAddress:  macAddress,
			ConfigSaved: false,
			LastUpdated: time.Time{}, // Zero time indicates never updated
		}
	} else if time.Since(state.LastUpdated) > 24*time.Hour {
		// Cache expired (>24h)
		needsRefresh = true
		reason = "cache expired"
	}

	// Refresh from MetaAPI if needed
	if needsRefresh {
		config, err := getENICorrectConfigFromMetadata(macAddress, eni)
		if err != nil {
			return nil, fmt.Errorf("failed to get ENI configuration from metadata: %w", err)
		}

		state.CorrectConfig = config
		state.ConfigSaved = true
		state.LastUpdated = time.Now()

		// Log the refresh reason
		logrus.WithFields(logrus.Fields{
			"eniID":      eniID,
			"macAddress": macAddress,
			"reason":     reason,
		}).Info("Refreshed ENI configuration from MetaAPI")

		// Store the state (whether new or updated)
		eniConfigStates.Store(macAddress, state)
	}

	return state, nil
}

// checkAndFixNetworkConfiguration checks and fixes network configuration
// 1. Check IP configuration - fix if wrong (add missing, remove extra)
// 2. Check MTU - log warning if wrong (don't fix to avoid disruption)
func (ec *eniLink) checkAndFixNetworkConfiguration(state *eniConfigState) error {
	if state == nil || state.CorrectConfig == nil {
		return fmt.Errorf("no configuration available")
	}

	config := state.CorrectConfig
	linkName := ec.link.Attrs().Name

	ec.log.WithField("interface", linkName).Debug("Checking and fixing network configuration")

	// 1. Check and fix IP configuration
	if err := ec.ensureCorrectIPConfiguration(config); err != nil {
		return fmt.Errorf("failed to fix IP configuration: %w", err)
	}

	// 2. Check MTU and log warning if mismatch (don't fix)
	if config.MTU > 0 {
		currentMTU := ec.link.Attrs().MTU
		if currentMTU != config.MTU {
			ec.log.WithFields(logrus.Fields{
				"interface":   linkName,
				"currentMTU":  currentMTU,
				"expectedMTU": config.MTU,
			}).Warning("MTU mismatch detected but not correcting to avoid network disruption")
		}
	}

	return nil
}

// ensureCorrectIPConfiguration ensures the interface has only the correct IP addresses from MetaAPI
// This function will: 1) Remove any incorrect/extra IPs, 2) Add missing correct IPs
func (ec *eniLink) ensureCorrectIPConfiguration(config *eniCorrectConfig) error {
	if config == nil {
		return fmt.Errorf("no configuration provided")
	}

	linkName := ec.link.Attrs().Name

	// Build expected IP set from MetaAPI configuration
	expectedIPs := make(map[string]bool)
	if config.PrimaryIPv4 != "" {
		expectedIPs[config.PrimaryIPv4] = true
	}
	if config.PrimaryIPv6 != "" {
		expectedIPs[config.PrimaryIPv6] = true
	}

	ec.log.WithFields(logrus.Fields{
		"interface":   linkName,
		"expectedIPs": expectedIPs,
	}).Debug("Ensuring correct IP configuration from MetaAPI")

	// Get current IP addresses on the interface
	actualAddrs, err := netlink.AddrList(ec.link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list current addresses: %w", err)
	}

	actualIPs := make(map[string]struct{})

	// Step 1: Remove incorrect/extra IP addresses
	for _, addr := range actualAddrs {
		ipStr := addr.IP.String()

		// Skip link-local and loopback addresses (these are system managed)
		if addr.IP.IsLinkLocalUnicast() || addr.IP.IsLoopback() {
			continue
		}

		// Skip non-global unicast addresses
		if !addr.IP.IsGlobalUnicast() {
			continue
		}

		actualIPs[ipStr] = struct{}{}
		// If this IP is not in our expected set, remove it
		if !expectedIPs[ipStr] {
			ec.log.WithFields(logrus.Fields{
				"interface": linkName,
				"ip":        ipStr,
			}).Info("Removing incorrect IP address from interface")

			if err := netlink.AddrDel(ec.link, &addr); err != nil {
				ec.log.WithError(err).WithFields(logrus.Fields{
					"interface": linkName,
					"ip":        ipStr,
				}).Warning("Failed to remove incorrect IP address")
				// Continue with other IPs even if one fails
			}
		}
	}

	// Step 2: Ensure correct IP addresses are present
	for expectedIP := range expectedIPs {
		if _, ok := actualIPs[expectedIP]; ok {
			continue
		}

		ec.log.WithFields(logrus.Fields{
			"interface": linkName,
			"ip":        expectedIP,
		}).Info("Adding missing correct IP address to interface")

		// Parse IP and determine subnet mask from config
		ipObj := net.ParseIP(expectedIP)
		if ipObj == nil {
			ec.log.WithFields(logrus.Fields{
				"interface": linkName,
				"ip":        expectedIP,
			}).Error("Failed to parse expected IP address")
			continue
		}

		// Get subnet mask from ENI spec
		var mask net.IPMask
		var err error
		if ipObj.To4() != nil {
			// IPv4: get subnet mask from ENI spec
			mask, err = ec.getSubnetMaskFromENI(ipObj.String())
			if err != nil {
				ec.log.WithError(err).WithField("ip", ipObj.String()).Error("Failed to get IPv4 subnet mask from ENI")
				continue // Skip this IP if we can't get the subnet mask
			}
		} else {
			// IPv6: get subnet mask from ENI spec
			mask, err = ec.getIPv6SubnetMaskFromENI(ipObj.String())
			if err != nil {
				ec.log.WithError(err).WithField("ip", ipObj.String()).Error("Failed to get IPv6 subnet mask from ENI")
				continue // Skip this IP if we can't get the subnet mask
			}
		}

		addr := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   ipObj,
				Mask: mask,
			},
		}

		if err := netlink.AddrAdd(ec.link, addr); err != nil {
			ec.log.WithError(err).WithFields(logrus.Fields{
				"interface": linkName,
				"ip":        expectedIP,
			}).Warning("Failed to add correct IP address")
			// Continue with other IPs even if one fails
		}
	}

	ec.log.WithField("interface", linkName).Debug("IP configuration correction completed")
	return nil
}

// getSubnetMaskFromENI gets subnet mask for IPv4 from ENI spec and Subnet CR
// Returns error instead of nil to provide better error handling for callers
func (ec *eniLink) getSubnetMaskFromENI(ipAddress string) (net.IPMask, error) {
	if ec.eni == nil {
		return nil, fmt.Errorf("ENI object is nil")
	}

	// Use ENI's subnet ID directly
	subnetID := ec.eni.Spec.ENI.SubnetID

	if subnetID == "" {
		return nil, fmt.Errorf("ENI subnet ID is empty")
	}

	// Get Subnet CR from cluster
	subnet, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(subnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet CR %s for IP %s: %w", subnetID, ipAddress, err)
	}

	// Parse CIDR from Subnet CR spec
	if subnet.Spec.CIDR == "" {
		return nil, fmt.Errorf("subnet %s has empty CIDR", subnetID)
	}

	_, ipnet, err := net.ParseCIDR(subnet.Spec.CIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CIDR %s from subnet %s: %w", subnet.Spec.CIDR, subnetID, err)
	}

	return ipnet.Mask, nil
}

// getIPv6SubnetMaskFromENI gets subnet mask for IPv6 from ENI spec and Subnet CR
// Returns error instead of nil to provide better error handling for callers
func (ec *eniLink) getIPv6SubnetMaskFromENI(ipAddress string) (net.IPMask, error) {
	if ec.eni == nil {
		return nil, fmt.Errorf("ENI object is nil")
	}

	// Use ENI's subnet ID directly
	subnetID := ec.eni.Spec.ENI.SubnetID

	if subnetID == "" {
		return nil, fmt.Errorf("ENI subnet ID is empty")
	}

	// Get Subnet CR from cluster
	subnet, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(subnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet CR %s for IPv6 %s: %w", subnetID, ipAddress, err)
	}

	// Parse IPv6 CIDR from Subnet CR spec
	if subnet.Spec.IPv6CIDR == "" {
		return nil, fmt.Errorf("subnet %s has empty IPv6 CIDR", subnetID)
	}

	_, ipnet, err := net.ParseCIDR(subnet.Spec.IPv6CIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse IPv6 CIDR %s from subnet %s: %w", subnet.Spec.IPv6CIDR, subnetID, err)
	}

	return ipnet.Mask, nil
}

// getENICorrectConfigFromMetadata retrieves the correct ENI configuration from metadata API
// Only retrieves primary IP configuration, secondary IPs are managed by IPAM
func getENICorrectConfigFromMetadata(macAddress string, eni *ccev2.ENI) (*eniCorrectConfig, error) {
	config := &eniCorrectConfig{}

	// Get primary IP only (we don't manage secondary IPs)
	primaryIP, err := metadata.DefaultMetaClient.GetLinkPrimaryIP(macAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get primary IP from metadata: %w", err)
	}
	config.PrimaryIPv4 = primaryIP

	// Get gateway for primary IP
	gateway, err := metadata.DefaultMetaClient.GetLinkGateway(macAddress, primaryIP)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway from metadata: %w", err)
	}
	config.Gateway = gateway

	// Get IPv6 primary IP if enabled
	if eni != nil && option.Config.EnableIPv6 && len(eni.Spec.ENI.IPV6PrivateIPSet) > 0 {
		for _, ipv6IP := range eni.Spec.ENI.IPV6PrivateIPSet {
			if ipv6IP.Primary {
				config.PrimaryIPv6 = ipv6IP.PrivateIPAddress
				break // Only need the primary IPv6
			}
		}
	}

	// Get MTU from metadata API (authoritative source)
	mtu, err := metadata.DefaultMetaClient.GetLinkMTU(macAddress)
	if err != nil {
		// If MTU is not available from metadata, use default
		config.MTU = defaultMTUforENI
		logrus.WithError(err).WithField("macAddress", macAddress).Warning("Failed to get MTU from metadata, using default 1500")
	} else {
		config.MTU = mtu
		logrus.WithFields(logrus.Fields{
			"macAddress": macAddress,
			"mtu":        mtu,
		}).Debug("Retrieved MTU from metadata API")
	}

	return config, nil
}

var (
	// eniIndexMutex protects the ENI index allocation to prevent race conditions
	// when multiple goroutines are trying to find available ENI indices simultaneously
	eniIndexMutex sync.Mutex
)

// findAvailableENIIndexLocked finds the first available ENI index.
// Caller must hold eniIndexMutex.
func findAvailableENIIndexLocked() (int, error) {
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

// findAvailableENIIndex finds the first available ENI index in a thread-safe manner
// This function prevents race conditions when multiple goroutines are trying to
// allocate ENI indices simultaneously
func findAvailableENIIndex() (int, error) {
	eniIndexMutex.Lock()
	defer eniIndexMutex.Unlock()
	return findAvailableENIIndexLocked()
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
	var cceName string
	if !strings.HasPrefix(udevName, namePrefix) {
		if !isPrimary {
			// Keep index allocation and LinkSetName in one critical section.
			// Otherwise two concurrent goroutines can allocate the same index
			// before either interface is actually renamed.
			eniIndexMutex.Lock()
			defer eniIndexMutex.Unlock()

			availableIndex, err := findAvailableENIIndexLocked()
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
		err = ec.release.HostOS().EnsureNetworkManagerUnmanaged(cceName)
		if err != nil {
			return fmt.Errorf("failed to ensure NetworkManager stops managing %s: %v", cceName, err)
		}
	} else {
		// Interface already has the correct prefix, use current name
		cceName = elink.Attrs().Name
	}

	// Ensure NetworkManager stops managing the renamed interface
	// Only execute when status is ErrorOnNode
	if ec.eni != nil && ec.eni.Status.CCEStatus == ccev2.ENIStatusErrorOnNode {
		err = ec.release.HostOS().EnsureNetworkManagerUnmanaged(cceName)
		if err != nil {
			return fmt.Errorf("failed to ensure NetworkManager stops managing %s: %v", cceName, err)
		}
	}

	// Disable IPv6 for the interface if IPv6 is not enabled in configuration
	if !option.Config.EnableIPv6 {
		disableIPv6Key := fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6", cceName)
		if err = sysctl.SetIfNeededFunc(disableIPv6Key, "1"); err != nil {
			return fmt.Errorf("failed to disable IPv6 for interface %s: %v", cceName, err)
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
	// 0. validate ENI configuration and clean up conflicts
	err = ec.ensureENIConfiguration()
	if err != nil {
		return err
	}

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
	gateway, err := metadata.DefaultMetaClient.GetLinkGateway(mac, ip.String())
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

// validateENIConfiguration validates that the ENI has the correct configuration
// and cleans up any conflicting NetworkManager configurations (checks periodically and handles NetworkManager restarts)
func (ec *eniLink) ensureENIConfiguration() error {
	eniID := ec.eni.Spec.ENI.ID

	// Get or create ENI configuration state using MAC address as primary identifier
	// MAC address is unique and persistent across renames and reboots
	state, err := getOrCreateENIConfigState(eniID, ec.macAddr, ec.eni)
	if err != nil {
		ec.log.WithError(err).Error("Failed to get ENI configuration state")
		return err
	}

	// Check and fix network configuration if need
	if err := ec.checkAndFixNetworkConfiguration(state); err != nil {
		ec.log.WithError(err).Warning("Failed to check and fix network configuration")
		return err
	}

	ec.log.WithField("eniID", eniID).Debug("ENI configuration validation completed")
	return nil
}
