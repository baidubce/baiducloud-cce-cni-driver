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
	"net"
	"time"

	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/addressing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/mtu"
)

type ownerMock struct{}

func (o *ownerMock) K8sEventReceived(resourceApiGroup, scope string, action string, valid, equal bool) {
}
func (o *ownerMock) K8sEventProcessed(scope string, action string, status bool)   {}
func (o *ownerMock) RegisterNetResourceSetSubscriber(s subscriber.NetResourceSet) {}

func (o *ownerMock) UpdateNetResourceSetResource()                                      {}
func (o *ownerMock) LocalAllocCIDRsUpdated(ipv4AllocCIDRs, ipv6AllocCIDRs []*cidr.CIDR) {}

var mtuMock = mtu.NewConfiguration(0, false, false, false, 1500, nil)

func (s *IPAMSuite) TestAllocatedIPDump(c *C) {
	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

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

func (s *IPAMSuite) TestExpirationTimer(c *C) {
	ip := net.ParseIP("1.1.1.1")
	timeout := 50 * time.Millisecond

	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	err := ipam.AllocateIP(ip, "foo")
	c.Assert(err, IsNil)

	uuid, err := ipam.StartExpirationTimer(ip, timeout)
	c.Assert(err, IsNil)
	c.Assert(uuid, Not(Equals), "")
	// must fail, already registered
	uuid, err = ipam.StartExpirationTimer(ip, timeout)
	c.Assert(err, Not(IsNil))
	c.Assert(uuid, Equals, "")
	// must fail, already in use
	err = ipam.AllocateIP(ip, "foo")
	c.Assert(err, Not(IsNil))
	// Let expiration timer expire
	time.Sleep(2 * timeout)
	// Must succeed, IP must be released again
	err = ipam.AllocateIP(ip, "foo")
	c.Assert(err, IsNil)
	// register new expiration timer
	uuid, err = ipam.StartExpirationTimer(ip, timeout)
	c.Assert(err, IsNil)
	c.Assert(uuid, Not(Equals), "")
	// attempt to stop with an invalid uuid, must fail
	err = ipam.StopExpirationTimer(ip, "unknown-uuid")
	c.Assert(err, Not(IsNil))
	// stop expiration with valid uuid
	err = ipam.StopExpirationTimer(ip, uuid)
	c.Assert(err, IsNil)
	// Let expiration timer expire
	time.Sleep(2 * timeout)
	// must fail as IP is properly in use now
	err = ipam.AllocateIP(ip, "foo")
	c.Assert(err, Not(IsNil))
	// release IP for real
	err = ipam.ReleaseIP(ip)
	c.Assert(err, IsNil)

	// allocate IP again
	err = ipam.AllocateIP(ip, "foo")
	c.Assert(err, IsNil)
	// register expiration timer
	uuid, err = ipam.StartExpirationTimer(ip, timeout)
	c.Assert(err, IsNil)
	c.Assert(uuid, Not(Equals), "")
	// release IP, must also stop expiration timer
	err = ipam.ReleaseIP(ip)
	c.Assert(err, IsNil)
	// allocate same IP again
	err = ipam.AllocateIP(ip, "foo")
	c.Assert(err, IsNil)
	// register expiration timer must succeed even though stop was never called
	uuid, err = ipam.StartExpirationTimer(ip, timeout)
	c.Assert(err, IsNil)
	c.Assert(uuid, Not(Equals), "")
	// release IP
	err = ipam.ReleaseIP(ip)
	c.Assert(err, IsNil)

}

func (s *IPAMSuite) TestAllocateNextWithExpiration(c *C) {
	timeout := 50 * time.Millisecond

	fakeAddressing := linux.NewNodeAddressing()
	ipam := NewIPAM(fakeAddressing, &testConfiguration{}, &ownerMock{}, &ownerMock{}, &mtuMock)

	ipv4, ipv6, err := ipam.AllocateNextWithExpiration("", "foo", timeout)
	c.Assert(err, IsNil)

	// IPv4 address must be in use
	err = ipam.AllocateIP(ipv4.IP, "foo")
	c.Assert(err, Not(IsNil))
	// IPv6 address must be in use
	err = ipam.AllocateIP(ipv6.IP, "foo")
	c.Assert(err, Not(IsNil))
	// Let expiration timer expire
	time.Sleep(2 * timeout)
	// IPv4 address must be available again
	err = ipam.AllocateIP(ipv4.IP, "foo")
	c.Assert(err, IsNil)
	// IPv6 address must be available again
	err = ipam.AllocateIP(ipv6.IP, "foo")
	c.Assert(err, IsNil)
	// Release IPs
	err = ipam.ReleaseIP(ipv4.IP)
	c.Assert(err, IsNil)
	err = ipam.ReleaseIP(ipv6.IP)
	c.Assert(err, IsNil)

	// Allocate IPs again and test stopping the expiration timer
	ipv4, ipv6, err = ipam.AllocateNextWithExpiration("", "foo", timeout)
	c.Assert(err, IsNil)

	// Stop expiration timer for IPv4 address
	err = ipam.StopExpirationTimer(ipv4.IP, ipv4.ExpirationUUID)
	c.Assert(err, IsNil)

	// Let expiration timer expire
	time.Sleep(2 * timeout)

	// IPv4 address must be in use
	err = ipam.AllocateIP(ipv4.IP, "foo")
	c.Assert(err, Not(IsNil))
	// IPv6 address must be available again
	err = ipam.AllocateIP(ipv6.IP, "foo")
	c.Assert(err, IsNil)
	// Release IPv4 address
	err = ipam.ReleaseIP(ipv4.IP)
	c.Assert(err, IsNil)
}
