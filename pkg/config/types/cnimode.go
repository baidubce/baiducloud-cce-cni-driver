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

package types

// ContainerNetworkMode defines container config
type ContainerNetworkMode string

const (
	// K8sNetworkModeKubenet using kubenet
	K8sNetworkModeKubenet ContainerNetworkMode = "kubenet"
	// CCEModeRouteVeth using vpc route plus veth
	CCEModeRouteVeth ContainerNetworkMode = "vpc-route-veth"
	// CCEModeRouteIPVlan using vpc route plus ipvlan
	CCEModeRouteIPVlan ContainerNetworkMode = "vpc-route-ipvlan"
	// CCEModeRouteAutoDetect using vpc route and auto detects veth or ipvlan due to kernel version
	CCEModeRouteAutoDetect ContainerNetworkMode = "vpc-route-auto-detect"
	// CCEModeSecondaryIPVeth using vpc secondary ip plus veth
	CCEModeSecondaryIPVeth ContainerNetworkMode = "vpc-secondary-ip-veth"
	// CCEModeSecondaryIPIPVlan using vpc secondary ip plus ipvlan
	CCEModeSecondaryIPIPVlan ContainerNetworkMode = "vpc-secondary-ip-ipvlan"
	// CCEModeSecondaryIPAutoDetect using vpc secondary ip and auto detects veth or ipvlan due to kernel version
	CCEModeSecondaryIPAutoDetect ContainerNetworkMode = "vpc-secondary-ip-auto-detect"
	// CCEModeBBCSecondaryIPVeth using vpc secondary ip plus veth (BBC only)
	CCEModeBBCSecondaryIPVeth ContainerNetworkMode = "bbc-vpc-secondary-ip-veth"
	// CCEModeBBCSecondaryIPIPVlan using vpc secondary ip plus ipvlan (BBC only)
	CCEModeBBCSecondaryIPIPVlan ContainerNetworkMode = "bbc-vpc-secondary-ip-ipvlan"
	// CCEModeBBCSecondaryIPAutoDetect using vpc secondary ip and auto detects veth or ipvlan due to kernel version (BBC only)
	CCEModeBBCSecondaryIPAutoDetect ContainerNetworkMode = "bbc-vpc-secondary-ip-auto-detect"

	// CCEModeHostLocalSecondaryIP using pre-allocated secondary ip on primary eni (BCC and BBC both ok)
	CCEModeHostLocalSecondaryIPVeth       ContainerNetworkMode = "host-local-secondary-ip-veth"
	CCEModeHostLocalSecondaryIPIPVlan     ContainerNetworkMode = "host-local-secondary-ip-ipvlan"
	CCEModeHostLocalSecondaryIPAutoDetect ContainerNetworkMode = "host-local-secondary-ip-auto-detect"
)

func IsCCECNIModeBasedOnVPCRoute(mode ContainerNetworkMode) bool {
	if mode == CCEModeRouteVeth ||
		mode == CCEModeRouteIPVlan ||
		mode == CCEModeRouteAutoDetect {
		return true
	}

	return false
}

func IsCCECNIModeAutoDetect(mode ContainerNetworkMode) bool {
	switch mode {
	case CCEModeRouteAutoDetect:
		return true
	case CCEModeSecondaryIPAutoDetect:
		return true
	case CCEModeBBCSecondaryIPAutoDetect:
		return true
	case CCEModeHostLocalSecondaryIPAutoDetect:
		return true
	default:
		return false
	}
}

func IsCCECNIModeBasedOnBCCSecondaryIP(mode ContainerNetworkMode) bool {
	if mode == CCEModeSecondaryIPVeth ||
		mode == CCEModeSecondaryIPIPVlan ||
		mode == CCEModeSecondaryIPAutoDetect {
		return true
	}

	return false
}

func IsCCECNIModeBasedOnBBCSecondaryIP(mode ContainerNetworkMode) bool {
	if mode == CCEModeBBCSecondaryIPVeth ||
		mode == CCEModeBBCSecondaryIPIPVlan ||
		mode == CCEModeBBCSecondaryIPAutoDetect {
		return true
	}

	return false
}

func IsCCECNIModeBasedOnSecondaryIP(mode ContainerNetworkMode) bool {
	return IsCCECNIModeBasedOnBCCSecondaryIP(mode) ||
		IsCCECNIModeBasedOnBBCSecondaryIP(mode)
}

func IsCCEHostLocalSecondaryIPMode(mode ContainerNetworkMode) bool {
	if mode == CCEModeHostLocalSecondaryIPVeth ||
		mode == CCEModeHostLocalSecondaryIPIPVlan ||
		mode == CCEModeHostLocalSecondaryIPAutoDetect {
		return true
	}
	return false
}

func IsCCECNIMode(mode ContainerNetworkMode) bool {
	return IsCCECNIModeBasedOnVPCRoute(mode) ||
		IsCCECNIModeBasedOnBCCSecondaryIP(mode) ||
		IsCCECNIModeBasedOnBBCSecondaryIP(mode) ||
		IsCCEHostLocalSecondaryIPMode(mode)
}

func IsKubenetMode(mode ContainerNetworkMode) bool {
	return mode == K8sNetworkModeKubenet
}
