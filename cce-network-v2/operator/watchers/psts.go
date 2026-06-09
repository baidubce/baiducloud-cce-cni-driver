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

package watchers

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"strings"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	bceoption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	cce_lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
)

const (
	pstsComponent = "psts-watcher"
)

var (
	pstsClient = &pstsUpdaterImpl{}
	pstsLog    = logging.NewSubysLogger("psts-watcher")
	// jsonMarshal is a seam for unit tests.
	jsonMarshal = json.Marshal
)

// PSTSEventHandler should implement the behavior to handle PSTS
type PSTSEventHandler interface {
	Update(resource *ccev2.PodSubnetTopologySpread) error
	Delete(namespace, pstsName string) error
}

func StartSynchronizingPSTS(ctx context.Context) error {
	log.Info("Starting to synchronize PSTS custom resources")

	// Initialize VPC client - this is a required dependency for PSTS operations
	vpcClient := bceoption.VPCClient()
	if vpcClient == nil {
		return fmt.Errorf("failed to initialize VPC client, cannot start PSTS syncher")
	}

	var (
		pstsManagerSyncHandler func(key string) error
		pstsManager            = &pstsSyncher{
			recorder:  k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: pstsComponent}),
			vpcClient: vpcClient,
		}
	)

	// TODO: The operator is currently storing a full copy of the
	// PSTS resource, as the resource grows, we may want to consider
	// introducing a slim version of it.
	PSTSLister := k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister()

	if pstsManagerSyncHandler == nil {
		pstsManagerSyncHandler = func(key string) error {
			namespace, name, err := cache.SplitMetaNamespaceKey(key)
			if err != nil {
				log.WithError(err).Error("Unable to process PSTS event")
				return err
			}
			obj, err := PSTSLister.PodSubnetTopologySpreads(namespace).Get(name)

			// Delete handling
			if errors.IsNotFound(err) ||
				(obj != nil && obj.DeletionTimestamp != nil) {
				return pstsManager.Delete(namespace, name)
			}
			if err != nil {
				log.WithError(err).Warning("Unable to retrieve PSTS from watcher store")
				return err
			}
			return pstsManager.Update(obj)
		}
	}

	controller := cm.NewResyncController("cce-psts-controller", int(operatorOption.Config.ResourceResyncWorkers), k8s.GetQPS(), k8s.GetBurst(),
		k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Informer(), pstsManagerSyncHandler)
	controller.RunWithResync(operatorOption.Config.ResourceResyncInterval)
	return nil
}

type pstsUpdaterImpl struct {
}

func (c *pstsUpdaterImpl) Lister() cce_lister.PodSubnetTopologySpreadLister {
	return k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister()
}

func (c *pstsUpdaterImpl) Create(psts *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(psts.Namespace).Create(context.TODO(), psts, metav1.CreateOptions{})
}

func (c *pstsUpdaterImpl) Update(newResource *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(newResource.Namespace).Update(context.TODO(), newResource, metav1.UpdateOptions{})
}

