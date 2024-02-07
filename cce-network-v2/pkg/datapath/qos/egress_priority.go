package qos

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/tc"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint/event"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

const (
	egressPrioritySys = "egress-priority-manager"

	AnnotaionPodEgressPriority = "cce.baidubce.com/egress-priority"
)

var (
	GlobalManager *EgressPriorityManager
	managerLog    = logging.NewSubysLogger(egressPrioritySys)
)

func GetEgressPriorityFromPod(podAnnotation map[string]string) *ccev2.EgressPriorityOpt {
	var egressOpt *ccev2.EgressPriorityOpt
	if !option.Config.EnableEgressPriority {
		return nil
	}
	if len(podAnnotation) == 0 {
		return nil
	}
	if mode, ok := podAnnotation[AnnotaionPodEgressPriority]; ok {
		egressOpt = ccev2.NewEngressPriorityOpt(mode)
	}
	return egressOpt
}

type EgressPriorityManager struct {
	lock    sync.Mutex
	linkMap map[string]*engressLinkRule

	cceEndpointClient *watchers.CCEEndpointClient
}

func (manager *EgressPriorityManager) Start(cceEndpointClient *watchers.CCEEndpointClient) {
	manager.cceEndpointClient = cceEndpointClient
	if !option.Config.EnableEgressPriority {
		return
	}
	controller.NewManager().UpdateController(egressPrioritySys, controller.ControllerParams{
		RunInterval: option.Config.ResourceResyncInterval,
		DoFunc: func(ctx context.Context) error {
			cepList, _ := cceEndpointClient.List()

			var ipsetMap = make(map[string][]*net.IPNet)
			for _, cep := range cepList {
				if cep.Status.Networking == nil || len(cep.Status.Networking.Addressing) == 0 {
					continue
				}
				if GetEgressPriorityFromPod(cep.GetAnnotations()) == nil {
					continue
				}
				for _, addrPair := range cep.Status.Networking.Addressing {
					dev := addrPair.Interface
					// for vpc-route mode, the interface name is not set in addressing
					if dev == "" {
						dev = "default"
					}
					ipsetMap[addrPair.Interface] = append(ipsetMap[addrPair.Interface], ip.ConvertIPPairToIPNet(addrPair))
				}
			}

			managerLog.Debugf("start to clean egress priority rule")

			manager.lock.Lock()
			defer manager.lock.Unlock()

			for name, linkRule := range manager.linkMap {
				num, err := linkRule.cleanOtherEgressRuleBySrcIP(ipsetMap[name])
				if err != nil {
					managerLog.Errorf("failed to clean egress rule for %s: %v", name, err)
				} else if num > 0 {
					managerLog.Infof("cleaned %d egress rules for %s", num, name)
				}
			}
			return nil
		},
	})
}

// AcceptType implements event.EndpointProbeEventHandler.
func (*EgressPriorityManager) AcceptType() event.EndpointProbeEventType {
	return event.EndpointProbeEventEgressPriority
}

// Handle implements event.EndpointProbeEventHandler.
func (manager *EgressPriorityManager) Handle(event *event.EndpointProbeEvent) (*ccev2.ExtFeatureStatus, error) {
	if event.Obj.Spec.Network.EgressPriority == nil ||
		len(event.Obj.Status.Networking.Addressing) == 0 {
		return nil, fmt.Errorf("invalid event %v", event)
	}

	var (
		opt     = event.Obj.Spec.Network.EgressPriority
		now     = metav1.Now()
		dataMap = map[string]string{
			"bands":    fmt.Sprintf("%d", opt.Bands),
			"priority": opt.Priority,
			"dscp":     fmt.Sprintf("%d", opt.DSCP),
		}
		priorityStatus = &ccev2.ExtFeatureStatus{
			ContainerID: event.Obj.Spec.ExternalIdentifiers.ContainerID,
			Data:        dataMap,
			UpdateTime:  &now,
		}
	)

	scopeLog := managerLog.WithFields(logrus.Fields{
		"endpoint": event.ID,
		"bands":    opt.Bands,
		"priority": opt.Priority,
		"dscp":     opt.DSCP,
	})
	scopeLog.Info("egress priority received event")

	linkRule := manager.findLinkRule(event.Obj)
	if linkRule == nil {
		errMsg := fmt.Sprintf("failed to find link rule for endpoint %v", event.ID)
		priorityStatus.Msg = errMsg
		return priorityStatus, fmt.Errorf(errMsg)
	}
	if linkRule.link != nil {
		dataMap["devIndex"] = fmt.Sprintf("%d", linkRule.link.Attrs().Index)
	}

	for _, addrPair := range event.Obj.Status.Networking.Addressing {
		ipnet := &net.IPNet{
			IP:   net.ParseIP(addrPair.IP),
			Mask: ip.IPv4Mask32,
		}
		if ipnet.IP.To4() == nil {
			ipnet.Mask = ip.IPv6Mask128
		}
		err := linkRule.ensureEgressRule(uint32(opt.Bands), uint32(opt.DSCP), ipnet)
		if err != nil {
			errMsg := fmt.Sprintf("failed to ensure egress rule for %s: %v", ipnet.String(), err)
			priorityStatus.Msg = errMsg
			return priorityStatus, fmt.Errorf(errMsg)
		}
	}
	priorityStatus.Ready = true

	return priorityStatus, nil
}

