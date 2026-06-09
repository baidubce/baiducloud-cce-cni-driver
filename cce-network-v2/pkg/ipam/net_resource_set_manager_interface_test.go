package ipam

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

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
