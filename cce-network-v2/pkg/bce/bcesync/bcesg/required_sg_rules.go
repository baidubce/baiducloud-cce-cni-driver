package bcesg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/sirupsen/logrus"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	ccev2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
)

const (
	// pod ip cidr + vpc cidr
	IPRangeClusterCIDR = "clusterCIDR"
	//  vpc cidr
	IPRangeVPCCIDR = "vpcCIDR"

	Empty               = ""
	IPv4RangeAll        = "0.0.0.0/0"
	All                 = "all"
	IPv4FloatingIPRange = "100.64.230.0/24"
	IPv6RangeAll        = "::/0"

	ProtocolAll  = "all"
	ProtocolIPv4 = "IPv4"
	ProtocolIPv6 = "IPv6"
	ProtocolTCP  = "tcp"
	ProtocolUDP  = "udp"
	ProtocolICMP = "icmp"

	RemarkDefault              = "CCE默认规则: 节点间内网通信"
	RemarkFloatingIP           = "CCE默认规则: 与隐藏子网节点内网通信"
	RemarkEgressAllAllowed     = "CCE默认规则: 出向全通"
	RemarkNodePort             = "CCE默认规则: K8s NodePort 默认范围"
	RemarkIPv6Default          = "CCE默认规则: IPv6 节点间内网通信"
	RemarkIPv6EgressAllAllowed = "CCE默认规则: IPv6 出向全通"
	RemarkIPv6NodePort         = "CCE默认规则: K8s NodePort 默认范围"
)

