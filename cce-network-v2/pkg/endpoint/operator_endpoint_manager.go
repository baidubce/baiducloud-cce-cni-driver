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
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/scheme"
	ccelisterv1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
	ccelister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/bce-sdk-go/services/vpc"
)

var (
	managerLog   = logging.NewSubysLogger("endpoint-manager")
	operatorName = "endpoint-manager"
)

type EndpointManager struct {
	directIPAllocator DirectIPAllocator

	// fixedIPPoolEndpointMap map with key subnetID/IP
	fixedIPPoolEndpointMap map[string]map[string]*ccev2.CCEEndpoint

	remoteIPPool map[string]ipamTypes.AllocationMap

	pstsAllocatorProvider EndpointMunalAllocatorProvider

	// localPool manages all PSTS local IP pools globally.
	// Each EndpointManager instance has exactly one localPool instance (singleton per operator).
	localPool *localPool

	// VPC client for IP reserve operations
	vpcClient *vpc.Client

	// k8sAPI is used to access the Kubernetes API
	k8sAPI        CCEEndpointGetterUpdater
	pstsLister    ccelister.PodSubnetTopologySpreadLister
	sbnLister     ccelisterv1.SubnetLister
	nrsLister     ccelister.NetResourceSetLister
	eventRecorder record.EventRecorder
	syncedPool    sync.Map // key: string, value: string (revision)
}

func NewEndpointManager(getterUpdater CCEEndpointGetterUpdater, reuseIPImplement DirectIPAllocator, vpcClient *vpc.Client) *EndpointManager {
	manager := &EndpointManager{
		directIPAllocator:      reuseIPImplement,
		fixedIPPoolEndpointMap: make(map[string]map[string]*ccev2.CCEEndpoint),
		localPool:              newLocalPool(),
		remoteIPPool:           make(map[string]ipamTypes.AllocationMap),
		vpcClient:              vpcClient,

		k8sAPI:        getterUpdater,
		pstsLister:    k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister(),
		sbnLister:     k8s.CCEClient().Informers.Cce().V1().Subnets().Lister(),
		nrsLister:     k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister(),
		eventRecorder: k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: operatorName}),
	}

	manager.pstsAllocatorProvider = &pstsAllocatorProvider{EndpointManager: manager}
	return manager
}

// Start kicks of the NodeManager by performing the initial state
// synchronization and starting the background sync go routine
func (manager *EndpointManager) Start(ctx context.Context) error {
	// Start an interval based  background resync for safety, it will
	// synchronize the state regularly and resolve eventual deficit if the
	// event driven trigger fails, and also release excess IP addresses
	// if release-excess-ips is enabled
	mngr := controller.NewManager()
	controllerName := "ipam-delegate-endpoint-interval-refresh"
	go func() {
		mngr.UpdateController(controllerName,
			controller.ControllerParams{
				RunInterval: operatorOption.Config.EndpointGCInterval,
				DoFunc:      manager.Resync,
			})
	}()
	mngr.TriggerController(controllerName)

	k8s.CCEClient().Informers.Cce().V1().Subnets().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: manager.localPool.updateSubnet,
			DeleteFunc: manager.deleteSubnet,
		},
	)

	sbnList, err := manager.sbnLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, sbn := range sbnList {
		manager.localPool.updateSubnet(nil, sbn)
	}

	// Watch PSTS resources to maintain local IP pools for PSTS allocations
	// Note: This watch is separate from operator/watchers/psts.go which handles PSTS lifecycle.
	// Here we only care about PSTS annotation changes (IPReserve info) to sync local pools.
	// TODO: Consider moving this to a dedicated PSTS pool manager to separate concerns.
	k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				manager.localPool.updatePSTS(nil, obj)
			},
			UpdateFunc: manager.localPool.updatePSTS,
			DeleteFunc: manager.localPool.deletePSTS,
		},
	)

	// Initialize existing PSTS pools
	pstsList, err := manager.pstsLister.List(labels.Everything())
	if err != nil {
		managerLog.WithError(err).Error("Failed to list PSTS resources")
	} else {
		for _, psts := range pstsList {
			manager.localPool.updatePSTS(nil, psts)
		}
	}

	// Restore realOccupied state in PSTS local pools from existing CEPs.
	ceps, err := manager.k8sAPI.Lister().List(labels.Everything())
	if err != nil {
		managerLog.WithError(err).Error("Failed to list CCEEndpoints for local pool restore")
	} else {
		totalIPs, marked := manager.localPool.restoreOccupiedFromCEPs(ceps)
		managerLog.Infof("Restored PSTS local pool occupancy from CEPs: totalIPs=%d, marked=%d", totalIPs, marked)
	}

	return nil
}