func (c *pstsUpdaterImpl) Delete(namespace, name string) error {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (c *pstsUpdaterImpl) UpdateStatus(newResource *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(newResource.Namespace).UpdateStatus(context.TODO(), newResource, metav1.UpdateOptions{})
}

type pstsSyncher struct {
	recorder  record.EventRecorder
	vpcClient *vpc.Client
}

// recordEvent records an event for PSTS resource if recorder is available
func (syncher *pstsSyncher) recordEvent(resource *ccev2.PodSubnetTopologySpread, eventType, reason, messageFmt string, args ...interface{}) {
	if syncher.recorder != nil {
		syncher.recorder.Eventf(resource, eventType, reason, messageFmt, args...)
	}
}

func (syncher *pstsSyncher) Update(resource *ccev2.PodSubnetTopologySpread) error {
	// Work on a copy to avoid mutating informer cache objects.
	resource = resource.DeepCopy()
	var (
		err       error
		scopedLog = pstsLog.WithFields(logrus.Fields{
			"namespace": resource.Namespace,
			"name":      resource.Name,
		})

		// new status to be updated to PSTS
		newStatus = ccev2.PodSubnetTopologySpreadStatus{
			Name:               resource.Name,
			AvailableSubnets:   make(map[string]ccev2.SubnetPodStatus),
			UnavailableSubnets: make(map[string]ccev2.SubnetPodStatus),
		}

		// matchedEndpoints are tracked by PSTS binding for reserve/unreserve safety.
		namespaceCEPs    []*ccev2.CCEEndpoint = make([]*ccev2.CCEEndpoint, 0)
		matchedEndpoints []*ccev2.CCEEndpoint = make([]*ccev2.CCEEndpoint, 0)
	)

	namespaceCEPs, err = CCEEndpointClient.Lister().CCEEndpoints(resource.Namespace).List(labels.Everything())
	if err != nil {
		scopedLog.Errorf("Failed to list endpoints in namespace %s: %v", resource.Namespace, err)
		namespaceCEPs = []*ccev2.CCEEndpoint{}
	}
	for i := range namespaceCEPs {
		ep := namespaceCEPs[i]
		// For reserve/unreserve safety, affected CEPs must be tracked by PSTS binding
		// instead of current selector matching.
		if ep.Spec.Network.IPAllocation != nil && ep.Spec.Network.IPAllocation.PSTSName == resource.Name {
			matchedEndpoints = append(matchedEndpoints, ep)
		}
	}
	newStatus.PodMatchedCount = int32(len(matchedEndpoints))

	// Step 1: Clean up reserves for subnets (including its cidrs & ranges) removed from Spec
	needsUpdate := false
	removedUpdated, err := syncher.cleanupRemovedSubnets(resource, matchedEndpoints, scopedLog)
	if err != nil {
		scopedLog.WithError(err).Error("Failed to clean up removed subnets")
		return err
	}
	if removedUpdated {
		needsUpdate = true
	}

	// Step 2: Handle IPReserve for each subnet in current Spec
	for sbnID, allocations := range resource.Spec.Subnets {
		subnetStatus, _, updated, err := syncher.processSubnetReserve(resource, sbnID, allocations, matchedEndpoints, scopedLog)
		if err != nil {
			scopedLog.WithError(err).Errorf("Failed to process subnet reserve for %s", sbnID)
			return err
		}
		if updated {
			needsUpdate = true
		}

		// Categorize subnet status
		if !subnetStatus.SubenetDetail.Enable || subnetStatus.SubenetDetail.AvailableIPNum == 0 {
			newStatus.UnavailableSubnets[sbnID] = subnetStatus
		} else {
			newStatus.AvailableSubnets[sbnID] = subnetStatus
		}
	}
	newStatus.SchedulableSubnetsNum = int32(len(newStatus.AvailableSubnets))
	newStatus.UnSchedulableSubnetsNum = int32(len(newStatus.UnavailableSubnets))

	// Always add finalizer for PSTS (regardless of whether it has CIDR reserves)
	// This ensures Delete() is always called to clean up any resources
	if !hasPSTSFinalizer(resource) {
		addPSTSFinalizer(resource)
		needsUpdate = true
		scopedLog.Info("Added PSTS finalizer")
	}

	// Determine if status needs update
	needsStatusUpdate := !reflect.DeepEqual(resource.Status, newStatus)
	if needsStatusUpdate {
		resource.Status = newStatus
	}

	// Persist changes with retry on conflict
	if needsUpdate || needsStatusUpdate {
		if err := syncher.persistPSTSUpdate(resource, needsUpdate, needsStatusUpdate, scopedLog); err != nil {
			return err
		}
		scopedLog.Info("Successfully synced PSTS")
	}
	return nil
}

// persistPSTSUpdate writes metadata and status changes to apiserver with conflict retry.
// When metadata changes (annotation/finalizer), it first calls Update to get the latest
// resourceVersion, then uses the returned object to UpdateStatus. This avoids resourceVersion
// conflicts between the two calls.
func (syncher *pstsSyncher) persistPSTSUpdate(
	resource *ccev2.PodSubnetTopologySpread,
	needsUpdate, needsStatusUpdate bool,
	scopedLog *logrus.Entry,
) error {
	// Capture desired changes once; we will re-apply them on conflict retries.
	desiredAnnotations := resource.Annotations
	desiredFinalizers := resource.Finalizers
	desiredStatus := resource.Status

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Always start from the latest version to reduce conflicts and avoid redundant updates.
		current, getErr := pstsClient.Lister().PodSubnetTopologySpreads(resource.Namespace).Get(resource.Name)
		if getErr != nil {
			return getErr
		}
		current = current.DeepCopy()

		// Step 1: Update metadata (annotations/finalizers) if changed
		if needsUpdate {
			metadataChanged := !reflect.DeepEqual(current.Annotations, desiredAnnotations) ||
				!reflect.DeepEqual(current.Finalizers, desiredFinalizers)
			if metadataChanged {
				current.Annotations = desiredAnnotations
				current.Finalizers = desiredFinalizers
				current.Status = desiredStatus
				updated, err := pstsClient.Update(current)
				if err != nil {
					if k8serrors.IsConflict(err) {
						return err // trigger retry
					}
					scopedLog.WithError(err).Error("Failed to update PSTS metadata")
					return err
				}
				// Use the returned object (with updated resourceVersion) for status update
				current = updated
			}
		}

		// Step 2: Update status
		if needsStatusUpdate {
			current.Status = desiredStatus
			if _, err := pstsClient.UpdateStatus(current); err != nil {
				if k8serrors.IsConflict(err) {
					return err // trigger retry
				}
				scopedLog.WithError(err).Error("Failed to update PSTS status")
				return err
			}
		}
		return nil
	})
}

