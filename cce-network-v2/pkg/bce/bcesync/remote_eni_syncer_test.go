package bcesync

import (
	"context"
	"testing"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// Mock ENI updater for testing
type mockENIUpdater struct {
	mock.Mock
}

func (m *mockENIUpdater) Update(eni *ccev2.ENI) (*ccev2.ENI, error) {
	args := m.Called(eni)
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

func (m *mockENIUpdater) Create(eni *ccev2.ENI) (*ccev2.ENI, error) {
	args := m.Called(eni)
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

func (m *mockENIUpdater) Delete(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *mockENIUpdater) Get(name string) (*ccev2.ENI, error) {
	args := m.Called(name)
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

func (m *mockENIUpdater) UpdateStatus(eni *ccev2.ENI) (*ccev2.ENI, error) {
	args := m.Called(eni)
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

// TestValidateAndSyncENIIPs_GivenConsistentIPs_WhenValidate_ThenReturnFalse 测试IP一致的情况
func TestValidateAndSyncENIIPs_GivenConsistentIPs_WhenValidate_ThenReturnFalse(t *testing.T) {
	// Given
	syncer := &remoteVpcEniSyncher{}

	// 创建一致的ENI数据
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-test123",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-test123",
				VpcID: "vpc-test123",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.1.10"},
					{PrivateIPAddress: "192.168.1.11"},
				},
				IPV6PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "2001:db8::1"},
				},
			},
		},
	}

	iaasENI := &enisdk.Eni{
		EniId: "eni-test123",
		VpcId: "vpc-test123",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.10"},
			{PrivateIpAddress: "192.168.1.11"},
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "2001:db8::1"},
		},
	}

	// When
	result := syncer.validateAndSyncENIIPs(existingENI, iaasENI)

	// Then
	assert.False(t, result, "Should return false when IPs are consistent")
}

// TestValidateAndSyncENIIPs_GivenInconsistentIPs_WhenValidate_ThenReturnTrueAndSync 测试IP不一致的情况
func TestValidateAndSyncENIIPs_GivenInconsistentIPs_WhenValidate_ThenReturnTrueAndSync(t *testing.T) {
	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	// 创建不一致的ENI数据
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-test456",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-test456",
				VpcID: "vpc-test456",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.1.10"}, // 这个IP在IaaS中不存在
					{PrivateIPAddress: "192.168.1.11"},
				},
				IPV6PrivateIPSet: []*models.PrivateIP{},
			},
		},
	}

	iaasENI := &enisdk.Eni{
		EniId:    "eni-test456",
		VpcId:    "vpc-test456",
		SubnetId: "subnet-test456",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.11"},
			{PrivateIpAddress: "192.168.1.12"}, // 这个IP在K8s中不存在
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "2001:db8::2"}, // 这个IPv6在K8s中不存在
		},
	}

	// 设置mock期望
	mockUpdater.On("Update", mock.AnythingOfType("*v2.ENI")).Return(
		func(eni *ccev2.ENI) *ccev2.ENI {
			// 验证更新的ENI包含了IaaS的数据
			assert.Len(t, eni.Spec.ENI.PrivateIPSet, 2)
			assert.Equal(t, "192.168.1.11", eni.Spec.ENI.PrivateIPSet[0].PrivateIPAddress)
			assert.Equal(t, "192.168.1.12", eni.Spec.ENI.PrivateIPSet[1].PrivateIPAddress)

			assert.Len(t, eni.Spec.ENI.IPV6PrivateIPSet, 1)
			assert.Equal(t, "2001:db8::2", eni.Spec.ENI.IPV6PrivateIPSet[0].PrivateIPAddress)

			return eni
		}, nil)

	// When
	result := syncer.validateAndSyncENIIPs(existingENI, iaasENI)

	// Then
	assert.True(t, result, "Should return true when IPs are inconsistent")
	mockUpdater.AssertExpectations(t)
}

