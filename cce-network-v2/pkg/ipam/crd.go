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
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/trigger"
)

var (
	sharedNodeStore *nodeStore
	initNodeStore   sync.Once
)

const (
	// customResourceUpdateRate is the maximum rate in which a custom
	// resource is updated
	customResourceUpdateRate = 15 * time.Second

	fieldName = "name"
)

// nodeStore represents a NetResourceSet custom resource and binds the CR to a list
// of allocators
type nodeStore struct {
	// mutex protects access to all members of this struct
	mutex lock.RWMutex

	// ownNode is the last known version of the own node resource
	ownNode *ccev2.NetResourceSet

	// allocators is a list of allocators tied to this custom resource
	allocators []*crdAllocator

	// refreshTrigger is the configured trigger to synchronize updates to
	// the custom resource with rate limiting
	refreshTrigger *trigger.Trigger

	// allocationPoolSize is the size of the IP pool for each address
	// family
	allocationPoolSize map[Family]int

	// signal for completion of restoration
	restoreFinished  chan struct{}
	restoreCloseOnce sync.Once

	conf      Configuration
	mtuConfig MtuConfiguration
}

// newNodeStore initializes a new store which reflects the NetResourceSet custom
// resource of the specified node name
func newNodeStore(nodeName string, conf Configuration, owner Owner, k8sEventReg K8sEventRegister, mtuConfig MtuConfiguration) *nodeStore {
	log.WithField(fieldName, nodeName).Info("Subscribed to NetResourceSet custom resource")

	store := &nodeStore{
		allocators:         []*crdAllocator{},
		allocationPoolSize: map[Family]int{},
		conf:               conf,
		mtuConfig:          mtuConfig,
	}
	store.restoreFinished = make(chan struct{})
	cceClient := k8s.CCEClient()

	t, err := trigger.NewTrigger(trigger.Parameters{
		Name:        "crd-allocator-node-refresher",
		MinInterval: customResourceUpdateRate,
		TriggerFunc: store.refreshNodeTrigger,
	})
	if err != nil {
		log.WithError(err).Fatal("Unable to initialize NetResourceSet synchronization trigger")
	}
	store.refreshTrigger = t

	// Create the NetResourceSet custom resource. This call will block until
	// the custom resource has been created
	owner.UpdateNetResourceSetResource()
	apiGroup := "cce.baidubce.com/v2::NetResourceSet"
	netResourceSetSelector := fields.ParseSelectorOrDie("metadata.name=" + nodeName)
	netResourceSetStore := cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc)
	netResourceSetInformer := informer.NewInformerWithStore(
		cache.NewListWatchFromClient(cceClient.CceV2().RESTClient(),
			ccev2.NRSPluralName, corev1.NamespaceAll, netResourceSetSelector),
		&ccev2.NetResourceSet{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() { k8sEventReg.K8sEventReceived(apiGroup, "NetResourceSet", "create", valid, equal) }()
				if node, ok := obj.(*ccev2.NetResourceSet); ok {
					valid = true
					store.updateLocalNodeResource(node.DeepCopy())
					k8sEventReg.K8sEventProcessed("NetResourceSet", "create", true)
				} else {
					log.Warningf("Unknown NetResourceSet object type %s received: %+v", reflect.TypeOf(obj), obj)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				var valid, equal bool
				defer func() { k8sEventReg.K8sEventReceived(apiGroup, "NetResourceSet", "update", valid, equal) }()
				if oldNode, ok := oldObj.(*ccev2.NetResourceSet); ok {
					if newNode, ok := newObj.(*ccev2.NetResourceSet); ok {
						valid = true
						newNode = newNode.DeepCopy()
						store.updateLocalNodeResource(newNode)
						k8sEventReg.K8sEventProcessed("NetResourceSet", "update", true)
					} else {
						log.Warningf("Unknown NetResourceSet object type %T received: %+v", oldNode, oldNode)
					}
				} else {
					log.Warningf("Unknown NetResourceSet object type %T received: %+v", oldNode, oldNode)
				}
			},
			DeleteFunc: func(obj interface{}) {
				// Given we are watching a single specific
				// resource using the node name, any delete
				// notification means that the resource
				// matching the local node name has been
				// removed. No attempt to cast is required.
				store.deleteLocalNodeResource()
				k8sEventReg.K8sEventProcessed("NetResourceSet", "delete", true)
				k8sEventReg.K8sEventReceived(apiGroup, "NetResourceSet", "delete", true, false)
			},
		},
		nil,
		netResourceSetStore,
	)

	go netResourceSetInformer.Run(wait.NeverStop)

	log.WithField(fieldName, nodeName).Info("Waiting for NetResourceSet custom resource to become available...")
	if ok := cache.WaitForCacheSync(wait.NeverStop, netResourceSetInformer.HasSynced); !ok {
		log.WithField(fieldName, nodeName).Fatal("Unable to synchronize NetResourceSet custom resource")
	} else {
		log.WithField(fieldName, nodeName).Info("Successfully synchronized NetResourceSet custom resource")
	}

	for {
		minimumReached, required, numAvailable := store.hasMinimumIPsInPool()
		logFields := logrus.Fields{
			fieldName:   nodeName,
			"required":  required,
			"available": numAvailable,
		}
		if minimumReached {
			log.WithFields(logFields).Info("All required IPs are available in CRD-backed allocation pool")
			break
		}

		log.WithFields(logFields).WithField(
			logfields.HelpMessage,
			"Check if cce-network-operator pod is running and does not have any warnings or error messages.",
		).Info("Waiting for IPs to become available in CRD-backed allocation pool")
		time.Sleep(5 * time.Second)
	}

	go func() {
		// Initial upstream sync must wait for the allocated IPs
		// to be restored
		<-store.restoreFinished
		store.refreshTrigger.TriggerWithReason("initial sync")
	}()

	return store
}

