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
	"github.com/prometheus/client_golang/prometheus"
	. "gopkg.in/check.v1"
)

func (s *MetricsSuite) TestGaugeWithThreshold(c *C) {
	threshold := 1.0
	underThreshold := threshold - 0.5
	overThreshold := threshold + 0.5
	gauge := NewGaugeWithThreshold(
		"test_metric",
		"test_subsystem",
		"test_metric",
		map[string]string{
			"test_label": "test_value",
		},
		threshold,
	)

	metrics, err := registry.Gather()
	c.Assert(err, IsNil)
	initMetricLen := len(metrics)

	gauge.Set(underThreshold)
	metrics, err = registry.Gather()
	c.Assert(err, IsNil)
	c.Assert(metrics, HasLen, initMetricLen)
	c.Assert(GetGaugeValue(gauge.gauge), Equals, underThreshold)

	gauge.Set(overThreshold)
	metrics, err = registry.Gather()
	c.Assert(err, IsNil)
	c.Assert(metrics, HasLen, initMetricLen+1)
	c.Assert(GetGaugeValue(gauge.gauge), Equals, overThreshold)

	gauge.Set(threshold)
	metrics, err = registry.Gather()
	c.Assert(err, IsNil)
	c.Assert(metrics, HasLen, initMetricLen)
	c.Assert(GetGaugeValue(gauge.gauge), Equals, threshold)

	gauge.Set(overThreshold)
	metrics, err = registry.Gather()
	c.Assert(err, IsNil)
	c.Assert(metrics, HasLen, initMetricLen+1)
	c.Assert(GetGaugeValue(gauge.gauge), Equals, overThreshold)

	gauge.Set(underThreshold)
	metrics, err = registry.Gather()
	c.Assert(err, IsNil)
	c.Assert(metrics, HasLen, initMetricLen)
	c.Assert(GetGaugeValue(gauge.gauge), Equals, underThreshold)
}

// TestDefaultMetrics_GivenEmptyInput_WhenCalled_ThenReturnsSubnetMetrics tests DefaultMetrics function
func (s *MetricsSuite) TestDefaultMetrics_GivenEmptyInput_WhenCalled_ThenReturnsSubnetMetrics(c *C) {
	// Given: Call DefaultMetrics function
	result := DefaultMetrics()

	// When: Check if subnet-related metrics are included
	expectedSubnetMetrics := []string{
		Namespace + "_subnet_ips_guage",
		Namespace + "_subnet_ips_count",
		Namespace + "_subnet_ips_available",
		Namespace + "_subnet_ips_used",
	}

	// Then: Verify all subnet metrics are present
	for _, expectedMetric := range expectedSubnetMetrics {
		_, exists := result[expectedMetric]
		c.Assert(exists, Equals, true, Commentf("Expected metric %s not found in DefaultMetrics result", expectedMetric))
	}
}

// TestCreateConfiguration_GivenSubnetMetrics_WhenCalled_ThenCreatesCorrectConfiguration tests CreateConfiguration function for subnet metrics
func (s *MetricsSuite) TestCreateConfiguration_GivenSubnetMetrics_WhenCalled_ThenCreatesCorrectConfiguration(c *C) {
	// Given: Subnet-related metric names
	subnetMetrics := []string{
		Namespace + "_subnet_ips_guage",
		Namespace + "_subnet_ips_count",
		Namespace + "_subnet_ips_available",
		Namespace + "_subnet_ips_used",
	}

	// When: Call CreateConfiguration with subnet metrics
	config, collectors := CreateConfiguration(subnetMetrics)

	// Then: Verify configuration is correct
	c.Assert(config.SubnetIPsGuageEnabled, Equals, true)
	c.Assert(config.SubnetIPsCountEnabled, Equals, true)
	c.Assert(config.SubnetIPsAvailableEnabled, Equals, true)
	c.Assert(config.SubnetIPsUsedEnabled, Equals, true)

	// Verify collectors are created
	c.Assert(len(collectors), Equals, 4)

	// Verify each collector is a GaugeVec
	for i, collector := range collectors {
		gaugeVec, ok := collector.(*prometheus.GaugeVec)
		c.Assert(ok, Equals, true, Commentf("Collector %d is not a GaugeVec", i))
		c.Assert(gaugeVec, NotNil)
	}
}

// TestCreateConfiguration_GivenSubnetIPsGuage_WhenCalled_ThenCreatesCorrectGaugeVec tests subnet_ips_guage metric creation
func (s *MetricsSuite) TestCreateConfiguration_GivenSubnetIPsGuage_WhenCalled_ThenCreatesCorrectGaugeVec(c *C) {
	// Given: Only subnet_ips_guage metric
	metrics := []string{Namespace + "_subnet_ips_guage"}

	// When: Call CreateConfiguration
	config, collectors := CreateConfiguration(metrics)

	// Then: Verify configuration
	c.Assert(config.SubnetIPsGuageEnabled, Equals, true)
	c.Assert(len(collectors), Equals, 1)

	// Verify the gauge vec has correct labels
	gaugeVec := collectors[0].(*prometheus.GaugeVec)
	c.Assert(gaugeVec, NotNil)
}