var (
	ruleIPv4IngressAllowed = RequiredSGRule{
		EtherType:    ProtocolIPv4,
		Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
		Protocol:     ProtocolAll,
		SourceIP:     IPRangeClusterCIDR,
		DestIP:       IPv4RangeAll,
		PortRangeMin: 0,
		PortRangeMax: 65535,
		Remark:       RemarkDefault,
	}
	ruleEgresssAllowed = RequiredSGRule{
		EtherType:    ProtocolIPv4,
		Direction:    string(vpc.ACL_RULE_DIRECTION_EGRESS),
		Protocol:     ProtocolAll,
		PortRangeMin: 0,
		PortRangeMax: 65535,
		SourceIP:     Empty,
		DestIP:       All,
		Remark:       RemarkEgressAllAllowed,
	}

	ruleIPv4FloatingIPIngressAllowed = RequiredSGRule{
		EtherType:    ProtocolIPv4,
		Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
		Protocol:     ProtocolAll,
		SourceIP:     IPv4FloatingIPRange,
		DestIP:       IPv4RangeAll,
		PortRangeMin: 6443,
		PortRangeMax: 6443,
		Remark:       RemarkFloatingIP,
	}

	masterRequiredRule []RequiredSGRule = []RequiredSGRule{
		ruleIPv4FloatingIPIngressAllowed,
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolTCP,
			SourceIP:     IPRangeClusterCIDR,
			DestIP:       Empty,
			PortRangeMin: 6443,
			PortRangeMax: 6443,
			Remark:       "CCE默认规则: apiserver 访问 6443",
		},
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolTCP,
			SourceIP:     IPRangeClusterCIDR,
			DestIP:       Empty,
			PortRangeMin: 443,
			PortRangeMax: 443,
			Remark:       "CCE默认规则: apiserver 访问 443",
		},
	}
	masterOptionRule []RequiredSGRule = []RequiredSGRule{
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolTCP,
			SourceIP:     All,
			DestIP:       Empty,
			PortRangeMin: 6443,
			PortRangeMax: 6443,
			Remark:       "CCE可选规则: 开放 apiserver 公网访问",
		},
	}
	nodeRequiredRule []RequiredSGRule = []RequiredSGRule{
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolTCP,
			SourceIP:     IPRangeClusterCIDR,
			DestIP:       IPv4RangeAll,
			PortRangeMin: 10250,
			PortRangeMax: 10250,
			Remark:       "CCE默认规则: Kubelet 节点间通信 10250 端口",
		},
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolUDP,
			SourceIP:     IPv4FloatingIPRange,
			DestIP:       IPv4RangeAll,
			PortRangeMin: 30000,
			PortRangeMax: 32768,
			Remark:       RemarkNodePort,
		},
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolTCP,
			SourceIP:     IPv4FloatingIPRange,
			DestIP:       IPv4RangeAll,
			PortRangeMin: 30000,
			PortRangeMax: 32768,
			Remark:       RemarkNodePort,
		},
	}
	nodeOptionRule []RequiredSGRule = []RequiredSGRule{
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolTCP,
			SourceIP:     All,
			DestIP:       Empty,
			PortRangeMin: 22,
			PortRangeMax: 22,
			Remark:       "CCE可选规则: 公网 SSH 登录",
		},
		{
			EtherType:    ProtocolIPv4,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolICMP,
			SourceIP:     All,
			DestIP:       Empty,
			PortRangeMin: -1,
			PortRangeMax: -1,
			Remark:       "CCE可选规则: 公网 ping 命令",
		},
	}

	nodeIPv6OptionRule []RequiredSGRule = []RequiredSGRule{
		{
			EtherType:    ProtocolIPv6,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolICMP,
			SourceIP:     IPv6RangeAll,
			DestIP:       Empty,
			PortRangeMin: -1,
			PortRangeMax: -1,
			Remark:       "CCE可选规则: 公网 ping 命令",
		},
		{
			EtherType:    ProtocolIPv6,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolTCP,
			SourceIP:     Empty,
			DestIP:       Empty,
			PortRangeMin: 30000,
			PortRangeMax: 32768,
			Remark:       RemarkIPv6NodePort,
		},
		{
			EtherType:    ProtocolIPv6,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolUDP,
			SourceIP:     All,
			DestIP:       Empty,
			PortRangeMin: 30000,
			PortRangeMax: 32768,
			Remark:       RemarkIPv6NodePort,
		},
	}

	podRequiredRule []RequiredSGRule = []RequiredSGRule{
		ruleEgresssAllowed,
		ruleIPv4IngressAllowed,
	}

	podIPv6OptionRule []RequiredSGRule = []RequiredSGRule{
		{
			EtherType:    ProtocolIPv6,
			Direction:    string(vpc.ACL_RULE_DIRECTION_INGRESS),
			Protocol:     ProtocolAll,
			SourceIP:     IPRangeVPCCIDR,
			DestIP:       Empty,
			PortRangeMin: 1,
			PortRangeMax: 65535,
			Remark:       RemarkIPv6Default,
		},
		{
			EtherType:    ProtocolIPv6,
			Direction:    string(vpc.ACL_RULE_DIRECTION_EGRESS),
			Protocol:     ProtocolAll,
			SourceIP:     IPRangeVPCCIDR,
			DestIP:       All,
			PortRangeMin: 1,
			PortRangeMax: 65535,
			Remark:       RemarkIPv6EgressAllAllowed,
		},
	}
)

