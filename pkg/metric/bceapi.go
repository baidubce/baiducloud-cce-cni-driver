/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package metric

import "github.com/prometheus/client_golang/prometheus"

var (
	// OpenAPILatency bce open api latency
	OpenAPILatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bce_openapi_latency",
			Help:    "bce openapi latency in ms",
			Buckets: []float64{50, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 14800, 16800, 20800, 28800, 44800},
		},
		[]string{"cluster", "api", "error", "code"},
	)
)
