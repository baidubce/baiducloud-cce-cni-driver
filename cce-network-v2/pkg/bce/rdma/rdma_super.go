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
package rdma

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
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
	retryDuration = 500 * time.Millisecond
	retryTimeout  = 15 * time.Second

	rdmaSubsys = "rdma-netresourceset-allocator"
)

type realNetResourceSetInf interface {
	refreshENIQuota(scopeLog *logrus.Entry) (RdmaEniQuotaManager, error)
	createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error)
	// PrepareIPAllocation is called to calculate the number of IPs that
	// can be allocated on the node and whether a new network interface
	// must be attached to the node.
	prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error)
	// AllocateIPs is called after invoking PrepareIPAllocation and needs
	// to perform the actual allocation.
	allocateIPs(ctx context.Context, scopedLog *logrus.Entry, iaasClient client.IaaSClient, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
		ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error)

	releaseIPs(ctx context.Context, iaasClient client.IaaSClient, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error
	getMaximumAllocatable(eniQuota RdmaEniQuotaManager) int
	getMinimumAllocatable() int
}

// bceRDMANetResourceSet represents a Kubernetes node running CCE with an associated
// NetResourceSet custom resource
type bceRDMANetResourceSet struct {
	// node contains the general purpose fields of a node
	node *ipam.NetResource

	// mutex protects members below this field
	mutex lock.RWMutex

	// k8sObj is the NetResourceSet custom resource representing the node
	k8sObj *ccev2.NetResourceSet

	// manager is the ecs node manager responsible for this node
	manager *rdmaInstancesManager

	// instanceID of the node
	instanceID   string
	instanceType string

	// bcc instance info
	bccInfo *bccapi.InstanceModel

	// The node currently being created by ENI should have only one.
	creatingENI *ccev2.ENI

	// limit
	eniQuota               RdmaEniQuotaManager
	lastResyncEniQuotaTime time.Time
	limiterLock            lock.RWMutex

	real realNetResourceSetInf

	// whatever the resource version of the eni have been synced
	// this key is eni id, value is the resource version
	expiredVPCVersion map[string]int64

	eventRecorder record.EventRecorder

	log                     *logrus.Entry
	eniAllocatedIPsErrorMap map[string]*ccev2.StatusChange
}

// NewNetResourceSet returns a new NetResourceSet
func NewNetResourceSet(node *ipam.NetResource, k8sObj *ccev2.NetResourceSet, manager *rdmaInstancesManager) *bceRDMANetResourceSet {
	// 1. Create a new RDMA NetResourceSet object
	anrs := &bceRDMANetResourceSet{
		node:                    node,
		k8sObj:                  k8sObj,
		manager:                 manager,
		instanceID:              k8sObj.Spec.InstanceID,
		expiredVPCVersion:       make(map[string]int64),
		eventRecorder:           k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: rdmaSubsys}),
		log:                     logging.NewSubysLogger(rdmaSubsys).WithField("instanceID", k8sObj.Spec.InstanceID).WithField("netResourceSetName", k8sObj.Name),
		eniAllocatedIPsErrorMap: make(map[string]*ccev2.StatusChange),
	}

	// 2. Create a new provider object for this node
	if anrs.k8sObj.Spec.ENI != nil {
		anrs.real = newRdmaNetResourceSetWrapper(anrs)
	}
	anrs.getRdmaEniQuota()

	return anrs
}

