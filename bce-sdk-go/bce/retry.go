/*
 * Copyright 2017 Baidu, Inc.
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
 */

// retry.go - define the retry policy when making requests to BCE services

package bce

import (
	"math/rand"
	"net"
	"time"

	"github.com/baidubce/bce-sdk-go/util/log"
)

// RetryPolicy defines the two methods to retry for sending request.
type RetryPolicy interface {
	ShouldRetry(BceError, int) bool
	GetDelayBeforeNextRetryInMillis(BceError, int) time.Duration
}

// NoRetryPolicy just does not retry.
type NoRetryPolicy struct{}

func (_ *NoRetryPolicy) ShouldRetry(err BceError, attempts int) bool {
	return false
}

func (_ *NoRetryPolicy) GetDelayBeforeNextRetryInMillis(
	err BceError, attempts int) time.Duration {
	return 0 * time.Millisecond
}

func NewNoRetryPolicy() *NoRetryPolicy {
	return &NoRetryPolicy{}
}

// BackOffRetryPolicy implements a policy that retries with exponential back-off strategy.
// This policy will keep retrying until the maximum number of retries is reached. The delay time
// will be a fixed interval for the first time then 2 * interval for the second, 4 * internal for
// the third, and so on.
// In general, the delay time will be 2^number_of_retries_attempted*interval. When a maximum of
// delay time is specified, the delay time will never exceed this limit.
type BackOffRetryPolicy struct {
	maxErrorRetry        int
	maxDelayInMillis     int64
	baseIntervalInMillis int64
}

func (b *BackOffRetryPolicy) ShouldRetry(err BceError, attempts int) bool {
	// Do not retry any more when retry the max times
	if attempts >= b.maxErrorRetry {
		return false
	}

	if err == nil {
		return false
	}

	// Always retry on IO error
	if _, ok := err.(net.Error); ok {
		return true
	}

	// Only retry on a service error
	if realErr, ok := err.(*BceServiceError); ok {
		// Retry on RequestExpired
		if realErr.Code == EREQUEST_EXPIRED {
			log.Warn("retry for request expired")
			return true
		}

		// Retry on RateLimit (421)
		if realErr.StatusCode == 421 || realErr.Code == "RateLimit" {
			log.Warn("retry for rate limit")
			return true
		}

		// Retry on all 5xx errors
		if realErr.StatusCode >= 500 && realErr.StatusCode < 600 {
			log.Warn("retry for 5xx error")
			return true
		}
	}
	return false
}

func (b *BackOffRetryPolicy) GetDelayBeforeNextRetryInMillis(
	err BceError, attempts int) time.Duration {
	if attempts < 0 {
		return 0 * time.Millisecond
	}
	// Exponential backoff
	delayInMillis := (1 << uint64(attempts)) * b.baseIntervalInMillis
	if delayInMillis > b.maxDelayInMillis {
		delayInMillis = b.maxDelayInMillis
	}
	// Add jitter: random value between 0.8x and 1.2x (±20%)
	jitter := 0.8 + rand.Float64()*0.4 // 0.8 to 1.2
	delayWithJitter := int64(float64(delayInMillis) * jitter)
	return time.Duration(delayWithJitter) * time.Millisecond
}

func NewBackOffRetryPolicy(maxRetry int, maxDelay, base int64) *BackOffRetryPolicy {
	return &BackOffRetryPolicy{maxRetry, maxDelay, base}
}
