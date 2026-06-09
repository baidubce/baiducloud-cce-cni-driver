//go:build !privileged_tests

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
	"fmt"
	"net"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/addressing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/mtu"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/stretchr/testify/assert"
)

// init init 函数用于初始化 fake k8s 客户端，避免 nil 指针解引用
// 如果初始化失败则 panic 出错误信息
func init() {
	// 初始化 fake k8s 客户端，避免 nil 指针解引用
	if err := k8s.InitNewFakeK8sClient(); err != nil {
		panic(fmt.Sprintf("failed to initialize fake k8s client: %v", err))
	}
}

type ownerMock struct{}

func (o *ownerMock) K8sEventReceived(resourceApiGroup, scope string, action string, valid, equal bool) {
}
func (o *ownerMock) K8sEventProcessed(scope string, action string, status bool)   {}
func (o *ownerMock) RegisterNetResourceSetSubscriber(s subscriber.NetResourceSet) {}

func (o *ownerMock) UpdateNetResourceSetResource()                                      {}
func (o *ownerMock) LocalAllocCIDRsUpdated(ipv4AllocCIDRs, ipv6AllocCIDRs []*cidr.CIDR) {}
func (o *ownerMock) ResourceType() string {
	return ccev2.NetResourceSetEventHandlerTypeEth
}

var mtuMock = mtu.NewConfiguration(0, false, false, false, 1500, nil)

// mockAllocator is a mock implementation of Allocator interface
type mockAllocator struct {
	allocatedIPs map[string]bool
}

func newMockAllocator() *mockAllocator {
	return &mockAllocator{
		allocatedIPs: make(map[string]bool),
	}
}

func (m *mockAllocator) Allocate(ip net.IP, owner string) (*AllocationResult, error) {
	m.allocatedIPs[ip.String()] = true
	return &AllocationResult{IP: ip}, nil
}

func (m *mockAllocator) AllocateWithoutSyncUpstream(ip net.IP, owner string) (*AllocationResult, error) {
	m.allocatedIPs[ip.String()] = true
	return &AllocationResult{IP: ip}, nil
}

func (m *mockAllocator) Release(ip net.IP) error {
	delete(m.allocatedIPs, ip.String())
	return nil
}

func (m *mockAllocator) AllocateNext(owner string) (*AllocationResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockAllocator) AllocateNextWithoutSyncUpstream(owner string) (*AllocationResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockAllocator) Dump() (map[string]string, string) {
	result := make(map[string]string)
	for ip := range m.allocatedIPs {
		result[ip] = ""
	}
	return result, ""
}

func (m *mockAllocator) RestoreFinished() {}

func (s *IPAMSuite) TestAllocatedIPDump(c *C) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	ipv4 := fakeIPv4AllocCIDRIP(fakeAddressing)
	ipv6 := fakeIPv6AllocCIDRIP(fakeAddressing)

	for i := 0; i < 10; i++ {
		_, err := addressing.NewCCEIPv4(ipv4.String())
		c.Assert(err, IsNil)
		nextIP(ipv4)

		_, err = addressing.NewCCEIPv6(ipv6.String())
		c.Assert(err, IsNil)
		nextIP(ipv6)
	}

	allocv4, allocv6, status := ipam.Dump()
	c.Assert(status, Not(Equals), "")

	// Test the format of the dumped ip addresses
	for ip := range allocv4 {
		c.Assert(net.ParseIP(ip), NotNil)
	}
	for ip := range allocv6 {
		c.Assert(net.ParseIP(ip), NotNil)
	}
}

