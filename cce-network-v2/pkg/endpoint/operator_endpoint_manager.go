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
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	operatorWatchers "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
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

	fixedIPProvider       EndpointMunalAllocatorProvider
	pstsAllocatorProvider EndpointMunalAllocatorProvider

	localPool *localPool

	// k8sAPI is used to access the Kubernetes API
	k8sAPI        CCEEndpointGetterUpdater
	pstsLister    ccelister.PodSubnetTopologySpreadLister
	sbnLister     ccelisterv1.SubnetLister
	eventRecorder record.EventRecorder
}

func NewEndpointManager(getterUpdater CCEEndpointGetterUpdater, reuseIPImplement DirectIPAllocator) *EndpointManager {
	manager := &EndpointManager{
		directIPAllocator:      reuseIPImplement,
		fixedIPPoolEndpointMap: make(map[string]map[string]*ccev2.CCEEndpoint),
		localPool:              newLocalPool(),
		remoteIPPool:           make(map[string]ipamTypes.AllocationMap),

		k8sAPI:        getterUpdater,
		pstsLister:    k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister(),
		sbnLister:     k8s.CCEClient().Informers.Cce().V1().Subnets().Lister(),
		eventRecorder: k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: operatorName}),
	}

	manager.fixedIPProvider = &reuseIPAllocatorProvider{manager}
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

	return nil
}

func (manager *EndpointManager) Create(resource *ccev2.CCEEndpoint) error {
	return manager.Update(resource)
}