func (n *bceRDMANetResourceSet) getRdmaEniQuota() RdmaEniQuotaManager {
	n.limiterLock.Lock()
	if n.eniQuota != nil && n.lastResyncEniQuotaTime.Add(DayDuration).After(time.Now()) {
		n.limiterLock.Unlock()
		return n.eniQuota
	}
	n.limiterLock.Unlock()

	// slow path to claculate limiter
	ctx := logfields.NewContext()
	scopedLog := n.log.WithFields(logrus.Fields{
		"nodeName":     n.k8sObj.Name,
		"method":       "getRdmaEniQuota",
		"instanceType": n.k8sObj.Spec.ENI.InstanceType,
	}).WithContext(ctx)

	// fast path to claculate eni quota when operaor has been restarted
	// isSetLimiter returns true if the node has set limiter
	// if the node has set limiter, it will return a limiter from the ENI spec
	if n.k8sObj.Annotations != nil && n.k8sObj.Annotations[k8s.AnnotationIPResourceCapacitySynced] != "" {
		lastResyncTime := n.k8sObj.Annotations[k8s.AnnotationIPResourceCapacitySynced]
		t, err := time.Parse(time.RFC3339, lastResyncTime)
		if err != nil || t.Add(DayDuration).Before(time.Now()) {
			goto slowPath
		}
		eniQuota := newCustomerIPQuota(nil, nil, nil, n.instanceID, nil)
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
func (n *bceRDMANetResourceSet) slowCalculateRealENICapacity(scopedLog *logrus.Entry) RdmaEniQuotaManager {
	rdmaEniQuota, err := n.real.refreshENIQuota(scopedLog)
	if err != nil {
		scopedLog.Errorf("calculate eniQuota failed: %v", err)
		return nil
	}

	if len(n.k8sObj.Annotations) > 0 {
		// override max eni num from annotation
		if maxRdmaEniStr, ok := n.k8sObj.Annotations[k8s.AnnotationNodeMaxRdmaEniNum]; ok {
			maxRdmaEni, err := strconv.Atoi(maxRdmaEniStr)
			if err != nil {
				scopedLog.Errorf("parse max rdma eni num [%s] from annotation failed: %v", maxRdmaEniStr, err)
			} else if maxRdmaEni < rdmaEniQuota.GetMaxENI() {
				scopedLog.Warnf("max eni num from annotation is smaller than real: %d < %d", maxRdmaEni, rdmaEniQuota.GetMaxENI())
			} else if maxRdmaEni >= 0 {
				rdmaEniQuota.SetMaxENI(maxRdmaEni)
				scopedLog.Infof("override max eni num from annotation: %d", maxRdmaEni)
			}
		}

		// override max ip num from annotation
		if maxRdmaIpStr, ok := n.k8sObj.Annotations[k8s.AnnotationNodeMaxPerRdmaEniIpsNum]; ok {
			maxRdmaIp, err := strconv.Atoi(maxRdmaIpStr)
			if err != nil {
				scopedLog.Errorf("parse max rdma ip num [%s] from annotation failed: %v", maxRdmaIpStr, err)
			} else if maxRdmaIp >= 0 {
				rdmaEniQuota.SetMaxIP(maxRdmaIp)
			}
		}
	}

	// if eniQuota is empty, it means the node has not set quota
	// it should be retry later
	if rdmaEniQuota.GetMaxENI() == 0 && rdmaEniQuota.GetMaxIP() == 0 {
		return nil
	}
	// 3. Override the CCE Node capacity to the CCE NetworkResourceSet object
	err = n.overrideENICapacityToNetworkResourceSet(rdmaEniQuota)
	if err != nil {
		scopedLog.Errorf("override ENI capacity to Node failed: %v", err)
		return nil
	}

	n.limiterLock.Lock()
	n.eniQuota = rdmaEniQuota
	n.lastResyncEniQuotaTime = time.Now()
	n.limiterLock.Unlock()

	scopedLog.WithFields(logrus.Fields{
		"maxRdmaEni":          n.eniQuota.GetMaxENI(),
		"maxRdmaIpPerRdmaEni": n.eniQuota.GetMaxIP(),
	}).Info("override RDMA ENI capacity to NetResourceSet success")
	return n.eniQuota
}

// overrideENICapacityToNetworkResourceSet accoding to the Node.limiter field, override the
// capacity of ENI to the Node.k8sObj.Spec
func (n *bceRDMANetResourceSet) overrideENICapacityToNetworkResourceSet(rdmaEniQuota RdmaEniQuotaManager) error {
	ctx := logfields.NewContext()
	// todo move capacity to better place in k8s rest api
	err := rdmaEniQuota.SyncCapacityToK8s(ctx)
	if err != nil {
		return fmt.Errorf("failed to sync rdma eni capacity to k8s node: %v", err)
	}
	// update the NetworkResourceSet until 30s timeout
	// if update operation return error, we will get the leatest version of NetworkResourceSet and try again
	err = wait.PollImmediate(200*time.Millisecond, 30*time.Second, func() (bool, error) {
		old, err := n.manager.nrsGetterUpdater.Get(n.k8sObj.Name)
		if err != nil {
			return false, err
		}
		k8sObj := old.DeepCopy()
		k8sObj.Spec.ENI.MaxAllocateENI = rdmaEniQuota.GetMaxENI()
		k8sObj.Spec.ENI.MaxIPsPerENI = rdmaEniQuota.GetMaxIP()
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
func (n *bceRDMANetResourceSet) UpdatedNode(obj *ccev2.NetResourceSet) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	n.k8sObj = obj
	n.instanceID = obj.Spec.InstanceID
}

// ResyncInterfacesAndIPs is called to synchronize the latest list of
// interfaces and IPs associated with the node. This function is called
// sparingly as this information is kept in sync based on the success
// of the functions AllocateIPs(), ReleaseIPs() and CreateInterface().
func (n *bceRDMANetResourceSet) ResyncInterfacesAndIPs(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
	var (
		a        = ipamTypes.AllocationMap{}
		allENIId []string
	)
	n.waitForENISynced(ctx)

	availabelENIsNumber := 0
	usedENIsNumber := 0

	// the vifFeatures is decided by the NetResourceSet's annotation
	vifFeatures := n.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]

	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			allENIId = append(allENIId, e.Name)

			// underlay-RDMA and e-RDMA(ERI) is need to check ENI VPCStatus
			if e.Status.VPCStatus != ccev2.VPCENIStatusInuse {
				return nil
			}

			availabelENIsNumber++
			if e.Status.CCEStatus == ccev2.ENIStatusUsingInPod {
				usedENIsNumber++
			}

			// The node currently being created by ENI can be viewed in the informer.
			// It can be deleted from the cache
			if n.creatingENI != nil && e.Name == n.creatingENI.Name {
				n.creatingENI = nil
			}

			addAllocationIP := func(ips []*models.PrivateIP) {
				for _, addr := range ips {
					// Only resolve primary IP addresses while ENI is primary mode
					if addr.Primary {
						continue
					}
					a[addr.PrivateIPAddress] = ipamTypes.AllocationIP{
						Resource: vifFeatures,
						SubnetID: addr.SubnetID,
					}
				}
			}

			addAllocationIP(e.Spec.ENI.PrivateIPSet)
			addAllocationIP(e.Spec.ENI.IPV6PrivateIPSet)

			return nil
		})

	return a, nil
}

// CreateInterface create a new ENI
func (n *bceRDMANetResourceSet) CreateInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	return n.real.createInterface(ctx, allocation, scopedLog)
}

