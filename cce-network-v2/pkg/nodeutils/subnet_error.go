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

package nodeutils

import (
	"fmt"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// ExtractSubnetIDs extracts subnet IDs from a slice of BorrowedSubnet
// This function is extracted as a common utility to be reused across different packages
func ExtractSubnetIDs(subnets []*bcesync.BorrowedSubnet) []string {
	subnetIDs := make([]string, len(subnets))
	for i, subnet := range subnets {
		subnetIDs[i] = subnet.Spec.ID
	}
	return subnetIDs
}

// FilterSubnetsByZone filters subnets that match the target availability zone
// This function is extracted as a common utility to be reused across different packages
func FilterSubnetsByZone(subnets []*bcesync.BorrowedSubnet, targetZoneName string) []*bcesync.BorrowedSubnet {
	var zoneMatchSubnets []*bcesync.BorrowedSubnet
	for _, subnet := range subnets {
		if strings.Contains(subnet.Spec.AvailabilityZone, targetZoneName) {
			zoneMatchSubnets = append(zoneMatchSubnets, subnet)
		}
	}
	return zoneMatchSubnets
}

// FilterSubnetsByIPCapacity filters subnets that have sufficient IP capacity
// Returns available subnets and IDs of subnets with insufficient IPs
// This function is extracted as a common utility to be reused across different packages
func FilterSubnetsByIPCapacity(subnets []*bcesync.BorrowedSubnet, minAllocateIPs int) ([]*bcesync.BorrowedSubnet, []string) {
	var (
		availableSubnets        []*bcesync.BorrowedSubnet
		insufficientIPSubnetIDs []string
	)

	for _, subnet := range subnets {
		if subnet.CanBorrow(minAllocateIPs) {
			availableSubnets = append(availableSubnets, subnet)
		} else {
			insufficientIPSubnetIDs = append(insufficientIPSubnetIDs, subnet.Spec.ID)
		}
	}

	return availableSubnets, insufficientIPSubnetIDs
}

// FilterAvailableSubnet filters subnets by availability zone and IP capacity, returning detailed error information
// This function is extracted as a common utility to be reused across different packages
func FilterAvailableSubnet(subnets []*bcesync.BorrowedSubnet, minAllocateIPs int, country, region, availabilityZone string) ([]*bcesync.BorrowedSubnet, error) {
	targetZoneName := api.TransAvailableZoneToZoneName(country, region, availabilityZone)

	// Filter subnets by availability zone first
	zoneMatchSubnets := FilterSubnetsByZone(subnets, targetZoneName)
	if len(zoneMatchSubnets) == 0 {
		return nil, fmt.Errorf("no subnets available in required zone %s", targetZoneName)
	}

	// Filter zone-matched subnets by IP availability
	availableSubnets, insufficientIPSubnetIDs := FilterSubnetsByIPCapacity(zoneMatchSubnets, minAllocateIPs)
	if len(availableSubnets) == 0 && len(insufficientIPSubnetIDs) > 0 {
		return nil, fmt.Errorf("insufficient IPs in zone %s subnets %v, required %d IPs", targetZoneName, insufficientIPSubnetIDs, minAllocateIPs)
	}

	return availableSubnets, nil
}

// CategorizeSubnetError categorizes subnet filter errors and returns appropriate error code, reason, and message
// This function is extracted as a common utility to be reused across different packages
func CategorizeSubnetError(filterErr error, subnetIDs []string, nodeName string) (string, string, string) {
	if filterErr != nil {
		if strings.Contains(filterErr.Error(), "no subnets available in required zone") {
			return ccev2.ErrorCodeNoAvailableSubnet, ReasonSubnetZoneMismatch,
				fmt.Sprintf("zone mismatch for node %s: %v", nodeName, filterErr.Error())
		} else if strings.Contains(filterErr.Error(), "insufficient IPs") {
			return ccev2.ErrorCodeENISubnetNoMoreIP, ReasonSubnetInsufficientIPs,
				fmt.Sprintf("insufficient IPs for node %s: %v", nodeName, filterErr.Error())
		} else {
			return ccev2.ErrorCodeNoAvailableSubnet, ReasonNoAvailableSubnet,
				fmt.Sprintf("subnets not available for node %s: %v", nodeName, filterErr.Error())
		}
	}
	return ccev2.ErrorCodeNoAvailableSubnet, ReasonNoAvailableSubnet,
		fmt.Sprintf("subnets [%v] are not available for node %s", subnetIDs, nodeName)
}
