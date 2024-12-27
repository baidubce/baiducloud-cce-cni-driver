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

package ipam

import (
	"context"
	"fmt"
	"sort"
	"time"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	listerv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/trigger"
)

// NetResourceSetGetterUpdater defines the interface used to interact with the k8s
// apiserver to retrieve and update the NetResourceSet custom resource
type NetResourceSetGetterUpdater interface {
	Create(node *v2.NetResourceSet) (*v2.NetResourceSet, error)
	Update(origResource, newResource *v2.NetResourceSet) (*v2.NetResourceSet, error)
	UpdateStatus(origResource, newResource *v2.NetResourceSet) (*v2.NetResourceSet, error)
	Get(name string) (*v2.NetResourceSet, error)
	Lister() listerv2.NetResourceSetLister
}

// NetResourceOperations is the interface an IPAM implementation must provide in order
// to provide IP allocation for a node. The structure implementing this API
// *must* be aware of the node connected to this implementation. This is
// achieved by considering the node context provided in
// AllocationImplementation.CreateNetResource() function and returning a
// NetResourceOperations implementation which performs operations in the context of
// that node.
type NetResourceOperations interface {
	// UpdateNode is called when an update to the NetResourceSet is received.
	UpdatedNode(obj *v2.NetResourceSet)

	// PopulateStatusFields is called to give the implementation a chance
	// to populate any implementation specific fields in NetResourceSet.Status.
	PopulateStatusFields(resource *v2.NetResourceSet)

	// CreateInterface is called to create a new interface. This is only
	// done if PrepareIPAllocation indicates that no more IPs are available
	// (AllocationAction.AvailableForAllocation == 0) for allocation but
	// interfaces are available for creation
	// (AllocationAction.AvailableInterfaces > 0). This function must
	// create the interface *and* allocate up to
	// AllocationAction.MaxIPsToAllocate.
	CreateInterface(ctx context.Context, allocation *AllocationAction, scopedLog *logrus.Entry) (int, string, error)

	// ResyncInterfacesAndIPs is called to synchronize the latest list of
	// interfaces and IPs associated with the node. This function is called
	// sparingly as this information is kept in sync based on the success
	// of the functions AllocateIPs(), ReleaseIPs() and CreateInterface().
	ResyncInterfacesAndIPs(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error)

	// PrepareIPAllocation is called to calculate the number of IPs that
	// can be allocated on the node and whether a new network interface
	// must be attached to the node.
	PrepareIPAllocation(scopedLog *logrus.Entry) (*AllocationAction, error)

	// AllocateIPs is called after invoking PrepareIPAllocation and needs
	// to perform the actual allocation.
	AllocateIPs(ctx context.Context, allocation *AllocationAction) error

	// PrepareIPRelease is called to calculate whether any IP excess needs
	// to be resolved. It behaves identical to PrepareIPAllocation but
	// indicates a need to release IPs.
	PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ReleaseAction

	// ReleaseIPs is called after invoking PrepareIPRelease and needs to
	// perform the release of IPs.
	ReleaseIPs(ctx context.Context, release *ReleaseAction) error

	// GetMaximumAllocatableIPv4 returns the maximum amount of IPv4 addresses
	// that can be allocated to the instance
	GetMaximumAllocatableIPv4() int

	// GetMinimumAllocatableIPv4 returns the minimum amount of IPv4 addresses that
	// must be allocated to the instance.
	GetMinimumAllocatableIPv4() int

	// IsPrefixDelegated helps identify if a node supports prefix delegation
	IsPrefixDelegated() bool

	// GetUsedIPWithPrefixes returns the total number of used IPs including all IPs in a prefix if at-least one of
	// the prefix IPs is in use.
	GetUsedIPWithPrefixes() int

	// GetMaximumBurstableAllocatableIPv4 returns the maximum amount of IPv4 addresses
	GetMaximumBurstableAllocatableIPv4() int
}

// AllocationImplementation is the interface an implementation must provide.
// Other than NodeOperations, this implementation is not related to a node
// specifically.
type AllocationImplementation interface {
	// CreateNetResource is called when the IPAM layer has learned about a new
	// node which requires IPAM services. This function must return a
	// NodeOperations implementation which will render IPAM services to the
	// node context provided.
	CreateNetResource(obj *v2.NetResourceSet, node *NetResource) NetResourceOperations

	// GetPoolQuota is called to retrieve the remaining IP addresses in all
	// IP pools known to the IPAM implementation.
	GetPoolQuota() ipamTypes.PoolQuotaMap

	// Resync is called periodically to give the IPAM implementation a
	// chance to resync its own state with external APIs or systems. It is
	// also called when the IPAM layer detects that state got out of sync.
	Resync(ctx context.Context) time.Time
}

