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
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/limit"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
)

// The following error constants represent the error conditions for
// CreateInterface without additional context embedded in order to make them
// usable for metrics accounting purposes.
const (
	errUnableToDetermineLimits    = "unable to determine limits"
	errUnableToGetSecurityGroups  = "unable to get security groups"
	errUnableToCreateENI          = "unable to create ENI"
	errUnableToAttachENI          = "unable to attach ENI"
	errCrurrentlyCreatingENI      = "The node currently being created by ENI should have only one."
	errUnableToMarkENIForDeletion = "unable to mark ENI for deletion"
	errUnableToFindSubnet         = "unable to find matching subnet"

	retryDuration = 500 * time.Millisecond
	retryTimeout  = 15 * time.Second

	vpcENISubsys = "vpc-eni-node-allocator"
)

type realNodeInf interface {
	calculateLimiter(scopeLog *logrus.Entry) (limit.IPResourceManager, error)
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
	getMaximumAllocatable(capacity *limit.NodeCapacity) int
	getMinimumAllocatable() int

	// direct ip allocation
	allocateIPCrossSubnet(ctx context.Context, sbnID string) ([]*models.PrivateIP, string, error)
	// ReuseIPs is called to reuse the IPs that are not used by any pods
	reuseIPs(ctx context.Context, ips []*models.PrivateIP, Owner string) (string, error)
}

// bceNode represents a Kubernetes node running CCE with an associated
// NetResourceSet custom resource
type bceNode struct {
	// node contains the general purpose fields of a node
	node *ipam.NetResource

	// mutex protects members below this field
	mutex lock.RWMutex

	// enis is the list of ENIs attached to the node indexed by ENI ID.
	// Protected by Node.mutex.
	enis map[string]ccev2.ENI

	// k8sObj is the NetResourceSet custom resource representing the node
	k8sObj *ccev2.NetResourceSet

	// manager is the ecs node manager responsible for this node
	manager *InstancesManager

	// instanceID of the node
	instanceID   string
	instanceType string

	// The node currently being created by ENI should have only one.
	creatingENI *ccev2.ENI

	// limit
	capacity    *limit.NodeCapacity
	limiterLock lock.RWMutex

	// nextCreateENITime not allow create eni before this time
	nextCreateENITime *time.Time

	real realNodeInf

	// whatever the resource version of the eni have been synced
	// this key is eni id, value is the resource version
	expiredVPCVersion map[string]int64

	creatingENINums int

	eventRecorder record.EventRecorder

	log *logrus.Entry
}

// NewNode returns a new Node
func NewNode(node *ipam.NetResource, k8sObj *ccev2.NetResourceSet, manager *InstancesManager) *bceNode {
	// 1. Create a new CCE Node object
	anode := &bceNode{
		node:              node,
		k8sObj:            k8sObj,
		manager:           manager,
		instanceID:        k8sObj.Spec.InstanceID,
		expiredVPCVersion: make(map[string]int64),
		log:               logging.NewSubysLogger(vpcENISubsys).WithField("instanceID", k8sObj.Spec.InstanceID).WithField("nodeName", k8sObj.Name),
		eventRecorder:     k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: vpcENISubsys}),
	}

	// 2. Get isSuportedENI from ccev2.NetResourceSet Label[k8s.LabelIsSupportENI], this Label is setted by nodediscovery determined by metadata api.
	if anode.k8sObj.Spec.ENI != nil {
		switch anode.k8sObj.Spec.ENI.InstanceType {
		case string(metadata.InstanceTypeExBBC):
			anode.real = newBBCNode(anode)
		default:
			anode.real = newBCCNode(anode)
		}
	}

	return anode
}

func (n *bceNode) calculateLimiter() *limit.NodeCapacity {
	n.limiterLock.Lock()
	defer n.limiterLock.Unlock()

	// faset path to claculate limiter
	if isSetLimiter(n) {
		return n.capacity
	}

	// slow path to claculate limiter
	ctx := logfields.NewContext()
	scopedLog := n.log.WithFields(logrus.Fields{
		"nodeName":     n.k8sObj.Name,
		"method":       "calculateLimiter",
		"instanceType": n.k8sObj.Spec.ENI.InstanceType,
	}).WithContext(ctx)

	ipResourceManager, err := n.real.calculateLimiter(scopedLog)
	if err != nil {
		scopedLog.Errorf("Calculate limiter failed: %v", err)
		return n.capacity
	}
	// 3. Override the CCE Node capacity to the CCE Node object
	err = n.overrideENICapacityToNode(ipResourceManager, n.capacity)
	if err != nil {
		scopedLog.Errorf("Override ENI capacity to NetResourceSet failed: %v", err)
		return n.capacity
	}
	scopedLog.WithField("limiter", logfields.Json(n.capacity)).
		Info("override ENI capacity to NetResourceSet success")
	return n.capacity
}

