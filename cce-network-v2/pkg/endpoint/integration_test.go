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
package endpoint

import (
	"fmt"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
)

func TestPodTrackingIntegration(t *testing.T) {
	// 初始化指标
	_, collectors := metrics.CreateConfiguration([]string{
		metrics.Namespace + "_" + metrics.SubsystemAgent + "_pod_ip_allocation_duration_seconds",
		metrics.Namespace + "_" + metrics.SubsystemAgent + "_pod_ip_release_duration_seconds",
	})

	// 注册收集器
	for _, collector := range collectors {
		metrics.MustRegister(collector)
	}

	// 测试完成后清理
	defer func() {
		for _, collector := range collectors {
			metrics.Unregister(collector)
		}
	}()

	owner := "test-namespace/test-pod"

	// 测试分配跟踪
	t.Run("AllocationTracking", func(t *testing.T) {
		// 开始跟踪
		startTime := podTracker.StartTracking(owner)
		if startTime.IsZero() {
			t.Error("Expected non-zero start time")
		}

		// 模拟一些处理时间
		time.Sleep(50 * time.Millisecond)

		// 记录成功指标
		if trackedStartTime, wasTracked := podTracker.GetTrackingStartTime(owner); wasTracked {
			duration := time.Since(trackedStartTime).Seconds()
			metrics.PodIPAllocationDuration.WithLabelValues(
				metrics.LabelValueOutcomeSuccess, "vpc-eni", "test-node",
			).Observe(duration)

			if duration < 0.05 {
				t.Errorf("Expected duration to be at least 0.05s, got %f", duration)
			}
		} else {
			t.Error("Expected tracking to exist")
		}

		// 停止跟踪
		_, exists := podTracker.StopTracking(owner)
		if !exists {
			t.Error("Expected tracking to exist when stopping")
		}
	})

	// 测试释放跟踪
	t.Run("ReleaseTracking", func(t *testing.T) {
		// 开始跟踪
		startTime := podReleaseTracker.StartTracking(owner)
		if startTime.IsZero() {
			t.Error("Expected non-zero start time")
		}

		// 模拟一些处理时间
		time.Sleep(30 * time.Millisecond)

		// 记录成功指标
		if trackedStartTime, wasTracked := podReleaseTracker.GetTrackingStartTime(owner); wasTracked {
			duration := time.Since(trackedStartTime).Seconds()
			metrics.PodIPReleaseDuration.WithLabelValues(
				metrics.LabelValueOutcomeSuccess, "test-node",
			).Observe(duration)

			if duration < 0.03 {
				t.Errorf("Expected duration to be at least 0.03s, got %f", duration)
			}
		} else {
			t.Error("Expected tracking to exist")
		}

		// 停止跟踪
		_, exists := podReleaseTracker.StopTracking(owner)
		if !exists {
			t.Error("Expected tracking to exist when stopping")
		}
	})
}

func TestPodTrackingRetryScenario(t *testing.T) {
	// 初始化指标
	_, collectors := metrics.CreateConfiguration([]string{
		metrics.Namespace + "_" + metrics.SubsystemAgent + "_pod_ip_allocation_duration_seconds",
		metrics.Namespace + "_" + metrics.SubsystemAgent + "_pod_ip_release_duration_seconds",
	})

	// 注册收集器
	for _, collector := range collectors {
		metrics.MustRegister(collector)
	}

	// 测试完成后清理
	defer func() {
		for _, collector := range collectors {
			metrics.Unregister(collector)
		}
	}()

	owner := "test-namespace/test-pod-retry"

	// 测试重试场景
	t.Run("AllocationRetry", func(t *testing.T) {
		// 第一次尝试开始跟踪
		startTime1 := podTracker.StartTracking(owner)
		time.Sleep(20 * time.Millisecond)

		// 模拟第一次失败，记录失败指标
		if trackedStartTime, wasTracked := podTracker.GetTrackingStartTime(owner); wasTracked {
			duration1 := time.Since(trackedStartTime).Seconds()
			metrics.PodIPAllocationDuration.WithLabelValues(
				metrics.LabelValueOutcomeFail, "vpc-eni", "test-node",
			).Observe(duration1)

			if duration1 < 0.02 {
				t.Errorf("Expected duration1 to be at least 0.02s, got %f", duration1)
			}
		}

		// 第二次尝试（重试），应该使用相同的开始时间
		startTime2 := podTracker.StartTracking(owner)
		if !startTime1.Equal(startTime2) {
			t.Errorf("Expected same start time for retry, got %v and %v", startTime1, startTime2)
		}

		time.Sleep(30 * time.Millisecond)

		// 模拟第二次成功，记录成功指标
		if trackedStartTime, wasTracked := podTracker.GetTrackingStartTime(owner); wasTracked {
			duration2 := time.Since(trackedStartTime).Seconds()
			metrics.PodIPAllocationDuration.WithLabelValues(
				metrics.LabelValueOutcomeSuccess, "vpc-eni", "test-node",
			).Observe(duration2)

			if duration2 < 0.05 {
				t.Errorf("Expected duration2 to be at least 0.05s, got %f", duration2)
			}
		}

		// 停止跟踪
		_, exists := podTracker.StopTracking(owner)
		if !exists {
			t.Error("Expected tracking to exist when stopping")
		}
	})
}

