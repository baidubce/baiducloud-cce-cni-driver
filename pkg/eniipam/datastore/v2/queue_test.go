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
	"testing"
	"time"
)

func TestPriorityQueue(t *testing.T) {
	var (
		pq = newPriorityQueue()

		now = time.Now()

		e1 = &AddressInfo{Address: "1.1.1.1", UnassignedTime: now}
		e2 = &AddressInfo{Address: "2.2.2.2", UnassignedTime: now}
		e3 = &AddressInfo{Address: "3.3.3.3", UnassignedTime: now.Add(time.Second)}
		e4 = &AddressInfo{Address: "4.4.4.4", UnassignedTime: now.Add(time.Second * 5)}
		e5 = &AddressInfo{Address: "5.5.5.5", UnassignedTime: now.Add(time.Second * 10)}
	)

	elements := []*AddressInfo{e5, e3, e1, e2, e4}
	sortedElements := []*AddressInfo{e1, e2, e3, e4, e5}
	for _, e := range elements {
		pq.Insert(e)
	}

	for _, e := range sortedElements {
		item, err := pq.Pop()
		if err != nil {
			t.Fatalf(err.Error())
		}

		if e != item {
			t.Fatalf("expected %v, got %v", e, item)
		}
	}
}
