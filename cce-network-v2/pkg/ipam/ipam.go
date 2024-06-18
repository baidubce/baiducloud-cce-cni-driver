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

package ipam

import (
	"net"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/types"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

var (
	log = logging.DefaultLogger.WithField(logfields.LogSubsys, "ipam")
)

type ErrAllocation error

// Family is the type describing all address families support by the IP
// allocation manager
type Family string

const (
	IPv6 Family = "ipv6"
	IPv4 Family = "ipv4"
)

// DeriveFamily derives the address family of an IP
func DeriveFamily(ip net.IP) Family {
	if ip.To4() == nil {
		return IPv6
	}
	return IPv4
}

// Configuration is the configuration passed into the IPAM subsystem
type Configuration interface {
	// IPv4Enabled must return true when IPv4 is enabled
	IPv4Enabled() bool

	// IPv6 must return true when IPv6 is enabled
	IPv6Enabled() bool

	// IPAMMode returns the IPAM mode
	IPAMMode() string

	// HealthCheckingEnabled must return true when health-checking is
	// enabled
	HealthCheckingEnabled() bool

	// UnreachableRoutesEnabled returns true when unreachable-routes is
	// enabled
	UnreachableRoutesEnabled() bool

	// SetIPv4NativeRoutingCIDR is called by the IPAM module to announce
	// the native IPv4 routing CIDR if it exists
	SetIPv4NativeRoutingCIDR(cidr *cidr.CIDR)

	// IPv4NativeRoutingCIDR is called by the IPAM module retrieve
	// the native IPv4 routing CIDR if it exists
	GetIPv4NativeRoutingCIDR() *cidr.CIDR

	//GetCCEEndpointGC interval of single machine recycling endpoint
	GetCCEEndpointGC() time.Duration

	// GetFixedIPTimeout FixedIPTimeout Timeout for waiting for the fixed IP assignment to succeed
	GetFixedIPTimeout() time.Duration
}

// Owner is the interface the owner of an IPAM allocator has to implement
type Owner interface {
	// UpdateNetResourceSetResource is called to create/update the NetResourceSet
	// resource. The function must block until the custom resource has been
	// created.
	UpdateNetResourceSetResource()

	// LocalAllocCIDRsUpdated informs the agent that the local allocation CIDRs have
	// changed.
	LocalAllocCIDRsUpdated(ipv4AllocCIDRs, ipv6AllocCIDRs []*cidr.CIDR)

	// GetResourceType
	ResourceType() string
}

// K8sEventRegister is used to register and handle events as they are processed
// by K8s controllers.
type K8sEventRegister interface {
	// K8sEventReceived is called to do metrics accounting for received
	// Kubernetes events, as well as calculating timeouts for k8s watcher
	// cache sync.
	K8sEventReceived(apiGroupResourceName string, scope string, action string, valid, equal bool)

	// K8sEventProcessed is called to do metrics accounting for each processed
	// Kubernetes event.
	K8sEventProcessed(scope string, action string, status bool)

	// RegisterNetResourceSetSubscriber allows registration of subscriber.NetResourceSet
	// implementations. Events for all NetResourceSet events (not just the local one)
	// will be sent to the subscriber.
	RegisterNetResourceSetSubscriber(s subscriber.NetResourceSet)
}

type MtuConfiguration interface {
	GetDeviceMTU() int
}

// NewIPAM returns a new IP address manager
func NewIPAM(networkResourceSetName string, nodeAddressing types.NodeAddressing, c Configuration, owner Owner, k8sEventReg K8sEventRegister, mtuConfig MtuConfiguration) *IPAM {
	ipam := &IPAM{
		nodeAddressing:   nodeAddressing,
		config:           c,
		owner:            map[string]string{},
		expirationTimers: map[string]string{},
		blacklist: IPBlacklist{
			ips: map[string]string{},
		},
	}

	switch c.IPAMMode() {
	case ipamOption.IPAMKubernetes, ipamOption.IPAMClusterPool:
		log.WithFields(logrus.Fields{
			logfields.V4Prefix: nodeAddressing.IPv4().AllocationCIDR(),
			logfields.V6Prefix: nodeAddressing.IPv6().AllocationCIDR(),
		}).Infof("Initializing %s IPAM", c.IPAMMode())

		if c.IPv6Enabled() {
			ipam.IPv6Allocator = newHostScopeAllocator(nodeAddressing.IPv6().AllocationCIDR().IPNet)
		}

		if c.IPv4Enabled() {
			ipam.IPv4Allocator = newHostScopeAllocator(nodeAddressing.IPv4().AllocationCIDR().IPNet)
		}
	case ipamOption.IPAMClusterPoolV2, ipamOption.IPAMVpcRoute:
		log.Info("Initializing ClusterPool v2 IPAM")

		if c.IPv6Enabled() {
			ipam.IPv6Allocator = newClusterPoolAllocator(IPv6, c, owner, k8sEventReg)
		}
		if c.IPv4Enabled() {
			ipam.IPv4Allocator = newClusterPoolAllocator(IPv4, c, owner, k8sEventReg)
		}
	case ipamOption.IPAMCRD, ipamOption.IPAMVpcEni, ipamOption.IPAMRdma, ipamOption.IPAMPrivateCloudBase:
		log.Info("Initializing CRD-based IPAM")
		if c.IPv6Enabled() {
			ipam.IPv6Allocator = newCRDAllocator(networkResourceSetName, IPv6, c, owner, k8sEventReg, mtuConfig)
		}

		if c.IPv4Enabled() {
			ipam.IPv4Allocator = newCRDAllocator(networkResourceSetName, IPv4, c, owner, k8sEventReg, mtuConfig)
		}
	case ipamOption.IPAMDelegatedPlugin:
		log.Info("Initializing no-op IPAM since we're using a CNI delegated plugin")
		if c.IPv6Enabled() {
			ipam.IPv6Allocator = &noOpAllocator{}
		}
		if c.IPv4Enabled() {
			ipam.IPv4Allocator = &noOpAllocator{}
		}
	default:
		log.Fatalf("Unknown IPAM backend %s", c.IPAMMode())
	}

	return ipam
}

func nextIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// BlacklistIP ensures that a certain IP is never allocated. It is preferred to
// use BlacklistIP() instead of allocating the IP as the allocation block can
// change and suddenly cover the IP to be blacklisted.
func (ipam *IPAM) BlacklistIP(ip net.IP, owner string) {
	ipam.allocatorMutex.Lock()
	ipam.blacklist.ips[ip.String()] = owner
	ipam.allocatorMutex.Unlock()
}

func (ipam *IPAM) RestoreFinished() {
	if ipam.IPv4Allocator != nil {
		ipam.IPv4Allocator.RestoreFinished()
	}
	if ipam.IPv6Allocator != nil {
		ipam.IPv6Allocator.RestoreFinished()
	}
}
