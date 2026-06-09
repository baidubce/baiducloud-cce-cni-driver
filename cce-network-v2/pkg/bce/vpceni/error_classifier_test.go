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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ErrorType 定义错误类型
type ErrorType int

const (
	ErrorTypeTimeout ErrorType = iota
	ErrorTypeRateLimit
	ErrorTypeInvalidParameter
	ErrorTypeNotFound
	ErrorTypeUnknown
)

// ClassifyError 分类错误类型
func ClassifyError(err error) ErrorType {
	if err == nil {
		return ErrorTypeUnknown
	}

	errStr := err.Error()

	// 超时错误
	if containsAny(errStr, []string{"timeout", "deadline exceeded", "context deadline exceeded"}) {
		return ErrorTypeTimeout
	}

	// 限流错误
	if containsAny(errStr, []string{"rate limit", "throttle", "qps limit", "too many requests"}) {
		return ErrorTypeRateLimit
	}

	// 参数错误
	if containsAny(errStr, []string{"invalid parameter", "bad request", "invalid argument"}) {
		return ErrorTypeInvalidParameter
	}

	// 资源不存在错误
	if containsAny(errStr, []string{"not found", "does not exist", "not exist"}) {
		return ErrorTypeNotFound
	}

	return ErrorTypeUnknown
}

// IsRetryable 判断错误是否可重试
func IsRetryable(errorType ErrorType) bool {
	switch errorType {
	case ErrorTypeTimeout, ErrorTypeRateLimit:
		return true
	default:
		return false
	}
}

// containsAny 检查字符串是否包含任意一个子字符串
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// TestClassifyError_Timeout 测试超时错误的分类
func TestClassifyError_Timeout(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorType
	}{
		{
			name:     "context deadline exceeded",
			err:      errors.New("context deadline exceeded"),
			expected: ErrorTypeTimeout,
		},
		{
			name:     "timeout error",
			err:      errors.New("timeout"),
			expected: ErrorTypeTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestClassifyError_RateLimit 测试限流错误的分类
func TestClassifyError_RateLimit(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorType
	}{
		{
			name:     "rate limit exceeded",
			err:      errors.New("rate limit exceeded"),
			expected: ErrorTypeRateLimit,
		},
		{
			name:     "throttle error",
			err:      errors.New("throttle"),
			expected: ErrorTypeRateLimit,
		},
		{
			name:     "qps limit",
			err:      errors.New("qps limit"),
			expected: ErrorTypeRateLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestClassifyError_InvalidParameter 测试参数错误的分类
func TestClassifyError_InvalidParameter(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorType
	}{
		{
			name:     "invalid parameter",
			err:      errors.New("invalid parameter"),
			expected: ErrorTypeInvalidParameter,
		},
		{
			name:     "bad request",
			err:      errors.New("bad request"),
			expected: ErrorTypeInvalidParameter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestClassifyError_NotFound 测试资源不存在错误的分类
func TestClassifyError_NotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorType
	}{
		{
			name:     "not found",
			err:      errors.New("not found"),
			expected: ErrorTypeNotFound,
		},
		{
			name:     "does not exist",
			err:      errors.New("does not exist"),
			expected: ErrorTypeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestClassifyError_Unknown 测试未知错误的分类
func TestClassifyError_Unknown(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorType
	}{
		{
			name:     "unknown error",
			err:      errors.New("unknown error"),
			expected: ErrorTypeUnknown,
		},
		{
			name:     "internal error",
			err:      errors.New("internal error"),
			expected: ErrorTypeUnknown,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: ErrorTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsRetryable 测试错误类型的重试判断
func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		errorType ErrorType
		expected  bool
	}{
		{
			name:      "timeout error is retryable",
			errorType: ErrorTypeTimeout,
			expected:  true,
		},
		{
			name:      "rate limit error is retryable",
			errorType: ErrorTypeRateLimit,
			expected:  true,
		},
		{
			name:      "invalid parameter error is not retryable",
			errorType: ErrorTypeInvalidParameter,
			expected:  false,
		},
		{
			name:      "not found error is not retryable",
			errorType: ErrorTypeNotFound,
			expected:  false,
		},
		{
			name:      "unknown error is not retryable",
			errorType: ErrorTypeUnknown,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryable(tt.errorType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