func (manager *EndpointManager) Create(resource *ccev2.CCEEndpoint) error {
	return manager.Update(resource)
}

func (manager *EndpointManager) Update(resource *ccev2.CCEEndpoint) error {
	// not managed endpoint
	if !IsPSTSEndpoint(resource) {
		return nil
	}

	var (
		ctx, cancel = context.WithTimeout(context.Background(), operatorOption.Config.FixedIPTimeout)
		start       = time.Now()
		err         error
		apiError    error
		logEntry    = managerLog.WithFields(logrus.Fields{
			"namespace": resource.Namespace,
			"name":      resource.Name,
			"event":     "update",
		})

		newNodeName = resource.Spec.Network.IPAllocation.NodeName

		newObj    = resource.DeepCopy()
		newStatus = &newObj.Status
		returnObj *ccev2.CCEEndpoint
	)

	defer func() {
		if apiError != nil {
			logEntry.WithError(apiError).Errorf("update cep to apiserver failed")
			if err == nil {
				err = apiError
			}
		}
		if err != nil {
			logEntry.WithError(err).Errorf("handler cep update event failed")
		}
		manager.handlerUpdateResult(ctx, start, logEntry, err, newObj, resource)
		cancel()
	}()

	// Generate unique identifier for CEP
	cepKey := fmt.Sprintf("%s/%s", resource.Namespace, resource.Name)

	// Check if CEP is in synchronization pool and compare revision
	if manager.isInSyncedPool(cepKey) {
		storedRevision := manager.getSyncedPoolRevision(cepKey)
		if storedRevision != "" {
			isNewer, err := compareResourceVersions(resource.ResourceVersion, storedRevision)
			if err != nil {
				logEntry.WithError(err).Error("Failed to compare resource versions")
				return err
			}
			if !isNewer {
				logEntry.Infof("CEP %s/%s is stale (current: %s, stored: %s), skipping update",
					resource.Namespace, resource.Name, resource.ResourceVersion, storedRevision)
				return nil
			}
			logEntry.Infof("CEP %s/%s has newer version (current: %s, stored: %s), removing from synced pool",
				resource.Namespace, resource.Name, resource.ResourceVersion, storedRevision)
			manager.removeFromSyncedPool(cepKey)
		}
	}

	// Check if Pod has been deleted (status changed to PodDeleted)
	// This must be checked BEFORE the reuse short-circuit below,
	// because a PodDeleted CEP still has valid Networking/Addressing/K8sObjectID.
	// For PSTS Fixed mode endpoints, release IP from ENI when Pod is deleted
	// Note: Elastic mode endpoints are deleted immediately by Agent, so they won't reach here
	if newStatus.State == string(models.EndpointStatePodDeleted) && IsPSTSEndpoint(resource) {
		err = manager.handlePodDeletedIPRelease(ctx, resource, logEntry)
		if err != nil {
			// Return error to trigger retry - critical operations must succeed
			logEntry.WithError(err).Error("Failed to handle Pod deleted IP release, will retry")
			return err
		}
		// Don't allocate new IP, just update status
		goto update
	}

	// Reuse IP when pod rescheduler or recreate.
	// Do not short-circuit when CEP is in "restoring" state, otherwise state cannot
	// progress to "ip-allocated" and may get stuck.
	if shouldSkipReuseUpdate(resource, newNodeName) {
		return nil
	}

	logEntry.Infof("received to update delegate cep: %s/%s, %v", newObj.Namespace, newObj.Name, newStatus.Networking)

	if newStatus.Networking == nil {
		newStatus.Networking = &ccev2.EndpointNetworking{
			NodeIP: newNodeName,
		}
	}

	// Allocate IP for PSTS Fixed mode endpoints
	err = manager.pstsAllocatorProvider.AllocateIP(ctx, logEntry, newObj)
	if err != nil || newStatus.Networking == nil || len(newStatus.Networking.Addressing) == 0 {
		errMsg := "IP allocation failed"
		if err != nil {
			errMsg = err.Error()
		}
		AppendEndpointStatusWithMessage(newStatus, models.EndpointStateInvalid, models.EndpointStatusChangeCodeFailed, errMsg)
		goto update
	}

	if newStatus.State != string(models.EndpointStatePodDeleted) {
		AppendEndpointStatus(newStatus, models.EndpointStateIPAllocated, models.EndpointStatusChangeCodeOk)
	}

	newStatus.Networking.IPs = newStatus.Networking.Addressing.ToIPsString()
	newStatus.Networking.NodeIP = newNodeName
	newStatus.ExternalIdentifiers = resource.Spec.ExternalIdentifiers
	newObj.Status = *newStatus

update:
	// update to k8s apiserver
	// Note that if writing to apiserver is unsuccessful, it may lead to local IP leakage
	apiError = wait.PollImmediateUntilWithContext(ctx, 200*time.Millisecond, func(context.Context) (bool, error) {
		// get the latest cep
		returnObj, apiError = manager.k8sAPI.Lister().CCEEndpoints(newObj.Namespace).Get(newObj.Name)
		if apiError != nil {
			return false, fmt.Errorf("get cep %s/%s failed: %v", newObj.Namespace, newObj.Name, apiError)
		}
		returnObj = returnObj.DeepCopy()

		// Force the latest state to be written to the object
		returnObj.Status = *newStatus
		returnObj, apiError = manager.k8sAPI.UpdateStatus(returnObj)
		// retry if conflict
		if errors.IsConflict(apiError) || errors.IsResourceExpired(apiError) {
			return false, nil
		}
		if apiError != nil || returnObj == nil {
			logEntry.WithError(apiError).Errorf("update cep failed")
			return false, apiError
		}

		return true, apiError
	})
	if apiError != nil {
		logEntry.WithError(apiError).Errorf("update cep failed")
		return apiError
	}
	logEntry.Infof("success to update cep")
	newObj = returnObj

	// Add CEP to synchronization pool after successful update with new revision
	manager.addToSyncedPool(cepKey, newObj.ResourceVersion)
	logEntry.Infof("add cep %s/%s to synced pool with revision %s", newObj.Namespace, newObj.Name, newObj.ResourceVersion)

	return apiError
}

