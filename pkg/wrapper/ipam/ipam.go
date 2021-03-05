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

package ipam

import (
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ipam"
)

// Interface is the wrapper interface for the containernetworking ipam
type Interface interface {
	ExecAdd(plugin string, netconf []byte) (types.Result, error)
	ExecDel(plugin string, netconf []byte) error
	ConfigureIface(ifName string, res *current.Result) error
}

type ipamType struct {
}

// New returns a new IPAM
func New() Interface {
	return &ipamType{}
}

func (*ipamType) ExecAdd(plugin string, netconf []byte) (types.Result, error) {
	return ipam.ExecAdd(plugin, netconf)
}

func (*ipamType) ExecDel(plugin string, netconf []byte) error {
	return ipam.ExecDel(plugin, netconf)
}

func (*ipamType) ConfigureIface(ifName string, res *current.Result) error {
	return ipam.ConfigureIface(ifName, res)
}