// MetricsAPI represents the metrics being maintained by a NodeManager
type MetricsAPI interface {
	IncAllocationAttempt(status, subnetID string)
	AddIPAllocation(subnetID string, allocated int64)
	AddIPRelease(subnetID string, released int64)
	SetAllocatedIPs(typ string, allocated int)
	SetAvailableInterfaces(available int)
	SetAvailableIPsPerSubnet(subnetID string, availabilityZone string, available int)
	SetNetResourceSets(category string, nodes int)
	IncResyncCount()
	PoolMaintainerTrigger() trigger.MetricsObserver
	K8sSyncTrigger() trigger.MetricsObserver
	ResyncTrigger() trigger.MetricsObserver
}

// netResourceMap is a mapping of node names to ENI nodes
type netResourceMap map[string]*NetResource

// NetResourceSetManager manages all nodes with ENIs
type NetResourceSetManager struct {
	mutex              lock.RWMutex
	netResources       netResourceMap
	instancesAPI       AllocationImplementation
	k8sAPI             NetResourceSetGetterUpdater
	metricsAPI         MetricsAPI
	resyncTrigger      *trigger.Trigger
	parallelWorkers    int64
	releaseExcessIPs   bool
	stableInstancesAPI bool
	prefixDelegation   bool
}

// ResourceType implements allocator.NetResourceSetEventHandler.
func (n *NetResourceSetManager) ResourceType() string {
	panic("unimplemented")
}

