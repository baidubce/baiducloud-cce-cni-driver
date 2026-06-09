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

// Package vpceni provides VPC ENI functionality for CCE network driver.
//
// This package includes functionality to set node conditions when errors occur,
// allowing Kubernetes to track the health status of nodes. The following node
// conditions are supported:
//
// - CCESubnetUnavailable: Indicates that subnets are not available for IP allocation
//
// When an error occurs, both an event and a node condition are set to provide
// comprehensive error tracking and monitoring capabilities.
package vpceni

import (
	"context"
	goerrors "errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/cenkalti/backoff/v4"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	bceconstants "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/constants"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/nodeutils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

const (
	DayDuration = 24 * time.Hour
)

func init() {
	ccev2.AddToScheme(scheme.Scheme)
}

// The following error constants represent the error conditions for
// CreateInterface without additional context embedded in order to make them
// usable for metrics accounting purposes.
const (
	errUnableToDetermineLimits    = "unable to determine limits"
	errUnableToGetEniQuota        = "unable to get ENI quota"
	errUnableToGetSecurityGroups  = "unable to get security groups"
	errUnableToCreateENI          = "unable to create ENI"
	errUnableToAttachENI          = "unable to attach ENI"
	errCrurrentlyCreatingENI      = "the node currently being created by ENI should have only one"
	errUnableToMarkENIForDeletion = "unable to mark ENI for deletion"
	errNoAvailableSubnet          = "no available subnet for ENI creation"

	retryDuration = 500 * time.Millisecond
	retryTimeout  = 15 * time.Second
	retryDelay    = 15 * time.Second

	vpcENISubsys = "vpc-eni-node-allocator"

	// createInterfaceCooldownPeriod is the cooldown period after CreateInterface
	// encounters a non-retryable error (e.g., no available subnet or subnet IP exhaustion).
	// During this period, subsequent calls will fail fast without making API requests,
	// reducing unnecessary QPS to the VPC API when the underlying issue cannot be resolved
	// by retrying. The cooldown automatically expires after this duration, allowing the
	// system to retry when conditions may have changed (e.g., new subnet added, IPs freed).
	createInterfaceCooldownPeriod = 5 * time.Minute
)

type realNodeInf interface {
	refreshENIQuota(scopeLog *logrus.Entry) (ENIQuotaManager, error)
	createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error)
	// PrepareIPAllocation is called to calculate the number of IPs that
	// can be allocated on the node and whether a new network interface
	// must be attached to the node.
	prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error)
	// AllocateIPs is called after invoking PrepareIPAllocation and needs
	// to perform the actual allocation.
	allocateIPs(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
		ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error)

	releaseIPs(ctx context.Context, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error

	// direct ip allocation
	allocateIPCrossSubnet(ctx context.Context, sbnID string) ([]*models.PrivateIP, string, error)
	// ReuseIPs is called to reuse the IPs that are not used by any pods
	reuseIPs(ctx context.Context, ips []*models.PrivateIP, owner, clientToken string) (eniID string, err error)
}

// bceNetworkResourceSet represents a Kubernetes node running CCE with an associated
// NetResourceSet custom resource
type bceNetworkResourceSet struct {
	// node contains the general purpose fields of a node
	node *ipam.NetResource

	// mutex protects members below this field
	mutex lock.RWMutex

	// k8sObj is the NetResourceSet custom resource representing the node
	k8sObj *ccev2.NetResourceSet

	// manager is the ecs node manager responsible for this node
	manager *InstancesManager

	bceclient cloud.Interface

	// instanceID of the node
	instanceID   string
	instanceType string

	// The node currently being created by ENI should have only one.
	creatingEni *creatingEniSet

	// eniQuota is the quota manager for ENI
	eniQuota               ENIQuotaManager
	lastResyncEniQuotaTime time.Time
	limiterLock            lock.RWMutex

	// nextCreateENITime not allow create eni before this time
	nextCreateENITime *time.Time

	// lastCreateInterfaceFailureTime records the last time CreateInterface failed with non-retryable error
	// (e.g., subnet IP exhaustion). Used to implement cooldown period to reduce QPS to VPC API.
	lastCreateInterfaceFailureTime *time.Time
	// lastCreateInterfaceFailureError records the last non-retryable error from CreateInterface
	lastCreateInterfaceFailureError error

	// clientTokens stores client tokens for ENI creation and IP allocation idempotency
	clientTokens *bceutils.ClientTokenSet

	real realNodeInf

	// whatever the resource version of the eni have been synced
	// this key is eni id, value is the resource version
	expiredVPCVersion map[string]int64

	eventRecorder record.EventRecorder

	log *logrus.Entry

	// availableSubnets subnets that can be used to allocate IPs and create ENIs
	availableSubnets []*bcesync.BorrowedSubnet
	// availableIPsNumber is the number of IPs that can be allocated on this node
	availableIPsNumber      int
	errorLock               lock.Mutex
	eniAllocatedIPsErrorMap map[string]*ccev2.StatusChange

	securityGroupIDs           []string
	enterpriseSecurityGroupIDs []string
}

// NewBCENetworkResourceSet returns a new *bceNetworkResourceSet
func NewBCENetworkResourceSet(node *ipam.NetResource, k8sObj *ccev2.NetResourceSet, manager *InstancesManager) *bceNetworkResourceSet {
	// 1. Create a new CCE Node object
	bceNrs := &bceNetworkResourceSet{
		node:                    node,
		k8sObj:                  k8sObj,
		manager:                 manager,
		bceclient:               manager.bceclientd,
		instanceID:              k8sObj.Spec.InstanceID,
		expiredVPCVersion:       make(map[string]int64),
		log:                     logging.NewSubysLogger(vpcENISubsys).WithField("instanceID", k8sObj.Spec.InstanceID).WithField("nodeName", k8sObj.Name),
		eventRecorder:           k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: vpcENISubsys}),
		eniAllocatedIPsErrorMap: make(map[string]*ccev2.StatusChange),
		creatingEni:             newCreatingEniSet(),
		clientTokens:            bceutils.NewClientTokenSet(),
	}

	// 2. Get isSuportedENI from ccev2.NetResourceSet Label[k8s.LabelIsSupportENI], this Label is setted by nodediscovery determined by metadata api.
	if bceNrs.k8sObj.Spec.ENI != nil {
		switch bceNrs.k8sObj.Spec.ENI.InstanceType {
		case string(metadata.InstanceTypeExBBC):
			bceNrs.real = newBBCNetworkResourceSet(bceNrs)
		case string(metadata.InstanceTypeExEBC), string(metadata.InstanceTypeExEHC):
			bceNrs.real = newEBCNetworkResourceSet(newBCCNetworkResourceSet(bceNrs))
		case string(metadata.InstanceTypeExHPAS):
			if operatorOption.Config.HPASStanderd { // HPAS 标准版使用 bceclient
				bceNrs.bceclient = manager.bceclientd
			} else { // HPAS 非标准版使用 hpasclient
				bceNrs.bceclient = manager.hpasclientd
			}
			bceNrs.real = newHPASNetworkResourceSet(newBCCNetworkResourceSet(bceNrs))
		default:
			bceNrs.real = newBCCNetworkResourceSet(bceNrs)
		}
		bceNrs.getENIQuota()
	}

	return bceNrs
}

var (
	foreachInstanceReadyOnNode = func(manager *InstancesManager, instanceID, nodeName string, fn ipamTypes.InterfaceIterator) error {
		return manager.ForeachInstanceReadyOnNode(instanceID, nodeName, fn)
	}
	forceGetENIFromInstanceGroupFunc = func(n *bceNetworkResourceSet, ctx context.Context, scopedLog *logrus.Entry, vpcID, instanceID string) (int, string, error) {
		return n.forceGetENIFromInstanceGroup(ctx, scopedLog, vpcID, instanceID)
	}
	forceGetENIFromIaaSFunc = func(n *bceNetworkResourceSet, ctx context.Context, scopedLog *logrus.Entry, vpcID, instanceID string) (int, string, error) {
		return n.forceGetENIFromIaaS(ctx, scopedLog, vpcID, instanceID)
	}
)

// GetBCEClient returns the BCE cloud client for IaaS operations
func (n *bceNetworkResourceSet) GetBCEClient() cloud.Interface {
	return n.bceclient
}

func (n *bceNetworkResourceSet) getENIQuota() ENIQuotaManager {
	n.limiterLock.RLock()
	if n.eniQuota != nil && n.lastResyncEniQuotaTime.Add(DayDuration).After(time.Now()) {
		n.limiterLock.RUnlock()
		return n.eniQuota
	}
	n.limiterLock.RUnlock()

	ctx := logfields.NewContext()
	var instanceType string
	if n.k8sObj.Spec.ENI != nil {
		instanceType = n.k8sObj.Spec.ENI.InstanceType
	}
	scopedLog := n.log.WithFields(logrus.Fields{
		"nodeName":     n.k8sObj.Name,
		"method":       "getENIQuota",
		"instanceType": instanceType,
	}).WithContext(ctx)

	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to get effective instanceID")
		return nil
	}

	// fast path to claculate eni quota when operaor has been restarted
	// isSetLimiter returns true if the node has set limiter
	// if the node has set limiter, it will return a limiter from the ENI spec
	if n.k8sObj.Annotations != nil && n.k8sObj.Annotations[k8s.AnnotationIPResourceCapacitySynced] != "" {
		lastResyncTime := n.k8sObj.Annotations[k8s.AnnotationIPResourceCapacitySynced]
		t, err := time.Parse(time.RFC3339, lastResyncTime)
		// if the last resync time is not set or expired one day ago, go to slow path
		if err != nil || t.Add(DayDuration).Before(time.Now()) {
			goto slowPath
		}
		eniQuota := newCustomerIPQuota(n.log, k8s.WatcherClient(), n.k8sObj.Name, effectiveInstanceID, nil)
		eniQuota.SetMaxENI(n.k8sObj.Spec.ENI.MaxAllocateENI)
		eniQuota.SetMaxIP(n.k8sObj.Spec.ENI.MaxIPsPerENI)
		// use customer max ip nums if defiend
		if operatorOption.Config.BCECustomerMaxIP != 0 && operatorOption.Config.BCECustomerMaxIP == eniQuota.GetMaxIP() {
			goto slowPath
		}

		n.limiterLock.Lock()
		n.eniQuota = eniQuota
		n.limiterLock.Unlock()
		return n.eniQuota
	}

slowPath:
	return n.slowCalculateRealENICapacity(scopedLog)
}

// slow path to claculate limiter
func (n *bceNetworkResourceSet) slowCalculateRealENICapacity(scopedLog *logrus.Entry) ENIQuotaManager {
	eniQuota, err := n.real.refreshENIQuota(scopedLog)
	if err != nil {
		scopedLog.Errorf("calculate eniQuota failed: %v", err)
		return nil
	}

	if len(n.k8sObj.Annotations) == 0 {
		n.k8sObj.Annotations = make(map[string]string)
	}
	// override max eni num from annotation
	if maxENIStr, ok := n.k8sObj.Annotations[k8s.AnnotationNodeMaxENINum]; ok {
		maxENI, err := strconv.Atoi(maxENIStr)
		if err != nil {
			scopedLog.Errorf("parse max eni num [%s] from annotation failed: %v", maxENIStr, err)
		} else if maxENI < eniQuota.GetMaxENI() {
			scopedLog.Warnf("max eni num from annotation is smaller than real: %d < %d", maxENI, eniQuota.GetMaxENI())
		} else if maxENI >= 0 {
			eniQuota.SetMaxENI(maxENI)
			scopedLog.Infof("override max eni num from annotation: %d", maxENI)
		}
	}

	// override max ip num from annotation
	if maxIPStr, ok := n.k8sObj.Annotations[k8s.AnnotationNodeMaxPerENIIPsNum]; ok {
		maxIP, err := strconv.Atoi(maxIPStr)
		if err != nil {
			scopedLog.Errorf("parse max ip num [%s] from annotation failed: %v", maxIPStr, err)
		} else if maxIP >= 0 {
			eniQuota.SetMaxIP(maxIP)
		}
	}

	// if eniQuota is empty, it means the node has not set quota
	// it should be retry later
	if eniQuota.GetMaxENI() == 0 && eniQuota.GetMaxIP() == 0 {
		return nil
	}
	// 3. Override the CCE Node capacity to the CCE NetworkResourceSet object
	err = n.overrideENICapacityToNetworkResourceSet(eniQuota)
	if err != nil {
		scopedLog.Errorf("override ENI capacity to CCE NetworkResourceSet failed: %v", err)
		return nil
	}

	n.limiterLock.Lock()
	n.eniQuota = eniQuota
	n.lastResyncEniQuotaTime = time.Now()
	n.limiterLock.Unlock()

	scopedLog.WithFields(logrus.Fields{
		"maxENI":      n.eniQuota.GetMaxENI(),
		"maxIPPerENI": n.eniQuota.GetMaxIP(),
	}).Info("override ENI capacity to NetResourceSet success")
	return n.eniQuota
}

