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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"time"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const maxVPCConflictRetries = 10

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
		owner  = resource.Namespace + "/" + resource.Name
		status = &resource.Status
		// lifecycleID scopes PSTS allocation tokens to a single sandbox/container attempt.
		lifecycleID string

		// reuseIPAction is the action to reuse IP
		action = &DirectIPAction{
			NodeName: resource.Spec.Network.IPAllocation.NodeName,
			Owner:    owner,
		}
	)
	log = log.WithField("psts", resource.Spec.Network.IPAllocation.PSTSName).WithField("endpoint", owner)
	lifecycleID, err := pstsAllocationLifecycleID(resource)
	if err != nil {
		log.WithError(err).Error("failed to resolve PSTS sandbox lifecycle identifier")
		return err
	}
	log.WithField("containerID", lifecycleID).Debug("selected PSTS allocation lifecycle identifier")
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
		// New pod: allocate new IP
		return provider.allocateNewIP(ctx, psts, operation, action, status, lifecycleID, log)
	}

	// Reuse existing IP
	return provider.reuseExistingIP(ctx, operation, action, status, psts.Name, lifecycleID, log)
}

// allocateNewIP allocates a new IP for a pod from PSTS
func (provider *pstsAllocatorProvider) allocateNewIP(
	ctx context.Context,
	psts *ccev2.PodSubnetTopologySpread,
	operation DirectEndpointOperation,
	action *DirectIPAction,
	status *ccev2.EndpointStatus,
	lifecycleID string,
	log *logrus.Entry,
) error {
	subnets, err := filterAvailableSubnet(psts, log, operation)
	if err != nil {
		return err
	}

	// Separate subnets with range/CIDRs and without
	rangeSubnets, noRangeSubnets := separateSubnetsByRange(psts, subnets)

	// Setup deferred release on error
	var releaseIPFuncs []func()
	defer func() {
		if err != nil && len(releaseIPFuncs) > 0 {
			for _, release := range releaseIPFuncs {
				release()
			}
		}
	}()

	// Try range subnets first (local pool allocation)
	if len(rangeSubnets) > 0 {
		allocated, releaseFuncs, err := provider.tryAllocateFromRangeSubnets(ctx, psts, rangeSubnets, operation, action, lifecycleID, log)
		if allocated {
			releaseIPFuncs = append(releaseIPFuncs, releaseFuncs...)
			status.Networking.Addressing = action.Addressing
			status.NodeSelectorRequirement = action.NodeSelectorRequirement
			return nil
		}
		if err != nil {
			return err
		}
	}

	// Range subnets exhausted - only fallback if there are no-range subnets available
	if len(noRangeSubnets) == 0 {
		return fmt.Errorf("PSTS %s/%s IP pool exhausted: all IPs in the configured range are in use, please expand the IP range or reduce pod count",
			psts.Namespace, psts.Name)
	}

	// Fallback to no-range subnets (VPC random allocation)
	return provider.allocateFromNoRangeSubnets(ctx, psts.Name, noRangeSubnets, operation, action, status, lifecycleID, log)
}

// separateSubnetsByRange separates subnets into those with custom ranges and those without
func separateSubnetsByRange(psts *ccev2.PodSubnetTopologySpread, subnets []*ccev1.Subnet) (rangeSubnets, noRangeSubnets []*ccev1.Subnet) {
	for _, subnet := range subnets {
		if hasCustomAllocationForSubnet(psts, subnet.Name) {
			rangeSubnets = append(rangeSubnets, subnet)
		} else {
			noRangeSubnets = append(noRangeSubnets, subnet)
		}
	}
	return
}

func filterOutSubnetByName(subnets []*ccev1.Subnet, subnetName string) []*ccev1.Subnet {
	filtered := make([]*ccev1.Subnet, 0, len(subnets))
	for _, subnet := range subnets {
		if subnet == nil || subnet.Name == subnetName {
			continue
		}
		filtered = append(filtered, subnet)
	}
	return filtered
}

