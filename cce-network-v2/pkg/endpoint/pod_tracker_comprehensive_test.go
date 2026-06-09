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
	"sync"
	"testing"
	"time"
)

// TestPodAllocationTracker_CleanupWithOldRecords 测试Cleanup函数清理过期记录
func TestPodAllocationTracker_CleanupWithOldRecords(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	owner1 := "test-namespace/test-pod-1"
	owner2 := "test-namespace/test-pod-2"
	owner3 := "test-namespace/test-pod-3"

	// 添加三个跟踪记录
	tracker.StartTracking(owner1)
	tracker.StartTracking(owner2)
	tracker.StartTracking(owner3)

	// 验证三个记录都存在
	if _, exists := tracker.GetTrackingStartTime(owner1); !exists {
		t.Error("Expected tracking 1 to exist")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); !exists {
		t.Error("Expected tracking 2 to exist")
	}
	if _, exists := tracker.GetTrackingStartTime(owner3); !exists {
		t.Error("Expected tracking 3 to exist")
	}

	// 手动修改时间，让前两个记录变老（超过10分钟）
	tracker.mu.Lock()
	tracker.podStartTimes[owner1] = time.Now().Add(-11 * time.Minute)
	tracker.podStartTimes[owner2] = time.Now().Add(-12 * time.Minute)
	// owner3保持当前时间
	tracker.mu.Unlock()

	// 执行清理
	tracker.Cleanup()

	// 验证过期记录被清理，新记录保留
	if _, exists := tracker.GetTrackingStartTime(owner1); exists {
		t.Error("Expected tracking 1 to be cleaned up (expired)")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); exists {
		t.Error("Expected tracking 2 to be cleaned up (expired)")
	}
	if _, exists := tracker.GetTrackingStartTime(owner3); !exists {
		t.Error("Expected tracking 3 to still exist (not expired)")
	}
}

// TestPodReleaseTracker_CleanupWithOldRecords 测试Cleanup函数清理过期记录
func TestPodReleaseTracker_CleanupWithOldRecords(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner1 := "test-namespace/test-pod-1"
	owner2 := "test-namespace/test-pod-2"
	owner3 := "test-namespace/test-pod-3"

	// 添加三个跟踪记录
	tracker.StartTracking(owner1)
	tracker.StartTracking(owner2)
	tracker.StartTracking(owner3)

	// 验证三个记录都存在
	if _, exists := tracker.GetTrackingStartTime(owner1); !exists {
		t.Error("Expected tracking 1 to exist")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); !exists {
		t.Error("Expected tracking 2 to exist")
	}
	if _, exists := tracker.GetTrackingStartTime(owner3); !exists {
		t.Error("Expected tracking 3 to exist")
	}

	// 手动修改时间，让前两个记录变老（超过10分钟）
	tracker.mu.Lock()
	tracker.podReleaseStarts[owner1] = time.Now().Add(-11 * time.Minute)
	tracker.podReleaseStarts[owner2] = time.Now().Add(-12 * time.Minute)
	// owner3保持当前时间
	tracker.mu.Unlock()

	// 执行清理
	tracker.Cleanup()

	// 验证过期记录被清理，新记录保留
	if _, exists := tracker.GetTrackingStartTime(owner1); exists {
		t.Error("Expected tracking 1 to be cleaned up (expired)")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); exists {
		t.Error("Expected tracking 2 to be cleaned up (expired)")
	}
	if _, exists := tracker.GetTrackingStartTime(owner3); !exists {
		t.Error("Expected tracking 3 to still exist (not expired)")
	}
}

