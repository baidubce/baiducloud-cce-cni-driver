/*
 * Copyright (c) 2024 Baidu, Inc. All Rights Reserved.
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

package watchers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	iputil "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	bce "github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const (
	// Annotation keys for storing Reserve IDs
	AnnotationIPReserveIDs   = "cce.baidubce.com/ip-reserve-ids"
	AnnotationIPReserveCIDRs = "cce.baidubce.com/ip-reserve-cidrs"

	// Finalizer for PSTS to ensure CIDRs are unreserved before deletion
	PSTSFinalizer = "cce.baidubce.com/psts-ipreserve"
)

// IPReserveInfo stores the mapping between CIDR and ReserveID
type IPReserveInfo struct {
	SubnetID string            `json:"subnetId"`
	Reserves map[string]string `json:"reserves"` // CIDR -> ReserveID
}

// calculateCIDRsForSubnetReserve calculates the CIDRs to reserve for a subnet
// Supports both Range (start-end IP pairs) and CIDRs (direct CIDR blocks)
// If neither is specified, return empty (no reserve needed, use VPC random allocation)
// Returns:
// - desiredCIDRs: the final CIDR list to reserve (ranges are expanded into CIDRs and merged with direct CIDRs)
// - cidrOrigins: map[cidr] -> human-readable origin label (e.g. "range 10.0.0.1-10.0.0.5"); empty for direct CIDRs
// - error: invalid range/CIDR input
func calculateCIDRsForSubnetReserve(subnetID string, allocations ccev2.CustomAllocationList) ([]string, map[string]string, error) {
	// If no range or CIDR specified, no reserve needed (VPC random allocation)
	if len(allocations) == 0 {
		return []string{}, nil, nil
	}

	// Collect all CIDRs from both Range and CIDRs fields
	cidrs := []string{}
	origins := make(map[string]string) // CIDR -> user-facing origin label
	for _, r := range allocations {
		// Process Range field: convert IP ranges to CIDRs
		for _, ipRange := range r.Range {
			startIP := net.ParseIP(ipRange.Start)
			endIP := net.ParseIP(ipRange.End)
			if startIP == nil || endIP == nil {
				return nil, nil, fmt.Errorf("invalid IP range: %s - %s", ipRange.Start, ipRange.End)
			}
			rangeLabel := fmt.Sprintf("range %s-%s", ipRange.Start, ipRange.End)
			rangeCIDRs, err := iputil.IPRangeToCIDRs(startIP, endIP)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to convert IP range %s-%s to CIDRs: %w", ipRange.Start, ipRange.End, err)
			}
			for _, c := range rangeCIDRs {
				origins[c] = rangeLabel
			}
			cidrs = append(cidrs, rangeCIDRs...)
		}

		// Process CIDRs field: validate and use directly
		for _, cidr := range r.CIDRs {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid CIDR %s: %v", cidr, err)
			}
			normalized := ipNet.String()
			origins[normalized] = ""
			cidrs = append(cidrs, normalized)
		}
	}
	return cidrs, origins, nil
}

// getReserveDescription generates the description for IPReserve
// when conflict(using the same subnet range in different cluster),
// or when we need to make sure whether the subnet is self-reserved,
// we can check the description to find the owner of the reserve
func getReserveDescription(psts *ccev2.PodSubnetTopologySpread) string {
	clusterID := operatorOption.Config.CCEClusterID
	selector := ""
	if psts.Spec.Selector != nil {
		labels := []string{}
		for k, v := range psts.Spec.Selector.MatchLabels {
			labels = append(labels, fmt.Sprintf("%s=%s", k, v))
		}
		selector = strings.Join(labels, ",")
	}
	return fmt.Sprintf("Reserved by CCE PSTS %s/%s in cluster %s for labels %s",
		psts.Namespace, psts.Name, clusterID, selector)
}

// isSelfReserve checks if a Reserve belongs to this PSTS
func isSelfReserve(description, namespace, name, clusterID string) bool {
	expectedPrefix := fmt.Sprintf("Reserved by CCE PSTS %s/%s in cluster %s", namespace, name, clusterID)
	return strings.HasPrefix(description, expectedPrefix)
}

// getAllReserveInfoFromAnnotation gets all Reserve info from PSTS annotation
func getAllReserveInfoFromAnnotation(psts *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
	if psts.Annotations == nil {
		return nil, nil
	}

	infoJSON := psts.Annotations[AnnotationIPReserveIDs]
	if infoJSON == "" {
		return nil, nil
	}

	var allInfo map[string]IPReserveInfo
	if err := json.Unmarshal([]byte(infoJSON), &allInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal reserve info: %w", err)
	}

	return allInfo, nil
}

// getReserveInfoFromAnnotationBySubnet gets the Reserve info for a subnet from PSTS annotation
func getReserveInfoFromAnnotationBySubnet(psts *ccev2.PodSubnetTopologySpread, subnetID string) (map[string]string, error) {
	allInfo, err := getAllReserveInfoFromAnnotation(psts)
	if err != nil || allInfo == nil {
		return nil, err
	}

	info, ok := allInfo[subnetID]
	if !ok {
		return nil, nil
	}

	return info.Reserves, nil
}

// setReserveInfoToAnnotation sets the Reserve info to PSTS annotation
func setReserveInfoToAnnotation(psts *ccev2.PodSubnetTopologySpread, subnetID string, reserves map[string]string) error {
	if psts.Annotations == nil {
		psts.Annotations = make(map[string]string)
	}

	// Get existing info
	var allInfo map[string]IPReserveInfo
	infoJSON := psts.Annotations[AnnotationIPReserveIDs]
	if infoJSON != "" {
		if err := json.Unmarshal([]byte(infoJSON), &allInfo); err != nil {
			// Corrupted annotation should not block reconciliation; reset and overwrite.
			allInfo = make(map[string]IPReserveInfo)
		}
	} else {
		allInfo = make(map[string]IPReserveInfo)
	}

	// Update info for this subnet
	allInfo[subnetID] = IPReserveInfo{
		SubnetID: subnetID,
		Reserves: reserves,
	}

	// Marshal back to JSON
	newInfoJSON, err := json.Marshal(allInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal reserve info: %w", err)
	}

	psts.Annotations[AnnotationIPReserveIDs] = string(newInfoJSON)
	return nil
}

// reserveCIDRsForSubnet reserves CIDRs for a subnet
// cidrOrigins maps each CIDR to its user-facing origin (e.g. "range 10.0.0.1-10.0.0.5" or "CIDR 10.0.0.0/24")
func reserveCIDRsForSubnet(ctx context.Context, vpcClient *vpc.Client, psts *ccev2.PodSubnetTopologySpread,
	subnetID string, cidrs []string, cidrOrigins map[string]string, log *logrus.Entry) (map[string]string, error) {

	description := getReserveDescription(psts)
	clusterID := operatorOption.Config.CCEClusterID
	reserves := make(map[string]string) // CIDR -> ReserveID

	// Query all existing reserves in the subnet from VPC (with pagination)
	log.Debugf("Querying existing reserves in subnet %s", subnetID)
	existingCIDRs := make(map[string]struct {
		reserveID   string
		isSelf      bool
		description string
	})

	marker := ""
	for {
		listResult, err := vpcClient.ListIpreserve(&vpc.ListIpeserveArgs{
			SubnetId: subnetID,
			Marker:   marker,
			MaxKeys:  1000,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list existing reserves in subnet %s: %w", subnetID, err)
		}

		// Build map of existing reserves: CIDR -> (ReserveID, isSelf)
		for _, reserve := range listResult.IpReserves {
			isSelf := isSelfReserve(reserve.Description, psts.Namespace, psts.Name, clusterID)
			existingCIDRs[reserve.IpCidr] = struct {
				reserveID   string
				isSelf      bool
				description string
			}{
				reserveID:   reserve.IpReserveId,
				isSelf:      isSelf,
				description: reserve.Description,
			}
			if isSelf {
				log.Debugf("Found existing self reserve: CIDR=%s, ID=%s", reserve.IpCidr, reserve.IpReserveId)
			} else {
				log.Debugf("Found existing other reserve: CIDR=%s, ID=%s, Description=%s", reserve.IpCidr, reserve.IpReserveId, reserve.Description)
			}
		}

		// Check if there are more pages
		if !listResult.IsTruncated {
			break
		}
		marker = listResult.NextMarker
	}

	// Check for conflicts: if any CIDR we want to reserve is already reserved by others
	var conflicts []string
	cidrsToReserve := make(map[string]bool) // CIDRs that need to be reserved
	for _, cidr := range cidrs {
		if existing, ok := existingCIDRs[cidr]; ok {
			if existing.isSelf {
				// Already reserved by us, reuse it
				log.Debugf("CIDR %s already reserved by self with ID %s, reuse", cidr, existing.reserveID)
				reserves[cidr] = existing.reserveID
			} else {
				// Reserved by others, conflict!
				label := cidr
				if origin, ok := cidrOrigins[cidr]; ok && origin != "" {
					label = fmt.Sprintf("%s (from %s)", cidr, origin)
				}
				conflicts = append(conflicts, fmt.Sprintf("%s is already used: %s", label, existing.description))
			}
		} else {
			// Not reserved yet, need to reserve
			cidrsToReserve[cidr] = true
		}
	}

	// If there are conflicts, fail immediately without reserving anything
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("CIDR conflicts detected in subnet %s, cannot reserve: %s", subnetID, strings.Join(conflicts, ", "))
	}

	// Now reserve the CIDRs that need to be reserved
	for cidr := range cidrsToReserve {
		// Determine IP version
		ipVersion := int32(4)
		if ip, _, err := net.ParseCIDR(cidr); err == nil && ip.To4() == nil {
			ipVersion = 6
		}

		scopedLog := log.WithFields(logrus.Fields{
			"cidr":     cidr,
			"subnetID": subnetID,
		})

		// Create Reserve
		scopedLog.Info("Creating reserve for CIDR")
		result, err := vpcClient.CreateIpreserve(&vpc.CreateIpreserveArgs{
			SubnetId:    subnetID,
			IpCidr:      cidr,
			IpVersion:   ipVersion,
			Description: description,
		})
		if err != nil {
			scopedLog.WithError(err).Error("Failed to reserve CIDR")
			// Check if it's a CIDR conflict error from VPC
			if bceErr, ok := err.(*bce.BceServiceError); ok && bceErr.Code == "IpReserve.IpCidrConflict" {
				// Find who owns the conflicting reserve from existingCIDRs
				conflictMsg := findConflictOwner(cidr, existingCIDRs, cidrOrigins)
				return nil, fmt.Errorf("CIDR conflicts detected in subnet %s: %s", subnetID, conflictMsg)
			}
			return nil, fmt.Errorf("failed to reserve CIDR %s: %w", cidr, err)
		}

		scopedLog.WithField("reserveID", result.IpReserveId).Info("Successfully reserved CIDR")
		reserves[cidr] = result.IpReserveId
	}

	return reserves, nil
}

// findConflictOwner finds who owns the conflicting reserve for a given CIDR.
// It checks existingCIDRs for overlapping networks and returns a user-friendly message.
func findConflictOwner(cidr string, existingCIDRs map[string]struct {
	reserveID   string
	isSelf      bool
	description string
}, cidrOrigins map[string]string) string {
	label := cidr
	if origin, ok := cidrOrigins[cidr]; ok && origin != "" {
		label = fmt.Sprintf("%s (from %s)", cidr, origin)
	}

	_, targetNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Sprintf("%s is already used", label)
	}

	// Find overlapping existing reserves
	for existingCIDR, info := range existingCIDRs {
		_, existingNet, err := net.ParseCIDR(existingCIDR)
		if err != nil {
			continue
		}
		// Check if networks overlap (either contains the other's first IP)
		if targetNet.Contains(existingNet.IP) || existingNet.Contains(targetNet.IP) {
			if info.description != "" {
				return fmt.Sprintf("%s is already used: %s", label, info.description)
			}
			return fmt.Sprintf("%s is already used (reserve ID: %s)", label, info.reserveID)
		}
	}

	return fmt.Sprintf("%s is already used", label)
}

// unreserveCIDRsForSubnet unreserves CIDRs for a subnet
// Queries VPC to get actual reserves instead of relying solely on annotation
func unreserveCIDRsForSubnet(ctx context.Context, vpcClient *vpc.Client, psts *ccev2.PodSubnetTopologySpread,
	subnetID string, log *logrus.Entry) error {

	clusterID := operatorOption.Config.CCEClusterID

	// Query VPC to get all reserves belonging to this PSTS in the subnet (with pagination)
	log.Debugf("Querying reserves belonging to this PSTS in subnet %s", subnetID)
	var reservesToDelete []struct {
		cidr      string
		reserveID string
	}

	marker := ""
	for {
		listResult, err := vpcClient.ListIpreserve(&vpc.ListIpeserveArgs{
			SubnetId: subnetID,
			Marker:   marker,
			MaxKeys:  1000,
		})
		if err != nil {
			return fmt.Errorf("failed to list reserves in subnet %s: %w", subnetID, err)
		}

		// Collect reserves belonging to this PSTS
		for _, reserve := range listResult.IpReserves {
			if isSelfReserve(reserve.Description, psts.Namespace, psts.Name, clusterID) {
				reservesToDelete = append(reservesToDelete, struct {
					cidr      string
					reserveID string
				}{
					cidr:      reserve.IpCidr,
					reserveID: reserve.IpReserveId,
				})
				log.Debugf("Found reserve to delete: CIDR=%s, ID=%s", reserve.IpCidr, reserve.IpReserveId)
			}
		}

		// Check if there are more pages
		if !listResult.IsTruncated {
			break
		}
		marker = listResult.NextMarker
	}

	if len(reservesToDelete) == 0 {
		log.Debug("No reserves found in VPC to delete")
		return nil
	}

	// Delete all reserves
	for _, reserve := range reservesToDelete {
		clientToken := uuid.New().String()
		if err := vpcClient.DeleteIpreserve(reserve.reserveID, clientToken); err != nil {
			if cloud.IsErrorIPReserveNotFound(err) {
				log.Infof("Reserve already deleted: CIDR %s (ReserveID: %s)", reserve.cidr, reserve.reserveID)
				continue
			}
			log.WithError(err).Errorf("Failed to unreserve CIDR %s (ReserveID: %s)", reserve.cidr, reserve.reserveID)
			return fmt.Errorf("failed to unreserve CIDR %s: %w", reserve.cidr, err)
		}
		log.Infof("Successfully unreserved CIDR %s (ReserveID: %s)", reserve.cidr, reserve.reserveID)
	}

	return nil
}

// hasPSTSFinalizer checks if PSTS has the finalizer
func hasPSTSFinalizer(psts *ccev2.PodSubnetTopologySpread) bool {
	for _, f := range psts.Finalizers {
		if f == PSTSFinalizer {
			return true
		}
	}
	return false
}

// addPSTSFinalizer adds the finalizer to PSTS
func addPSTSFinalizer(psts *ccev2.PodSubnetTopologySpread) {
	if !hasPSTSFinalizer(psts) {
		psts.Finalizers = append(psts.Finalizers, PSTSFinalizer)
	}
}

// removePSTSFinalizer removes the finalizer from PSTS
func removePSTSFinalizer(psts *ccev2.PodSubnetTopologySpread) bool {
	for i, f := range psts.Finalizers {
		if f == PSTSFinalizer {
			psts.Finalizers = append(psts.Finalizers[:i], psts.Finalizers[i+1:]...)
			return true
		}
	}
	return false
}