// overrideENICapacityToNetworkResourceSet accoding to the Node.limiter field, override the
// capacity of ENI to the Node.k8sObj.Spec
func (n *bceNetworkResourceSet) overrideENICapacityToNetworkResourceSet(eniQuota ENIQuotaManager) error {
	// update the node until 30s timeout
	// if update operation return error, we will get the leatest version of node and try again
	err := wait.PollImmediate(200*time.Millisecond, 30*time.Second, func() (bool, error) {
		old, err := n.manager.nrsGetterUpdater.Get(n.k8sObj.Name)
		if err != nil {
			return false, err
		}
		k8sObj := old.DeepCopy()

		if len(n.k8sObj.Annotations) == 0 {
			n.k8sObj.Annotations = make(map[string]string)
		}
		if len(n.securityGroupIDs) > 0 {
			n.k8sObj.Annotations[k8s.AnnotationUseSecurityGroupIDs] = logfields.Json(n.securityGroupIDs)
		}
		if len(n.enterpriseSecurityGroupIDs) > 0 {
			n.k8sObj.Annotations[k8s.AnnotationUseEnterpriseSecurityGroupIDs] = logfields.Json(n.enterpriseSecurityGroupIDs)
		}

		k8sObj.Spec.ENI.MaxAllocateENI = eniQuota.GetMaxENI()
		k8sObj.Spec.ENI.MaxIPsPerENI = eniQuota.GetMaxIP()

		// update ippool capacity
		if k8sObj.Spec.ENI.BurstableMehrfachENI > 0 {
			k8sObj.Spec.IPAM.MinAllocate = math.IntMin(eniQuota.GetMaxIP()-1, 10)
			k8sObj.Spec.IPAM.PreAllocate = math.IntMin(eniQuota.GetMaxIP()-1, k8sObj.Spec.IPAM.PreAllocate)
			k8sObj.Spec.IPAM.MaxAboveWatermark = eniQuota.GetMaxIP() - 1
		}
		if k8sObj.Annotations == nil {
			k8sObj.Annotations = map[string]string{}
		}

		now := time.Now().Format(time.RFC3339)
		k8sObj.Annotations[k8s.AnnotationIPResourceCapacitySynced] = now
		updated, err := n.manager.nrsGetterUpdater.Update(old, k8sObj)
		if err == nil {
			n.k8sObj = updated
			return true, nil
		}
		n.log.Errorf("failed to override nrs %s capacity: %v", n.k8sObj.Name, err)
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}

// UpdatedNode is called when an update to the NetResourceSet is received.
func (n *bceNetworkResourceSet) UpdatedNode(obj *ccev2.NetResourceSet) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.k8sObj = obj
	n.instanceID = obj.Spec.InstanceID

	eniQuota := n.getENIQuota()
	if eniQuota != nil && obj.Spec.ENI.BurstableMehrfachENI > 0 {
		eniCapacity := n.GetMaximumBurstableAllocatableIPv4()
		if obj.Spec.IPAM.PreAllocate == 0 {
			obj.Spec.IPAM.PreAllocate = math.IntMax(obj.Spec.IPAM.PreAllocate, eniCapacity/2)
		}
		if obj.Spec.IPAM.PreAllocate > 10 {
			obj.Spec.IPAM.MinAllocate = 10
		} else {
			obj.Spec.IPAM.MinAllocate = n.GetMinimumAllocatableIPv4()
		}
		obj.Spec.IPAM.MaxAboveWatermark = math.IntMax(eniCapacity*obj.Spec.ENI.BurstableMehrfachENI, obj.Spec.IPAM.MaxAboveWatermark)
	}
}

// ResyncInterfacesAndIPs is called to synchronize the latest list of
// interfaces and IPs associated with the node. This function is called
// sparingly as this information is kept in sync based on the success
// of the functions AllocateIPs(), ReleaseIPs() and CreateInterface().
func (n *bceNetworkResourceSet) ResyncInterfacesAndIPs(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
	var (
		a        = ipamTypes.AllocationMap{}
		allENIId []string
	)

	n.waitForENISynced(ctx)

	availableENIsNumber := 0
	availableIPsNumber := 0
	usedENIsNumber := 0
	haveCreatingENI := false

	// effectiveInstanceID will changed in ForeachInstanceWithAllEni
	n.manager.ForeachInstanceWithAllEni(n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}

			allENIId = append(allENIId, e.Name)

			if e.Status.VPCStatus != ccev2.VPCENIStatusInuse &&
				(e.Spec.Type == ccev2.ENIForBBC || e.Spec.UseMode == ccev2.ENIUseModePrimaryWithSecondaryIP) {

				err := n.forceUpdateEniToInuse(ctx, scopedLog, e.Name)
				if err != nil {
					scopedLog.WithError(err).WithField("eniName", e.Name).Error("Failed to force update ENI to inuse")
					return err
				}
			}

			if e.Status.VPCStatus != ccev2.VPCENIStatusInuse || e.Status.CCEStatus == ccev2.ENIMSGPCIERROR || e.Status.CCEStatus == "" {
				// *** The if condition here is based on two considerations ***
				// 1. There are network interfaces currently in the process of transitioning to the ready state,
				//    including those that are available, attaching, or in use but not yet ready on the node.
				//    In this case, there is no need to create a new network interface.
				// 2. The current node has network interfaces in an error state, or they lack either a MAC address
				//    or an inumber. In this situation, we believe that continuing to create network interfaces
				//    would still pose issues, so we halt further creation on this node.

				// In version 2.12.18, we believe that if there is a non-inuse ENI or an ENI problem caused by PCI,
				// or the ENI is on the way to be ready, you should not create a new ENI to stop loss.
				// In other cases, you can create an ENI
				haveCreatingENI = true
				n.creatingEni.addCreatingENI(e.Name, e.CreationTimestamp.Time)
				scopedLog.Logger.Infof("ENI %s is not ready to use, CCEStatus is %s ",
					e.Name, e.Status.CCEStatus)
				return nil
			} else if e.Status.CCEStatus != ccev2.ENIStatusReadyOnNode {
				scopedLog.Logger.Infof("ENI %s is not ready to use, but will not forbid to create new ENI, CCEStatus is %s ",
					e.Name, e.Status.CCEStatus)
				return nil
			}

			availableENIsNumber++
			if e.Status.CCEStatus == ccev2.ENIStatusUsingInPod {
				usedENIsNumber++
			}

			// The node currently being created by ENI can be viewed in the informer.
			// It can be deleted from the cache
			n.creatingEni.removeCreatingENI(e.Name)

			addAllocationIP := func(ips []*models.PrivateIP, isIPv6 bool) {
				for _, addr := range ips {
					// Skip IPv4 primary IP in secondary mode — it's the ENI management address.
					// IPv6 primary IP is just a tag, it can be assigned to pods, so let it into pool.
					if !isIPv6 && e.Spec.UseMode != ccev2.ENIUseModePrimaryIP && addr.Primary {
						continue
					}
					if !isIPv6 {
						availableIPsNumber++
					}

					a[addr.PrivateIPAddress] = ipamTypes.AllocationIP{
						Resource: e.Name,
						SubnetID: addr.SubnetID,
					}
				}
			}

			addAllocationIP(e.Spec.ENI.PrivateIPSet, false)
			addAllocationIP(e.Spec.ENI.IPV6PrivateIPSet, true)

			return nil
		})

	n.creatingEni.cleanExpiredCreatingENI(allENIId, operatorOption.Config.ResourceResyncInterval*3)

	// the 	CreateInterface function will not be called in the primary IP mode
	// so we need to create a new ENI in the primary IP mode at this time
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) &&
		availableENIsNumber == usedENIsNumber && !haveCreatingENI {
		go func() {
			_, _, err := n.CreateInterface(ctx, nil, scopedLog)
			scopedLog.WithError(err).Debugf("try to create new ENI for primary IP mode")
		}()
	}

	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		availableIPsNumber = 0
	} else {
		availableENIsNumber = 0
	}

	eniQuota := n.getENIQuota()
	if eniQuota == nil {
		scopedLog.Error("ENI quota is nil, returning error")
		return a, goerrors.New(errUnableToGetEniQuota)
	}

	err := eniQuota.RefreshEniCapacityToK8s(ctx, availableENIsNumber, availableIPsNumber)
	if err != nil {
		scopedLog.WithError(err).Errorf("refresh eni capacity to k8s node error")
	}

	n.mutex.Lock()
	n.availableIPsNumber = availableIPsNumber
	n.mutex.Unlock()

	// clean borrowed subnet ip
	maxAllocatableIPv4 := n.GetMaximumAllocatableIPv4()
	if availableIPsNumber >= maxAllocatableIPv4 {
		foreachInstanceReadyOnNode(n.manager, n.instanceID, n.k8sObj.Name,
			func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
				e, ok := iface.Resource.(*eniResource)
				if !ok {
					return nil
				}
				if subnet, err := bcesync.GlobalBSM().GetSubnet(e.Spec.ENI.SubnetID); err == nil {
					borrowedIPNum := subnet.GetBorrowedIPNum(interfaceID)
					if borrowedIPNum > 0 {
						subnet.Cancel(interfaceID)
					}
				}
				return nil
			})
	}

	return a, nil
}

func (n *bceNetworkResourceSet) getAvailableIPv4() int {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	return n.availableIPsNumber
}

func (n *bceNetworkResourceSet) forceUpdateEniToInuse(ctx context.Context, scopedLog *logrus.Entry, eniID string) error {
	eni, err := n.manager.enilister.Get(eniID)
	if err == nil {
		scopedLog = scopedLog.WithFields(logrus.Fields{
			"eniID":     eniID,
			"oldStatus": eni.Status.VPCStatus,
		})
		if eni.DeletionTimestamp == nil && len(eni.Finalizers) == 0 {
			eni.Finalizers = append(eni.Finalizers, bcesync.FinalizerENI)
		}
		eni, err = k8s.CCEClient().CceV2().ENIs().Update(ctx, eni, metav1.UpdateOptions{})
		if err != nil {
			scopedLog.WithError(err).Error("failed to update primary ENI finalizers")
			return fmt.Errorf("failed to update primary ENI finalizers: %w", err)
		}
		(&eni.Status).AppendVPCStatus(ccev2.VPCENIStatusInuse)
		_, err = k8s.CCEClient().CceV2().ENIs().UpdateStatus(ctx, eni, metav1.UpdateOptions{})
		if err != nil {
			scopedLog.WithError(err).Error("failed to update primary ENI status")
			return fmt.Errorf("failed to update primary ENI status: %w", err)
		}
		scopedLog.Infof("update primary ENI status to inuse successed")
	}
	return nil
}