// appendAllocatedIPError append eni allocated ip error to bcenode
// those errors will be added to the status of nrs by 2 minutes
func (n *bceRDMANetResourceSet) appendAllocatedIPError(eniID string, openAPIStatus *ccev2.StatusChange) {
	n.mutex.Lock()
	n.eniAllocatedIPsErrorMap[eniID] = openAPIStatus
	n.mutex.Unlock()

	// append status change to nrs event
	if openAPIStatus.Code != ccev2.ErrorCodeSuccess {
		n.eventRecorder.Eventf(
			n.k8sObj, corev1.EventTypeWarning, openAPIStatus.Code, openAPIStatus.Message)
	}
}

// refreshENIAllocatedIPErrors fill nrs status with eni allocated ip errors
// error are only valid if they occured within the last 10 minutes
func (n *bceRDMANetResourceSet) refreshENIAllocatedIPErrors(resource *ccev2.NetResourceSet) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	now := time.Now()
	for eniID := range resource.Status.ENIs {
		eniStastic := resource.Status.ENIs[eniID]
		eniStastic.LastAllocatedIPError = nil

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

// PopulateStatusFields is called to give the implementation a chance
// to populate any implementation specific fields in NetResourceSet.Status.
func (n *bceRDMANetResourceSet) PopulateStatusFields(resource *ccev2.NetResourceSet) {
	eniStatusMap := make(map[string]ccev2.SimpleENIStatus)
	crossSubnetUsed := make(ipamTypes.AllocationMap, 0)

	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			eni, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			simple := ccev2.SimpleENIStatus{
				ID:        eni.Spec.ENI.ID,
				VPCStatus: string(eni.Status.VPCStatus),
				CCEStatus: string(eni.Status.CCEStatus),
			}
			eniStatusMap[eni.Name] = simple

			var addCrossSubnetAddr = func(addr *models.PrivateIP) {
				allocationIP := ipamTypes.AllocationIP{
					Resource:  eni.Name,
					IsPrimary: addr.Primary,
					SubnetID:  addr.SubnetID,
				}
				ceps, _ := n.manager.cepClient.GetByIP(addr.PrivateIPAddress)
				if len(ceps) > 0 {
					allocationIP.Owner = ceps[0].Namespace + "/" + ceps[0].Name
				}
				if addr.SubnetID != eni.Spec.ENI.SubnetID {
					crossSubnetUsed[addr.PrivateIPAddress] = allocationIP
				}
			}

			for _, addr := range eni.Spec.PrivateIPSet {
				addCrossSubnetAddr(addr)
			}
			for _, addr := range eni.Spec.IPV6PrivateIPSet {
				addCrossSubnetAddr(addr)
			}
			return nil
		})

	resource.Status.ENIs = eniStatusMap
	resource.Status.IPAM.CrossSubnetUsed = crossSubnetUsed
	n.refreshENIAllocatedIPErrors(resource)
}

