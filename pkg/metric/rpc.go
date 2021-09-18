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
	RPCLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cni_rpc_latency",
			Help:    "cni rpc latency in ms",
			Buckets: prometheus.ExponentialBuckets(50, 2, 12),
		},
		[]string{"cluster", "ip_type", "rpc_api", "error"},
	)

	RPCConcurrency = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cni_rpc_concurrency",
			Help: "cni rpc concurrency",
		},
		[]string{"cluster", "ip_type", "rpc_api"},
	)

	RPCErrorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cni_rpc_error_count",
			Help: "cni rpc error count",
		},
		[]string{"cluster", "ip_type", "rpc_api"},
	)

	RPCRejectedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cni_rpc_rejected_count",
			Help: "cni rpc rejected count",
		},
		[]string{"cluster", "ip_type", "rpc_api"},
	)
)
