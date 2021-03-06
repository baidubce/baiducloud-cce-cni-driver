/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package eni

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	cidrutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/cidr"
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/nlmonitor"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
)

const (
	fromENIPrimaryIPRulePriority = 1024
	rpFilterSysctlTemplate       = "net.ipv4.conf.%s.rp_filter"
)

const (
	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5
)

var (
	eniNameMatcher = regexp.MustCompile(`eth(\d+)`)
	eniNamePrefix  = "eth"
)

// Controller manages ENI at node level
type Controller struct {
	cloudClient   cloud.Interface
	metaClient    metadata.Interface
	kubeClient    kubernetes.Interface
	crdClient     clientset.Interface
	eventRecorder record.EventRecorder
	netlink       netlinkwrapper.Interface
	sysctl        sysctlwrapper.Interface
	netutil       networkutil.Interface
	clusterID     string
	nodeName      string
	ippoolName    string
	instanceID    string
	vpcID         string
	rtTableOffset int
	eniSyncPeriod time.Duration
}

// New creates ENI Controller
func New(
	cloudClient cloud.Interface,
	metaClient metadata.Interface,
	kubeClient kubernetes.Interface,
	crdClient clientset.Interface,
	eventRecorder record.EventRecorder,
	clusterID string,
	nodeName string,
	instanceID string,
	vpcID string,
	rtTableOffset int,
	eniSyncPeriod time.Duration,
) *Controller {
	c := &Controller{
		cloudClient:   cloudClient,
		metaClient:    metaClient,
		kubeClient:    kubeClient,
		crdClient:     crdClient,
		eventRecorder: eventRecorder,
		netlink:       netlinkwrapper.New(),
		sysctl:        sysctlwrapper.New(),
		netutil:       networkutil.New(),
		clusterID:     clusterID,
		nodeName:      nodeName,
		ippoolName:    utilippool.GetNodeIPPoolName(nodeName),
		instanceID:    instanceID,
		vpcID:         vpcID,
		rtTableOffset: rtTableOffset,
		eniSyncPeriod: eniSyncPeriod,
	}

	return c
}

func (c *Controller) ReconcileENIs() {
	ctx := log.NewContext()

	// run netlink monitor, bootstrap eni initialization process
	lm, err := nlmonitor.NewLinkMonitor(c)
	if err != nil {
		log.Errorf(ctx, "new link monitor error: %v", err)
	} else {
		defer lm.Close()
		log.Info(ctx, "new link monitor successfully")
	}

	go c.taintNodeIfNoEniBound()

	// reconcile eni regularly
	err = wait.PollImmediateInfinite(wait.Jitter(c.eniSyncPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()

		eniCnt, err := c.reconcileENIs(ctx)
		if err != nil {
			log.Errorf(ctx, "error reconciling enis: %v", err)
			c.eventRecorder.Eventf(&v1.ObjectReference{
				Kind: "ENI",
				Name: "ENIReconciliation",
			}, v1.EventTypeWarning, "ENI Not Ready", "CCE ENI Controller failed to reconcile enis: %v", err)
		}
		// only update when eni ready, never taint node
		if eniCnt > 0 {
			patchErr := c.updateNetworkingCondition(ctx, true)
			if patchErr != nil {
				log.Errorf(ctx, "eni: update networking condition for node %v error: %v", c.nodeName, patchErr)
			}
		}

		return false, nil
	})
	if err != nil {
		log.Errorf(context.TODO(), "failed to sync reconcile enis: %v", err)
	}
}

