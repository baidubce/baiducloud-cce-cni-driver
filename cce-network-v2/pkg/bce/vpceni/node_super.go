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

	"github.com/baidubce/bce-sdk-go/services/eni"
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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
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
	errUnableToFindSubnet         = "unable to find matching subnet"

	retryDuration = 500 * time.Millisecond
	retryTimeout  = 15 * time.Second
	retryDelay    = 15 * time.Second

	vpcENISubsys = "vpc-eni-node-allocator"
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
	reuseIPs(ctx context.Context, ips []*models.PrivateIP, Owner string) (eniID string, ipDeletedFromoldEni bool, ipsReleased []string, err error)
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
		instanceID:              k8sObj.Spec.InstanceID,
		expiredVPCVersion:       make(map[string]int64),
		log:                     logging.NewSubysLogger(vpcENISubsys).WithField("instanceID", k8sObj.Spec.InstanceID).WithField("nodeName", k8sObj.Name),
		eventRecorder:           k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: vpcENISubsys}),
		eniAllocatedIPsErrorMap: make(map[string]*ccev2.StatusChange),
		creatingEni:             newCreatingEniSet(),
	}

	// 2. Get isSuportedENI from ccev2.NetResourceSet Label[k8s.LabelIsSupportENI], this Label is setted by nodediscovery determined by metadata api.
	if bceNrs.k8sObj.Spec.ENI != nil {
		switch bceNrs.k8sObj.Spec.ENI.InstanceType {
		case string(metadata.InstanceTypeExBBC):
			bceNrs.real = newBBCNetworkResourceSet(bceNrs)
		case string(metadata.InstanceTypeExEBC), string(metadata.InstanceTypeExEHC):
			bceNrs.real = newEBCNetworkResourceSet(newBCCNetworkResourceSet(bceNrs))
		case string(metadata.InstanceTypeExHPAS):
			manager.HPASWrapper()
			bceNrs.real = newHPASNetworkResourceSet(newBCCNetworkResourceSet(bceNrs))
		default:
			bceNrs.real = newBCCNetworkResourceSet(bceNrs)
		}
		bceNrs.getENIQuota()
	}

	return bceNrs
}

