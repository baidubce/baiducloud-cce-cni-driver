//go:build ipam_provider_operator

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

package main

import (
	// These dependencies should be included only when this file is included in the build.
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/allocator/clusterpool"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
)

func init() {
	allocatorProviders[ipamOption.IPAMClusterPool] = &clusterpool.AllocatorOperator{}
	allocatorProviders[ipamOption.IPAMClusterPoolV2] = &clusterpool.AllocatorOperator{}
}