func (n *bceNetworkResourceSet) forceGetENIFromInstanceGroup(ctx context.Context, scopedLog *logrus.Entry, vpcID, instanceID string) (interfaceNum int, msg string, err error) {
	startTime := time.Now()
	scopedLog = scopedLog.WithField("NetworkResourceSet", n.k8sObj.Name)
	scopedLog.WithField("StartTime", startTime).Infof("Start force get ENI from instance-group")
	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to get effective instanceID")
		return 0, "", err
	}

	selector, err := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelInstanceID:           effectiveInstanceID,
		"cce.baidubce.com/created-by": "instance-controller",
	}))
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to construct selector for eni from instance-group")
		return 0, "", err
	}
	var (
		eniList []*ccev2.ENI
		listErr error
	)
	if err = wait.PollImmediateWithContext(ctx, eniSyncPeriod, maxENISyncDuration, func(context.Context) (done bool, err error) {
		eniListTemp, listErrTemp := n.manager.enilister.List(selector)
		if listErrTemp != nil {
			scopedLog.Warningf("failed to get eni from instance-group: %v, retry after %s", listErr, eniSyncPeriod)
			return false, nil
		}
		for _, eni := range eniListTemp {
			if _, ok := eni.Labels[k8s.LabelNodeName]; ok {
				continue
			}
			eniList = append(eniList, eni)
		}
		return true, nil
	}); err != nil {
		scopedLog.WithError(err).Errorf("failed to get eni from instance-group")
		return 0, "", err
	}
	scopedLog.WithField("CostTime", time.Since(startTime)).Infof("Force get ENI from instance-group, eniList length: %d", len(eniList))

	if len(eniList) == 0 {
		return 0, "", fmt.Errorf("no eni found from instance-group")
	}
	createExternalENINum := 0
	for _, eni := range eniList {
		newENI := eni.DeepCopy()
		newENI.ObjectMeta.Labels[k8s.LabelInstanceID] = effectiveInstanceID
		newENI.ObjectMeta.Labels[k8s.LabelNodeName] = n.k8sObj.Name
		newENI.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: ccev2.SchemeGroupVersion.String(),
			Kind:       ccev2.NRSKindDefinition,
			Name:       n.k8sObj.Name,
			UID:        n.k8sObj.UID,
		}}
		newENI.ObjectMeta.Name = eni.Spec.ENI.ID
		newENI.Spec.UseMode = ccev2.ENIUseMode(n.k8sObj.Spec.ENI.UseMode)
		newENI.Spec.NodeName = n.k8sObj.Name
		newENI.Spec.RouteTableOffset = n.k8sObj.Spec.ENI.RouteTableOffset
		newENI.Spec.InstallSourceBasedRouting = n.k8sObj.Spec.ENI.InstallSourceBasedRouting
		newENI.Spec.Type = ccev2.ENIType(n.k8sObj.Spec.ENI.InstanceType)

		// bcesync.ElectENIIPv6PrimaryIP(newENI)
		_, err = k8s.CCEClient().CceV2().ENIs().Update(ctx, newENI, metav1.UpdateOptions{})
		if err != nil {
			scopedLog.WithError(err).Errorf("failed to create ENI from instance-group : %v", err)
			return createExternalENINum, "partial create external ENI from instance-group to k8s error", fmt.Errorf("failed to create ENI: %w", err)
		}
		createExternalENINum++

		n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeNormal, "CreateENISuccess", "create external ENI %s from instance-group on NRS %s success", newENI.Name, n.k8sObj.Name)
		scopedLog = scopedLog.WithField("ENIID", newENI.Name)
	}
	return createExternalENINum, "", nil
}

func (n *bceNetworkResourceSet) forceGetENIFromIaaS(ctx context.Context, scopedLog *logrus.Entry, vpcID, instanceID string) (interfaceNum int, msg string, err error) {
	startTime := time.Now()
	scopedLog = scopedLog.WithField("NetworkResourceSet", n.k8sObj.Name)
	scopedLog.WithField("StartTime", startTime).Infof("Start force get ENI from IaaS")

	// Get effective instance ID based on instance type
	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		scopedLog.WithError(err).Error("Failed to get effective instance ID")
		return 0, "", fmt.Errorf("failed to get effective instance ID: %w", err)
	}

	if effectiveInstanceID != instanceID {
		scopedLog.WithFields(logrus.Fields{
			"instanceType":        n.k8sObj.Spec.ENI.InstanceType,
			"originalInstanceID":  instanceID,
			"effectiveInstanceID": effectiveInstanceID,
		}).Info("Using HPASID for HPAS instance type ENI query")
	}

	args := enisdk.ListEniArgs{
		VpcId:      vpcID,
		InstanceId: effectiveInstanceID,
		Name:       fmt.Sprintf("%s/", option.Config.ClusterID),
	}

	// Call ListENIs directly without retry - rely on VPC API's built-in retry mechanism
	eniList, listErr := n.bceclient.ListENIs(ctx, args)
	if listErr != nil {
		scopedLog.WithError(listErr).Error("Failed to list ENIs from VPC")
		return 0, "", fmt.Errorf("failed to list ENIs: %w", listErr)
	}

	scopedLog.WithField("CostTime", time.Since(startTime)).Infof("Force get ENI from IaaS, eniList length: %d", len(eniList))

	if len(eniList) == 0 {
		scopedLog.Info("No ENI found in VPC")
		return 0, "no ENI found in VPC", nil
	}

	createExternalENINum := 0
	for _, eniObj := range eniList {
		newENI := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					k8s.LabelInstanceID: effectiveInstanceID,
					k8s.LabelNodeName:   n.k8sObj.Name,
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: ccev2.SchemeGroupVersion.String(),
					Kind:       ccev2.NRSKindDefinition,
					Name:       n.k8sObj.Name,
					UID:        n.k8sObj.UID,
				}},
				Name: eniObj.EniId,
			},
			Spec: ccev2.ENISpec{
				NodeName: n.k8sObj.Name,
				UseMode:  ccev2.ENIUseMode(n.k8sObj.Spec.ENI.UseMode),
				ENI: models.ENI{
					ID:                         eniObj.EniId,
					Name:                       eniObj.Name,
					MacAddress:                 eniObj.MacAddress,
					InstanceID:                 eniObj.InstanceId,
					SecurityGroupIds:           eniObj.SecurityGroupIds,
					EnterpriseSecurityGroupIds: eniObj.EnterpriseSecurityGroupIds,
					Description:                eniObj.Description,
					VpcID:                      eniObj.VpcId,
					ZoneName:                   eniObj.ZoneName,
					SubnetID:                   eniObj.SubnetId,
					PrivateIPSet:               bcesync.ToModelPrivateIP(eniObj.PrivateIpSet, eniObj.VpcId, eniObj.SubnetId),
					IPV6PrivateIPSet:           bcesync.ToModelPrivateIP(eniObj.Ipv6PrivateIpSet, eniObj.VpcId, eniObj.SubnetId),
				},
				RouteTableOffset:          n.k8sObj.Spec.ENI.RouteTableOffset,
				InstallSourceBasedRouting: n.k8sObj.Spec.ENI.InstallSourceBasedRouting,
				Type:                      ccev2.ENIType(n.k8sObj.Spec.ENI.InstanceType),
			},
		}
		bcesync.ElectENIIPv6PrimaryIP(newENI)
		_, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, newENI, metav1.CreateOptions{})
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return createExternalENINum, "partial create external ENI from IaaS to k8s error", fmt.Errorf("failed to create ENI: %w", err)
		}
		createExternalENINum++

		n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeNormal, "CreateENISuccess", "create external ENI %s from IaaS on NRS %s success", newENI.Name, n.k8sObj.Name)
		scopedLog = scopedLog.WithField("ENIID", newENI.Name)
	}

	return createExternalENINum, "", nil
}

// isInCooldownPeriod checks if we are currently in cooldown period
// Returns true if in cooldown, along with remaining time and last error
func (n *bceNetworkResourceSet) isInCooldownPeriod() (inCooldown bool, remaining time.Duration, lastErr error) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	if n.lastCreateInterfaceFailureTime == nil {
		return false, 0, nil
	}

	timeSinceFailure := time.Since(*n.lastCreateInterfaceFailureTime)
	if timeSinceFailure < createInterfaceCooldownPeriod {
		return true, createInterfaceCooldownPeriod - timeSinceFailure, n.lastCreateInterfaceFailureError
	}

	return false, 0, nil
}

// clearCooldown clears the cooldown state
// This should only be called after isInCooldownPeriod() returns false
func (n *bceNetworkResourceSet) clearCooldown(scopedLog *logrus.Entry) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if n.lastCreateInterfaceFailureTime != nil {
		scopedLog.WithField("lastFailureTime", n.lastCreateInterfaceFailureTime.Format(time.RFC3339)).Debug("Cooldown period expired, clearing failure state")
		n.lastCreateInterfaceFailureTime = nil
		n.lastCreateInterfaceFailureError = nil
	}
}

// enterCooldown enters cooldown period due to non-retryable error
func (n *bceNetworkResourceSet) enterCooldown(err error, msg string, scopedLog *logrus.Entry) {
	now := time.Now()
	n.mutex.Lock()
	n.lastCreateInterfaceFailureTime = &now
	n.lastCreateInterfaceFailureError = err
	n.mutex.Unlock()

	scopedLog.WithFields(logrus.Fields{
		"error":          err,
		"msg":            msg,
		"cooldownPeriod": createInterfaceCooldownPeriod,
	}).Warning("CreateInterface failed with non-retryable error, entering cooldown period")
}

// isNonRetryableError checks if the error is non-retryable (subnet IP exhaustion or no available subnet)
func isNonRetryableError(err error, msg string) bool {
	return cloud.IsErrorSubnetHasNoMoreIP(err) || msg == errNoAvailableSubnet
}

// CreateInterface create a new ENI
func (n *bceNetworkResourceSet) CreateInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	// Check if we are in cooldown period due to previous non-retryable failure
	if inCooldown, remaining, lastErr := n.isInCooldownPeriod(); inCooldown {
		scopedLog.WithFields(logrus.Fields{
			"cooldownRemaining": remaining,
			"lastError":         lastErr,
		}).Info("Skipping CreateInterface due to cooldown period after non-retryable error")
		return 0, "", fmt.Errorf("in cooldown period after non-retryable error (remaining: %v): %w", remaining, lastErr)
	}

	// Clear cooldown state since we passed the cooldown period
	n.clearCooldown(scopedLog)

	var (
		availableENICount int
		allENIId          []string
		eniQuota          = n.getENIQuota()
	)
	if eniQuota == nil {
		return 0, "", fmt.Errorf("eni quota is nil, please retry later")
	}

	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get effective instanceID: %w", err)
	}

	foreachInstanceReadyOnNode(n.manager, effectiveInstanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			eni, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			availableENICount++
			allENIId = append(allENIId, eni.Name)
			return nil
		})
	n.creatingEni.cleanExpiredCreatingENI(allENIId, operatorOption.Config.ResourceResyncInterval*3)
	if availableENICount == 0 {
		interfaceNum, msg, err = n.forceGetENIFromIaaS(ctx, scopedLog, n.k8sObj.Spec.ENI.VpcID, effectiveInstanceID)
		if err != nil {
			scopedLog.WithError(err).Warnf("force get ENI from IaaS failed")
		}
		if interfaceNum > 0 {
			return interfaceNum, msg, err
		}
	}

	if n.creatingEni.hasCreatingENI() {
		scopedLog.Debugf("skip to creating new eni, concurrent eni creating")
		return 0, "", nil
	}

	err = n.refreshAvailableSubnets()
	if err != nil {
		return 0, "refresh available subnet error", err
	}

	n.creatingEni.add(1)
	inums, msg, err := n.real.createInterface(ctx, allocation, scopedLog)
	n.creatingEni.add(-1)

	// Handle errors from the first createInterface call
	if err != nil {
		// Check if this is a non-retryable error
		if isNonRetryableError(err, msg) {
			// Enter cooldown period and return immediately
			n.enterCooldown(err, msg, scopedLog)
		}
		// For other errors, return the error but don't enter cooldown
		return 0, msg, err
	}

	// Success: proceed to create additional ENIs in background

	preAllocateENINum := math.IntMin(eniQuota.GetMaxENI(), n.k8sObj.Spec.ENI.PreAllocateENI)
	preAllocateENINum = preAllocateENINum - availableENICount - 1
	retryTimes := preAllocateENINum * 2

	for i := 0; i < preAllocateENINum; i++ {
		inums++
		go func() {
			retry := 0
			for retry < retryTimes {
				n.creatingEni.add(1)
				_, msg, e := n.real.createInterface(ctx, allocation, scopedLog)
				n.creatingEni.add(-1)
				if e == nil {
					scopedLog.Infof("create addition interface success")
					return
				}

				// Check if this is a non-retryable error
				if isNonRetryableError(e, msg) {
					n.enterCooldown(e, msg, scopedLog)
					return
				}

				scopedLog.WithError(e).Warnf("create addition interface failed, retry later for %ds(%d/%d)", retryDelay, retry+1, retryTimes)
				retry++
				// attaching ENI is need to wait serveral seconds (<15s) for the ENI to be attached,
				// so we can retry preAllocateENINum times for every retryDelay(15s) seconds later.
				time.Sleep(retryDelay)
			}
		}()

	}
	return inums, msg, err
}

