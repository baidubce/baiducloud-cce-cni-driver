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
	"encoding/json"
	"fmt"
	"net"
	"sync"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

const (
	// Annotation key for storing Reserve IDs (same as in operator/watchers/psts_ipreserve.go)
	AnnotationIPReserveIDs = "cce.baidubce.com/ip-reserve-ids"
)

// IPReserveInfo stores the mapping between CIDR and ReserveID
// Must match the definition in operator/watchers/psts_ipreserve.go
type IPReserveInfo struct {
	SubnetID string            `json:"subnetId"`
	Reserves map[string]string `json:"reserves"` // CIDR -> ReserveID
}

type localPool struct {
	sync.Mutex
	// pstsLocalPool maps poolID to pstsLocalPool instances.
	// Key format (length-prefixed, collision-free):
	// "psts|ns:{len}:{namespace}|name:{len}:{pstsName}|subnet:{len}:{subnetID}|family:{len}:{family}"
	// Structure example:
	//   psts|ns:3:ns1|name:5:psts1|subnet:7:subnet1|family:1:4
	//   psts|ns:3:ns1|name:5:psts1|subnet:7:subnet1|family:1:6
	//   psts|ns:3:ns2|name:5:psts2|subnet:7:subnet2|family:1:4
	pstsLocalPool map[string]*pstsLocalPool
}

func newLocalPool() *localPool {
	return &localPool{
		pstsLocalPool: make(map[string]*pstsLocalPool),
	}
}

func (e *localPool) updateSubnet(oldObj, newObj interface{}) {
	// No longer needed - PSTS now manages all IP allocation
	// This handler is kept for backward compatibility but does nothing
}

func (e *localPool) updatePSTS(oldObj, newObj interface{}) {
	newPSTS, newOK := newObj.(*ccev2.PodSubnetTopologySpread)
	if !newOK {
		return
	}

	// Build expected pools from annotation. These pools only have config fields
	// (subnetID, family, ranges) populated. Runtime fields (pstsName, poolID, log,
	// realOccupied) are not set yet — they will be filled by initPoolRuntime
	// when a pool is actually activated in Step 2.
	expectedPools, err := e.buildExpectedPoolsFromAnnotation(newPSTS)
	if err != nil {
		managerLog.WithError(err).Errorf("Failed to build expected pools from annotation for PSTS %s/%s, skip pool update", newPSTS.Namespace, newPSTS.Name)
		return
	}

	// Step 1: Delete pools that are no longer needed (only for current PSTS)
	e.Lock()
	for poolID, pool := range e.pstsLocalPool {
		// Only consider pools belonging to this PSTS (match namespace and name)
		if pool.pstsNamespace != newPSTS.Namespace || pool.pstsName != newPSTS.Name {
			continue
		}
		if _, stillNeeded := expectedPools[poolID]; !stillNeeded {
			delete(e.pstsLocalPool, poolID)
			managerLog.Infof("Deleted PSTS local pool %s (no longer in annotation)", poolID)
		}
	}
	e.Unlock()

	// Step 2: Sync expected pools into active pools
	for poolID, expectedPool := range expectedPools {
		e.Lock()
		pool, exists := e.pstsLocalPool[poolID]
		if !exists {
			// New pool - sort ranges, fill runtime fields and activate
			sortRanges(expectedPool.ranges)
			expectedPool.initPoolRuntime(poolID, newPSTS.Namespace, newPSTS.Name, managerLog)
			e.pstsLocalPool[poolID] = expectedPool
			e.Unlock()
			managerLog.Infof("Created PSTS local pool %s for PSTS %s/%s subnet %s family %s",
				poolID, newPSTS.Namespace, newPSTS.Name, expectedPool.subnetID, expectedPool.family)
		} else {
			// Pool exists - skip if ranges unchanged
			if rangesEqual(pool.ranges, expectedPool.ranges) {
				e.Unlock()
				continue
			}
			// Ranges changed - update ranges and adjust cursor to avoid replaying full ranges
			pool.updateRanges(expectedPool.ranges)
			e.Unlock()
			managerLog.Infof("Updated PSTS local pool %s ranges", poolID)
		}
	}
}

// expectedPoolMap maps poolID (format:
// "psts|ns:{len}:{namespace}|name:{len}:{pstsName}|subnet:{len}:{subnetID}|family:{len}:{family}")
// to its expected pool config.
// The pool config only has config fields (subnetID, family, ranges) populated;
// runtime fields (poolID, pstsName, log, realOccupied) are NOT set.
type expectedPoolMap map[string]*pstsLocalPool