func (n *bceNetworkResourceSet) getENIQuota() ENIQuotaManager {
	n.limiterLock.Lock()
	if n.eniQuota != nil && n.lastResyncEniQuotaTime.Add(DayDuration).After(time.Now()) {
		n.limiterLock.Unlock()
		return n.eniQuota
	}
	n.limiterLock.Unlock()

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
		eniQuota := newCustomerIPQuota(n.log, k8s.WatcherClient(), n.k8sObj.Name, n.instanceID, nil)
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
	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			allENIId = append(allENIId, e.Name)

			// eni is not ready to use
			eniSubnet := e.Spec.ENI.SubnetID
			if e.Status.VPCStatus != ccev2.VPCENIStatusInuse {
				if e.Spec.Type == ccev2.ENIForBBC || e.Spec.UseMode == ccev2.ENIUseModePrimaryWithSecondaryIP {
					err := n.forceUpdateEniToInuse(ctx, scopedLog, e.Name)
					if err != nil {
						return err
					}
				} else {
					haveCreatingENI = true
					n.creatingEni.addCreatingENI(e.Name, e.CreationTimestamp.Time)
					return nil
				}
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
					// Only resolve primary IP addresses while ENI is primary mode
					if e.Spec.UseMode != ccev2.ENIUseModePrimaryIP && addr.Primary {
						continue
					}
					if !isIPv6 {
						availableIPsNumber++
					}
					// filter cross subnet private ip
					if addr.SubnetID != eniSubnet &&
						// only bbc enable cross subnet ip to ippool
						(n.instanceType != string(metadata.InstanceTypeExBBC) || !n.enableNodeAnnotationSubnet()) {
						continue
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
	if availableIPsNumber >= n.GetMaximumAllocatableIPv4() {
		n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
			func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
				e, ok := iface.Resource.(*eniResource)
				if !ok {
					return nil
				}
				if subnet, err := bcesync.GlobalBSM().GetSubnet(e.Spec.ENI.SubnetID); err == nil {
					if subnet.GetBorrowedIPNum(interfaceID) > 0 {
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
	selector, err := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelInstanceID:           n.instanceID,
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
		newENI.ObjectMeta.Labels[k8s.LabelInstanceID] = n.instanceID
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

	args := eni.ListEniArgs{
		VpcId:      vpcID,
		InstanceId: instanceID,
	}

	var (
		eniList []eni.Eni
		listErr error
	)
	if err = wait.PollImmediateWithContext(ctx, eniSyncPeriod, maxENISyncDuration, func(context.Context) (done bool, err error) {
		eniList, listErr = n.manager.bceclient.ListENIs(ctx, args)
		if listErr != nil {
			scopedLog.Warningf("failed to get eni: %v, retry after %s", listErr, eniSyncPeriod)
			return false, nil
		}
		return true, nil
	}); err != nil {
		scopedLog.WithError(err).Errorf("failed to get eni")
		return 0, "", err
	}
	scopedLog.WithField("CostTime", time.Since(startTime)).Infof("Force get ENI from IaaS, eniList length: %d", len(eniList))

	if len(eniList) == 0 {
		return 0, "", fmt.Errorf("no eni found")
	}
	createExternalENINum := 0
	for _, eni := range eniList {
		newENI := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					k8s.LabelInstanceID: n.instanceID,
					k8s.LabelNodeName:   n.k8sObj.Name,
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: ccev2.SchemeGroupVersion.String(),
					Kind:       ccev2.NRSKindDefinition,
					Name:       n.k8sObj.Name,
					UID:        n.k8sObj.UID,
				}},
				Name: eni.EniId,
			},
			Spec: ccev2.ENISpec{
				NodeName: n.k8sObj.Name,
				UseMode:  ccev2.ENIUseMode(n.k8sObj.Spec.ENI.UseMode),
				ENI: models.ENI{
					ID:                         eni.EniId,
					Name:                       eni.Name,
					MacAddress:                 eni.MacAddress,
					InstanceID:                 eni.InstanceId,
					SecurityGroupIds:           eni.SecurityGroupIds,
					EnterpriseSecurityGroupIds: eni.EnterpriseSecurityGroupIds,
					Description:                eni.Description,
					VpcID:                      eni.VpcId,
					ZoneName:                   eni.ZoneName,
					SubnetID:                   eni.SubnetId,
					PrivateIPSet:               bcesync.ToModelPrivateIP(eni.PrivateIpSet, eni.VpcId, eni.SubnetId),
					IPV6PrivateIPSet:           bcesync.ToModelPrivateIP(eni.Ipv6PrivateIpSet, eni.VpcId, eni.SubnetId),
				},
				RouteTableOffset:          n.k8sObj.Spec.ENI.RouteTableOffset,
				InstallSourceBasedRouting: n.k8sObj.Spec.ENI.InstallSourceBasedRouting,
				Type:                      ccev2.ENIType(n.k8sObj.Spec.ENI.InstanceType),
			},
		}
		bcesync.ElectENIIPv6PrimaryIP(newENI)
		_, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, newENI, metav1.CreateOptions{})
		if err != nil {
			return createExternalENINum, "partial create external ENI from IaaS to k8s error", fmt.Errorf("failed to create ENI: %w", err)
		}
		createExternalENINum++

		n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeNormal, "CreateENISuccess", "create external ENI %s from IaaS on NRS %s success", newENI.Name, n.k8sObj.Name)
		scopedLog = scopedLog.WithField("ENIID", newENI.Name)
	}
	return createExternalENINum, "", nil
}

