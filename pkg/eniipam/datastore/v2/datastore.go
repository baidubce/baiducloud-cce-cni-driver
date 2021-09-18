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

package v2

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
)

const (
	addressCoolingPeriod = 30 * time.Second
)

var (
	UnknownNodeError = errors.New("datastore: unknown Node")

	UnknownSubnetError = errors.New("datastore: unknown subnet")

	UnknownIPError = errors.New("datastore: unknown IP")

	EmptyNodeError = errors.New("datastore: empty Node")

	EmptySubnetError = errors.New("datastore: empty subnet")

	NoAvailableIPAddressInDataStoreError = errors.New("no available ip address in datastore")

	NoAvailableIPAddressInSubnetBucketError = errors.New("no available ip address in subnet bucket")
)

var (
	// metrics
	metricTotalIPCount     = metric.PrimaryEniMultiIPEniIPTotalCount
	metricAllocatedIPCount = metric.PrimaryEniMultiIPEniIPAllocatedCount
	metricAvailableIPCount = metric.PrimaryEniMultiIPEniIPAvailableCount
)

type DataStore struct {
	store map[string]*Instance
	clock clock.Clock
	lock  sync.RWMutex
}

type Instance struct {
	ID       string
	total    int
	assigned int
	pool     map[string]*SubnetBucket
}

type SubnetBucket struct {
	ID            string
	IPv4Addresses map[string]*AddressInfo
}

type AddressInfo struct {
	Address        string
	Assigned       bool
	SubnetID       string
	UnassignedTime time.Time
}

func NewDataStore() *DataStore {
	return &DataStore{
		store: map[string]*Instance{},
		clock: clock.RealClock{},
	}
}

func (sb *SubnetBucket) AssignedIPv4Addresses() int {
	count := 0
	for _, addr := range sb.IPv4Addresses {
		if addr.Assigned {
			count++
		}
	}
	return count
}

func (sb *SubnetBucket) TotalIPv4Addresses() int {
	return len(sb.IPv4Addresses)
}

// inCoolingPeriod checks whether an addr is in addressCoolingPeriod
func (addr AddressInfo) inCoolingPeriod() bool {
	return time.Since(addr.UnassignedTime) < addressCoolingPeriod
}

func (ds *DataStore) AllocatePodPrivateIP(node string) (string, string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return "", "", UnknownNodeError
	}

	for _, sbucket := range instance.pool {
		for _, addr := range sbucket.IPv4Addresses {
			if addr.Assigned || addr.inCoolingPeriod() {
				continue
			}
			// update status
			addr.Assigned = true
			instance.assigned++

			metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Inc()
			metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Dec()

			return addr.Address, addr.SubnetID, nil
		}
	}

	return "", "", NoAvailableIPAddressInDataStoreError
}

func (ds *DataStore) AllocatePodPrivateIPBySubnet(node, subnetID string) (string, string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return "", "", UnknownNodeError
	}

	sbucket, ok := instance.pool[subnetID]
	if !ok {
		return "", "", UnknownSubnetError
	}

	for _, addr := range sbucket.IPv4Addresses {
		if addr.Assigned || addr.inCoolingPeriod() {
			continue
		}
		// update status
		addr.Assigned = true
		instance.assigned++

		metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Inc()
		metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Dec()

		return addr.Address, addr.SubnetID, nil
	}

	return "", "", NoAvailableIPAddressInSubnetBucketError
}

func (ds *DataStore) ReleasePodPrivateIP(node, subnetID, ip string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return UnknownNodeError
	}

	sbucket, ok := instance.pool[subnetID]
	if !ok {
		return UnknownSubnetError
	}

	addr, ok := sbucket.IPv4Addresses[ip]
	if ok {
		if addr.Assigned {
			instance.assigned--

			metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Dec()
			metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Inc()
		}
		addr.Assigned = false
		return nil
	}

	return UnknownIPError
}

func (ds *DataStore) AddPrivateIPToStore(node, subnetID, ipAddress string, assigned bool) error {
	if node == "" {
		return EmptyNodeError
	}

	if subnetID == "" {
		return EmptySubnetError
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return UnknownNodeError
	}

	_, ok = instance.pool[subnetID]
	if !ok {
		// if not exist, create subnet bucket
		instance.pool[subnetID] = &SubnetBucket{
			ID:            subnetID,
			IPv4Addresses: make(map[string]*AddressInfo),
		}
	}

	sbucket, _ := instance.pool[subnetID]

	// Already exists
	_, ok = sbucket.IPv4Addresses[ipAddress]
	if ok {
		return fmt.Errorf("ip %v already in datastore", ipAddress)
	}

	// increase counter
	instance.total++
	metricTotalIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Inc()
	if assigned {
		instance.assigned++
		metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Inc()
	} else {
		metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Inc()
	}

	// update store
	sbucket.IPv4Addresses[ipAddress] = &AddressInfo{
		Address:  ipAddress,
		Assigned: assigned,
		SubnetID: subnetID,
	}

	return nil
}

func (ds *DataStore) DeletePrivateIPFromStore(node, subnetID, ipAddress string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return UnknownNodeError
	}

	sbucket, ok := instance.pool[subnetID]
	if !ok {
		return UnknownSubnetError
	}

	// decrease total
	addr, ok := sbucket.IPv4Addresses[ipAddress]
	if ok {
		instance.total--
		metricTotalIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Dec()

		if addr.Assigned {
			instance.assigned--
			metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Dec()
		} else {
			metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Dec()
		}
	}

	// update store
	delete(sbucket.IPv4Addresses, ipAddress)

	return nil
}

func (ds *DataStore) AddNodeToStore(node, instanceID string) error {
	if node == "" {
		return EmptyNodeError
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	_, ok := ds.store[node]
	if ok {
		return fmt.Errorf("node %v already in datastore", node)
	}

	ds.store[node] = &Instance{
		ID:   instanceID,
		pool: map[string]*SubnetBucket{},
	}

	return nil
}

func (ds *DataStore) DeleteNodeFromStore(node string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	if nodeStore, ok := ds.store[node]; ok {
		for subnet, _ := range nodeStore.pool {
			metricTotalIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnet).Set(0)
			metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnet).Set(0)
			metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnet).Set(0)
		}
	}

	delete(ds.store, node)

	return nil
}

func (ds *DataStore) NodeExistsInStore(node string) bool {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	_, ok := ds.store[node]
	return ok
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

func (ds *DataStore) GetSubnetBucketStats(node, subnetID string) (int, int, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return 0, 0, UnknownNodeError
	}

	sbucket, ok := instance.pool[subnetID]
	if !ok {
		return 0, 0, UnknownSubnetError
	}

	return sbucket.TotalIPv4Addresses(), sbucket.AssignedIPv4Addresses(), nil
}

func (ds *DataStore) GetUnassignedPrivateIPByNode(node string) ([]string, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return nil, UnknownNodeError
	}

	var result []string
	for _, sbucket := range instance.pool {
		for _, ip := range sbucket.IPv4Addresses {
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