// tryAllocateFromRangeSubnets attempts to allocate IP from subnets with custom ranges
// Returns: (allocated, releaseFuncs, error)
func (provider *pstsAllocatorProvider) tryAllocateFromRangeSubnets(
	ctx context.Context,
	psts *ccev2.PodSubnetTopologySpread,
	rangeSubnets []*ccev1.Subnet,
	operation DirectEndpointOperation,
	action *DirectIPAction,
	lifecycleID string,
	log *logrus.Entry,
) (bool, []func(), error) {
	remainingSubnets := append([]*ccev1.Subnet(nil), rangeSubnets...)
	localAllocator := &localAllocator{
		localPool: provider.localPool,
		subnets:   remainingSubnets,
		psts:      psts,
		log:       log.WithField("mod", "localAllocator"),
	}

	// Try allocation in a loop, marking conflicts and retrying next IP
	conflictAttempts := 0
	for {
		localAllocator.subnets = remainingSubnets
		ipv4Address, ipv6Address, release := localAllocator.allocateNext()
		if ipv4Address == nil {
			log.WithField("step", "allocateFromLocalPool").Warn("no available ipv4 ip in range subnets, fallback to no-range subnets")
			return false, nil, nil // Not allocated, no error, caller should fallback
		}

		// Prepare a trial action for this candidate.
		// Keep the input action unchanged until allocation succeeds so fallback logic
		// can continue from the original state.
		trialAction := *action
		if action.NodeSelectorRequirement != nil {
			trialAction.NodeSelectorRequirement = append([]corev1.NodeSelectorRequirement(nil), action.NodeSelectorRequirement...)
		}
		trialAction.Addressing = append(make(ccev2.AddressPairList, 0, 2), ipv4Address)
		trialAction.SubnetID = ipv4Address.Subnet
		trialAction.Interface = ipv4Address.Interface
		if ipv6Address != nil {
			trialAction.Addressing = append(trialAction.Addressing, ipv6Address)
		}
		trialAction.ClientToken = generatePSTSAllocateClientToken(action.Owner, psts.Name, lifecycleID, trialAction.SubnetID, trialAction.Addressing)

		log := log.WithField("ipv4", logfields.Repr(ipv4Address)).
			WithField("ipv6", logfields.Repr(ipv6Address)).
			WithField("step", "allocate remote psts ip")
		log.Info("selected candidate IP from local pool")

		// Try to allocate IP from VPC
		err := operation.AllocateIP(ctx, &trialAction)
		if err == nil {
			// Success! IP is now truly occupied
			*action = trialAction
			provider.resetCandidateCooldown(psts, ipv4Address, log)
			provider.resetCandidateCooldown(psts, ipv6Address, log)
			log.Info("allocate remote psts ip success")
			return true, release, nil
		}

		// Check if error is due to IP conflict
		if isIPConflictError(err) {
			log.WithError(err).Warnf("VPC IP conflict for %s, releasing from local pool and trying next", ipv4Address.IP)
			provider.markCandidateUnavailable(psts, ipv4Address, log)
			provider.markCandidateUnavailable(psts, ipv6Address, log)
			// Release the conflicting IP from local pool
			for _, releaseFunc := range release {
				releaseFunc()
			}
			conflictAttempts++
			if conflictAttempts >= maxVPCConflictRetries {
				return false, nil, fmt.Errorf("vpc ip conflict after %d attempts", maxVPCConflictRetries)
			}
			// Continue to next IP in ring
			continue
		}
		if cloud.IsErrorSubnetHasNoMoreIP(err) {
			log.WithError(err).Warnf("range subnet %s has no more IP in VPC, skipping this subnet for current allocation", ipv4Address.Subnet)
			for _, releaseFunc := range release {
				releaseFunc()
			}
			remainingSubnets = filterOutSubnetByName(remainingSubnets, ipv4Address.Subnet)
			if len(remainingSubnets) == 0 {
				log.WithField("step", "allocateFromLocalPool").Warn("range subnets exhausted after filtering VPC no-more-ip subnets, fallback to no-range subnets")
				return false, nil, nil
			}
			continue
		}

		// Non-conflict error, fail immediately
		log.WithError(err).Errorf("failed to allocate psts ip (non-conflict error)")
		for _, releaseFunc := range release {
			releaseFunc()
		}
		return false, nil, err
	}
}

// allocateFromNoRangeSubnets allocates IP from subnets without custom ranges (VPC random)
func (provider *pstsAllocatorProvider) allocateFromNoRangeSubnets(
	ctx context.Context,
	pstsName string,
	noRangeSubnets []*ccev1.Subnet,
	operation DirectEndpointOperation,
	action *DirectIPAction,
	status *ccev2.EndpointStatus,
	lifecycleID string,
	log *logrus.Entry,
) error {
	if len(noRangeSubnets) == 0 {
		return fmt.Errorf("no available subnets for IP allocation")
	}

	log.WithField("step", "allocate from no-range subnet").Info("allocating from VPC random allocation")
	action.SubnetID = noRangeSubnets[0].Name
	action.ClientToken = generatePSTSAllocateClientToken(action.Owner, pstsName, lifecycleID, action.SubnetID, nil)

	err := operation.AllocateIP(ctx, action)
	if err != nil {
		log.WithError(err).Errorf("failed to allocate ip from no-range subnet")
		return err
	}

	log.WithField("addrs", logfields.Repr(action.Addressing)).Info("allocate ip from no-range subnet success")
	status.Networking.Addressing = action.Addressing
	status.NodeSelectorRequirement = action.NodeSelectorRequirement
	return nil
}

