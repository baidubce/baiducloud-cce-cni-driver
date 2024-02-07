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

package ipam

import (
	"errors"
	"net"
)

var errNotSupported = errors.New("Operation not supported")

// noOpAllocator implements ipam.Allocator with no-op behavior.
// It is used for IPAMDelegatedPlugin, where the CNI binary is responsible for assigning IPs
// without relying on the cce daemon or operator.
type noOpAllocator struct{}

func (n *noOpAllocator) Allocate(ip net.IP, owner string) (*AllocationResult, error) {
	return nil, errNotSupported
}

func (n *noOpAllocator) AllocateWithoutSyncUpstream(ip net.IP, owner string) (*AllocationResult, error) {
	return nil, errNotSupported
}

func (n *noOpAllocator) Release(ip net.IP) error {
	return errNotSupported
}

func (n *noOpAllocator) AllocateNext(owner string) (*AllocationResult, error) {
	return nil, errNotSupported
}

func (n *noOpAllocator) AllocateNextWithoutSyncUpstream(owner string) (*AllocationResult, error) {
	return nil, errNotSupported
}

func (n *noOpAllocator) Dump() (map[string]string, string) {
	return nil, "delegated to plugin"
}

func (n *noOpAllocator) RestoreFinished() {
}