func deriveVpcCIDRs(node *ccev2.NetResourceSet) (primaryCIDR *cidr.CIDR, secondaryCIDRs []*cidr.CIDR) {
	// A node belongs to a single VPC so we can pick the first ENI
	// in the list and derive the VPC CIDR from it.

	return
}

func (n *nodeStore) autoDetectIPv4NativeRoutingCIDR() bool {
	if primaryCIDR, secondaryCIDRs := deriveVpcCIDRs(n.ownNode); primaryCIDR != nil {
		allCIDRs := append([]*cidr.CIDR{primaryCIDR}, secondaryCIDRs...)
		if nativeCIDR := n.conf.GetIPv4NativeRoutingCIDR(); nativeCIDR != nil {
			found := false
			for _, vpcCIDR := range allCIDRs {
				logFields := logrus.Fields{
					"vpc-cidr":                   vpcCIDR.String(),
					option.IPv4NativeRoutingCIDR: nativeCIDR.String(),
				}

				ranges4, _ := ip.CoalesceCIDRs([]*net.IPNet{nativeCIDR.IPNet, vpcCIDR.IPNet})
				if len(ranges4) != 1 {
					log.WithFields(logFields).Info("Native routing CIDR does not contain VPC CIDR, trying next")
				} else {
					found = true
					log.WithFields(logFields).Info("Native routing CIDR contains VPC CIDR, ignoring autodetected VPC CIDRs.")
					break
				}
			}
			if !found {
				log.Fatal("None of the VPC CIDRs contains the specified native routing CIDR")
			}
		} else {
			log.WithFields(logrus.Fields{
				"vpc-cidr": primaryCIDR.String(),
			}).Info("Using autodetected primary VPC CIDR.")
			n.conf.SetIPv4NativeRoutingCIDR(primaryCIDR)
		}
		return true
	} else {
		log.Info("Could not determine VPC CIDRs")
		return false
	}
}

// hasMinimumIPsInPool returns true if the required number of IPs is available
// in the allocation pool. It also returns the number of IPs required and
// available.
func (n *nodeStore) hasMinimumIPsInPool() (minimumReached bool, required, numAvailable int) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	if n.ownNode == nil {
		return
	}

	switch {
	case n.ownNode.Spec.IPAM.MinAllocate != 0:
		required = n.ownNode.Spec.IPAM.MinAllocate
	case n.ownNode.Spec.IPAM.PreAllocate != 0:
		required = n.ownNode.Spec.IPAM.PreAllocate
	default:
		required = 1
	}

	if n.ownNode.Spec.IPAM.Pool != nil {
		for ip := range n.ownNode.Spec.IPAM.Pool {
			if !n.isIPInReleaseHandshake(ip) {
				numAvailable++
			}
		}
		if len(n.ownNode.Spec.IPAM.Pool) >= required {
			minimumReached = true
		}
	}

	return
}

