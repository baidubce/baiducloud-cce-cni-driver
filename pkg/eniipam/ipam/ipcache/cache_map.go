package ipcache

import (
	"sync"
)

type CacheMap[T any] struct {
	lock sync.RWMutex
	pool map[string]T
}

func NewCacheMap[T any]() *CacheMap[T] {
	return &CacheMap[T]{
		pool: make(map[string]T),
	}
}

func (cm *CacheMap[T]) Add(key string, value T) {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	if key == "" {
		return
	}
	cm.pool[key] = value
}

func (cm *CacheMap[T]) AddIfNotExists(key string, value T) {
	if !cm.Exists(key) {
		cm.Add(key, value)
	}
}

func (cm *CacheMap[T]) Delete(key string) bool {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	_, ok := cm.pool[key]
	delete(cm.pool, key)
	return ok
}

func (cm *CacheMap[T]) Exists(key string) bool {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	_, ok := cm.pool[key]
	return ok
}

func (cm *CacheMap[T]) Get(key string) (T, bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	v, ok := cm.pool[key]
	return v, ok
}

func (cm *CacheMap[T]) ForEach(fun func(key string, item T) bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	for key, arr := range cm.pool {
		if !fun(key, arr) {
			return
		}
	}
}

type CacheMapArray[T any] struct {
	lock sync.RWMutex
	pool map[string][]T
}

func NewCacheMapArray[T any]() *CacheMapArray[T] {
	return &CacheMapArray[T]{
		pool: make(map[string][]T),
	}
}

func (cm *CacheMapArray[T]) Append(key string, value ...T) {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	if key == "" {
		return
	}
	cm.pool[key] = append(cm.pool[key], value...)
}

func (cm *CacheMapArray[T]) Delete(key string) bool {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	_, ok := cm.pool[key]
	delete(cm.pool, key)
	return ok
}

func (cm *CacheMapArray[T]) Exists(key string) bool {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	v, ok := cm.pool[key]
	if !ok || len(v) == 0 {
		return false
	}
	return true
}

func (cm *CacheMapArray[T]) Get(key string) ([]T, bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	v, ok := cm.pool[key]
	if !ok || len(v) == 0 {
		return v, false
	}
	return v, ok
}

func (cm *CacheMapArray[T]) ForEachSubItem(fun func(key string, index int, item T) bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	for key, arr := range cm.pool {
		for i, v := range arr {
			if !fun(key, i, v) {
				return
			}
		}
	}
}

func (cm *CacheMapArray[T]) ForEach(fun func(key string, item []T) bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	for key, arr := range cm.pool {
		if !fun(key, arr) {
			return
		}
	}
}