// PrepareIPAllocation is called to calculate the number of IPs that
// can be allocated on the node and whether a new network interface
// must be attached to the node.
func (n *bceRDMANetResourceSet) PrepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	if quota := n.getRdmaEniQuota(); quota == nil {
		return nil, fmt.Errorf("RdmaEniQuota is nil, please retry later")
	}
	return n.real.prepareIPAllocation(scopedLog)
}

// AllocateIPs is called after invoking PrepareIPAllocation and needs
// to perform the actual allocation.
func (n *bceRDMANetResourceSet) AllocateIPs(ctx context.Context, allocation *ipam.AllocationAction) error {
	rdmaEniQuota := n.getRdmaEniQuota()
	if rdmaEniQuota == nil {
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
		eni, err := n.manager.eniLister.Get(string(allocation.InterfaceID))
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
			ipv4ToAllocate = math.IntMin(allocation.AvailableForAllocationIPv4, rdmaEniQuota.GetMaxIP()-len(eni.Spec.ENI.PrivateIPSet))
		}
		if operatorOption.Config.EnableIPv6 {
			// ipv6 address should not be more than ipv4 address
			ipv6ToAllocate = math.IntMin(allocation.AvailableForAllocationIPv6, rdmaEniQuota.GetMaxIP()-len(eni.Spec.ENI.IPV6PrivateIPSet))
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

		// the vifFeatures is decided by the NetResourceSet's annotation
		vifFeatures := n.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]
		iaasClient := n.manager.getIaaSClient(vifFeatures)

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
			partialIPv4Result, partialIPv6Result, err := n.real.allocateIPs(ctx, scopedLog, iaasClient, allocation, ipv4PaticalToAllocate, ipv6PaticalToAllocate)
			if err != nil {
				allocateLog.WithError(err).Error("failed to allocate ips from eni")
				err = fmt.Errorf("allocate RDMA %d IP(s) on NRS %s failed, error: %v", ipv4PaticalToAllocate, n.k8sObj.Name, err) // IPv6 is not supported
				n.appendAllocatedIPError(allocation.InterfaceID, ccev2.NewErrorStatusChange(err.Error()))
				if !isPartialSuccess {
					return err
				} else {
					// if partial success, we will continue to allocate
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
		n.eventRecorder.Eventf(n.k8sObj, corev1.EventTypeNormal, "AllocateIPsSuccess",
			"allocate RDMA IPs %d on NRS %s success", ipv4PaticalToAllocate, n.k8sObj.Name) // IPv6 is not supported

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
func (n *bceRDMANetResourceSet) PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ipam.ReleaseAction {
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
func (n *bceRDMANetResourceSet) ReleaseIPs(ctx context.Context, release *ipam.ReleaseAction) error {
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
	eni, err := n.manager.eniLister.Get(string(release.InterfaceID))
	if err != nil {
		return fmt.Errorf("get eni %s failed: %v", release.InterfaceID, err)
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
		var isPartialSuccess = false
		// the vifFeatures is decided by the NetResourceSet's annotation
		vifFeatures := n.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]
		iaasClient := n.manager.getIaaSClient(vifFeatures)

		for _, ipToRelease := range toReleaseSet {
			scopeLog = scopeLog.WithFields(logrus.Fields{
				"partialToRelease": ipToRelease,
			})
			if isIPv6 {
				err = n.real.releaseIPs(ctx, iaasClient, release, []string{}, ipToRelease)
			} else {
				err = n.real.releaseIPs(ctx, iaasClient, release, ipToRelease, []string{})
			}
			if err != nil {
				scopeLog.WithError(err).Error("release ip failed")
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

func (n *bceRDMANetResourceSet) updateENIWithPoll(ctx context.Context, eni *ccev2.ENI, refresh func(eni *ccev2.ENI) *ccev2.ENI) error {
	var oldversion int64

	err := wait.PollImmediate(retryDuration, retryTimeout, func() (bool, error) {
		// get eni
		// append ip to eni
		eni, ierr := n.manager.eniLister.Get(string(eni.Name))
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
		if errors.IsConflict(ierr) || errors.IsResourceExpired(ierr) {
			return false, nil
		}
		// we should recorde log with eni attributes and ips if update eni success
		log.WithFields(logrus.Fields{
			"eniID":      eni.Name,
			"instanceID": eni.Spec.InstanceID,
			"node":       eni.Spec.NodeName,
			"eni":        logfields.Json(eni.Spec.ENI),
		}).Infof("update eni success")
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
func (n *bceRDMANetResourceSet) GetMaximumAllocatableIPv4() int {
	rdmaEniQuota := n.getRdmaEniQuota()
	if rdmaEniQuota == nil {
		return 0
	}
	return n.real.getMaximumAllocatable(rdmaEniQuota)
}

// GetMinimumAllocatableIPv4 impl
func (n *bceRDMANetResourceSet) GetMinimumAllocatableIPv4() int {
	return n.real.getMinimumAllocatable()
}

// IsPrefixDelegated impl
func (n *bceRDMANetResourceSet) IsPrefixDelegated() bool {
	return false
}

// GetUsedIPWithPrefixes Impl
func (n *bceRDMANetResourceSet) GetUsedIPWithPrefixes() int {
	return 0
}

// FilterAvailableSubnet implements endpoint.DirectEndpointOperation
func (n *bceRDMANetResourceSet) FilterAvailableSubnet(subnets []*bcesync.BorrowedSubnet, minAllocateIPs int) []*bcesync.BorrowedSubnet {
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
func (n *bceRDMANetResourceSet) FilterAvailableSubnetIds(subnetIDs []string, minAllocateIPs int) []*bcesync.BorrowedSubnet {
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

func (n *bceRDMANetResourceSet) GetMaximumBurstableAllocatableIPv4() int {
	quota := n.getRdmaEniQuota()
	if quota == nil {
		return 0
	}
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return 0
	}
	if n.k8sObj.Spec.ENI.BurstableMehrfachENI > 0 {
		return quota.GetMaxIP() - 1
	}
	return 0
}
