/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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
package endpoint

import (
	"context"
	"fmt"
	"net"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/pststrategy"
	"github.com/sirupsen/logrus"
)

// DirectIPAllocatorProvider defines the functions of IPAM provider front-end
// these are implemented by e.g. pkg/ipam/allocator/{privatecloudbase}.
type DirectIPAllocatorProvider interface {
	Init(ctx context.Context) error
	StartEndpointManager(ctx context.Context, getterUpdater CCEEndpointGetterUpdater) (EndpointEventHandler, error)
}

// EndpointEventHandler should implement the behavior to handle CCEEndpoint
type EndpointEventHandler interface {
	Create(resource *ccev2.CCEEndpoint) error
	Update(resource *ccev2.CCEEndpoint) error
	Delete(namespace, endpointName string) error
	Resync(context.Context) error
}

type EndpointMunalAllocatorProvider interface {
	AllocateIP(ctx context.Context, log *logrus.Entry, resource *ccev2.CCEEndpoint) error
}

type pstsAllocatorProvider struct {
	*EndpointManager
}

// AllocateIP implements EndpointMunalAllocatorProvider
func (provider *pstsAllocatorProvider) AllocateIP(ctx context.Context, log *logrus.Entry, resource *ccev2.CCEEndpoint) error {
	var (
		owner          = resource.Namespace + "/" + resource.Name
		status         = &resource.Status
		releaseIPFuncs []func()
		err            error

		// reuseIPAction is the action to reuse IP
		action = &DirectIPAction{
			NodeName: resource.Spec.Network.IPAllocation.NodeIP,
			Owner:    owner,
		}
	)
	log = log.WithField("psts", resource.Spec.Network.IPAllocation.PSTSName).WithField("endpoint", owner)
	log.Info("start allocate ip from psts")

	psts, err := provider.pstsLister.PodSubnetTopologySpreads(resource.Namespace).Get(resource.Spec.Network.IPAllocation.PSTSName)
	if err != nil {
		log.Errorf("failed to get psts: %v", err)
		return err
	}

	operation, err := provider.directIPAllocator.NodeEndpoint(resource)
	if err != nil {
		log.Errorf("failed to get node endpoint %v", err)
		return err
	}

	if len(status.Networking.Addressing) == 0 {
		subnets, err := filterAvailableSubnet(psts, provider, log, operation)
		if err != nil {
			return err
		}
		if pststrategy.EnableReuseIPPSTS(psts) {
			// allocate ip from local pool
			localAllocator := &localAllocator{
				localPool: provider.localPool,
				subnets:   subnets,
				psts:      psts,
				log:       log.WithField("mod", "localAllocator"),
			}
			ipv4Address, ipv6Address, release := localAllocator.allocateNext()
			if ipv4Address == nil {
				log.WithField("step", "allocateFromLocalPool").Error("no available ipv4 ip")
				return fmt.Errorf("no available ipv4 ip")
			}

			releaseIPFuncs = append(releaseIPFuncs, release...)
			log = log.WithField("ipv4", logfields.Repr(ipv4Address)).WithField("ipv6", logfields.Repr(ipv6Address))
			log.WithField("step", "allocate local ip").Info("allocate local ip success")

			action.Addressing = append(action.Addressing, ipv4Address)
			action.SubnetID = ipv4Address.Subnet
			action.Interface = ipv4Address.Interface
			if ipv6Address != nil {
				action.Addressing = append(action.Addressing, ipv6Address)
				action.SubnetID = ipv6Address.Subnet
				action.Interface = ipv6Address.Interface
			}
		} else {
			action.SubnetID = subnets[0].Name
		}
	} else {
		// reuse ip
		action.Addressing = status.Networking.Addressing
		action.SubnetID = status.Networking.Addressing[0].Subnet
		action.Interface = status.Networking.Addressing[0].Interface
	}
	defer func() {
		if err != nil && len(releaseIPFuncs) > 0 {
			for _, release := range releaseIPFuncs {
				release()
			}
		}
	}()
	log = log.WithField("step", "allocate remote psts ip").
		WithField("action", logfields.Repr(action))

	err = operation.AllocateIP(ctx, action)
	if err != nil {
		log.WithError(err).Errorf("failed to allocate psts ip")
		return err
	}

	log.Info("allocate remote psts ip success")

	status.Networking.Addressing = action.Addressing
	status.NodeSelectorRequirement = action.NodeSelectorRequirement
	return nil
}

