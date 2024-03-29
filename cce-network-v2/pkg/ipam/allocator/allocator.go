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

package allocator

import (
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
)

// Allocator provides an IP allocator based on a list of Pools
//
// Implementations:
//   - PoolGroupAllocator
//   - NoOpAllocator
type Allocator interface {
	GetPoolQuota() types.PoolQuotaMap
	FirstPoolWithAvailableQuota(preferredPoolIDs []types.PoolID) (types.PoolID, int)
	PoolExists(poolID types.PoolID) bool
	Allocate(poolID types.PoolID, ip net.IP) error
	AllocateMany(poolID types.PoolID, num int) ([]net.IP, error)
	ReleaseMany(poolID types.PoolID, ips []net.IP) error
}