// buildExpectedPoolsFromAnnotation parses the PSTS annotation and builds expected pools.
// Runtime fields (poolID, pstsName, log, realOccupied) are NOT set — caller must
// call initPoolRuntime before putting them into active use.
func (e *localPool) buildExpectedPoolsFromAnnotation(psts *ccev2.PodSubnetTopologySpread) (expectedPoolMap, error) {
	expectedPools := make(expectedPoolMap)

	// Parse annotation to get per-subnet reserve info (subnetID -> reserved CIDRs)
	subnetReserveMap, err := e.parseReserveAnnotation(psts)
	if err != nil {
		return expectedPools, err
	}
	if len(subnetReserveMap) == 0 {
		return expectedPools, nil
	}

	// For each subnet, group its reserved CIDRs by IP family and build expected pool configs
	for subnetID, reserveInfo := range subnetReserveMap {
		if err := e.buildExpectedPoolsForSubnet(subnetID, reserveInfo, psts.Namespace, psts.Name, expectedPools); err != nil {
			return expectedPools, err
		}
	}

	return expectedPools, nil
}

// parseReserveAnnotation parses the IPReserve annotation from PSTS.
// Returns: subnetID -> IPReserveInfo (containing the CIDR->ReserveID mapping for that subnet)
func (e *localPool) parseReserveAnnotation(psts *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
	if psts.Annotations == nil {
		return nil, nil
	}

	infoJSON := psts.Annotations[AnnotationIPReserveIDs]
	if infoJSON == "" {
		return nil, nil
	}

	var subnetReserveMap map[string]IPReserveInfo
	if err := json.Unmarshal([]byte(infoJSON), &subnetReserveMap); err != nil {
		managerLog.WithError(err).Error("Failed to unmarshal reserve info from annotation")
		return nil, err
	}

	return subnetReserveMap, nil
}

// buildExpectedPoolsForSubnet groups a subnet's reserved CIDRs by IP family,
// and creates one expected pool config per family. Only config fields are set.
func (e *localPool) buildExpectedPoolsForSubnet(subnetID string, reserveInfo IPReserveInfo, namespace, pstsName string, expectedPools expectedPoolMap) error {
	// Group reserved CIDRs by IP family ("4" or "6")
	rangesByFamily := make(map[string][]ipRange)

	for cidr := range reserveInfo.Reserves {
		family, ipRng, err := e.parseCIDRToIPRange(cidr)
		if err != nil {
			return fmt.Errorf("invalid CIDR in PSTS reserve annotation for subnet %s: %w", subnetID, err)
		}
		if ipRng != nil {
			rangesByFamily[family] = append(rangesByFamily[family], *ipRng)
		}
	}

	// Create one expected pool config per family (config fields only, no runtime fields)
	for family, ranges := range rangesByFamily {
		poolID := getPSTSPoolID(namespace, pstsName, subnetID, family)
		expectedPools[poolID] = newExpectedPool(subnetID, family, ranges)
	}

	return nil
}

// parseCIDRToIPRange converts a CIDR string (e.g. "10.0.1.0/24") into its IP family and ipRange.
func (e *localPool) parseCIDRToIPRange(cidr string) (string, *ipRange, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}

	family := string(ccev2.IPv4Family)
	if ip.To4() == nil {
		family = string(ccev2.IPv6Family)
	}

	startIP := ipNet.IP
	endIP := lastIPInCIDR(ipNet)
	return family, &ipRange{
		startIP: startIP,
		endIP:   endIP,
		count:   calculateIPCount(startIP, endIP),
	}, nil
}

// lastIPInCIDR calculates the last IP address in a CIDR block
func lastIPInCIDR(cidr *net.IPNet) net.IP {
	ip := cidr.IP
	mask := cidr.Mask

	// Convert to proper byte length
	if ip.To4() != nil {
		ip = ip.To4()
	} else {
		ip = ip.To16()
	}

	lastIP := make(net.IP, len(ip))
	copy(lastIP, ip)

	// Set all host bits to 1
	for i := range lastIP {
		lastIP[i] = ip[i] | ^mask[i]
	}

	return lastIP
}

