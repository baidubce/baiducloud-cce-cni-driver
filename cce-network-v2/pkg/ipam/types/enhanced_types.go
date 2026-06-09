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
	"sync"
	"time"
)

// ErrorType defines the type of errors for classification
type ErrorType string

const (
	ErrorTypeNone             ErrorType = "none"
	ErrorTypeTimeout          ErrorType = "timeout"
	ErrorTypeRateLimit        ErrorType = "rate_limit"
	ErrorTypeInvalidParameter ErrorType = "invalid_parameter"
	ErrorTypeNotFound         ErrorType = "not_found"
	ErrorTypeUnknown          ErrorType = "unknown"
)

// IPReleaseResult represents the result of IP release operation
type IPReleaseResult struct {
	SuccessfulIPs []string
	FailedIPs     []IPReleaseFailure
}

// IPReleaseFailure represents a failed IP release
type IPReleaseFailure struct {
	IP        string
	Error     error
	Retryable bool
}

// BackoffState represents the backoff state for a specific IP
type BackoffState struct {
	RetryCount    int
	NextRetryTime time.Time
	LastError     error
}

// BackoffManager manages exponential backoff for IP release retries
type BackoffManager struct {
	mutex      sync.RWMutex
	backoffMap map[string]*BackoffState
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

// NewBackoffManager creates a new BackoffManager
func NewBackoffManager(maxRetries int, baseDelay, maxDelay time.Duration) *BackoffManager {
	return &BackoffManager{
		backoffMap: make(map[string]*BackoffState),
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
	}
}

// GetNextRetryDelay calculates the next retry delay for an IP
func (bm *BackoffManager) GetNextRetryDelay(ip string) time.Duration {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	return bm.getNextRetryDelayLocked(ip)
}

// getNextRetryDelayLocked calculates the next retry delay for an IP (must be called with lock held)
func (bm *BackoffManager) getNextRetryDelayLocked(ip string) time.Duration {
	state := bm.backoffMap[ip]
	if state == nil {
		return bm.baseDelay
	}

	delay := bm.baseDelay * time.Duration(1<<state.RetryCount)
	if delay > bm.maxDelay {
		delay = bm.maxDelay
	}
	return delay
}

// ShouldRetry determines if an IP should be retried
func (bm *BackoffManager) ShouldRetry(ip string) bool {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	state := bm.backoffMap[ip]
	if state == nil {
		return true
	}

	// Check both retry count and time
	if state.RetryCount >= bm.maxRetries {
		return false
	}

	return time.Now().After(state.NextRetryTime)
}

// RecordRetry records a retry attempt for an IP
func (bm *BackoffManager) RecordRetry(ip string, err error) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	state := bm.backoffMap[ip]
	if state == nil {
		state = &BackoffState{}
		bm.backoffMap[ip] = state
	}

	state.RetryCount++
	state.LastError = err
	state.NextRetryTime = time.Now().Add(bm.getNextRetryDelayLocked(ip))
}

// ResetRetry resets the retry state for an IP
func (bm *BackoffManager) ResetRetry(ip string) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	delete(bm.backoffMap, ip)
}

// IPReleaseConfig contains configuration for IP release operations
type IPReleaseConfig struct {
	MaxRetries             int           `json:"maxRetries"`
	BaseDelay              time.Duration `json:"baseDelay"`
	MaxDelay               time.Duration `json:"maxDelay"`
	EnableIaaSVerification bool          `json:"enableIaaSVerification"`
	VerificationTimeout    time.Duration `json:"verificationTimeout"`
	EnableENISync          bool          `json:"enableENISync"`
	SyncInterval           time.Duration `json:"syncInterval"`
}

// DefaultIPReleaseConfig returns default configuration
func DefaultIPReleaseConfig() *IPReleaseConfig {
	return &IPReleaseConfig{
		MaxRetries:             1000, // 设置很大的值，实际上会一直重试直到满足退出条件
		BaseDelay:              1 * time.Second,
		MaxDelay:               60 * time.Second, // 最大延迟60秒
		EnableIaaSVerification: true,
		VerificationTimeout:    30 * time.Second,
		EnableENISync:          true,
		SyncInterval:           5 * time.Minute,
	}
}

// IPReleaseStats contains statistics for IP release operations
type IPReleaseStats struct {
	TotalRequested     int     `json:"totalRequested"`
	SuccessfulReleases int     `json:"successfulReleases"`
	FailedReleases     int     `json:"failedReleases"`
	RetriedReleases    int     `json:"retriedReleases"`
	AverageRetries     float64 `json:"averageRetries"`
}