// getAllReserveInfoFromAnnotationWithFallback loads reserve info from annotation.
// If annotation is corrupted, it rebuilds reserve info from VPC and records an event.
// Returns (reserveMap, rebuiltFromVPC, error).
func (syncher *pstsSyncher) getAllReserveInfoFromAnnotationWithFallback(
	resource *ccev2.PodSubnetTopologySpread,
	scopedLog *logrus.Entry,
	stage string,
) (map[string]IPReserveInfo, bool, error) {
	subnetReserveMap, err := getAllReserveInfoFromAnnotation(resource)
	if err == nil {
		return subnetReserveMap, false, nil
	}

	if stage == "" {
		stage = "during operation"
	}
	scopedLog.WithError(err).Warnf("Reserve annotation corrupted %s, rebuilding from VPC", stage)
	syncher.recordEvent(resource, corev1.EventTypeWarning, "ReserveAnnotationCorrupted",
		"Reserve annotation corrupted %s, rebuilding from VPC: %v", stage, err)

	subnetReserveMap, err = syncher.rebuildReserveInfoFromVPC(resource, scopedLog)
	if err != nil {
		return nil, false, fmt.Errorf("failed to rebuild reserve info from VPC %s: %w", stage, err)
	}
	return subnetReserveMap, true, nil
}

// cleanupRemovedSubnets unreserves CIDRs for subnets that were removed from PSTS Spec
func (syncher *pstsSyncher) cleanupRemovedSubnets(
	resource *ccev2.PodSubnetTopologySpread,
	matchedEndpoints []*ccev2.CCEEndpoint,
	scopedLog *logrus.Entry,
) (bool, error) {
	updated := false

	// Parse annotation to get all subnets with reserves
	subnetReserveMap, rebuilt, err := syncher.getAllReserveInfoFromAnnotationWithFallback(resource, scopedLog, "during update")
	if err != nil {
		return false, err
	}
	if len(subnetReserveMap) == 0 {
		if rebuilt {
			updated = true
			// Clear annotation if rebuilt and no reserves found.
			delete(resource.Annotations, AnnotationIPReserveIDs)
			if len(resource.Annotations) == 0 {
				resource.Annotations = nil
			}
			return true, nil
		}
		return false, nil
	}

	clusterID := operatorOption.Config.CCEClusterID

	// Check each subnet in annotation - if not in Spec anymore, unreserve it
	for sbnID := range subnetReserveMap {
		if _, stillInSpec := resource.Spec.Subnets[sbnID]; stillInSpec {
			continue // Subnet still in Spec, skip
		}

		// Subnet removed from Spec - need to unreserve all its CIDRs
		scopedLog.Infof("Subnet %s removed from PSTS Spec, cleaning up reserves", sbnID)

		// Query actual reserves from VPC to ensure reliability
		cidrs, err := syncher.listPSTSReservedCIDRsBySubnetID(resource, sbnID, clusterID, scopedLog)
		if err != nil {
			return updated, err
		}

		if len(cidrs) > 0 {
			// Collect IPs in use for this subnet
			ipsInUse := collectIPsInUseForSubnet(matchedEndpoints, sbnID)

			// Check if any IP is still in use
			cidrsToCheck := make(map[string]string, len(cidrs))
			for _, cidr := range cidrs {
				cidrsToCheck[cidr] = cidr
			}
			if err := checkCIDRsNotInUse(cidrsToCheck, ipsInUse); err != nil {
				syncher.recordEvent(resource, corev1.EventTypeWarning, "SubnetRemovalBlocked",
					"Cannot remove subnet %s from PSTS: %v", sbnID, err)
				return updated, fmt.Errorf("cannot remove subnet %s: %v", sbnID, err)
			}

			// Unreserve all CIDRs for this subnet (list+delete from VPC)
			if err := unreserveCIDRsForSubnet(context.TODO(), syncher.vpcClient, resource, sbnID, scopedLog); err != nil {
				return updated, fmt.Errorf("failed to unreserve reserves for removed subnet %s: %w", sbnID, err)
			}
		}

		// Remove subnet from annotation map
		delete(subnetReserveMap, sbnID)
		updated = true

		scopedLog.Infof("Successfully cleaned up reserves for removed subnet %s", sbnID)
		syncher.recordEvent(resource, corev1.EventTypeNormal, "SubnetCleanupSuccess",
			"Successfully cleaned up reserves for removed subnet %s", sbnID)
	}

	if updated || rebuilt {
		if len(subnetReserveMap) == 0 {
			delete(resource.Annotations, AnnotationIPReserveIDs)
			if len(resource.Annotations) == 0 {
				resource.Annotations = nil
			}
		} else {
			newInfoJSON, err := jsonMarshal(subnetReserveMap)
			if err != nil {
				return updated, fmt.Errorf("failed to marshal reserve info after cleanup: %w", err)
			}
			if resource.Annotations == nil {
				resource.Annotations = make(map[string]string)
			}
			resource.Annotations[AnnotationIPReserveIDs] = string(newInfoJSON)
		}
	}

	return updated || rebuilt, nil
}

