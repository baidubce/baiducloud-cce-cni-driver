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

package keymutex

import (
	"sync"
	"time"
)

const (
	waitLockSleepTime = 10 * time.Microsecond
)

type KeyMutex struct {
	m sync.Map
}

func (k *KeyMutex) TryLock(key interface{}) bool {
	_, ok := k.m.LoadOrStore(key, struct{}{})
	return !ok
}

func (k *KeyMutex) WaitLock(key interface{}, timeout time.Duration) bool {
	start := time.Now()

	for {
		if k.TryLock(key) {
			return true
		}

		time.Sleep(waitLockSleepTime)
		if time.Since(start) >= timeout {
			return false
		}
	}
	return false
}

func (k *KeyMutex) UnLock(key interface{}) {
	k.m.Delete(key)
}
