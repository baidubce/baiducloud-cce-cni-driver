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

package option

const (
	// IPAMKubernetes is the value to select the Kubernetes PodCIDR based
	// hostscope IPAM mode
	IPAMKubernetes = "kubernetes"

	// IPAMCRD is the value to select the CRD-backed IPAM plugin for
	// option.IPAM
	IPAMCRD = "crd"

	// IPAMVpcEni is the value to select the ENI Secondary IPAM plugin for option.IPAM
	IPAMVpcEni = "vpc-eni"
	// IPAMVpcRoute is the value to select the VPC Router IPAM plugin for option.IPAM
	IPAMVpcRoute = "vpc-route"
	// IPAMRdma is the value to select the RDMA-Backed IPAM plugin for option.IPAM
	IPAMRdma = "rdma"

	// IPAMClusterPool is the value to select the cluster pool mode for
	// option.IPAM
	IPAMClusterPool = "cluster-pool"

	// IPAMClusterPoolV2 is the value to select cluster pool version 2
	IPAMClusterPoolV2 = "cluster-pool-v2beta"

	// IPAMPrivateCloudBase is the value to select the baidu private plugin for option IPAM
	IPAMPrivateCloudBase = "privatecloudbase"

	// IPAMDelegatedPlugin is the value to select CNI delegated IPAM plugin mode.
	// In this mode, CCE CNI invokes another CNI binary (the delegated plugin) for IPAM.
	// See https://www.cni.dev/docs/spec/#section-4-plugin-delegation
	IPAMDelegatedPlugin = "delegated-plugin"
)

const (
	IPAMMarkForRelease  = "marked-for-release"
	IPAMReadyForRelease = "ready-for-release"
	IPAMDoNotRelease    = "do-not-release"
	IPAMReleased        = "released"
)

// ENIPDBlockSizeIPv4 is the number of IPs available on an ENI IPv4 prefix. Currently, AWS only supports /28 fixed size
// prefixes. Every /28 prefix contains 16 IP addresses.
// See https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-prefix-eni.html#ec2-prefix-basics for more details
const ENIPDBlockSizeIPv4 = 16
