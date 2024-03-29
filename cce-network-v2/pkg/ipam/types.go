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

	"github.com/davecgh/go-spew/spew"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
)

// AllocationResult is the result of an allocation
type AllocationResult struct {
	// IP is the allocated IP
	IP net.IP

	// CIDRs is a list of all CIDRs to which the IP has direct access to.
	// This is primarily useful if the IP has been allocated out of a VPC
	// subnet range and the VPC provides routing to a set of CIDRs in which
	// the IP is routable.
	CIDRs []string

	// PrimaryMAC is the MAC address of the primary interface. This is useful
	// when the IP is a secondary address of an interface which is
	// represented on the node as a Linux device and all routing of the IP
	// must occur through that master interface.
	PrimaryMAC string

	// GatewayIP is the IP of the gateway which must be used for this IP.
	// If the allocated IP is derived from a VPC, then the gateway
	// represented the gateway of the VPC or VPC subnet.
	GatewayIP string

	// ExpirationUUID is the UUID of the expiration timer. This field is
	// only set if AllocateNextWithExpiration is used.
	ExpirationUUID string

	// InterfaceNumber is a field for generically identifying an interface.
	// This is only useful in ENI mode.
	InterfaceNumber string
}

// Allocator is the interface for an IP allocator implementation
type Allocator interface {
	// Allocate allocates a specific IP or fails
	Allocate(ip net.IP, owner string) (*AllocationResult, error)

	// AllocateWithoutSyncUpstream allocates a specific IP without syncing
	// upstream or fails
	AllocateWithoutSyncUpstream(ip net.IP, owner string) (*AllocationResult, error)

	// Release releases a previously allocated IP or fails
	Release(ip net.IP) error

	// AllocateNext allocates the next available IP or fails if no more IPs
	// are available
	AllocateNext(owner string) (*AllocationResult, error)

	// AllocateNextWithoutSyncUpstream allocates the next available IP without syncing
	// upstream or fails if no more IPs are available
	AllocateNextWithoutSyncUpstream(owner string) (*AllocationResult, error)

	// Dump returns a map of all allocated IPs with the IP represented as
	// key in the map. Dump must also provide a status one-liner to
	// represent the overall status, e.g. number of IPs allocated and
	// overall health information if available.
	Dump() (map[string]string, string)

	// RestoreFinished marks the status of restoration as done
	RestoreFinished()
}

// IPBlacklist is a structure used to store information related to blacklisted
// IPs and IPNetworks.
type IPBlacklist struct {
	// A hashmap containing IP and the corresponding owners.
	ips map[string]string
}

// IPAM is the configuration used for a particular IPAM type.
type IPAM struct {
	nodeAddressing types.NodeAddressing
	config         Configuration

	IPv6Allocator Allocator
	IPv4Allocator Allocator

	// owner maps an IP to the owner
	owner map[string]string

	// expirationTimers is a map of all expiration timers. Each entry
	// represents a IP allocation which is protected by an expiration
	// timer.
	expirationTimers map[string]string

	// mutex covers access to all members of this struct
	allocatorMutex lock.RWMutex

	blacklist IPBlacklist
}

// DebugStatus implements debug.StatusObject to provide debug status collection
// ability
func (ipam *IPAM) DebugStatus() string {
	if ipam == nil {
		return "<nil>"
	}

	ipam.allocatorMutex.RLock()
	str := spew.Sdump(ipam)
	ipam.allocatorMutex.RUnlock()
	return str
}

// GetVpcCIDRs returns all the CIDRs associated with the VPC this node belongs to.
// This works only cloud provider IPAM modes and returns nil for other modes.
// sharedNodeStore must be initialized before calling this method.
func (ipam *IPAM) GetVpcCIDRs() (vpcCIDRs []*cidr.CIDR) {
	sharedNetResourceSetStore.mutex.RLock()
	defer sharedNetResourceSetStore.mutex.RUnlock()
	primary, secondary := deriveVpcCIDRs(sharedNetResourceSetStore.ownNode)
	if primary == nil {
		return nil
	}
	if secondary == nil {
		return []*cidr.CIDR{primary}
	}
	return append(secondary, primary)
}