// reuseExistingIP reuses existing IP for pod restart
func (provider *pstsAllocatorProvider) reuseExistingIP(
	ctx context.Context,
	operation DirectEndpointOperation,
	action *DirectIPAction,
	status *ccev2.EndpointStatus,
	pstsName string,
	lifecycleID string,
	log *logrus.Entry,
) error {
	action.Addressing = status.Networking.Addressing
	action.SubnetID = status.Networking.Addressing[0].Subnet
	action.Interface = status.Networking.Addressing[0].Interface
	action.ClientToken = generatePSTSAllocateClientToken(action.Owner, pstsName, lifecycleID, action.SubnetID, action.Addressing)

	// For IP reuse, allocation failure is NOT tolerable
	log = log.WithField("step", "allocate remote psts ip").
		WithField("action", logfields.Repr(action))

	err := operation.AllocateIP(ctx, action)
	if err != nil {
		log.WithError(err).Errorf("failed to allocate psts ip for existing CEP")
		return err
	}

	log.Info("allocate remote psts ip success (reuse)")
	status.Networking.Addressing = action.Addressing
	status.NodeSelectorRequirement = action.NodeSelectorRequirement

	return nil
}

// isIPConflictError checks if the error is due to IP already in use
func isIPConflictError(err error) bool {
	if err == nil {
		return false
	}
	// Besides standard PrivateIpInUse, PSTS fixed-mode cross-subnet candidate allocation
	// may get a specific BadRequest when the candidate IP is already occupied by VPC
	// system resources (e.g. subnet route reserved IP). In this path, treat it as conflict.
	return cloud.IsErrorPrivateIPInUse(err) || cloud.IsErrorPSTSCrossSubnetCandidateConflict(err)
}

func (provider *pstsAllocatorProvider) markCandidateUnavailable(
	psts *ccev2.PodSubnetTopologySpread,
	addr *ccev2.AddressPair,
	log *logrus.Entry,
) {
	if provider == nil || provider.localPool == nil || psts == nil || addr == nil || addr.IP == "" || addr.Subnet == "" {
		return
	}
	ip := net.ParseIP(addr.IP)
	if ip == nil {
		return
	}

	poolID := getPSTSPoolID(psts.Namespace, psts.Name, addr.Subnet, string(addr.Family))
	pool := provider.localPool.getPSTSPoolByID(poolID)
	if pool == nil {
		return
	}

	failures, backoff, until, ok := pool.markCandidateUnavailable(ip)
	if !ok {
		return
	}
	log.WithFields(logrus.Fields{
		"poolID":      poolID,
		"ip":          addr.IP,
		"failures":    failures,
		"cooldown":    backoff.String(),
		"availableAt": until.Format(time.RFC3339),
	}).WithField("step", "markCandidateUnavailable").
		Warn("candidate IP is temporarily unavailable in local pool")
}

func (provider *pstsAllocatorProvider) resetCandidateCooldown(
	psts *ccev2.PodSubnetTopologySpread,
	addr *ccev2.AddressPair,
	log *logrus.Entry,
) {
	if provider == nil || provider.localPool == nil || psts == nil || addr == nil || addr.IP == "" || addr.Subnet == "" {
		return
	}
	ip := net.ParseIP(addr.IP)
	if ip == nil {
		return
	}

	poolID := getPSTSPoolID(psts.Namespace, psts.Name, addr.Subnet, string(addr.Family))
	pool := provider.localPool.getPSTSPoolByID(poolID)
	if pool == nil {
		return
	}

	pool.resetCandidateCooldown(ip)
	log.WithFields(logrus.Fields{
		"poolID": poolID,
		"ip":     addr.IP,
	}).WithField("step", "resetCandidateCooldown").
		Debug("reset candidate cooldown after successful allocation")
}

