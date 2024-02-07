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

package k8s

import (
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/annotation"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/addressing"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// ParseNodeAddressType converts a Kubernetes NodeAddressType to a CCE
// NodeAddressType. If the Kubernetes NodeAddressType does not have a
// corresponding CCE AddressType, returns an error.
func ParseNodeAddressType(k8sAddress corev1.NodeAddressType) (addressing.AddressType, error) {

	var err error
	convertedAddr := addressing.AddressType(k8sAddress)

	switch convertedAddr {
	case addressing.NodeExternalDNS, addressing.NodeExternalIP, addressing.NodeHostName, addressing.NodeInternalIP, addressing.NodeInternalDNS:
	default:
		err = fmt.Errorf("invalid Kubernetes NodeAddressType %s", convertedAddr)
	}
	return convertedAddr, err
}

// ParseNode parses a kubernetes node to a cce node
func ParseNode(k8sNode *corev1.Node) *nodeTypes.Node {
	scopedLog := log.WithFields(logrus.Fields{
		logfields.NodeName:  k8sNode.Name,
		logfields.K8sNodeID: k8sNode.UID,
	})
	addrs := []nodeTypes.Address{}
	for _, addr := range k8sNode.Status.Addresses {
		// We only care about this address types,
		// we ignore all other types.
		switch addr.Type {
		case corev1.NodeInternalIP, corev1.NodeExternalIP:
		default:
			continue
		}
		// If the address is not set let's not parse it at all.
		// This can be the case for corev1.NodeExternalIPs
		if addr.Address == "" {
			continue
		}
		ip := net.ParseIP(addr.Address)
		if ip == nil {
			scopedLog.WithFields(logrus.Fields{
				logfields.IPAddr: addr.Address,
				"type":           addr.Type,
			}).Warn("Ignoring invalid node IP")
			continue
		}

		addressType, err := ParseNodeAddressType(addr.Type)

		if err != nil {
			scopedLog.WithError(err).Warn("invalid address type for node")
		}

		na := nodeTypes.Address{
			Type: addressType,
			IP:   ip,
		}
		addrs = append(addrs, na)
	}

	k8sNodeAddHostIP := func(annotation string) {
		if cceInternalIP, ok := k8sNode.Annotations[annotation]; !ok || cceInternalIP == "" {
			scopedLog.Debugf("Missing %s. Annotation required when IPSec Enabled", annotation)
		} else if ip := net.ParseIP(cceInternalIP); ip == nil {
			scopedLog.Debugf("ParseIP %s error", cceInternalIP)
		} else {
			na := nodeTypes.Address{
				Type: addressing.NodeCCEInternalIP,
				IP:   ip,
			}
			addrs = append(addrs, na)
			scopedLog.Debugf("Add NodeCCEInternalIP: %s", ip)
		}
	}

	k8sNodeAddHostIP(annotation.CCEHostIP)
	k8sNodeAddHostIP(annotation.CCEHostIPv6)

	newNode := &nodeTypes.Node{
		Name:        k8sNode.Name,
		Cluster:     option.Config.ClusterID,
		IPAddresses: addrs,
	}

	if len(k8sNode.Spec.PodCIDRs) != 0 {
		if len(k8sNode.Spec.PodCIDRs) > 2 {
			scopedLog.WithField("podCIDR", k8sNode.Spec.PodCIDRs).Errorf("Invalid PodCIDRs expected 1 or 2 PodCIDRs, received %d", len(k8sNode.Spec.PodCIDRs))
		} else {
			for _, podCIDR := range k8sNode.Spec.PodCIDRs {
				if allocCIDR, err := cidr.ParseCIDR(podCIDR); err != nil {
					scopedLog.WithError(err).WithField("podCIDR", k8sNode.Spec.PodCIDR).Warn("Invalid PodCIDR value for node")
				} else {
					if allocCIDR.IP.To4() != nil {
						newNode.IPv4AllocCIDR = allocCIDR
					} else {
						newNode.IPv6AllocCIDR = allocCIDR
					}
				}
			}
		}
	} else if len(k8sNode.Spec.PodCIDR) != 0 {
		if allocCIDR, err := cidr.ParseCIDR(k8sNode.Spec.PodCIDR); err != nil {
			scopedLog.WithError(err).WithField(logfields.V4Prefix, k8sNode.Spec.PodCIDR).Warn("Invalid PodCIDR value for node")
		} else {
			if allocCIDR.IP.To4() != nil {
				newNode.IPv4AllocCIDR = allocCIDR
			} else {
				newNode.IPv6AllocCIDR = allocCIDR
			}
		}
	}
	// Spec.PodCIDR takes precedence since it's
	// the CIDR assigned by k8s controller manager
	// In case it's invalid or empty then we fall back to our annotations.
	if newNode.IPv4AllocCIDR == nil {
		if ipv4CIDR, ok := k8sNode.Annotations[annotation.V4CIDRName]; !ok || ipv4CIDR == "" {
			scopedLog.Debug("Empty IPv4 CIDR annotation in node")
		} else {
			allocCIDR, err := cidr.ParseCIDR(ipv4CIDR)
			if err != nil {
				scopedLog.WithError(err).WithField(logfields.V4Prefix, ipv4CIDR).Error("BUG, invalid IPv4 annotation CIDR in node")
			} else {
				newNode.IPv4AllocCIDR = allocCIDR
			}
		}
	}

	if newNode.IPv6AllocCIDR == nil {
		if ipv6CIDR, ok := k8sNode.Annotations[annotation.V6CIDRName]; !ok || ipv6CIDR == "" {
			scopedLog.Debug("Empty IPv6 CIDR annotation in node")
		} else {
			allocCIDR, err := cidr.ParseCIDR(ipv6CIDR)
			if err != nil {
				scopedLog.WithError(err).WithField(logfields.V6Prefix, ipv6CIDR).Error("BUG, invalid IPv6 annotation CIDR in node")
			} else {
				newNode.IPv6AllocCIDR = allocCIDR
			}
		}
	}

	newNode.Labels = k8sNode.GetLabels()

	return newNode
}