// TestStartPodTrackerCleaner 测试后台清理任务
func TestStartPodTrackerCleaner(t *testing.T) {
	// 创建独立的tracker实例用于测试
	testAllocTracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}
	testReleaseTracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	// 添加一些测试数据
	owner1 := "test-namespace/test-pod-1"
	owner2 := "test-namespace/test-pod-2"

	testAllocTracker.StartTracking(owner1)
	testAllocTracker.StartTracking(owner2)
	testReleaseTracker.StartTracking(owner1)
	testReleaseTracker.StartTracking(owner2)

	// 验证数据存在
	if _, exists := testAllocTracker.GetTrackingStartTime(owner1); !exists {
		t.Error("Expected allocation tracking 1 to exist")
	}
	if _, exists := testReleaseTracker.GetTrackingStartTime(owner1); !exists {
		t.Error("Expected release tracking 1 to exist")
	}

	// 手动修改时间为过期时间
	testAllocTracker.mu.Lock()
	testAllocTracker.podStartTimes[owner1] = time.Now().Add(-11 * time.Minute)
	testAllocTracker.podStartTimes[owner2] = time.Now().Add(-12 * time.Minute)
	testAllocTracker.mu.Unlock()

	testReleaseTracker.mu.Lock()
	testReleaseTracker.podReleaseStarts[owner1] = time.Now().Add(-11 * time.Minute)
	testReleaseTracker.podReleaseStarts[owner2] = time.Now().Add(-12 * time.Minute)
	testReleaseTracker.mu.Unlock()

	// 执行清理
	testAllocTracker.Cleanup()
	testReleaseTracker.Cleanup()

	// 验证过期记录被清理
	if _, exists := testAllocTracker.GetTrackingStartTime(owner1); exists {
		t.Error("Expected allocation tracking 1 to be cleaned up")
	}
	if _, exists := testAllocTracker.GetTrackingStartTime(owner2); exists {
		t.Error("Expected allocation tracking 2 to be cleaned up")
	}
	if _, exists := testReleaseTracker.GetTrackingStartTime(owner1); exists {
		t.Error("Expected release tracking 1 to be cleaned up")
	}
	if _, exists := testReleaseTracker.GetTrackingStartTime(owner2); exists {
		t.Error("Expected release tracking 2 to be cleaned up")
	}
}

// TestPodAllocationTracker_EdgeCases 测试边界情况
func TestPodAllocationTracker_EdgeCases(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	// 测试空字符串owner
	startTime := tracker.StartTracking("")
	if startTime.IsZero() {
		t.Error("Expected non-zero start time for empty owner")
	}

	// 测试重复的空字符串owner
	startTime2 := tracker.StartTracking("")
	if !startTime.Equal(startTime2) {
		t.Error("Expected same start time for repeated empty owner")
	}

	// 测试特殊字符owner
	specialOwner := "test-namespace/test-pod-with-special-chars-!@#$%^&*()"
	startTime3 := tracker.StartTracking(specialOwner)
	if startTime3.IsZero() {
		t.Error("Expected non-zero start time for special owner")
	}

	// 测试获取不存在的owner
	_, exists := tracker.GetTrackingStartTime("non-existent-owner")
	if exists {
		t.Error("Expected non-existent owner to not exist")
	}

	// 测试停止不存在的owner
	_, exists = tracker.StopTracking("non-existent-owner")
	if exists {
		t.Error("Expected stopping non-existent owner to return false")
	}
}

// TestPodReleaseTracker_EdgeCases 测试边界情况
func TestPodReleaseTracker_EdgeCases(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	// 测试空字符串owner
	startTime := tracker.StartTracking("")
	if startTime.IsZero() {
		t.Error("Expected non-zero start time for empty owner")
	}

	// 测试重复的空字符串owner
	startTime2 := tracker.StartTracking("")
	if !startTime.Equal(startTime2) {
		t.Error("Expected same start time for repeated empty owner")
	}

	// 测试特殊字符owner
	specialOwner := "test-namespace/test-pod-with-special-chars-!@#$%^&*()"
	startTime3 := tracker.StartTracking(specialOwner)
	if startTime3.IsZero() {
		t.Error("Expected non-zero start time for special owner")
	}

	// 测试获取不存在的owner
	_, exists := tracker.GetTrackingStartTime("non-existent-owner")
	if exists {
		t.Error("Expected non-existent owner to not exist")
	}

	// 测试停止不存在的owner
	_, exists = tracker.StopTracking("non-existent-owner")
	if exists {
		t.Error("Expected stopping non-existent owner to return false")
	}
}