func (c *Controller) reconcileENIs(ctx context.Context) (int, error) {
	log.Infof(ctx, "reconcile enis for %v begins...", c.nodeName)
	defer log.Infof(ctx, "reconcile enis for %v ends...", c.nodeName)

	// list enis attached to node
	var (
		listENIArgs = enisdk.ListEniArgs{
			VpcId:      c.vpcID,
			InstanceId: c.instanceID,
		}

		listENIBackoff = wait.Backoff{
			Steps:    5,
			Duration: 2 * time.Second,
			Factor:   4.5,
			Jitter:   0.2,
		}

		enis = make([]enisdk.Eni, 0)
	)

	err := wait.ExponentialBackoff(listENIBackoff, func() (bool, error) {
		var err error
		enis, err = c.cloudClient.ListENIs(ctx, listENIArgs)
		if err != nil {
			log.Errorf(ctx, "failed to list enis attached to instance %s: %v", c.instanceID, err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return 0, err
	}

	// set eni link up
	// add eni link ip addr
	// report link index
	eniCnt, err := c.setupENINetwork(ctx, enis)
	if err != nil {
		log.Errorf(ctx, "failed to update ippool %v status: %v", c.ippoolName, err)
		return 0, err
	}

	return eniCnt, nil
}

// listAvailableENIs lists ENIs that are available and owned by this node
func (c *Controller) listAvailableENIs(ctx context.Context, enis []enisdk.Eni) []enisdk.Eni {
	var reusableENIs []enisdk.Eni

	// find out available enis
	for _, eni := range enis {
		if eni.Status == utileni.ENIStatusAvailable && utileni.ENIOwnedByNode(&eni, c.clusterID, c.instanceID) {
			reusableENIs = append(reusableENIs, eni)
		}
	}

	return reusableENIs
}

func (c *Controller) setupENINetwork(ctx context.Context, enis []enisdk.Eni) (int, error) {
	var (
		eniStatus         = make(map[string]v1alpha1.ENI)
		readyENICount int = 0
	)

	for _, eni := range enis {
		if !utileni.ENIOwnedByNode(&eni, c.clusterID, c.instanceID) {
			continue
		}

		linkIndex, err := c.setupENILink(ctx, &eni)
		if err != nil {
			log.Errorf(ctx, "failed to ensure eni %v ready: %v", eni.EniId, err)
		} else {
			readyENICount++
		}
		eniStatus[eni.EniId] = v1alpha1.ENI{
			ID:               eni.EniId,
			MAC:              eni.MacAddress,
			AvailabilityZone: eni.ZoneName,
			Description:      eni.Description,
			InterfaceIndex:   linkIndex,
			Subnet:           eni.SubnetId,
			PrivateIPSet:     utileni.GetPrivateIPSet(&eni),
			VPC:              eni.VpcId,
		}
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(ctx, c.ippoolName, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", c.ippoolName, err)
			return err
		}
		result.Status.ENI.ENIs = eniStatus

		_, updateErr := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(ctx, result, metav1.UpdateOptions{})
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v status: %v", c.ippoolName, updateErr)
			return updateErr
		}
		return nil
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v status: %v", c.ippoolName, retryErr)
		return 0, retryErr
	}

	return readyENICount, nil
}

func (c *Controller) setupENILink(ctx context.Context, eni *enisdk.Eni) (int, error) {
	eniIntf, err := c.findENILinkByMac(ctx, eni)
	if err != nil {
		return -1, err
	}
	eniIndex := eniIntf.Attrs().Index

	err = c.setLinkUP(ctx, eniIntf)
	if err != nil {
		return eniIndex, err
	}

	err = c.addPrimaryIP(ctx, eniIntf, getPrimaryIP(eni))
	if err != nil {
		return eniIndex, err
	}

	err = c.delScopeLinkRoute(ctx, eniIntf)
	if err != nil {
		return eniIndex, err
	}

	err = c.addFromENIRule(ctx, eni, eniIntf)
	if err != nil {
		log.Errorf(ctx, "failed to add from eni primary ip rule: %v", err)
		return eniIndex, err
	}

	_ = c.disableRPFCheck(ctx, eniIntf)

	return eniIndex, nil
}

func (c *Controller) setLinkUP(ctx context.Context, intf netlink.Link) error {
	if intf.Attrs().Flags&net.FlagUp != 0 {
		return nil
	}
	// if link is down, set link up
	log.Warningf(ctx, "link %v is down, will bring it up", intf.Attrs().Name)
	err := c.netlink.LinkSetUp(intf)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) delScopeLinkRoute(ctx context.Context, intf netlink.Link) error {
	addrs, err := c.netlink.AddrList(intf, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		dst := net.IPNet{
			IP:   addr.IP.Mask(addr.Mask),
			Mask: addr.Mask,
		}
		err = c.netlink.RouteDel(&netlink.Route{
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

func (c *Controller) findENILinkByMac(ctx context.Context, eni *enisdk.Eni) (netlink.Link, error) {
	var eniIntf netlink.Link
	// list all interfaces, and find ENI by mac address
	interfaces, err := c.netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %v", interfaces)
	}

	for _, intf := range interfaces {
		if intf.Attrs().HardwareAddr.String() == eni.MacAddress {
			eniIntf = intf
			break
		}
	}

	if eniIntf == nil {
		return nil, fmt.Errorf("eni with mac address %v not found", eni.MacAddress)
	}

	return eniIntf, nil
}

func (c *Controller) addFromENIRule(ctx context.Context, eni *enisdk.Eni, intf netlink.Link) error {
	primaryIP := getPrimaryIP(eni)
	if primaryIP == "" {
		return fmt.Errorf("no primary ip found")
	}

	eniIndex, err := getInterfaceIndex(intf)
	if err != nil {
		return err
	}
	rtTable := c.rtTableOffset + int(eniIndex)

	// ip rule add from {primaryIP} lookup {rtTable} prio 1024
	rule := c.netlink.NewRule()
	rule.Src = &net.IPNet{
		IP:   net.ParseIP(primaryIP),
		Mask: net.CIDRMask(32, 32),
	}
	rule.Table = rtTable
	rule.Priority = fromENIPrimaryIPRulePriority
	existed, err := c.isRuleExisted(rule)
	if err != nil {
		return err
	}
	if !existed {
		if err := c.netlink.RuleAdd(rule); err != nil && !netlinkwrapper.IsExistsError(err) {
			log.Errorf(ctx, "failed to add rule %+v: %v", *rule, err)
			return err
		}
	}

	var gateway net.IP
	defaultRt, err := c.findLinkDefaultRoute(ctx, intf)
	if err != nil {
		log.Errorf(ctx, "failed to get gateway of eni %v from link default route: %v", eni.EniId, err)
		log.Infof(ctx, "fall back to get gateway of eni %v from meta-data", eni.EniId)
		// fallback to meta-data api
		gw, err := c.getLinkGateway(ctx, eni)
		if err != nil {
			log.Errorf(ctx, "failed to get gateway of eni %v from meta-data: %v", eni.EniId, err)
			return err
		}
		gateway = gw
	} else {
		gateway = defaultRt.Gw
	}

	log.Infof(ctx, "eni %v with primary IP %v has gateway: %v", eni.EniId, primaryIP, gateway)

	natgreLink, useBigNat := c.bigNatLinkExists(ctx)
	if useBigNat {
		log.Infof(ctx, "detect bignat used...")
		c.addBigNatRoutes(ctx, natgreLink, intf, gateway, rtTable)
	} else {
		// ip route replace default via {eniGW} dev ethX table {rtTable} onlink
		rt, err := c.replaceRoute(cidrutil.IPv4ZeroCIDR, gateway, intf, rtTable, true)
		if err != nil {
			msg := fmt.Sprintf("failed to replace default route %+v in table %v: %v", *rt, rtTable, err)
			log.Error(ctx, msg)
			return errors.New(msg)
		}
	}

	return nil
}

func (c *Controller) replaceRoute(dst *net.IPNet, gw net.IP, dev netlink.Link, table int, onlink bool) (*netlink.Route, error) {
	rt := &netlink.Route{
		LinkIndex: dev.Attrs().Index,
		Dst:       dst,
		Table:     table,
		Gw:        gw,
	}
	if onlink {
		rt.SetFlag(netlink.FLAG_ONLINK)
	} else {
		rt.Scope = netlink.SCOPE_LINK
	}

	return rt, c.netlink.RouteReplace(rt)
}

// addPrimaryIP add eni primary IP to
func (c *Controller) addPrimaryIP(ctx context.Context, intf netlink.Link, primaryIP string) error {
	addrs, err := c.netlink.AddrList(intf, netlink.FAMILY_V4)
	if err != nil {
		log.Errorf(ctx, "failed to list addresses of link %v: %v", intf.Attrs().Name, err)
		return err
	}

	for _, addr := range addrs {
		// primary IP already on link
		if addr.IP.String() == primaryIP {
			return nil
		}
	}

	log.Infof(ctx, "start to add primary IP %v to link %v", primaryIP, intf.Attrs().Name)
	// mask is in format like 255.255.240.0
	mask, err := c.metaClient.GetLinkMask(intf.Attrs().HardwareAddr.String(), primaryIP)
	if err != nil {
		log.Errorf(ctx, "failed to get link %v mask: %v", intf.Attrs().Name, err)
		return err
	}
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(primaryIP),
			Mask: net.IPMask(net.ParseIP(mask).To4()),
		},
	}

	err = c.netlink.AddrAdd(intf, addr)
	if err != nil && !netlinkwrapper.IsExistsError(err) {
		log.Errorf(ctx, "failed to add primary IP %v to link %v", addr.String(), intf.Attrs().Name)
		return err
	}

	log.Infof(ctx, "add primary IP %v to link %v successfully", addr.String(), intf.Attrs().Name)
	return nil
}

func (c *Controller) disableRPFCheck(ctx context.Context, eniIntf netlink.Link) error {
	var errs []error

	primaryInterfaceName, _ := c.netutil.DetectDefaultRouteInterfaceName()

	for _, intf := range []string{"all", primaryInterfaceName, eniIntf.Attrs().Name} {
		if intf != "" {
			if _, err := c.sysctl.Sysctl(fmt.Sprintf(rpFilterSysctlTemplate, intf), "0"); err != nil {
				errs = append(errs, err)
				log.Errorf(ctx, "failed to disable RP filter for interface %v: %v", intf, err)
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

func (c *Controller) findLinkDefaultRoute(ctx context.Context, intf netlink.Link) (*netlink.Route, error) {
	// ip route show dev ethX
	routes, err := c.netlink.RouteList(intf, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list dev %v routes: %v", intf.Attrs().Name, err)
	}
	// find eni default route
	for _, r := range routes {
		if r.Dst == nil || r.Dst.String() == "0.0.0.0/0" {
			return &r, nil
		}
	}

	return nil, fmt.Errorf("default route of %v not found", intf.Attrs().Name)
}

func (c *Controller) isRuleExisted(rule *netlink.Rule) (bool, error) {
	rules, err := c.netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		return false, err
	}
	for _, r := range rules {
		if r.Src != nil && rule.Src != nil {
			if r.Priority == rule.Priority && r.Src.String() == rule.Src.String() && r.Table == rule.Table {
				return true, nil
			}
		}
	}
	return false, nil
}

func (c *Controller) getLinkGateway(ctx context.Context, eni *enisdk.Eni) (net.IP, error) {
	gateway, err := c.metaClient.GetLinkGateway(eni.MacAddress, getPrimaryIP(eni))
	if err != nil {
		return nil, err
	}

	gw := net.ParseIP(gateway)
	if gw == nil {
		return nil, fmt.Errorf("error parsing gateway IP address: %v", gateway)
	}
	return gw, nil
}

func getPrimaryIP(eni *enisdk.Eni) string {
	for _, ip := range eni.PrivateIpSet {
		if ip.Primary {
			return ip.PrivateIpAddress
		}
	}
	return ""
}

func getInterfaceIndex(intf netlink.Link) (int, error) {
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

func (c *Controller) HandleNewlink(link netlink.Link) {
	ctx := log.NewContext()

	if link.Type() != "device" {
		return
	}

	_, err := getInterfaceIndex(link)
	if err != nil {
		return
	}

	// use link name and link type to filter out eni link
	log.Infof(ctx, "netlink monitor detected eni link %v added", link.Attrs().Name)
	eniCnt, err := c.reconcileENIs(ctx)
	if err != nil {
		log.Errorf(ctx, "error reconciling enis: %v", err)
	}

	if eniCnt > 0 {
		patchErr := c.updateNetworkingCondition(ctx, true)
		if patchErr != nil {
			log.Errorf(ctx, "eni: update networking condition for node %v error: %v", c.nodeName, patchErr)
		}
	}
}

func (c *Controller) HandleDellink(link netlink.Link) {}

func (c *Controller) updateNetworkingCondition(ctx context.Context, ready bool) error {
	return k8sutil.UpdateNetworkingCondition(
		ctx,
		c.kubeClient,
		c.nodeName,
		ready,
		"AllENIReady",
		"NotAllENIReady",
		"CCE ENI Controller reconciles ENI",
		"CCE ENI Controller failed to reconcile ENI",
	)
}

func (c *Controller) taintNodeIfNoEniBound() error {
	_ = wait.PollImmediateInfinite(wait.Jitter(time.Second*30, 0.5), func() (done bool, err error) {
		var (
			eniLinkFound bool
		)

		ctx := log.NewContext()
		// try several eni links...
		for i := 1; i <= 7; i++ {
			eniLinkName := fmt.Sprintf("%s%d", eniNamePrefix, i)
			link, err := c.netlink.LinkByName(eniLinkName)
			if err == nil && link.Type() == "device" {
				eniLinkFound = true
				break
			}
		}

		if !eniLinkFound {
			patchErr := c.updateNetworkingCondition(ctx, false)
			if patchErr != nil {
				log.Errorf(ctx, "eni: update networking condition for node %v error: %v", c.nodeName, patchErr)
			}
		}

		return false, nil
	})

	return nil
}
