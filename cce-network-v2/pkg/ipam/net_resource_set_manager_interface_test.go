package ipam

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestNetResourceOperations_ReleaseIPs_InterfaceSignature_GivenInterface_WhenCheckSignature_ThenReturnsStringSliceAndError 测试ReleaseIPs接口签名
func TestNetResourceOperations_ReleaseIPs_InterfaceSignature_GivenInterface_WhenCheckSignature_ThenReturnsStringSliceAndError(t *testing.T) {
	// Given
	mockOps := &mockNetResourceOperationsForInterface{}

	// When & Then - 编译时检查接口签名
	var ops NetResourceOperations = mockOps

	// 验证ReleaseIPs方法签名正确
	ctx := context.Background()
	release := &ReleaseAction{
		IPsToRelease: []string{"10.0.0.1", "10.0.0.2"},
	}

	// 设置mock期望
	expectedIPs := []string{"10.0.0.1"}
	mockOps.On("ReleaseIPs", ctx, release).Return(expectedIPs, nil)

	// 调用方法验证签名
	releasedIPs, err := ops.ReleaseIPs(ctx, release)

	// 验证返回类型
	assert.NoError(t, err)
	assert.IsType(t, []string{}, releasedIPs, "ReleaseIPs should return []string")
	assert.Equal(t, expectedIPs, releasedIPs, "Released IPs should match expected")

	mockOps.AssertExpectations(t)
}

// TestNetResourceOperations_AllMethods_GivenInterface_WhenCheckMethods_ThenAllMethodsExist 测试接口所有方法存在
func TestNetResourceOperations_AllMethods_GivenInterface_WhenCheckMethods_ThenAllMethodsExist(t *testing.T) {
	// Given
	mockOps := &mockNetResourceOperationsForInterface{}
	var ops NetResourceOperations = mockOps

	// When & Then - 检查所有方法是否存在
	t.Run("PrepareIPAllocation method exists", func(t *testing.T) {
		var method func(*logrus.Entry) (*AllocationAction, error) = ops.PrepareIPAllocation
		assert.NotNil(t, method, "PrepareIPAllocation method should exist")
	})

	t.Run("AllocateIPs method exists", func(t *testing.T) {
		var method func(context.Context, *AllocationAction) error = ops.AllocateIPs
		assert.NotNil(t, method, "AllocateIPs method should exist")
	})

	t.Run("PrepareIPRelease method exists", func(t *testing.T) {
		var method func(int, *logrus.Entry) *ReleaseAction = ops.PrepareIPRelease
		assert.NotNil(t, method, "PrepareIPRelease method should exist")
	})

	t.Run("ReleaseIPs method exists with correct signature", func(t *testing.T) {
		// 验证ReleaseIPs方法签名：(context.Context, *ReleaseAction) ([]string, error)
		var method func(context.Context, *ReleaseAction) ([]string, error) = ops.ReleaseIPs
		assert.NotNil(t, method, "ReleaseIPs method should exist with correct signature")
	})

	t.Run("GetMaximumAllocatableIPv4 method exists", func(t *testing.T) {
		var method func() int = ops.GetMaximumAllocatableIPv4
		assert.NotNil(t, method, "GetMaximumAllocatableIPv4 method should exist")
	})

	t.Run("GetMinimumAllocatableIPv4 method exists", func(t *testing.T) {
		var method func() int = ops.GetMinimumAllocatableIPv4
		assert.NotNil(t, method, "GetMinimumAllocatableIPv4 method should exist")
	})

	t.Run("IsPrefixDelegated method exists", func(t *testing.T) {
		var method func() bool = ops.IsPrefixDelegated
		assert.NotNil(t, method, "IsPrefixDelegated method should exist")
	})

	t.Run("GetUsedIPWithPrefixes method exists", func(t *testing.T) {
		var method func() int = ops.GetUsedIPWithPrefixes
		assert.NotNil(t, method, "GetUsedIPWithPrefixes method should exist")
	})

	t.Run("GetMaximumBurstableAllocatableIPv4 method exists", func(t *testing.T) {
		var method func() int = ops.GetMaximumBurstableAllocatableIPv4
		assert.NotNil(t, method, "GetMaximumBurstableAllocatableIPv4 method should exist")
	})
}

