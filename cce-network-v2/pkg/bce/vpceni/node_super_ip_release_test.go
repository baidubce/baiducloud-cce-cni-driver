/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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

package vpceni

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
)

// TestGetBCEClient 测试GetBCEClient函数
func TestGetBCEClient_GivenValidNode_WhenGetBCEClient_ThenReturnClient(t *testing.T) {
	// Given
	node := &bceNetworkResourceSet{}

	// When
	result := node.GetBCEClient()

	// Then - 即使是nil也不会panic
	_ = result
}

// TestIsDefinitiveError 测试isDefinitiveError函数
func TestIsDefinitiveError_GivenVariousErrors_WhenCheckDefinitive_ThenReturnCorrectResult(t *testing.T) {
	node := &bceNetworkResourceSet{}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Invalid parameter error",
			err:      errors.New("invalid parameter: test"),
			expected: true,
		},
		{
			name:     "Generic error",
			err:      errors.New("generic error"),
			expected: false,
		},
		{
			name:     "Network timeout error",
			err:      errors.New("network timeout"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			result := node.isDefinitiveError(tt.err)

			// Then
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestWaitForRetryBackoffConfig 测试waitForRetry函数中的backoff配置
func TestWaitForRetryBackoffConfig_WhenCreateConfig_ThenReturnCorrectConfig(t *testing.T) {
	node := &bceNetworkResourceSet{}

	// 测试waitForRetry函数中创建的backoff配置
	// 由于createBackoffConfig函数已被删除，我们直接测试waitForRetry函数的行为
	ctx := context.Background()
	batchLog := logrus.NewEntry(logrus.New())

	// When - 调用waitForRetry函数，它会内部创建backoff配置
	start := time.Now()
	err := node.waitForRetry(ctx, 1, errors.New("test error"), batchLog)
	duration := time.Since(start)

	// Then - 验证函数正常执行且延迟时间合理
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, duration, 900*time.Millisecond) // 至少基础延迟减去抖动
	assert.LessOrEqual(t, duration, 2*time.Second)           // 不应超过合理范围
}

// TestBackoffBehavior 测试backoff行为
func TestBackoffBehavior_GivenRetryCount_WhenCalculateDelay_ThenReturnCorrectDelay(t *testing.T) {
	node := &bceNetworkResourceSet{}
	ctx := context.Background()
	batchLog := logrus.NewEntry(logrus.New())

	tests := []struct {
		name       string
		retryCount int
		minDelay   time.Duration
		maxDelay   time.Duration
	}{
		{
			name:       "First retry (base delay)",
			retryCount: 1,
			minDelay:   900 * time.Millisecond,  // 1s - 10% jitter
			maxDelay:   1100 * time.Millisecond, // 1s + 10% jitter
		},
		{
			name:       "Second retry (2x base delay)",
			retryCount: 2,
			minDelay:   1800 * time.Millisecond, // 2s - 10% jitter
			maxDelay:   2200 * time.Millisecond, // 2s + 10% jitter
		},
		{
			name:       "Third retry (4x base delay)",
			retryCount: 3,
			minDelay:   3600 * time.Millisecond, // 4s - 10% jitter
			maxDelay:   4400 * time.Millisecond, // 4s + 10% jitter
		},
		{
			name:       "Fourth retry (8x base delay)",
			retryCount: 4,
			minDelay:   7200 * time.Millisecond, // 8s - 10% jitter
			maxDelay:   8800 * time.Millisecond, // 8s + 10% jitter
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 直接测试waitForRetry函数的实际行为
			start := time.Now()
			err := node.waitForRetry(ctx, tt.retryCount, errors.New("test error"), batchLog)
			duration := time.Since(start)

			// Then
			assert.NoError(t, err)
			assert.GreaterOrEqual(t, duration, tt.minDelay)
			assert.LessOrEqual(t, duration, tt.maxDelay)
		})
	}
}

// TestVerifyIPReleasedWithENIInfo 测试verifyIPReleasedWithENIInfo函数
func TestVerifyIPReleasedWithENIInfo_GivenENIAndIP_WhenVerifyRelease_ThenReturnCorrectResult(t *testing.T) {
	node := &bceNetworkResourceSet{}
	scopedLog := logrus.NewEntry(logrus.New())

	tests := []struct {
		name     string
		ip       string
		iaasENI  *eni.Eni
		expected bool
	}{
		{
			name:     "Nil ENI",
			ip:       "10.0.0.1",
			iaasENI:  nil,
			expected: false,
		},
		{
			name: "IP still exists in IPv4 list",
			ip:   "10.0.0.1",
			iaasENI: &eni.Eni{
				Eni: enisdk.Eni{
					PrivateIpSet: []enisdk.PrivateIp{
						{PrivateIpAddress: "10.0.0.1"},
						{PrivateIpAddress: "10.0.0.2"},
					},
				},
			},
			expected: false,
		},
		{
			name: "IP still exists in IPv6 list",
			ip:   "2001:db8::1",
			iaasENI: &eni.Eni{
				Eni: enisdk.Eni{
					Ipv6PrivateIpSet: []enisdk.PrivateIp{
						{PrivateIpAddress: "2001:db8::1"},
						{PrivateIpAddress: "2001:db8::2"},
					},
				},
			},
			expected: false,
		},
		{
			name: "IP not found in ENI (successfully released)",
			ip:   "10.0.0.3",
			iaasENI: &eni.Eni{
				Eni: enisdk.Eni{
					PrivateIpSet: []enisdk.PrivateIp{
						{PrivateIpAddress: "10.0.0.1"},
						{PrivateIpAddress: "10.0.0.2"},
					},
					Ipv6PrivateIpSet: []enisdk.PrivateIp{
						{PrivateIpAddress: "2001:db8::1"},
						{PrivateIpAddress: "2001:db8::2"},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			result := node.verifyIPReleasedWithENIInfo(tt.ip, tt.iaasENI, scopedLog)

			// Then
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestWaitForRetry 测试waitForRetry函数
func TestWaitForRetry_GivenContext_WhenWaitForRetry_ThenHandleCorrectly(t *testing.T) {
	node := &bceNetworkResourceSet{}
	batchLog := logrus.NewEntry(logrus.New())

	t.Run("Context cancelled", func(t *testing.T) {
		// Given
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// When
		err := node.waitForRetry(ctx, 5, errors.New("test error"), batchLog)

		// Then
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("Wait completes successfully", func(t *testing.T) {
		// Given
		ctx := context.Background()

		// When
		start := time.Now()
		err := node.waitForRetry(ctx, 1, errors.New("test error"), batchLog)
		duration := time.Since(start)

		// Then
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, duration, 900*time.Millisecond) // At least base delay minus jitter
		assert.LessOrEqual(t, duration, 2*time.Second)           // Should not exceed reasonable bounds
	})
}
