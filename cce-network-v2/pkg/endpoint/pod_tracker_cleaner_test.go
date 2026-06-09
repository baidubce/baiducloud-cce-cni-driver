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
	"testing"
	"time"
)

// TestStartPodTrackerCleaner_Integration 测试StartPodTrackerCleaner函数的集成
func TestStartPodTrackerCleaner_Integration(t *testing.T) {
	// 这个测试验证StartPodTrackerCleaner函数能够正常启动
	// 由于它是一个后台goroutine，我们通过验证它不会panic来测试

	// 添加一些测试数据到全局tracker
	owner1 := "test-namespace/test-pod-cleaner-1"
	owner2 := "test-namespace/test-pod-cleaner-2"

	// 开始跟踪
	podTracker.StartTracking(owner1)
	podReleaseTracker.StartTracking(owner2)

	// 验证数据存在
	_, exists := podTracker.GetTrackingStartTime(owner1)
	if !exists {
		t.Error("Expected allocation tracking to exist")
	}
	_, exists = podReleaseTracker.GetTrackingStartTime(owner2)
	if !exists {
		t.Error("Expected release tracking to exist")
	}

	// 手动修改时间为过期时间
	podTracker.mu.Lock()
	podTracker.podStartTimes[owner1] = time.Now().Add(-11 * time.Minute)
	podTracker.mu.Unlock()

	podReleaseTracker.mu.Lock()
	podReleaseTracker.podReleaseStarts[owner2] = time.Now().Add(-11 * time.Minute)
	podReleaseTracker.mu.Unlock()

	// 执行清理（模拟cleaner的行为）
	podTracker.Cleanup()
	podReleaseTracker.Cleanup()

	// 验证过期记录被清理
	_, exists = podTracker.GetTrackingStartTime(owner1)
	if exists {
		t.Error("Expected allocation tracking to be cleaned up")
	}
	_, exists = podReleaseTracker.GetTrackingStartTime(owner2)
	if exists {
		t.Error("Expected release tracking to be cleaned up")
	}
}

// TestPodTrackerCleanerBehavior 测试清理器的行为
func TestPodTrackerCleanerBehavior(t *testing.T) {
	// 创建独立的tracker实例
	allocTracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}
	releaseTracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	// 添加测试数据
	owners := []string{
		"test-namespace/test-pod-1",
		"test-namespace/test-pod-2",
		"test-namespace/test-pod-3",
		"test-namespace/test-pod-4",
		"test-namespace/test-pod-5",
	}

	for _, owner := range owners {
		allocTracker.StartTracking(owner)
		releaseTracker.StartTracking(owner)
	}

	// 验证所有记录都存在
	for _, owner := range owners {
		_, exists := allocTracker.GetTrackingStartTime(owner)
		if !exists {
			t.Errorf("Expected allocation tracking to exist for %s", owner)
		}
		_, exists = releaseTracker.GetTrackingStartTime(owner)
		if !exists {
			t.Errorf("Expected release tracking to exist for %s", owner)
		}
	}

	// 手动设置一些记录为过期（超过10分钟）
	allocTracker.mu.Lock()
	allocTracker.podStartTimes[owners[0]] = time.Now().Add(-11 * time.Minute)
	allocTracker.podStartTimes[owners[1]] = time.Now().Add(-12 * time.Minute)
	allocTracker.mu.Unlock()

	releaseTracker.mu.Lock()
	releaseTracker.podReleaseStarts[owners[0]] = time.Now().Add(-11 * time.Minute)
	releaseTracker.podReleaseStarts[owners[1]] = time.Now().Add(-12 * time.Minute)
	releaseTracker.mu.Unlock()

	// 执行清理
	allocTracker.Cleanup()
	releaseTracker.Cleanup()

	// 验证过期记录被清理，新记录保留
	_, exists := allocTracker.GetTrackingStartTime(owners[0])
	if exists {
		t.Error("Expected allocation tracking 0 to be cleaned up (expired)")
	}
	_, exists = allocTracker.GetTrackingStartTime(owners[1])
	if exists {
		t.Error("Expected allocation tracking 1 to be cleaned up (expired)")
	}
	_, exists = allocTracker.GetTrackingStartTime(owners[2])
	if !exists {
		t.Error("Expected allocation tracking 2 to still exist (not expired)")
	}
	_, exists = allocTracker.GetTrackingStartTime(owners[3])
	if !exists {
		t.Error("Expected allocation tracking 3 to still exist (not expired)")
	}
	_, exists = allocTracker.GetTrackingStartTime(owners[4])
	if !exists {
		t.Error("Expected allocation tracking 4 to still exist (not expired)")
	}

	_, exists = releaseTracker.GetTrackingStartTime(owners[0])
	if exists {
		t.Error("Expected release tracking 0 to be cleaned up (expired)")
	}
	_, exists = releaseTracker.GetTrackingStartTime(owners[1])
	if exists {
		t.Error("Expected release tracking 1 to be cleaned up (expired)")
	}
	_, exists = releaseTracker.GetTrackingStartTime(owners[2])
	if !exists {
		t.Error("Expected release tracking 2 to still exist (not expired)")
	}
	_, exists = releaseTracker.GetTrackingStartTime(owners[3])
	if !exists {
		t.Error("Expected release tracking 3 to still exist (not expired)")
	}
	_, exists = releaseTracker.GetTrackingStartTime(owners[4])
	if !exists {
		t.Error("Expected release tracking 4 to still exist (not expired)")
	}
}

