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
	"sort"
	"testing"
)

func TestPriorityQueue(t *testing.T) {
	pq := New()
	elements := []int{5, 3, 7, 8, 6, 2, 9}
	for _, e := range elements {
		pq.Insert(e, int64(e))
	}

	sort.Ints(elements)
	for _, e := range elements {
		item, err := pq.Pop()
		if err != nil {
			t.Fatalf(err.Error())
		}

		if e != item {
			t.Fatalf("expected %v, got %v", e, item)
		}
	}
}

func TestPriorityQueueUpdate(t *testing.T) {
	pq := New()
	pq.Insert("foo", 3)
	pq.Insert("bar", 4)
	pq.UpdatePriority("bar", 2)

	item, err := pq.Pop()
	if err != nil {
		t.Fatal(err.Error())
	}

	if item.(string) != "bar" {
		t.Fatal("priority update failed")
	}
}

func TestPriorityQueueLen(t *testing.T) {
	pq := New()
	if pq.Len() != 0 {
		t.Fatal("empty queue should have length of 0")
	}

	pq.Insert("foo", 1)
	pq.Insert("bar", 1)
	if pq.Len() != 2 {
		t.Fatal("queue should have length of 2 after 2 inserts")
	}
}

func TestDoubleAddition(t *testing.T) {
	var err error

	pq := New()
	err = pq.Insert("foo", 2)
	err = pq.Insert("bar", 3)
	err = pq.Insert("bar", 1)

	if pq.Len() != 2 && err != ErrorItemDuplicated {
		t.Fatal("queue should ignore inserting the same element twice")
	}

	item, _ := pq.Pop()
	if item.(string) != "foo" {
		t.Fatal("queue should ignore duplicate insert, not update existing item")
	}
}

func TestPopEmptyQueue(t *testing.T) {
	pq := New()
	_, err := pq.Pop()
	if err == nil && err != ErrorEmptyQueue {
		t.Fatal("should produce error when performing pop on empty queue")
	}
}

func TestUpdateNonExistingItem(t *testing.T) {
	pq := New()

	pq.Insert("foo", 4)
	err := pq.UpdatePriority("bar", 5)

	if pq.Len() != 1 && err != ErrorItemNotFound {
		t.Fatal("update should not add items")
	}

	item, _ := pq.Pop()
	if item.(string) != "foo" {
		t.Fatalf("update should not overwrite item, expected \"foo\", got \"%v\"", item.(string))
	}
}
