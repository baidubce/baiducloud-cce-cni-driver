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

package eri

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"

	"github.com/vishvananda/netlink"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/rdma/client"
	cniconf "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/controller/cniconf"
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

var (
	eniNameMatcher = regexp.MustCompile(`eth(\d+)`)
	eniNamePrefix  = "eth"
)
var noforever = true

type Controller struct {
	metaClient    metadata.Interface
	eriClient     *client.EriClient
	netlink       netlinkwrapper.Interface
	sysctl        sysctlwrapper.Interface
	netutil       networkutil.Interface
	clusterID     string
	nodeName      string
	instanceID    string
	vpcID         string
	eniSyncPeriod time.Duration
}

// New creates ERI Controller
func New(
	metaClient metadata.Interface,
	eriClient *client.EriClient,
	netlink netlinkwrapper.Interface,
	sysctl sysctlwrapper.Interface,
	netutil networkutil.Interface,
	clusterID string,
	nodeName string,
	instanceID string,
	vpcID string,
	eniSyncPeriod time.Duration,
) *Controller {
	c := &Controller{
		metaClient:    metaClient,
		eriClient:     eriClient,
		netlink:       netlink,
		sysctl:        sysctl,
		netutil:       netutil,
		clusterID:     clusterID,
		nodeName:      nodeName,
		instanceID:    instanceID,
		vpcID:         vpcID,
		eniSyncPeriod: eniSyncPeriod,
	}

	return c
}

func (c *Controller) ReconcileERIs() {
	ctx := log.NewContext()

	// run netlink monitor, bootstrap eni initialization process
	lm, err := nlmonitor.NewLinkMonitor(c)
	if err != nil {
		log.Errorf(ctx, "new link monitor error: %v", err)
	} else {
		defer lm.Close()
		log.Info(ctx, "new link monitor successfully")
	}

	// reconcile eri regularly
	err = wait.PollImmediateInfinite(wait.Jitter(c.eniSyncPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()

		err := c.reconcileERIs(ctx)
		if err != nil {
			log.Errorf(ctx, "error reconciling eris: %v", err)
		}
		return noforever, nil
	})
	if err != nil {
		log.Errorf(context.TODO(), "failed to sync reconcile eris: %v", err)
	}
}

func (c *Controller) reconcileERIs(ctx context.Context) error {
	log.Infof(ctx, "reconcile eris for %v begins...", c.nodeName)
	defer log.Infof(ctx, "reconcile eris for %v ends...", c.nodeName)

	// list enis attached to node
	var (
		listENIBackoff = wait.Backoff{
			Steps:    5,
			Duration: 2 * time.Second,
			Factor:   4.5,
			Jitter:   0.2,
		}

		eris = make([]client.EniResult, 0)
	)

	err := wait.ExponentialBackoff(listENIBackoff, func() (bool, error) {
		var err error
		eris, err = c.eriClient.ListEnis(ctx, c.vpcID, c.instanceID)
		if err != nil {
			log.Errorf(ctx, "failed to list eris attached to instance %s: %v", c.instanceID, err)
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		return err
	}
	log.Infof(ctx, "get eris:  %v ", eris)

	// set eni link up
	// add eni link ip addr
	_ = c.setupENINetwork(ctx, eris)
	return nil
}

func (c *Controller) setupENINetwork(ctx context.Context, eris []client.EniResult) error {
	for _, eri := range eris {
		_, err := c.setupENILink(ctx, &eri)
		if err != nil {
			log.Errorf(ctx, "failed to ensure eri %v ready: %v", eri.EniID, err)
		}
	}
	return nil
}

func (c *Controller) setupENILink(ctx context.Context, eri *client.EniResult) (int, error) {
	eniIntf, err := c.findENILinkByMac(ctx, eri)
	if err != nil {
		return -1, err
	}
	eniIndex := eniIntf.Attrs().Index

	mtu := cniconf.DefaultMTU
	err = c.setLinkUP(ctx, eniIntf, mtu)
	if err != nil {
		log.Errorf(ctx, "eni set uplink error. index: %d, mac: %s, mtu: %d, errmsg: %v", eniIntf.Attrs().Index, eniIntf.Attrs().HardwareAddr.String(), mtu, err)
		return eniIndex, err
	}

	err = c.addPrimaryIP(ctx, eniIntf, getPrimaryIP(eri))
	if err != nil {
		return eniIndex, err
	}

	_ = c.disableRPFCheck(ctx, eniIntf)

	return eniIndex, nil
}

func (c *Controller) setLinkUP(ctx context.Context, intf netlink.Link, mtu int) error {
	if intf.Attrs().Flags&net.FlagUp != 0 {
		return nil
	}
	// if link is down, set link up
	log.Warningf(ctx, "link %v is down, will bring it up", intf.Attrs().Name)
	log.Infof(ctx, "setup eri: index: %d, mac: %s, mtu: %d", intf.Attrs().Index, intf.Attrs().HardwareAddr.String(), mtu)
	err := c.netlink.LinkSetMTU(intf, mtu)
	if err != nil {
		return err
	}

	err = c.netlink.LinkSetUp(intf)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) findENILinkByMac(ctx context.Context, eri *client.EniResult) (netlink.Link, error) {
	var eniIntf netlink.Link
	// list all interfaces, and find ENI by mac address
	interfaces, err := c.netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %v", interfaces)
	}

	for _, intf := range interfaces {
		if intf.Attrs().HardwareAddr.String() == eri.MacAddress {
			eniIntf = intf
			break
		}
	}

	if eniIntf == nil {
		return nil, fmt.Errorf("eni with mac address %v not found", eri.MacAddress)
	}

	return eniIntf, nil
}

// addPrimaryIP add eri primary IP to
func (c *Controller) addPrimaryIP(ctx context.Context, intf netlink.Link, primaryIP string) error {
	addrs, err := c.netlink.AddrList(intf, netlink.FAMILY_V4)
	if err != nil {
		log.Errorf(ctx, "failed to list addresses of link %v: %v", intf.Attrs().Name, err)
		return err
	}

	for _, addr := range addrs {
		// primary IP already on link
		if addr.IP.String() == primaryIP {
			log.Infof(ctx, "primary IP already on link: %v, primaryIP:%v", intf.Attrs().Name, primaryIP)
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

func getPrimaryIP(eri *client.EniResult) string {
	for _, ip := range eri.PrivateIPSet {
		if ip.Primary {
			return ip.PrivateIPAddress
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

	// use link name and link type to filter out eri link
	log.Infof(ctx, "netlink monitor detected eri link %v added", link.Attrs().Name)
	err = c.reconcileERIs(ctx)
	if err != nil {
		log.Errorf(ctx, "error reconciling eris: %v", err)
	}
}

func (c *Controller) HandleDellink(link netlink.Link) {}
