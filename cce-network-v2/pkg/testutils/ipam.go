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

package testutils

import (
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	_ "github.com/cilium/deepequal-gen/generators"
	_ "github.com/golang/mock/mockgen/model"
	_ "k8s.io/code-generator"
	_ "sigs.k8s.io/controller-tools/pkg/deepcopy"
)

// FakeAcknowledgeReleaseIps Fake acknowledge IPs marked for release like cce agent would.
func FakeAcknowledgeReleaseIps(cn *v2.NetResourceSet) {
	for ip, status := range cn.Status.IPAM.ReleaseIPs {
		if status == ipamOption.IPAMMarkForRelease {
			cn.Status.IPAM.ReleaseIPs[ip] = ipamOption.IPAMReadyForRelease
		}
	}
}