// isSetLimiter returns true if the node has set limiter
// if the node has set limiter, it will return a limiter from the ENI spec
func isSetLimiter(n *bceNode) bool {
	if n.capacity != nil {
		return true
	}
	if n.k8sObj.Labels != nil && n.k8sObj.Labels[k8s.LabelIPResourceCapacitySynced] != "" {
		n.capacity = &limit.NodeCapacity{
			MaxENINum:   n.k8sObj.Spec.ENI.MaxAllocateENI,
			MaxIPPerENI: n.k8sObj.Spec.ENI.MaxIPsPerENI,
		}
		// use customer max ip nums if defiend
		if operatorOption.Config.BCECustomerMaxIP != 0 && operatorOption.Config.BCECustomerMaxIP != n.capacity.MaxIPPerENI {
			return false
		}
		return true
	}
	return false
}

// overrideENICapacityToNode accoding to the Node.limiter field, override the
// capacity of ENI to the Node.k8sObj.Spec
func (n *bceNode) overrideENICapacityToNode(ipResourceManager limit.IPResourceManager, limiter *limit.NodeCapacity) error {
	ctx := logfields.NewContext()
	// todo move capacity to better place in k8s rest api
	err := ipResourceManager.SyncCapacity(ctx)
	if err != nil {
		return fmt.Errorf("sync capacity failed: %v", err)
	}
	// update the node until 30s timeout
	// if update operation return error, we will get the leatest version of node and try again
	err = wait.PollImmediate(200*time.Millisecond, 30*time.Second, func() (bool, error) {
		k8sObj, err := k8s.CCEClient().CceV2().NetResourceSets().Get(ctx, n.k8sObj.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		k8sObj = k8sObj.DeepCopy()
		k8sObj.Spec.ENI.MaxAllocateENI = limiter.MaxENINum
		k8sObj.Spec.ENI.MaxIPsPerENI = limiter.MaxIPPerENI
		if k8sObj.Labels == nil {
			k8sObj.Labels = map[string]string{}
		}
		k8sObj.Labels[k8s.LabelIPResourceCapacitySynced] = "true"
		updated, err := k8s.CCEClient().CceV2().NetResourceSets().Update(ctx, k8sObj, metav1.UpdateOptions{})
		if err == nil {
			k8sObj = updated
			n.k8sObj = k8sObj
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}

// UpdatedNode is called when an update to the NetResourceSet is received.
func (n *bceNode) UpdatedNode(obj *ccev2.NetResourceSet) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.k8sObj = obj
	n.instanceID = obj.Spec.InstanceID
}

// ResyncInterfacesAndIPs is called to synchronize the latest list of
// interfaces and IPs associated with the node. This function is called
// sparingly as this information is kept in sync based on the success
// of the functions AllocateIPs(), ReleaseIPs() and CreateInterface().
func (n *bceNode) ResyncInterfacesAndIPs(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
	var (
		a = ipamTypes.AllocationMap{}
	)
	n.waitForENISynced(ctx)

	n.mutex.RLock()
	defer n.mutex.RUnlock()

	availabelENIsNumber := 0
	usedENIsNumber := 0
	haveCreatingENI := false
	n.manager.ForeachInstance(n.instanceID,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}

			// eni is not ready to use
			eniSubnet := e.Spec.ENI.SubnetID
			if e.Status.VPCStatus != ccev2.VPCENIStatusInuse {
				haveCreatingENI = true
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
					if e.Spec.UseMode != ccev2.ENIUseModePrimaryIP && addr.Primary {
						continue
					}
					// filter cross subnet private ip
					if addr.SubnetID != eniSubnet {
						continue
					}
					a[addr.PrivateIPAddress] = ipamTypes.AllocationIP{
						Resource: e.Name,
						SubnetID: addr.SubnetID,
					}
				}
			}

			addAllocationIP(e.Spec.ENI.PrivateIPSet)
			addAllocationIP(e.Spec.ENI.IPV6PrivateIPSet)

			return nil
		})

	// the 	CreateInterface function will not be called in the primary IP mode
	// so we need to create a new ENI in the primary IP mode at this time
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) &&
		availabelENIsNumber == usedENIsNumber && !haveCreatingENI {
		go func() {
			_, _, err := n.CreateInterface(ctx, nil, scopedLog)
			scopedLog.WithError(err).Debugf("try to create new ENI for primary IP mode")
		}()
	}
	return a, nil
}