// deleteLocalNodeResource is called when the NetResourceSet resource representing
// the local node has been deleted.
func (n *nodeStore) deleteLocalNodeResource() {
	n.mutex.Lock()
	n.ownNode = nil
	n.mutex.Unlock()
}

// updateLocalNodeResource is called when the NetResourceSet resource representing
// the local node has been added or updated. It updates the available IPs based
// on the custom resource passed into the function.
func (n *nodeStore) updateLocalNodeResource(node *ccev2.NetResourceSet) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	n.ownNode = node
	n.allocationPoolSize[IPv4] = 0
	n.allocationPoolSize[IPv6] = 0
	if node.Spec.IPAM.Pool != nil {
		for ipString := range node.Spec.IPAM.Pool {
			if ip := net.ParseIP(ipString); ip != nil {
				if ip.To4() != nil {
					n.allocationPoolSize[IPv4]++
				} else {
					n.allocationPoolSize[IPv6]++
				}
			}
		}
	}

	releaseUpstreamSyncNeeded := false
	// ACK or NACK IPs marked for release by the operator
	for ip, status := range n.ownNode.Status.IPAM.ReleaseIPs {
		if n.ownNode.Spec.IPAM.Pool == nil {
			continue
		}
		// Ignore states that agent previously responded to.
		if status == ipamOption.IPAMReadyForRelease || status == ipamOption.IPAMDoNotRelease {
			continue
		}
		if _, ok := n.ownNode.Spec.IPAM.Pool[ip]; !ok {
			if status == ipamOption.IPAMReleased {
				// Remove entry from release-ips only when it is removed from .spec.ipam.pool as well
				delete(n.ownNode.Status.IPAM.ReleaseIPs, ip)
				releaseUpstreamSyncNeeded = true

				// Remove the unreachable route for this IP
				if n.conf.UnreachableRoutesEnabled() {
					parsedIP := net.ParseIP(ip)
					if parsedIP == nil {
						// Unable to parse IP, no point in trying to remove the route
						log.Warningf("Unable to parse IP %s", ip)
						continue
					}

					err := netlink.RouteDel(&netlink.Route{
						Dst:   &net.IPNet{IP: parsedIP, Mask: net.CIDRMask(32, 32)},
						Table: unix.RT_TABLE_MAIN,
						Type:  unix.RTN_UNREACHABLE,
					})
					if err != nil && !errors.Is(err, unix.ESRCH) {
						// We ignore ESRCH, as it means the entry was already deleted
						log.WithError(err).Warningf("Unable to delete unreachable route for IP %s", ip)
						continue
					}
				}
			} else if status == ipamOption.IPAMMarkForRelease {
				// NACK the IP, if this node doesn't own the IP
				n.ownNode.Status.IPAM.ReleaseIPs[ip] = ipamOption.IPAMDoNotRelease
				releaseUpstreamSyncNeeded = true
			}
			continue
		}

		// Ignore all other states, transition to do-not-release and ready-for-release are allowed only from
		// marked-for-release
		if status != ipamOption.IPAMMarkForRelease {
			continue
		}
		// Retrieve the appropriate allocator
		var allocator *crdAllocator
		var ipFamily Family
		if ipAddr := net.ParseIP(ip); ipAddr != nil {
			ipFamily = DeriveFamily(ipAddr)
		}
		if ipFamily == "" {
			continue
		}
		for _, a := range n.allocators {
			if a.family == ipFamily {
				allocator = a
			}
		}
		if allocator == nil {
			continue
		}

		// Some functions like crdAllocator.Allocate() acquire lock on allocator first and then on nodeStore.
		// So release nodestore lock before acquiring allocator lock to avoid potential deadlocks from inconsistent
		// lock ordering.
		n.mutex.Unlock()
		allocator.mutex.Lock()
		_, ok := allocator.allocated[ip]
		allocator.mutex.Unlock()
		n.mutex.Lock()

		if ok {
			// IP still in use, update the operator to stop releasing the IP.
			n.ownNode.Status.IPAM.ReleaseIPs[ip] = ipamOption.IPAMDoNotRelease
		} else {
			n.ownNode.Status.IPAM.ReleaseIPs[ip] = ipamOption.IPAMReadyForRelease
		}
		releaseUpstreamSyncNeeded = true
	}

	if releaseUpstreamSyncNeeded {
		n.refreshTrigger.TriggerWithReason("excess IP release")
	}
}

