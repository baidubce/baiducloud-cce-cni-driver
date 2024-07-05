package privatecloudbase

import (
	"context"
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase/api"
	"github.com/sirupsen/logrus"
)

// Node represents a Kubernetes node running CCE with an associated
// NetResourceSet custom resource
type Node struct {
	// node contains the general purpose fields of a node
	node *ipam.NetResource

	// mutex protects members below this field
	mutex lock.RWMutex

	// enis is the list of ENIs attached to the node indexed by ENI ID.
	// Protected by Node.mutex.
	//enis map[string]eniTypes.ENI

	// k8sObj is the NetResourceSet custom resource representing the node
	k8sObj *v2.NetResourceSet

	// manager is the ecs node manager responsible for this node
	manager *InstancesManager

	// instanceID of the node
	instanceID string
	subnetID   string
}

// NewNode returns a new Node
func NewNode(node *ipam.NetResource, k8sObj *v2.NetResourceSet, manager *InstancesManager) *Node {
	return &Node{
		node:       node,
		k8sObj:     k8sObj,
		manager:    manager,
		instanceID: node.InstanceID(),
		subnetID:   GetSubnetIDFromNodeLabels(k8sObj.Labels),
	}
}

func (n *Node) UpdatedNode(obj *v2.NetResourceSet) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.k8sObj = obj
	n.subnetID = GetSubnetIDFromNodeLabels(n.k8sObj.Labels)
}

func (n *Node) PopulateStatusFields(resource *v2.NetResourceSet) {
	status := &resource.Status
	status.PrivateCloudSubnet = n.manager.GetSubnet(n.subnetID)
	if status.PrivateCloudSubnet == nil {
		log.WithField("method", "PopulateStatusFields").Warning("subnet status is nil")
	}
	log.WithField("method", "PopulateStatusFields").WithField("subnet", logfields.Json(status.PrivateCloudSubnet)).Debug("populate subnet status")
}

func (n *Node) CreateInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (int, string, error) {
	return 0, "", nil
}

// ResyncInterfacesAndIPs is called to synchronize the latest list of
// interfaces and IPs associated with the node. This function is called
// sparingly as this information is kept in sync based on the success
// of the functions AllocateIPs(), ReleaseIPs() and CreateInterface().
func (n *Node) ResyncInterfacesAndIPs(ctx context.Context, scopedLog *logrus.Entry) (ipamTypes.AllocationMap, error) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()
	a := ipamTypes.AllocationMap{}
	if n.subnetID == "" {
		return nil, fmt.Errorf("")
	}
	ips := n.manager.GetIPPool().GetIPByOwner(n.subnetID, api.NodePoolID(n.k8sObj.Name, "v1"))
	for _, ip := range ips {
		a[ip.IP] = ipamTypes.AllocationIP{}
	}

	return a, nil
}

func (n *Node) PrepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	if n.subnetID == "" {
		return nil, fmt.Errorf("no subnet label at node %s", n.k8sObj.Name)
	}
	a = &ipam.AllocationAction{}

	a.AvailableForAllocationIPv4 = defaults.IPAMPreAllocation
	a.MaxIPsToAllocate = 1024
	a.PoolID = ipamTypes.PoolID(n.subnetID)
	return
}

func (n *Node) AllocateIPs(ctx context.Context, allocation *ipam.AllocationAction) error {
	req := api.BatchAcquireIPBySubnetRequest{
		ID:     api.NodePoolID(n.k8sObj.Name, "v1"),
		Subnet: n.subnetID,
		Count:  allocation.AvailableForAllocationIPv4,
	}
	allocateList, err := n.manager.api.BatchAcquireIPBySubnet(ctx, req)
	if err != nil {
		return err
	}
	for i := range allocateList.AllocatedIPs {
		n.manager.GetIPPool().AddIP(&allocateList.AllocatedIPs[i])
	}

	return nil
}

func (n *Node) PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ipam.ReleaseAction {
	r := &ipam.ReleaseAction{}

	freeIPs := []string{}
	for ip, _ := range n.k8sObj.Spec.IPAM.Pool {
		_, ipUsed := n.k8sObj.Status.IPAM.Used[ip]
		// exclude primary IPs
		if !ipUsed {
			freeIPs = append(freeIPs, ip)
		}
	}

	freeOnENICount := len(freeIPs)

	if freeOnENICount <= 0 {
		return r
	}
	scopedLog.WithFields(logrus.Fields{
		"instanceId": n.instanceID,
		"excessIPs":  excessIPs,
	}).Debug("instance has unused IPs that can be released")
	maxReleaseOnENI := math.IntMin(freeOnENICount, excessIPs)

	r.PoolID = ipamTypes.PoolID(n.subnetID)
	r.IPsToRelease = freeIPs[:maxReleaseOnENI]
	return r
}

func (n *Node) ReleaseIPs(ctx context.Context, release *ipam.ReleaseAction) error {
	var ipToRelease = make(map[string][]string)
	for _, ip := range release.IPsToRelease {
		allocated := n.manager.IPPool.GetIP(n.subnetID, ip)
		if allocated == nil {
			return fmt.Errorf("can not find allocated ip %s", ip)
		}
		ipToRelease[allocated.VPC] = append(ipToRelease[allocated.VPC], ip)
	}

	for vpc, ips := range ipToRelease {
		err := n.manager.api.BatchReleaseIP(ctx, api.BatchReleaseIPRequest{IPs: ips, VPC: vpc})
		if err != nil {
			log.WithField("ipsCount", len(ipToRelease)).WithError(err).Error("failed to release ips")
			return err
		}
		log.WithField("ipsCount", len(ipToRelease)).Info("release ips success")
	}
	n.mutex.Lock()
	defer n.mutex.Unlock()

	for _, ip := range release.IPsToRelease {
		n.manager.IPPool.DeleteByIP(n.subnetID, ip)
	}
	return nil
}

func (n *Node) GetMaximumAllocatableIPv4() int {
	return 250
}

func (n *Node) GetMinimumAllocatableIPv4() int {

	return defaults.IPAMPreAllocation
}

func (n *Node) IsPrefixDelegated() bool {
	return false
}

func (n *Node) GetUsedIPWithPrefixes() int {
	if n.k8sObj == nil {
		return 0
	}
	return len(n.k8sObj.Status.IPAM.Used)
}

func (n *Node) GetMaximumBurstableAllocatableIPv4() int {
	return 0
}