func (n *bceNode) alignDualStackIPs(ctx context.Context, scopedLog *logrus.Entry, e *eniResource) {
	if operatorOption.Config.EnableIPv6 && operatorOption.Config.EnableIPv4 {
		if len(e.Spec.ENI.PrivateIPSet) != len(e.Spec.ENI.IPV6PrivateIPSet) {
			scopedLog.Warnf("eni %s ipv4 and ipv6 private ip number not equal", e.Name)
			a, err := n.PrepareIPAllocation(scopedLog)
			if err != nil {
				scopedLog.WithError(err).Errorf("prepare ip allocation failed")
			}
			err = n.AllocateIPs(ctx, a)
			if err != nil {
				scopedLog.WithError(err).Errorf("allocate ip failed")
			}
		}
	}
}

// CreateInterface create a new ENI
func (n *bceNode) CreateInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	availableENICount := 0
	n.manager.ForeachInstance(n.instanceID,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			_, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			availableENICount++
			return nil
		})

	n.mutex.RLock()
	if n.creatingENINums > 0 {
		n.mutex.RUnlock()
		return 0, "", fmt.Errorf("eni [%d] be creating", n.creatingENINums)
	}
	n.mutex.RUnlock()

	n.addCreatingENINumbers(1)
	inums, msg, err := n.real.createInterface(ctx, allocation, scopedLog)
	n.addCreatingENINumbers(-1)

	preAllocateENI := n.k8sObj.Spec.ENI.PreAllocateENI - availableENICount - 1
	for i := 0; i < preAllocateENI; i++ {
		inums++
		go func() {
			n.addCreatingENINumbers(1)
			_, _, e := n.real.createInterface(ctx, allocation, scopedLog)
			n.addCreatingENINumbers(-1)
			if e == nil {
				scopedLog.Infof("create addition interface success")
				return
			}
			scopedLog.Errorf("create addition interface failed, err: %s", e)
		}()

	}
	return inums, msg, err
}

func (n *bceNode) addCreatingENINumbers(num int) {
	n.mutex.Lock()
	n.creatingENINums += num
	n.mutex.Unlock()
}

// PopulateStatusFields is called to give the implementation a chance
// to populate any implementation specific fields in NetResourceSet.Status.
func (n *bceNode) PopulateStatusFields(resource *ccev2.NetResourceSet) {
	eniStatusMap := make(map[string]ccev2.SimpleENIStatus)
	crossSubnetUsed := make(ipamTypes.AllocationMap, 0)
	// Select only the ENI of the local node
	selector, _ := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelInstanceID: n.instanceID,
	}))
	enis, err := n.manager.enilister.List(selector)
	if err != nil {
		return
	}

	for i := 0; i < len(enis); i++ {
		simple := ccev2.SimpleENIStatus{
			ID:        enis[i].Spec.ENI.ID,
			VPCStatus: string(enis[i].Status.VPCStatus),
			CCEStatus: string(enis[i].Status.CCEStatus),
		}
		eniStatusMap[enis[i].Name] = simple

		var addCrossSubnetAddr = func(addr *models.PrivateIP) {
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
			}
		}

		for _, addr := range enis[i].Spec.PrivateIPSet {
			addCrossSubnetAddr(addr)
		}
		for _, addr := range enis[i].Spec.IPV6PrivateIPSet {
			addCrossSubnetAddr(addr)
		}
	}
	resource.Status.ENIs = eniStatusMap
	resource.Status.IPAM.CrossSubnetUsed = crossSubnetUsed
}

// PrepareIPAllocation is called to calculate the number of IPs that
// can be allocated on the node and whether a new network interface
// must be attached to the node.
func (n *bceNode) PrepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	n.calculateLimiter()
	return n.real.prepareIPAllocation(scopedLog)
}

