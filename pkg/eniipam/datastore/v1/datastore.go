/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package v1

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	addressCoolingPeriodEnvKey = "DATASTORE_ADDRESS_COOLING_PERIOD"
	// prefix for data store
	crossSubnetKeyPrefix = "cce.baidubce.com/crossSubnet"
)

var (
	addressCoolingPeriod = 5 * time.Second
)

var (
	ErrUnknownNode = errors.New("datastore: unknown Node")

	ErrUnknownENI = errors.New("datastore: unknown ENI")

	ErrUnknownIP = errors.New("datastore: unknown IP")

	ErrEmptyNode = errors.New("datastore: empty Node")

	ErrEmptyENI = errors.New("datastore: empty ENI")

	ErrNoAvailableIPAddressInDataStore = errors.New("no available ip address in datastore")

	ErrNoAvailableIPAddressInENI = errors.New("no available ip address in eni")
)

func ErrNoAvailableIPAddressWithInCoolingPeriodInENI(addressInfo *AddressInfo) error {
	errStr := fmt.Sprintf("no available ip address in eni, the ip %s is in cooling period, time left %s, unassigned time is %s",
		addressInfo.Address, addressInfo.coolingPeriodTimeLeft().String(), addressInfo.UnassignedTime.Format("2006-01-02T15:04:05Z"))
	return errors.New(errStr)
}

type DataStore struct {
	store map[string]*Instance
	// key is node name
	crossSubnetStore map[string]*Instance
	lock             sync.RWMutex
}

type Instance struct {
	ID       string
	total    int
	assigned int
	eniPool  map[string]*ENI
}

type ENI struct {
	ID            string
	IPv4Addresses map[string]*AddressInfo
	idle          *priorityQueue
}

type AddressInfo struct {
	Address        string
	Assigned       bool
	UnassignedTime time.Time
	CrossSubnet    bool
}

type PodKey struct {
	Name        string
	Namespace   string
	ContainerID string
}

type PodIPInfo struct {
	Address    string
	InstanceID string
	ENIID      string
}

func NewDataStore() *DataStore {
	setAddressCoolingPeriodByEnv()

	return &DataStore{
		store:            make(map[string]*Instance),
		crossSubnetStore: make(map[string]*Instance),
	}
}

func (e *ENI) AssignedIPv4Addresses() int {
	count := 0
	for _, addr := range e.IPv4Addresses {
		if addr.Assigned {
			count++
		}
	}
	return count
}

func (e *ENI) TotalIPv4Addresses() int {
	return len(e.IPv4Addresses)
}

// inCoolingPeriod checks whether an addr is in addressCoolingPeriod
func (addr AddressInfo) inCoolingPeriod() bool {
	return time.Since(addr.UnassignedTime) < addressCoolingPeriod
}

// coolingPeriodTimeLeft return a time.Duration for cooling period time left
func (addr AddressInfo) coolingPeriodTimeLeft() time.Duration {
	timeSince := time.Since(addr.UnassignedTime)
	if timeSince < addressCoolingPeriod {
		return addressCoolingPeriod - timeSince
	}
	return 0
}

// Synchronized Executing transactions in locks
func (ds *DataStore) Synchronized(task func() error) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	return task()
}

func (ds *DataStore) AllocatePodPrivateIP(node string) (string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return "", ErrUnknownNode
	}

	for _, eni := range instance.eniPool {
		addr, err := eni.idle.Top()
		if err != nil {
			continue
		}

		if addr.Assigned || addr.inCoolingPeriod() {
			continue
		}
		// update status
		addr.Assigned = true
		instance.assigned++
		eni.idle.Pop()

		return addr.Address, nil
	}

	return "", ErrNoAvailableIPAddressInDataStore
}

func (ds *DataStore) AllocatePodPrivateIPByENI(node, eniID string) (string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return "", ErrUnknownNode
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return "", ErrUnknownENI
	}

	addr, err := eni.idle.Top()
	if err != nil || addr.Assigned {
		return "", ErrNoAvailableIPAddressInENI
	}
	if addr.inCoolingPeriod() {
		return "", ErrNoAvailableIPAddressWithInCoolingPeriodInENI(addr)
	}

	// update status
	addr.Assigned = true
	instance.assigned++
	eni.idle.Pop()

	return addr.Address, nil
}

func (ds *DataStore) ReleasePodPrivateIP(node, eniID, ip string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.ReleasePodPrivateIPUnsafe(node, eniID, ip)
	return nil
}