// setOwnNodeWithoutPoolUpdate overwrites the local node copy (e.g. to update
// its resourceVersion) without updating the available IP pool.
func (n *nodeStore) setOwnNodeWithoutPoolUpdate(node *ccev2.NetResourceSet) {
	n.mutex.Lock()
	n.ownNode = node
	n.mutex.Unlock()
}

// refreshNodeTrigger is called to refresh the custom resource after taking the
// configured rate limiting into account
//
// Note: The function signature includes the reasons argument in order to
// implement the trigger.TriggerFunc interface despite the argument being
// unused.
func (n *nodeStore) refreshNodeTrigger(reasons []string) {
	if err := n.refreshNode(); err != nil {
		log.WithError(err).Warning("Unable to update NetResourceSet custom resource")
		n.refreshTrigger.TriggerWithReason("retry after error")
	}
}

// refreshNode updates the custom resource in the apiserver based on the latest
// information in the local node store
func (n *nodeStore) refreshNode() error {
	n.mutex.RLock()
	if n.ownNode == nil {
		n.mutex.RUnlock()
		return nil
	}

	node := n.ownNode.DeepCopy()
	staleCopyOfAllocators := make([]*crdAllocator, len(n.allocators))
	copy(staleCopyOfAllocators, n.allocators)
	n.mutex.RUnlock()

	node.Status.IPAM.Used = ipamTypes.AllocationMap{}

	for _, a := range staleCopyOfAllocators {
		a.mutex.RLock()
		for ip, ipInfo := range a.allocated {
			node.Status.IPAM.Used[ip] = ipInfo
		}
		a.mutex.RUnlock()
	}

	var err error
	cceClient := k8s.CCEClient()
	_, err = cceClient.CceV2().NetResourceSets().UpdateStatus(context.TODO(), node, metav1.UpdateOptions{})

	return err
}

// addAllocator adds a new CRD allocator to the node store
func (n *nodeStore) addAllocator(allocator *crdAllocator) {
	n.mutex.Lock()
	n.allocators = append(n.allocators, allocator)
	n.mutex.Unlock()
}

// allocate checks if a particular IP can be allocated or return an error
func (n *nodeStore) allocate(ip net.IP) (*ipamTypes.AllocationIP, error) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	if n.ownNode == nil {
		return nil, fmt.Errorf("NetResourceSet for own node is not available")
	}

	if n.ownNode.Spec.IPAM.Pool == nil {
		return nil, fmt.Errorf("No IPs available")
	}

	if n.isIPInReleaseHandshake(ip.String()) {
		return nil, fmt.Errorf("IP not available, marked or ready for release")
	}

	ipInfo, ok := n.ownNode.Spec.IPAM.Pool[ip.String()]
	if !ok {
		return nil, NewIPNotAvailableInPoolError(ip)
	}

	return &ipInfo, nil
}

// isIPInReleaseHandshake validates if a given IP is currently in the process of being released
func (n *nodeStore) isIPInReleaseHandshake(ip string) bool {
	if n.ownNode.Status.IPAM.ReleaseIPs == nil {
		return false
	}
	if status, ok := n.ownNode.Status.IPAM.ReleaseIPs[ip]; ok {
		if status == ipamOption.IPAMMarkForRelease || status == ipamOption.IPAMReadyForRelease || status == ipamOption.IPAMReleased {
			return true
		}
	}
	return false
}

// allocateNext allocates the next available IP or returns an error
func (n *nodeStore) allocateNext(allocated ipamTypes.AllocationMap, family Family) (net.IP, *ipamTypes.AllocationIP, error) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	if n.ownNode == nil {
		return nil, nil, fmt.Errorf("NetResourceSet for own node is not available")
	}

	// FIXME: This is currently using a brute-force method that can be
	// optimized
	for ip, ipInfo := range n.ownNode.Spec.IPAM.Pool {
		if _, ok := allocated[ip]; !ok {

			if n.isIPInReleaseHandshake(ip) {
				continue // IP not available
			}
			parsedIP := net.ParseIP(ip)
			if parsedIP == nil {
				log.WithFields(logrus.Fields{
					fieldName: n.ownNode.Name,
					"ip":      ip,
				}).Warning("Unable to parse IP in NetResourceSet custom resource")
				continue
			}

			if DeriveFamily(parsedIP) != family {
				continue
			}

			return parsedIP, &ipInfo, nil
		}
	}

	return nil, nil, fmt.Errorf("No more IPs available")
}

