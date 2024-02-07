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
	"fmt"
	"net"
	"sync"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/allocator"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

type localPool struct {
	sync.Mutex
	localPool map[string]*allocator.SubRangePoolAllocator
}

func newLocalPool() *localPool {
	return &localPool{
		localPool: make(map[string]*allocator.SubRangePoolAllocator),
	}
}

func (e *localPool) updateSubnet(oldObj, newObj interface{}) {
	newSubnet, ok := newObj.(*ccev1.Subnet)
	if !ok {
		return
	}

	poolID := newSubnet.Name
	if newSubnet.Spec.CIDR != "" {
		if err := e.addNewRangePoolIfNotExist(getPoolID(poolID, string(ccev2.IPv4Family)), newSubnet.Spec.CIDR); err != nil {
			return
		}
	}
	if newSubnet.Spec.IPv6CIDR != "" {
		if err := e.addNewRangePoolIfNotExist(getPoolID(poolID, string(ccev2.IPv6Family)), newSubnet.Spec.IPv6CIDR); err != nil {
			return
		}
	}
}

func (e *EndpointManager) deleteSubnet(oldObj interface{}) {
	// do nothing
}

func (e *localPool) addNewRangePoolIfNotExist(poolID, cidrstr string) error {
	e.Lock()
	defer e.Unlock()
	if _, ok := e.localPool[poolID]; ok {
		return nil
	}

	cidrs, err := cidr.ParseCIDR(cidrstr)
	if err != nil {
		managerLog.WithField("scope", "update subnet").Errorf("parse cidr %s failed, err: %v", cidrstr, err)
		return err
	}
	pool, err := allocator.NewSubRangePoolAllocator(ipamTypes.PoolID(poolID), cidrs, operatorOption.Config.PSTSSubnetReversedIPNum)
	if err != nil {
		managerLog.WithField("scope", "update subnet").Errorf("create subnet pool %s failed, err: %v", poolID, err)
		return err
	}
	e.localPool[poolID] = pool
	return nil
}

// updateEndpoint update endpoint ip in local pool
func (e *localPool) addEndpointAddress(cep *ccev2.CCEEndpoint) {
	if cep.Status.Networking == nil {
		return
	}
	if cep.Spec.Network.IPAllocation == nil || cep.Spec.Network.IPAllocation.PSTSName == "" {
		return
	}
	e.Lock()
	defer e.Unlock()
	for _, address := range cep.Status.Networking.Addressing {
		if address.IP == "" {
			continue
		}
		poolID := getPoolID(address.Subnet, string(address.Family))
		if pool, ok := e.localPool[poolID]; ok {
			pool.Allocate(net.ParseIP(address.IP))
		}
	}
}

// updateEndpoint update endpoint ip in local pool
func (e *localPool) deleteEndpointAddress(cep *ccev2.CCEEndpoint) {
	if cep.Status.Networking == nil {
		return
	}
	if cep.Spec.Network.IPAllocation == nil || cep.Spec.Network.IPAllocation.PSTSName == "" {
		return
	}
	for _, address := range cep.Status.Networking.Addressing {
		if address.IP == "" {
			continue
		}
		e.releaseLocalIPs(address)
	}
}

func (e *localPool) releaseLocalIPs(addr *ccev2.AddressPair) {
	e.Lock()
	defer e.Unlock()
	pool, ok := e.localPool[getPoolID(addr.Subnet, string(addr.Family))]
	if !ok {
		return
	}
	pool.ReleaseMany([]net.IP{net.ParseIP(addr.IP)})
}

func getPoolID(sbnID, family string) string {
	return fmt.Sprintf("%s-%s", sbnID, family)
}

func (e *localPool) getPool(poolID, family string) *allocator.SubRangePoolAllocator {
	e.Lock()
	defer e.Unlock()
	return e.localPool[getPoolID(poolID, family)]
}

// releaseMany releases IPs from local pool
// this method tries to release the IP as much as possible. And ignore the error
func (e *localPool) releaseMany(subnetID string, ips []net.IP) {
	e.Lock()
	defer e.Unlock()
	release := func(poolName string) {
		allocator, ok := e.localPool[poolName]
		if ok {
			allocator.ReleaseMany(ips)
		}
	}
	release(getPoolID(subnetID, string(ccev2.IPv4Family)))
	release(getPoolID(subnetID, string(ccev2.IPv6Family)))
}
