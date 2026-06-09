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

func TestPodAllocationTracker_StartTracking(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	owner := "test-namespace/test-pod"

	// 第一次开始跟踪
	startTime1 := tracker.StartTracking(owner)
	if startTime1.IsZero() {
		t.Error("Expected non-zero start time")
	}

	// 第二次开始跟踪，应该返回相同的开始时间
	time.Sleep(10 * time.Millisecond)
	startTime2 := tracker.StartTracking(owner)
	if !startTime1.Equal(startTime2) {
		t.Errorf("Expected same start time, got %v and %v", startTime1, startTime2)
	}
}

func TestPodAllocationTracker_GetTrackingStartTime(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	owner := "test-namespace/test-pod"

	// 未开始跟踪时应该返回false
	_, exists := tracker.GetTrackingStartTime(owner)
	if exists {
		t.Error("Expected tracking to not exist")
	}

	// 开始跟踪后应该返回true
	startTime := tracker.StartTracking(owner)
	retrievedTime, exists := tracker.GetTrackingStartTime(owner)
	if !exists {
		t.Error("Expected tracking to exist")
	}
	if !startTime.Equal(retrievedTime) {
		t.Errorf("Expected same time, got %v and %v", startTime, retrievedTime)
	}
}

func TestPodAllocationTracker_StopTracking(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	owner := "test-namespace/test-pod"

	// 未开始跟踪时停止应该返回false
	_, exists := tracker.StopTracking(owner)
	if exists {
		t.Error("Expected tracking to not exist")
	}

	// 开始跟踪后停止应该返回true
	startTime := tracker.StartTracking(owner)
	retrievedTime, exists := tracker.StopTracking(owner)
	if !exists {
		t.Error("Expected tracking to exist")
	}
	if !startTime.Equal(retrievedTime) {
		t.Errorf("Expected same time, got %v and %v", startTime, retrievedTime)
	}

	// 停止后再次获取应该返回false
	_, exists = tracker.GetTrackingStartTime(owner)
	if exists {
		t.Error("Expected tracking to not exist after stop")
	}
}

func TestPodAllocationTracker_Cleanup(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	owner1 := "test-namespace/test-pod-1"
	owner2 := "test-namespace/test-pod-2"

	// 添加两个跟踪记录
	tracker.StartTracking(owner1)
	tracker.StartTracking(owner2)

	// 验证两个记录都存在
	if _, exists := tracker.GetTrackingStartTime(owner1); !exists {
		t.Error("Expected tracking 1 to exist")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); !exists {
		t.Error("Expected tracking 2 to exist")
	}

	// 清理后应该都消失（因为Cleanup会清理所有记录，不管时间）
	tracker.CleanupAll()
	if _, exists := tracker.GetTrackingStartTime(owner1); exists {
		t.Error("Expected tracking 1 to be cleaned up")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); exists {
		t.Error("Expected tracking 2 to be cleaned up")
	}
}

func TestPodAllocationTracker_Concurrency(t *testing.T) {
	tracker := &PodAllocationTracker{
		podStartTimes: make(map[string]time.Time),
	}

	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

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

func TestPodReleaseTracker_StartTracking(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner := "test-namespace/test-pod"

	// 第一次开始跟踪
	startTime1 := tracker.StartTracking(owner)
	if startTime1.IsZero() {
		t.Error("Expected non-zero start time")
	}

	// 第二次开始跟踪，应该返回相同的开始时间
	time.Sleep(10 * time.Millisecond)
	startTime2 := tracker.StartTracking(owner)
	if !startTime1.Equal(startTime2) {
		t.Errorf("Expected same start time, got %v and %v", startTime1, startTime2)
	}
}

func TestPodReleaseTracker_GetTrackingStartTime(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner := "test-namespace/test-pod"

	// 未开始跟踪时应该返回false
	_, exists := tracker.GetTrackingStartTime(owner)
	if exists {
		t.Error("Expected tracking to not exist")
	}

	// 开始跟踪后应该返回true
	startTime := tracker.StartTracking(owner)
	retrievedTime, exists := tracker.GetTrackingStartTime(owner)
	if !exists {
		t.Error("Expected tracking to exist")
	}
	if !startTime.Equal(retrievedTime) {
		t.Errorf("Expected same time, got %v and %v", startTime, retrievedTime)
	}
}

func TestPodReleaseTracker_StopTracking(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner := "test-namespace/test-pod"

	// 未开始跟踪时停止应该返回false
	_, exists := tracker.StopTracking(owner)
	if exists {
		t.Error("Expected tracking to not exist")
	}

	// 开始跟踪后停止应该返回true
	startTime := tracker.StartTracking(owner)
	retrievedTime, exists := tracker.StopTracking(owner)
	if !exists {
		t.Error("Expected tracking to exist")
	}
	if !startTime.Equal(retrievedTime) {
		t.Errorf("Expected same time, got %v and %v", startTime, retrievedTime)
	}

	// 停止后再次获取应该返回false
	_, exists = tracker.GetTrackingStartTime(owner)
	if exists {
		t.Error("Expected tracking to not exist after stop")
	}
}

func TestPodReleaseTracker_Cleanup(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	owner1 := "test-namespace/test-pod-1"
	owner2 := "test-namespace/test-pod-2"

	// 添加两个跟踪记录
	tracker.StartTracking(owner1)
	tracker.StartTracking(owner2)

	// 验证两个记录都存在
	if _, exists := tracker.GetTrackingStartTime(owner1); !exists {
		t.Error("Expected tracking 1 to exist")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); !exists {
		t.Error("Expected tracking 2 to exist")
	}

	// 清理后应该都消失（因为Cleanup会清理所有记录，不管时间）
	tracker.CleanupAll()
	if _, exists := tracker.GetTrackingStartTime(owner1); exists {
		t.Error("Expected tracking 1 to be cleaned up")
	}
	if _, exists := tracker.GetTrackingStartTime(owner2); exists {
		t.Error("Expected tracking 2 to be cleaned up")
	}
}

func TestPodReleaseTracker_Concurrency(t *testing.T) {
	tracker := &PodReleaseTracker{
		podReleaseStarts: make(map[string]time.Time),
	}

	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

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