// processSubnetReserve handles CIDR reservation for a single subnet
// Returns: (subnetStatus, hasReservedCIDRs, needsUpdate, error)
func (syncher *pstsSyncher) processSubnetReserve(
	resource *ccev2.PodSubnetTopologySpread,
	sbnID string,
	allocations ccev2.CustomAllocationList,
	matchedEndpoints []*ccev2.CCEEndpoint,
	scopedLog *logrus.Entry,
) (ccev2.SubnetPodStatus, bool, bool, error) {

	// Ensure subnet exists (EnsureSubnet already includes Lister().Get() internally)
	sbn, err := CCESubnetClient.EnsureSubnet("", sbnID)
	if err != nil {
		return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to ensure subnet %s: %v", sbnID, err)
	}
	if sbn == nil {
		return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("subnet %s not found", sbnID)
	}

	// Calculate desired CIDRs from spec
	desiredCIDRs, cidrOrigins, err := calculateCIDRsForSubnetReserve(sbnID, allocations)
	if err != nil {
		return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to calculate CIDRs: %v", err)
	}

	needsUpdate := false

	// Get existing reserves from annotation; if missing/corrupted, rebuild from VPC.
	existingReserves, err := getReserveInfoFromAnnotationBySubnet(resource, sbnID)
	if err != nil || existingReserves == nil {
		clusterID := operatorOption.Config.CCEClusterID
		rebuiltReserves, rebuildErr := syncher.listPSTSReservedCIDRsBySubnetIDWithReserveID(resource, sbnID, clusterID, scopedLog)
		if rebuildErr != nil {
			if err != nil {
				return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to get reserve info from annotation for subnet %s: %w", sbnID, err)
			}
			return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to rebuild reserve info from vpc for subnet %s: %w", sbnID, rebuildErr)
		}
		existingReserves = rebuiltReserves
		// Persist rebuilt info if annotation was corrupted or there are actual reserves.
		if err != nil || len(existingReserves) > 0 {
			if err := setReserveInfoToAnnotation(resource, sbnID, existingReserves); err != nil {
				return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to set reserve info to annotation for subnet %s: %w", sbnID, err)
			}
			needsUpdate = true
		}
	}
	if existingReserves == nil {
		existingReserves = make(map[string]string)
	}

	// Calculate diff: toAdd and toRemove
	toAdd, toRemove := calculateCIDRDiff(desiredCIDRs, existingReserves)

	// Collect IPs in use for this subnet from matched endpoints
	ipsInUse := collectIPsInUseForSubnet(matchedEndpoints, sbnID)

	// Fixed mode protects in-use IPs during reserve shrink.
	// Elastic mode allows range/CIDR -> no-range transition while running, and cleanup proceeds in background.
	if shouldCheckReserveShrinkInUse(resource.Spec.Strategy) {
		if err := checkCIDRsNotInUse(toRemove, ipsInUse); err != nil {
			syncher.recordEvent(resource, corev1.EventTypeWarning, "IPReserveChangeFailed",
				"Cannot remove CIDR range for subnet %s: %v", sbnID, err)
			return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("cannot remove CIDR range: %v", err)
		}
	}

	finalReserves := make(map[string]string)

	// Copy existing reserves that are still needed
	for cidr, reserveID := range existingReserves {
		if _, shouldRemove := toRemove[cidr]; !shouldRemove {
			finalReserves[cidr] = reserveID
		}
	}

	// Remove CIDRs that are no longer needed
	if len(toRemove) > 0 {
		for cidr, reserveID := range toRemove {
			if err := syncher.unreserveSingleCIDR(resource, sbnID, cidr, reserveID, scopedLog); err != nil {
				return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to unreserve CIDR %s: %v", cidr, err)
			}
			needsUpdate = true
		}
	}

	// Add new CIDRs
	if len(toAdd) > 0 {
		addedReserves, err := syncher.reserveNewCIDRs(resource, sbnID, toAdd, cidrOrigins, scopedLog)
		if err != nil {
			return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to reserve CIDRs: %v", err)
		}
		for cidr, reserveID := range addedReserves {
			finalReserves[cidr] = reserveID
		}
		needsUpdate = true
	}

	// Update annotation if changed
	if needsUpdate {
		if err := setReserveInfoToAnnotation(resource, sbnID, finalReserves); err != nil {
			return ccev2.SubnetPodStatus{}, false, false, fmt.Errorf("failed to set reserve info to annotation for subnet %s: %w", sbnID, err)
		}
	}

	// Build subnet status
	subnetStatus := buildSubnetStatus(sbn, matchedEndpoints, sbnID, finalReserves)
	hasReservedCIDRs := len(finalReserves) > 0

	return subnetStatus, hasReservedCIDRs, needsUpdate, nil
}

