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
package pststrategy

import (
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// EnableReuseIPPSTS check if reuse ip is enabled
func EnableReuseIPPSTS(psts *ccev2.PodSubnetTopologySpread) bool {
	if psts == nil {
		return false
	}
	if psts.Spec.Strategy == nil {
		return false
	}
	switch psts.Spec.Strategy.Type {
	case ccev2.IPAllocTypeFixed:
		return true
	}
	return psts.Spec.Strategy.EnableReuseIPAddress
}
