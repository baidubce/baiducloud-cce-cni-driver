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

package cnitypes

import (
	"github.com/containernetworking/cni/pkg/types"
)

// Interface is the wrapper interface for containernetworking types package
type Interface interface {
	LoadArgs(args string, container interface{}) error
	PrintResult(result types.Result, version string) error
}

type cniTypes struct{}

// New returns a new Interface
func New() Interface {
	return &cniTypes{}
}

func (*cniTypes) LoadArgs(args string, container interface{}) error {
	return types.LoadArgs(args, container)
}

func (*cniTypes) PrintResult(result types.Result, version string) error {
	return types.PrintResult(result, version)
}
