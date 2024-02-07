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
package rate

import (
	"context"
	"sync"

	metric "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
)

// ServiceLimiterManager manages ratelimiting for a set of services.
type ServiceLimiterManager interface {
	Wait(ctx context.Context, name string) (LimitedRequest, error)
	Limiter(name string) *APILimiter
}

// DefaultAPILimiterSet returns a APILimiterSet that is a copy of the passed in set.
// create new APILimiter with the default param if APILimiter is not exsits when
// the `Limiter()` function is called.
type DefaultAPILimiterSet struct {
	sync.RWMutex
	*APILimiterSet
	defaultParam *APILimiterParameters
}

// APILimiterSetWrapDefault creates a new APILimiterSet based on a set of rate limiting
// configurations and the default configuration. Any rate limiter that is
// configured in the config OR the defaults will be configured and made
// available via the Limiter(name) and Wait() function.
func APILimiterSetWrapDefault(limiterSet *APILimiterSet, defaultParam *APILimiterParameters) *DefaultAPILimiterSet {
	return &DefaultAPILimiterSet{
		APILimiterSet: limiterSet,
		defaultParam:  defaultParam,
	}
}

// Limiter returns the APILimiter with a given name
func (s *DefaultAPILimiterSet) Limiter(name string) *APILimiter {
	s.RLock()
	limiter, ok := s.limiters[name]
	s.RUnlock()
	if ok {
		return limiter
	}
	s.Lock()
	defer s.Unlock()
	newLimiter := NewAPILimiter(name, *s.defaultParam, s.metrics)
	s.limiters[name] = newLimiter
	return newLimiter
}

// SimpleMetricsObserver sets the metrics observer to a safe value
type simpleMetricsObserver struct{}

func (a *simpleMetricsObserver) ProcessedRequest(name string, v MetricsValues) {
	metric.APILimiterRequestsInFlight.WithLabelValues(name, "in-flight").Set(float64(v.CurrentRequestsInFlight))
	metric.APILimiterRequestsInFlight.WithLabelValues(name, "limit").Set(float64(v.ParallelRequests))
	metric.APILimiterRateLimit.WithLabelValues(name, "limit").Set(float64(v.Limit))
	metric.APILimiterRateLimit.WithLabelValues(name, "burst").Set(float64(v.Burst))
	metric.APILimiterAdjustmentFactor.WithLabelValues(name).Set(v.AdjustmentFactor)

	if v.Outcome == "" {
		v.Outcome = metric.Error2Outcome(v.Error)
		metric.APILimiterProcessHistoryDuration.WithLabelValues(name, "process", v.Outcome).Observe(float64(v.ProcessDuration.Milliseconds()))
	}
	metric.APILimiterProcessHistoryDuration.WithLabelValues(name, "wait", v.Outcome).Observe(float64(v.WaitDuration.Milliseconds()))
	metric.APILimiterProcessHistoryDuration.WithLabelValues(name, "total", v.Outcome).Observe(float64(v.WaitDuration.Milliseconds()))
	metric.APILimiterProcessedRequests.WithLabelValues(name, v.Outcome).Inc()
}

// SimpleMetricsObserver sets the metrics observer to a safe value
var SimpleMetricsObserver MetricsObserver = &simpleMetricsObserver{}