func (manager *EgressPriorityManager) findLinkRule(cep *ccev2.CCEEndpoint) *engressLinkRule {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	for _, addrPair := range cep.Status.Networking.Addressing {
		if _, ok := manager.linkMap[addrPair.Interface]; ok {
			return manager.linkMap[addrPair.Interface]
		}
	}
	if option.Config.IPAM != ipamOption.IPAMVpcEni {
		return manager.linkMap["default"]
	}
	return nil
}

func InitEgressPriorityManager() {
	if GlobalManager != nil {
		return
	}
	GlobalManager = &EgressPriorityManager{
		linkMap: make(map[string]*engressLinkRule),
	}
	if !option.Config.EnableEgressPriority {
		return
	}

	if option.Config.IPAM != ipamOption.IPAMVpcEni {
		// find the default route
		defaultLink, err := link.DetectDefaultRouteInterface()
		if err != nil {
			managerLog.WithError(err).Warn("failed to detect default route interface")
			option.Config.EnableBandwidthManager = false
			return
		}
		GlobalManager.linkMap["default"] = &engressLinkRule{
			link: defaultLink,
		}
	}
}

func (manager *EgressPriorityManager) ENIUpdateEventHandler(eni *ccev2.ENI) {
	if _, ok := manager.linkMap[eni.Spec.ID]; !ok {
		link, err := netlink.LinkByIndex(eni.Status.InterfaceIndex)
		if err != nil {
			managerLog.WithError(err).WithField("eni", eni.Spec.ID).Warn("failed to get link by index")
			return
		}
		manager.linkMap[eni.Spec.ID] = &engressLinkRule{
			eni:  eni,
			link: link,
		}
	}
}

var _ event.EndpointProbeEventHandler = &EgressPriorityManager{}

type engressLinkRule struct {
	eni  *ccev2.ENI
	link netlink.Link
}

func (elr *engressLinkRule) ensureEgressRule(classID, dscp uint32, ipNet *net.IPNet) error {
	link := elr.link
	err := tc.EnsureMQQdisc(link)
	if err != nil {
		return err
	}

	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		return fmt.Errorf("list qdisc for dev %s error, %w", link.Attrs().Name, err)
	}
	for _, qdisc := range qdiscs {
		major, minor := netlink.MajorMinor(qdisc.Attrs().Parent)
		if major != 1 || minor == 0 {
			continue
		}
		if qdisc.Type() != "prio" {
			err = tc.QdiscReplace(netlink.NewPrio(netlink.QdiscAttrs{
				LinkIndex: link.Attrs().Index,
				Parent:    qdisc.Attrs().Parent,
			}))
			if err != nil {
				return err
			}
			managerLog.WithField("linkName", link.Attrs().Name).Infof("ensure qdisc %s to prio under mq", qdisc.Type())
		}
	}

	// TODO: we shoul set dscp
	return foreachPrioQdisc(link, func(q netlink.Qdisc) error {
		u32 := tc.NewU32RuleBySrcIP(link.Attrs().Index, q.Attrs().Handle, ipNet)
		major, _ := netlink.MajorMinor(q.Attrs().Handle)
		u32.ClassId = netlink.MakeHandle(major, uint16(classID))
		return tc.FilterAdd(u32)
	})
}

// cleanEgressRule will remove all the rules
func (elr *engressLinkRule) cleanOtherEgressRuleBySrcIP(ipset []*net.IPNet) (int, error) {
	link := elr.link

	var toCleanFilterNum int
	return toCleanFilterNum, foreachPrioQdisc(link, func(q netlink.Qdisc) error {
		filters, err := netlink.FilterList(link, q.Attrs().Handle)
		if err != nil {
			return err
		}

		var (
			matches [][]nl.TcU32Key
		)
		for _, ipNet := range ipset {
			matches = append(matches, tc.U32MatchSrc(ipNet))
		}

		for _, f := range filters {
			u32, ok := f.(*netlink.U32)
			if !ok {
				continue
			}
			if u32.Attrs().LinkIndex != link.Attrs().Index ||
				u32.Protocol != unix.ETH_P_IP ||
				u32.Sel == nil {
				continue
			}

			var matched bool
			// check all matches is satisfied with current
			for _, match := range matches {
				if u32.Sel.Nkeys != uint8(len(match)) {
					continue
				}

				for i := range u32.Sel.Keys {
					if u32.Sel.Keys[i].Mask != match[i].Mask ||
						u32.Sel.Keys[i].Off != match[i].Off ||
						u32.Sel.Keys[i].Val != match[i].Val {
						continue
					}
					matched = true
					break
				}
			}

			if !matched {
				toCleanFilterNum++
				err = tc.FilterDel(f)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func foreachPrioQdisc(link netlink.Link, f func(netlink.Qdisc) error) error {
	qds, err := netlink.QdiscList(link)
	if err != nil {
		return fmt.Errorf("failed to list qdisc for dev %s: %w", link.Attrs().Name, err)
	}
	for _, q := range qds {
		if q.Type() != "prio" {
			continue
		}
		err = f(q)
		if err != nil {
			return err
		}
	}
	return nil
}
