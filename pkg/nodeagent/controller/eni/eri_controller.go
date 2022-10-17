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
	"fmt"
	"net"
	"time"

	"github.com/vishvananda/netlink"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
)

const (
	route_table_local   = 255
	route_table_main    = 254
	route_table_default = 253
)

// Controller manages ENI at node level
type ERIController struct {
	metaClient    metadata.Interface
	netlink       netlinkwrapper.Interface
	sysctl        sysctlwrapper.Interface
	netutil       networkutil.Interface
	nodeName      string
	instanceID    string
	eriSyncPeriod time.Duration
}

// New creates ERI Controller
func NewERI(
	metaClient metadata.Interface,
	nodeName string,
	instanceID string,
	eriSyncPeriod time.Duration,
) *ERIController {
	c := &ERIController{
		metaClient:    metaClient,
		netlink:       netlinkwrapper.New(),
		sysctl:        sysctlwrapper.New(),
		netutil:       networkutil.New(),
		nodeName:      nodeName,
		instanceID:    instanceID,
		eriSyncPeriod: eriSyncPeriod,
	}

	return c
}

func (c *ERIController) ReconcileERIs() {
	err := wait.PollImmediateInfinite(wait.Jitter(c.eriSyncPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()
		log.Infof(ctx, "ReconcileERIs eris for %v begins...", c.nodeName)
		err := c.reconcileERIs(ctx)
		if err != nil {
			log.Errorf(ctx, "error reconciling eris: %v", err)
		}

		return false, nil
	})
	if err != nil {
		log.Errorf(context.TODO(), "failed to sync reconcile eris: %v", err)
	}
}

func (c *ERIController) reconcileERIs(ctx context.Context) error {
	log.Infof(ctx, "reconcile eris for %v begins...", c.nodeName)
	defer log.Infof(ctx, "reconcile eris for %v ends...", c.nodeName)

	linkList, err := c.netlink.LinkList()
	if err != nil {
		return err
	}

	for _, l := range linkList {
		if l.Attrs().Name == "lo" {
			continue
		}

		if l.Type() == "device" {
			err = c.setupERILink(ctx, l)
			if err != nil {
				log.Errorf(ctx, "failed to setup eri link: %s", err.Error())
				continue
			}
		}
	}

	c.removeUnusedRules(ctx)

	return nil
}

func (c *ERIController) getPrimaryIP(ctx context.Context, l netlink.Link) (string, error) {
	// get primary ip and secondary ips
	primaryIP, _, err := c.metaClient.GetLinkPrimaryAndSecondaryIPs(l.Attrs().HardwareAddr.String())
	if err != nil {
		log.Errorf(ctx, "failed to get primary ip and secondary ips of link %v: %v", l.Attrs().Name, err)
		return "", err
	}

	return primaryIP, nil
}

func (c *ERIController) setupERILink(ctx context.Context, l netlink.Link) error {
	err := c.setLinkUP(ctx, l)
	if err != nil {
		return err
	}

	primaryIp, err := c.getPrimaryIP(ctx, l)
	if err != nil {
		return err
	}

	err = c.addPrimaryIP(ctx, l, primaryIp)
	if err != nil {
		return err
	}

	_ = c.disableRPFCheck(ctx, l)

	return nil
}

func (c *ERIController) setLinkUP(ctx context.Context, intf netlink.Link) error {
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

func (c *ERIController) addPrimaryIP(ctx context.Context, intf netlink.Link, primaryIP string) error {
	addrs, err := c.netlink.AddrList(intf, netlink.FAMILY_V4)
	if err != nil {
		log.Errorf(ctx, "failed to list addresses of link %v: %v", intf.Attrs().Name, err)
		return err
	}

	for _, addr := range addrs {
		// primary IP already on link
		if addr.IP.String() == primaryIP {
			log.Infof(ctx, "primary ip is already exist:%s", primaryIP)
			return nil
		}
	}

	log.Infof(ctx, "start to add primary IP %v to link %v", primaryIP, intf.Attrs().Name)
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

func (c *ERIController) disableRPFCheck(ctx context.Context, l netlink.Link) error {
	var errs []error

	for _, intf := range []string{"all", l.Attrs().Name} {
		log.Infof(ctx, "disable RPF : %s", intf)
		if intf != "" {
			if _, err := c.sysctl.Sysctl(fmt.Sprintf(rpFilterSysctlTemplate, intf), "0"); err != nil {
				errs = append(errs, err)
				log.Errorf(ctx, "failed to disable RP filter for interface %v: %v", intf, err)
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

func (c *ERIController) removeUnusedRules(ctx context.Context) error {
	rules, err := c.netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for _, r := range rules {
		if r.Table != route_table_default && r.Table != route_table_local && r.Table != route_table_main {
			err = c.netlink.RuleDel(&r)
			if err != nil {
				log.Errorf(ctx, "failed to delete rule,table id: %d,error: %s", r.Table, err.Error())
			} else {
				log.Infof(ctx, "remove rule successfully,table id:%d", r.Table)
			}
		}
	}
	return nil
}