func (ds *DataStore) ReleasePodPrivateIPUnsafe(node, eniID, ip string) {
	ds.__ReleasePodPrivateIPUnsafe(node, eniID, ip, false)
	ds.__ReleasePodPrivateIPUnsafe(node, eniID, ip, true)
}
func (ds *DataStore) __ReleasePodPrivateIPUnsafe(node, eniID, ip string, crossSubnet bool) error {
	instance, ok := ds.getNodeInstance(node, crossSubnet)
	if !ok {
		return ErrUnknownNode
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return ErrUnknownENI
	}

	addr, ok := eni.IPv4Addresses[ip]
	if ok {
		if addr.Assigned {
			instance.assigned--
			addr.UnassignedTime = time.Now()
			err := eni.idle.Insert(addr)
			if err != nil {
				return err
			}
		}
		addr.Assigned = false
		return nil
	}

	return ErrUnknownIP
}

// Add the IP address to the Eni cache, and mark whether the IP address is an IP address across the subnet
func (ds *DataStore) AddPrivateIPToStoreUnsafe(node, eniID, ipAddress string, assigned, crossSubnet bool) error {
	if node == "" {
		return ErrEmptyNode
	}

	if eniID == "" {
		return ErrEmptyENI
	}

	instance, ok := ds.getNodeInstance(node, crossSubnet)
	if !ok {
		return ErrUnknownNode
	}

	_, ok = instance.eniPool[eniID]
	if !ok {
		// eni not exist
		instance.eniPool[eniID] = &ENI{
			ID:            eniID,
			IPv4Addresses: make(map[string]*AddressInfo),
			idle:          newPriorityQueue(),
		}
	}

	eni := instance.eniPool[eniID]

	// already exists
	_, ok = eni.IPv4Addresses[ipAddress]
	if ok {
		return fmt.Errorf("ip %v already in datastore", ipAddress)
	}

	addr := &AddressInfo{
		Address:     ipAddress,
		Assigned:    assigned,
		CrossSubnet: crossSubnet,
	}

	// increase counter
	instance.total++
	if assigned {
		instance.assigned++
	} else {
		eni.idle.Insert(addr)
	}

	// update store
	eni.IPv4Addresses[ipAddress] = addr

	return nil
}

func (ds *DataStore) AddPrivateIPToStore(node, eniID, ipAddress string, assigned bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	return ds.AddPrivateIPToStoreUnsafe(node, eniID, ipAddress, assigned, false)
}

func (ds *DataStore) DeletePrivateIPFromStore(node, eniID, ipAddress string) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.DeletePrivateIPFromStoreUnsafe(node, eniID, ipAddress)
}

func (ds *DataStore) DeletePrivateIPFromStoreUnsafe(node, eniID, ipAddress string) {
	ds.__DeletePrivateIPFromStoreUnsafe(node, eniID, ipAddress, false)
	// delete cross subnet ip
	ds.__DeletePrivateIPFromStoreUnsafe(node, eniID, ipAddress, true)

}

func (ds *DataStore) __DeletePrivateIPFromStoreUnsafe(node, eniID, ipAddress string, crossSubnet bool) error {
	instance, ok := ds.getNodeInstance(node, crossSubnet)
	if !ok {
		return ErrUnknownNode
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return ErrUnknownENI
	}

	// decrease total
	addr, ok := eni.IPv4Addresses[ipAddress]
	if ok {
		instance.total--
		if addr.Assigned {
			instance.assigned--
		}
	}

	// update store
	if addr != nil {
		eni.idle.Remove(addr)
	}
	delete(eni.IPv4Addresses, ipAddress)

	return nil
}