// TestReleaseIPs_PartialSuccess_GivenMixedResults_WhenReleaseIPs_ThenReturnPartiallyReleasedIPs 测试部分成功的情况
func TestReleaseIPs_PartialSuccess_GivenMixedResults_WhenReleaseIPs_ThenReturnPartiallyReleasedIPs(t *testing.T) {
	// Given
	mockOps := &mockNetResourceOperationsForInterface{}
	ctx := context.Background()

	release := &ReleaseAction{
		IPsToRelease: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
	}

	// 模拟部分成功：只有2个IP成功释放
	expectedReleasedIPs := []string{"10.0.0.1", "10.0.0.3"}
	mockOps.On("ReleaseIPs", ctx, release).Return(expectedReleasedIPs, nil)

	// When
	releasedIPs, err := mockOps.ReleaseIPs(ctx, release)

	// Then
	assert.NoError(t, err)
	assert.Len(t, releasedIPs, 2, "Should return 2 successfully released IPs")
	assert.Contains(t, releasedIPs, "10.0.0.1", "Should contain first IP")
	assert.Contains(t, releasedIPs, "10.0.0.3", "Should contain third IP")
	assert.NotContains(t, releasedIPs, "10.0.0.2", "Should not contain failed IP")

	mockOps.AssertExpectations(t)
}

// TestReleaseIPs_EmptyRequest_GivenEmptyIPList_WhenReleaseIPs_ThenReturnEmptyList 测试空请求的情况
func TestReleaseIPs_EmptyRequest_GivenEmptyIPList_WhenReleaseIPs_ThenReturnEmptyList(t *testing.T) {
	// Given
	mockOps := &mockNetResourceOperationsForInterface{}
	ctx := context.Background()

	release := &ReleaseAction{
		IPsToRelease: []string{},
	}

	mockOps.On("ReleaseIPs", ctx, release).Return([]string{}, nil)

	// When
	releasedIPs, err := mockOps.ReleaseIPs(ctx, release)

	// Then
	assert.NoError(t, err)
	assert.Empty(t, releasedIPs, "Should return empty slice for empty request")

	mockOps.AssertExpectations(t)
}

// TestReleaseIPs_AllFailed_GivenFailedRelease_WhenReleaseIPs_ThenReturnEmptyListWithError 测试全部失败的情况
func TestReleaseIPs_AllFailed_GivenFailedRelease_WhenReleaseIPs_ThenReturnEmptyListWithError(t *testing.T) {
	// Given
	mockOps := &mockNetResourceOperationsForInterface{}
	ctx := context.Background()

	release := &ReleaseAction{
		IPsToRelease: []string{"10.0.0.1", "10.0.0.2"},
	}

	expectedError := assert.AnError
	mockOps.On("ReleaseIPs", ctx, release).Return([]string{}, expectedError)

	// When
	releasedIPs, err := mockOps.ReleaseIPs(ctx, release)

	// Then
	assert.Error(t, err, "Should return error when all releases fail")
	assert.Equal(t, expectedError, err, "Should return expected error")
	assert.Empty(t, releasedIPs, "Should return empty slice when all fail")

	mockOps.AssertExpectations(t)
}

// Mock implementation for testing the interface
type mockNetResourceOperationsForInterface struct {
	mock.Mock
}

func (m *mockNetResourceOperationsForInterface) PrepareIPAllocation(scopedLog *logrus.Entry) (*AllocationAction, error) {
	args := m.Called(scopedLog)
	return args.Get(0).(*AllocationAction), args.Error(1)
}

func (m *mockNetResourceOperationsForInterface) AllocateIPs(ctx context.Context, allocation *AllocationAction) error {
	args := m.Called(ctx, allocation)
	return args.Error(0)
}

func (m *mockNetResourceOperationsForInterface) PrepareIPRelease(excessIPs int, scopedLog *logrus.Entry) *ReleaseAction {
	args := m.Called(excessIPs, scopedLog)
	return args.Get(0).(*ReleaseAction)
}

func (m *mockNetResourceOperationsForInterface) ReleaseIPs(ctx context.Context, release *ReleaseAction) ([]string, error) {
	args := m.Called(ctx, release)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockNetResourceOperationsForInterface) GetMaximumAllocatableIPv4() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockNetResourceOperationsForInterface) GetMinimumAllocatableIPv4() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockNetResourceOperationsForInterface) IsPrefixDelegated() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockNetResourceOperationsForInterface) GetUsedIPWithPrefixes() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockNetResourceOperationsForInterface) GetMaximumBurstableAllocatableIPv4() int {
	args := m.Called()
	return args.Int(0)
}