// rebuildReserveInfoFromVPC rebuilds the full reserve info by scanning VPC reserves across all subnets.
// This is used only when annotation is corrupted. It returns a full map: subnetID -> IPReserveInfo.
func (syncher *pstsSyncher) rebuildReserveInfoFromVPC(
	resource *ccev2.PodSubnetTopologySpread,
	scopedLog *logrus.Entry,
) (map[string]IPReserveInfo, error) {
	subnets, err := CCESubnetClient.Lister().List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list subnets for rebuild: %w", err)
	}
	if len(subnets) == 0 {
		return nil, fmt.Errorf("no subnets available to rebuild reserve info")
	}

	clusterID := operatorOption.Config.CCEClusterID
	allInfo := make(map[string]IPReserveInfo)

	for _, sbn := range subnets {
		sbnID := sbn.Name
		reserves, err := syncher.listPSTSReservedCIDRsBySubnetIDWithReserveID(resource, sbnID, clusterID, scopedLog)
		if err != nil {
			return nil, fmt.Errorf("failed to list reserves in subnet %s during rebuild: %w", sbnID, err)
		}
		if len(reserves) == 0 {
			continue
		}
		allInfo[sbnID] = IPReserveInfo{
			SubnetID: sbnID,
			Reserves: reserves,
		}
	}

	return allInfo, nil
}

// calculateCIDRDiff calculates CIDRs to add and remove
// Returns: (toAdd []string, toRemove map[cidr]reserveID)
func calculateCIDRDiff(desiredCIDRs []string, existingReserves map[string]string) ([]string, map[string]string) {
	toAdd := []string{}
	toRemove := make(map[string]string)

	// Build set of desired CIDRs
	desiredSet := make(map[string]bool)
	for _, cidr := range desiredCIDRs {
		desiredSet[cidr] = true
	}

	// Find CIDRs to remove (in existing but not in desired)
	for cidr, reserveID := range existingReserves {
		if !desiredSet[cidr] {
			toRemove[cidr] = reserveID
		}
	}

	// Find CIDRs to add (in desired but not in existing)
	for _, cidr := range desiredCIDRs {
		if _, exists := existingReserves[cidr]; !exists {
			toAdd = append(toAdd, cidr)
		}
	}

	return toAdd, toRemove
}

// collectIPsInUseForSubnet collects all IPs in use for a specific subnet
func collectIPsInUseForSubnet(endpoints []*ccev2.CCEEndpoint, subnetID string) map[string]string {
	ipsInUse := make(map[string]string) // IP -> endpoint name
	for _, ep := range endpoints {
		if ep.Status.Networking != nil {
			for _, addr := range ep.Status.Networking.Addressing {
				if addr.Subnet == subnetID {
					ipsInUse[addr.IP] = ep.Namespace + "/" + ep.Name
				}
			}
		}
	}
	return ipsInUse
}