// filterAvailableSubnet filters available subnets from psts and remote delegate
func filterAvailableSubnet(psts *ccev2.PodSubnetTopologySpread, log *logrus.Entry, operation DirectEndpointOperation) ([]*ccev1.Subnet, error) {
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

// allocateFromLocalPool allocates candidate IPs from local pool (not yet allocated in VPC)
// num 1 means allocate one ip, and this function usual usage is to return only one IP address
// Returns: map[subnetID][]candidateIPs and release functions to clear local pool marks on failure
func (local *localAllocator) allocateFromLocalPool(family ccev2.IPFamily, num int) (map[string][]net.IP, []func()) {
	var (
		subnetCandidateIPs          = make(map[string][]net.IP) // Candidate IPs (marked in local pool, not yet allocated in VPC)
		releaseIPFuncs     []func() = make([]func(), 0)
		count                       = 0 // count means the number of candidate IPs selected
	)
	for _, subnet := range local.subnets {
		// Use PSTS-specific pool with ring allocation (length-prefixed poolID format)
		pstsPoolID := getPSTSPoolID(local.psts.Namespace, local.psts.Name, subnet.Name, string(family))
		pstsPool := local.localPool.getPSTSPoolByID(pstsPoolID)
		if pstsPool == nil {
			local.log.WithField("step", "get psts pool allocator").
				WithField("subnet", subnet.Name).
				WithField("poolID", pstsPoolID).
				Warn("psts pool not found for subnet, trying next subnet")
			continue
		}

		// Allocate using ring-based allocation
		for i := 0; i < num && count < num; i++ {
			ip, release, err := pstsPool.allocateNext()
			if err != nil {
				local.log.WithField("step", "allocate psts ip").WithError(err).Debug("allocate psts ip failed")
				break
			}
			local.log.WithField("step", "allocate psts ip").Debugf("selected candidate IP %s from local pool", ip.String())
			subnetCandidateIPs[subnet.Name] = append(subnetCandidateIPs[subnet.Name], ip)
			releaseIPFuncs = append(releaseIPFuncs, release)
			count++
		}

		if count >= num {
			break
		}
	}
	return subnetCandidateIPs, releaseIPFuncs
}

// hasCustomAllocationForSubnet checks if a specific subnet has Range or CIDRs specified in PSTS
func hasCustomAllocationForSubnet(psts *ccev2.PodSubnetTopologySpread, subnetID string) bool {
	ranges, ok := psts.Spec.Subnets[subnetID]
	if !ok || len(ranges) == 0 {
		return false
	}
	for _, r := range ranges {
		if len(r.Range) > 0 || len(r.CIDRs) > 0 {
			return true
		}
	}
	return false
}

// pstsAllocationLifecycleID returns the only accepted PSTS token lifecycle identifier:
// the CEP spec container-id, which changes with sandbox/container recreation.
//
// Any missing CEP or missing container-id is treated as an error instead of falling back
// to weaker identifiers. For fixed-IP PSTS, silently degrading to Pod UID / CEP UID / owner
// would hide lifecycle gaps and reintroduce stale-token reuse across sandbox rebuilds.
func pstsAllocationLifecycleID(resource *ccev2.CCEEndpoint) (string, error) {
	if resource == nil {
		return "", fmt.Errorf("nil CCEEndpoint when resolving PSTS lifecycle identifier")
	}
	if resource.Spec.ExternalIdentifiers == nil {
		return "", fmt.Errorf("missing external identifiers for PSTS endpoint %s/%s", resource.Namespace, resource.Name)
	}
	if resource.Spec.ExternalIdentifiers.ContainerID == "" {
		return "", fmt.Errorf("missing container-id for PSTS endpoint %s/%s", resource.Namespace, resource.Name)
	}
	return resource.Spec.ExternalIdentifiers.ContainerID, nil
}

func generatePSTSAllocateClientToken(owner, pstsName, lifecycleID, subnetID string, addressing ccev2.AddressPairList) string {
	if owner == "" || pstsName == "" || subnetID == "" {
		return ""
	}
	raw := fmt.Sprintf("psts-alloc-%s-%s-%s-%s", operatorOption.Config.CCEClusterID, owner, pstsName, subnetID)
	if lifecycleID != "" {
		raw += "|" + lifecycleID
	}
	for _, addr := range addressing {
		raw += "|" + addr.IP
	}
	hash := md5.Sum([]byte(raw))
	return "psts-alloc-" + hex.EncodeToString(hash[:])
}

var _ EndpointMunalAllocatorProvider = &pstsAllocatorProvider{}
