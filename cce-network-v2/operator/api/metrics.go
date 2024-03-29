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

package api

import (
	"github.com/go-openapi/runtime/middleware"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/operator/server/restapi/metrics"
	opMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
)

type getMetrics struct {
	*Server
}

// NewGetMetricsHandler handles metrics requests.
func NewGetMetricsHandler(s *Server) metrics.GetMetricsHandler {
	return &getMetrics{Server: s}
}

// Handle handles GET requests for /metrics/ .
func (h *getMetrics) Handle(params metrics.GetMetricsParams) middleware.Responder {
	m, err := opMetrics.DumpMetrics()
	if err != nil {
		return metrics.NewGetMetricsFailed()
	}

	return metrics.NewGetMetricsOK().WithPayload(m)
}