func effectivePSTSStrategyType(strategy *ccev2.IPAllocationStrategy) ccev2.IPAllocType {
	// Keep behavior consistent with webhook/defaulting: nil/empty means Elastic.
	if strategy == nil || strategy.Type == ccev2.IPAllocTypeNil {
		return ccev2.IPAllocTypeElastic
	}
	return strategy.Type
}

func shouldCheckReserveShrinkInUse(strategy *ccev2.IPAllocationStrategy) bool {
	return effectivePSTSStrategyType(strategy) == ccev2.IPAllocTypeFixed
}

// checkCIDRsNotInUse checks if any IP in the CIDRs is still in use
// Returns detailed error message listing all conflicting IPs
func checkCIDRsNotInUse(cidrsToRemove map[string]string, ipsInUse map[string]string) error {
	conflicts := make(map[string][]string) // CIDR -> list of "IP (pod)"

	for cidr := range cidrsToRemove {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		for ip, epName := range ipsInUse {
			if ipNet.Contains(net.ParseIP(ip)) {
				conflicts[cidr] = append(conflicts[cidr], fmt.Sprintf("%s (pod: %s)", ip, epName))
			}
		}
	}

	if len(conflicts) > 0 {
		var errMsgs []string
		for cidr, ips := range conflicts {
			errMsgs = append(errMsgs, fmt.Sprintf("CIDR %s has %d IP(s) in use: [%s]",
				cidr, len(ips), strings.Join(ips, ", ")))
		}
		return fmt.Errorf("cannot remove CIDR(s) with IPs in use: %s", strings.Join(errMsgs, "; "))
	}
	return nil
}

// unreserveSingleCIDR unreserves a single CIDR
func (syncher *pstsSyncher) unreserveSingleCIDR(
	resource *ccev2.PodSubnetTopologySpread,
	sbnID, cidr, reserveID string,
	scopedLog *logrus.Entry,
) error {
	clientToken := uuid.New().String()
	if err := syncher.vpcClient.DeleteIpreserve(reserveID, clientToken); err != nil {
		if cloud.IsErrorIPReserveNotFound(err) {
			scopedLog.Infof("Reserve already deleted: CIDR %s (ReserveID: %s)", cidr, reserveID)
			return nil
		}
		return err
	}
	scopedLog.Infof("Successfully unreserved CIDR %s (ReserveID: %s)", cidr, reserveID)
	syncher.recordEvent(resource, corev1.EventTypeNormal, "IPUnreserveSuccess",
		"Successfully unreserved CIDR %s for subnet %s", cidr, sbnID)
	return nil
}

// reserveNewCIDRs reserves new CIDRs and returns the reserve mapping
func (syncher *pstsSyncher) reserveNewCIDRs(
	resource *ccev2.PodSubnetTopologySpread,
	sbnID string,
	cidrs []string,
	cidrOrigins map[string]string,
	scopedLog *logrus.Entry,
) (map[string]string, error) {
	reserves, err := reserveCIDRsForSubnet(context.TODO(), syncher.vpcClient, resource, sbnID, cidrs, cidrOrigins, scopedLog)
	if err != nil {
		syncher.recordEvent(resource, corev1.EventTypeWarning, "IPReserveFailed",
			"Failed to reserve IP ranges for subnet %s: %v", sbnID, err)
		return nil, err
	}
	syncher.recordEvent(resource, corev1.EventTypeNormal, "IPReserveSuccess",
		"Successfully reserved %d IP range(s) for subnet %s", len(reserves), sbnID)
	return reserves, nil
}