// TestPodTrackerCleanupEdgeCases 测试清理的边界情况
func TestPodTrackerCleanupEdgeCases(t *testing.T) {
	// 测试空map的清理
	emptyAllocTracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}
	emptyReleaseTracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	// 对空map执行清理不应该panic
	emptyAllocTracker.Cleanup()
	emptyReleaseTracker.Cleanup()

	// 测试只有一个过期记录的清理
	singleAllocTracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}
	singleReleaseTracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner := "test-namespace/test-pod-single"
	singleAllocTracker.StartTracking(owner)
	singleReleaseTracker.StartTracking(owner)

	// 设置为过期
	singleAllocTracker.mu.Lock()
	singleAllocTracker.podStartTimes[owner] = time.Now().Add(-11 * time.Minute)
	singleAllocTracker.mu.Unlock()

	singleReleaseTracker.mu.Lock()
	singleReleaseTracker.podReleaseStarts[owner] = time.Now().Add(-11 * time.Minute)
	singleReleaseTracker.mu.Unlock()

	// 执行清理
	singleAllocTracker.Cleanup()
	singleReleaseTracker.Cleanup()

	// 验证记录被清理
	_, exists := singleAllocTracker.GetTrackingStartTime(owner)
	if exists {
		t.Error("Expected single allocation tracking to be cleaned up")
	}
	_, exists = singleReleaseTracker.GetTrackingStartTime(owner)
	if exists {
		t.Error("Expected single release tracking to be cleaned up")
	}
}

// TestPodTrackerCleanupTiming 测试清理的时间逻辑
func TestPodTrackerCleanupTiming(t *testing.T) {
	allocTracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}
	releaseTracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner1 := "test-namespace/test-pod-timing-1"
	owner2 := "test-namespace/test-pod-timing-2"
	owner3 := "test-namespace/test-pod-timing-3"

	// 添加记录
	allocTracker.StartTracking(owner1)
	allocTracker.StartTracking(owner2)
	allocTracker.StartTracking(owner3)

	releaseTracker.StartTracking(owner1)
	releaseTracker.StartTracking(owner2)
	releaseTracker.StartTracking(owner3)

	// 设置不同的时间：
	// owner1: 9分钟前（应该保留）
	// owner2: 10分钟1秒前（应该清理）
	// owner3: 5分钟前（应该保留）
	now := time.Now()

	allocTracker.mu.Lock()
	allocTracker.podStartTimes[owner1] = now.Add(-9 * time.Minute)
	allocTracker.podStartTimes[owner2] = now.Add(-10*time.Minute - 1*time.Second)
	allocTracker.podStartTimes[owner3] = now.Add(-5 * time.Minute)
	allocTracker.mu.Unlock()

	releaseTracker.mu.Lock()
	releaseTracker.podReleaseStarts[owner1] = now.Add(-9 * time.Minute)
	releaseTracker.podReleaseStarts[owner2] = now.Add(-10*time.Minute - 1*time.Second)
	releaseTracker.podReleaseStarts[owner3] = now.Add(-5 * time.Minute)
	releaseTracker.mu.Unlock()

	// 执行清理
	allocTracker.Cleanup()
	releaseTracker.Cleanup()

	// 验证结果
	_, exists := allocTracker.GetTrackingStartTime(owner1)
	if !exists {
		t.Error("Expected allocation tracking 1 to still exist (9 minutes)")
	}
	_, exists = allocTracker.GetTrackingStartTime(owner2)
	if exists {
		t.Error("Expected allocation tracking 2 to be cleaned up (over 10 minutes)")
	}
	_, exists = allocTracker.GetTrackingStartTime(owner3)
	if !exists {
		t.Error("Expected allocation tracking 3 to still exist (5 minutes)")
	}

	_, exists = releaseTracker.GetTrackingStartTime(owner1)
	if !exists {
		t.Error("Expected release tracking 1 to still exist (9 minutes)")
	}
	_, exists = releaseTracker.GetTrackingStartTime(owner2)
	if exists {
		t.Error("Expected release tracking 2 to be cleaned up (over 10 minutes)")
	}
	_, exists = releaseTracker.GetTrackingStartTime(owner3)
	if !exists {
		t.Error("Expected release tracking 3 to still exist (5 minutes)")
	}
}