// TestValidateAndSyncENIIPs_GivenEmptyK8sIPs_WhenValidate_ThenSyncFromIaaS 测试K8s为空IP的情况
func TestValidateAndSyncENIIPs_GivenEmptyK8sIPs_WhenValidate_ThenSyncFromIaaS(t *testing.T) {
	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	// K8s ENI没有IP
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-empty",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:               "eni-empty",
				VpcID:            "vpc-empty",
				PrivateIPSet:     []*models.PrivateIP{},
				IPV6PrivateIPSet: []*models.PrivateIP{},
			},
		},
	}

	// IaaS有IP
	iaasENI := &enisdk.Eni{
		EniId:    "eni-empty",
		VpcId:    "vpc-empty",
		SubnetId: "subnet-empty",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.2.10"},
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{},
	}

	// 设置mock期望
	mockUpdater.On("Update", mock.AnythingOfType("*v2.ENI")).Return(
		func(eni *ccev2.ENI) *ccev2.ENI {
			// 验证更新的ENI包含了IaaS的IP
			assert.Len(t, eni.Spec.ENI.PrivateIPSet, 1)
			assert.Equal(t, "192.168.2.10", eni.Spec.ENI.PrivateIPSet[0].PrivateIPAddress)
			return eni
		}, nil)

	// When
	result := syncer.validateAndSyncENIIPs(existingENI, iaasENI)

	// Then
	assert.True(t, result, "Should return true when K8s has no IPs but IaaS has IPs")
	mockUpdater.AssertExpectations(t)
}

// TestValidateAndSyncENIIPs_GivenEmptyIaaSIPs_WhenValidate_ThenSyncToEmpty 测试IaaS为空IP的情况
func TestValidateAndSyncENIIPs_GivenEmptyIaaSIPs_WhenValidate_ThenSyncToEmpty(t *testing.T) {
	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	// K8s ENI有IP
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-k8s-has-ip",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-k8s-has-ip",
				VpcID: "vpc-k8s-has-ip",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.3.10"},
				},
				IPV6PrivateIPSet: []*models.PrivateIP{},
			},
		},
	}

	// IaaS没有IP
	iaasENI := &enisdk.Eni{
		EniId:            "eni-k8s-has-ip",
		VpcId:            "vpc-k8s-has-ip",
		SubnetId:         "subnet-k8s-has-ip",
		PrivateIpSet:     []enisdk.PrivateIp{},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{},
	}

	// 设置mock期望
	mockUpdater.On("Update", mock.AnythingOfType("*v2.ENI")).Return(
		func(eni *ccev2.ENI) *ccev2.ENI {
			// 验证更新的ENI清空了IP（同步IaaS的空状态）
			assert.Len(t, eni.Spec.ENI.PrivateIPSet, 0)
			assert.Len(t, eni.Spec.ENI.IPV6PrivateIPSet, 0)
			return eni
		}, nil)

	// When
	result := syncer.validateAndSyncENIIPs(existingENI, iaasENI)

	// Then
	assert.True(t, result, "Should return true when K8s has IPs but IaaS has no IPs")
	mockUpdater.AssertExpectations(t)
}

// TestValidateAndSyncENIIPs_GivenUpdateError_WhenValidate_ThenHandleError 测试更新失败的情况
func TestValidateAndSyncENIIPs_GivenUpdateError_WhenValidate_ThenHandleError(t *testing.T) {
	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-update-error",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-update-error",
				VpcID: "vpc-update-error",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.4.10"},
				},
			},
		},
	}

	iaasENI := &enisdk.Eni{
		EniId:    "eni-update-error",
		VpcId:    "vpc-update-error",
		SubnetId: "subnet-update-error",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.4.11"}, // 不同的IP
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{},
	}

	// 设置mock期望返回错误
	expectedError := assert.AnError
	mockUpdater.On("Update", mock.AnythingOfType("*v2.ENI")).Return((*ccev2.ENI)(nil), expectedError)

	// When
	result := syncer.validateAndSyncENIIPs(existingENI, iaasENI)

	// Then - 即使更新失败，函数仍然返回true表示检测到了不一致
	assert.True(t, result, "Should return true even when update fails")
	mockUpdater.AssertExpectations(t)
}
