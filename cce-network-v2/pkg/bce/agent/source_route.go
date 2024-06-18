package agent

import (
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/set"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/sysctl"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	toENISecondaryIPRulePriority   = 512
	fromENISecondaryIPRulePriority = 1025

	mainRouteTableID          = 254
	defaultRouteTableIDOffset = 127
)

// ensureENIRule install source based routing rule offset
func ensureENIRule(scopedLog *logrus.Entry, eni *ccev2.ENI) error {
	// install source based routing rule offset
	var (
		tableID         = eni.Status.ENIIndex + eni.Spec.RouteTableOffset
		fromRulePrority = tableID + fromENISecondaryIPRulePriority
		toRulePrority   = tableID + toENISecondaryIPRulePriority

		allSourceIPs           []string
		alreadyExistedFromRule = make(map[string]netlink.Rule)
		alreadyExistedToRule   = make(map[string]netlink.Rule)
	)
	for _, v := range eni.Spec.PrivateIPSet {
		allSourceIPs = append(allSourceIPs, v.PrivateIPAddress)
		if v.PublicIPAddress != "" {
			allSourceIPs = append(allSourceIPs, v.PublicIPAddress)
		}
	}
	for _, v := range eni.Spec.IPV6PrivateIPSet {
		allSourceIPs = append(allSourceIPs, v.PrivateIPAddress)
		if v.PublicIPAddress != "" {
			allSourceIPs = append(allSourceIPs, v.PublicIPAddress)
		}
	}
	if len(allSourceIPs) == 0 {
		return nil
	}

	// install source based routing
	// Get a list of rules and routes ready.
	rules, err := netlink.RuleList(netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list all rules: %w", err)
	}
	for _, r := range rules {
		var key string
		// which table the rule belongs to
		var ruleTable = tableID
		fromFlow := true
		// source based routing rule is formatted as: 192.168.4.2/32

		if r.Src != nil && r.Src.IP != nil &&
			!r.Src.IP.Equal(net.IPv4zero) &&
			!r.Src.IP.Equal(net.IPv6zero) &&
			r.Priority == fromRulePrority {
			key = r.Src.IP.String()
			if r.Table == ruleTable {
				alreadyExistedFromRule[key] = r
			}
		} else if r.Dst != nil && r.Dst.IP != nil &&
			!r.Dst.IP.Equal(net.IPv4zero) &&
			!r.Dst.IP.Equal(net.IPv6zero) &&
			r.Priority == toRulePrority {
			fromFlow = false
			key = r.Dst.IP.String()
			ruleTable = mainRouteTableID
			if r.Table == ruleTable {
				alreadyExistedToRule[key] = r
			}
		}
		if key == "" {
			continue
		}

		// clean up old rules for the eni
		if set.SlinceContains(allSourceIPs, key) {
			if r.Table != ruleTable ||
				(fromFlow && r.Priority != fromRulePrority) ||
				(!fromFlow && r.Priority != toRulePrority) {
				// if the rule is not belong to the table, replace the table id
				netlink.RuleDel(&r)
				scopedLog.WithFields(logrus.Fields{
					"rule": logfields.Repr(r),
				}).Infof("rule not belong to table %d, delete the rule", tableID)

			}
			continue
		} else if (fromFlow && r.Priority == fromRulePrority) ||
			(!fromFlow && r.Priority == toRulePrority) {
			// if the rule is belong to the eni, delete the rule
			// Eni's secondary IP is constantly dynamically updated
			netlink.RuleDel(&r)
			scopedLog.WithFields(logrus.Fields{
				"rule": logfields.Repr(r),
			}).Infof("rule not belong to eni yet, delete the rule")
			continue
		}
		// delete old rules which not belong to the eni
		if r.Table == ruleTable && ruleTable != mainRouteTableID {
			if err = netlink.RuleDel(&r); err != nil {
				return fmt.Errorf("failed to delete rule: %w", err)
			}
			scopedLog.WithFields(logrus.Fields{
				"rule": logfields.Repr(r),
			}).Infof("delete rule for source based routing success")
		}
	}

	// add new rules for source based routing
	for _, ip := range allSourceIPs {
		if _, ok := alreadyExistedFromRule[ip]; !ok {
			addRule(scopedLog, ip, tableID, true, fromRulePrority)
		}
		if _, ok := alreadyExistedToRule[ip]; !ok {
			addRule(scopedLog, ip, mainRouteTableID, false, toRulePrority)
		}
	}
	return nil
}

