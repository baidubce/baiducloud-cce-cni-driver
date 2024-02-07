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
	"fmt"
	"math/big"
	"net"

	"github.com/cilium/ipam/service/ipallocator"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
)

// PoolAllocator is an IP allocator allocating out of a particular CIDR pool
type PoolAllocator struct {
	PoolID         types.PoolID
	AllocationCIDR *cidr.CIDR
	allocator      *ipallocator.Range
}

// NewPoolAllocator returns a new Allocator
func NewPoolAllocator(poolID types.PoolID, allocationCIDR *cidr.CIDR) (*PoolAllocator, error) {
	allocator, err := ipallocator.NewCIDRRange(allocationCIDR.IPNet)
	if err != nil {
		return nil, fmt.Errorf("unable to create IP allocator: %s", err)
	}

	return &PoolAllocator{PoolID: poolID, allocator: allocator, AllocationCIDR: allocationCIDR}, nil
}

// Free returns the number of available IPs for allocation
func (s *PoolAllocator) Free() int {
	return s.allocator.Free()
}

// Allocate allocates a particular IP
func (s *PoolAllocator) Allocate(ip net.IP) error {
	return s.allocator.Allocate(ip)
}

// AllocateMany allocates multiple IP addresses. The operation succeeds if all
// IPs can be allocated. On failure, all IPs are released again.
func (s *PoolAllocator) AllocateMany(num int) ([]net.IP, error) {
	ips := make([]net.IP, 0, num)

	for i := 0; i < num; i++ {
		ip, err := s.allocator.AllocateNext()
		if err != nil {
			for _, ip = range ips {
				s.allocator.Release(ip)
			}
			return nil, err
		}

		ips = append(ips, ip)
	}

	return ips, nil
}

// ReleaseMany releases a slice of IP addresses
func (s *PoolAllocator) ReleaseMany(ips []net.IP) {
	for _, ip := range ips {
		s.allocator.Release(ip)
	}
}

type SubRangePoolAllocator struct {
	*PoolAllocator
}

// NewSubRangePoolAllocator returns a new Allocator
func NewSubRangePoolAllocator(poolID types.PoolID, allocationCIDR *cidr.CIDR, reserve int) (*SubRangePoolAllocator, error) {
	allocator, err := NewPoolAllocator(poolID, allocationCIDR)
	if err != nil {
		return nil, err
	}
	// reserve the first ip
	allocator.AllocateMany(reserve)
	return &SubRangePoolAllocator{allocator}, nil
}

func (s *SubRangePoolAllocator) ReservedAllocateMany(startIP, endIP net.IP, num int) ([]net.IP, error) {
	ips := make([]net.IP, 0, num)

	var (
		startIndex = bigForIP(s.AllocationCIDR.IPNet.IP)
		endIndex   = big.NewInt(0).Add(startIndex, big.NewInt(int64(s.AllocationCIDR.AvailableIPs())))
	)

	if startIP != nil && !s.AllocationCIDR.Contains(startIP) {
		return nil, fmt.Errorf("start ip %s is not in cidr %s", startIndex.String(), s.AllocationCIDR.String())
	}
	if endIP != nil && !s.AllocationCIDR.Contains(endIP) {
		return nil, fmt.Errorf("end ip %s is not in cidr %s", endIndex.String(), s.AllocationCIDR.String())
	}
	if startIP != nil {
		startIndex = bigForIP(startIP)
	}
	if endIP != nil {
		endIndex = bigForIP(endIP)
	}

	for ; startIndex.Cmp(endIndex) <= 0; startIndex.Add(startIndex, big.NewInt(1)) {
		ip := bigIntToip(startIndex)
		if s.allocator.Has(ip) {
			continue
		}
		if !s.AllocationCIDR.Contains(ip) {
			break
		}
		err := s.Allocate(ip)
		if err == nil {
			ips = append(ips, ip)
		}
		if len(ips) == num {
			break
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no ip available")
	}
	return ips, nil
}

// bigForIP creates a big.Int based on the provided net.IP
func bigForIP(ip net.IP) *big.Int {
	// NOTE: Convert to 16-byte representation so we can
	// handle v4 and v6 values the same way.
	return big.NewInt(0).SetBytes(ip.To16())
}

// addIPOffset adds the provided integer offset to a base big.Int representing a net.IP
// NOTE: If you started with a v4 address and overflow it, you get a v6 result.
func addIPOffset(base *big.Int, offset int) net.IP {
	r := big.NewInt(0).Add(base, big.NewInt(int64(offset))).Bytes()
	r = append(make([]byte, 16), r...)
	return net.IP(r[len(r)-16:])
}

// calculateIPOffset calculates the integer offset of ip from base such that
// base + offset = ip. It requires ip >= base.
func calculateIPOffset(base *big.Int, ip net.IP) int {
	return int(big.NewInt(0).Sub(bigForIP(ip), base).Int64())
}

func bigIntToip(bigint *big.Int) net.IP {
	r := bigint.Bytes()
	r = append(make([]byte, 16), r...)
	return net.IP(r[len(r)-16:])
}
