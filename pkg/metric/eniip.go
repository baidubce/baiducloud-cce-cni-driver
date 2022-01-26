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

// bcc multi eni mode
var (
	MultiEniMultiIPEniCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multi_eni_multi_ip_eni_count",
			Help: "eni count of bcc",
		},
		[]string{"cluster", "vpc", "subnet", "eni_status"},
	)

	MultiEniMultiIPEniIPCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multi_eni_multi_ip_eniip_count",
			Help: "eni ip count of bcc (exclude primary ip)",
		},
		[]string{"cluster", "vpc", "subnet"},
	)
)

// bbc primary eni mode
var (
	PrimaryEniMultiIPEniIPTotalCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "primary_eni_multi_ip_eniip_total_count",
			Help: "count of total ip on primary eni",
		},
		[]string{"cluster", "vpc", "node", "subnet"},
	)

	PrimaryEniMultiIPEniIPAllocatedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "primary_eni_multi_ip_eniip_allocated_count",
			Help: "count of allocated ip on primary eni",
		},
		[]string{"cluster", "vpc", "node", "subnet"},
	)

	PrimaryEniMultiIPEniIPAvailableCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "primary_eni_multi_ip_eniip_available_count",
			Help: "count of available ip on primary eni",
		},
		[]string{"cluster", "vpc", "node", "subnet"},
	)
)

var (
	SubnetAvailableIPCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "subnet_available_ip_count",
			Help: "subnet available ip count",
		},
		[]string{"cluster", "vpc", "zone", "subnet"},
	)
)
