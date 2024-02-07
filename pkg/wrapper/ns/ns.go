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

package ns

import (
	"github.com/containernetworking/plugins/pkg/ns"
)

// Interface is the wrapper interface for the containernetworking ns
type Interface interface {
	WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error
	GetNS(nspath string) (ns.NetNS, error)
	GetCurrentNS() (ns.NetNS, error)
}

type nsType struct {
}

// New returns a new NS
func New() Interface {
	return &nsType{}
}

func (*nsType) WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error {
	return ns.WithNetNSPath(nspath, toRun)
}

func (*nsType) GetNS(nspath string) (ns.NetNS, error) {
	return ns.GetNS(nspath)
}

func (*nsType) GetCurrentNS() (ns.NetNS, error) {
	return ns.GetCurrentNS()
}
