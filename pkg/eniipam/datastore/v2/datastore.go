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
	"os"
	"strconv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
)

const (
	addressCoolingPeriodEnvKey = "DATASTORE_ADDRESS_COOLING_PERIOD"
)

var (
	addressCoolingPeriod = 5 * time.Second
)

var (
	ErrUnknownNode = errors.New("datastore: unknown Node")

	UnknownSubnetError = errors.New("datastore: unknown subnet")

	ErrUnknownIP = errors.New("datastore: unknown IP")

	ErrEmptyNode = errors.New("datastore: empty Node")

	EmptySubnetError = errors.New("datastore: empty subnet")

	ErrNoAvailableIPAddressInDataStore = errors.New("no available ip address in datastore")

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
	idle          *priorityQueue
}

type AddressInfo struct {
	Address        string
	Assigned       bool
	SubnetID       string
	UnassignedTime time.Time
}

func NewDataStore() *DataStore {
	setAddressCoolingPeriodByEnv()

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
		return "", "", ErrUnknownNode
	}

	for _, sbucket := range instance.pool {
		addr, err := sbucket.idle.Top()
		if err != nil {
			continue
		}

		if addr.Assigned || addr.inCoolingPeriod() {
			continue
		}
		// update status
		addr.Assigned = true
		instance.assigned++
		sbucket.idle.Pop()

		metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Inc()
		metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Dec()

		return addr.Address, addr.SubnetID, nil
	}

	return "", "", ErrNoAvailableIPAddressInDataStore
}

func (ds *DataStore) AllocatePodPrivateIPBySubnet(node, subnetID string) (string, string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return "", "", ErrUnknownNode
	}

	sbucket, ok := instance.pool[subnetID]
	if !ok {
		return "", "", UnknownSubnetError
	}

	addr, err := sbucket.idle.Top()
	if err != nil || addr.Assigned || addr.inCoolingPeriod() {
		return "", "", NoAvailableIPAddressInSubnetBucketError
	}
	// update status
	addr.Assigned = true
	instance.assigned++
	sbucket.idle.Pop()

	metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Inc()
	metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Dec()

	return addr.Address, addr.SubnetID, nil
}

func (ds *DataStore) ReleasePodPrivateIP(node, subnetID, ip string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return ErrUnknownNode
	}

	sbucket, ok := instance.pool[subnetID]
	if !ok {
		return UnknownSubnetError
	}

	addr, ok := sbucket.IPv4Addresses[ip]
	if ok {
		if addr.Assigned {
			instance.assigned--
			addr.UnassignedTime = time.Now()
			err := sbucket.idle.Insert(addr)
			if err != nil {
				return err
			}

			metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Dec()
			metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, addr.SubnetID).Inc()
		}
		addr.Assigned = false
		return nil
	}

	return ErrUnknownIP
}

func (ds *DataStore) AddPrivateIPToStore(node, subnetID, ipAddress string, assigned bool) error {
	if node == "" {
		return ErrEmptyNode
	}

	if subnetID == "" {
		return EmptySubnetError
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return ErrUnknownNode
	}

	_, ok = instance.pool[subnetID]
	if !ok {
		// if not exist, create subnet bucket
		instance.pool[subnetID] = &SubnetBucket{
			ID:            subnetID,
			IPv4Addresses: make(map[string]*AddressInfo),
			idle:          newPriorityQueue(),
		}
	}

	sbucket := instance.pool[subnetID]

	// Already exists
	_, ok = sbucket.IPv4Addresses[ipAddress]
	if ok {
		return fmt.Errorf("ip %v already in datastore", ipAddress)
	}

	addr := &AddressInfo{
		Address:  ipAddress,
		Assigned: assigned,
		SubnetID: subnetID,
	}

	// increase counter
	instance.total++
	metricTotalIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Inc()
	if assigned {
		instance.assigned++
		metricAllocatedIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Inc()
	} else {
		sbucket.idle.Insert(addr)
		metricAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, node, subnetID).Inc()
	}

	// update store
	sbucket.IPv4Addresses[ipAddress] = addr

	return nil
}

func (ds *DataStore) DeletePrivateIPFromStore(node, subnetID, ipAddress string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	instance, ok := ds.store[node]
	if !ok {
		return ErrUnknownNode
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
	if addr != nil {
		sbucket.idle.Remove(addr)
	}
	delete(sbucket.IPv4Addresses, ipAddress)

	return nil
}

func (ds *DataStore) AddNodeToStore(node, instanceID string) error {
	if node == "" {
		return ErrEmptyNode
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
		return 0, 0, ErrUnknownNode
	}

	return instance.total, instance.assigned, nil
}

func (ds *DataStore) GetSubnetBucketStats(node, subnetID string) (int, int, error) {
	ds.lock.RLock()
	defer ds.lock.RUnlock()

	instance, ok := ds.store[node]
	if !ok {
		return 0, 0, ErrUnknownNode
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
		return nil, ErrUnknownNode
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

func setAddressCoolingPeriodByEnv() {
	periodStr := os.Getenv(addressCoolingPeriodEnvKey)
	period, err := strconv.Atoi(periodStr)
	if err != nil {
		return
	}
	addressCoolingPeriod = time.Duration(period) * time.Second
}
