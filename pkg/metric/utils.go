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
	"context"
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
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
func RegisterPrometheusMetrics(ctx context.Context) {
	var collectors []interface{}
	collectors = append(collectors, MEMUsagePercent)

	collectors = append(collectors, OpenAPILatency)

	collectors = append(collectors, RPCLatency)
	collectors = append(collectors, RPCConcurrency)
	collectors = append(collectors, RPCErrorCounter)
	collectors = append(collectors, RPCRejectedCounter)

	collectors = append(collectors, RPCPerPodLatency)
	collectors = append(collectors, RPCPerPodLockLatency)

	collectors = append(collectors, MultiEniMultiIPEniCount)
	collectors = append(collectors, MultiEniMultiIPEniIPCount)

	collectors = append(collectors, PrimaryEniMultiIPEniIPTotalCount)
	collectors = append(collectors, PrimaryEniMultiIPEniIPAllocatedCount)
	collectors = append(collectors, PrimaryEniMultiIPEniIPAvailableCount)

	collectors = append(collectors, SubnetAvailableIPCount)

	var err error
	for _, collector := range collectors {
		err = prometheus.Register(collector.(prometheus.Collector))
		if err != nil {
			log.Fatalf(ctx, "register collector %v failed: %v", reflect.TypeOf(collector), err)
		}
	}
}