// PopulateStatusFields is called to give the implementation a chance
// to populate any implementation specific fields in NetResourceSet.Status.
func (n *bceNetworkResourceSet) PopulateStatusFields(resource *ccev2.NetResourceSet) {
	eniStatusMap := make(map[string]ccev2.SimpleENIStatus)
	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		return
	}
	// Select only the ENI of the local node
	selector, err := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelInstanceID: effectiveInstanceID,
		k8s.LabelNodeName:   n.k8sObj.Name,
	}))
	if err != nil {
		panic(fmt.Errorf("failed to create label selector: %v", err))
	}
	enis, err := n.manager.enilister.List(selector)
	if err != nil {
		return
	}

	for i := 0; i < len(enis); i++ {
		// skip the eni that is not ready on node
		if enis[i].Status.CCEStatus != ccev2.ENIStatusReadyOnNode {
			continue
		}
		// add eni statistics
		simple := ccev2.SimpleENIStatus{
			ID:                        enis[i].Spec.ENI.ID,
			VPCStatus:                 string(enis[i].Status.VPCStatus),
			CCEStatus:                 string(enis[i].Status.CCEStatus),
			SubnetID:                  enis[i].Spec.SubnetID,
			IsMoreAvailableIPInSubnet: false,
		}

		// eni quota may be nil
		if eniQuota := n.getENIQuota(); eniQuota != nil {
			capacity := eniQuota.GetMaxIP()
			simple.AvailableIPNum = capacity - len(enis[i].Spec.PrivateIPSet)
		}

		simple.AllocatedIPNum = len(enis[i].Spec.PrivateIPSet)

		for j := range n.availableSubnets {
			if n.availableSubnets[j].Spec.ID == enis[i].Spec.ENI.SubnetID {
				simple.IsMoreAvailableIPInSubnet = !n.availableSubnets[j].Status.HasNoMoreIP
			}
		}
		eniStatusMap[enis[i].Name] = simple
	}
	resource.Status.ENIs = eniStatusMap

	// populate PSTSUsed: scan all IPs on all ENIs, find those used by PSTS pods
	pstsUsed := make(ipamTypes.AllocationMap)
	for i := 0; i < len(enis); i++ {
		if enis[i].Status.CCEStatus != ccev2.ENIStatusReadyOnNode {
			continue
		}
		addPSTSUsedIP := func(addr *models.PrivateIP) {
			if addr.Primary {
				return
			}
			ceps, _ := n.manager.cepClient.GetByIP(addr.PrivateIPAddress)
			if len(ceps) == 0 {
				return
			}
			cep := ceps[0]
			if cep.Spec.Network.IPAllocation == nil || cep.Spec.Network.IPAllocation.PSTSName == "" {
				return
			}
			pstsUsed[addr.PrivateIPAddress] = ipamTypes.AllocationIP{
				Resource: enis[i].Name,
				SubnetID: addr.SubnetID,
				Owner:    cep.Namespace + "/" + cep.Name,
			}
		}
		for _, addr := range enis[i].Spec.PrivateIPSet {
			addPSTSUsedIP(addr)
		}
		for _, addr := range enis[i].Spec.IPV6PrivateIPSet {
			addPSTSUsedIP(addr)
		}
	}
	if len(pstsUsed) > 0 {
		resource.Status.IPAM.PSTSUsed = pstsUsed
	} else {
		resource.Status.IPAM.PSTSUsed = nil
	}

	// update available subnet ids
	var availableSubnetIDs []string
	for i := range n.availableSubnets {
		availableSubnetIDs = append(availableSubnetIDs, n.availableSubnets[i].Spec.ID)
	}
	resource.Status.IPAM.AvailableSubnetIDs = availableSubnetIDs
	n.refreshENIAllocatedIPErrors(resource)
}

// PrepareIPAllocation is called to calculate the number of IPs that
// can be allocated on the node and whether a new network interface
// must be attached to the node.
func (n *bceNetworkResourceSet) PrepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	if quota := n.getENIQuota(); quota == nil {
		return nil, fmt.Errorf("eniQuota is nil, please retry later")
	}
	return n.real.prepareIPAllocation(scopedLog)
}

func (n *bceNetworkResourceSet) enableNodeAnnotationSubnet() bool {
	if !operatorOption.Config.EnableNodeAnnotationSync {
		return false
	}
	// wait for node annotation sync
	_, instanceOk := n.k8sObj.Annotations[k8s.AnnotationCCEInstanceLabel]
	value, ok := n.k8sObj.Annotations[k8s.AnnotationNodeAnnotationSynced]
	annotationSynced := ok && value == k8s.ValueStringTrue
	if len(n.k8sObj.Annotations) == 0 ||
		(!annotationSynced && !instanceOk) {
		return false
	}

	_, ok = n.k8sObj.Annotations[k8s.AnnotationNodeEniSubnetIDs]
	return ok
}

func (n *bceNetworkResourceSet) refreshAvailableSubnets() error {
	if operatorOption.Config.EnableNodeAnnotationSync {
		// wait for node annotation sync
		_, instanceOk := n.k8sObj.Annotations[k8s.AnnotationCCEInstanceLabel]
		value, ok := n.k8sObj.Annotations[k8s.AnnotationNodeAnnotationSynced]
		annotationSynced := ok && value == k8s.ValueStringTrue
		if len(n.k8sObj.Annotations) == 0 ||
			(!annotationSynced && !instanceOk) {
			return fmt.Errorf("node annotation sync is not ready, please wait for a moment")
		}

		var sbnIDs []string
		if annoSubnetIDs, ok := n.k8sObj.Annotations[k8s.AnnotationNodeEniSubnetIDs]; ok {
			idsArr := strings.Split(annoSubnetIDs, ",")
			for _, id := range idsArr {
				sbnID := strings.TrimSpace(id)
				if id == "" {
					continue
				}
				sbnIDs = append(sbnIDs, sbnID)
			}

			// update nrs subnet ids
			err := n.updateNrsSubnetIfNeed(sbnIDs)
			if err != nil {
				return err
			}
		}
	}
	// Ensure all configured subnets
	allSubnets := n.ensureConfiguredSubnets()

	// Filter subnets with detailed error information
	subnets, filterErr := nodeutils.FilterAvailableSubnet(
		allSubnets,
		n.GetMinimumAllocatableIPv4(),
		operatorOption.Config.BCECloudContry,
		operatorOption.Config.BCECloudRegion,
		n.k8sObj.Spec.ENI.AvailabilityZone,
	)

	if len(subnets) == 0 {
		errorCode, reason, errStr := nodeutils.CategorizeSubnetError(filterErr, n.k8sObj.Spec.ENI.SubnetIDs, n.k8sObj.Name)
		n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeWarning, errorCode, errStr)
		metrics.IPAMErrorCounter.WithLabelValues(errorCode, "NetResourceSet", n.k8sObj.Name).Inc()
		watchers.SetNodeCondition(n.k8sObj.Name, watchers.CCESubnetUnavailable, corev1.ConditionTrue, reason, errStr, n.log)
		return goerrors.New(errStr)
	}

	// Set node condition to False when subnets are available
	subnetIDs := nodeutils.ExtractSubnetIDs(subnets)
	successMsg := fmt.Sprintf("subnets [%v] are available for node %s, total %d subnets ready", subnetIDs, n.k8sObj.Name, len(subnets))
	watchers.SetNodeCondition(n.k8sObj.Name, watchers.CCESubnetUnavailable, corev1.ConditionFalse, watchers.ReasonSubnetsAvailable, successMsg, n.log)

	n.availableSubnets = subnets
	return nil
}

