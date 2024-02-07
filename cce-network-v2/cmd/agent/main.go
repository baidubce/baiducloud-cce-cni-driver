//go:build go1.18

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

// Ensure build fails on versions of Go that are not supported by CCE.
// This build tag should be kept in sync with the version specified in go.mod.

package main

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/agent/cmd"
)

func main() {
	cmd.Execute()
}
