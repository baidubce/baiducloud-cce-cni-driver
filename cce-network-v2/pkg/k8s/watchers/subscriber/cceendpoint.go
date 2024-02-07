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

var _ CCEEndpoint = (*CCEEndpointChain)(nil)

// CCEEndpoint is implemented by event handlers responding to CCEEndpoint events.
type CCEEndpoint interface {
	OnAddCCEEndpoint(node *ccev2.CCEEndpoint) error
	OnUpdateCCEEndpoint(oldObj, newObj *ccev2.CCEEndpoint) error
	OnDeleteCCEEndpoint(node *ccev2.CCEEndpoint) error
}

// CCEEndpointChain holds the subsciber.CCEEndpoint implementations that are
// notified when reacting to CCEEndpoint resource / object changes in the K8s
// watchers.
//
// CCEEndpointChain itself is an implementation of subscriber.CCEEndpointChain
// with an additional Register method for attaching children subscribers to the
// chain.
type CCEEndpointChain struct {
	list

	subs []CCEEndpoint
}

// NewCCEEndpointChain creates a CCEEndpointChain ready for its
// Register method to be called.
func NewCCEEndpointChain() *CCEEndpointChain {
	return &CCEEndpointChain{}
}

// Register registers s as a subscriber for reacting to CCEEndpoint objects
// into the list.
func (l *CCEEndpointChain) Register(s CCEEndpoint) {
	l.Lock()
	l.subs = append(l.subs, s)
	l.Unlock()
}

// OnAddCCEEndpoint notifies all the subscribers of an add event to a CCEEndpoint.
func (l *CCEEndpointChain) OnAddCCEEndpoint(node *ccev2.CCEEndpoint) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnAddCCEEndpoint(node); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}

// OnUpdateCCEEndpoint notifies all the subscribers of an update event to a CCEEndpoint.
func (l *CCEEndpointChain) OnUpdateCCEEndpoint(oldNode, newNode *ccev2.CCEEndpoint) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnUpdateCCEEndpoint(oldNode, newNode); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}

// OnDeleteCCEEndpoint notifies all the subscribers of an update event to a CCEEndpoint.
func (l *CCEEndpointChain) OnDeleteCCEEndpoint(node *ccev2.CCEEndpoint) error {
	l.RLock()
	defer l.RUnlock()
	errs := []error{}
	for _, s := range l.subs {
		if err := s.OnDeleteCCEEndpoint(node); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Errors: %v", errs)
	}
	return nil
}