func (n *bceNetworkResourceSet) updateNrsSubnetIfNeed(sbnIDs []string) error {
	if !reflect.DeepEqual(n.k8sObj.Spec.ENI.SubnetIDs, sbnIDs) {
		err := wait.PollImmediate(200*time.Millisecond, 30*time.Second, func() (bool, error) {
			old, err := n.manager.nrsGetterUpdater.Get(n.k8sObj.Name)
			if err != nil {
				return false, err
			}
			k8sObj := old.DeepCopy()
			k8sObj.Spec.ENI.SubnetIDs = sbnIDs
			updated, err := n.manager.nrsGetterUpdater.Update(k8sObj, k8sObj)
			if err == nil && updated != nil {
				n.k8sObj = updated
				return true, nil
			}
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("update nrs subnet ids failed: %w", err)
		}
	}
	return nil
}

// AllocateIPs is called after invoking PrepareIPAllocation and needs
// to perform the actual allocation.
func (n *bceNetworkResourceSet) AllocateIPs(ctx context.Context, allocation *ipam.AllocationAction) error {
	eniQuota := n.getENIQuota()
	if eniQuota == nil {
		return fmt.Errorf("eniQuota is nil, please check the node status")
	}

	// if instance type is BCC, we need to allocate ENI secondary ip from subnet
	// subnet id and ENI id is in allocation.PoolID and allocation.InterfaceID that
	//  we use them to allocate ip through the bceclient of instance manager
	if bceutils.IsSecondaryIPMode(n.k8sObj.Spec.ENI.UseMode) {
		scopedLog := n.log.WithFields(logrus.Fields{
			"eniID": allocation.InterfaceID,
			"pool":  allocation.PoolID,
		})

		// get eni
		eni, err := n.manager.enilister.Get(string(allocation.InterfaceID))
		if err != nil {
			return fmt.Errorf("get eni [%s] failed: %v", allocation.InterfaceID, err)
		}
		eni = eni.DeepCopy()

		var (
			ipv4ToAllocate = 0
			ipv6ToAllocate = 0

			ipv4PaticalToAllocate = 0
			ipv6PaticalToAllocate = 0
			isPartialSuccess      = false

			ipv4AllocatedSet = []*models.PrivateIP{}
			ipv6AllocatedSet = []*models.PrivateIP{}
		)

		if operatorOption.Config.EnableIPv4 {
			ipv4ToAllocate = math.IntMin(allocation.AvailableForAllocationIPv4, eniQuota.GetMaxIP()-len(eni.Spec.ENI.PrivateIPSet))
		}
		if operatorOption.Config.EnableIPv6 {
			// ipv6 address should not be more than ipv4 address
			ipv6ToAllocate = math.IntMin(allocation.AvailableForAllocationIPv6, eniQuota.GetMaxIP()-len(eni.Spec.ENI.IPV6PrivateIPSet))
		}
		// ensures that the difference set is supplemented completely
		if operatorOption.Config.EnableIPv6 && operatorOption.Config.EnableIPv4 {
			ipv6Diff := len(eni.Spec.IPV6PrivateIPSet) - len(eni.Spec.PrivateIPSet)
			if ipv6Diff > 0 {
				ipv4ToAllocate = math.IntMin(ipv4ToAllocate, ipv6Diff)
				ipv6ToAllocate = 0
			} else if ipv6Diff < 0 {
				ipv6ToAllocate = math.IntMin(ipv6ToAllocate, -ipv6Diff)
				ipv4ToAllocate = 0
			}
		}

		for ipv4ToAllocate > 0 || ipv6ToAllocate > 0 {
			ipv4PaticalToAllocate = math.IntMin(ipv4ToAllocate, bceconstants.BatchAddOrDeleteIPLimit)
			ipv6PaticalToAllocate = math.IntMin(ipv6ToAllocate, bceconstants.BatchAddOrDeleteIPLimit)
			allocateLog := scopedLog.WithFields(logrus.Fields{
				"ipv4ToAllocate":        ipv4ToAllocate,
				"ipv6ToAllocate":        ipv6ToAllocate,
				"ipv4PaticalToAllocate": ipv4PaticalToAllocate,
				"ipv6PaticalToAllocate": ipv6PaticalToAllocate,
			})
			// allocate ip from real eni implementation
			partialIPv4Result, partialIPv6Result, err := n.real.allocateIPs(ctx, scopedLog, allocation, ipv4PaticalToAllocate, ipv6PaticalToAllocate)
			if err != nil {
				// append error to eni status
				n.appendAllocatedIPError(allocation.InterfaceID,
					ccev2.NewErrorStatusChange(
						fmt.Sprintf("allocate ips from eni [%s] failed: %v", allocation.InterfaceID, err)))

				allocateLog.WithError(err).Error("failed to allocate ips from eni")
				if !isPartialSuccess {
					return err
				} else {
					// if partial success, we will continue to allocate
					// DO NOT EDIT HERE！Because the actual allocation is indeed not this much, but it can guarantee exiting the loop.
					ipv4ToAllocate -= ipv4PaticalToAllocate
					ipv6ToAllocate -= ipv6PaticalToAllocate
					continue
				}
			} else {
				allocateLog.Info("allocate ips from eni success")
				ipv4ToAllocate -= ipv4PaticalToAllocate
				ipv6ToAllocate -= ipv6PaticalToAllocate
				isPartialSuccess = true
			}

			// update borrowed subnet
			borrowedSubnet, err := bcesync.GlobalBSM().GetSubnet(string(allocation.PoolID))
			if err != nil {
				scopedLog.WithField("subnetID", string(allocation.PoolID)).Warnf("get borrowed subnet failed: %v", err)
			} else {
				borrowedSubnet.Done(allocation.InterfaceID, len(partialIPv4Result))
			}

			ipv4AllocatedSet = appenUniqueElement(ipv4AllocatedSet, partialIPv4Result)
			ipv6AllocatedSet = appenUniqueElement(ipv6AllocatedSet, partialIPv6Result)
		}

		if len(ipv4AllocatedSet) == 0 && len(ipv6AllocatedSet) == 0 {
			return fmt.Errorf("no ip has been allocated on eni [%s]", allocation.InterfaceID)
		}
		scopedLog.WithFields(logrus.Fields{
			"ipv4AllocatedSet": ipv4AllocatedSet,
			"ipv6AllocatedSet": ipv6AllocatedSet,
		}).Infof("allocated ips from eni success")

		return n.updateENIWithPoll(ctx, eni, func(eni *ccev2.ENI) *ccev2.ENI {
			eni.Spec.ENI.PrivateIPSet = appenUniqueElement(eni.Spec.PrivateIPSet, ipv4AllocatedSet)
			eni.Spec.ENI.IPV6PrivateIPSet = appenUniqueElement(eni.Spec.IPV6PrivateIPSet, ipv6AllocatedSet)

			// set primary ip for IPv6 if not set
			bcesync.ElectENIIPv6PrimaryIP(eni)
			return eni
		})
	}
	return nil
}

// appenUniqueElement appends the elements in toadd to arr if they are not already in arr.
func appenUniqueElement(arr, toadd []*models.PrivateIP) []*models.PrivateIP {
	for _, addr := range toadd {
		exist := false
		for _, a := range arr {
			if a.PrivateIPAddress == addr.PrivateIPAddress {
				exist = true
				break
			}
		}
		if !exist {
			arr = append(arr, addr)
		}
	}
	return arr
}

// PrepareIPRelease is called to calculate whether any IP excess needs
// to be resolved. It behaves identical to PrepareIPAllocation but
// indicates a need to release IPs.
func (n *bceNetworkResourceSet) PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ipam.ReleaseAction {
	r := &ipam.ReleaseAction{}

	scopedLog.WithFields(logrus.Fields{
		"function":   "PrepareIPRelease",
		"excessIPs":  excessIPs,
		"instanceID": n.instanceID,
		"nodeName":   n.k8sObj.Name,
	}).Info("release: starting IP release preparation")

	// Needed for selecting the same ENI to release IPs from
	// when more than one ENI qualifies for release
	// effectiveInstanceID will changed in ForeachInstanceReadyOnNode
	foreachInstanceReadyOnNode(n.manager, n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			// if r.IPsToRelease is not empty, don't excute the following code
			if len(r.IPsToRelease) > 0 {
				scopedLog.WithFields(logrus.Fields{
					"interfaceID": interfaceID,
					"reason":      "already_selected_eni",
				}).Debug("release: skipping ENI, already selected one")
				return nil
			}

			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			if e.Spec.UseMode == ccev2.ENIUseModePrimaryIP {
				return nil
			}
			scopeENILog := scopedLog.WithFields(logrus.Fields{
				"eni":          e.Name,
				"eniID":        e.Spec.ENI.ID,
				"index":        e.Status.InterfaceIndex,
				"numAddresses": len(e.Spec.ENI.PrivateIPSet),
				"useMode":      e.Spec.UseMode,
			})
			scopeENILog.Debug("release: considering ENI for IP release")

			// Count free IP addresses on this ENI
			var (
				ipv4ToFree      []string
				ipv6ToFree      []string
				ipv4ToFreeCount = 0
				ipv6ToFreeCount = 0
				excessIPsCount  = excessIPs
			)

			filteFreeIPsOnENI := func(addr *models.PrivateIP) bool {
				if addr.Primary || addr.SubnetID != e.Spec.ENI.SubnetID {
					return false
				}
				ip := addr.PrivateIPAddress
				n.mutex.Lock()
				_, ipUsed := n.k8sObj.Status.IPAM.Used[ip]
				n.mutex.Unlock()
				// exclude primary IPs and cross subnet IPs
				return !ipUsed
			}

			// for ipv4
			for _, addr := range e.Spec.ENI.PrivateIPSet {
				if filteFreeIPsOnENI(addr) {
					ipv4ToFree = append(ipv4ToFree, addr.PrivateIPAddress)
				}
			}
			// for ipv6
			for _, addr := range e.Spec.ENI.IPV6PrivateIPSet {
				if filteFreeIPsOnENI(addr) {
					ipv6ToFree = append(ipv6ToFree, addr.PrivateIPAddress)
				}
			}

			// Align the total number of IPv4 and IPv6
			if operatorOption.Config.EnableIPv6 && operatorOption.Config.EnableIPv4 {
				ipv6Diff := len(e.Spec.ENI.IPV6PrivateIPSet) - len(e.Spec.ENI.PrivateIPSet)
				if ipv6Diff > 0 {
					ipv6ToFreeCount = ipv6Diff
					excessIPsCount = excessIPsCount - ipv6Diff
				} else if ipv6Diff < 0 {
					ipv4ToFreeCount = -ipv6Diff
					excessIPsCount = excessIPsCount - ipv4ToFreeCount
				}
				if excessIPsCount > 1 {
					if excessIPsCount%2 == 1 {
						excessIPsCount = excessIPsCount - 1
					}
					excessIPsCount = excessIPsCount / 2
				}

				ipv6ToFreeCount = math.IntMin(len(ipv6ToFree), ipv6ToFreeCount+excessIPsCount)
				ipv4ToFreeCount = math.IntMin(len(ipv4ToFree), ipv4ToFreeCount+excessIPsCount)
			} else if operatorOption.Config.EnableIPv6 && !operatorOption.Config.EnableIPv4 {
				// IPv6 only
				ipv6ToFreeCount = math.IntMin(len(ipv6ToFree), excessIPsCount)
			} else if !operatorOption.Config.EnableIPv6 && operatorOption.Config.EnableIPv4 {
				// IPv4 only
				ipv4ToFreeCount = math.IntMin(len(ipv4ToFree), excessIPsCount)
			}

			if ipv4ToFreeCount <= 0 && ipv6ToFreeCount <= 0 {
				return nil
			}

			scopeENILog.WithFields(logrus.Fields{
				"excessIPs":       excessIPs,
				"ipv4ToFreeCount": ipv4ToFreeCount,
				"ipv6ToFreeCount": ipv6ToFreeCount,
				"ipv4ToFree":      ipv4ToFree,
				"ipv6ToFree":      ipv6ToFree,
				"totalFreeIPs":    len(ipv4ToFree) + len(ipv6ToFree),
			}).Debug("release: ENI has unused IPs that can be released")

			// Select the ENI with the most addresses available for release
			if len(r.IPsToRelease) == 0 {
				r.InterfaceID = e.Spec.ENI.ID
				r.PoolID = ipamTypes.PoolID(e.Spec.ENI.SubnetID)
				if ipv4ToFreeCount > 0 && len(ipv4ToFree) > 0 {
					r.IPsToRelease = ipv4ToFree[:ipv4ToFreeCount]
				}
				if ipv6ToFreeCount > 0 && len(ipv6ToFree) > 0 {
					r.IPsToRelease = append(r.IPsToRelease, ipv6ToFree[:ipv6ToFreeCount]...)
				}

				scopeENILog.WithFields(logrus.Fields{
					"selectedENI":    e.Spec.ENI.ID,
					"selectedSubnet": e.Spec.ENI.SubnetID,
					"ipsToRelease":   r.IPsToRelease,
					"releaseCount":   len(r.IPsToRelease),
				}).Info("release: selected ENI for IP release")
			}
			return nil
		})

	scopedLog.WithFields(logrus.Fields{
		"function":         "PrepareIPRelease",
		"excessIPs":        excessIPs,
		"selectedENI":      r.InterfaceID,
		"selectedSubnet":   r.PoolID,
		"ipsToRelease":     r.IPsToRelease,
		"releaseCount":     len(r.IPsToRelease),
		"hasReleaseAction": len(r.IPsToRelease) > 0,
	}).Info("release: completed IP release preparation")

	return r
}

type ipToReleaseSet [][]string

func (set ipToReleaseSet) append(ip string, maxLen int) [][]string {
	if len(set) == 0 {
		return [][]string{{ip}}
	}
	if len(set[len(set)-1]) < maxLen {
		set[len(set)-1] = append(set[len(set)-1], ip)
		return set
	}
	return append(set, []string{ip})
}

// ReleaseIPs is called after invoking PrepareIPRelease and needs to
// perform the release of IPs with enhanced retry and verification logic.
func (n *bceNetworkResourceSet) ReleaseIPs(ctx context.Context, release *ipam.ReleaseAction) ([]string, error) {
	if release == nil || len(release.IPsToRelease) == 0 {
		return nil, nil
	}

	scopeLog := n.log.WithFields(logrus.Fields{
		"ips":   release.IPsToRelease,
		"eniID": release.InterfaceID,
	})
	scopeLog.Debug("start to releasing IPs with enhanced retry logic")

	// 批量释放IP，带重试逻辑
	successfullyReleasedIPs, err := n.releaseBatchIPsWithRetry(ctx, release, release.IPsToRelease, scopeLog)

	// 记录释放结果
	if len(successfullyReleasedIPs) > 0 {
		scopeLog.WithFields(logrus.Fields{
			"totalIPs":      len(release.IPsToRelease),
			"successfulIPs": len(successfullyReleasedIPs),
		}).Info("Batch IP release completed")
	}

	// 如果有部分失败，记录错误但不失败整个操作
	if len(successfullyReleasedIPs) < len(release.IPsToRelease) {
		failedCount := len(release.IPsToRelease) - len(successfullyReleasedIPs)
		scopeLog.WithFields(logrus.Fields{
			"totalIPs":      len(release.IPsToRelease),
			"successfulIPs": len(successfullyReleasedIPs),
			"failedIPs":     failedCount,
		}).Warning("Partial IP release completed")
	}

	// 更新ENI，移除成功释放的IP
	if len(successfullyReleasedIPs) > 0 {
		updateErr := n.updateENIWithReleasedIPs(ctx, release.InterfaceID, successfullyReleasedIPs, scopeLog)
		if updateErr != nil {
			scopeLog.WithError(updateErr).Error("Failed to update ENI after IP release")
			// 如果更新ENI失败，但IP已经从IaaS释放了，这是一个严重问题
			// 这里不认为IP已经成功释放，返回的success ReleaseIP为空，IP 仍然在 nrs 的待释放 status 中
			// 由于 IP 仍然处在待释放状态，后续IP会重试删除（非本轮）
			// 后续的删除操作在IAAS侧返回已经不存在视为成功，于是再次进入 update ENI 操作
			return nil, updateErr // eni update 失败也不认为释放成功
		}
	}

	return successfullyReleasedIPs, err
}

