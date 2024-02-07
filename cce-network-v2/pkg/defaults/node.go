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

package defaults

import (
	"net"
)

const (
	// DefaultIPv4Prefix is the prefix for all the IPv4 addresses.
	// %d is substituted with the last byte of first global IPv4 address
	// configured on the system.
	DefaultIPv4Prefix = "10.%d.0.1"

	// DefaultIPv4PrefixLen is the length used to allocate container IPv4 addresses from.
	DefaultIPv4PrefixLen = 16

	// HostDevice is the name of the device that connects the cce IP
	// space with the host's networking model
	HostDevice = "cce_host"

	// SecondHostDevice is the name of the second interface of the host veth pair.
	SecondHostDevice = "cce_net"

	// CCEK8sAnnotationPrefix is the prefix key for the annotations used in kubernetes.
	CCEK8sAnnotationPrefix = "cce.baidubce.com/"

	// AgentNotReadyNodeTaint is a node taint which prevents pods from being
	// scheduled. Once cce is setup it is removed from the node. Mostly
	// used in cloud providers to prevent existing CNI plugins from managing
	// pods.
	AgentNotReadyNodeTaint = "node." + CCEK8sAnnotationPrefix + "agent-not-ready"
)

var (
	// Default addressing schema
	//
	// node:                    beef:beef:beef:beef:<node>:<node>:/96
	// lxc:                     beef:beef:beef:beef:<node>:<node>::<lxc>/128

	// ContainerIPv6Mask is the IPv6 prefix length for address assigned to
	// container. The default is L3 only and thus /128.
	ContainerIPv6Mask = net.CIDRMask(128, 128)

	// ContainerIPv4Mask is the IPv4 prefix length for address assigned to
	// container. The default is L3 only and thus /32.
	ContainerIPv4Mask = net.CIDRMask(32, 32)

	// IPv6DefaultRoute is the default IPv6 route.
	IPv6DefaultRoute = net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}

	// IPv4DefaultRoute is the default IPv4 route.
	IPv4DefaultRoute = net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
)
