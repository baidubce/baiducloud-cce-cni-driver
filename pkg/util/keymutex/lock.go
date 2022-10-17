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
	"fmt"
	"sync"
	"time"

	"github.com/alexflint/go-filemutex"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	waitLockSleepTime = 10 * time.Microsecond
	fileLockTimeOut   = 10 * time.Second
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

type Locker struct {
	m *filemutex.FileMutex
}

// Close close
func (l *Locker) Close() error {
	if l.m != nil {
		return l.m.Unlock()
	}
	return nil
}

// GrabFileLock get file lock with timeout 11seconds
func GrabFileLock(lockfilePath string) (*Locker, error) {
	var m, err = filemutex.New(lockfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %s: %v", lockfilePath, err)
	}

	err = wait.PollImmediate(200*time.Millisecond, fileLockTimeOut, func() (bool, error) {
		if err := m.Lock(); err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %v", err)
	}

	return &Locker{
		m: m,
	}, nil
}