func (n *bceNetworkResourceSet) updateENIWithPoll(ctx context.Context, toUpdateEni *ccev2.ENI, refresh func(eni *ccev2.ENI) *ccev2.ENI) error {
	var oldversion int64

	if toUpdateEni == nil || toUpdateEni.Name == "" {
		return fmt.Errorf("eni is nil or eni.Name is empty")
	}

	err := wait.PollImmediate(retryDuration, retryTimeout, func() (bool, error) {
		// get eni
		// append ip to eni
		log.WithFields(logrus.Fields{
			"eniID": toUpdateEni.Name,
		}).Info("start to update eni spec")
		eni, ierr := n.manager.enilister.Get(string(toUpdateEni.Name))
		if ierr != nil {
			return false, fmt.Errorf("get eni %s failed: %v", toUpdateEni.Name, ierr)
		}
		eni = eni.DeepCopy()
		oldversion = eni.Spec.VPCVersion
		eni.Spec.VPCVersion = eni.Spec.VPCVersion + 1
		eni = refresh(eni)
		// update eni
		_, ierr = k8s.CCEClient().CceV2().ENIs().Update(ctx, eni, metav1.UpdateOptions{})
		// retry if conflict
		// if errors.IsConflict(ierr) || errors.IsResourceExpired(ierr) {
		// retry if error to update eni
		if ierr != nil {
			log.WithError(ierr).
				WithFields(logrus.Fields{
					"eniID":      toUpdateEni.Name,
					"instanceID": toUpdateEni.Spec.InstanceID,
					"node":       toUpdateEni.Spec.NodeName,
					"error":      ierr,
				}).Errorf("update eni spec failed, retrying...")
			return false, nil
		}
		// we should recorde log with eni attributes and ips if update eni success
		log.WithFields(logrus.Fields{
			"eniID":      eni.Name,
			"instanceID": eni.Spec.InstanceID,
			"node":       eni.Spec.NodeName,
			"eni":        logfields.Json(eni.Spec.ENI),
		}).Infof("update eni spec success")
		return true, ierr
	})
	if err != nil {
		return fmt.Errorf("update eni %s failed: %v", toUpdateEni.Name, err)
	}

	n.mutex.Lock()
	n.expiredVPCVersion[toUpdateEni.Name] = oldversion
	n.mutex.Unlock()
	return nil
}

// GetMaximumAllocatableIPv4 Impl
func (n *bceNetworkResourceSet) GetMaximumAllocatableIPv4() int {
	eniQuota := n.getENIQuota()
	if eniQuota == nil {
		return 0
	}

	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return 0
	}

	if eniQuota.GetMaxENI() == 0 {
		return eniQuota.GetMaxIP() - 1
	}
	max := eniQuota.GetMaxENI() * (eniQuota.GetMaxIP() - 1)
	if n.k8sObj.Spec.IPAM.MaxAllocate > 0 {
		return math.IntMin(n.k8sObj.Spec.IPAM.MaxAllocate, max)
	}
	return max
}

// GetMinimumAllocatableIPv4 impl
func (n *bceNetworkResourceSet) GetMinimumAllocatableIPv4() int {
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return 0
	}
	min := n.k8sObj.Spec.IPAM.MinAllocate
	if min == 0 {
		min = defaults.IPAMPreAllocation
	}
	return min
}

// IsPrefixDelegated impl
func (n *bceNetworkResourceSet) IsPrefixDelegated() bool {
	return false
}

func (n *bceNetworkResourceSet) GetMaximumBurstableAllocatableIPv4() int {
	eniQuota := n.getENIQuota()
	if eniQuota == nil {
		return 0
	}
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return 0
	}
	if n.k8sObj.Spec.ENI.BurstableMehrfachENI > 0 {
		return math.IntMin(eniQuota.GetMaxIP()-1, n.GetMaximumAllocatableIPv4())
	}
	return 0
}

// GetUsedIPWithPrefixes Impl
func (n *bceNetworkResourceSet) GetUsedIPWithPrefixes() int {
	return 0
}

// ensureConfiguredSubnets ensures all configured subnets and returns available ones
func (n *bceNetworkResourceSet) ensureConfiguredSubnets() []*bcesync.BorrowedSubnet {
	var allSubnets []*bcesync.BorrowedSubnet
	for _, subnetID := range n.k8sObj.Spec.ENI.SubnetIDs {
		sbn, err := bcesync.GlobalBSM().EnsureSubnet(n.k8sObj.Spec.ENI.VpcID, subnetID)
		if err != nil {
			continue
		}
		allSubnets = append(allSubnets, sbn)
	}
	return allSubnets
}

// FilterAvailableSubnetIds filter subnets by subnet ids
func (n *bceNetworkResourceSet) FilterAvailableSubnetIds(subnetIDs []string, minAllocateIPs int) []*bcesync.BorrowedSubnet {
	var filtedSubnets []*bcesync.BorrowedSubnet
	for _, subnetID := range subnetIDs {
		sbn, err := bcesync.GlobalBSM().EnsureSubnet(n.k8sObj.Spec.ENI.VpcID, subnetID)
		if err != nil {
			continue
		}
		filtedSubnets = append(filtedSubnets, sbn)
	}
	result, _ := nodeutils.FilterAvailableSubnet(
		filtedSubnets,
		minAllocateIPs,
		operatorOption.Config.BCECloudContry,
		operatorOption.Config.BCECloudRegion,
		n.k8sObj.Spec.ENI.AvailabilityZone,
	)
	return result
}

// searchMaxAvailableSubnet search subnet with max available ip
func searchMaxAvailableSubnet(subnets []*bcesync.BorrowedSubnet) *bcesync.BorrowedSubnet {
	var bestSubnet *bcesync.BorrowedSubnet
	for _, sbn := range subnets {
		// filter out ipv6 subnet if ipv6 is not enabled
		if operatorOption.Config.EnableIPv6 {
			if sbn.Spec.IPv6CIDR == "" {
				continue
			}
		}
		if bestSubnet == nil || bestSubnet.BorrowedAvailableIPsCount < sbn.BorrowedAvailableIPsCount {
			bestSubnet = sbn
		}
	}
	return bestSubnet
}

// AllocateIP implements endpoint.DirectEndpointOperation
func (n *bceNetworkResourceSet) AllocateIP(ctx context.Context, action *endpoint.DirectIPAction) error {
	var (
		ipset []*models.PrivateIP
		err   error

		// interfaceID is the ENI ID or bbc instance ID
		interfaceID string
	)

	if action.SubnetID == "" {
		return fmt.Errorf("subnet id is empty")
	}

	sbn, err := n.manager.sbnlister.Get(action.SubnetID)
	if err != nil {
		return fmt.Errorf("get subnet %s failed: %v", action.SubnetID, err)
	}

	if len(action.Addressing) == 0 {
		ipset, interfaceID, err = n.real.allocateIPCrossSubnet(ctx, action.SubnetID)
		if err != nil {
			return err
		}
	} else {
		// convert AddressPair to PrivateIP
		for _, addressPair := range action.Addressing {
			ipset = append(ipset, &models.PrivateIP{
				PrivateIPAddress: addressPair.IP,
				Primary:          false,
				SubnetID:         action.SubnetID,
			})
		}
		interfaceID, err = n.real.reuseIPs(ctx, ipset, action.Owner, action.ClientToken)
		if err != nil {
			return err
		}
	}

	var resultAddressPairList ccev2.AddressPairList
	// convert PrivateIP to AddressPair
	for _, privateIP := range ipset {
		addrePair, err := utils.ConvertPrivateIP2AddressPair(privateIP, sbn)
		if err != nil {
			n.log.WithError(err).Warnf("convert privateIP %s to AddressPair failed", privateIP.PrivateIPAddress)
			continue
		}
		addrePair.Interface = interfaceID
		resultAddressPairList = append(resultAddressPairList, addrePair)
	}

	action.Addressing = resultAddressPairList

	// add node affinities
	zone, err := api.TransZoneNameToAvailableZone(sbn.Spec.AvailabilityZone)
	if err != nil {
		return fmt.Errorf("trans zone name to available zone failed: %v", err)
	}
	action.NodeSelectorRequirement = append(action.NodeSelectorRequirement, corev1.NodeSelectorRequirement{
		Key:      k8s.TopologyKeyOfPod,
		Operator: corev1.NodeSelectorOpIn,
		Values:   []string{zone},
	})

	eni, err := n.manager.enilister.Get(interfaceID)
	if err != nil {
		return fmt.Errorf("get eni %s failed: %v", interfaceID, err)
	}

	updateErr := n.updateENIWithPoll(ctx, eni, func(eni *ccev2.ENI) *ccev2.ENI {
		for index := range ipset {
			address := ipset[index]
			if address.PrivateIPAddress == "" {
				continue
			}
			ip := net.ParseIP(address.PrivateIPAddress)
			if ip.To4() == nil {
				eni.Spec.ENI.IPV6PrivateIPSet = appenUniqueElement(eni.Spec.IPV6PrivateIPSet, []*models.PrivateIP{address})
			} else {
				eni.Spec.ENI.PrivateIPSet = appenUniqueElement(eni.Spec.PrivateIPSet, []*models.PrivateIP{address})
			}
		}
		return eni
	})
	return updateErr
}

// DeleteIP implements endpoint.DirectEndpointOperation
func (n *bceNetworkResourceSet) DeleteIP(ctx context.Context, allocation *endpoint.DirectIPAction) error {
	var action *ipam.ReleaseAction = &ipam.ReleaseAction{
		PoolID: ipamTypes.PoolID(allocation.SubnetID),
	}

	for _, addressPair := range allocation.Addressing {
		if addressPair.Interface != "" {
			action.InterfaceID = addressPair.Interface
		}
		action.IPsToRelease = append(action.IPsToRelease, addressPair.IP)
	}
	_, err := n.ReleaseIPs(ctx, action)
	return err
}

// appendAllocatedIPError append eni allocated ip error to bcenode
// those errors will be added to the status of nrs by 2 minutes
func (n *bceNetworkResourceSet) appendAllocatedIPError(eniID string, openAPIStatus *ccev2.StatusChange) {
	n.errorLock.Lock()
	defer n.errorLock.Unlock()

	n.eniAllocatedIPsErrorMap[eniID] = openAPIStatus

	// append status change to nrs event
	if openAPIStatus.Code != ccev2.ErrorCodeSuccess {
		n.eventRecorder.Eventf(
			n.k8sObj, corev1.EventTypeWarning, openAPIStatus.Code, openAPIStatus.Message)
	}
}