// buildSubnetStatus builds the subnet status for PSTS.
// When a subnet is backed by PSTS reserves, AvailableIPNum should reflect the
// reserved CIDR capacity instead of the raw subnet-wide available IP number.
func buildSubnetStatus(
	sbn *ccev1.Subnet,
	matchedEndpoints []*ccev2.CCEEndpoint,
	sbnID string,
	reservedCIDRs map[string]string,
) ccev2.SubnetPodStatus {
	subnetStatus := ccev2.SubnetPodStatus{
		SubenetDetail: ccev2.SubenetDetail{
			AvailableIPNum:   sbn.Status.AvailableIPNum,
			Enable:           sbn.Status.Enable,
			HasNoMoreIP:      sbn.Status.HasNoMoreIP,
			ID:               sbn.Spec.ID,
			Name:             sbn.Spec.Name,
			AvailabilityZone: sbn.Spec.AvailabilityZone,
			CIDR:             sbn.Spec.CIDR,
			IPv6CIDR:         sbn.Spec.IPv6CIDR,
		},
		IPAllocations: make(map[string]string),
	}
	for _, ep := range matchedEndpoints {
		if ep.Status.Networking != nil {
			for _, addr := range ep.Status.Networking.Addressing {
				if addr.Subnet == sbnID {
					subnetStatus.IPAllocations[addr.IP] = ep.Namespace + "/" + ep.Name
				}
			}
		}
	}
	if len(reservedCIDRs) > 0 {
		availableInReserved := countAvailableIPsInReservedCIDRs(reservedCIDRs, subnetStatus.IPAllocations)
		if availableInReserved > subnetStatus.SubenetDetail.AvailableIPNum {
			availableInReserved = subnetStatus.SubenetDetail.AvailableIPNum
		}
		subnetStatus.SubenetDetail.AvailableIPNum = availableInReserved
		subnetStatus.SubenetDetail.HasNoMoreIP = subnetStatus.SubenetDetail.HasNoMoreIP || availableInReserved == 0
	}
	subnetStatus.PodCount = int32(len(subnetStatus.IPAllocations))
	return subnetStatus
}

func countAvailableIPsInReservedCIDRs(reservedCIDRs map[string]string, ipAllocations map[string]string) int {
	parsedCIDRs := make([]*net.IPNet, 0, len(reservedCIDRs))
	totalReserved := 0

	for cidr := range reservedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		parsedCIDRs = append(parsedCIDRs, ipNet)
		totalReserved = saturatingAddInt(totalReserved, countIPsInCIDR(ipNet))
	}
	if len(parsedCIDRs) == 0 {
		return 0
	}

	usedReserved := 0
	for ip := range ipAllocations {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}
		for _, ipNet := range parsedCIDRs {
			if ipNet.Contains(parsedIP) {
				usedReserved++
				break
			}
		}
	}
	if usedReserved >= totalReserved {
		return 0
	}
	return totalReserved - usedReserved
}

func countIPsInCIDR(ipNet *net.IPNet) int {
	if ipNet == nil {
		return 0
	}
	ones, bits := ipNet.Mask.Size()
	if ones < 0 || bits < 0 || ones > bits {
		return 0
	}

	maxInt := int(^uint(0) >> 1)
	count := new(big.Int).Lsh(big.NewInt(1), uint(bits-ones))
	if !count.IsInt64() || count.Int64() > int64(maxInt) {
		return maxInt
	}
	return int(count.Int64())
}

func saturatingAddInt(base, delta int) int {
	maxInt := int(^uint(0) >> 1)
	if delta > maxInt-base {
		return maxInt
	}
	return base + delta
}

