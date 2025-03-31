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

// Package nswrapper is a wrapper interface for the containernetworking ns plugin
package nswrapper

import (
	"github.com/containernetworking/plugins/pkg/ns"
)

// NS is the wrapper interface for the containernetworking ns plugin
type NS interface {
	WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error
}

type nsType struct {
}

// NewNS returns a new NS
func NewNS() NS {
	return &nsType{}
}

func (*nsType) WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error {
	return ns.WithNetNSPath(nspath, toRun)
}