type RequiredSGRule struct {
	EtherType    string `json:"etherType,omitempty"`
	Direction    string `json:"direction,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	SourceIP     string `json:"sourceIp,omitempty"`
	DestIP       string `json:"destIp,omitempty"`
	PortRangeMin int    `json:"portRangeMin,omitempty"`
	PortRangeMax int    `json:"portRangeMax,omitempty"`
	Remark       string `json:"remark,omitempty"`
}

func (r *RequiredSGRule) matchSGPort(portRange string) bool {
	if isAnyAllStr(portRange) {
		return true
	}

	portMinAndMax := strings.Split(portRange, "-")
	if len(portMinAndMax) != 2 {
		portMin, err := strconv.Atoi(portMinAndMax[0])
		if err != nil {
			return false
		}
		if r.PortRangeMin == portMin && r.PortRangeMax == portMin {
			return true
		}
	} else {
		portMin, err := strconv.Atoi(portMinAndMax[0])
		if err != nil {
			return false
		}
		portMax, err := strconv.Atoi(portMinAndMax[1])
		if err != nil {
			return false
		}
		if portMin == 1 {
			portMin = 0
		}
		if r.PortRangeMin <= portMax && r.PortRangeMax >= portMin {
			return true
		}
	}
	return false
}

func (r *RequiredSGRule) isSubSGPort(portRange string) bool {
	if isAnyAllStr(portRange) {
		return true
	}

	portMinAndMax := strings.Split(portRange, "-")
	if len(portMinAndMax) != 2 {
		portMin, err := strconv.Atoi(portMinAndMax[0])
		if err != nil {
			return false
		}
		if r.PortRangeMin == portMin && r.PortRangeMax == portMin {
			return true
		}
	} else {
		portMin, err := strconv.Atoi(portMinAndMax[0])
		if err != nil {
			return false
		}
		portMax, err := strconv.Atoi(portMinAndMax[1])
		if err != nil {
			return false
		}
		if portMin == 1 {
			portMin = 0
		}
		if r.PortRangeMin >= portMin && r.PortRangeMax <= portMax {
			return true
		}
	}
	return false
}

// violateRules Determine if the existing security group has violated the preset rules
// Return:
// true: warning: the existing security group has violated the preset rules
//
//	false: the existing security group has not violated the preset rules
func (r *RequiredSGRule) violateRules(sgRules *sgRulesCollector) bool {
	var (
		directionAllowedRules []*bccapi.SecurityGroupRuleModel
		directionDropRules    []*bccapi.SecurityGroupRuleModel
	)
	if r.Direction == string(vpc.ACL_RULE_DIRECTION_EGRESS) {
		directionAllowedRules = sgRules.egressAllowedRules
		directionDropRules = sgRules.egressDropRules
	} else {
		directionAllowedRules = sgRules.ingressAllowedRules
		directionDropRules = sgRules.ingressDropRules
	}

	// 1. match any drop rules, packet will be dropped
	for _, rule := range directionDropRules {
		if rule.Ethertype != r.EtherType {
			continue
		}
		if !bceFuzzyMatchAny(r.Protocol, rule.Protocol) {
			continue
		}
		if !cidrFuzzyMatchAny(r.SourceIP, rule.SourceIp) {
			continue
		}
		if !cidrFuzzyMatchAny(r.DestIP, rule.DestIp) {
			continue
		}
		if r.matchSGPort(rule.PortRange) {
			return true
		}
	}

	// 2. match any allow rules, packet will be allowed
	for _, rule := range directionAllowedRules {
		if rule.Ethertype != r.EtherType {
			continue
		}
		if !isAnyAllStr(rule.Protocol) && !strings.EqualFold(r.Protocol, rule.Protocol) {
			continue
		}
		if !cidrFuzzyContains(rule.SourceIp, r.SourceIP) {
			continue
		}
		if !cidrFuzzyContains(rule.DestIp, r.DestIP) {
			continue
		}
		if r.isSubSGPort(rule.PortRange) {
			return false
		}
	}

	return true
}

type SecurityValidator struct {
	VpcCIDR            []string
	ClusterCIDR        []string
	EnableNodeOptCheck bool
	EnableIPv6Check    bool

	updater            syncer.SecurityGroupUpdater
	checkInterval      time.Duration
	ctl                *controller.Manager
	checkErrorCacheMap map[string]error
	lock               sync.Mutex

	lastErrorTime time.Time

	haveInit bool
}

var BceSecurityValidator *SecurityValidator

func initSecurityValidator(updater syncer.SecurityGroupUpdater, checkInterval time.Duration, vpcCIDR, clusterCIDR []string, enableNodeOptCheck, enableIPvtCheck bool) error {
	BceSecurityValidator = &SecurityValidator{
		updater:            updater,
		checkInterval:      checkInterval,
		ctl:                controller.NewManager(),
		VpcCIDR:            vpcCIDR,
		ClusterCIDR:        clusterCIDR,
		EnableNodeOptCheck: enableNodeOptCheck,
		EnableIPv6Check:    enableIPvtCheck,

		checkErrorCacheMap: make(map[string]error),
		lock:               sync.Mutex{},
		haveInit:           true,
	}

	controller.NewManager().UpdateController("security-validator-clean-error", controller.ControllerParams{
		DoFunc: func(ctx context.Context) error {
			BceSecurityValidator.lock.Lock()
			BceSecurityValidator.checkErrorCacheMap = make(map[string]error)
			BceSecurityValidator.lock.Unlock()
			return nil
		},
		RunInterval:     checkInterval,
		DisableDebugLog: true,
	})
	return nil
}

type SecurityCheckOpt struct {
	Indentifier string
	Role        ccev2alpha1.SecurityGroupUserRoles
	RuleIds     []string
}

// Violation of safety constraints
type SafetyConstraintsViolations struct {
	Remark  []string
	RuleIds []string
}

// Error implements error.
func (s *SafetyConstraintsViolations) Error() string {
	return fmt.Sprintf("Violations of safety constraints: RuleIDs: [%v], Remark: [%v]", s.RuleIds, s.Remark)
}

var _ error = &SafetyConstraintsViolations{}

type sgRulesCollector struct {
	egressAllowedRules  []*bccapi.SecurityGroupRuleModel
	egressDropRules     []*bccapi.SecurityGroupRuleModel
	ingressAllowedRules []*bccapi.SecurityGroupRuleModel
	ingressDropRules    []*bccapi.SecurityGroupRuleModel
}

func (bsv *SecurityValidator) ViolateSecurityRules(opts *SecurityCheckOpt) (err error, validate bool) {
	bsv.lock.Lock()
	if !bsv.haveInit {
		bsv.lock.Unlock()
		return nil, false
	}
	bsv.lock.Unlock()
	if len(opts.RuleIds) == 0 {
		return
	}

	if err, validate = bsv.getErrorCache(opts.RuleIds); validate {
		return
	}
	if !BceSecurityValidator.haveInit {
		err = fmt.Errorf("security validator not init")
		return
	}
	scopedLog := log.WithFields(logrus.Fields{
		"func":    "ViolateSecurityRules",
		"role":    opts.Role,
		"id":      opts.Indentifier,
		"ruleIDs": opts.RuleIds,
	})

	defer bsv.setErrorCache(opts.RuleIds, err)

	var sgs []*ccev2alpha1.SecurityGroup
	for _, ruleID := range opts.RuleIds {
		if ruleID == "" {
			continue
		}
		sg, err := BceSecurityValidator.updater.Lister().Get(ruleID)
		if err != nil {
			scopedLog.Warnf("violate security rules,get security group rule %s failed: %v", ruleID, err)
			continue
		}

		sgs = append(sgs, sg)
	}
	if len(sgs) == 0 {
		err = errors.New("no security group rule found")
		return
	}

	var sgRules = sgRulesCollector{}

	for _, sg := range sgs {
		sgRules.egressAllowedRules = append(sgRules.egressAllowedRules, sg.Spec.EgressRule.Allows...)
		sgRules.egressDropRules = append(sgRules.egressDropRules, sg.Spec.EgressRule.Drops...)
		sgRules.ingressAllowedRules = append(sgRules.ingressAllowedRules, sg.Spec.IngressRule.Allows...)
		sgRules.ingressDropRules = append(sgRules.ingressDropRules, sg.Spec.IngressRule.Drops...)
	}

	violation := bsv.invokeReuiredRules(opts.Role, &sgRules)
	if len(violation.Remark) > 0 {
		err = errors.New(strings.Join(violation.Remark, "; "))
		validate = true
	}
	return
}

func (bsv *SecurityValidator) getErrorCache(ids []string) (error, bool) {
	bsv.lock.Lock()
	defer bsv.lock.Unlock()

	if bsv.lastErrorTime.Add(bsv.checkInterval).Before(time.Now()) {
		return nil, true
	}

	key := strings.Join(ids, ",")
	if err, ok := bsv.checkErrorCacheMap[key]; ok {
		return err, true
	}
	return nil, false
}

func (bsv *SecurityValidator) setErrorCache(ids []string, err error) {
	bsv.lock.Lock()
	defer bsv.lock.Unlock()

	key := strings.Join(ids, ",")
	bsv.checkErrorCacheMap[key] = err

	if err != nil {
		bsv.lastErrorTime = time.Now()
	}
}

func (bsv *SecurityValidator) invokeReuiredRules(role ccev2alpha1.SecurityGroupUserRoles, sgRules *sgRulesCollector) *SafetyConstraintsViolations {
	var violation = &SafetyConstraintsViolations{}
	// 1. prepare rules to invoke
	var toCheckRules []RequiredSGRule
	switch role {
	case ccev2alpha1.SecurityGroupUserRolesMaster:
		toCheckRules = append(toCheckRules, masterRequiredRule...)
		toCheckRules = append(toCheckRules, nodeRequiredRule...)
		if bsv.EnableNodeOptCheck {
			toCheckRules = append(toCheckRules, masterOptionRule...)
			toCheckRules = append(toCheckRules, nodeOptionRule...)
		}
		if bsv.EnableIPv6Check {
			toCheckRules = append(toCheckRules, nodeIPv6OptionRule...)
		}
	case ccev2alpha1.SecurityGroupUserRolesNode:
		toCheckRules = append(toCheckRules, nodeRequiredRule...)
		if bsv.EnableNodeOptCheck {
			toCheckRules = append(toCheckRules, nodeOptionRule...)
		}
		if bsv.EnableIPv6Check {
			toCheckRules = append(toCheckRules, nodeIPv6OptionRule...)
		}
	default:
	}
	toCheckRules = append(toCheckRules, podRequiredRule...)
	if bsv.EnableIPv6Check {
		toCheckRules = append(toCheckRules, podIPv6OptionRule...)
	}

	// 2. Check each constraint, and if it matches all the rules in the security
	// group but still cannot pass, it is considered a check failure.
	for _, rule := range toCheckRules {
		if rule.SourceIP == IPRangeClusterCIDR {
			for _, cidr := range bsv.ClusterCIDR {
				rule.SourceIP = cidr
				if rule.violateRules(sgRules) {
					violation.Remark = append(violation.Remark, rule.Remark)
				}
			}
			for _, cidr := range bsv.VpcCIDR {
				rule.SourceIP = cidr
				if rule.violateRules(sgRules) {
					violation.Remark = append(violation.Remark, rule.Remark)
				}
			}

		} else {
			if rule.violateRules(sgRules) {
				violation.Remark = append(violation.Remark, rule.Remark)
			}
		}
	}

	return violation
}

func bceFuzzyMatchAny(a, b string) bool {
	return isAnyAllStr(a) || isAnyAllStr(b) || strings.EqualFold(a, b)
}

func isAnyAllStr(a string) bool {
	return a == Empty || a == All
}

func cidrFuzzyMatchAny(a, b string) bool {
	if bceFuzzyMatchAny(a, b) {
		return true
	}
	cidrs, invalids := ip.ParseCIDRs([]string{a, b})
	if len(invalids) > 0 {
		return false
	}
	return ip.IsOverlapCIDRs(cidrs[0], cidrs[1])
}

func cidrFuzzyContains(a, b string) bool {
	if isAnyAllStr(a) {
		return true
	}
	cidrs, invalids := ip.ParseCIDRs([]string{a, b})
	if len(invalids) > 0 {
		return false
	}
	return ip.IsContainsCIDRs(cidrs[0], cidrs[1])
}
