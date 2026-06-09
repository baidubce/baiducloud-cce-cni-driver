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

package types

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewBackoffManager 测试NewBackoffManager函数
func TestNewBackoffManager_GivenParameters_WhenCreateManager_ThenReturnCorrectInstance(t *testing.T) {
	// Given
	maxRetries := 5
	baseDelay := 100 * time.Millisecond
	maxDelay := 5 * time.Second

	// When
	manager := NewBackoffManager(maxRetries, baseDelay, maxDelay)

	// Then
	assert.NotNil(t, manager)
	assert.Equal(t, maxRetries, manager.maxRetries)
	assert.Equal(t, baseDelay, manager.baseDelay)
	assert.Equal(t, maxDelay, manager.maxDelay)
	assert.NotNil(t, manager.backoffMap)
	assert.Empty(t, manager.backoffMap)
}

// TestBackoffManager_GetNextRetryDelay 测试GetNextRetryDelay函数
func TestBackoffManager_GetNextRetryDelay_GivenIP_WhenGetDelay_ThenReturnCorrectDelay(t *testing.T) {
	manager := NewBackoffManager(5, 100*time.Millisecond, 5*time.Second)
	ip := "10.0.0.1"

	tests := []struct {
		name         string
		setupBackoff func()
		expectedMin  time.Duration
		expectedMax  time.Duration
	}{
		{
			name:         "First retry",
			setupBackoff: func() {},
			expectedMin:  100 * time.Millisecond,
			expectedMax:  100 * time.Millisecond,
		},
		{
			name: "Second retry",
			setupBackoff: func() {
				manager.backoffMap[ip] = &BackoffState{
					RetryCount:    1,
					NextRetryTime: time.Now(),
					LastError:     errors.New("test error"),
				}
			},
			expectedMin: 200 * time.Millisecond,
			expectedMax: 200 * time.Millisecond,
		},
		{
			name: "Max delay reached",
			setupBackoff: func() {
				manager.backoffMap[ip] = &BackoffState{
					RetryCount:    10, // Large number to exceed max delay
					NextRetryTime: time.Now(),
					LastError:     errors.New("test error"),
				}
			},
			expectedMin: 5 * time.Second,
			expectedMax: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			manager.backoffMap = make(map[string]*BackoffState) // Reset
			tt.setupBackoff()

			// When
			delay := manager.GetNextRetryDelay(ip)

			// Then
			assert.GreaterOrEqual(t, delay, tt.expectedMin)
			assert.LessOrEqual(t, delay, tt.expectedMax)
		})
	}
}