// refreshENIAllocatedIPErrors fill nrs status with eni allocated ip errors
// error are only valid if they occured within the last 10 minutes
func (n *bceNetworkResourceSet) refreshENIAllocatedIPErrors(resource *ccev2.NetResourceSet) {
	n.errorLock.Lock()
	defer n.errorLock.Unlock()

	now := time.Now()
	for eniID := range resource.Status.ENIs {
		eniStastic := resource.Status.ENIs[eniID]
		eniStastic.LastAllocatedIPError = nil

		if eniStatus, ok := n.eniAllocatedIPsErrorMap[eniID]; ok {
			if now.Sub(n.eniAllocatedIPsErrorMap[eniID].Time.Time) < 10*time.Minute {
				eniStastic.LastAllocatedIPError = eniStatus
			}
		}

		// Update the map with the modified struct
		resource.Status.ENIs[eniID] = eniStastic
	}

	for eniID := range n.eniAllocatedIPsErrorMap {
		if _, ok := resource.Status.ENIs[eniID]; !ok {
			delete(n.eniAllocatedIPsErrorMap, eniID)
		}
	}
}

func (n *bceNetworkResourceSet) tryBorrowIPs(newENI *ccev2.ENI) error {
	var borrowedIPs int

	if n.k8sObj.Spec.ENI.BurstableMehrfachENI > 0 {
		subnet, err := bcesync.GlobalBSM().EnsureSubnet(newENI.Spec.VpcID, newENI.Spec.SubnetID)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to get borrow subnet %s: %v", newENI.Spec.SubnetID, err)
			n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeWarning, "FailedBorrowSubnet", errMsg)
			// Set node condition for subnet ensure failure
			watchers.SetNodeCondition(n.k8sObj.Name, watchers.CCESubnetUnavailable, corev1.ConditionTrue, watchers.ReasonSubnetEnsureFailure, errMsg, n.log)
			return err
		}

		// Set node condition to False when subnet is successfully ensured
		ensureSuccessMsg := fmt.Sprintf("Successfully ensured subnet %s for ENI %s", newENI.Spec.SubnetID, newENI.Spec.ENI.ID)
		watchers.SetNodeCondition(n.k8sObj.Name, watchers.CCESubnetUnavailable, corev1.ConditionFalse, watchers.ReasonSubnetEnsured, ensureSuccessMsg, n.log)

		var (
			maxAllocateIPs = n.GetMaximumAllocatableIPv4() - n.getAvailableIPv4()
			eniQuota       = n.getENIQuota()
		)
		if eniQuota == nil {
			return goerrors.New(errUnableToGetEniQuota)
		}
		quotaIPs := eniQuota.GetMaxIP() - len(newENI.Spec.PrivateIPSet)
		toBorrowIps := quotaIPs

		if maxAllocateIPs < quotaIPs {
			toBorrowIps = maxAllocateIPs + 1
		}

		borrowedIPs = subnet.Borrow(newENI.Spec.ENI.ID, toBorrowIps)
		if borrowedIPs != toBorrowIps {
			subnet.Cancel(newENI.Name)
			errMsg := fmt.Sprintf("Failed to borrow ENI ips (%d/%d) for eni %s, please change subnet of %s instance", borrowedIPs, toBorrowIps, newENI.Spec.SubnetID, n.instanceType)
			n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeWarning, "FailedBorrowEniIPs", errMsg)
			metrics.IPAMErrorCounter.WithLabelValues(ccev2.ErrorCodeNoAvailableSubnetCreateENI, "Subnet", newENI.Spec.SubnetID).Inc()
			// Set node condition for ENI IP borrow failure
			watchers.SetNodeCondition(n.k8sObj.Name, watchers.CCESubnetUnavailable, corev1.ConditionTrue, watchers.ReasonSubnetInsufficientIPs, errMsg, n.log)
			return goerrors.New(errMsg)
		}
		newENI.Spec.BorrowIPCount = borrowedIPs

		// Set node condition to False when IP borrowing succeeds
		successMsg := fmt.Sprintf("Successfully borrowed %d IPs for ENI %s from subnet %s", borrowedIPs, newENI.Spec.ENI.ID, newENI.Spec.SubnetID)
		watchers.SetNodeCondition(n.k8sObj.Name, watchers.CCESubnetUnavailable, corev1.ConditionFalse, watchers.ReasonIPBorrowSucceeded, successMsg, n.log)
	}
	return nil
}

type fnReleaseIPsFromCLoud func(ctx context.Context, scopedLog *logrus.Entry, eniID string, toReleaseIPs []string) error

// rleaseOldIP release old ip if it is not used by other endpoint
// return true: ip is used by current endpoint, do not need to release
// return false: ip is used by other endpoint, need to release
func (n *bceNetworkResourceSet) releaseOldIP(ctx context.Context, scopedLog *logrus.Entry, ips []*models.PrivateIP, namespace string, name string, releaseIPsFromCLoud fnReleaseIPsFromCLoud) (isLocalIP bool, eniID string, err error) {
	for _, privateIP := range ips {
		ceps, err := n.manager.cepClient.GetByIP(privateIP.PrivateIPAddress)
		if err == nil && len(ceps) > 0 {
			for _, cep := range ceps {
				if cep.Namespace != namespace || cep.Name != name {
					scopedLog.Errorf("ip %s has been used by other endpoint %s/%s", privateIP.PrivateIPAddress, cep.Namespace, cep.Name)
					return false, "", fmt.Errorf("ip %s has been used by other endpoint %s/%s", privateIP.PrivateIPAddress, cep.Namespace, cep.Name)
				}
			}
		}
	}

	// Initialize return value
	isLocalIP = false

	// Get endpoint information for subsequent deletion operations
	cep, err := n.manager.cepClient.Lister().CCEEndpoints(namespace).Get(name)
	if err != nil {
		return false, "", fmt.Errorf("get endpoint %s/%s failed: %v", namespace, name, err)
	}

	if cep.Status.Networking == nil {
		return isLocalIP, "", nil
	}

	// Iterate through all IPs and handle ENIs directly
	for _, privateIP := range ips {
		enis, err := watchers.ENIClient.GetByIP(privateIP.PrivateIPAddress)
		if err != nil {
			return false, "", fmt.Errorf("get eni by ip %s failed: %v", privateIP.PrivateIPAddress, err)
		}

		if len(enis) == 0 {
			scopedLog.Debugf("ip %s is not allocated on any eni", privateIP.PrivateIPAddress)
			break
		}

		// Check if it's a local IP: exactly one ENI and belongs to current node
		if len(enis) == 1 && enis[0].Spec.NodeName == n.k8sObj.Name {
			isLocalIP = true
			scopedLog.Debugf("ip %s is local ip on eni %s", privateIP.PrivateIPAddress, enis[0].Name)
			return isLocalIP, enis[0].Name, nil
		}

		scopedLog.Infof("ip %s is allocated on eni %v, need to release from all eni", privateIP.PrivateIPAddress, enis)
		// If not local IP, need to release IP from all ENIs
		// Process local ENIs first, then remote ENIs
		var localENIs, remoteENIs []*ccev2.ENI
		for _, eni := range enis {
			if eni.Spec.NodeName == n.k8sObj.Name {
				localENIs = append(localENIs, eni)
			} else {
				remoteENIs = append(remoteENIs, eni)
			}
		}

		// Merge processing order: local ENIs first
		allENIs := append(localENIs, remoteENIs...)

		for _, eni := range allENIs {

			if eni.Spec.SubnetID == privateIP.SubnetID {
				return false, "", fmt.Errorf("ip %s has been used by other eni %s for cached ip", privateIP.PrivateIPAddress, eni.Name)
			}
			scopedLog.Debugf("start to release ip %s from eni %s", privateIP.PrivateIPAddress, eni.Name)

			// Release IP from cloud directly
			err = releaseIPsFromCLoud(ctx, scopedLog, eni.Name, []string{privateIP.PrivateIPAddress})
			if err != nil {
				return false, "", fmt.Errorf("release ip %s from eni %s failed: %v", privateIP.PrivateIPAddress, eni.Name, err)
			}
			scopedLog.Debugf("release ip %s from eni %s success", privateIP.PrivateIPAddress, eni.Name)

			// Immediately remove IP from ENI after successful release
			err = n.removeReleasedIPsFromENI(ctx, eni.Name, []string{privateIP.PrivateIPAddress})
			if err != nil {
				return false, "", fmt.Errorf("remove released ip %s from eni %s failed: %v", privateIP.PrivateIPAddress, eni.Name, err)
			}
			scopedLog.Debugf("successfully released and removed ip %s from eni %s", privateIP.PrivateIPAddress, eni.Name)
		}

		scopedLog.Infof("release ip %s from eni %v success", privateIP.PrivateIPAddress, enis)
	}

	return isLocalIP, "", nil
}

var _ endpoint.DirectEndpointOperation = &bceNetworkResourceSet{}

type creatingEniSet struct {
	mutex *lock.Mutex
	// The node currently being created by ENI should have only one.
	creatingENI     map[string]time.Time
	creatingENINums int
}

func newCreatingEniSet() *creatingEniSet {
	return &creatingEniSet{
		mutex:       &lock.Mutex{},
		creatingENI: map[string]time.Time{},
	}
}

func (c *creatingEniSet) addCreatingENI(eniID string, createTime time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.creatingENI[eniID] = createTime
}

func (c *creatingEniSet) removeCreatingENI(eniID string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.creatingENI, eniID)
}

// clean expired creating eni
// eniIDs: slice of eniIDs that are not being expired
// duration: the max time that a creating eni can be expired
func (c *creatingEniSet) cleanExpiredCreatingENI(eniIDs []string, duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	now := time.Now()

	exsitENIIDs := make(map[string]bool)
	for _, eniID := range eniIDs {
		exsitENIIDs[eniID] = true
	}
	for eniID := range c.creatingENI {
		if _, ok := exsitENIIDs[eniID]; !ok {
			if now.Sub(c.creatingENI[eniID]) > duration {
				delete(c.creatingENI, eniID)
			}
		}
	}
}

func (c *creatingEniSet) add(num int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.creatingENINums += num
}

func (c *creatingEniSet) hasCreatingENI() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return len(c.creatingENI) > 0 || c.creatingENINums > 0
}

// removeReleasedIPsFromENI removes the released IPs from the given ENI
// and updates the ENI resource in Kubernetes
func (n *bceNetworkResourceSet) removeReleasedIPsFromENI(ctx context.Context, interfaceID string, ipsReleased []string) error {
	eni, err := n.manager.enilister.Get(interfaceID)
	if err != nil {
		return fmt.Errorf("fail to get eni %s: %v", interfaceID, err)
	}

	if len(ipsReleased) == 0 {
		return nil
	}

	// Create a map for quick lookup of released IPs
	releaseIPMap := make(map[string]bool)
	for _, ipstr := range ipsReleased {
		releaseIPMap[ipstr] = true
	}

	// Update ENI by removing released IPs
	return n.updateENIWithPoll(ctx, eni, func(eni *ccev2.ENI) *ccev2.ENI {
		var (
			ipv4Addrs []*models.PrivateIP
			ipv6Addrs []*models.PrivateIP
		)
		// Filter out released IPv4 addresses
		for _, addr := range eni.Spec.ENI.PrivateIPSet {
			if _, ok := releaseIPMap[addr.PrivateIPAddress]; !ok {
				ipv4Addrs = append(ipv4Addrs, addr)
			}
		}
		// Filter out released IPv6 addresses
		for _, addr := range eni.Spec.ENI.IPV6PrivateIPSet {
			if _, ok := releaseIPMap[addr.PrivateIPAddress]; !ok {
				ipv6Addrs = append(ipv6Addrs, addr)
			}
		}
		eni.Spec.ENI.PrivateIPSet = ipv4Addrs
		eni.Spec.ENI.IPV6PrivateIPSet = ipv6Addrs
		return eni
	})
}