// NewNetResourceSetManager returns a new NodeManager
func NewNetResourceSetManager(instancesAPI AllocationImplementation, k8sAPI NetResourceSetGetterUpdater, metrics MetricsAPI,
	parallelWorkers int64, releaseExcessIPs bool, prefixDelegation bool) (*NetResourceSetManager, error) {
	if parallelWorkers < 1 {
		parallelWorkers = 1
	}

	mngr := &NetResourceSetManager{
		netResources:     netResourceMap{},
		instancesAPI:     instancesAPI,
		k8sAPI:           k8sAPI,
		metricsAPI:       metrics,
		parallelWorkers:  parallelWorkers,
		releaseExcessIPs: releaseExcessIPs,
		prefixDelegation: prefixDelegation,
	}

	resyncName := "ipam-net-resource-set-manager-resync"
	resyncTrigger, err := trigger.NewTrigger(trigger.Parameters{
		Name:             resyncName,
		MinInterval:      10 * time.Millisecond,
		MetricsObserver:  metrics.ResyncTrigger(),
		MaxDelayDuration: 600 * time.Second, // 5 minutes
		Log:              log.WithField("trigger", resyncName),
		TriggerFunc: func(reasons []string) {
			if syncTime, ok := mngr.instancesAPIResync(context.TODO()); ok {
				mngr.Resync(context.TODO(), syncTime)
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to initialize resync trigger: %s", err)
	}

	mngr.resyncTrigger = resyncTrigger
	// Assume readiness, the initial blocking resync in Start() will update
	// the readiness
	mngr.SetInstancesAPIReadiness(true)

	return mngr, nil
}

func (n *NetResourceSetManager) instancesAPIResync(ctx context.Context) (time.Time, bool) {
	syncTime := n.instancesAPI.Resync(ctx)
	success := !syncTime.IsZero()
	n.SetInstancesAPIReadiness(success)
	return syncTime, success
}

// Start kicks of the NodeManager by performing the initial state
// synchronization and starting the background sync go routine
func (n *NetResourceSetManager) Start(ctx context.Context) error {
	// Trigger the initial resync in a blocking manner
	if _, ok := n.instancesAPIResync(ctx); !ok {
		return fmt.Errorf("initial synchronization with instances API failed")
	}

	// Start an interval based  background resync for safety, it will
	// synchronize the state regularly and resolve eventual deficit if the
	// event driven trigger fails, and also release excess IP addresses
	// if release-excess-ips is enabled
	go func() {
		mngr := controller.NewManager()
		mngr.UpdateController("ipam-node-interval-refresh",
			controller.ControllerParams{
				RunInterval: operatorOption.Config.ResourceResyncInterval,
				DoFunc: func(ctx context.Context) error {
					if n.resyncTrigger != nil {
						n.resyncTrigger.TriggerWithReason("ipam-node-interval-refresh")
					}
					return nil
				},
			})
	}()

	return nil
}

// SetInstancesAPIReadiness sets the readiness state of the instances API
func (n *NetResourceSetManager) SetInstancesAPIReadiness(ready bool) {
	n.mutex.Lock()
	n.stableInstancesAPI = ready
	n.mutex.Unlock()
}

// InstancesAPIIsReady returns true if the instances API is stable and ready
func (n *NetResourceSetManager) InstancesAPIIsReady() bool {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return n.stableInstancesAPI
}

// GetNames returns the list of all node names
func (n *NetResourceSetManager) GetNames() (allNetResourceSetNames []string) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	allNetResourceSetNames = make([]string, 0, len(n.netResources))

	for name := range n.netResources {
		allNetResourceSetNames = append(allNetResourceSetNames, name)
	}

	return
}

func (n *NetResourceSetManager) Create(resource *v2.NetResourceSet) error {
	return n.Update(resource)
}

// Update is called whenever a NetResourceSet resource has been updated in the
// Kubernetes apiserver
func (n *NetResourceSetManager) Update(resource *v2.NetResourceSet) error {
	n.mutex.Lock()
	netResource, ok := n.netResources[resource.Name]
	n.mutex.Unlock()

	defer func() {
		netResource.UpdatedResource(resource)
	}()
	if !ok {
		netResource = &NetResource{
			name:                  resource.Name,
			manager:               n,
			ipsMarkedForRelease:   make(map[string]time.Time),
			ipReleaseStatus:       make(map[string]string),
			logLimiter:            logging.NewLimiter(10*time.Second, 3), // 1 log / 10 secs, burst of 3
			lastMaxAdapterWarning: time.Now(),
		}

		netResource.ops = n.instancesAPI.CreateNetResource(resource, netResource)

		maintainerName := fmt.Sprintf("ipam-pool-maintainer-%s", resource.Name)
		poolMaintainer, err := trigger.NewTrigger(trigger.Parameters{
			Name:             maintainerName,
			MinInterval:      10 * time.Millisecond,
			MetricsObserver:  n.metricsAPI.PoolMaintainerTrigger(),
			MaxDelayDuration: operatorOption.Config.ResourceResyncInterval, // 5 minutes
			Log:              netResource.logger().WithField("trigger", maintainerName),
			TriggerFunc: func(reasons []string) {
				if err := netResource.MaintainIPPool(context.TODO()); err != nil {
					netResource.logger().WithError(err).Warning("Unable to maintain ip pool of node")
				}
			},
		})
		if err != nil {
			netResource.logger().WithError(err).Error("Unable to create pool-maintainer trigger")
			return err
		}

		retryName := fmt.Sprintf("ipam-pool-maintainer-%s-retry", resource.Name)
		retry, err := trigger.NewTrigger(trigger.Parameters{
			Name:             retryName,
			MinInterval:      5 * time.Second,                              // large minimal interval to not retry too often
			MaxDelayDuration: operatorOption.Config.ResourceResyncInterval, // 5 minutes
			Log:              netResource.logger().WithField("trigger", retryName),
			TriggerFunc:      func(reasons []string) { poolMaintainer.Trigger() },
		})
		if err != nil {
			netResource.logger().WithError(err).Error("Unable to create pool-maintainer-retry trigger")
			return err
		}
		netResource.retry = retry

		syncName := fmt.Sprintf("ipam-node-k8s-sync-%s", resource.Name)
		k8sSync, err := trigger.NewTrigger(trigger.Parameters{
			Name:             syncName,
			MinInterval:      10 * time.Millisecond,
			MetricsObserver:  n.metricsAPI.K8sSyncTrigger(),
			MaxDelayDuration: 300 * time.Second, // 5 minutes
			Log:              netResource.logger().WithField("trigger", syncName),
			TriggerFunc: func(reasons []string) {
				netResource.syncToAPIServer()
				netResource.SetRunning()
			},
		})
		if err != nil {
			poolMaintainer.Shutdown()
			netResource.logger().WithError(err).Error("Unable to create k8s-sync trigger")
			return err
		}

		netResource.poolMaintainer = poolMaintainer
		netResource.k8sSync = k8sSync

		n.mutex.Lock()
		n.netResources[netResource.name] = netResource
		n.mutex.Unlock()

		log.WithField(fieldName, resource.Name).Info("Discovered new NetResourceSet custom resource")
	}

	return nil
}

// Delete is called after a NetResourceSet resource has been deleted via the
// Kubernetes apiserver
func (n *NetResourceSetManager) Delete(nodeName string) error {
	n.mutex.Lock()

	if node, ok := n.netResources[nodeName]; ok {
		if node.poolMaintainer != nil {
			node.poolMaintainer.Shutdown()
		}
		if node.k8sSync != nil {
			node.k8sSync.Shutdown()
		}
		if node.retry != nil {
			node.retry.Shutdown()
		}
	}

	delete(n.netResources, nodeName)
	n.mutex.Unlock()
	return nil
}

// Get returns the node with the given name
func (n *NetResourceSetManager) Get(nodeName string) *NetResource {
	n.mutex.RLock()
	node := n.netResources[nodeName]
	n.mutex.RUnlock()
	return node
}

// GetNodesByIPWatermark returns all nodes that require addresses to be
// allocated or released, sorted by the number of addresses needed to be operated
// in descending order. Number of addresses to be released is negative value
// so that nodes with IP deficit are resolved first
func (n *NetResourceSetManager) GetNodesByIPWatermark() []*NetResource {
	n.mutex.RLock()
	list := make([]*NetResource, len(n.netResources))
	index := 0
	for _, node := range n.netResources {
		list[index] = node
		index++
	}
	n.mutex.RUnlock()

	sort.Slice(list, func(i, j int) bool {
		valuei := list[i].GetNeededAddresses()
		valuej := list[j].GetNeededAddresses()
		// Number of addresses to be released is negative value,
		// nodes with more excess addresses are released earlier
		if valuei < 0 && valuej < 0 {
			return valuei < valuej
		}
		return valuei > valuej
	})

	return list
}

type resyncStats struct {
	mutex               lock.Mutex
	totalUsed           int
	totalAvailable      int
	totalNeeded         int
	remainingInterfaces int
	nodes               int
	nodesAtCapacity     int
	nodesInDeficit      int
}

func (n *NetResourceSetManager) resyncNode(ctx context.Context, node *NetResource, stats *resyncStats, syncTime time.Time) {
	if node == nil {
		return
	}
	node.SetRunning()
	node.updateLastResync(syncTime)
	node.recalculate()
	allocationNeeded := node.allocationNeeded()
	releaseNeeded := node.releaseNeeded()
	if allocationNeeded || releaseNeeded {
		node.poolMaintainer.Trigger()
	}

	nodeStats := node.Stats()

	stats.mutex.Lock()
	stats.totalUsed += nodeStats.UsedIPs
	availableOnNode := nodeStats.AvailableIPs - nodeStats.UsedIPs
	stats.totalAvailable += availableOnNode
	stats.totalNeeded += nodeStats.NeededIPs
	stats.remainingInterfaces += nodeStats.RemainingInterfaces
	stats.nodes++

	if allocationNeeded {
		stats.nodesInDeficit++
	}

	if nodeStats.RemainingInterfaces == 0 && availableOnNode == 0 {
		stats.nodesAtCapacity++
	}

	stats.mutex.Unlock()

	node.k8sSync.Trigger()
}

// Resync will attend all nodes and resolves IP deficits. The order of
// attendance is defined by the number of IPs needed to reach the configured
// watermarks. Any updates to the node resource are synchronized to the
// Kubernetes apiserver.
func (n *NetResourceSetManager) Resync(ctx context.Context, syncTime time.Time) {
	stats := resyncStats{}
	sem := semaphore.NewWeighted(n.parallelWorkers)

	for _, node := range n.GetNodesByIPWatermark() {
		err := sem.Acquire(ctx, 1)
		if err != nil {
			continue
		}

		ctx, cancelFn := context.WithTimeout(ctx, operatorOption.Config.ResourceResyncInterval)
		resultChan := make(chan struct{})

		go func(node *NetResource, stats *resyncStats) {
			n.resyncNode(ctx, node, stats, syncTime)
			resultChan <- struct{}{}
		}(node, &stats)
		select {
		case <-ctx.Done():
			sem.Release(1)
			log.Warningf("resync nrs %s timeout", node.name)
		case <-resultChan:
			sem.Release(1)
		}
		cancelFn()
	}

	// Acquire the full semaphore, this requires all go routines to
	// complete and thus blocks until all nodes are synced
	sem.Acquire(ctx, n.parallelWorkers)

	n.metricsAPI.IncResyncCount()
	n.metricsAPI.SetAllocatedIPs("used", stats.totalUsed)
	n.metricsAPI.SetAllocatedIPs("available", stats.totalAvailable)
	n.metricsAPI.SetAllocatedIPs("needed", stats.totalNeeded)
	n.metricsAPI.SetAvailableInterfaces(stats.remainingInterfaces)
	n.metricsAPI.SetNetResourceSets("total", stats.nodes)
	n.metricsAPI.SetNetResourceSets("in-deficit", stats.nodesInDeficit)
	n.metricsAPI.SetNetResourceSets("at-capacity", stats.nodesAtCapacity)

	for poolID, quota := range n.instancesAPI.GetPoolQuota() {
		n.metricsAPI.SetAvailableIPsPerSubnet(string(poolID), quota.AvailabilityZone, quota.AvailableIPs)
	}
}
