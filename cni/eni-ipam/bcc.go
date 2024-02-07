package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"

	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/vishvananda/netlink"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
)

type bccENIMultiIP struct {
	name      string
	namespace string
	netlink   netlinkwrapper.Interface
}

var _ networkClient = &bccENIMultiIP{}

func (client *bccENIMultiIP) SetupNetwork(
	ctx context.Context,
	result *current.Result,
	ipamConf *IPAMConf,
	resp *rpc.AllocateIPReply,
) error {
	allocRespNetworkInfo := resp.GetENIMultiIP()

	if allocRespNetworkInfo == nil {
		err := errors.New(fmt.Sprintf("failed to allocate IP for pod (%v %v): NetworkInfo is nil", client.namespace, client.name))
		log.Errorf(ctx, err.Error())
		return err
	}

	log.Infof(ctx, "allocate IP %v for pod(%v %v) successfully", allocRespNetworkInfo.IP, client.namespace, client.name)

	allocatedIP := net.ParseIP(allocRespNetworkInfo.IP)
	if allocatedIP == nil {
		return fmt.Errorf("alloc IP %v format error", allocRespNetworkInfo.IP)
	}
	version := "4"
	addrBits := 32
	if allocatedIP.To4() == nil {
		version = "6"
		addrBits = 128
	}

	result.IPs = []*current.IPConfig{
		{
			Version: version,
			Address: net.IPNet{IP: allocatedIP, Mask: net.CIDRMask(addrBits, addrBits)},
		},
	}

	netutil := networkutil.New()
	eniIntf, err := netutil.GetLinkByMacAddress(allocRespNetworkInfo.Mac)
	if err != nil {
		log.Errorf(ctx, "failed to get eni interface by mac: %v", err)
		return err
	}

	log.Infof(ctx, "pod (%v %v) with IP %v is subject to interface: %v", client.namespace, client.name,
		allocRespNetworkInfo.IP, eniIntf.Attrs().Name)

	ethIndex, err := getENIInterfaceIndex(ipamConf.ENILinkPrefix, eniIntf)
	if err != nil {
		return err
	}

	rtTable := ipamConf.RouteTableIDOffset + ethIndex

	// add to/from ip rule
	// ip rule add from all to <pod> table main
	// ip rule add from <pod> table XXX

	allocIPNet := &net.IPNet{IP: allocatedIP, Mask: net.CIDRMask(addrBits, addrBits)}
	// clean up old rule
	_ = client.delToOrFromContainerRule(true, allocIPNet)
	_ = client.delToOrFromContainerRule(false, allocIPNet)

	// add to container rule
	err = client.addToOrFromContainerRule(true, allocIPNet, toContainerRulePriority, mainRouteTableID)
	if err != nil {
		msg := fmt.Sprintf("failed to add to-container rule: %v", err)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	// add from container rule
	err = client.addToOrFromContainerRule(false, allocIPNet, fromContainerRulePriority, rtTable)
	if err != nil {
		msg := fmt.Sprintf("failed to add from-container rule: %v", err)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	log.Infof(ctx, "add to/from ip rule for pod (%v %v) successfully", client.namespace, client.name)

	// del eni scope link route
	if ipamConf.DeleteENIScopeLinkRoute {
		_ = client.delScopeLinkRoute(eniIntf)
	}

	// check whether eni is working
	eniErr := client.validateEni(eniIntf, rtTable)
	if eniErr != nil {
		return fmt.Errorf("eni isn't working: %s", eniErr)
	}

	return nil
}

func (client *bccENIMultiIP) TeardownNetwork(
	ctx context.Context,
	ipamConf *IPAMConf,
	resp *rpc.ReleaseIPReply,
) error {
	releaseRespNetworkInfo := resp.GetENIMultiIP()

	if releaseRespNetworkInfo == nil {
		return nil
	}

	log.Infof(ctx, "release IP %v for pod(%v %v) successfully", releaseRespNetworkInfo.IP, client.namespace, client.name)

	releaseIP := net.ParseIP(releaseRespNetworkInfo.IP)
	if releaseIP == nil {
		return fmt.Errorf("release IP %v format error", releaseRespNetworkInfo.IP)
	}
	addrBits := 32
	if releaseIP.To4() == nil {
		addrBits = 128
	}
	releaseIPNet := &net.IPNet{IP: releaseIP, Mask: net.CIDRMask(addrBits, addrBits)}

	err := client.delToOrFromContainerRule(true, releaseIPNet)
	if err != nil && !netlinkwrapper.IsNotExistError(err) {
		msg := fmt.Sprintf("failed to delete to-container rule: %v", err)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	err = client.delToOrFromContainerRule(false, releaseIPNet)
	if err != nil && !netlinkwrapper.IsNotExistError(err) {
		msg := fmt.Sprintf("failed to delete from-container rule: %v", err)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	log.Infof(ctx, "delete to/from ip rule for pod (%v %v) successfully", client.namespace, client.name)

	return nil
}

func getENIInterfaceIndex(linkPrefix string, intf netlink.Link) (int, error) {
	eniNameMatcher := regexp.MustCompile(fmt.Sprintf("%s(\\d+)", linkPrefix))
	matches := eniNameMatcher.FindStringSubmatch(intf.Attrs().Name)
	if len(matches) != 2 {
		return -1, fmt.Errorf("invalid interface name: %v", intf.Attrs().Name)
	}
	index, err := strconv.ParseInt(matches[1], 10, 32)
	if err != nil {
		return -1, fmt.Errorf("error parsing interface link index: %v", err)
	}
	return int(index), nil
}

func (client *bccENIMultiIP) delScopeLinkRoute(intf netlink.Link) error {
	addrs, err := client.netlink.AddrList(intf, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		dst := net.IPNet{
			IP:   addr.IP.Mask(addr.Mask),
			Mask: addr.Mask,
		}
		err = client.netlink.RouteDel(&netlink.Route{
			Dst:       &dst,
			Scope:     netlink.SCOPE_LINK,
			LinkIndex: intf.Attrs().Index,
		})
		if err != nil && !netlinkwrapper.IsNotExistError(err) {
			return err
		}
	}

	return nil
}

func (client *bccENIMultiIP) addToOrFromContainerRule(isToContainer bool, addr *net.IPNet, priority int, rtTable int) error {
	rule := netlink.NewRule()
	rule.Table = rtTable
	rule.Priority = priority

	if isToContainer {
		rule.Dst = addr // ip rule add from all to `addr` lookup `table` prio `xxx`
	} else {
		rule.Src = addr // ip rule add from `addr` lookup `table` prio `xxx`
	}

	err := client.netlink.RuleDel(rule)
	if err != nil && !netlinkwrapper.IsNotExistError(err) {
		return err
	}

	if err := client.netlink.RuleAdd(rule); err != nil {
		return err
	}
	return nil
}

func (client *bccENIMultiIP) delToOrFromContainerRule(isToContainer bool, addr *net.IPNet) error {
	rule := netlink.NewRule()

	if isToContainer {
		rule.Dst = addr // ip rule add from all to `addr` lookup `table` prio `xxx`
	} else {
		rule.Src = addr // ip rule add from `addr` lookup `table` prio `xxx`
	}

	err := client.netlink.RuleDel(rule)
	if err != nil && !netlinkwrapper.IsNotExistError(err) {
		return err
	}

	return nil
}

func (client *bccENIMultiIP) validateEni(eniIntf netlink.Link, rtTable int) error {
	if err := client.validateEniRoute(eniIntf, rtTable); err != nil {
		return err
	}

	if err := client.validateEniIP(eniIntf); err != nil {
		return err
	}

	return nil
}

func (client *bccENIMultiIP) validateEniRoute(eniIntf netlink.Link, rtTable int) error {
	filter := &netlink.Route{
		LinkIndex: eniIntf.Attrs().Index,
		Table:     rtTable,
	}
	routes, listErr := client.netlink.RouteListFiltered(netlink.FAMILY_V4, filter,
		netlink.RT_FILTER_TABLE|netlink.RT_FILTER_OIF)
	if listErr != nil {
		return fmt.Errorf("failed to list route of eni %s, linkIndex %d, table %d, err: %s",
			eniIntf.Attrs().Name, filter.LinkIndex, filter.Table, listErr)
	}

	if len(routes) == 0 {
		return fmt.Errorf("route table %d of eni %s not found", rtTable, eniIntf.Attrs().Name)
	}
	return nil
}

func (client *bccENIMultiIP) validateEniIP(eniIntf netlink.Link) error {
	eniAddressList, addrErr := client.netlink.AddrList(eniIntf, netlink.FAMILY_V4)
	if addrErr != nil {
		return fmt.Errorf("failed to list addr of eni %s, err: %s", eniIntf.Attrs().Name, addrErr)
	}

	if len(eniAddressList) == 0 {
		return fmt.Errorf("failed to get address of eni %s", eniIntf.Attrs().Name)
	}
	return nil
}