func shouldSkipReuseUpdate(resource *ccev2.CCEEndpoint, newNodeName string) bool {
	if resource == nil {
		return false
	}
	if resource.Status.State == string(models.EndpointStateRestoring) {
		return false
	}
	return (resource.Status.Networking != nil &&
		resource.Status.Networking.NodeIP == newNodeName &&
		len(resource.Status.Networking.Addressing) != 0) &&
		(resource.Status.ExternalIdentifiers != nil &&
			resource.Spec.ExternalIdentifiers != nil &&
			resource.Status.ExternalIdentifiers.K8sObjectID == resource.Spec.ExternalIdentifiers.K8sObjectID)
}

// handlerUpdateResult handles the result of CEP update operation
// Only records metrics - local pool state is managed by Informer (addEndpointAddress/deleteEndpointAddress)
func (manager *EndpointManager) handlerUpdateResult(ctx context.Context, start time.Time, logEntry *logrus.Entry, err error, newObj, oldObj *ccev2.CCEEndpoint) {
	if err != nil {
		logEntry.WithError(err).Error("Failed to handle CEP update")
	}
	// Record metrics for monitoring
	manager.recordEvent(metrics.LabelEventMethodUpdate, start, err, newObj)
}

// Delete removes the endpoint from the endpointMap
func (manager *EndpointManager) Delete(namespace, name string) error {
	var (
		log = managerLog.WithFields(logrus.Fields{
			logfields.LogSubsys: "EndpointManager",
			"namespace":         namespace,
			"name":              name,
			"event":             "delete",
		})
		err       error
		ctx       = context.TODO()
		operation DirectEndpointOperation
		action    *DirectIPAction
		start     = time.Now()
	)
	log.Debug("try to delete delegate cep")

	// not manager by endpoint manager
	cep, err := manager.k8sAPI.Lister().CCEEndpoints(namespace).Get(name)
	if errors.IsNotFound(err) {
		log.Debugf("cep %s/%s not found", namespace, name)
		return nil
	}
	if err != nil {
		log.WithError(err).Errorf("failed to get cep %s/%s", namespace, name)
		return err
	}
	newCEP := cep.DeepCopy()

	defer func() {
		if err == nil {
			manager.localPool.deleteEndpointAddress(newCEP)
		}
		manager.recordEvent(metrics.LabelEventMethodDelete, start, err, cep)
	}()

	if !IsPSTSEndpoint(newCEP) {
		// if the node of cep is not exist, we should delete this cep immediately,
		// else if the node of cep is exist, we should wait for the agent to gc this cep
		if newCEP != nil && newCEP.Spec.Network.IPAllocation != nil {
			_, err = manager.nrsLister.Get(newCEP.Spec.Network.IPAllocation.NodeName)
			if errors.IsNotFound(err) {
				goto removeFinalizer
			}
			return nil
		}
		goto removeFinalizer
	}

	log.Debug("received to delete delegate cep")

	if newCEP.Status.Networking == nil || len(newCEP.Status.Networking.Addressing) == 0 {
		log.Info("remove remote ip finalizer while no addressing to delete")
		goto removeFinalizer
	}

	// Release IP from ENI - critical to free ENI resources
	operation, err = manager.directIPAllocator.NodeEndpoint(newCEP)
	if err != nil {
		if errors.IsNotFound(err) {
			log.WithError(err).Warn("node endpoint not found while deleting CEP, treat as success and remove finalizer")
			goto removeFinalizer
		}
		log.Errorf("failed to get node endpoint %v", err)
		return err
	}
	if operation == nil {
		log.Warn("node endpoint operation is nil while deleting CEP, skip release and remove finalizer")
		goto removeFinalizer
	}
	action = &DirectIPAction{
		NodeName:   newCEP.Spec.Network.IPAllocation.NodeName,
		Owner:      namespace + "/" + name,
		Addressing: newCEP.Status.Networking.Addressing,
	}
	err = operation.DeleteIP(ctx, action)
	// if err is eni not found, it means that the ip has been released, just ignore it and continue to remove finalizer
	if err != nil && !errors.IsNotFound(err) && !cloud.IsIPAlreadyDeleted(err) {
		log.Errorf("failed to delete IP from ENI, will retry: %v", err)
		return err
	}

removeFinalizer:
	// remove finalizer when ips have been released from direct ip allocator
	if k8s.FinalizerRemoveRemoteIP(newCEP) {
		_, err = manager.k8sAPI.Update(newCEP)
		if err != nil {
			return fmt.Errorf("failed to remove cep finallizer :%w", err)
		}
	}
	log.Info("remove RemoteIPFinalizer finaller for cep success")

	return nil
}

