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

package subscriber

import (
	"k8s.io/client-go/tools/cache"
)

var _ cache.ResourceEventHandler = (*RawChain)(nil)

// RawChain holds the raw subscribers to any K8s resource / object changes in
// the K8s watchers.
//
// RawChain itself is an implementation of cache.ResourceEventHandler with
// an additional Register method for attaching children subscribers to the
// chain.
type RawChain struct {
	list

	subs []cache.ResourceEventHandler
}

// NewRaw creates a new raw subscriber list.
func NewRawChain() *RawChain {
	return &RawChain{}
}

// Register registers the raw event handler as a subscriber.
func (l *RawChain) Register(cb cache.ResourceEventHandler) {
	l.Lock()
	l.subs = append(l.subs, cb)
	l.Unlock()
}

// NotifyAdd notifies all the subscribers of an add event to an object.
func (l *RawChain) OnAdd(obj interface{}) {
	l.RLock()
	defer l.RUnlock()
	for _, s := range l.subs {
		s.OnAdd(obj)
	}
}

// NotifyUpdate notifies all the subscribers of an update event to an object.
func (l *RawChain) OnUpdate(oldObj, newObj interface{}) {
	l.RLock()
	defer l.RUnlock()
	for _, s := range l.subs {
		s.OnUpdate(oldObj, newObj)
	}
}

// NotifyDelete notifies all the subscribers of an update event to an object.
func (l *RawChain) OnDelete(obj interface{}) {
	l.RLock()
	defer l.RUnlock()
	for _, s := range l.subs {
		s.OnDelete(obj)
	}
}