// TestBackoffManager_ShouldRetry 测试ShouldRetry函数
func TestBackoffManager_ShouldRetry_GivenIPAndRetryState_WhenCheckRetry_ThenReturnCorrectResult(t *testing.T) {
	manager := NewBackoffManager(3, 100*time.Millisecond, 5*time.Second)
	ip := "10.0.0.1"

	tests := []struct {
		name         string
		setupBackoff func()
		expected     bool
	}{
		{
			name:         "No previous retry attempts",
			setupBackoff: func() {},
			expected:     true,
		},
		{
			name: "Within retry limit",
			setupBackoff: func() {
				manager.backoffMap[ip] = &BackoffState{
					RetryCount:    2,
					NextRetryTime: time.Now().Add(-1 * time.Second), // Past time
					LastError:     errors.New("test error"),
				}
			},
			expected: true,
		},
		{
			name: "Exceeded retry limit",
			setupBackoff: func() {
				manager.backoffMap[ip] = &BackoffState{
					RetryCount:    3,
					NextRetryTime: time.Now().Add(-1 * time.Second),
					LastError:     errors.New("test error"),
				}
			},
			expected: false,
		},
		{
			name: "Not yet time to retry",
			setupBackoff: func() {
				manager.backoffMap[ip] = &BackoffState{
					RetryCount:    1,
					NextRetryTime: time.Now().Add(1 * time.Hour), // Far future time
					LastError:     errors.New("test error"),
				}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			manager.backoffMap = make(map[string]*BackoffState) // Reset
			tt.setupBackoff()

			// When
			result := manager.ShouldRetry(ip)

			// Then
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBackoffManager_RecordRetry 测试RecordRetry函数
func TestBackoffManager_RecordRetry_GivenIPAndError_WhenRecordRetry_ThenUpdateBackoffState(t *testing.T) {
	manager := NewBackoffManager(5, 100*time.Millisecond, 5*time.Second)
	ip := "10.0.0.1"
	testErr := errors.New("test error")

	tests := []struct {
		name               string
		setupBackoff       func()
		expectedRetryCount int
	}{
		{
			name:               "First retry record",
			setupBackoff:       func() {},
			expectedRetryCount: 1,
		},
		{
			name: "Subsequent retry record",
			setupBackoff: func() {
				manager.backoffMap[ip] = &BackoffState{
					RetryCount:    2,
					NextRetryTime: time.Now(),
					LastError:     errors.New("previous error"),
				}
			},
			expectedRetryCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			manager.backoffMap = make(map[string]*BackoffState) // Reset
			tt.setupBackoff()

			// When
			manager.RecordRetry(ip, testErr)

			// Then
			state := manager.backoffMap[ip]
			assert.NotNil(t, state)
			assert.Equal(t, tt.expectedRetryCount, state.RetryCount)
			assert.Equal(t, testErr, state.LastError)
			assert.True(t, state.NextRetryTime.After(time.Now()))
		})
	}
}

// TestBackoffManager_ResetRetry 测试ResetRetry函数
func TestBackoffManager_ResetRetry_GivenIP_WhenResetRetry_ThenRemoveBackoffState(t *testing.T) {
	manager := NewBackoffManager(5, 100*time.Millisecond, 5*time.Second)
	ip := "10.0.0.1"

	// Given - Setup initial backoff state
	manager.backoffMap[ip] = &BackoffState{
		RetryCount:    3,
		NextRetryTime: time.Now().Add(5 * time.Second),
		LastError:     errors.New("test error"),
	}

	// When
	manager.ResetRetry(ip)

	// Then
	_, exists := manager.backoffMap[ip]
	assert.False(t, exists)
}

// TestDefaultIPReleaseConfig 测试DefaultIPReleaseConfig函数
func TestDefaultIPReleaseConfig_WhenGetDefaultConfig_ThenReturnCorrectConfig(t *testing.T) {
	// When
	config := DefaultIPReleaseConfig()

	// Then
	assert.NotNil(t, config)
	assert.Equal(t, 1000, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.BaseDelay)
	assert.Equal(t, 60*time.Second, config.MaxDelay)
	assert.True(t, config.EnableIaaSVerification)
	assert.Equal(t, 30*time.Second, config.VerificationTimeout)
	assert.True(t, config.EnableENISync)
	assert.Equal(t, 5*time.Minute, config.SyncInterval)
}

// TestIPReleaseResult 测试IPReleaseResult结构
func TestIPReleaseResult_GivenSuccessfulAndFailedIPs_WhenCreateResult_ThenContainCorrectData(t *testing.T) {
	// Given
	successfulIPs := []string{"10.0.0.1", "10.0.0.2"}
	failedIPs := []IPReleaseFailure{
		{
			IP:        "10.0.0.3",
			Error:     errors.New("network error"),
			Retryable: true,
		},
		{
			IP:        "10.0.0.4",
			Error:     errors.New("permission denied"),
			Retryable: false,
		},
	}

	// When
	result := IPReleaseResult{
		SuccessfulIPs: successfulIPs,
		FailedIPs:     failedIPs,
	}

	// Then
	assert.Equal(t, 2, len(result.SuccessfulIPs))
	assert.Equal(t, "10.0.0.1", result.SuccessfulIPs[0])
	assert.Equal(t, "10.0.0.2", result.SuccessfulIPs[1])

	assert.Equal(t, 2, len(result.FailedIPs))
	assert.Equal(t, "10.0.0.3", result.FailedIPs[0].IP)
	assert.True(t, result.FailedIPs[0].Retryable)
	assert.Equal(t, "10.0.0.4", result.FailedIPs[1].IP)
	assert.False(t, result.FailedIPs[1].Retryable)
}

// TestIPReleaseStats 测试IPReleaseStats结构
func TestIPReleaseStats_GivenStats_WhenCreateStats_ThenContainCorrectData(t *testing.T) {
	// Given & When
	stats := IPReleaseStats{
		TotalRequested:     10,
		SuccessfulReleases: 8,
		FailedReleases:     2,
		RetriedReleases:    3,
		AverageRetries:     1.5,
	}

	// Then
	assert.Equal(t, 10, stats.TotalRequested)
	assert.Equal(t, 8, stats.SuccessfulReleases)
	assert.Equal(t, 2, stats.FailedReleases)
	assert.Equal(t, 3, stats.RetriedReleases)
	assert.Equal(t, 1.5, stats.AverageRetries)
}

// TestBackoffManager_ConcurrentAccess 测试BackoffManager的并发访问
func TestBackoffManager_ConcurrentAccess_GivenConcurrentOperations_WhenAccessManager_ThenThreadSafe(t *testing.T) {
	manager := NewBackoffManager(5, 100*time.Millisecond, 5*time.Second)
	ip := "10.0.0.1"
	testErr := errors.New("test error")

	// Given - Run concurrent operations
	done := make(chan bool, 3)

	// Concurrent RecordRetry
	go func() {
		for i := 0; i < 10; i++ {
			manager.RecordRetry(ip, testErr)
		}
		done <- true
	}()

	// Concurrent ShouldRetry
	go func() {
		for i := 0; i < 10; i++ {
			manager.ShouldRetry(ip)
		}
		done <- true
	}()

	// Concurrent GetNextRetryDelay
	go func() {
		for i := 0; i < 10; i++ {
			manager.GetNextRetryDelay(ip)
		}
		done <- true
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	// Then - Should not panic and should have some state
	state := manager.backoffMap[ip]
	assert.NotNil(t, state)
	assert.GreaterOrEqual(t, state.RetryCount, 1)
}