func (manager *EndpointManager) Resync(ctx context.Context) error {
	// Note: PSTS Fixed mode CEP TTL-based GC is handled by Agent's pstsFixedIPEndpointsGC()
	// Operator only handles finalizer removal when CEP is deleted
	return nil
}

func (manager *EndpointManager) recordEvent(op string, start time.Time, err error, obj *ccev2.CCEEndpoint) {
	errstr := metrics.LabelValueOutcomeSuccess
	if err != nil {
		errstr = metrics.LabelError
		manager.eventRecorder.Eventf(obj, corev1.EventTypeWarning, "HandlerCEPFailed", "failed to handler cep %s: %v", op, err.Error())
	}
	operatorMetrics.ControllerHandlerDurationMilliseconds.WithLabelValues(operatorOption.Config.CCEClusterID, operatorName, op, errstr).Observe(float64(time.Since(start).Milliseconds()))
}

var _ EndpointEventHandler = &EndpointManager{}

// Helper methods for SyncedPool management
// addToSyncedPool adds a CEP to the synchronization pool with current revision
func (manager *EndpointManager) addToSyncedPool(key string, revision string) {
	manager.syncedPool.Store(key, revision)
	managerLog.WithFields(logrus.Fields{
		"key":      key,
		"revision": revision,
	}).Debug("Added CEP to synchronization pool")
}

