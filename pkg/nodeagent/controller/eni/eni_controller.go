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
	"k8s.io/client-go/util/retry"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	bccipam "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/bcc"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
)

const (
	eniAttachTimeout  int = 50
	eniAttachMaxRetry int = 10

	fromENIPrimaryIPRulePriority = 1024
)

var (
	eniNameMatcher = regexp.MustCompile(`eth(\d+)`)
)

// Controller manages ENI at node level
type Controller struct {
	cloudClient   cloud.Interface
	metaClient    metadata.Interface
	kubeClient    kubernetes.Interface
	crdClient     clientset.Interface
	netlink       netlinkwrapper.Interface
	clusterID     string
	nodeName      string
	defaultIPPool string
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
		netlink:       netlinkwrapper.New(),
		clusterID:     clusterID,
		nodeName:      nodeName,
		defaultIPPool: utilippool.GetDefaultIPPoolName(nodeName),
		instanceID:    instanceID,
		vpcID:         vpcID,
		rtTableOffset: rtTableOffset,
		eniSyncPeriod: eniSyncPeriod,
	}

	return c
}

func (c *Controller) ReconcileENIs() {
	err := wait.PollImmediateInfinite(c.eniSyncPeriod, func() (bool, error) {
		ctx := log.NewContext()

		err := c.reconcileENIs(ctx)
		if err != nil {
			log.Errorf(ctx, "error reconciling enis: %v", err)
		}

		var networkReady = true
		if err != nil && !cloud.IsErrorRateLimit(err) {
			networkReady = false
		}

		patchErr := k8sutil.UpdateNetworkingCondition(
			ctx,
			c.kubeClient,
			c.nodeName,
			networkReady,
			"AllENIReady",
			"NotAllENIReady",
			"CCE ENI Controller reconciles ENI",
			"CCE ENI Controller failed to reconcile ENI",
		)

		if patchErr != nil {
			log.Errorf(ctx, "eni: update networking condition for node %v error: %v", c.nodeName, patchErr)
		}

		return false, nil
	})
	if err != nil {
		log.Errorf(context.TODO(), "failed to sync reconcile enis: %v", err)
	}
}

func (c *Controller) reconcileENIs(ctx context.Context) error {
	log.Infof(ctx, "reconcile enis for %v begins...", c.nodeName)
	defer log.Infof(ctx, "reconcile enis for %v ends...", c.nodeName)

	// list all enis in vpc
	enis, err := c.cloudClient.ListENIs(ctx, c.vpcID)
	if err != nil {
		log.Errorf(ctx, "failed to list enis in vpc %s: %v", c.vpcID, err)
		return err
	}

	// list available enis
	availableENIs := c.listAvailableENIs(ctx, enis)
	if len(availableENIs) != 0 {
		log.Infof(ctx, "node %v has %d available enis", c.nodeName, len(availableENIs))
		// sleep to wait ipam attaching eni
		time.Sleep(bccipam.ENIReadyTimeToAttach * 2)
		// free leaked available enis after sleep
		err = c.freeLeakedAvailableENIs(ctx, availableENIs)
		if err != nil {
			log.Errorf(ctx, "failed to free all available enis: %v", err)
		}
	}

	// update cr status and ip link up and add addr
	err = c.updateIPPoolStatus(ctx, enis)
	if err != nil {
		log.Errorf(ctx, "failed to update ippool %v status: %v", c.defaultIPPool, err)
		return err
	}

	return nil
}

func (c *Controller) waitForUnstableENIWithTimeout(ctx context.Context) error {
	sleepTime := time.Duration(eniAttachTimeout/eniAttachMaxRetry) * time.Second

	for i := 0; i < eniAttachMaxRetry; i++ {
		// list all enis in vpc
		enis, err := c.cloudClient.ListENIs(ctx, c.vpcID)
		if err != nil {
			log.Errorf(ctx, "waitForUnstableENIWithTimeout tries %d time: failed to list enis in vpc %s: %v", i, c.vpcID, err)
			time.Sleep(sleepTime)
			continue
		}

		allENIStable := true

		for _, eni := range enis {
			// if eni not owned by local node, just ignore
			if !utileni.ENIOwnedByNode(&eni, c.clusterID, c.instanceID) {
				continue
			}

			if eni.Status == utileni.ENIStatusAttaching || eni.Status == utileni.ENIStatusDetaching {
				allENIStable = false
				log.Infof(ctx, "waitForUnstableENIWithTimeout tries %d time: eni %s is in status %s", i, eni.EniId, eni.Status)
			}
		}

		if allENIStable {
			log.Info(ctx, "waitForUnstableENIWithTimeout: all enis are stable")
			return nil
		}

		time.Sleep(sleepTime)
	}

	// attach timeout
	log.Errorf(ctx, "waitForUnstableENIWithTimeout: eni in attaching/detaching status too long")
	return fmt.Errorf("eni in attaching/detaching status too long")
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

// freeLeakedAvailableENIs frees ENIs that are available and owned by this node
func (c *Controller) freeLeakedAvailableENIs(ctx context.Context, enis []enisdk.Eni) error {
	var errs []error
	for _, eni := range enis {
		// check eni status before delete
		resp, err := c.cloudClient.StatENI(ctx, eni.EniId)
		if err != nil {
			log.Errorf(ctx, "failed to stat eni %v: %v", eni.EniId, err)
			continue
		}
		if resp.Status != utileni.ENIStatusAvailable {
			continue
		}

		err = c.cloudClient.DeleteENI(ctx, eni.EniId)
		if err != nil {
			log.Errorf(ctx, "failed to delete eni %v: %v", eni.EniId, err)
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)

}

func (c *Controller) updateIPPoolStatus(ctx context.Context, enis []enisdk.Eni) error {
	eniStatus := map[string]v1alpha1.ENI{}

	for _, eni := range enis {
		if !utileni.ENIOwnedByNode(&eni, c.clusterID, c.instanceID) {
			continue
		}

		linkIndex, err := c.setupENILink(ctx, &eni)
		if err != nil {
			log.Errorf(ctx, "failed to ensure eni %v ready: %v", eni.EniId, err)
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
		result, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(c.defaultIPPool, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", c.defaultIPPool, err)
			return err
		}
		result.Status.ENI.ENIs = eniStatus

		_, updateErr := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(result)
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v status: %v", c.defaultIPPool, updateErr)
			return updateErr
		}
		return nil
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v status: %v", c.defaultIPPool, retryErr)
		return retryErr
	}

	return nil
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
	addrs, err := netlink.AddrList(intf, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		dst := net.IPNet{
			IP:   addr.IP.Mask(addr.Mask),
			Mask: addr.Mask,
		}
		err = netlink.RouteDel(&netlink.Route{
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
	defautRt, err := c.findLinkDefaultRoute(ctx, intf)
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
		gateway = defautRt.Gw
	}

	log.Infof(ctx, "eni %v with primary IP %v has gateway: %v", eni.EniId, primaryIP, gateway)
	_, dstNet, _ := net.ParseCIDR("0.0.0.0/0")
	// ip route add default via {eniGW} dev ethX table {rtTable} onlink
	rt := &netlink.Route{
		LinkIndex: intf.Attrs().Index,
		Dst:       dstNet,
		Table:     rtTable,
		Gw:        gateway,
	}
	rt.SetFlag(netlink.FLAG_ONLINK)

	err = c.netlink.RouteReplace(rt)
	if err != nil {
		msg := fmt.Sprintf("failed to replace default route %+v in table %v: %v", *rt, rtTable, err)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	return nil
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