// crdAllocator implements the CRD-backed IP allocator
type crdAllocator struct {
	// store is the node store backing the custom resource
	store *nodeStore

	// mutex protects access to the allocated map
	mutex lock.RWMutex

	// allocated is a map of all allocated IPs indexed by the allocated IP
	// represented as string
	allocated ipamTypes.AllocationMap

	// family is the address family this allocator is allocator for
	family Family

	conf Configuration

	// subnet
}

// newCRDAllocator creates a new CRD-backed IP allocator
func newCRDAllocator(family Family, c Configuration, owner Owner, k8sEventReg K8sEventRegister, mtuConfig MtuConfiguration) Allocator {
	initNodeStore.Do(func() {
		sharedNodeStore = newNodeStore(nodeTypes.GetName(), c, owner, k8sEventReg, mtuConfig)
	})

	allocator := &crdAllocator{
		allocated: ipamTypes.AllocationMap{},
		family:    family,
		store:     sharedNodeStore,
		conf:      c,
	}

	sharedNodeStore.addAllocator(allocator)

	return allocator
}

// deriveGatewayIP accept the CIDR and the index of the IP in this CIDR.
func deriveGatewayIP(cidr string, index int) string {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		log.WithError(err).Warningf("Unable to parse subnet CIDR %s", cidr)
		return ""
	}
	gw := ip.GetIPAtIndex(*ipNet, int64(index))
	if gw == nil {
		return ""
	}
	return gw.String()
}

func (a *crdAllocator) buildAllocationResult(ip net.IP, ipInfo *ipamTypes.AllocationIP) (result *AllocationResult, err error) {
	result = &AllocationResult{IP: ip}

	switch a.conf.IPAMMode() {
	case ipamOption.IPAMVpcEni:
		if ipInfo.SubnetID == "" {
			err = fmt.Errorf("subnet ID is for %s in %s empty", ip.String(), ipInfo.Resource)
			return
		}
		var sbn *ccev1.Subnet
		sbn, err = k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(ipInfo.SubnetID)
		if err != nil {
			err = fmt.Errorf("failed to get subnet %s: %v", ipInfo.SubnetID, err)
			return
		}
		cidr := sbn.Spec.CIDR
		if ip.To4() == nil {
			cidr = sbn.Spec.IPv6CIDR
		}
		result.GatewayIP = deriveGatewayIP(cidr, 1)
		result.CIDRs = []string{cidr}

	// In ENI mode, the Resource points to the ENI so we can derive the
	// master interface and all CIDRs of the VPC
	case ipamOption.IPAMPrivateCloudBase:
		a.store.mutex.RLock()
		defer a.store.mutex.RUnlock()

		if a.store.ownNode == nil {
			return
		}
		if a.store.ownNode.Status.PrivateCloudSubnet != nil {
			sbn := a.store.ownNode.Status.PrivateCloudSubnet
			result.GatewayIP = sbn.Gateway
			result.CIDRs = []string{sbn.CIDR}
		} else {
			if a.family == IPv6 {
				result.GatewayIP = node.GetIPv6().String()
				result.CIDRs = []string{node.GetIPv6AllocRange().String()}
			} else {
				result.GatewayIP = node.GetIPv4().String()
				result.CIDRs = []string{node.GetIPv4AllocRange().String()}
			}
		}
	}

	return
}

// Allocate will attempt to find the specified IP in the custom resource and
// allocate it if it is available. If the IP is unavailable or already
// allocated, an error is returned. The custom resource will be updated to
// reflect the newly allocated IP.
func (a *crdAllocator) Allocate(ip net.IP, owner string) (*AllocationResult, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if _, ok := a.allocated[ip.String()]; ok {
		return nil, fmt.Errorf("IP already in use")
	}

	ipInfo, err := a.store.allocate(ip)
	if err != nil {
		return nil, err
	}

	result, err := a.buildAllocationResult(ip, ipInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to associate IP %s inside NetResourceSet: %w", ip, err)
	}

	a.markAllocated(ip, owner, *ipInfo)
	// Update custom resource to reflect the newly allocated IP.
	a.store.refreshTrigger.TriggerWithReason(fmt.Sprintf("allocation of IP %s", ip.String()))

	return result, nil
}

