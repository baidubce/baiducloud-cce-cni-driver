package link

import (
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/sysctl"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

func DetectDefaultRouteInterfaceName() (string, error) {
	link, err := DetectDefaultRouteInterface()
	if err != nil {
		return "", err
	}

	return link.Attrs().Name, nil
}

func DetectDefaultRouteInterface() (netlink.Link, error) {
	routeToDstIP, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, err
	}

	for _, v := range routeToDstIP {
		if v.Dst == nil || v.Dst.IP.IsUnspecified() {
			l, err := netlink.LinkByIndex(v.LinkIndex)
			if err != nil {
				return nil, err
			}
			return l, nil
		}
	}
	return nil, fmt.Errorf("no default route interface found")
}

// DisableRpFilter tries to disable rpfilter on specified interface
func DisableRpFilter(ifName string) error {
	if val, err := sysctl.Read(fmt.Sprintf("net.ipv4.conf.%s.rp_filter", ifName)); err != nil {
		return fmt.Errorf("Unable to read net.ipv4.conf.%s.rp_filter: %s. Ignoring the check",
			ifName, err)
	} else {
		if val == "1" {
			return sysctl.Disable(fmt.Sprintf("net.ipv4.conf.%s.rp_filter", ifName))
		}
	}
	return nil
}

// FindENILinkByMac the MAC address inputted will return the corresponding netlink.Link network equipment.
func FindENILinkByMac(macAddress string) (netlink.Link, error) {
	var eniIntf netlink.Link
	// list all interfaces, and find ENI by mac address
	interfaces, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %v", interfaces)
	}

	for _, intf := range interfaces {
		if intf.Attrs().HardwareAddr.String() == macAddress {
			eniIntf = intf
			break
		}
	}

	if eniIntf == nil {
		return nil, fmt.Errorf("eni with mac address %v not found", macAddress)
	}

	return eniIntf, nil
}

// MoveAndRenameLink Reename the network equipment after moving to target namespace
func MoveAndRenameLink(dev netlink.Link, targetNs ns.NetNS, devName string) (err error) {
	ifName := dev.Attrs().Name
	// Devices can be renamed only when down
	if err = netlink.LinkSetDown(dev); err != nil {
		return fmt.Errorf("failed to set %q down: %v", ifName, err)
	}

	defer func() {
		// If moving the device to the host namespace fails, set its name back to ifName so that this
		// function can be retried. Also bring the device back up, unless it was already down before.
		if err != nil {
			_ = netlink.LinkSetName(dev, ifName)
			if dev.Attrs().Flags&net.FlagUp == net.FlagUp {
				_ = netlink.LinkSetUp(dev)
			}
		}
	}()

	// Rename the device to its original name from the host namespace
	if err = netlink.LinkSetName(dev, devName); err != nil {
		return fmt.Errorf("failed to restore %q to original name %q: %v", ifName, dev.Attrs().Alias, err)
	}

	if err = netlink.LinkSetNsFd(dev, int(targetNs.Fd())); err != nil {
		return fmt.Errorf("failed to move %q to host netns: %v", dev.Attrs().Alias, err)
	}
	return nil
}

func EnsureLinkAddr(addr *netlink.Addr, dev netlink.Link) error {
	family := netlink.FAMILY_V4
	if addr.IP.To4() == nil {
		family = netlink.FAMILY_V6
	}
	addrs, err := netlink.AddrList(dev, family)
	if err != nil {
		return err
	}
	for i := 0; i < len(addrs); i++ {
		if !addrs[i].IP.IsGlobalUnicast() {
			continue
		}
		// if the address already exists, return
		if addrs[i].IP.Equal(addr.IP) {
			return nil
		}
	}

	return netlink.AddrReplace(dev, addr)
}

func ReplaceRoute(routes []netlink.Route) error {
	if len(routes) == 0 {
		return nil
	}
	var noReplace = make(map[string]netlink.Route)
	link, err := netlink.LinkByIndex(routes[0].LinkIndex)
	if err != nil {
		return fmt.Errorf("ReplaceRoute failed to get link by index %v: %v", routes[0].LinkIndex, err)
	}
	exsitsRoutes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("ReplaceRoute failed to list routes: %v", err)
	}
	for _, rt := range routes {
		for _, exsitsRt := range exsitsRoutes {
			if exsitsRt.Equal(rt) {
				noReplace[rt.String()] = rt
				break
			}
		}
	}

	for _, rt := range routes {
		if _, ok := noReplace[rt.String()]; !ok {
			err := netlink.RouteReplace(&rt)
			if err != nil {
				return fmt.Errorf("failed to replace route %v: %v", rt, err)
			}
		}
	}
	return nil
}
