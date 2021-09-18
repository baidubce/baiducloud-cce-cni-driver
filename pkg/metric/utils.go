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

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	MetaInfo metaInfo
)

type metaInfo struct {
	ClusterID string
	VPCID     string
}

func SetMetricMetaInfo(
	clusterID string,
	vpcID string,
) {
	MetaInfo.ClusterID = clusterID
	MetaInfo.VPCID = vpcID
}

// MsSince returns milliseconds since start.
func MsSince(start time.Time) float64 {
	return float64(time.Since(start) / time.Millisecond)
}

// RegisterPrometheusMetrics register metrics to prometheus server
func RegisterPrometheusMetrics() {
	prometheus.Register(OpenAPILatency)

	prometheus.Register(RPCLatency)
	prometheus.Register(RPCConcurrency)
	prometheus.Register(RPCErrorCounter)
	prometheus.Register(RPCRejectedCounter)

	prometheus.Register(MultiEniMultiIPEniCount)
	prometheus.Register(MultiEniMultiIPEniIPCount)

	prometheus.Register(PrimaryEniMultiIPEniIPTotalCount)
	prometheus.Register(PrimaryEniMultiIPEniIPAllocatedCount)
	prometheus.Register(PrimaryEniMultiIPEniIPAvailableCount)

	prometheus.Register(SubnetAvailableIPCount)
}