func ensureENINDPProxy(scopedLog *logrus.Entry, eni *ccev2.ENI) error {
	elink, err := link.FindENILinkByMac(eni.Spec.ENI.MacAddress)
	if err != nil {
		return err
	}
	linkName := elink.Attrs().Name

	proxys, err := netlink.NeighProxyList(elink.Attrs().Index, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list all proxy: %w", err)
	}
	ipv6Proxy := make(map[string]netlink.Neigh)

	for _, proxy := range proxys {
		if proxy.IP.To4() == nil {
			ipv6Proxy[proxy.IP.String()] = proxy
		}
	}

	// enable ndproxy
	if len(eni.Spec.IPV6PrivateIPSet) > 0 {
		proxyNDPPath := fmt.Sprintf("net.ipv6.conf.%s.proxy_ndp", linkName)
		value, err := sysctl.Read(proxyNDPPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", proxyNDPPath, err)
		}
		if value != "1" {
			if err := sysctl.Enable(proxyNDPPath); err != nil {
				return fmt.Errorf("failed to set %s: %w", proxyNDPPath, err)
			}
		}
	}

	// add new proxy ndp
	for _, privateIPAddress := range eni.Spec.IPV6PrivateIPSet {
		if privateIPAddress.Primary {
			continue
		}
		// add ndp proxy
		neigh := &netlink.Neigh{
			LinkIndex: elink.Attrs().Index,
			IP:        net.ParseIP(privateIPAddress.PrivateIPAddress),
			Flags:     netlink.NTF_PROXY,
		}
		if _, ok := ipv6Proxy[privateIPAddress.PrivateIPAddress]; !ok {
			if err := netlink.NeighAdd(neigh); err != nil {
				scopedLog.Warningf("failed to ip neigh add proxy %s dev %s: %v", privateIPAddress.PrivateIPAddress, elink.Attrs().Name, err)
			}
			scopedLog.WithFields(logrus.Fields{
				"ip": privateIPAddress.PrivateIPAddress,
			}).Infof("add proxy success")
		}
	}
	return nil
}

func ensureENIArpProxy(scopedLog *logrus.Entry, eni *ccev2.ENI) error {
	elink, err := link.FindENILinkByMac(eni.Spec.ENI.MacAddress)
	if err != nil {
		return err
	}
	linkName := elink.Attrs().Name
	err = sysctl.CompareAndSet(fmt.Sprintf("net.ipv4.conf.%s.proxy_arp", linkName), "0", "1")
	if err != nil {
		return fmt.Errorf("failed to set net.ipv4.conf.%s.proxy_arp: %w", linkName, err)
	}
	return nil
}

func addRule(scopedLog *logrus.Entry, ip string, table int, from bool, priority int) {
	// Source must be restricted to a single IP, not a full subnet
	var (
		src  net.IPNet
		rule = netlink.NewRule()
	)
	src.IP = net.ParseIP(ip)
	if src.IP.To4() != nil {
		src.Mask = net.CIDRMask(32, 32)
	} else {
		src.Mask = net.CIDRMask(128, 128)
	}
	rule.Table = table
	rule.Priority = priority
	if from {
		rule.Src = &src
	} else {
		rule.Dst = &src
	}

	if err := netlink.RuleAdd(rule); err != nil {
		scopedLog.WithFields(logrus.Fields{
			"rule": logfields.Repr(rule),
		}).Infof("add rule for source based routing failed")
		return
	}
	scopedLog.WithFields(logrus.Fields{
		"rule": logfields.Repr(rule),
	}).Infof("add rule for source based routing success")
}