// removeFromSyncedPool removes a CEP from the synchronization pool
func (manager *EndpointManager) removeFromSyncedPool(key string) {
	manager.syncedPool.Delete(key)
	managerLog.WithFields(logrus.Fields{
		"key": key,
	}).Debug("Removed CEP from synchronization pool")
}

// isInSyncedPool checks if a CEP exists in the synchronization pool
func (manager *EndpointManager) isInSyncedPool(key string) bool {
	_, exists := manager.syncedPool.Load(key)
	return exists
}

func (manager *EndpointManager) getSyncedPoolRevision(key string) string {
	if value, exists := manager.syncedPool.Load(key); exists {
		if revision, ok := value.(string); ok {
			return revision
		}
	}
	return ""
}

// handlePodDeletedIPRelease handles IP release when Pod is deleted (PSTS Fixed mode)
// Returns error to trigger retry if critical operations fail.
func (manager *EndpointManager) handlePodDeletedIPRelease(ctx context.Context, cep *ccev2.CCEEndpoint, logEntry *logrus.Entry) error {
	if cep.Status.Networking == nil || len(cep.Status.Networking.Addressing) == 0 {
		return nil
	}

	logEntry.Info("Pod deleted (Fixed mode), handling IP release")

	// Release IP from ENI
	// This is critical to free ENI resources, should retry on failure
	if manager.directIPAllocator == nil {
		return fmt.Errorf("direct IP allocator is nil")
	}
	operation, err := manager.directIPAllocator.NodeEndpoint(cep)
	if err != nil {
		if errors.IsNotFound(err) {
			logEntry.WithError(err).Warn("Node endpoint not found on Pod deletion, treat as success")
			return nil
		}
		logEntry.WithError(err).Error("Failed to get direct IP operation on Pod deletion, will retry")
		return fmt.Errorf("failed to get direct IP operation: %w", err)
	}
	if operation == nil {
		logEntry.Error("Direct IP operation is nil on Pod deletion, will retry")
		return fmt.Errorf("direct IP operation is nil")
	}

	action := &DirectIPAction{
		NodeName:   cep.Spec.Network.IPAllocation.NodeName,
		Owner:      cep.Namespace + "/" + cep.Name,
		Addressing: cep.Status.Networking.Addressing,
	}
	if err := operation.DeleteIP(ctx, action); err != nil {
		if errors.IsNotFound(err) || cloud.IsIPAlreadyDeleted(err) {
			logEntry.WithError(err).Warn("IP already deleted or ENI not found, treat as success")
		} else {
			logEntry.WithError(err).Error("Failed to release IP from ENI on Pod deletion, will retry")
			return fmt.Errorf("failed to release IP from ENI: %w", err)
		}
	}
	logEntry.Info("Successfully released IP from ENI on Pod deletion")

	return nil
}

// compareResourceVersions compares two resourceVersion strings and returns true if current is newer than stored.
func compareResourceVersions(current, stored string) (bool, error) {
	curRV, err1 := resource.ParseQuantity(current)
	storedRV, err2 := resource.ParseQuantity(stored)
	if err1 != nil || err2 != nil {
		return false, fmt.Errorf("invalid resource version format: current=%s, stored=%s", current, stored)
	}
	return curRV.Cmp(storedRV) >= 0, nil
}