// AllocateWithoutSyncUpstream will attempt to find the specified IP in the
// custom resource and allocate it if it is available. If the IP is
// unavailable or already allocated, an error is returned. The custom resource
// will not be updated.
func (a *crdAllocator) AllocateWithoutSyncUpstream(ip net.IP, owner string) (*AllocationResult, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if _, ok := a.allocated[ip.String()]; ok {
		return nil, fmt.Errorf("IP already in use")
	}

	ipInfo, err := a.store.allocate(ip)
	if err != nil {
		return nil, err
	}

	result, err := a.buildAllocationResult(ip, ipInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to associate IP %s inside NetResourceSet: %w", ip, err)
	}

	a.markAllocated(ip, owner, *ipInfo)

	return result, nil
}

// Release will release the specified IP or return an error if the IP has not
// been allocated before. The custom resource will be updated to reflect the
// released IP.
func (a *crdAllocator) Release(ip net.IP) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if _, ok := a.allocated[ip.String()]; !ok {
		return fmt.Errorf("IP %s is not allocated", ip.String())
	}

	delete(a.allocated, ip.String())
	// Update custom resource to reflect the newly released IP.
	a.store.refreshTrigger.TriggerWithReason(fmt.Sprintf("release of IP %s", ip.String()))

	return nil
}

// markAllocated marks a particular IP as allocated
func (a *crdAllocator) markAllocated(ip net.IP, owner string, ipInfo ipamTypes.AllocationIP) {
	ipInfo.Owner = owner
	a.allocated[ip.String()] = ipInfo
}

// AllocateNext allocates the next available IP as offered by the custom
// resource or return an error if no IP is available. The custom resource will
// be updated to reflect the newly allocated IP.
func (a *crdAllocator) AllocateNext(owner string) (*AllocationResult, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	ip, ipInfo, err := a.store.allocateNext(a.allocated, a.family)
	if err != nil {
		return nil, err
	}

	result, err := a.buildAllocationResult(ip, ipInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to associate IP %s inside NetResourceSet: %w", ip, err)
	}

	a.markAllocated(ip, owner, *ipInfo)
	// Update custom resource to reflect the newly allocated IP.
	a.store.refreshTrigger.TriggerWithReason(fmt.Sprintf("allocation of IP %s", ip.String()))

	return result, nil
}

// AllocateNextWithoutSyncUpstream allocates the next available IP as offered
// by the custom resource or return an error if no IP is available. The custom
// resource will not be updated.
func (a *crdAllocator) AllocateNextWithoutSyncUpstream(owner string) (*AllocationResult, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	ip, ipInfo, err := a.store.allocateNext(a.allocated, a.family)
	if err != nil {
		return nil, err
	}

	result, err := a.buildAllocationResult(ip, ipInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to associate IP %s inside NetResourceSet: %w", ip, err)
	}

	a.markAllocated(ip, owner, *ipInfo)

	return result, nil
}

// totalPoolSize returns the total size of the allocation pool
// a.mutex must be held
func (a *crdAllocator) totalPoolSize() int {
	if num, ok := a.store.allocationPoolSize[a.family]; ok {
		return num
	}
	return 0
}

// Dump provides a status report and lists all allocated IP addresses
func (a *crdAllocator) Dump() (map[string]string, string) {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	allocs := map[string]string{}
	for ip := range a.allocated {
		allocs[ip] = ""
	}

	status := fmt.Sprintf("%d/%d allocated", len(allocs), a.totalPoolSize())
	return allocs, status
}

// RestoreFinished marks the status of restoration as done
func (a *crdAllocator) RestoreFinished() {
	a.store.restoreCloseOnce.Do(func() {
		close(a.store.restoreFinished)
	})
}

// NewIPNotAvailableInPoolError returns an error resprenting the given IP not
// being available in the IPAM pool.
func NewIPNotAvailableInPoolError(ip net.IP) error {
	return &ErrIPNotAvailableInPool{ip: ip}
}

// ErrIPNotAvailableInPool represents an error when an IP is not available in
// the pool.
type ErrIPNotAvailableInPool struct {
	ip net.IP
}

func (e *ErrIPNotAvailableInPool) Error() string {
	return fmt.Sprintf("IP %s is not available", e.ip.String())
}

// Is provides this error type with the logic for use with errors.Is.
func (e *ErrIPNotAvailableInPool) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	t, ok := target.(*ErrIPNotAvailableInPool)
	if !ok {
		return ok
	}
	if t == nil {
		return false
	}
	return t.ip.Equal(e.ip)
}