// filterAvailableSubnet filters available subnets from psts and remote delegate
func filterAvailableSubnet(psts *ccev2.PodSubnetTopologySpread, provider *pstsAllocatorProvider, log *logrus.Entry, operation DirectEndpointOperation) ([]*ccev1.Subnet, error) {
	var subnetIDs []string
	for sbnID := range psts.Status.AvailableSubnets {
		subnetIDs = append(subnetIDs, sbnID)
	}

	bsubnets := operation.FilterAvailableSubnetIds(subnetIDs, 1)
	if len(bsubnets) == 0 {
		log.WithField("step", "FilterAvailableSubnet").Error("no available subnet")
		return nil, fmt.Errorf("no available subnet")
	}

	var subnets []*ccev1.Subnet
	for _, bsbn := range bsubnets {

		subnets = append(subnets, bsbn.Subnet)
	}
	return subnets, nil
}

type localAllocator struct {
	localPool *localPool

	subnets []*ccev1.Subnet
	psts    *ccev2.PodSubnetTopologySpread
	log     *logrus.Entry
}

// allocate allocates IPs from local pool
// this method will allocate IPv4 and IPv6 IPs at the same time
func (local *localAllocator) allocateNext() (ipv4, ipv6 *ccev2.AddressPair, releaseIPFuncs []func()) {
	// allocate ipv4
	ipv4Array, release := local.allocateFromLocalPool(ccev2.IPv4Family, 1)
	if len(ipv4Array) == 0 {
		local.log.WithField("step", "allocateFromLocalPool").Error("no available ipv4 ip")
		return
	}
	// pick the first ip from ipv4Array
	for sbnID, ips := range ipv4Array {
		ip := ips[0]
		ipv4 = &ccev2.AddressPair{
			Family: ccev2.IPv4Family,
			IP:     ip.String(),
			Subnet: sbnID,
		}
		releaseIPFuncs = append(releaseIPFuncs, release...)
		break
	}

	// allocate ipv6
	if operatorOption.Config.EnableIPv6 {
		ipv6Array, releaseF := local.allocateFromLocalPool(ccev2.IPv6Family, 1)
		if len(ipv6Array) == 0 {
			local.log.WithField("step", "allocateFromLocalPool").Error("no available ipv6 ip")
		} else {
			// pick the first ip from ipv6Array
			for sbnID, ips := range ipv6Array {
				ip := ips[0]
				ipv6 = &ccev2.AddressPair{
					Family: ccev2.IPv6Family,
					IP:     ip.String(),
					Subnet: sbnID,
				}
				releaseIPFuncs = append(releaseIPFuncs, releaseF...)
				break
			}
		}
	}
	return
}

// allocateFromLocalPool allocates IPs from local pool
// num 1 means allocate one ip, and this function usual usage is to return only one IP address
func (local *localAllocator) allocateFromLocalPool(family ccev2.IPFamily, num int) (map[string][]net.IP, []func()) {
	var (
		subnetAvailableIPs          = make(map[string][]net.IP)
		releaseIPFuncs     []func() = make([]func(), 0)
		count                       = 0
	)
	for _, subnet := range local.subnets {
		allocator := local.localPool.getPool(subnet.Name, string(family))
		if allocator == nil {
			local.log.WithField("step", "get customer range allocator").
				WithField("subnet", subnet.Name).
				Error("no available pool")
			continue
		}

		// allocate ip from local pool
		allocate := func(startIP, endIP net.IP) bool {
			if count >= num {
				return false
			}
			ips, err := allocator.ReservedAllocateMany(startIP, endIP, 1)
			if err != nil {
				local.log.WithField("step", "allocate local ip").WithError(err).Debug("allocate local ip failed")
				return false
			}
			local.log.WithField("step", "allocate local ip").Debug("allocate local ip success")
			subnetAvailableIPs[subnet.Name] = append(subnetAvailableIPs[subnet.Name], ips...)
			releaseIPFuncs = append(releaseIPFuncs, func() {
				allocator.ReleaseMany(ips)
			})
			count++
			return true
		}

		customs := local.psts.Spec.Subnets[subnet.Name]
		if len(customs) == 0 {
			allocate(nil, nil)
		}
		for _, custom := range customs {
			if custom.Family != family {
				continue
			}
			if count >= num {
				break
			}
			// allocate ip from customer range
			if custom.Range == nil {
				allocate(nil, nil)
			} else {
				for _, rg := range custom.Range {
					if count >= num {
						break
					}
					allocate(net.ParseIP(rg.Start), net.ParseIP(rg.End))
				}
			}

		}
	}
	return subnetAvailableIPs, releaseIPFuncs
}

var _ EndpointMunalAllocatorProvider = &pstsAllocatorProvider{}
