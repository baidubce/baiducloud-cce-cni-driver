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

package types

import (
	"net"

	"gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
)

var testMACAddress MACAddr = [6]byte{1, 2, 3, 4, 5, 6}

type MACAddrSuite struct{}

var _ = check.Suite(&MACAddrSuite{})

func (s *MACAddrSuite) TestHardwareAddr(c *check.C) {
	var expectedAddress net.HardwareAddr
	expectedAddress = []byte{1, 2, 3, 4, 5, 6}
	result := testMACAddress.hardwareAddr()

	c.Assert(result, checker.DeepEquals, expectedAddress)
}

func (s *MACAddrSuite) TestString(c *check.C) {
	expectedStr := "01:02:03:04:05:06"
	result := testMACAddress.String()

	c.Assert(result, check.Equals, expectedStr)
}