func (manager *EndpointManager) Update(resource *ccev2.CCEEndpoint) error {
	// not managed endpoint
	if !IsFixedIPEndpoint(resource) && !IsPSTSEndpoint(resource) {
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

		newNodeName = resource.Spec.Network.IPAllocation.NodeIP

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

	// reuse ip when pod rescheduler or recreate
	if (resource.Status.Networking != nil &&
		resource.Status.Networking.NodeIP == newNodeName &&
		len(resource.Status.Networking.Addressing) != 0) &&
		(resource.Status.ExternalIdentifiers != nil &&
			resource.Status.ExternalIdentifiers.K8sObjectID == resource.Spec.ExternalIdentifiers.K8sObjectID) {
		return nil
	}

	logEntry.Info("received to update delegate cep")

	if newStatus.Networking == nil {
		newStatus.Networking = &ccev2.EndpointNetworking{
			NodeIP: newNodeName,
		}
	}
	if IsPSTSEndpoint(resource) {
		err = manager.pstsAllocatorProvider.AllocateIP(ctx, logEntry, newObj)
	} else if IsFixedIPEndpoint(resource) {
		err = manager.fixedIPProvider.AllocateIP(ctx, logEntry, newObj)
	}

	if err != nil || newStatus.Networking == nil || len(newStatus.Networking.Addressing) == 0 {
		AppendEndpointStatus(newStatus, models.EndpointStateInvalid, models.EndpointStatusChangeCodeFailed)
		goto update
	}

	if newStatus.State != string(models.EndpointStatePodDeleted) {
		AppendEndpointStatus(newStatus, models.EndpointStateIPAllocated, models.EndpointStatusChangeCodeOk)
	}

	newStatus.Networking.IPs = newStatus.Networking.Addressing.ToIPsString()
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
		if apiError != nil {
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
	return apiError
}

// handlerUpdateResult 是EndpointManager结构体的一个方法，用于处理更新结果的回调函数
func (manager *EndpointManager) handlerUpdateResult(ctx context.Context, start time.Time, logEntry *logrus.Entry, err error, newObj, oldObj *ccev2.CCEEndpoint) {
	// case 1: successfully assigned IP, and the new and old IPs are different
	// The remote IP has been released, so the local IP is directly released
	if err == nil {
		manager.localPool.addEndpointAddress(newObj)

		if oldObj.Status.Networking != nil && len(oldObj.Status.Networking.Addressing) > 0 {
			var oldIPs = map[string]*ccev2.AddressPair{}
			for i := range oldObj.Status.Networking.Addressing {
				oldIPs[oldObj.Status.Networking.Addressing[i].IP] = oldObj.Status.Networking.Addressing[i]
			}
			for _, existingAddr := range newObj.Status.Networking.Addressing {
				if _, ok := oldIPs[existingAddr.IP]; ok {
					delete(oldIPs, existingAddr.IP)
				}
			}
			for _, oldAddr := range oldIPs {
				manager.localPool.releaseLocalIPs(oldAddr)
			}
		}
	} else {
		logEntry.WithError(err).Errorf("failed to handle cep update, try to release the allocated IPs")
		// case 2: failed to assign the new IP, just release the new IPs
		if oldObj.Status.Networking == nil || len(oldObj.Status.Networking.Addressing) == 0 {
			if oldObj.Status.Networking == nil || len(oldObj.Status.Networking.Addressing) == 0 {
				logEntry.Info("cep is not allocated, no need to release the IP")
				return
			}
			operation, err := manager.directIPAllocator.NodeEndpoint(newObj)
			if err != nil {
				logEntry.Errorf("can not gc the allocated failture, failed to get node endpoint %v", err)
			}
			action := &DirectIPAction{
				NodeName:   newObj.Spec.Network.IPAllocation.NodeIP,
				Owner:      newObj.Namespace + "/" + newObj.Name,
				Addressing: newObj.Status.Networking.Addressing,
			}
			err = operation.DeleteIP(ctx, action)
			if err != nil {
				logEntry.WithField("addrs", logfields.Repr(newObj.Status.Networking.Addressing)).
					WithError(err).
					Errorf("release the allocated IPs failed")
			}

			logEntry.WithField("addrs", logfields.Repr(newObj.Status.Networking.Addressing)).Info("release the allocated IPs success")
			if newObj.Status.Networking != nil && len(newObj.Status.Networking.Addressing) > 0 {
				for _, newAddr := range newObj.Status.Networking.Addressing {
					manager.localPool.releaseLocalIPs(newAddr)
				}
			}
		}
	}
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
	log.Debug("1. try to delete delegate cep")

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

	if !IsFixedIPEndpoint(newCEP) && !IsPSTSEndpoint(newCEP) {
		return nil
	}
	log.Debug("received to delete delegate cep")

	if newCEP.Status.Networking == nil || len(newCEP.Status.Networking.Addressing) == 0 {
		log.Info("remove remote ip finalizer while no addressing to delete")
		goto removeFinalizer
	}

	operation, err = manager.directIPAllocator.NodeEndpoint(newCEP)
	if err != nil {
		log.Errorf("failed to get node endpoint %v", err)
		return err
	}
	action = &DirectIPAction{
		NodeName:   newCEP.Spec.Network.IPAllocation.NodeIP,
		Owner:      namespace + "/" + name,
		Addressing: newCEP.Status.Networking.Addressing,
	}
	err = operation.DeleteIP(ctx, action)
	// if err is eni not found, it means that the ip has been released, just ignore it and continue to remove finalizer
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("failed to delete ip: %v", err)
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
	manager.gcReusedEndpointTTL()
	return nil
}

// gcReusedEndpointTTL delete fixed ip endpoint when ttl is expired
func (manager *EndpointManager) gcReusedEndpointTTL() {
	var (
		logEntry = managerLog.WithFields(logrus.Fields{
			logfields.LogSubsys: "EndpointManager",
			"event":             "gcFixedEndpoint",
		})
		epToDelete []*ccev2.CCEEndpoint
	)
	// for each all endpoints to check endpoint ttl
	ceps, err := manager.k8sAPI.Lister().CCEEndpoints(metav1.NamespaceAll).List(labels.Everything())
	if err != nil {
		logEntry.WithError(err).Error("failed to get ceps")
		return
	}
	for _, cep := range ceps {
		if !IsFixedIPEndpoint(cep) && !IsPSTSEndpoint(cep) {
			continue
		}
		if cep.Spec.Network.IPAllocation.ReleaseStrategy != ccev2.ReleaseStrategyTTL {
			continue
		}

		// if endpoint is not fixed ip mode, skip it
		if cep.Spec.Network.IPAllocation.Type != ccev2.IPAllocTypeElastic &&
			cep.Spec.Network.IPAllocation.Type != ccev2.IPAllocTypeFixed {
			continue
		}

		log := logEntry.WithFields(logrus.Fields{
			"namespace": cep.Namespace,
			"name":      cep.Name,
			"step":      "gcFixedIPEndpoint",
		})
		// repair endpoint status for endpoint is 'deleted' while pod is 'running'
		// BUG: may cause by operator and agent update endpoint currently
		if manager.repairEPState(cep, log) {
			continue
		}

		if cep.Spec.Network.IPAllocation.ReleaseStrategy != ccev2.ReleaseStrategyTTL ||
			cep.Spec.Network.IPAllocation.Type != ccev2.IPAllocTypeElastic {
			continue
		}

		// if endpoint is not reused ip mode, skip it
		if cep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted == nil {
			expireDuration := int64(operatorOption.Config.FixedIPTTL.Seconds())
			cep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted = &expireDuration
		}

		if cep.Status.State == string(models.EndpointStatePodDeleted) {
			if len(cep.Status.Log) == 0 {
				log.Warningf("gc fixed ip failed, no controller log in endpoint status")
				continue
			}

			expireDuration := operatorOption.Config.FixedIPTTL
			if cep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted != nil {
				userTTL := *cep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted
				if userTTL != 0 {
					expireDuration = time.Duration(userTTL) * time.Second
				}
			}
			podDeletedTime, err := time.Parse(time.RFC3339, cep.Status.Log[len(cep.Status.Log)-1].Timestamp)
			if err != nil {
				log.WithError(err).Error("gc fixed ip failed")
				continue
			}
			if time.Now().After(podDeletedTime.Add(expireDuration)) {
				log.Infof("fixed ip endpoint is expired after delete %s, prepare to delete", expireDuration.String())
				epToDelete = append(epToDelete, cep)
			}
		}

	}

	// delete ip in cloud provider and endpoint in k8s
	for _, cep := range epToDelete {
		err := manager.k8sAPI.Delete(cep.Namespace, cep.Name)
		logEntry.WithFields(logrus.Fields{
			"namespace": cep.Namespace,
			"name":      cep.Name,
			"step":      "repair",
		}).WithError(err).Info("try to delete cep")
	}
}

// repairEPState repair endpoint state
func (manager *EndpointManager) repairEPState(old *ccev2.CCEEndpoint, log *logrus.Entry) bool {
	log = log.WithFields(logrus.Fields{
		"step": "repair",
	})
	needUpdate := false
	newStatus := models.EndpointStateIPAllocated
	pod, err := operatorWatchers.PodClient.Lister().Pods(old.Namespace).Get(old.Name)
	if err == nil && pod != nil {
		if pod.DeletionTimestamp != nil {
			return false
		}
		if old.Status.State == string(models.EndpointStatePodDeleted) {
			needUpdate = true
			if pod.Status.Phase == corev1.PodPending {
				newStatus = models.EndpointStateRestoring
			}
		} else if old.Status.State == string(models.EndpointStateRestoring) {
			if old.Status.ExternalIdentifiers != nil &&
				old.Status.ExternalIdentifiers.K8sObjectID == old.Spec.ExternalIdentifiers.K8sObjectID &&
				old.Status.Networking != nil && len(old.Status.Networking.Addressing) > 0 {
				needUpdate = true
			}
		}
		// pod have been deleted
	} else if errors.IsNotFound(err) && old.Status.State != string(models.EndpointStatePodDeleted) {
		newStatus = models.EndpointStatePodDeleted
		needUpdate = true
	}
	if needUpdate {
		newE := old.DeepCopy()
		if !AppendEndpointStatus(&newE.Status, newStatus, models.EndpointStatusChangeCodeOk) {
			return false
		}
		_, err := manager.k8sAPI.Update(newE)
		log.WithError(err).Info("try to repair fixed endpoint")
	}
	return needUpdate
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
