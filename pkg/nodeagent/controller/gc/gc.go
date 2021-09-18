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

package gc

import (
	"context"
	"time"

	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"

	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
)

const (
	toContainerRulePriority   = 512
	fromContainerRulePriority = 1536
	mainRouteTableID          = 254
)

const (
	gcIntervalPeriod         = 120 * time.Second
	policyRuleExpiredTimeout = 300 * time.Second
)

type policyRuleKey struct {
	containerIP   string
	isToContainer bool
}

type policyRule struct {
	rule       *netlink.Rule
	activeTime time.Time
}

type Controller struct {
	netlink netlinkwrapper.Interface
	clock   clock.Clock

	// policy route gc
	possibleLeakedRules map[policyRuleKey]policyRule
	routeDstLinkEntries map[string]string
}

func New() *Controller {
	return &Controller{
		netlink:             netlinkwrapper.New(),
		clock:               clock.RealClock{},
		possibleLeakedRules: make(map[policyRuleKey]policyRule),
		routeDstLinkEntries: make(map[string]string),
	}
}

func (c *Controller) GC() {
	err := wait.PollImmediateInfinite(gcIntervalPeriod, func() (bool, error) {
		ctx := log.NewContext()

		err := c.gcLeakedPolicyRule(ctx)
		if err != nil {
			log.Errorf(ctx, "failed to gc leaked policy rule: %v", err)
		}

		return false, nil
	})

	if err != nil {
		log.Errorf(context.TODO(), "failed to run gc controller: %v", err)
	}
}

func (c *Controller) gcLeakedPolicyRule(ctx context.Context) error {
	routes, err := c.netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		log.Errorf(ctx, "failed to list ip routes: %v", err)
		return err
	}

	rules, err := c.netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		log.Errorf(ctx, "failed to list ip rules: %v", err)
		return err
	}

	// build route cache
	c.buildRouteDstLinkEntries(routes)
	log.V(6).Infof(ctx, "ip route show result: ", c.routeDstLinkEntries)

	for idx, rule := range rules {
		var containerIP string

		// to container rule
		if rule.Priority == toContainerRulePriority {
			if rule.Dst == nil || rule.Dst.IP == nil {
				continue
			}
			containerIP = rule.Dst.IP.String()

			if rule.Table != mainRouteTableID {
				log.Warningf(ctx, "found unknown rule to %v", containerIP)
				continue
			}

			c.addToPossibleLeakedPolicyRule(ctx, &rules[idx], containerIP, true)
		}

		// from container rule
		if rule.Priority == fromContainerRulePriority {
			if rule.Src == nil || rule.Src.IP == nil {
				continue
			}
			containerIP = rule.Src.IP.String()

			c.addToPossibleLeakedPolicyRule(ctx, &rules[idx], containerIP, false)
		}
	}

	err = c.pruneExpiredPolicyRule(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) addToPossibleLeakedPolicyRule(ctx context.Context, rule *netlink.Rule, containerIP string, isToContainer bool) {
	key := policyRuleKey{containerIP: containerIP, isToContainer: false}

	_, exist := c.routeDstLinkEntries[containerIP]
	if !exist {
		// if route not exist, maybe a leaked ip rule, add to possible leaked rule pool
		if _, ok := c.possibleLeakedRules[key]; !ok {
			c.possibleLeakedRules[key] = policyRule{
				rule:       rule,
				activeTime: c.clock.Now(),
			}

			if isToContainer {
				log.Infof(ctx, "found leaked ip rule to container: %+v to %v", *rule, containerIP)
			} else {
				log.Infof(ctx, "found leaked ip rule from container: %+v", rule)
			}
		}
	} else {
		// if route exist, maybe a false positive
		delete(c.possibleLeakedRules, key)
	}
}

func (c *Controller) pruneExpiredPolicyRule(ctx context.Context) error {
	var leakedRules []*netlink.Rule

	if len(c.possibleLeakedRules) != 0 {
		log.Infof(ctx, "possible leaked rules: %+v", c.possibleLeakedRules)
	}

	for k, r := range c.possibleLeakedRules {
		// if rule has existed for quite long time, we should cleanup
		if c.clock.Now().Sub(r.activeTime) > policyRuleExpiredTimeout {
			leakedRules = append(leakedRules, r.rule)
			delete(c.possibleLeakedRules, k)
		}
	}

	// let's clean
	for _, rule := range leakedRules {
		// must be cautious when deleting rule
		if rule.Priority == toContainerRulePriority || rule.Priority == fromContainerRulePriority {
			log.Infof(ctx, "try deleting leaked ip rule: %v", *rule)
			err := c.netlink.RuleDel(rule)
			if err != nil && !netlinkwrapper.IsNotExistError(err) {
				log.Errorf(ctx, "failed to delete leaked ip rule %v:", *rule)
			}
		}
	}

	return nil
}

func (c *Controller) buildRouteDstLinkEntries(routes []netlink.Route) {
	c.routeDstLinkEntries = make(map[string]string)

	for _, rt := range routes {
		if rt.Dst != nil && rt.Dst.IP != nil {
			dst := rt.Dst.IP
			intf, err := c.netlink.LinkByIndex(rt.LinkIndex)
			if dst != nil && err == nil {
				c.routeDstLinkEntries[dst.String()] = intf.Attrs().Name
			}
		}
	}
}
