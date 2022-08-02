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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func init() {
	os.Setenv(addressCoolingPeriodEnvKey, "0")
}
func TestDataStore(t *testing.T) {
	var (
		store = NewDataStore()
	)

	var (
		node       = "node"
		instanceID = "i-xxxxx"
		eniID      = "eni-xxx"
	)

	err := store.AddNodeToStore(node, instanceID)
	assert.NoError(t, err)
	assert.Equal(t, store.NodeExistsInStore(node), true)
	assert.Equal(t, len(store.ListNodes()), 1)

	// add eni
	store.AddENIToStore(node, eniID)

	total, assigned, _ := store.GetENIStats(node, eniID)
	assert.Equal(t, total, 0)
	assert.Equal(t, assigned, 0)

	// add 2 ips
	store.AddPrivateIPToStore(node, eniID, "1.1.1.1", true)
	store.AddPrivateIPToStore(node, eniID, "2.2.2.2", false)

	ips, _ := store.GetUnassignedPrivateIPByNode(node)
	assert.Equal(t, ips, []string{"2.2.2.2"})

	total, assigned, _ = store.GetNodeStats(node)
	assert.Equal(t, total, 2)
	assert.Equal(t, assigned, 1)

	// del ip
	store.DeletePrivateIPFromStore(node, eniID, "2.2.2.2")
	ips, _ = store.GetUnassignedPrivateIPByNode(node)
	assert.Equal(t, len(ips), 0)

	// del eni
	assert.Equal(t, store.ENIExistsInStore(node, eniID), true)
	store.DeleteENIFromStore(node, eniID)
	total, assigned, _ = store.GetNodeStats(node)
	assert.Equal(t, total, 0)
	assert.Equal(t, assigned, 0)

	err = store.DeleteNodeFromStore(node)
	assert.NoError(t, err)
}

func TestExhaustPodPrivateIP(t *testing.T) {
	var (
		store = NewDataStore()
	)

	var (
		node       = "node"
		instanceID = "i-xxxxx"
		eniID      = "eni-xxx"
	)

	err := store.AddNodeToStore(node, instanceID)
	assert.NoError(t, err)

	// add ips
	store.AddPrivateIPToStore(node, eniID, "1.1.1.1", false)

	addr, _ := store.AllocatePodPrivateIP(node)
	assert.Equal(t, addr, "1.1.1.1")

	_, err = store.AllocatePodPrivateIP(node)
	assert.Equal(t, err, NoAvailableIPAddressInDataStoreError)

	err = store.ReleasePodPrivateIP(node, eniID, "1.1.1.1")
	assert.NoError(t, err)

	addr, _ = store.AllocatePodPrivateIPByENI(node, eniID)
	assert.Equal(t, addr, "1.1.1.1")

	err = store.DeletePrivateIPFromStore(node, eniID, "1.1.1.1")
	assert.NoError(t, err)

	_, err = store.AllocatePodPrivateIP(node)
	assert.Error(t, err)

	ips, err := store.GetUnassignedPrivateIPByNode(node)
	assert.Equal(t, len(ips), 0)
	assert.NoError(t, err)
}

func TestRecyclePodPrivateIP(t *testing.T) {
	var (
		store = NewDataStore()
	)

	var (
		node       = "node"
		instanceID = "i-xxxxx"
		eniID      = "eni-xxx"
	)

	err := store.AddNodeToStore(node, instanceID)
	assert.NoError(t, err)

	// add ips
	store.AddPrivateIPToStore(node, eniID, "1.1.1.1", false)
	store.AddPrivateIPToStore(node, eniID, "2.2.2.2", false)
	store.AddPrivateIPToStore(node, eniID, "3.3.3.3", false)

	addr, _ := store.AllocatePodPrivateIP(node)
	assert.Equal(t, addr, "1.1.1.1")

	addr, _ = store.AllocatePodPrivateIP(node)
	assert.Equal(t, addr, "3.3.3.3")

	addr, _ = store.AllocatePodPrivateIPByENI(node, eniID)
	assert.Equal(t, addr, "2.2.2.2")

	// check unassigned ip
	ips, _ := store.GetUnassignedPrivateIPByNode(node)
	assert.Equal(t, len(ips), 0)

	err = store.ReleasePodPrivateIP(node, eniID, "2.2.2.2")
	assert.NoError(t, err)

	addr, err = store.AllocatePodPrivateIPByENI(node, eniID)
	assert.NoError(t, err)
	assert.Equal(t, addr, "2.2.2.2")

	total, assigned, _ := store.GetENIStats(node, eniID)
	assert.Equal(t, total, 3)
	assert.Equal(t, assigned, 3)
}
