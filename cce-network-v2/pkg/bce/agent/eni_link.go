package agent

import (
	"fmt"
	"net"
	"strconv"
	"strings"
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

	// maxENIIndex max ENI index, this is the max number of ENI that can be attached to a node
	// it is also the max route table for a node
	maxENIIndex = 252
)

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

	linkList, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list links: %v", err)
	}

	udevName := elink.Attrs().Name
	// rename link to cce-eni-{index}
	if !strings.HasPrefix(udevName, ENINamePrefix) {
		var cceName = fmt.Sprintf("%s-%d", ENINamePrefix, eniIndex)
		if !isPrimary {
			// find a free index for eni
			for i := 0; i < maxENIIndex; i++ {
				findENI := false
				cceName = fmt.Sprintf("%s-%d", ENINamePrefix, i)
				for _, link := range linkList {
					if link.Attrs().Name == cceName {
						findENI = true
						break
					}
				}
				if !findENI {
					eniIndex = i
					break
				}
			}
		}

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
	}

	if strings.HasPrefix(elink.Attrs().Name, ENINamePrefix) {
		nameAndIndex := strings.Split(elink.Attrs().Name, ENINamePrefix+"-")
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

	// 3. add primary IP
	err = ec.ensureENIAddr()
	if err != nil {
		return err
	}

	// 4. disable rtfilter
	err = ec.disableRPFCheck()
	if err != nil {
		return err
	}

	// 5. set from promary ip rule route with table 127 + index
	err = ec.ensureFromPrimaryRoute()
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
			link.EnsureLinkAddr(addr, ec.link)
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
		if err = setAddr(subnet.Spec.IPv6CIDR, ec.eni.Spec.ENI.IPV6PrivateIPSet, netlink.FAMILY_V6); err != nil {
			return err
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
			ec.log.Errorf("ipv6 failed to ensure route for eni from primary route: %v", err)
			return err
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
