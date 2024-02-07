//go:build !privileged_tests

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

package metrics

import (
	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/health/client/connectivity"
	healthModels "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/health/models"
)

type StatusCollectorTest struct{}

var _ = Suite(&StatusCollectorTest{})

var sampleSingleClusterConnectivityResponse = &connectivity.GetStatusOK{
	Payload: &healthModels.HealthStatusResponse{
		Local: &healthModels.SelfStatus{
			Name: "kind-worker",
		},
		Nodes: []*healthModels.NodeStatus{
			{
				HealthEndpoint: &healthModels.EndpointStatus{
					PrimaryAddress: &healthModels.PathStatus{
						HTTP: &healthModels.ConnectivityStatus{
							Latency: 212100,
						},
						Icmp: &healthModels.ConnectivityStatus{
							Latency: 672600,
						},
						IP: "10.244.3.219",
					},
					SecondaryAddresses: []*healthModels.PathStatus{
						{
							HTTP: &healthModels.ConnectivityStatus{
								Latency: 212101,
							},
							Icmp: &healthModels.ConnectivityStatus{
								Latency: 672601,
							},
							IP: "10.244.3.220",
						},
						{
							HTTP: &healthModels.ConnectivityStatus{
								Latency: 212102,
							},
							Icmp: &healthModels.ConnectivityStatus{
								Latency: 672602,
							},
							IP: "10.244.3.221",
						},
					},
				},
				Host: &healthModels.HostStatus{
					PrimaryAddress: &healthModels.PathStatus{
						HTTP: &healthModels.ConnectivityStatus{
							Latency: 165362,
						},
						Icmp: &healthModels.ConnectivityStatus{
							Latency: 704179,
						},
						IP: "172.18.0.3",
					},
					SecondaryAddresses: nil,
				},
				Name: "kind-worker",
			},
		},
	},
}

const expectedStatusMetric = `
# HELP cce_controllers_failing Number of failing controllers
# TYPE cce_controllers_failing gauge
cce_controllers_failing 1
# HELP cce_ip_addresses Number of allocated IP addresses
# TYPE cce_ip_addresses gauge
cce_ip_addresses{family="ipv4"} 3
cce_ip_addresses{family="ipv6"} 3
# HELP cce_unreachable_health_endpoints Number of health endpoints that cannot be reached
# TYPE cce_unreachable_health_endpoints gauge
cce_unreachable_health_endpoints 0
# HELP cce_unreachable_nodes Number of nodes that cannot be reached
# TYPE cce_unreachable_nodes gauge
cce_unreachable_nodes 0
`

type fakeConnectivityClient struct {
	response *connectivity.GetStatusOK
}

func (f *fakeConnectivityClient) GetStatus(params *connectivity.GetStatusParams) (*connectivity.GetStatusOK, error) {
	return f.response, nil
}

func (s *StatusCollectorTest) Test_statusCollector_Collect(c *C) {
	tests := []struct {
		name                 string
		connectivityResponse *connectivity.GetStatusOK
		expectedMetric       string
		expectedCount        int
	}{
		{
			name:                 "check status metrics",
			connectivityResponse: sampleSingleClusterConnectivityResponse,
			expectedCount:        5,
			expectedMetric:       expectedStatusMetric,
		},
	}

	for _, tt := range tests {
		c.Log("Test :", tt.name)

	}

}