func (e *EndpointManager) deleteSubnet(oldObj interface{}) {
	// do nothing
}

func (e *localPool) deletePSTS(oldObj interface{}) {
	psts, ok := oldObj.(*ccev2.PodSubnetTopologySpread)
	if !ok {
		return
	}

	e.Lock()
	defer e.Unlock()

	// Clean up all pools for this PSTS (match namespace and name)
	for poolID, pool := range e.pstsLocalPool {
		if pool.pstsNamespace != psts.Namespace || pool.pstsName != psts.Name {
			continue
		}
		delete(e.pstsLocalPool, poolID)
		managerLog.Infof("Deleted PSTS local pool %s for PSTS %s/%s", poolID, psts.Namespace, psts.Name)
	}
}

// updateEndpoint update endpoint ip in local pool
func (e *localPool) deleteEndpointAddress(cep *ccev2.CCEEndpoint) {
	if cep.Status.Networking == nil {
		return
	}
	if cep.Spec.Network.IPAllocation == nil || cep.Spec.Network.IPAllocation.PSTSName == "" {
		return
	}
	pstsName := cep.Spec.Network.IPAllocation.PSTSName
	for _, address := range cep.Status.Networking.Addressing {
		if address.IP == "" {
			continue
		}
		e.releaseLocalIPs(cep.Namespace, pstsName, address)
	}
}

func (e *localPool) releaseLocalIPs(cepNamespace, pstsName string, addr *ccev2.AddressPair) {
	e.Lock()
	defer e.Unlock()

	// Try PSTS-specific pool first.
	pstsPoolID := getPSTSPoolID(cepNamespace, pstsName, addr.Subnet, string(addr.Family))
	if pstsPool, ok := e.pstsLocalPool[pstsPoolID]; ok {
		pstsPool.release(net.ParseIP(addr.IP))
		managerLog.Debugf("Released IP %s from PSTS pool %s", addr.IP, pstsPoolID)
		return
	}

	// No pool found - this is expected for no-range subnets (VPC random allocation)
	managerLog.Debugf("No PSTS pool found for IP %s (subnet %s, family %s) - likely no-range subnet",
		addr.IP, addr.Subnet, addr.Family)
}

// restoreOccupiedFromCEPs rebuilds realOccupied in local pools based on existing CEPs.
// Returns counts: totalIPsSeen, totalMarked.
func (e *localPool) restoreOccupiedFromCEPs(ceps []*ccev2.CCEEndpoint) (int, int) {
	totalIPs := 0
	marked := 0

	for _, cep := range ceps {
		if cep == nil || !IsPSTSEndpoint(cep) {
			continue
		}
		if cep.Spec.Network.IPAllocation == nil || cep.Spec.Network.IPAllocation.PSTSName == "" {
			continue
		}
		if cep.Status.Networking == nil || len(cep.Status.Networking.Addressing) == 0 {
			continue
		}

		pstsName := cep.Spec.Network.IPAllocation.PSTSName
		for _, addr := range cep.Status.Networking.Addressing {
			if addr.IP == "" || addr.Subnet == "" {
				continue
			}
			totalIPs++

			poolID := getPSTSPoolID(cep.Namespace, pstsName, addr.Subnet, string(addr.Family))
			e.Lock()
			pool := e.pstsLocalPool[poolID]
			e.Unlock()
			if pool == nil {
				continue
			}

			ip := net.ParseIP(addr.IP)
			if ip == nil {
				continue
			}
			if pool.markOccupied(ip) {
				marked++
			}
		}
	}

	return totalIPs, marked
}

// getPSTSPoolID generates a deterministic, collision-free pool ID.
// Length prefixes avoid ambiguity when fields contain the same delimiters.
func getPSTSPoolID(namespace, pstsName, subnetID, family string) string {
	return fmt.Sprintf(
		"psts|ns:%d:%s|name:%d:%s|subnet:%d:%s|family:%d:%s",
		len(namespace), namespace,
		len(pstsName), pstsName,
		len(subnetID), subnetID,
		len(family), family,
	)
}

// getPSTSPoolByID returns PSTS-specific pool by full poolID
func (e *localPool) getPSTSPoolByID(poolID string) *pstsLocalPool {
	e.Lock()
	defer e.Unlock()
	return e.pstsLocalPool[poolID]
}

// releaseMany releases IPs from local pool
// this method tries to release the IP as much as possible. And ignore the error
