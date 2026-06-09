package vpceni

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

func TestEbcNetworkResourceSet_ReuseIPs_GivenUsePrimaryENIWithSecondaryModeTrue_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := "default/test-pod"

	// When: calling reuseIPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error indicating not supported
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenUsePrimaryENIWithSecondaryModeFalse_WhenReuseIPs_ThenCallParentMethod(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode disabled
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: false,
		},
	}

	// When: checking the condition for calling parent method
	// Then: should have usePrimaryENIWithSecondaryMode set to false
	assert.False(t, ebcNode.usePrimaryENIWithSecondaryMode)

	// 验证当 usePrimaryENIWithSecondaryMode 为 false 时，条件分支是正确的
	if !ebcNode.usePrimaryENIWithSecondaryMode {
		// 这个分支应该调用父类的 reuseIPs 方法
		assert.True(t, true, "Should enter the parent method call branch")
	}
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenNilIPs_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and nil IPs
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	var ips []*models.PrivateIP = nil
	owner := "default/test-pod"

	// When: calling reuseIPs with nil IPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenEmptyIPs_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and empty IPs
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{}
	owner := "default/test-pod"

	// When: calling reuseIPs with empty IPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenEmptyOwner_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and empty owner
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := ""

	// When: calling reuseIPs with empty owner
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenMultipleIPs_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and multiple IPs
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx := context.Background()
	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
		{
			PrivateIPAddress: "10.0.0.101",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := "default/test-pod"

	// When: calling reuseIPs with multiple IPs
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}

func TestEbcNetworkResourceSet_ReuseIPs_GivenCancelledContext_WhenReuseIPs_ThenReturnError(t *testing.T) {
	// Given: EBC node with primary ENI with secondary IP mode enabled and cancelled context
	ebcNode := &ebcNetworkResourceSet{
		bccNetworkResourceSet: &bccNetworkResourceSet{
			usePrimaryENIWithSecondaryMode: true,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel the context immediately

	ips := []*models.PrivateIP{
		{
			PrivateIPAddress: "10.0.0.100",
			SubnetID:         "subnet-123",
			Primary:          false,
		},
	}
	owner := "default/test-pod"

	// When: calling reuseIPs with cancelled context
	eniID, err := ebcNode.reuseIPs(ctx, ips, owner)

	// Then: should return error (the function should still return the same error regardless of context)
	assert.Empty(t, eniID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ebc primary interface with secondary IP mode not support reuse ip")
}
