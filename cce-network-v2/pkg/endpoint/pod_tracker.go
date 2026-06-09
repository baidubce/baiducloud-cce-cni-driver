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
	"sync"
	"time"
)

const (
	// PodTrackerCleanupInterval 定期清理间隔
	PodTrackerCleanupInterval = 5 * time.Minute
	// PodTrackerExpirationTime Pod跟踪记录过期时间
	PodTrackerExpirationTime = 10 * time.Minute
)

// PodAllocationTracker 跟踪Pod IP分配的端到端时间
type PodAllocationTracker struct {
	mu            sync.RWMutex
	podStartTimes map[string]time.Time // owner -> 首次申请时间
}

var podTracker = &PodAllocationTracker{
	podStartTimes: make(map[string]time.Time),
}

// StartTracking 开始跟踪Pod的IP分配，如果已经在跟踪则返回首次开始时间
func (t *PodAllocationTracker) StartTracking(owner string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()

	if startTime, exists := t.podStartTimes[owner]; exists {
		return startTime // 已经在跟踪，返回首次开始时间
	}

	startTime := time.Now()
	t.podStartTimes[owner] = startTime
	return startTime
}

// GetTrackingStartTime 获取Pod的跟踪开始时间，不停止跟踪
func (t *PodAllocationTracker) GetTrackingStartTime(owner string) (time.Time, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	startTime, exists := t.podStartTimes[owner]
	return startTime, exists
}

// StopTracking 停止跟踪Pod并返回开始时间
func (t *PodAllocationTracker) StopTracking(owner string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	startTime, exists := t.podStartTimes[owner]
	if exists {
		delete(t.podStartTimes, owner)
	}
	return startTime, exists
}

// Cleanup 清理过期的Pod跟踪记录
func (t *PodAllocationTracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for owner, startTime := range t.podStartTimes {
		// 清理过期的记录
		if now.Sub(startTime) > PodTrackerExpirationTime {
			delete(t.podStartTimes, owner)
		}
	}
}

// CleanupAll 清理所有Pod跟踪记录（用于测试）
func (t *PodAllocationTracker) CleanupAll() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.podStartTimes = make(map[string]time.Time)
}

// PodReleaseTracker 跟踪Pod IP回收的端到端时间
type PodReleaseTracker struct {
	mu               sync.RWMutex
	podReleaseStarts map[string]time.Time // owner -> 首次回收时间
}

var podReleaseTracker = &PodReleaseTracker{
	podReleaseStarts: make(map[string]time.Time),
}

// StartTracking 开始跟踪Pod的IP回收，如果已经在跟踪则返回首次开始时间
func (t *PodReleaseTracker) StartTracking(owner string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()

	if startTime, exists := t.podReleaseStarts[owner]; exists {
		return startTime // 已经在跟踪，返回首次开始时间
	}

	startTime := time.Now()
	t.podReleaseStarts[owner] = startTime
	return startTime
}

// GetTrackingStartTime 获取Pod的跟踪开始时间，不停止跟踪
func (t *PodReleaseTracker) GetTrackingStartTime(owner string) (time.Time, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	startTime, exists := t.podReleaseStarts[owner]
	return startTime, exists
}

// StopTracking 停止跟踪Pod并返回开始时间
func (t *PodReleaseTracker) StopTracking(owner string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	startTime, exists := t.podReleaseStarts[owner]
	if exists {
		delete(t.podReleaseStarts, owner)
	}
	return startTime, exists
}

// Cleanup 清理过期的Pod回收跟踪记录
func (t *PodReleaseTracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for owner, startTime := range t.podReleaseStarts {
		// 清理过期的记录
		if now.Sub(startTime) > PodTrackerExpirationTime {
			delete(t.podReleaseStarts, owner)
		}
	}
}

// CleanupAll 清理所有Pod回收跟踪记录（用于测试）
func (t *PodReleaseTracker) CleanupAll() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.podReleaseStarts = make(map[string]time.Time)
}

// StartPodTrackerCleaner 启动定期清理Pod跟踪记录的后台任务
func StartPodTrackerCleaner() {
	go func() {
		ticker := time.NewTicker(PodTrackerCleanupInterval)
		defer ticker.Stop()

		for range ticker.C {
			podTracker.Cleanup()
			podReleaseTracker.Cleanup()
		}
	}()
}