func TestPodTrackingMemoryManagement(t *testing.T) {
	// 测试内存管理
	t.Run("Cleanup", func(t *testing.T) {
		// 添加一些跟踪记录
		owners := []string{
			"test-namespace/test-pod-1",
			"test-namespace/test-pod-2",
			"test-namespace/test-pod-3",
		}

		for _, owner := range owners {
			podTracker.StartTracking(owner)
			podReleaseTracker.StartTracking(owner)
		}

		// 验证记录存在
		for _, owner := range owners {
			if _, exists := podTracker.GetTrackingStartTime(owner); !exists {
				t.Errorf("Expected tracking to exist for %s", owner)
			}
			if _, exists := podReleaseTracker.GetTrackingStartTime(owner); !exists {
				t.Errorf("Expected release tracking to exist for %s", owner)
			}
		}

		// 执行清理
		podTracker.CleanupAll()
		podReleaseTracker.CleanupAll()

		// 验证记录被清理
		for _, owner := range owners {
			if _, exists := podTracker.GetTrackingStartTime(owner); exists {
				t.Errorf("Expected tracking to be cleaned up for %s", owner)
			}
			if _, exists := podReleaseTracker.GetTrackingStartTime(owner); exists {
				t.Errorf("Expected release tracking to be cleaned up for %s", owner)
			}
		}
	})
}

func TestPodTrackingConcurrentAccess(t *testing.T) {
	// 测试并发访问
	t.Run("ConcurrentAccess", func(t *testing.T) {
		const numGoroutines = 5
		const numOperations = 20

		// 初始化指标
		_, collectors := metrics.CreateConfiguration([]string{
			metrics.Namespace + "_" + metrics.SubsystemAgent + "_pod_ip_allocation_duration_seconds",
			metrics.Namespace + "_" + metrics.SubsystemAgent + "_pod_ip_release_duration_seconds",
		})

		// 注册收集器
		for _, collector := range collectors {
			metrics.MustRegister(collector)
		}

		// 测试完成后清理
		defer func() {
			for _, collector := range collectors {
				metrics.Unregister(collector)
			}
		}()

		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(goroutineID int) {
				defer func() { done <- true }()

				for j := 0; j < numOperations; j++ {
					owner := fmt.Sprintf("test-namespace/test-pod-%d-%d", goroutineID, j)

					// 测试分配跟踪
					podTracker.StartTracking(owner)
					time.Sleep(1 * time.Millisecond)
					if _, exists := podTracker.GetTrackingStartTime(owner); !exists {
						t.Errorf("Expected tracking to exist for %s", owner)
					}
					metrics.PodIPAllocationDuration.WithLabelValues(
						metrics.LabelValueOutcomeSuccess, "vpc-eni", "test-node",
					).Observe(0.001)
					podTracker.StopTracking(owner)

					// 测试释放跟踪
					podReleaseTracker.StartTracking(owner)
					time.Sleep(1 * time.Millisecond)
					if _, exists := podReleaseTracker.GetTrackingStartTime(owner); !exists {
						t.Errorf("Expected release tracking to exist for %s", owner)
					}
					metrics.PodIPReleaseDuration.WithLabelValues(
						metrics.LabelValueOutcomeSuccess, "test-node",
					).Observe(0.001)
					podReleaseTracker.StopTracking(owner)
				}
			}(i)
		}

		// 等待所有goroutine完成
		for i := 0; i < numGoroutines; i++ {
			<-done
		}
	})
}