// TestPodAllocationTracker_StressTest 压力测试
func TestPodAllocationTracker_StressTest(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	const numOperations = 1000
	const numGoroutines = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// 启动多个goroutine进行并发操作
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				owner := fmt.Sprintf("test-namespace/test-pod-%d-%d", goroutineID, j)

				// 开始跟踪
				tracker.StartTracking(owner)

				// 获取跟踪时间
				_, exists := tracker.GetTrackingStartTime(owner)
				if !exists {
					t.Errorf("Expected tracking to exist for %s", owner)
				}

				// 停止跟踪
				_, exists = tracker.StopTracking(owner)
				if !exists {
					t.Errorf("Expected tracking to exist when stopping %s", owner)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestPodReleaseTracker_StressTest 压力测试
func TestPodReleaseTracker_StressTest(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	const numOperations = 1000
	const numGoroutines = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// 启动多个goroutine进行并发操作
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				owner := fmt.Sprintf("test-namespace/test-pod-%d-%d", goroutineID, j)

				// 开始跟踪
				tracker.StartTracking(owner)

				// 获取跟踪时间
				_, exists := tracker.GetTrackingStartTime(owner)
				if !exists {
					t.Errorf("Expected tracking to exist for %s", owner)
				}

				// 停止跟踪
				_, exists = tracker.StopTracking(owner)
				if !exists {
					t.Errorf("Expected tracking to exist when stopping %s", owner)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestPodAllocationTracker_MixedOperations 测试混合操作
func TestPodAllocationTracker_MixedOperations(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	owner1 := "test-namespace/test-pod-1"
	owner2 := "test-namespace/test-pod-2"
	owner3 := "test-namespace/test-pod-3"

	// 混合操作：开始、获取、停止、重新开始
	tracker.StartTracking(owner1)
	tracker.StartTracking(owner2)

	// 获取时间
	_, exists := tracker.GetTrackingStartTime(owner1)
	if !exists {
		t.Error("Expected tracking 1 to exist")
	}

	// 停止一个
	_, exists = tracker.StopTracking(owner1)
	if !exists {
		t.Error("Expected tracking 1 to exist when stopping")
	}

	// 验证停止后不存在
	_, exists = tracker.GetTrackingStartTime(owner1)
	if exists {
		t.Error("Expected tracking 1 to not exist after stopping")
	}

	// 重新开始
	startTime := tracker.StartTracking(owner1)
	if startTime.IsZero() {
		t.Error("Expected non-zero start time for restarted tracking")
	}

	// 添加第三个
	tracker.StartTracking(owner3)

	// 验证所有都存在
	_, exists = tracker.GetTrackingStartTime(owner1)
	if !exists {
		t.Error("Expected tracking 1 to exist after restart")
	}
	_, exists = tracker.GetTrackingStartTime(owner2)
	if !exists {
		t.Error("Expected tracking 2 to exist")
	}
	_, exists = tracker.GetTrackingStartTime(owner3)
	if !exists {
		t.Error("Expected tracking 3 to exist")
	}
}

// TestPodReleaseTracker_MixedOperations 测试混合操作
func TestPodReleaseTracker_MixedOperations(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner1 := "test-namespace/test-pod-1"
	owner2 := "test-namespace/test-pod-2"
	owner3 := "test-namespace/test-pod-3"

	// 混合操作：开始、获取、停止、重新开始
	tracker.StartTracking(owner1)
	tracker.StartTracking(owner2)

	// 获取时间
	_, exists := tracker.GetTrackingStartTime(owner1)
	if !exists {
		t.Error("Expected tracking 1 to exist")
	}

	// 停止一个
	_, exists = tracker.StopTracking(owner1)
	if !exists {
		t.Error("Expected tracking 1 to exist when stopping")
	}

	// 验证停止后不存在
	_, exists = tracker.GetTrackingStartTime(owner1)
	if exists {
		t.Error("Expected tracking 1 to not exist after stopping")
	}

	// 重新开始
	startTime := tracker.StartTracking(owner1)
	if startTime.IsZero() {
		t.Error("Expected non-zero start time for restarted tracking")
	}

	// 添加第三个
	tracker.StartTracking(owner3)

	// 验证所有都存在
	_, exists = tracker.GetTrackingStartTime(owner1)
	if !exists {
		t.Error("Expected tracking 1 to exist after restart")
	}
	_, exists = tracker.GetTrackingStartTime(owner2)
	if !exists {
		t.Error("Expected tracking 2 to exist")
	}
	_, exists = tracker.GetTrackingStartTime(owner3)
	if !exists {
		t.Error("Expected tracking 3 to exist")
	}
}