// CreateInterface create a new ENI
func (n *bceNetworkResourceSet) CreateInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	var (
		availableENICount int
		allENIId          []string
		eniQuota          = n.getENIQuota()
	)
	if eniQuota == nil {
		return 0, "", fmt.Errorf("eni quota is nil, please retry later")
	}
	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
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
		interfaceNum, msg, err = n.forceGetENIFromInstanceGroup(ctx, scopedLog, n.k8sObj.Spec.ENI.VpcID, n.instanceID)
		if interfaceNum > 0 {
			return interfaceNum, msg, err
		}

		interfaceNum, msg, err = n.forceGetENIFromIaaS(ctx, scopedLog, n.k8sObj.Spec.ENI.VpcID, n.instanceID)
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

	preAllocateENINum := math.IntMin(eniQuota.GetMaxENI(), n.k8sObj.Spec.ENI.PreAllocateENI)
	preAllocateENINum = preAllocateENINum - availableENICount - 1
	retryTimes := preAllocateENINum * 2
	for i := 0; i < preAllocateENINum; i++ {
		inums++
		go func() {
			retry := 0
			for retry < retryTimes {
				n.creatingEni.add(1)
				_, _, e := n.real.createInterface(ctx, allocation, scopedLog)
				n.creatingEni.add(-1)
				if e == nil {
					scopedLog.Infof("create addition interface success")
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
	crossSubnetUsed := make(ipamTypes.AllocationMap, 0)
	// Select only the ENI of the local node
	selector, err := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelInstanceID: n.instanceID,
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
		allocatedCrossSubnetIPNum := 0
		// add eni statistics
		simple := ccev2.SimpleENIStatus{
			ID:                        enis[i].Spec.ENI.ID,
			VPCStatus:                 string(enis[i].Status.VPCStatus),
			CCEStatus:                 string(enis[i].Status.CCEStatus),
			SubnetID:                  enis[i].Spec.SubnetID,
			IsMoreAvailableIPInSubnet: false,
		}
		eniStatusMap[enis[i].Name] = simple

		addCrossSubnetAddr := func(addr *models.PrivateIP) {
			allocationIP := ipamTypes.AllocationIP{
				Resource:  enis[i].Name,
				IsPrimary: addr.Primary,
				SubnetID:  addr.SubnetID,
			}
			ceps, _ := n.manager.cepClient.GetByIP(addr.PrivateIPAddress)
			if len(ceps) > 0 {
				allocationIP.Owner = ceps[0].Namespace + "/" + ceps[0].Name
			}
			if addr.SubnetID != enis[i].Spec.ENI.SubnetID {
				crossSubnetUsed[addr.PrivateIPAddress] = allocationIP
				allocatedCrossSubnetIPNum++
			}
		}

		for _, addr := range enis[i].Spec.PrivateIPSet {
			addCrossSubnetAddr(addr)
		}
		for _, addr := range enis[i].Spec.IPV6PrivateIPSet {
			addCrossSubnetAddr(addr)
		}

		// eni quota may be nil
		if eniQuota := n.getENIQuota(); eniQuota != nil {
			capacity := eniQuota.GetMaxIP()
			simple.AvailableIPNum = capacity - len(enis[i].Spec.PrivateIPSet)
		}

		simple.AllocatedCrossSubnetIPNum = allocatedCrossSubnetIPNum
		simple.AllocatedIPNum = len(enis[i].Spec.PrivateIPSet)

		for j := range n.availableSubnets {
			if n.availableSubnets[j].Spec.ID == enis[i].Spec.ENI.SubnetID {
				simple.IsMoreAvailableIPInSubnet = !n.availableSubnets[j].Status.HasNoMoreIP
			}
		}
	}
	resource.Status.ENIs = eniStatusMap
	resource.Status.IPAM.CrossSubnetUsed = crossSubnetUsed

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
	subnets := n.FilterAvailableSubnetIds(n.k8sObj.Spec.ENI.SubnetIDs, n.GetMinimumAllocatableIPv4())
	if len(subnets) == 0 {
		errStr := fmt.Sprintf("subnets [%v] are not available for node %s", n.k8sObj.Spec.ENI.SubnetIDs, n.k8sObj.Name)
		n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeWarning, ccev2.ErrorCodeNoAvailableSubnet, errStr)
		metrics.IPAMErrorCounter.WithLabelValues(ccev2.ErrorCodeNoAvailableSubnet, "NetResourceSet", n.k8sObj.Name).Inc()
		return goerrors.New(errStr)
	}
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
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModeSecondaryIP) {
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
			ipv4PaticalToAllocate = math.IntMin(ipv4ToAllocate, 10)
			ipv6PaticalToAllocate = math.IntMin(ipv6ToAllocate, 10)
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

	// Needed for selecting the same ENI to release IPs from
	// when more than one ENI qualifies for release
	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			// if r.IPsToRelease is not empty, don't excute the following code
			if len(r.IPsToRelease) > 0 {
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
			})
			scopeENILog.Debug("Considering ENI for IP release")

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
			}).Debug("ENI has unused IPs that can be released")
			// Select the ENI with the most addresses available for release
			if len(r.IPsToRelease) == 0 {
				r.InterfaceID = e.Spec.ENI.ID
				r.PoolID = ipamTypes.PoolID(e.Spec.ENI.SubnetID)
				if ipv4ToFreeCount > 0 && len(ipv4ToFree) > 0 {
					r.IPsToRelease = ipv4ToFree[:ipv4ToFreeCount]
				}
				if ipv6ToFreeCount > 0 && len(ipv4ToFree) > 0 {
					r.IPsToRelease = append(r.IPsToRelease, ipv6ToFree[:ipv6ToFreeCount]...)
				}
			}
			return nil
		})

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
// perform the release of IPs.
func (n *bceNetworkResourceSet) ReleaseIPs(ctx context.Context, release *ipam.ReleaseAction) error {
	if release == nil || len(release.IPsToRelease) == 0 {
		return nil
	}
	scopeLog := n.log.WithFields(logrus.Fields{
		"ips":   release.IPsToRelease,
		"eniID": release.InterfaceID,
	})
	scopeLog.Debug("start to releasing IPs")
	var (
		ipv4ToReleaseSet ipToReleaseSet
		ipv6ToReleaseSet ipToReleaseSet
		releaseIPMap     = make(map[string]bool)
	)
	// append ip to eni
	// get eni
	eni, err := n.manager.enilister.Get(string(release.InterfaceID))
	if err != nil {
		scopeLog.WithField("eniID", release.InterfaceID).WithError(err).Warn("get eni failed")
		return err
	}
	eni = eni.DeepCopy()

	// 1. check to release IP
	for _, ipstr := range release.IPsToRelease {
		ip := net.ParseIP(ipstr)
		if ip != nil && ip.To4() == nil {
			containsIP := false
			for _, privateAddress := range eni.Spec.IPV6PrivateIPSet {
				if privateAddress.PrivateIPAddress == ipstr {
					containsIP = true
					break
				}
			}
			if containsIP {
				ipv6ToReleaseSet = ipv6ToReleaseSet.append(ipstr, 10)
			}
		} else if ip != nil {
			containsIP := false
			for _, privateAddress := range eni.Spec.PrivateIPSet {
				if privateAddress.PrivateIPAddress == ipstr {
					containsIP = true
					break
				}
			}
			if containsIP {
				ipv4ToReleaseSet = ipv4ToReleaseSet.append(ipstr, 10)
			}
		}
	}

	// 2. do release IP
	doRelease := func(toReleaseSet ipToReleaseSet, isIPv6 bool) error {
		isPartialSuccess := false
		for _, ipToRelease := range toReleaseSet {
			scopeLog = scopeLog.WithFields(logrus.Fields{
				"partialToRelease": ipToRelease,
			})
			if isIPv6 {
				err = n.real.releaseIPs(ctx, release, []string{}, ipToRelease)
			} else {
				err = n.real.releaseIPs(ctx, release, ipToRelease, []string{})
			}
			if err != nil && !strings.Contains(err.Error(), "PrivateIpInvalid") {
				err = fmt.Errorf("release ip %s failed: %v", ipToRelease, err)
				n.appendAllocatedIPError(release.InterfaceID, ccev2.NewErrorStatusChange(err.Error()))
				if !isPartialSuccess {
					return err
				}
			} else {
				scopeLog.Info("release ip success")
				isPartialSuccess = true

				// update releaseIPMap
				for _, ipstr := range ipToRelease {
					releaseIPMap[ipstr] = true
				}
			}
		}
		return nil
	}
	err = doRelease(ipv4ToReleaseSet, false)
	if err != nil {
		return err
	}
	err = doRelease(ipv6ToReleaseSet, true)
	if err != nil {
		return err
	}

	// 3. update IP on eni
	return n.updateENIWithPoll(ctx, eni, func(eni *ccev2.ENI) *ccev2.ENI {
		var (
			ipv4Addrs []*models.PrivateIP
			ipv6Addrs []*models.PrivateIP
		)
		for _, addr := range eni.Spec.ENI.PrivateIPSet {
			if _, ok := releaseIPMap[addr.PrivateIPAddress]; !ok {
				ipv4Addrs = append(ipv4Addrs, addr)
			}
		}
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

func (n *bceNetworkResourceSet) updateENIWithPoll(ctx context.Context, eni *ccev2.ENI, refresh func(eni *ccev2.ENI) *ccev2.ENI) error {
	var oldversion int64

	err := wait.PollImmediate(retryDuration, retryTimeout, func() (bool, error) {
		// get eni
		// append ip to eni
		eni, ierr := n.manager.enilister.Get(string(eni.Name))
		if ierr != nil {
			return false, fmt.Errorf("get eni %s failed: %v", eni.Name, ierr)
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
		return fmt.Errorf("update eni %s failed: %v", eni.Name, err)
	}

	n.mutex.Lock()
	n.expiredVPCVersion[eni.Name] = oldversion
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

// FilterAvailableSubnet implements endpoint.DirectEndpointOperation
func (n *bceNetworkResourceSet) FilterAvailableSubnet(subnets []*bcesync.BorrowedSubnet, minAllocateIPs int) []*bcesync.BorrowedSubnet {
	contry := operatorOption.Config.BCECloudContry
	region := operatorOption.Config.BCECloudRegion
	zone := n.k8sObj.Spec.ENI.AvailabilityZone

	var filtedSubnets []*bcesync.BorrowedSubnet
	for _, subnet := range subnets {
		if !strings.Contains(subnet.Spec.AvailabilityZone, api.TransAvailableZoneToZoneName(contry, region, zone)) {
			continue
		}
		if !subnet.CanBorrow(minAllocateIPs) {
			continue
		}
		filtedSubnets = append(filtedSubnets, subnet)
	}
	return filtedSubnets
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
	return n.FilterAvailableSubnet(filtedSubnets, minAllocateIPs)
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
		if sbn.Spec.Exclusive {
			continue
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
		ipset       []*models.PrivateIP
		ipsReleased []string
		err         error

		// interfaceID is the ENI ID or bbc instance ID
		interfaceID         string
		ipDeletedFromoldEni bool = false
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
		interfaceID, ipDeletedFromoldEni, ipsReleased, err = n.real.reuseIPs(ctx, ipset, action.Owner)
		if ipDeletedFromoldEni && (interfaceID != action.Interface) {
			oldEniID := action.Interface
			oldEni, err := n.manager.enilister.Get(oldEniID)
			// if eni not found, ignore it, maybe it has been deleted
			if err != nil && !kerrors.IsNotFound(err) {
				return fmt.Errorf("get eni %s failed: %v", oldEniID, err)
			}
			if oldEni != nil {
				// update old eni status

				releaseIPMap := make(map[string]bool)
				// update releaseIPMap
				for _, ipstr := range ipsReleased {
					releaseIPMap[ipstr] = true
				}
				// update old eni for release IPs
				n.updateENIWithPoll(ctx, oldEni, func(eni *ccev2.ENI) *ccev2.ENI {
					var (
						ipv4Addrs []*models.PrivateIP
						ipv6Addrs []*models.PrivateIP
					)
					for _, addr := range eni.Spec.ENI.PrivateIPSet {
						if _, ok := releaseIPMap[addr.PrivateIPAddress]; !ok {
							ipv4Addrs = append(ipv4Addrs, addr)
						}
					}
					for _, addr := range eni.Spec.ENI.IPV6PrivateIPSet {
						if _, ok := releaseIPMap[addr.PrivateIPAddress]; !ok {
							ipv6Addrs = append(ipv6Addrs, addr)
						}
					}
					eni.Spec.ENI.PrivateIPSet = ipv4Addrs
					eni.Spec.ENI.IPV6PrivateIPSet = ipv6Addrs
					return eni
				})
			} else {
				n.log.Warnf("the old eni %s of the cep reused ip %s is not found, ignore it", oldEniID, ipset[0].PrivateIPAddress)
			}
		}

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
	return n.updateENIWithPoll(ctx, eni, func(eni *ccev2.ENI) *ccev2.ENI {
		for index := range ipset {
			address := ipset[index]
			if address.PrivateIPAddress == "" {
				continue
			}
			ip := net.ParseIP(address.PrivateIPAddress)
			if ip.To4() == nil {
				eni.Spec.ENI.PrivateIPSet = appenUniqueElement(eni.Spec.IPV6PrivateIPSet, []*models.PrivateIP{address})
			} else {
				eni.Spec.ENI.PrivateIPSet = appenUniqueElement(eni.Spec.PrivateIPSet, []*models.PrivateIP{address})
			}
		}
		return eni
	})
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
	return n.ReleaseIPs(ctx, action)
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

		if eniStatus, ok := n.eniAllocatedIPsErrorMap[eniID]; ok {
			if now.Sub(n.eniAllocatedIPsErrorMap[eniID].Time.Time) < 10*time.Minute {
				eniStastic.LastAllocatedIPError = eniStatus
			}
		}
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
			n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeWarning, "FailedBorrowSubnet",
				"Failed to get borrow subnet %s: %v", newENI.Spec.SubnetID, err)
			return err
		}

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
			return goerrors.New(errMsg)
		}
		newENI.Spec.BorrowIPCount = borrowedIPs
	}
	return nil
}

type fnReleaseIPsFromCLoud func(ctx context.Context, scopedLog *logrus.Entry, eniID string, toReleaseIPs []string) error

// rleaseOldIP release old ip if it is not used by other endpoint
// return true: ip is used by current endpoint, do not need to release
// return false: ip is used by other endpoint, need to release
func (n *bceNetworkResourceSet) rleaseOldIP(ctx context.Context, scopedLog *logrus.Entry, ips []*models.PrivateIP, namespace string, name string, releaseIPsFromCLoud fnReleaseIPsFromCLoud) (isLocalIP bool, ipsReleased []string, err error) {
	for _, privateIP := range ips {
		ceps, err := n.manager.cepClient.GetByIP(privateIP.PrivateIPAddress)
		if err == nil && len(ceps) > 0 {
			for _, cep := range ceps {
				if cep.Namespace != namespace || cep.Name != name {
					return false, ipsReleased, fmt.Errorf("ip %s has been used by other endpoint %s/%s", privateIP.PrivateIPAddress, cep.Namespace, cep.Name)
				}
			}
		}
	}

	// if ip is local eni, do not need to release
	isLocalENI := false
	toReleaseEniIPs := make(map[string][]string, 0)
	for _, privateIP := range ips {
		enis, err := watchers.ENIClient.GetByIP(privateIP.PrivateIPAddress)
		if err != nil && len(enis) == 0 {
			isLocalENI = false
			break
		}
		if err == nil {
			for _, eni := range enis {
				if eni.Spec.NodeName == n.k8sObj.Name {
					isLocalENI = true
				} else {
					// do not release ip which use same subnet with eni
					if eni.Spec.SubnetID == privateIP.SubnetID {
						return false, ipsReleased, fmt.Errorf("ip %s has been used by other eni %s for cached ip", privateIP.PrivateIPAddress, eni.Name)
					}
					toReleaseEniIPs[eni.Name] = append(toReleaseEniIPs[eni.Name], privateIP.PrivateIPAddress)
					isLocalENI = false
					break
				}
			}
		}
	}
	if isLocalENI {
		return true, ipsReleased, nil
	}

	cep, err := n.manager.cepClient.Lister().CCEEndpoints(namespace).Get(name)
	if err != nil {
		return false, ipsReleased, fmt.Errorf("get endpoint %s/%s failed: %v", namespace, name, err)
	}
	if cep.Status.Networking != nil && cep.Status.Networking.NodeIP != n.k8sObj.Name && len(toReleaseEniIPs) > 0 {
		scopedLog.WithField("namespace", namespace).WithField("name", name).Debug("try to clean ip from old eni")

		for eniID := range toReleaseEniIPs {
			toReleaseIPs := toReleaseEniIPs[eniID]
			if len(toReleaseIPs) > 0 {
				err = releaseIPsFromCLoud(ctx, scopedLog, eniID, toReleaseIPs)
				if err != nil {
					return false, ipsReleased, fmt.Errorf("release ip %v from eni %s failed: %v", toReleaseIPs, eniID, err)
				}
				ipsReleased = append(ipsReleased, toReleaseIPs...)
			}
		}
	}
	return false, ipsReleased, err
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
