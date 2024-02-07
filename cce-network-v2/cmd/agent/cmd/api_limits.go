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

package cmd

import (
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
)

const (
	apiRequestPostENI            = "cni/enim/postENI"
	apiRequestDeleteENI          = "cni/enim/deleteENI"
	apiRequestPostIPAM           = "cni/ipam/post"
	apiRequestDeleteIPAMIP       = "cni/ipam/deleteIP"
	apiRequestPostIPAMIP         = "cni/ipam/postIP"
	apiRequestGetExtPluginStatus = "cni/endpoint/getExtPluginStatus"
	apiRequestPutEndpointProbe   = "cni/endpoint/probe"
)

var apiRateLimitDefaults = map[string]rate.APILimiterParameters{
	apiRequestPostENI: {
		AutoAdjust:                  true,
		EstimatedProcessingDuration: time.Second,
		RateLimit:                   8,
		RateBurst:                   8,
		ParallelRequests:            1,
		SkipInitial:                 1,
		MaxWaitDuration:             30 * time.Second,
	},
	// No maximum wait time is enforced as delete calls should always
	// succeed. Permit a large number of parallel requests to minimize
	// latency of delete calls, if the system performance allows for it,
	// the maximum number of parallel requests will grow to a larger number
	// but it will never shrink below 4. Logging is enabled for visibility
	// as frequency should be low.
	apiRequestDeleteENI: {
		EstimatedProcessingDuration: 200 * time.Millisecond,
		AutoAdjust:                  true,
		ParallelRequests:            1,
		RateLimit:                   8,
		RateBurst:                   8,
	},
	apiRequestPostIPAM: {
		AutoAdjust:                  true,
		EstimatedProcessingDuration: time.Second,
		RateLimit:                   10,
		RateBurst:                   15,
		ParallelRequests:            10,
		SkipInitial:                 1,
		MaxWaitDuration:             30 * time.Second,
	},
	apiRequestDeleteIPAMIP: {
		AutoAdjust:                  true,
		EstimatedProcessingDuration: 200 * time.Millisecond,
		RateLimit:                   10,
		RateBurst:                   15,
		ParallelRequests:            10,
		SkipInitial:                 1,
		MaxWaitDuration:             30 * time.Second,
	},
	apiRequestGetExtPluginStatus: {
		RateLimit:        10,
		RateBurst:        10,
		ParallelRequests: 10,
		MaxWaitDuration:  30 * time.Second,
	},
	apiRequestPutEndpointProbe: {
		RateLimit:        100,
		RateBurst:        100,
		ParallelRequests: 100,
		MaxWaitDuration:  200 * time.Second,
	},
}
