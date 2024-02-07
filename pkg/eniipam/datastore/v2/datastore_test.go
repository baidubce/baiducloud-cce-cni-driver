package v2

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
		subnetID   = "sbn-xxx"
	)

	err := store.AddNodeToStore(node, instanceID)
	assert.NoError(t, err)
	assert.Equal(t, store.NodeExistsInStore(node), true)
	assert.Equal(t, len(store.ListNodes()), 1)

	// add 2 ips
	store.AddPrivateIPToStore(node, subnetID, "1.1.1.1", true)
	store.AddPrivateIPToStore(node, subnetID, "2.2.2.2", false)

	ips, _ := store.GetUnassignedPrivateIPByNode(node)
	assert.Equal(t, ips, []string{"2.2.2.2"})

	total, assigned, _ := store.GetNodeStats(node)
	assert.Equal(t, total, 2)
	assert.Equal(t, assigned, 1)

	total, assigned, _ = store.GetSubnetBucketStats(node, subnetID)
	assert.Equal(t, total, 2)
	assert.Equal(t, assigned, 1)

	// del ip
	store.DeletePrivateIPFromStore(node, subnetID, "2.2.2.2")
	ips, _ = store.GetUnassignedPrivateIPByNode(node)
	assert.Equal(t, len(ips), 0)

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
		subnetID   = "sbn-xxx"
	)

	err := store.AddNodeToStore(node, instanceID)
	assert.NoError(t, err)

	// add ips
	store.AddPrivateIPToStore(node, subnetID, "1.1.1.1", false)

	addr, _, _ := store.AllocatePodPrivateIP(node)
	assert.Equal(t, addr, "1.1.1.1")

	_, _, err = store.AllocatePodPrivateIP(node)
	assert.Error(t, err)

	err = store.ReleasePodPrivateIP(node, subnetID, "1.1.1.1")
	assert.NoError(t, err)

	addr, _, _ = store.AllocatePodPrivateIP(node)
	assert.Equal(t, addr, "1.1.1.1")

	err = store.DeletePrivateIPFromStore(node, subnetID, "1.1.1.1")
	assert.NoError(t, err)

	_, _, err = store.AllocatePodPrivateIP(node)
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
		subnetID   = "sbn-xxx"
	)

	err := store.AddNodeToStore(node, instanceID)
	assert.NoError(t, err)

	// add ips
	store.AddPrivateIPToStore(node, subnetID, "1.1.1.1", false)
	store.AddPrivateIPToStore(node, subnetID, "2.2.2.2", false)
	store.AddPrivateIPToStore(node, subnetID, "3.3.3.3", false)

	addr, _, _ := store.AllocatePodPrivateIP(node)
	assert.Equal(t, addr, "1.1.1.1")

	addr, _, _ = store.AllocatePodPrivateIP(node)
	assert.Equal(t, addr, "3.3.3.3")

	addr, _, _ = store.AllocatePodPrivateIPBySubnet(node, subnetID)
	assert.Equal(t, addr, "2.2.2.2")

	// check unassigned ip
	ips, _ := store.GetUnassignedPrivateIPByNode(node)
	assert.Equal(t, len(ips), 0)

	err = store.ReleasePodPrivateIP(node, subnetID, "2.2.2.2")
	assert.NoError(t, err)

	addr, _, err = store.AllocatePodPrivateIPBySubnet(node, subnetID)
	assert.NoError(t, err)
	assert.Equal(t, addr, "2.2.2.2")
}
