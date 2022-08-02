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
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	addressCoolingPeriodEnvKey = "DATASTORE_ADDRESS_COOLING_PERIOD"
)

var (
	addressCoolingPeriod = 5 * time.Second
)

var (
	UnknownNodeError = errors.New("datastore: unknown Node")

	UnknownENIError = errors.New("datastore: unknown ENI")

	UnknownIPError = errors.New("datastore: unknown IP")

	EmptyNodeError = errors.New("datastore: empty Node")

	EmptyENIError = errors.New("datastore: empty ENI")

	NoAvailableIPAddressInDataStoreError = errors.New("no available ip address in datastore")

	NoAvailableIPAddressInENIError = errors.New("no available ip address in eni")
)

type DataStore struct {
	store map[string]*Instance
	lock  sync.RWMutex
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
		store: make(map[string]*Instance),
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

func (ds *DataStore) AllocatePodPrivateIP(node string) (string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return "", UnknownNodeError
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

	return "", NoAvailableIPAddressInDataStoreError
}

func (ds *DataStore) AllocatePodPrivateIPByENI(node, eniID string) (string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return "", UnknownNodeError
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return "", UnknownENIError
	}

	addr, err := eni.idle.Top()
	if err != nil || addr.Assigned || addr.inCoolingPeriod() {
		return "", NoAvailableIPAddressInENIError
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

	instance, ok := ds.store[node]
	if !ok {
		return UnknownNodeError
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return UnknownENIError
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

	return UnknownIPError
}

func (ds *DataStore) AddPrivateIPToStore(node, eniID, ipAddress string, assigned bool) error {
	if node == "" {
		return EmptyNodeError
	}

	if eniID == "" {
		return EmptyENIError
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return UnknownNodeError
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
		Address:  ipAddress,
		Assigned: assigned,
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

func (ds *DataStore) DeletePrivateIPFromStore(node, eniID, ipAddress string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return UnknownNodeError
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return UnknownENIError
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
		return UnknownNodeError
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

	return nil
}

func (ds *DataStore) AddENIToStore(node, eniID string) error {
	if eniID == "" {
		return EmptyENIError
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return UnknownNodeError
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
		return UnknownNodeError
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return UnknownENIError
	}

	total := eni.TotalIPv4Addresses()
	assigned := eni.AssignedIPv4Addresses()

	instance.total -= total
	instance.assigned -= assigned

	delete(instance.eniPool, eniID)

	return nil
}

func (ds *DataStore) GetNodeStats(node string) (int, int, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return 0, 0, UnknownNodeError
	}

	return instance.total, instance.assigned, nil
}

func (ds *DataStore) GetENIStats(node, eniID string) (int, int, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return 0, 0, UnknownNodeError
	}

	eni, ok := instance.eniPool[eniID]
	if !ok {
		return 0, 0, UnknownENIError
	}

	total := eni.TotalIPv4Addresses()
	assigned := eni.AssignedIPv4Addresses()

	return total, assigned, nil
}

func (ds *DataStore) GetUnassignedPrivateIPByNode(node string) ([]string, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return nil, UnknownNodeError
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
