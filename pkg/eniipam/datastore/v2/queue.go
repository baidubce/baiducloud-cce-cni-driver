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

package v2

import (
	utilpq "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/priorityqueue"
)

type priorityQueue struct {
	pq utilpq.PriorityQueue
}

func newPriorityQueue() *priorityQueue {
	return &priorityQueue{
		pq: utilpq.New(),
	}
}

func (p *priorityQueue) Len() int {
	return p.pq.Len()
}

func (p *priorityQueue) Insert(addr *AddressInfo) error {
	return p.pq.Insert(addr, addr.UnassignedTime.UnixNano())
}

func (p *priorityQueue) Pop() (*AddressInfo, error) {
	item, err := p.pq.Pop()
	if err != nil {
		return nil, err
	}
	return item.(*AddressInfo), nil
}

func (p *priorityQueue) Top() (*AddressInfo, error) {
	item, err := p.pq.Top()
	if err != nil {
		return nil, err
	}
	return item.(*AddressInfo), nil
}

func (p *priorityQueue) Remove(addr *AddressInfo) {
	p.pq.Remove(addr)
}

func (p *priorityQueue) UpdatePriority(addr *AddressInfo, newPriority int64) error {
	return p.pq.UpdatePriority(addr, newPriority)
}