// AllocateIPs is called after invoking PrepareIPAllocation and needs
// to perform the actual allocation.
func (n *bceNode) AllocateIPs(ctx context.Context, allocation *ipam.AllocationAction) error {
	limiter := n.calculateLimiter()
	if limiter == nil {
		return fmt.Errorf("limiter is nil")
	}
	effectiveLimits := limiter.MaxIPPerENI

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
			ipv4ToAllocate = math.IntMin(allocation.AvailableForAllocationIPv4, effectiveLimits-len(eni.Spec.ENI.PrivateIPSet))
		}
		if operatorOption.Config.EnableIPv6 {
			// ipv6 address should not be more than ipv4 address
			ipv6ToAllocate = math.IntMin(allocation.AvailableForAllocationIPv6, effectiveLimits-len(eni.Spec.ENI.IPV6PrivateIPSet))
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
				allocateLog.WithError(err).Error("failed to allocate ips from eni")
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
func (n *bceNode) PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ipam.ReleaseAction {
	r := &ipam.ReleaseAction{}
	n.mutex.Lock()
	defer n.mutex.Unlock()

	// Needed for selecting the same ENI to release IPs from
	// when more than one ENI qualifies for release
	n.manager.ForeachInstance(n.instanceID,
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
				_, ipUsed := n.k8sObj.Status.IPAM.Used[ip]
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
func (n *bceNode) ReleaseIPs(ctx context.Context, release *ipam.ReleaseAction) error {
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
		for _, ipToRelease := range toReleaseSet {
			scopeLog = scopeLog.WithFields(logrus.Fields{
				"partialToRelease": ipToRelease,
			})
			if isIPv6 {
				err = n.real.releaseIPs(ctx, release, []string{}, ipToRelease)
			} else {
				err = n.real.releaseIPs(ctx, release, ipToRelease, []string{})
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

func (n *bceNode) updateENIWithPoll(ctx context.Context, eni *ccev2.ENI, refresh func(eni *ccev2.ENI) *ccev2.ENI) error {
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
		if errors.IsConflict(ierr) || errors.IsResourceExpired(ierr) {
			return false, nil
		}
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
func (n *bceNode) GetMaximumAllocatableIPv4() int {
	capacity := n.calculateLimiter()
	if capacity == nil {
		return 0
	}
	return n.real.getMaximumAllocatable(capacity)
}

// GetMinimumAllocatableIPv4 impl
func (n *bceNode) GetMinimumAllocatableIPv4() int {
	return n.real.getMinimumAllocatable()
}

// IsPrefixDelegated impl
func (n *bceNode) IsPrefixDelegated() bool {
	return false
}

// GetUsedIPWithPrefixes Impl
func (n *bceNode) GetUsedIPWithPrefixes() int {
	return 0
}

// FilterAvailableSubnet implements endpoint.DirectEndpointOperation
func (n *bceNode) FilterAvailableSubnet(subnets []*ccev1.Subnet) []*ccev1.Subnet {
	contry := operatorOption.Config.BCECloudContry
	region := operatorOption.Config.BCECloudRegion
	zone := n.k8sObj.Spec.ENI.AvailabilityZone

	var filtedSubnets []*ccev1.Subnet
	for _, subnet := range subnets {
		if strings.Contains(subnet.Spec.AvailabilityZone, api.TransAvailableZoneToZoneName(contry, region, zone)) {
			filtedSubnets = append(filtedSubnets, subnet)
		}
	}
	return filtedSubnets
}

// AllocateIP implements endpoint.DirectEndpointOperation
func (n *bceNode) AllocateIP(ctx context.Context, action *endpoint.DirectIPAction) error {
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
	} else {
		// convert AddressPair to PrivateIP
		for _, addressPair := range action.Addressing {
			ipset = append(ipset, &models.PrivateIP{
				PrivateIPAddress: addressPair.IP,
				Primary:          false,
				SubnetID:         action.SubnetID,
			})
		}
		interfaceID, err = n.real.reuseIPs(ctx, ipset, action.Owner)
	}
	if err != nil {
		return err
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
func (n *bceNode) DeleteIP(ctx context.Context, allocation *endpoint.DirectIPAction) error {
	var (
		action *ipam.ReleaseAction = &ipam.ReleaseAction{
			PoolID: ipamTypes.PoolID(allocation.SubnetID),
		}
	)

	for _, addressPair := range allocation.Addressing {
		if addressPair.Interface != "" {
			action.InterfaceID = addressPair.Interface
		}
		action.IPsToRelease = append(action.IPsToRelease, addressPair.IP)
	}
	return n.ReleaseIPs(ctx, action)
}

var (
	_ endpoint.DirectEndpointOperation = &bceNode{}
)
