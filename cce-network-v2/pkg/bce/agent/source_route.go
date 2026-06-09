package agent

import (
	"fmt"
	"net"
	"os"
	"strings"

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
					"type": "ENI",
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
				"type": "ENI",
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
				"type": "ENI",
				"rule": logfields.Repr(r),
			}).Infof("delete rule for source based routing success")
		}
	}

	// add new rules for source based routing
	for _, ip := range allSourceIPs {
		if _, ok := alreadyExistedFromRule[ip]; !ok {
			if err := addRule(scopedLog, ip, tableID, true, fromRulePrority); err != nil {
				return fmt.Errorf("failed to add from rule for IP %s: %w", ip, err)
			}
		}
		if _, ok := alreadyExistedToRule[ip]; !ok {
			if err := addRule(scopedLog, ip, mainRouteTableID, false, toRulePrority); err != nil {
				return fmt.Errorf("failed to add to rule for IP %s: %w", ip, err)
			}
		}
	}
	return nil
}

// ensureERIRule install source based routing rule offset
func ensureERIRule(scopedLog *logrus.Entry, eni *ccev2.ENI) error {
	// install source based routing rule offset
	var (
		tableID         = eni.Status.ENIIndex + eni.Spec.RouteTableOffset
		fromRulePrority = tableID + fromENISecondaryIPRulePriority
		toRulePrority   = tableID + toENISecondaryIPRulePriority

		allSourceIPs           []string
		alreadyExistedFromRule = make(map[string]netlink.Rule)
		alreadyExistedToRule   = make(map[string]netlink.Rule)
	)

	// Only process primary IP, skip non-primary private IP rules
	for _, v := range eni.Spec.PrivateIPSet {
		if v.Primary {
			allSourceIPs = append(allSourceIPs, v.PrivateIPAddress)
			if v.PublicIPAddress != "" {
				allSourceIPs = append(allSourceIPs, v.PublicIPAddress)
			}
		}
	}
	for _, v := range eni.Spec.IPV6PrivateIPSet {
		if v.Primary {
			allSourceIPs = append(allSourceIPs, v.PrivateIPAddress)
			if v.PublicIPAddress != "" {
				allSourceIPs = append(allSourceIPs, v.PublicIPAddress)
			}
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

		if set.SlinceContains(allSourceIPs, key) {
			if r.Table != ruleTable ||
				(fromFlow && r.Priority != fromRulePrority) ||
				(!fromFlow && r.Priority != toRulePrority) {
				// if the rule is not belong to the table, replace the table id
				netlink.RuleDel(&r)
				scopedLog.WithFields(logrus.Fields{
					"type": "ERI",
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
				"type": "ERI",
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
				"type": "ERI",
				"rule": logfields.Repr(r),
			}).Infof("delete rule for source based routing success")
		}
	}

	// add new rules for source based routing
	for _, ip := range allSourceIPs {
		if _, ok := alreadyExistedFromRule[ip]; !ok {
			if err := addRule(scopedLog, ip, tableID, true, fromRulePrority); err != nil {
				return fmt.Errorf("failed to add from rule for IP %s: %w", ip, err)
			}
		}
		if _, ok := alreadyExistedToRule[ip]; !ok {
			if err := addRule(scopedLog, ip, mainRouteTableID, false, toRulePrority); err != nil {
				return fmt.Errorf("failed to add to rule for IP %s: %w", ip, err)
			}
		}
	}

	// Add additional from all oif <interface_name> lookup rt_<interface_name> rule
	elink, err := link.FindENILinkByMac(eni.Spec.ENI.MacAddress)
	if err != nil {
		return fmt.Errorf("failed to find ENI link by mac: %w", err)
	}
	linkName := elink.Attrs().Name

	// Check if from all oif rule already exists and clean up old cce-eri rules
	oifRuleExists, err := cleanupOldCCEERIRulesAndCheckOifRule(scopedLog, rules, linkName, tableID, fromRulePrority)
	if err != nil {
		return err
	}

	// Add from all oif rule if it doesn't exist
	if !oifRuleExists {
		rule := netlink.NewRule()
		rule.Table = tableID
		rule.Priority = fromRulePrority
		rule.OifName = linkName

		if err := netlink.RuleAdd(rule); err != nil {
			scopedLog.WithFields(logrus.Fields{
				"type": "ERI",
				"rule": logfields.Repr(rule),
			}).Errorf("add from all oif rule for ERI failed: %v", err)
			return fmt.Errorf("failed to add from all oif rule for ERI: %w", err)
		}
		scopedLog.WithFields(logrus.Fields{
			"type": "ERI",
			"rule": logfields.Repr(rule),
		}).Infof("add from all oif rule for ERI success")
	}

	return nil
}

// isOldCCEERIRuleToDelete checks if a rule is an old cce-eri rule that should be deleted
func isOldCCEERIRuleToDelete(r netlink.Rule, linkName string, tableID int, fromRulePriority int) bool {
	return r.OifName != "" && strings.HasPrefix(r.OifName, "cce-eri") &&
		(r.OifName != linkName || r.Table != tableID || r.Priority != fromRulePriority)
}

// cleanupOldCCEERIRulesAndCheckOifRule cleans up old cce-eri rules that don't match current conditions
// and checks if the oif rule already exists
func cleanupOldCCEERIRulesAndCheckOifRule(scopedLog *logrus.Entry, rules []netlink.Rule, linkName string, tableID int, fromRulePrority int) (bool, error) {
	oifRuleExists := false
	for _, r := range rules {
		if isOldCCEERIRuleToDelete(r, linkName, tableID, fromRulePrority) {
			if err := netlink.RuleDel(&r); err != nil {
				scopedLog.WithFields(logrus.Fields{
					"type": "ERI",
					"rule": logfields.Repr(r),
				}).Errorf("failed to delete old cce-eri rule: %v", err)
				return false, fmt.Errorf("failed to delete old cce-eri rule: %w", err)
			}
			scopedLog.WithFields(logrus.Fields{
				"type": "ERI",
				"rule": logfields.Repr(r),
			}).Infof("deleted old cce-eri rule that doesn't match current conditions")
			continue
		}

		if r.OifName == linkName && r.Table == tableID && r.Priority == fromRulePrority {
			oifRuleExists = true
			break
		}
	}
	return oifRuleExists, nil
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

func ensureENIArpProxy(scopedLog *logrus.Entry, macAddr string) error {
	elink, err := link.FindENILinkByMac(macAddr)
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

func addRule(scopedLog *logrus.Entry, ip string, table int, from bool, priority int) error {
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
		if os.IsExist(err) {
			return ensureRuleExists(scopedLog, rule, &src, table, from, priority)
		}
		// Other errors
		scopedLog.WithFields(logrus.Fields{
			"rule": logfields.Repr(rule),
		}).Errorf("add rule for source based routing failed: %v", err)
		return fmt.Errorf("failed to add rule for source based routing: %w", err)
	}
	scopedLog.WithFields(logrus.Fields{
		"rule": logfields.Repr(rule),
	}).Infof("add rule for source based routing success")
	return nil
}

func ensureRuleExists(scopedLog *logrus.Entry, rule *netlink.Rule, src *net.IPNet, table int, from bool, priority int) error {
	// Verify that an identical rule already exists using RuleListFiltered
	family, filter, mask := buildFilterArgs(src, table, from, priority)
	existing, listErr := netlink.RuleListFiltered(family, filter, mask)
	if listErr != nil {
		scopedLog.WithFields(logrus.Fields{
			"rule": logfields.Repr(rule),
		}).Errorf("rule exists but failed to verify with RuleListFiltered: %v", listErr)
		return fmt.Errorf("rule exists but failed to verify with RuleListFiltered: %w", listErr)
	}
	for _, r := range existing {
		if r.Table == table && r.Priority == priority {
			if (from && r.Src != nil && r.Src.String() == src.String()) || (!from && r.Dst != nil && r.Dst.String() == src.String()) {
				scopedLog.WithFields(logrus.Fields{
					"rule": logfields.Repr(rule),
				}).Debugf("rule already exists and matches, skipping add")
				return nil
			}
		}
	}
	// No identical rule found
	scopedLog.WithFields(logrus.Fields{
		"rule": logfields.Repr(rule),
	}).Errorf("rule add reported exist but no identical rule found")
	return fmt.Errorf("rule add reported exist but no identical rule found")
}

func buildFilterArgs(src *net.IPNet, table int, from bool, priority int) (int, *netlink.Rule, uint64) {
	filter := netlink.NewRule()
	filter.Table = table
	filter.Priority = priority
	if from {
		filter.Src = src
	} else {
		filter.Dst = src
	}
	family := netlink.FAMILY_V4
	if src.IP.To4() == nil {
		family = netlink.FAMILY_V6
	}
	mask := netlink.RT_FILTER_TABLE | netlink.RT_FILTER_PRIORITY
	if from {
		mask |= netlink.RT_FILTER_SRC
	} else {
		mask |= netlink.RT_FILTER_DST
	}
	return family, filter, mask
}
