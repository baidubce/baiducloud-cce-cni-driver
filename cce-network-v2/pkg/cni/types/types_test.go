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
	"os"
	"path"
	"testing"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	"gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
)

func Test(t *testing.T) {
	check.TestingT(t)
}

type CNITypesSuite struct{}

var _ = check.Suite(&CNITypesSuite{})

func testConfRead(c *check.C, confContent string, netconf *NetConf) {
	dir, err := os.MkdirTemp("", "cce-cnitype-testsuite")
	c.Assert(err, check.IsNil)
	defer os.RemoveAll(dir)

	p := path.Join(dir, "conf1")
	err = os.WriteFile(p, []byte(confContent), 0644)
	c.Assert(err, check.IsNil)

	netConf, err := ReadNetConf(p)
	c.Assert(err, check.IsNil)

	c.Assert(netConf, checker.DeepEquals, netconf)
}

func (t *CNITypesSuite) TestReadCNIConf(c *check.C) {
	confFile1 := `
{
  "name": "cce",
  "type": "cce-cni"
}
`

	netConf1 := NetConf{
		NetConf: cnitypes.NetConf{
			Name: "cce",
			Type: "cce-cni",
		},
	}
	testConfRead(c, confFile1, &netConf1)

	confFile2 := `
{
  "name": "cce",
  "type": "cce-cni",
  "mtu": 9000
}
`

	netConf2 := NetConf{
		NetConf: cnitypes.NetConf{
			Name: "cce",
			Type: "cce-cni",
		},
		MTU: 9000,
	}
	testConfRead(c, confFile2, &netConf2)
}

func (t *CNITypesSuite) TestReadCNIConfClusterPoolV2(c *check.C) {
	confFile1 := `
{
  "cniVersion":"0.3.1",
  "name":"cce",
  "type":"cce-cni",
  "plugins": [
    {
      "cniVersion":"0.3.1",
      "type":"cce-cni",
      "ipam": {
        "pod-cidr-allocation-threshold": 10,
        "pod-cidr-release-threshold": 20
      }
    }
  ]
}
`
	netConf1 := NetConf{
		NetConf: cnitypes.NetConf{
			CNIVersion: "0.3.1",
			Name:       "cce",
			Type:       "cce-cni",
		},
		IPAM: IPAM{
			// we remove the configuration of plugins field from v2.10.0,
			// instead of plugins field, we use the configmap of agent directly
		},
	}
	testConfRead(c, confFile1, &netConf1)
}

func (t *CNITypesSuite) TestReadCNIConfIPAMType(c *check.C) {
	confFile := `
{
  "cniVersion":"0.3.1",
  "name":"cce",
  "type":"cce-cni",
  "plugins": [
    {
      "cniVersion":"0.3.1",
      "type":"cce-cni",
      "ipam": {
        "type": "delegated-ipam"
      }
    }
  ]
}
`
	netConf := NetConf{
		NetConf: cnitypes.NetConf{
			CNIVersion: "0.3.1",
			Name:       "cce",
			Type:       "cce-cni",
		},
		IPAM: IPAM{
			// we remove the configuration of plugins field from v2.10.0,
			// instead of plugins field, we use the configmap of agent directly
		},
	}
	testConfRead(c, confFile, &netConf)
}

func (t *CNITypesSuite) TestReadCNIConfError(c *check.C) {
	// Try to read errorneous CNI configuration file with MTU provided as
	// string instead of int
	errorConf := `
{
  "name": "cce",
  "type": "cce-cni",
  "mtu": "9000"
}
`

	dir, err := os.MkdirTemp("", "cce-cnitype-testsuite")
	c.Assert(err, check.IsNil)
	defer os.RemoveAll(dir)

	p := path.Join(dir, "errorconf")
	err = os.WriteFile(p, []byte(errorConf), 0644)
	c.Assert(err, check.IsNil)

	_, err = ReadNetConf(p)
	c.Assert(err, check.Not(check.IsNil))
}