func (p *pstsSyncher) Delete(namespace, pstsName string) error {
	scopedLog := pstsLog.WithFields(logrus.Fields{
		"namespace": namespace,
		"name":      pstsName,
	})

	// Get PSTS from lister to read annotations
	psts, err := pstsClient.Lister().PodSubnetTopologySpreads(namespace).Get(pstsName)
	if err != nil {
		if errors.IsNotFound(err) {
			scopedLog.Info("PSTS not found, skip unreserve")
			return nil
		}
		scopedLog.WithError(err).Error("Failed to get PSTS")
		return err
	}

	clusterID := operatorOption.Config.CCEClusterID

	// Unreserve CIDRs for each subnet (include subnets from annotation)
	allUnreserved := true
	subnetIDs := make(map[string]struct{})
	for sbnID := range psts.Spec.Subnets {
		subnetIDs[sbnID] = struct{}{}
	}

	annotatedSubnets := make(map[string]struct{})
	subnetReserveMap, _, err := p.getAllReserveInfoFromAnnotationWithFallback(psts, scopedLog, "during delete")
	if err != nil {
		return err
	}
	for sbnID := range subnetReserveMap {
		annotatedSubnets[sbnID] = struct{}{}
		subnetIDs[sbnID] = struct{}{}
	}

	for sbnID := range subnetIDs {
		// If subnet only exists in Spec and has no range/CIDR, it likely has no reserves
		if _, fromAnnotation := annotatedSubnets[sbnID]; !fromAnnotation {
			if allocations, ok := psts.Spec.Subnets[sbnID]; ok && !hasRangeOrCIDR(allocations) {
				continue
			}
		}
		if err := unreserveCIDRsForSubnet(context.TODO(), p.vpcClient, psts, sbnID, scopedLog); err != nil {
			scopedLog.WithError(err).Errorf("Failed to unreserve CIDRs for subnet %s", sbnID)
			allUnreserved = false
			// Continue to unreserve other subnets
		}
	}

	// Verify all reserves belonging to this PSTS are actually deleted from VPC
	if allUnreserved {
		for sbnID := range subnetIDs {
			// If subnet only exists in Spec and has no range/CIDR, skip verification
			if _, fromAnnotation := annotatedSubnets[sbnID]; !fromAnnotation {
				if allocations, ok := psts.Spec.Subnets[sbnID]; ok && !hasRangeOrCIDR(allocations) {
					continue
				}
			}
			remaining, err := p.listPSTSReservedCIDRsBySubnetID(psts, sbnID, clusterID, scopedLog)
			if err != nil {
				scopedLog.WithError(err).Errorf("Failed to list reserves for subnet %s", sbnID)
				allUnreserved = false
			} else if len(remaining) > 0 {
				scopedLog.Errorf("Subnet %s still has %d reserves belonging to this PSTS: %v", sbnID, len(remaining), remaining)
				allUnreserved = false
			}
		}
	}

	// Remove finalizer only if all CIDRs are verified unreserved
	if allUnreserved && hasPSTSFinalizer(psts) {
		pstsCopy := psts.DeepCopy()
		if removePSTSFinalizer(pstsCopy) {
			_, err = pstsClient.Update(pstsCopy)
			if err != nil {
				scopedLog.WithError(err).Error("Failed to remove PSTS finalizer")
				return err
			}
			scopedLog.Info("Removed PSTS finalizer after verified all CIDRs unreserved")
		}
	}

	if !allUnreserved {
		return fmt.Errorf("failed to unreserve all CIDRs or verification failed")
	}

	return nil
}

// listPSTSReservedCIDRsBySubnetID queries VPC to list all CIDR reserves belonging to this PSTS in the subnet
// Returns the list of CIDRs that still have reserves (with pagination support)
func (p *pstsSyncher) listPSTSReservedCIDRsBySubnetID(psts *ccev2.PodSubnetTopologySpread, sbnID, clusterID string, scopedLog *logrus.Entry) ([]string, error) {
	reserves, err := p.listPSTSReservedCIDRsBySubnetIDWithReserveID(psts, sbnID, clusterID, scopedLog)
	if err != nil {
		return nil, err
	}

	var remaining []string
	for cidr, reserveID := range reserves {
		remaining = append(remaining, cidr)
		scopedLog.Warnf("Found remaining reserve: CIDR=%s, ID=%s", cidr, reserveID)
	}

	return remaining, nil
}

// listPSTSReservedCIDRsBySubnetIDWithReserveID queries VPC to list all CIDR reserves belonging to this PSTS in the subnet
// Returns map: CIDR -> ReserveID (with pagination support)
func (p *pstsSyncher) listPSTSReservedCIDRsBySubnetIDWithReserveID(psts *ccev2.PodSubnetTopologySpread, sbnID, clusterID string, scopedLog *logrus.Entry) (map[string]string, error) {
	if p.vpcClient == nil {
		return nil, fmt.Errorf("vpc client is nil")
	}

	remaining := make(map[string]string)
	marker := ""

	for {
		listResult, err := p.vpcClient.ListIpreserve(&vpc.ListIpeserveArgs{
			SubnetId: sbnID,
			Marker:   marker,
			MaxKeys:  1000,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list reserves in subnet %s: %w", sbnID, err)
		}

		for _, reserve := range listResult.IpReserves {
			if isSelfReserve(reserve.Description, psts.Namespace, psts.Name, clusterID) {
				remaining[reserve.IpCidr] = reserve.IpReserveId
			}
		}

		// Check if there are more pages
		if !listResult.IsTruncated {
			break
		}
		marker = listResult.NextMarker
	}

	return remaining, nil
}

// hasRangeOrCIDR checks if allocations contain any range or CIDR
func hasRangeOrCIDR(allocations ccev2.CustomAllocationList) bool {
	for _, alloc := range allocations {
		if len(alloc.Range) > 0 || len(alloc.CIDRs) > 0 {
			return true
		}
	}
	return false
}