// TestReleaseIPWithOwner tests the ReleaseIPWithOwner function
func TestReleaseIPWithOwner(t *testing.T) {
	type TestFields struct {
		ipam *IPAM
		ip   net.IP
	}

	type TestArgs struct {
		ipStr string
		owner string
	}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields)
		Args        TestArgs
		WantErr     error
	}

	cases := []TestCase{
		{
			Name: "TestReleaseIPWithOwner_GivenInvalidIP_WhenCalled_ThenReturnError",
			TestSetup: func() *TestFields {
				fakeAddressing := linux.NewNodeAddressing()
				ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
				return &TestFields{
					ipam: ipam,
					ip:   nil,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Verify IP was not released
				_, exists := fields.ipam.owner["invalid-ip"]
				assert.False(t, exists, "IP should not exist in owner map")
			},
			Args: TestArgs{
				ipStr: "invalid-ip",
				owner: "test-owner",
			},
			WantErr: fmt.Errorf("invalid IP address: invalid-ip"),
		},
		{
			Name: "TestReleaseIPWithOwner_GivenIPNotAllocated_WhenCalled_ThenReturnError",
			TestSetup: func() *TestFields {
				fakeAddressing := linux.NewNodeAddressing()
				ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
				return &TestFields{
					ipam: ipam,
					ip:   net.ParseIP("192.168.1.100"),
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Verify IP was not released
				_, exists := fields.ipam.owner[fields.ip.String()]
				assert.False(t, exists, "IP should not exist in owner map")
			},
			Args: TestArgs{
				ipStr: "192.168.1.100",
				owner: "test-owner",
			},
			WantErr: fmt.Errorf("IP 192.168.1.100 is not allocated"),
		},
		{
			Name: "TestReleaseIPWithOwner_GivenOwnerMismatch_WhenCalled_ThenReturnError",
			TestSetup: func() *TestFields {
				fakeAddressing := linux.NewNodeAddressing()
				ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
				ip := net.ParseIP("192.168.1.100")
				// Manually set owner in the map to simulate allocated IP
				ipam.owner[ip.String()] = "original-owner"
				return &TestFields{
					ipam: ipam,
					ip:   ip,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Verify IP was not released (owner still exists)
				owner, exists := fields.ipam.owner[fields.ip.String()]
				assert.True(t, exists, "IP should still exist in owner map")
				assert.Equal(t, "original-owner", owner, "Owner should remain unchanged")
			},
			Args: TestArgs{
				ipStr: "192.168.1.100",
				owner: "different-owner",
			},
			WantErr: fmt.Errorf("IP 192.168.1.100 owner mismatch: expected different-owner, got original-owner"),
		},
		{
			Name: "TestReleaseIPWithOwner_GivenOwnerMatches_WhenCalled_ThenReleaseIP",
			TestSetup: func() *TestFields {
				fakeAddressing := linux.NewNodeAddressing()
				ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
				ip := net.ParseIP("192.168.1.100")
				// Set up mock allocator
				mockAlloc := newMockAllocator()
				ipam.IPv4Allocator = mockAlloc
				// Manually set owner in the map to simulate allocated IP
				ipam.owner[ip.String()] = "test-owner"
				// Allocate the IP in mock allocator
				_, _ = mockAlloc.AllocateWithoutSyncUpstream(ip, "test-owner")
				return &TestFields{
					ipam: ipam,
					ip:   ip,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Verify IP was released (owner removed from map)
				_, exists := fields.ipam.owner[fields.ip.String()]
				assert.False(t, exists, "IP should be removed from owner map after release")
			},
			Args: TestArgs{
				ipStr: "192.168.1.100",
				owner: "test-owner",
			},
			WantErr: nil,
		},
		{
			Name: "TestReleaseIPWithOwner_GivenIPv6OwnerMatches_WhenCalled_ThenReleaseIP",
			TestSetup: func() *TestFields {
				fakeAddressing := linux.NewNodeAddressing()
				ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
				ip := net.ParseIP("2001:db8::1")
				// Set up mock allocator
				mockAlloc := newMockAllocator()
				ipam.IPv6Allocator = mockAlloc
				// Manually set owner in the map to simulate allocated IP
				ipam.owner[ip.String()] = "test-owner"
				// Allocate the IP in mock allocator
				_, _ = mockAlloc.AllocateWithoutSyncUpstream(ip, "test-owner")
				return &TestFields{
					ipam: ipam,
					ip:   ip,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Verify IP was released (owner removed from map)
				_, exists := fields.ipam.owner[fields.ip.String()]
				assert.False(t, exists, "IP should be removed from owner map after release")
			},
			Args: TestArgs{
				ipStr: "2001:db8::1",
				owner: "test-owner",
			},
			WantErr: nil,
		},
		{
			Name: "TestReleaseIPWithOwner_GivenEmptyIPString_WhenCalled_ThenReturnError",
			TestSetup: func() *TestFields {
				fakeAddressing := linux.NewNodeAddressing()
				ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
				return &TestFields{
					ipam: ipam,
					ip:   nil,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Verify nothing was changed
				assert.Equal(t, 0, len(fields.ipam.owner), "Owner map should be empty")
			},
			Args: TestArgs{
				ipStr: "",
				owner: "test-owner",
			},
			WantErr: fmt.Errorf("invalid IP address: "),
		},
		{
			Name: "TestReleaseIPWithOwner_GivenEmptyOwner_WhenCalled_ThenReturnErrorIfOwnerMismatch",
			TestSetup: func() *TestFields {
				fakeAddressing := linux.NewNodeAddressing()
				ipam := NewIPAM(nodeTypes.GetName(), fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)
				ip := net.ParseIP("192.168.1.100")
				// Manually set owner in the map to simulate allocated IP with non-empty owner
				ipam.owner[ip.String()] = "original-owner"
				return &TestFields{
					ipam: ipam,
					ip:   ip,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Verify IP was not released (owner still exists)
				owner, exists := fields.ipam.owner[fields.ip.String()]
				assert.True(t, exists, "IP should still exist in owner map")
				assert.Equal(t, "original-owner", owner, "Owner should remain unchanged")
			},
			Args: TestArgs{
				ipStr: "192.168.1.100",
				owner: "",
			},
			WantErr: fmt.Errorf("IP 192.168.1.100 owner mismatch: expected , got original-owner"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			gotErr := fields.ipam.ReleaseIPWithOwner(tc.Args.ipStr, tc.Args.owner)
			if (gotErr == nil) != (tc.WantErr == nil) {
				t.Errorf("got err %v, want %v", gotErr, tc.WantErr)
			} else if gotErr != nil && tc.WantErr != nil && gotErr.Error() != tc.WantErr.Error() {
				t.Errorf("got err %v, want %v", gotErr, tc.WantErr)
			}
			if tc.TestPostRun != nil {
				tc.TestPostRun(fields)
			}
		})
	}
}
