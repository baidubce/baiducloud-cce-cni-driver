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

package priorityqueue

import (
	"container/heap"
	"errors"
)

var (
	ErrorEmptyQueue     = errors.New("empty queue")
	ErrorItemNotFound   = errors.New("item not found")
	ErrorItemDuplicated = errors.New("item duplicated")
)

// PriorityQueue represents the queue
type PriorityQueue struct {
	itemHeap *itemHeap
	lookup   map[interface{}]*item
}

// New initializes an empty priority queue.
func New() PriorityQueue {
	return PriorityQueue{
		itemHeap: &itemHeap{},
		lookup:   make(map[interface{}]*item),
	}
}

// Len returns the number of elements in the queue.
func (p *PriorityQueue) Len() int {
	return p.itemHeap.Len()
}

// Insert inserts a new element into the queue. No action is performed on duplicate elements.
func (p *PriorityQueue) Insert(v interface{}, priority int64) error {
	_, ok := p.lookup[v]
	if ok {
		return ErrorItemDuplicated
	}

	newItem := &item{
		value:    v,
		priority: priority,
	}
	heap.Push(p.itemHeap, newItem)
	p.lookup[v] = newItem

	return nil
}

// Pop removes the element with the highest priority from the queue and returns it.
// In case of an empty queue, an error is returned.
func (p *PriorityQueue) Pop() (interface{}, error) {
	if len(*p.itemHeap) == 0 {
		return nil, ErrorEmptyQueue
	}

	item := heap.Pop(p.itemHeap).(*item)
	delete(p.lookup, item.value)
	return item.value, nil
}

// Top returns the element with the highest priority from the queue.
func (p *PriorityQueue) Top() (interface{}, error) {
	if len(*p.itemHeap) == 0 {
		return nil, ErrorEmptyQueue
	}

	return (*p.itemHeap)[0].value, nil
}

// Remove removes element
func (p *PriorityQueue) Remove(x interface{}) {
	item, ok := p.lookup[x]
	if !ok {
		return
	}

	heap.Remove(p.itemHeap, item.index)
}

// UpdatePriority changes the priority of a given item.
// If the specified item is not present in the queue, no action is performed.
func (p *PriorityQueue) UpdatePriority(x interface{}, newPriority int64) error {
	item, ok := p.lookup[x]
	if !ok {
		return ErrorItemNotFound
	}

	item.priority = newPriority
	heap.Fix(p.itemHeap, item.index)

	return nil
}

type itemHeap []*item

type item struct {
	value    interface{}
	priority int64
	index    int
}

func (ih *itemHeap) Len() int {
	return len(*ih)
}

func (ih *itemHeap) Less(i, j int) bool {
	return (*ih)[i].priority < (*ih)[j].priority
}

func (ih *itemHeap) Swap(i, j int) {
	(*ih)[i], (*ih)[j] = (*ih)[j], (*ih)[i]
	(*ih)[i].index = i
	(*ih)[j].index = j
}

func (ih *itemHeap) Push(x interface{}) {
	it := x.(*item)
	it.index = len(*ih)
	*ih = append(*ih, it)
}

func (ih *itemHeap) Pop() interface{} {
	old := *ih
	item := old[len(old)-1]
	*ih = old[0 : len(old)-1]
	return item
}