// TestCreateConfiguration_GivenSubnetIPsCount_WhenCalled_ThenCreatesCorrectGaugeVec tests subnet_ips_count metric creation
func (s *MetricsSuite) TestCreateConfiguration_GivenSubnetIPsCount_WhenCalled_ThenCreatesCorrectGaugeVec(c *C) {
	// Given: Only subnet_ips_count metric
	metrics := []string{Namespace + "_subnet_ips_count"}

	// When: Call CreateConfiguration
	config, collectors := CreateConfiguration(metrics)

	// Then: Verify configuration
	c.Assert(config.SubnetIPsCountEnabled, Equals, true)
	c.Assert(len(collectors), Equals, 1)

	// Verify the gauge vec has correct labels
	gaugeVec := collectors[0].(*prometheus.GaugeVec)
	c.Assert(gaugeVec, NotNil)
}

// TestCreateConfiguration_GivenSubnetIPsAvailable_WhenCalled_ThenCreatesCorrectGaugeVec tests subnet_ips_available metric creation
func (s *MetricsSuite) TestCreateConfiguration_GivenSubnetIPsAvailable_WhenCalled_ThenCreatesCorrectGaugeVec(c *C) {
	// Given: Only subnet_ips_available metric
	metrics := []string{Namespace + "_subnet_ips_available"}

	// When: Call CreateConfiguration
	config, collectors := CreateConfiguration(metrics)

	// Then: Verify configuration
	c.Assert(config.SubnetIPsAvailableEnabled, Equals, true)
	c.Assert(len(collectors), Equals, 1)

	// Verify the gauge vec has correct labels
	gaugeVec := collectors[0].(*prometheus.GaugeVec)
	c.Assert(gaugeVec, NotNil)
}

// TestCreateConfiguration_GivenSubnetIPsUsed_WhenCalled_ThenCreatesCorrectGaugeVec tests subnet_ips_used metric creation
func (s *MetricsSuite) TestCreateConfiguration_GivenSubnetIPsUsed_WhenCalled_ThenCreatesCorrectGaugeVec(c *C) {
	// Given: Only subnet_ips_used metric
	metrics := []string{Namespace + "_subnet_ips_used"}

	// When: Call CreateConfiguration
	config, collectors := CreateConfiguration(metrics)

	// Then: Verify configuration
	c.Assert(config.SubnetIPsUsedEnabled, Equals, true)
	c.Assert(len(collectors), Equals, 1)

	// Verify the gauge vec has correct labels
	gaugeVec := collectors[0].(*prometheus.GaugeVec)
	c.Assert(gaugeVec, NotNil)
}

// TestCreateConfiguration_GivenNoSubnetMetrics_WhenCalled_ThenSubnetMetricsDisabled tests when no subnet metrics are provided
func (s *MetricsSuite) TestCreateConfiguration_GivenNoSubnetMetrics_WhenCalled_ThenSubnetMetricsDisabled(c *C) {
	// Given: No subnet metrics
	metrics := []string{}

	// When: Call CreateConfiguration
	config, collectors := CreateConfiguration(metrics)

	// Then: Verify subnet metrics are disabled
	c.Assert(config.SubnetIPsGuageEnabled, Equals, false)
	c.Assert(config.SubnetIPsCountEnabled, Equals, false)
	c.Assert(config.SubnetIPsAvailableEnabled, Equals, false)
	c.Assert(config.SubnetIPsUsedEnabled, Equals, false)
	c.Assert(len(collectors), Equals, 0)
}

// TestCreateConfiguration_GivenMixedMetrics_WhenCalled_ThenOnlySubnetMetricsEnabled tests mixed metrics scenario
func (s *MetricsSuite) TestCreateConfiguration_GivenMixedMetrics_WhenCalled_ThenOnlySubnetMetricsEnabled(c *C) {
	// Given: Mixed metrics including subnet and non-subnet metrics
	metrics := []string{
		Namespace + "_subnet_ips_guage",
		Namespace + "_version",
		Namespace + "_subnet_ips_count",
	}

	// When: Call CreateConfiguration
	config, collectors := CreateConfiguration(metrics)

	// Then: Verify only subnet metrics are enabled
	c.Assert(config.SubnetIPsGuageEnabled, Equals, true)
	c.Assert(config.SubnetIPsCountEnabled, Equals, true)
	c.Assert(config.SubnetIPsAvailableEnabled, Equals, false)
	c.Assert(config.SubnetIPsUsedEnabled, Equals, false)
	c.Assert(config.VersionMetric, Equals, true)
	c.Assert(len(collectors), Equals, 3)
}
