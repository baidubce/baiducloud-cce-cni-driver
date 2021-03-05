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

package datastore

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	addressCoolingPeriod = 30 * time.Second
)

var (
	UnknownNodeError = errors.New("datastore: unknown Node")

	UnknownENIError = errors.New("datastore: unknown ENI")

	UnknownIPError = errors.New("datastore: unknown IP")
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
	return &DataStore{
		store: map[string]*Instance{},
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

	// TODO: allocate ip backward
	for _, eni := range instance.eniPool {
		for _, addr := range eni.IPv4Addresses {
			if addr.Assigned || addr.inCoolingPeriod() {
				continue
			}
			// update status
			addr.Assigned = true
			instance.assigned++

			return addr.Address, nil
		}

	}

	return "", fmt.Errorf("no available ip address in datastore")
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
		}
		addr.Assigned = false
		return nil
	}

	return UnknownIPError
}

func (ds *DataStore) AddPrivateIPToStore(node, eniID, ipAddress string, assigned bool) error {
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

	// Already exists
	_, ok = eni.IPv4Addresses[ipAddress]
	if ok {
		return fmt.Errorf("ip %v already in datastore", ipAddress)
	}

	// increase counter
	instance.total++
	if assigned {
		instance.assigned++
	}

	// update store
	eni.IPv4Addresses[ipAddress] = &AddressInfo{
		Address:  ipAddress,
		Assigned: assigned,
	}

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
	_, ok = eni.IPv4Addresses[ipAddress]
	if ok {
		instance.total--
	}

	// update store
	delete(eni.IPv4Addresses, ipAddress)

	return nil
}

func (ds *DataStore) AddNodeToStore(node, instanceID string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	_, ok := ds.store[node]
	if ok {
		return fmt.Errorf("node %v already in datastore", node)
	}

	ds.store[node] = &Instance{
		ID:      instanceID,
		eniPool: map[string]*ENI{},
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
		IPv4Addresses: map[string]*AddressInfo{},
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
