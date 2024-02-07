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

package annotation

const (
	// Prefix is the common prefix for all annotations
	Prefix = "cce.baidubce.com"

	// Name is an optional annotation to the NetworkPolicy
	// resource which specifies the name of the policy node to which all
	// rules should be applied to.
	Name = Prefix + ".name"

	// V4CIDRName is the annotation name used to store the IPv4
	// pod CIDR in the node's annotations.
	V4CIDRName = Prefix + ".network.ipv4-pod-cidr"
	// V6CIDRName is the annotation name used to store the IPv6
	// pod CIDR in the node's annotations.
	V6CIDRName = Prefix + ".network.ipv6-pod-cidr"

	// V4HealthName is the annotation name used to store the IPv4
	// address of the cce-health endpoint in the node's annotations.
	V4HealthName = Prefix + ".network.ipv4-health-ip"
	// V6HealthName is the annotation name used to store the IPv6
	// address of the cce-health endpoint in the node's annotations.
	V6HealthName = Prefix + ".network.ipv6-health-ip"

	// V4IngressName is the annotation name used to store the IPv4
	// address of the Ingress listener in the node's annotations.
	V4IngressName = Prefix + ".network.ipv4-Ingress-ip"
	// V6IngressName is the annotation name used to store the IPv6
	// address of the Ingress listener in the node's annotations.
	V6IngressName = Prefix + ".network.ipv6-Ingress-ip"

	// CCEHostIP is the annotation name used to store the IPv4 address
	// of the cce host interface in the node's annotations.
	CCEHostIP = Prefix + ".network.ipv4-cce-host"

	// CCEHostIPv6 is the annotation name used to store the IPv6 address
	// of the cce host interface in the node's annotation.
	CCEHostIPv6 = Prefix + ".network.ipv6-cce-host"

	// CCEEncryptionKey is the annotation name used to store the encryption
	// key of the cce host interface in the node's annotation.
	CCEEncryptionKey = Prefix + ".network.encryption-key"

	// GlobalService if set to true, marks a service to become a global
	// service
	GlobalService = Prefix + "/global-service"

	// SharedService if set to false, prevents a service from being shared,
	// the default is true if GlobalService is set, otherwise false,
	// Setting the annotation SharedService to false while setting
	// GlobalService to true allows to expose remote endpoints without
	// sharing local endpoints.
	SharedService = Prefix + "/shared-service"

	// ServiceAffinity annotations determines the preferred endpoint destination
	// Allowed values:
	//  - local
	//		preferred endpoints from local cluster if available
	//  - remote
	// 		preferred endpoints from remote cluster if available
	//  - none (default)
	//		no preference. Default behavior if this annotation does not exist
	ServiceAffinity = Prefix + "/service-affinity"

	// ProxyVisibility is the annotation name used to indicate whether proxy
	// visibility should be enabled for a given pod (i.e., all traffic for the
	// pod is redirected to the proxy for the given port / protocol in the
	// annotation
	ProxyVisibility = Prefix + ".proxy-visibility"

	// NoTrack is the annotation name used to store the port and protocol
	// that we should bypass kernel conntrack for a given pod. This applies for
	// both TCP and UDP connection. Current use case is NodeLocalDNS.
	NoTrack = Prefix + ".no-track-port"
)
