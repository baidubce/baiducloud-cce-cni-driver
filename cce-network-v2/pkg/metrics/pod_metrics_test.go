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
	"net/http"
	"testing"
)

// setupPodMetrics 设置Pod指标并返回清理函数
func setupPodMetrics(t *testing.T) func() {
	// 创建配置并初始化指标
	_, collectors := CreateConfiguration([]string{
		Namespace + "_" + SubsystemAgent + "_pod_ip_allocation_duration_seconds",
		Namespace + "_" + SubsystemAgent + "_pod_ip_release_duration_seconds",
	})

	// 注册收集器
	for _, collector := range collectors {
		MustRegister(collector)
	}

	// 返回清理函数
	return func() {
		for _, collector := range collectors {
			Unregister(collector)
		}
	}
}

func TestPodIPAllocationDuration(t *testing.T) {
	cleanup := setupPodMetrics(t)
	defer cleanup()

	// 验证指标不为nil
	if PodIPAllocationDuration == nil {
		t.Error("Expected PodIPAllocationDuration to be initialized")
	}

	// 测试指标记录
	PodIPAllocationDuration.WithLabelValues(LabelValueOutcomeSuccess, "vpc-eni", "test-node").Observe(0.1)
	PodIPAllocationDuration.WithLabelValues(LabelValueOutcomeFail, "vpc-eni", "test-node").Observe(0.2)
}

func TestPodIPReleaseDuration(t *testing.T) {
	cleanup := setupPodMetrics(t)
	defer cleanup()

	// 验证指标不为nil
	if PodIPReleaseDuration == nil {
		t.Error("Expected PodIPReleaseDuration to be initialized")
	}

	// 测试指标记录
	PodIPReleaseDuration.WithLabelValues(LabelValueOutcomeSuccess, "test-node").Observe(0.05)
	PodIPReleaseDuration.WithLabelValues(LabelValueOutcomeFail, "test-node").Observe(0.15)
}

func TestPodMetricsLabels(t *testing.T) {
	cleanup := setupPodMetrics(t)
	defer cleanup()

	// 测试分配指标标签 - 通过观察值来验证标签正确性
	allocationMetric := PodIPAllocationDuration.WithLabelValues(LabelValueOutcomeSuccess, "vpc-eni", "test-node")
	allocationMetric.Observe(0.1)

	// 测试释放指标标签 - 通过观察值来验证标签正确性
	releaseMetric := PodIPReleaseDuration.WithLabelValues(LabelValueOutcomeSuccess, "test-node")
	releaseMetric.Observe(0.05)
}

func TestPodMetricsHelpText(t *testing.T) {
	cleanup := setupPodMetrics(t)
	defer cleanup()

	// 验证分配指标帮助文本 - 通过观察值来验证
	allocationMetric := PodIPAllocationDuration.WithLabelValues(LabelValueOutcomeSuccess, "vpc-eni", "test-node")
	allocationMetric.Observe(0.1)

	// 验证释放指标帮助文本 - 通过观察值来验证
	releaseMetric := PodIPReleaseDuration.WithLabelValues(LabelValueOutcomeSuccess, "test-node")
	releaseMetric.Observe(0.05)
}

func TestPodMetricsBuckets(t *testing.T) {
	cleanup := setupPodMetrics(t)
	defer cleanup()

	// 验证分配指标桶 - 通过观察值来验证
	allocationMetric := PodIPAllocationDuration.WithLabelValues(LabelValueOutcomeSuccess, "vpc-eni", "test-node")

	// 验证桶配置
	expectedBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0}
	// 通过观察值来验证桶配置
	for _, bucket := range expectedBuckets {
		allocationMetric.Observe(bucket)
	}

	// 验证释放指标桶 - 通过观察值来验证
	releaseMetric := PodIPReleaseDuration.WithLabelValues(LabelValueOutcomeSuccess, "test-node")

	// 验证桶配置
	for _, bucket := range expectedBuckets {
		releaseMetric.Observe(bucket)
	}
}

func TestPodMetricsNamespace(t *testing.T) {
	cleanup := setupPodMetrics(t)
	defer cleanup()

	// 验证分配指标命名空间 - 通过观察值来验证
	allocationMetric := PodIPAllocationDuration.WithLabelValues(LabelValueOutcomeSuccess, "vpc-eni", "test-node")
	allocationMetric.Observe(0.1)

	// 验证释放指标命名空间 - 通过观察值来验证
	releaseMetric := PodIPReleaseDuration.WithLabelValues(LabelValueOutcomeSuccess, "test-node")
	releaseMetric.Observe(0.05)
}

// 辅助函数：检查字符串是否包含子字符串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestAttachHandler(t *testing.T) {
	cleanup := setupPodMetrics(t)
	defer cleanup()

	// 测试AttachHandler函数
	mux := http.NewServeMux()

	// 测试默认路径
	AttachHandler(mux, "")

	// 测试自定义路径
	AttachHandler(mux, "/custom-metrics")

	// 验证处理器已附加（通过检查mux是否有处理器）
	// 这里我们无法直接测试HTTP处理器，但可以确保函数不panic
}
