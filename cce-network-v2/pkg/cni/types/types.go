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
	"encoding/json"
	"fmt"
	"net"
	"os"

	cniTypes "github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
)

// NetConf is the CCE specific CNI network configuration
type NetConf struct {
	cniTypes.NetConf
	MTU  int  `json:"mtu"`
	Args Args `json:"args"`
	IPAM IPAM `json:"ipam,omitempty"` // Shadows the JSON field "ipam" in cniTypes.NetConf.
}

// IPAM is the CCE specific CNI IPAM configuration
type IPAM struct {
	cniTypes.IPAM
	ipamTypes.IPAMSpec
	ENI         *api.ENISpec      `json:"eni"`
	Routes      []*cniTypes.Route `json:"routes"`
	EnableDebug bool              `json:"enable-debug"`
	LogFormat   string            `json:"log-format"`
	LogFile     string            `json:"log-file"`

	// for private cloud base
	// use host as gateway
	HostLink       string `json:"hostLink"`
	UseHostGateWay bool   `json:"use-host-gate-way"`
}

// NetConfList is a CNI chaining configuration
type NetConfList struct {
	Plugins []*NetConf `json:"plugins,omitempty"`
}

func parsePrevResult(n *NetConf) (*NetConf, error) {
	if n.RawPrevResult != nil {
		resultBytes, err := json.Marshal(n.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %v", err)
		}
		res, err := version.NewResult(n.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
		n.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}

	return n, nil
}

// ReadNetConf reads a CNI configuration file and returns the corresponding
// NetConf structure
func ReadNetConf(path string) (*NetConf, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Unable to read CNI configuration '%s': %s", path, err)
	}

	netConfList := &NetConfList{}
	if err := json.Unmarshal(b, netConfList); err == nil {
		for _, plugin := range netConfList.Plugins {
			if plugin.Type == "cptp" {
				return parsePrevResult(plugin)
			}
		}
	}

	return LoadNetConf(b)
}

// ReadRdmaNetConf reads a RDMA CNI configuration file and returns the corresponding
// RDMA NetConf structure
func ReadRdmaNetConf(path string) (*NetConf, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Unable to read RDMA CNI configuration '%s': %s", path, err)
	}

	netConfList := &NetConfList{}
	if err := json.Unmarshal(b, netConfList); err == nil {
		for _, plugin := range netConfList.Plugins {
			if plugin.Type == "roce" {
				return parsePrevResult(plugin)
			}
		}
	}

	return LoadNetConf(b)
}

// LoadNetConf unmarshals a CCE network configuration from JSON and returns
// a NetConf together with the CNI version
func LoadNetConf(bytes []byte) (*NetConf, error) {
	n := &NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, fmt.Errorf("failed to load netconf: %s", err)
	}

	return parsePrevResult(n)
}

// ArgsSpec is the specification of additional arguments of the CNI ADD call
type ArgsSpec struct {
	cniTypes.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               cniTypes.UnmarshallableString
	K8S_POD_NAMESPACE          cniTypes.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID cniTypes.UnmarshallableString
}

// Args contains arbitrary information a scheduler
// can pass to the cni plugin
type Args struct{}
