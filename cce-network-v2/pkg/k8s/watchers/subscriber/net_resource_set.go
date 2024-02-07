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
	"fmt"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

var _ NetResourceSet = (*NetResourceSetChain)(nil)

var log = logging.NewSubysLogger("netresourceset-subscriber")

// NetResourceSet is implemented by event handlers responding to NetResourceSet events.
type NetResourceSet interface {
	OnAddNetResourceSet(node *ccev2.NetResourceSet, swg *lock.StoppableWaitGroup) error
	OnUpdateNetResourceSet(oldObj, newObj *ccev2.NetResourceSet, swg *lock.StoppableWaitGroup) error
	OnDeleteNetResourceSet(node *ccev2.NetResourceSet, swg *lock.StoppableWaitGroup) error
}

// NetResourceSetChain holds the subsciber.NetResourceSet implementations that are
// notified when reacting to NetResourceSet resource / object changes in the K8s
// watchers.
//
// NetResourceSetChain itself is an implementation of subscriber.NetResourceSetChain
// with an additional Register method for attaching children subscribers to the
// chain.
type NetResourceSetChain struct {
	list

	subs []NetResourceSet
}

// NewNetResourceSetChain creates a NetResourceSetChain ready for its
// Register method to be called.
func NewNetResourceSetChain() *NetResourceSetChain {
	return &NetResourceSetChain{}
}

// Register registers s as a subscriber for reacting to NetResourceSet objects
// into the list.
func (l *NetResourceSetChain) Register(s NetResourceSet) {
	l.Lock()
	l.subs = append(l.subs, s)
	l.Unlock()
}

// OnAddNetResourceSet notifies all the subscribers of an add event to a NetResourceSet.
func (l *NetResourceSetChain) OnAddNetResourceSet(node *ccev2.NetResourceSet, swg *lock.StoppableWaitGroup) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnAddNetResourceSet(node, swg); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}

// OnUpdateNetResourceSet notifies all the subscribers of an update event to a NetResourceSet.
func (l *NetResourceSetChain) OnUpdateNetResourceSet(oldNode, newNode *ccev2.NetResourceSet, swg *lock.StoppableWaitGroup) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnUpdateNetResourceSet(oldNode, newNode, swg); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}

// OnDeleteNetResourceSet notifies all the subscribers of an update event to a NetResourceSet.
func (l *NetResourceSetChain) OnDeleteNetResourceSet(node *ccev2.NetResourceSet, swg *lock.StoppableWaitGroup) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnDeleteNetResourceSet(node, swg); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}
