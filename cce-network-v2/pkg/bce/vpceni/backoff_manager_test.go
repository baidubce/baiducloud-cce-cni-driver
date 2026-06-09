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
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// BackoffManager 退避管理器
type BackoffManager struct {
	baseDelay   time.Duration
	maxDelay    time.Duration
	maxRetries  int
	retryCount  int
	lastAttempt time.Time
}

// NewBackoffManager 创建新的退避管理器
func NewBackoffManager(baseDelay, maxDelay time.Duration, maxRetries int) *BackoffManager {
	return &BackoffManager{
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
		maxRetries: maxRetries,
		retryCount: 0,
	}
}

// GetNextRetryDelay 获取下一次重试的延迟时间
func (bm *BackoffManager) GetNextRetryDelay() time.Duration {
	if bm.retryCount == 0 {
		return bm.baseDelay
	}

	// 指数退避：delay = baseDelay * 2^(retryCount-1)
	delay := bm.baseDelay
	for i := 1; i < bm.retryCount; i++ {
		delay *= 2
		if delay > bm.maxDelay {
			delay = bm.maxDelay
			break
		}
	}

	// 添加抖动避免雷群效应 (±10%)
	jitter := time.Duration(float64(delay) * 0.1 * (0.5 - rand.Float64()))
	return delay + jitter
}

// ShouldRetry 判断是否应该重试
func (bm *BackoffManager) ShouldRetry() bool {
	return bm.retryCount < bm.maxRetries
}

// RecordRetry 记录重试
func (bm *BackoffManager) RecordRetry() {
	bm.retryCount++
	bm.lastAttempt = time.Now()
}

// ResetRetry 重置重试状态
func (bm *BackoffManager) ResetRetry() {
	bm.retryCount = 0
	bm.lastAttempt = time.Time{}
}

// TestNewBackoffManager 测试创建退避管理器
func TestNewBackoffManager(t *testing.T) {
	baseDelay := time.Second
	maxDelay := 60 * time.Second
	maxRetries := 5

	bm := NewBackoffManager(baseDelay, maxDelay, maxRetries)

	assert.Equal(t, baseDelay, bm.baseDelay)
	assert.Equal(t, maxDelay, bm.maxDelay)
	assert.Equal(t, maxRetries, bm.maxRetries)
	assert.Equal(t, 0, bm.retryCount)
	assert.True(t, bm.lastAttempt.IsZero())
}

// TestGetNextRetryDelay 测试重试延迟计算
func TestGetNextRetryDelay(t *testing.T) {
	baseDelay := time.Second
	maxDelay := 60 * time.Second
	maxRetries := 5

	bm := NewBackoffManager(baseDelay, maxDelay, maxRetries)

	// 首次重试：应返回基础延迟（没有抖动）
	delay := bm.GetNextRetryDelay()
	assert.Equal(t, baseDelay, delay)

	// 记录重试
	bm.RecordRetry()

	// 第二次重试：应返回基础延迟（考虑抖动范围）
	delay = bm.GetNextRetryDelay()
	expectedDelay := baseDelay
	jitterRange := time.Duration(float64(expectedDelay) * 0.1)
	assert.GreaterOrEqual(t, delay, expectedDelay-jitterRange)
	assert.LessOrEqual(t, delay, expectedDelay+jitterRange)

	// 记录重试
	bm.RecordRetry()

	// 第三次重试：应返回2倍基础延迟（考虑抖动范围）
	delay = bm.GetNextRetryDelay()
	expectedDelay = baseDelay * 2
	jitterRange = time.Duration(float64(expectedDelay) * 0.1)
	assert.GreaterOrEqual(t, delay, expectedDelay-jitterRange)
	assert.LessOrEqual(t, delay, expectedDelay+jitterRange)

	// 记录重试
	bm.RecordRetry()

	// 第四次重试：应返回4倍基础延迟（考虑抖动范围）
	delay = bm.GetNextRetryDelay()
	expectedDelay = baseDelay * 4
	jitterRange = time.Duration(float64(expectedDelay) * 0.1)
	assert.GreaterOrEqual(t, delay, expectedDelay-jitterRange)
	assert.LessOrEqual(t, delay, expectedDelay+jitterRange)

	// 记录重试
	bm.RecordRetry()

	// 第五次重试：应返回8倍基础延迟（考虑抖动范围）
	delay = bm.GetNextRetryDelay()
	expectedDelay = baseDelay * 8
	jitterRange = time.Duration(float64(expectedDelay) * 0.1)
	assert.GreaterOrEqual(t, delay, expectedDelay-jitterRange)
	assert.LessOrEqual(t, delay, expectedDelay+jitterRange)
}

// TestGetNextRetryDelay_MaxDelay 测试最大延迟限制
func TestGetNextRetryDelay_MaxDelay(t *testing.T) {
	baseDelay := time.Second
	maxDelay := 5 * time.Second
	maxRetries := 10

	bm := NewBackoffManager(baseDelay, maxDelay, maxRetries)

	// 设置重试次数为10，应该达到最大延迟
	bm.retryCount = 10

	delay := bm.GetNextRetryDelay()
	// 考虑抖动，延迟应该不超过最大延迟加上抖动范围
	jitterRange := time.Duration(float64(maxDelay) * 0.1)
	assert.LessOrEqual(t, delay, maxDelay+jitterRange)
}

// TestShouldRetry 测试是否应该重试
func TestShouldRetry(t *testing.T) {
	baseDelay := time.Second
	maxDelay := 60 * time.Second
	maxRetries := 3

	bm := NewBackoffManager(baseDelay, maxDelay, maxRetries)

	// 未达到最大重试次数：返回true
	assert.True(t, bm.ShouldRetry())

	// 记录重试
	bm.RecordRetry()
	assert.True(t, bm.ShouldRetry())

	// 记录重试
	bm.RecordRetry()
	assert.True(t, bm.ShouldRetry())

	// 记录重试
	bm.RecordRetry()
	assert.False(t, bm.ShouldRetry())
}

// TestRecordRetry 测试记录重试
func TestRecordRetry(t *testing.T) {
	baseDelay := time.Second
	maxDelay := 60 * time.Second
	maxRetries := 5

	bm := NewBackoffManager(baseDelay, maxDelay, maxRetries)

	// 初始状态
	assert.Equal(t, 0, bm.retryCount)
	assert.True(t, bm.lastAttempt.IsZero())

	// 记录重试
	bm.RecordRetry()
	assert.Equal(t, 1, bm.retryCount)
	assert.False(t, bm.lastAttempt.IsZero())

	// 再次记录重试
	bm.RecordRetry()
	assert.Equal(t, 2, bm.retryCount)
}

// TestResetRetry 测试重置重试状态
func TestResetRetry(t *testing.T) {
	baseDelay := time.Second
	maxDelay := 60 * time.Second
	maxRetries := 5

	bm := NewBackoffManager(baseDelay, maxDelay, maxRetries)

	// 记录一些重试
	bm.RecordRetry()
	bm.RecordRetry()
	bm.RecordRetry()

	assert.Equal(t, 3, bm.retryCount)
	assert.False(t, bm.lastAttempt.IsZero())

	// 重置重试状态
	bm.ResetRetry()

	assert.Equal(t, 0, bm.retryCount)
	assert.True(t, bm.lastAttempt.IsZero())
}