// releaseBatchIPsWithRetry attempts to release a batch of IPs with retry logic
// Note: Each batch can only release up to BatchAddOrDeleteIPLimit IPs at once due to API limitations
func (n *bceNetworkResourceSet) releaseBatchIPsWithRetry(ctx context.Context, release *ipam.ReleaseAction, ipsToRelease []string, scopedLog *logrus.Entry) ([]string, error) {
	var successfullyReleasedIPs []string
	remainingIPs := make([]string, len(ipsToRelease))
	copy(remainingIPs, ipsToRelease)

	batchLog := scopedLog.WithFields(logrus.Fields{
		"totalIPs":          len(ipsToRelease),
		"selectedInterface": release.InterfaceID,
	})

	// 获取ENI信息，如果ENI不存在则直接退出
	eni, err := n.manager.enilister.Get(string(release.InterfaceID))
	if err != nil {
		batchLog.WithError(err).Warn("Failed to get ENI, skipping IP release")
		return successfullyReleasedIPs, err
	}

	retryCount := 0
	// 持续重试批量释放，直到满足退出条件
	for len(remainingIPs) > 0 {
		batchLog.WithField("remainingIPs", len(remainingIPs)).Debug("Attempting batch IP release")

		// 取出当前批次要处理的IP（最多10个）
		currentBatchSize := len(remainingIPs)
		if currentBatchSize > bceconstants.BatchAddOrDeleteIPLimit {
			currentBatchSize = bceconstants.BatchAddOrDeleteIPLimit
		}
		currentBatch := remainingIPs[:currentBatchSize]

		batchLog.WithField("currentBatchSize", currentBatchSize).Debug("Processing current batch")

		// 尝试释放当前批次的IP
		err := n.attemptBatchRelease(ctx, release, currentBatch, eni, scopedLog)
		if err == nil {
			// 当前批次释放成功
			successfullyReleasedIPs = append(successfullyReleasedIPs, currentBatch...)
			remainingIPs = remainingIPs[currentBatchSize:]
			batchLog.WithField("releasedCount", currentBatchSize).Debug("Current batch released successfully")
			continue
		}

		// 检查是否为明确的错误
		if n.isDefinitiveError(err) {
			batchLog.WithError(err).Warning("Batch IP release failed with definitive error")
			return successfullyReleasedIPs, err
		}

		// 对于不明确的错误，通过IaaS验证当前批次中哪些IP已经实际释放
		verifiedReleasedIPs, stillPendingIPs := n.verifyReleasedIPs(ctx, release.InterfaceID, currentBatch, scopedLog)

		// 更新已释放的IP列表
		if len(verifiedReleasedIPs) > 0 {
			successfullyReleasedIPs = append(successfullyReleasedIPs, verifiedReleasedIPs...)
			batchLog.WithField("verifiedCount", len(verifiedReleasedIPs)).Info("IPs verified as released through IaaS check")
		}

		// 更新剩余待释放的IP列表
		// 将当前批次中仍然待释放的IP与后续批次合并
		newRemainingIPs := make([]string, 0, len(stillPendingIPs)+len(remainingIPs)-currentBatchSize)
		newRemainingIPs = append(newRemainingIPs, stillPendingIPs...)
		if currentBatchSize < len(remainingIPs) {
			newRemainingIPs = append(newRemainingIPs, remainingIPs[currentBatchSize:]...)
		}
		remainingIPs = newRemainingIPs

		if len(remainingIPs) == 0 {
			break
		}
		retryCount++
		// 等待重试延迟，每次重试延迟时间会指数增长
		if err := n.waitForRetry(ctx, retryCount, err, batchLog); err != nil {
			return successfullyReleasedIPs, err
		}
	}

	batchLog.WithFields(logrus.Fields{
		"totalReleased":  len(successfullyReleasedIPs),
		"totalRequested": len(ipsToRelease),
	}).Info("Batch IP release completed")

	return successfullyReleasedIPs, nil
}

// attemptBatchRelease 尝试批量释放IP (最多40个IP)
func (n *bceNetworkResourceSet) attemptBatchRelease(ctx context.Context, release *ipam.ReleaseAction, ipsToRelease []string, eni *ccev2.ENI, scopedLog *logrus.Entry) error {
	if len(ipsToRelease) > bceconstants.BatchAddOrDeleteIPLimit {
		return fmt.Errorf("batch size %d exceeds maximum limit of %d", len(ipsToRelease), bceconstants.BatchAddOrDeleteIPLimit)
	}

	// 分类IP为IPv4和IPv6
	var ipv4ToRelease, ipv6ToRelease []string
	for _, ipstr := range ipsToRelease {
		ip := net.ParseIP(ipstr)
		if ip == nil {
			continue
		}

		if ip.To4() == nil {
			// IPv6 - 检查是否在ENI中
			for _, privateAddress := range eni.Spec.IPV6PrivateIPSet {
				if privateAddress.PrivateIPAddress == ipstr {
					ipv6ToRelease = append(ipv6ToRelease, ipstr)
					break
				}
			}
		} else {
			// IPv4 - 检查是否在ENI中
			for _, privateAddress := range eni.Spec.PrivateIPSet {
				if privateAddress.PrivateIPAddress == ipstr {
					ipv4ToRelease = append(ipv4ToRelease, ipstr)
					break
				}
			}
		}
	}

	// 释放IPv4地址
	if len(ipv4ToRelease) > 0 {
		err := n.real.releaseIPs(ctx, release, ipv4ToRelease, []string{})
		if err != nil && !strings.Contains(err.Error(), "PrivateIpInvalid") {
			scopedLog.WithError(err).WithField("ipv4ToRelease", ipv4ToRelease).Warn("Failed to release IPv4 addresses")
			return err // 返回原始错误，保持错误类型用于后续判断
		}
		scopedLog.WithField("ipv4Released", len(ipv4ToRelease)).Debug("IPv4 addresses released")
	}

	// 释放IPv6地址
	if len(ipv6ToRelease) > 0 {
		err := n.real.releaseIPs(ctx, release, []string{}, ipv6ToRelease)
		if err != nil && !strings.Contains(err.Error(), "PrivateIpInvalid") {
			scopedLog.WithError(err).WithField("ipv6ToRelease", ipv6ToRelease).Warn("Failed to release IPv6 addresses")
			return err // 返回原始错误，保持错误类型用于后续判断
		}
		scopedLog.WithField("ipv6Released", len(ipv6ToRelease)).Debug("IPv6 addresses released")
	}

	return nil
}

// isDefinitiveError 判断是否为明确的错误（如QPS限制、参数错误等）
func (n *bceNetworkResourceSet) isDefinitiveError(err error) bool {
	return cloud.IsErrorENIPrivateIPNotFound(err) ||
		cloud.IsErrorRateLimit(err) ||
		cloud.IsErrorQuotaLimitExceeded(err) ||
		cloud.IsErrorBBCENIPrivateIPNotFound(err) ||
		cloud.IsErrorBBCENIPrivateIPExceedLimit(err) ||
		strings.Contains(err.Error(), "invalid parameter")
}

// verifyReleasedIPs 通过IaaS验证哪些IP已经实际释放
func (n *bceNetworkResourceSet) verifyReleasedIPs(ctx context.Context, eniID string, remainingIPs []string, scopedLog *logrus.Entry) (verified, stillPending []string) {
	// 一次性从IaaS获取ENI信息，避免多次API调用
	iaasENI, err := n.getENIFromIaaS(ctx, eniID, scopedLog)
	if err != nil {
		scopedLog.WithError(err).Warning("Failed to get ENI info from IaaS for verification, treating all IPs as still pending")
		return nil, remainingIPs
	}

	// 使用获取到的ENI信息验证每个IP
	for _, ip := range remainingIPs {
		if n.verifyIPReleasedWithENIInfo(ip, iaasENI, scopedLog) {
			verified = append(verified, ip)
			scopedLog.WithField("ip", ip).Debug("IP verified as released through IaaS check")
		} else {
			stillPending = append(stillPending, ip)
		}
	}

	return verified, stillPending
}

// waitForRetry 等待重试延迟，使用指数退避
func (n *bceNetworkResourceSet) waitForRetry(ctx context.Context, retryCount int, lastErr error, batchLog *logrus.Entry) error {
	// 创建指数退避配置
	backoffConfig := backoff.NewExponentialBackOff()
	backoffConfig.InitialInterval = time.Second
	backoffConfig.MaxInterval = 60 * time.Second
	backoffConfig.Multiplier = 2.0
	backoffConfig.RandomizationFactor = 0.1

	// 使用context包装的backoff
	b := backoff.WithContext(backoffConfig, ctx)

	// 使用Ticker进行重试等待
	ticker := backoff.NewTicker(b)
	defer ticker.Stop()

	// 等待下一个tick
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
		batchLog.WithFields(logrus.Fields{
			"error":      lastErr,
			"retryCount": retryCount,
		}).Warning("Batch IP release failed, retrying after backoff delay")
		return nil
	}
}

// getENIFromIaaS 从IaaS获取ENI信息，优化版本避免重复调用
func (n *bceNetworkResourceSet) getENIFromIaaS(ctx context.Context, eniID string, scopedLog *logrus.Entry) (*eni.Eni, error) {
	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 从IaaS获取ENI详情
	eniCache, err := n.bceclient.StatENI(verifyCtx, eniID)
	if err != nil {
		scopedLog.WithError(err).Warning("Failed to get ENI details from IaaS")
		return nil, err
	}

	if eniCache == nil {
		scopedLog.Warning("ENI not found in IaaS")
		return nil, fmt.Errorf("ENI %s not found in IaaS", eniID)
	}

	// 转换为包装类型，与remote_eni_syncer.go中的模式一致
	result := eni.Eni{}
	result.Eni = *eniCache

	scopedLog.WithField("eniID", eniID).Debug("Successfully retrieved ENI info from IaaS")
	return &result, nil
}

// verifyIPReleasedWithENIInfo 使用已获取的ENI信息验证IP是否已释放
func (n *bceNetworkResourceSet) verifyIPReleasedWithENIInfo(ip string, iaasENI *eni.Eni, scopedLog *logrus.Entry) bool {
	if iaasENI == nil {
		return false
	}

	// 检查IP是否在IaaS的ENI中
	for _, privateIP := range iaasENI.PrivateIpSet {
		if privateIP.PrivateIpAddress == ip {
			scopedLog.WithField("ip", ip).Debug("IP still exists in IaaS ENI")
			return false // IP仍然存在，说明没有释放成功
		}
	}

	for _, ipv6IP := range iaasENI.Ipv6PrivateIpSet {
		if ipv6IP.PrivateIpAddress == ip {
			scopedLog.WithField("ip", ip).Debug("IPv6 IP still exists in IaaS ENI")
			return false // IP仍然存在，说明没有释放成功
		}
	}

	// IP不在IaaS的ENI中，说明已经成功释放
	scopedLog.WithField("ip", ip).Debug("IP verified as released from IaaS ENI")
	return true
}

// updateENIWithReleasedIPs 更新ENI，移除已释放的IP
func (n *bceNetworkResourceSet) updateENIWithReleasedIPs(ctx context.Context, interfaceID string, releasedIPs []string, scopedLog *logrus.Entry) error {
	if len(releasedIPs) == 0 {
		return nil
	}

	eni, err := n.manager.enilister.Get(interfaceID)
	if err != nil {
		return fmt.Errorf("fail to get eni %s: %v", interfaceID, err)
	}

	// 创建已释放IP的映射表
	releaseIPMap := make(map[string]bool)
	for _, ipstr := range releasedIPs {
		releaseIPMap[ipstr] = true
	}

	// 更新ENI，移除已释放的IP
	return n.updateENIWithPoll(ctx, eni, func(eni *ccev2.ENI) *ccev2.ENI {
		var (
			ipv4Addrs []*models.PrivateIP
			ipv6Addrs []*models.PrivateIP
		)
		// 过滤掉已释放的IPv4地址
		for _, addr := range eni.Spec.ENI.PrivateIPSet {
			if _, ok := releaseIPMap[addr.PrivateIPAddress]; !ok {
				ipv4Addrs = append(ipv4Addrs, addr)
			}
		}
		// 过滤掉已释放的IPv6地址
		for _, addr := range eni.Spec.ENI.IPV6PrivateIPSet {
			if _, ok := releaseIPMap[addr.PrivateIPAddress]; !ok {
				ipv6Addrs = append(ipv6Addrs, addr)
			}
		}
		eni.Spec.ENI.PrivateIPSet = ipv4Addrs
		eni.Spec.ENI.IPV6PrivateIPSet = ipv6Addrs
		return eni
	})
}
