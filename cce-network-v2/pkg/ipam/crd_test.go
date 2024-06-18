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
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/addressing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/trigger"
)

func TestIPNotAvailableInPoolError(t *testing.T) {
	err := NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	err2 := NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	assert.Equal(t, err, err2)
	assert.True(t, errors.Is(err, err2))

	err = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))

	err = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	err2 = errors.New("another error")
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))

	err = errors.New("another error")
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))

	err = NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	err2 = nil
	assert.False(t, errors.Is(err, err2))

	err = nil
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("1.1.1.1"))
	assert.False(t, errors.Is(err, err2))

	// We don't match against strings. It must be the sentinel value.
	err = errors.New("IP 2.1.1.1 is not available")
	err2 = NewIPNotAvailableInPoolError(net.ParseIP("2.1.1.1"))
	assert.NotEqual(t, err, err2)
	assert.False(t, errors.Is(err, err2))
}

type testConfigurationCRD struct{}

func (t *testConfigurationCRD) IPv4Enabled() bool                        { return true }
func (t *testConfigurationCRD) IPv6Enabled() bool                        { return false }
func (t *testConfigurationCRD) HealthCheckingEnabled() bool              { return true }
func (t *testConfigurationCRD) UnreachableRoutesEnabled() bool           { return false }
func (t *testConfigurationCRD) IPAMMode() string                         { return ipamOption.IPAMCRD }
func (t *testConfigurationCRD) BlacklistConflictingRoutesEnabled() bool  { return false }
func (t *testConfigurationCRD) SetIPv4NativeRoutingCIDR(cidr *cidr.CIDR) {}
func (t *testConfigurationCRD) GetIPv4NativeRoutingCIDR() *cidr.CIDR     { return nil }
func (t *testConfigurationCRD) IPv4NativeRoutingCIDR() *cidr.CIDR        { return nil }
func (t *testConfigurationCRD) GetCCEEndpointGC() time.Duration          { return 0 }
func (t *testConfigurationCRD) GetFixedIPTimeout() time.Duration         { return 0 }

func newFakeNodeStore(conf Configuration, c *C) *nodeStore {
	t, err := trigger.NewTrigger(trigger.Parameters{
		Name:        "fake-crd-allocator-node-refresher",
		MinInterval: 3 * time.Second,
		TriggerFunc: func(reasons []string) {},
	})
	if err != nil {
		log.WithError(err).Fatal("Unable to initialize NetResourceSet synchronization trigger")
	}
	store := &nodeStore{
		allocators:         []*crdAllocator{},
		allocationPoolSize: map[Family]int{},
		conf:               conf,
		refreshTrigger:     t,
	}
	return store
}

func (s *IPAMSuite) TestMarkForReleaseNoAllocate(c *C) {
	cn := newNetResourceSet("node1", 4, 4, 0)
	dummyResource := ipamTypes.AllocationIP{Resource: "foo"}
	for i := 1; i <= 4; i++ {
		cn.Spec.IPAM.Pool[fmt.Sprintf("1.1.1.%d", i)] = dummyResource
	}

	fakeAddressing := linux.NewNodeAddressing()
	conf := &testConfigurationCRD{}
	initNodeStore.Do(func() {
		sharedNodeStore = newFakeNodeStore(conf, c)
		sharedNodeStore.ownNode = cn
	})
	ipam := NewIPAM(fakeAddressing, conf, &ownerMock{}, &ownerMock{}, &mtuMock)
	sharedNodeStore.updateLocalNodeResource(cn)

	// Allocate the first 3 IPs
	for i := 1; i <= 3; i++ {
		epipv4, _ := addressing.NewCCEIPv4(fmt.Sprintf("1.1.1.%d", i))
		_, err := ipam.IPv4Allocator.Allocate(epipv4.IP(), fmt.Sprintf("test%d", i))
		c.Assert(err, IsNil)
	}

	// Update 1.1.1.4 as marked for release like operator would.
	cn.Status.IPAM.ReleaseIPs["1.1.1.4"] = ipamOption.IPAMMarkForRelease
	// Attempts to allocate 1.1.1.4 should fail, since it's already marked for release
	epipv4, _ := addressing.NewCCEIPv4("1.1.1.4")
	_, err := ipam.IPv4Allocator.Allocate(epipv4.IP(), "test")
	c.Assert(err, NotNil)
	// Call agent's CRD update function. status for 1.1.1.4 should change from marked for release to ready for release
	sharedNodeStore.updateLocalNodeResource(cn)
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["1.1.1.4"]), checker.Equals, ipamOption.IPAMReadyForRelease)

	// Verify that 1.1.1.3 is denied for release, since it's already in use
	cn.Status.IPAM.ReleaseIPs["1.1.1.3"] = ipamOption.IPAMMarkForRelease
	sharedNodeStore.updateLocalNodeResource(cn)
	c.Assert(string(cn.Status.IPAM.ReleaseIPs["1.1.1.3"]), checker.Equals, ipamOption.IPAMDoNotRelease)
}
