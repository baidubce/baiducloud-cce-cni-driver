package privatecloudbase

import (
	"context"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	api2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

var (
	log = logging.DefaultLogger.WithField(logfields.LogSubsys, "pcb-api")
)

// InstancesManager maintains the list of instances. It must be kept up to date
// by calling resync() regularly.
type InstancesManager struct {
	mutex lock.RWMutex

	getterUpdater ipam.NetResourceSetGetterUpdater
	api           api2.Client

	// status of Private Cloud Base
	// index by subnetID and ip
	IPPool *IPPool

	// Fixed IP objects that have been allocated in cache history
	reuseIPMap *IPPool
	nodeMap    map[string]*Node
	subnets    map[string]*api2.Subnet
}

// NewInstancesManager returns a new instances manager
func NewInstancesManager(api api2.Client, getterUpdater ipam.NetResourceSetGetterUpdater) *InstancesManager {
	return &InstancesManager{
		api:           api,
		reuseIPMap:    NewIPPool(),
		IPPool:        NewIPPool(),
		nodeMap:       make(map[string]*Node),
		subnets:       make(map[string]*api2.Subnet),
		getterUpdater: getterUpdater,
	}
}

// CreateNetResource is called when the IPAM layer has learned about a new
// node which requires IPAM services. This function must return a
// NodeOperations implementation which will render IPAM services to the
// node context provided.
func (m *InstancesManager) CreateNetResource(obj *v2.NetResourceSet, node *ipam.NetResource) ipam.NetResourceOperations {
	np := NewNode(node, obj, m)
	m.mutex.Lock()
	m.nodeMap[np.k8sObj.Name] = np
	m.mutex.Unlock()
	return np
}

// GetPoolQuota returns the number of available IPs in all IP pools
func (m *InstancesManager) GetPoolQuota() ipamTypes.PoolQuotaMap {
	pool := ipamTypes.PoolQuotaMap{}
	return pool
}

// Resync is called periodically to give the IPAM implementation a
// chance to resync its own state with external APIs or systems. It is
// also called when the IPAM layer detects that state got out of sync.
func (m *InstancesManager) Resync(ctx context.Context) time.Time {
	resyncStart := time.Now()

	vpcs, err := m.api.AllocatedIPs(ctx)
	if err != nil {
		log.WithError(err).Error("Unable to synchronize VPC list")
		return time.Time{}
	}

	// // release ips when node not exist
	var ipToRelease = make(map[string][]string)

	ippools := NewIPPool()
	podPools := NewIPPool()
	for i := range vpcs.AllocatedIPs {
		allocated := vpcs.AllocatedIPs[i]
		// release ips when node not exist
		nodeName := api2.GetNodeIDName(allocated.ID)
		if nodeName != "" {
			_, err := m.getterUpdater.Lister().Get(nodeName)
			// add to gc list
			if k8serrors.IsNotFound(err) {
				//ipToRelease[allocated.VPC] = append(ipToRelease[allocated.VPC], allocated.IP)
				log.WithError(err).Errorf("cce node %s is not found", nodeName)
			}
		}

		ippools.AddIP(&allocated)
		if api2.ISPodID(allocated.ID) {
			podPools.AddIP(&allocated)
		}
	}

	log.Debug("Synchronized ENI information")

	if len(ipToRelease) != 0 {
		for vpc, ips := range ipToRelease {
			err := m.api.BatchReleaseIP(ctx, api2.BatchReleaseIPRequest{IPs: ips, VPC: vpc})
			if err != nil {
				log.WithField("ipsCount", len(ipToRelease)).WithError(err).Error("failed to gc ips when node deleted")
			}
			log.WithField("ipsCount", len(ipToRelease)).Info("gc ips when node deleted success")
		}
	}

	subnets := make(map[string]*api2.Subnet)
	sbns, err := m.api.ListSubnets(ctx)
	if err == nil {
		for i := range sbns.Subnets {
			subnets[sbns.Subnets[i].Name] = sbns.Subnets[i]
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.IPPool = ippools
	m.reuseIPMap = podPools
	m.subnets = subnets

	return resyncStart
}

func (m *InstancesManager) GetIPPool() *IPPool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.IPPool
}

func (m *InstancesManager) GetPodPool() *IPPool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.reuseIPMap
}

func (m *InstancesManager) GetSubnet(id string) *api2.Subnet {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.subnets[id]
}

var (
	_ ipam.AllocationImplementation = &InstancesManager{}
)
