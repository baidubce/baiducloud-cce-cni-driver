package privatecloudbase

import (
	"strings"
	"sync"

	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	api2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase/api"
)

type IPPool struct {
	mutex sync.RWMutex
	data  map[string]map[string]*api2.AllocatedIP
}

func NewIPPool() *IPPool {
	return &IPPool{
		data: make(map[string]map[string]*api2.AllocatedIP),
	}
}

func (pool *IPPool) AddIP(ip *api2.AllocatedIP) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	subnetPool, ok := pool.data[ip.Subnet]
	if !ok {
		subnetPool = make(map[string]*api2.AllocatedIP)
		pool.data[ip.Subnet] = subnetPool
	}
	subnetPool[ip.IP] = ip
}

// GetIPByOwner Owner has two values
//
// 1. Node's available IP cache pool node/{nodeName}/{uid}
//
// 2. Fixed IP pod pod/{namespace}/{name}
func (pool *IPPool) GetIPByOwner(subnet, id string) []*api2.AllocatedIP {
	pool.mutex.RLock()
	defer pool.mutex.RUnlock()

	var ipArray []*api2.AllocatedIP

	sbnPool, ok := pool.data[subnet]
	if ok {
		for _, allocatedIp := range sbnPool {
			if strings.HasPrefix(allocatedIp.ID, id) {
				ipArray = append(ipArray, allocatedIp)
			}
		}
	}
	return ipArray
}

func (pool *IPPool) GetIP(subnet, ip string) *api2.AllocatedIP {
	pool.mutex.RLock()
	defer pool.mutex.RUnlock()

	sbnPool, ok := pool.data[subnet]
	if ok {
		return sbnPool[ip]
	}
	return nil
}

func (pool *IPPool) DeleteIP(ip *api2.AllocatedIP) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	subnetPool, ok := pool.data[ip.Subnet]
	if ok {
		delete(subnetPool, ip.IP)
	}
}

func (pool *IPPool) DeleteByIP(subnet, ip string) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	subnetPool, ok := pool.data[subnet]
	if ok {
		delete(subnetPool, ip)
	}
}

func (pool *IPPool) toAllocationMap() map[string]ipamTypes.AllocationMap {
	pool.mutex.RLock()
	defer pool.mutex.RUnlock()

	result := make(map[string]ipamTypes.AllocationMap)
	for sbnID, sbnPool := range pool.data {
		sbnMap, ok := result[sbnID]
		if !ok {
			sbnMap = make(ipamTypes.AllocationMap)
			result[sbnID] = sbnMap
		}

		for ip, allocated := range sbnPool {
			namespace, name := api2.IDToNamespaceName(allocated.ID)
			sbnMap[ip] = ipamTypes.AllocationIP{
				Owner: namespace + "/" + name,
			}
		}
	}
	return result
}