func (ds *DataStore) AddNodeToStore(node, instanceID string) error {
	if node == "" || instanceID == "" {
		return ErrUnknownNode
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	_, ok := ds.store[node]
	if ok {
		return fmt.Errorf("node %v already in datastore", node)
	}

	ds.store[node] = &Instance{
		ID:      instanceID,
		eniPool: make(map[string]*ENI),
	}

	ds.crossSubnetStore[node] = &Instance{
		ID:      instanceID,
		eniPool: make(map[string]*ENI),
	}

	return nil
}

func (ds *DataStore) NodeExistsInStore(node string) bool {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	_, ok := ds.store[node]
	return ok
}

func (ds *DataStore) DeleteNodeFromStore(node string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	delete(ds.store, node)
	delete(ds.crossSubnetStore, node)

	return nil
}

func (ds *DataStore) AddENIToStore(node, eniID string) error {
	if eniID == "" {
		return ErrEmptyENI
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return ErrUnknownNode
	}

	_, ok = instance.eniPool[eniID]
	if ok {
		return fmt.Errorf("eni %v already in datastore", eniID)
	}

	instance.eniPool[eniID] = &ENI{
		ID:            eniID,
		IPv4Addresses: make(map[string]*AddressInfo),
		idle:          newPriorityQueue(),
	}

	// add eni to cross subnet
	instance, ok = ds.getNodeInstance(node, true)
	if ok {
		_, ok = instance.eniPool[eniID]
		if !ok {
			instance.eniPool[eniID] = &ENI{
				ID:            eniID,
				IPv4Addresses: make(map[string]*AddressInfo),
				idle:          newPriorityQueue(),
			}
		}
	}

	return nil
}

func (ds *DataStore) ENIExistsInStore(node, eniID string) bool {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return false
	}

	_, ok = instance.eniPool[eniID]
	return ok
}

func (ds *DataStore) DeleteENIFromStore(node, eniID string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return ErrUnknownNode
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return ErrUnknownENI
	}

	total := eni.TotalIPv4Addresses()
	assigned := eni.AssignedIPv4Addresses()

	instance.total -= total
	instance.assigned -= assigned
	delete(instance.eniPool, eniID)

	// delete eni cross subnet
	if crossSubnetInstance, ok := ds.crossSubnetStore[node]; ok {
		eni, ok := crossSubnetInstance.eniPool[eniID]
		if !ok {
			return nil
		}

		total := eni.TotalIPv4Addresses()
		assigned := eni.AssignedIPv4Addresses()

		crossSubnetInstance.total -= total
		crossSubnetInstance.assigned -= assigned
		delete(crossSubnetInstance.eniPool, eniID)
	}
	return nil
}

func (ds *DataStore) GetNodeStats(node string) (int, int, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return 0, 0, ErrUnknownNode
	}

	total := instance.total
	assigned := instance.assigned

	if crossSubnetInstance, ok := ds.crossSubnetStore[node]; ok {
		total += crossSubnetInstance.total
		assigned += crossSubnetInstance.assigned
	}

	return total, assigned, nil
}

func (ds *DataStore) GetENIStats(node, eniID string) (int, int, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return 0, 0, ErrUnknownNode
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return 0, 0, ErrUnknownENI
	}

	total := eni.TotalIPv4Addresses()
	assigned := eni.AssignedIPv4Addresses()

	if instance, ok := ds.crossSubnetStore[node]; ok {
		eni, ok := instance.eniPool[eniID]
		if ok {
			total += eni.TotalIPv4Addresses()
			assigned += eni.AssignedIPv4Addresses()
			logger.V(5).Infof(context.TODO(), "eni %s of node %s datastore total: %d ,assigned: %d", eniID, node, total, assigned)
		}
	}

	return total, assigned, nil
}

func (ds *DataStore) GetUnassignedPrivateIPByNode(node string) ([]string, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return nil, ErrUnknownNode
	}

	var result []string
	for _, eni := range instance.eniPool {
		for _, ip := range eni.IPv4Addresses {
			if !ip.Assigned {
				result = append(result, ip.Address)
			}
		}
	}

	return result, nil
}

func (ds *DataStore) ListNodes() []string {
	var nodes []string
	for n, _ := range ds.store {
		nodes = append(nodes, n)
	}
	return nodes
}

func setAddressCoolingPeriodByEnv() {
	periodStr := os.Getenv(addressCoolingPeriodEnvKey)
	period, err := strconv.Atoi(periodStr)
	if err != nil {
		return
	}
	addressCoolingPeriod = time.Duration(period) * time.Second
}

// get bcc machine instance which contains some enis
// from datastore when crossSubnet is false or
// from crossSubnetDataStore when crossSubnet is true and create a new instance by normal datastore
// if instance is not exists
func (ds *DataStore) getNodeInstance(node string, crossSubnet bool) (*Instance, bool) {
	var (
		instance       *Instance
		normalInstance *Instance
		ok             bool
	)
	if crossSubnet {
		instance, ok = ds.crossSubnetStore[node]
		if !ok {
			normalInstance, ok = ds.store[node]
			if ok {
				instance = &Instance{
					ID:      normalInstance.ID,
					eniPool: make(map[string]*ENI),
				}
				ds.crossSubnetStore[node] = instance
			}
		}
	} else {
		instance, ok = ds.store[node]
	}
	return instance, ok
}
