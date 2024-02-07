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
)

var _ ENI = (*ENIChain)(nil)

// ENI is implemented by event handlers responding to ENI events.
type ENI interface {
	OnAddENI(node *ccev2.ENI) error
	OnUpdateENI(oldObj, newObj *ccev2.ENI) error
	OnDeleteENI(node *ccev2.ENI) error
}

// ENIChain holds the subsciber.ENI implementations that are
// notified when reacting to ENI resource / object changes in the K8s
// watchers.
//
// ENIChain itself is an implementation of subscriber.ENIChain
// with an additional Register method for attaching children subscribers to the
// chain.
type ENIChain struct {
	list

	subs []ENI
}

// NewENIChain creates a ENIChain ready for its
// Register method to be called.
func NewENIChain() *ENIChain {
	return &ENIChain{}
}

// Register registers s as a subscriber for reacting to ENI objects
// into the list.
func (l *ENIChain) Register(s ENI) {
	l.Lock()
	l.subs = append(l.subs, s)
	l.Unlock()
}

// OnAddENI notifies all the subscribers of an add event to a ENI.
func (l *ENIChain) OnAddENI(node *ccev2.ENI) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnAddENI(node); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}

// OnUpdateENI notifies all the subscribers of an update event to a ENI.
func (l *ENIChain) OnUpdateENI(oldNode, newNode *ccev2.ENI) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnUpdateENI(oldNode, newNode); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}

// OnDeleteENI notifies all the subscribers of an update event to a ENI.
func (l *ENIChain) OnDeleteENI(node *ccev2.ENI) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnDeleteENI(node); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}
