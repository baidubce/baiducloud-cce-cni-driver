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

package types

import (
	"encoding/json"
	"net"
	"path"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/addressing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

// Identity represents the node identity of a node.
type Identity struct {
	Name    string
	Cluster string
}

// String returns the string representation on NodeIdentity.
func (nn Identity) String() string {
	return path.Join(nn.Cluster, nn.Name)
}

// ParseNetResourceSet parses a NetResourceSet custom resource and returns a Node
// instance. Invalid IP and CIDRs are silently ignored
func ParseNetResourceSet(n *ccev2.NetResourceSet) (node Node) {
	node = Node{
		Name:        n.Name,
		Cluster:     option.Config.ClusterID,
		ClusterID:   option.Config.ClusterID,
		Labels:      n.ObjectMeta.Labels,
		Annotations: n.Annotations,
	}

	for _, cidrString := range n.Spec.IPAM.PodCIDRs {
		ipnet, err := cidr.ParseCIDR(cidrString)
		if err == nil {
			if ipnet.IP.To4() != nil {
				if node.IPv4AllocCIDR == nil {
					node.IPv4AllocCIDR = ipnet
				} else {
					node.IPv4SecondaryAllocCIDRs = append(node.IPv4SecondaryAllocCIDRs, ipnet)
				}
			} else {
				if node.IPv6AllocCIDR == nil {
					node.IPv6AllocCIDR = ipnet
				} else {
					node.IPv6SecondaryAllocCIDRs = append(node.IPv6SecondaryAllocCIDRs, ipnet)
				}
			}
		}
	}

	for _, address := range n.Spec.Addresses {
		if ip := net.ParseIP(address.IP); ip != nil {
			node.IPAddresses = append(node.IPAddresses, Address{Type: address.Type, IP: ip})
		}
	}

	return
}

// ToNetResourceSet converts the node to a NetResourceSet
func (n *Node) ToNetResourceSet() *ccev2.NetResourceSet {
	var (
		podCIDRs    []string
		ipAddrs     []ccev2.NodeAddress
		annotations = map[string]string{}
	)

	if n.IPv4AllocCIDR != nil {
		podCIDRs = append(podCIDRs, n.IPv4AllocCIDR.String())
	}
	if n.IPv6AllocCIDR != nil {
		podCIDRs = append(podCIDRs, n.IPv6AllocCIDR.String())
	}
	for _, ipv4AllocCIDR := range n.IPv4SecondaryAllocCIDRs {
		podCIDRs = append(podCIDRs, ipv4AllocCIDR.String())
	}
	for _, ipv6AllocCIDR := range n.IPv6SecondaryAllocCIDRs {
		podCIDRs = append(podCIDRs, ipv6AllocCIDR.String())
	}

	for _, address := range n.IPAddresses {
		ipAddrs = append(ipAddrs, ccev2.NodeAddress{
			Type: address.Type,
			IP:   address.IP.String(),
		})
	}

	return &ccev2.NetResourceSet{
		ObjectMeta: v1.ObjectMeta{
			Name:        n.Name,
			Labels:      n.Labels,
			Annotations: annotations,
		},
		Spec: ccev2.NetResourceSpec{
			Addresses: ipAddrs,
			IPAM: ipamTypes.IPAMSpec{
				PodCIDRs: podCIDRs,
			},
		},
	}
}

// RegisterNode overloads GetKeyName to ignore the cluster name, as cluster name may not be stable during node registration.
//
// +k8s:deepcopy-gen=true
type RegisterNode struct {
	Node
}

// GetKeyName Overloaded key name w/o cluster name
func (n *RegisterNode) GetKeyName() string {
	return n.Name
}

// Node contains the nodes name, the list of addresses to this address
//
// +k8s:deepcopy-gen=true
type Node struct {
	// Name is the name of the node. This is typically the hostname of the node.
	Name string

	// Cluster is the name of the cluster the node is associated with
	Cluster string

	IPAddresses []Address

	// IPv4AllocCIDR if set, is the IPv4 address pool out of which the node
	// allocates IPs for local endpoints from
	IPv4AllocCIDR *cidr.CIDR

	// IPv4SecondaryAllocCIDRs contains additional IPv4 CIDRs from which this
	//node allocates IPs for its local endpoints from
	IPv4SecondaryAllocCIDRs []*cidr.CIDR

	// IPv6AllocCIDR if set, is the IPv6 address pool out of which the node
	// allocates IPs for local endpoints from
	IPv6AllocCIDR *cidr.CIDR

	// IPv6SecondaryAllocCIDRs contains additional IPv6 CIDRs from which this
	// node allocates IPs for its local endpoints from
	IPv6SecondaryAllocCIDRs []*cidr.CIDR

	// ClusterID is the unique identifier of the cluster
	ClusterID string

	// Node labels
	Labels      map[string]string
	Annotations map[string]string
}

// Fullname returns the node's full name including the cluster name if a
// cluster name value other than the default value has been specified
func (n *Node) Fullname() string {
	if n.Cluster != defaults.ClusterName {
		return path.Join(n.Cluster, n.Name)
	}

	return n.Name
}

// Address is a node address which contains an IP and the address type.
//
// +k8s:deepcopy-gen=true
type Address struct {
	Type addressing.AddressType
	IP   net.IP
}

// GetNodeIP returns one of the node's IP addresses available with the
// following priority:
// - NodeInternalIP
// - NodeExternalIP
// - other IP address type
func (n *Node) GetNodeIP(ipv6 bool) net.IP {
	var backupIP net.IP
	for _, addr := range n.IPAddresses {
		if (ipv6 && addr.IP.To4() != nil) ||
			(!ipv6 && addr.IP.To4() == nil) {
			continue
		}
		switch addr.Type {
		// Ignore CCEInternalIPs
		case addressing.NodeCCEInternalIP:
			continue
		// Always prefer a cluster internal IP
		case addressing.NodeInternalIP:
			return addr.IP
		case addressing.NodeExternalIP:
			// Fall back to external Node IP
			// if no internal IP could be found
			backupIP = addr.IP
		default:
			// As a last resort, if no internal or external
			// IP was found, use any node address available
			if backupIP == nil {
				backupIP = addr.IP
			}
		}
	}
	return backupIP
}

// GetExternalIP returns ExternalIP of k8s Node. If not present, then it
// returns nil;
func (n *Node) GetExternalIP(ipv6 bool) net.IP {
	for _, addr := range n.IPAddresses {
		if (ipv6 && addr.IP.To4() != nil) || (!ipv6 && addr.IP.To4() == nil) {
			continue
		}
		if addr.Type == addressing.NodeExternalIP {
			return addr.IP
		}
	}

	return nil
}

// GetK8sNodeIPs returns k8s Node IP (either InternalIP or ExternalIP or nil;
// the former is preferred).
func (n *Node) GetK8sNodeIP() net.IP {
	var externalIP net.IP

	for _, addr := range n.IPAddresses {
		if addr.Type == addressing.NodeInternalIP {
			return addr.IP
		} else if addr.Type == addressing.NodeExternalIP {
			externalIP = addr.IP
		}
	}

	return externalIP
}

// GetCCEInternalIP returns the CCEInternalIP e.g. the IP associated
// with cce_host on the node.
func (n *Node) GetCCEInternalIP(ipv6 bool) net.IP {
	for _, addr := range n.IPAddresses {
		if (ipv6 && addr.IP.To4() != nil) ||
			(!ipv6 && addr.IP.To4() == nil) {
			continue
		}
		if addr.Type == addressing.NodeCCEInternalIP {
			return addr.IP
		}
	}
	return nil
}

func (n *Node) GetIPByType(addrType addressing.AddressType, ipv6 bool) net.IP {
	for _, addr := range n.IPAddresses {
		if addr.Type != addrType {
			continue
		}
		if is4 := addr.IP.To4() != nil; (!ipv6 && is4) || (ipv6 && !is4) {
			return addr.IP
		}
	}
	return nil
}

func (n *Node) getPrimaryAddress() *models.NodeAddressing {
	v4 := n.GetNodeIP(false)
	v6 := n.GetNodeIP(true)

	var ipv4AllocStr, ipv6AllocStr string
	if n.IPv4AllocCIDR != nil {
		ipv4AllocStr = n.IPv4AllocCIDR.String()
	}
	if n.IPv6AllocCIDR != nil {
		ipv6AllocStr = n.IPv6AllocCIDR.String()
	}

	var v4Str, v6Str string
	if v4 != nil {
		v4Str = v4.String()
	}
	if v6 != nil {
		v6Str = v6.String()
	}

	return &models.NodeAddressing{
		IPV4: &models.NodeAddressingElement{
			Enabled:    option.Config.EnableIPv4,
			IP:         v4Str,
			AllocRange: ipv4AllocStr,
		},
		IPV6: &models.NodeAddressingElement{
			Enabled:    option.Config.EnableIPv6,
			IP:         v6Str,
			AllocRange: ipv6AllocStr,
		},
	}
}

func (n *Node) isPrimaryAddress(addr Address, ipv4 bool) bool {
	return addr.IP.String() == n.GetNodeIP(!ipv4).String()
}

func (n *Node) getSecondaryAddresses() []*models.NodeAddressingElement {
	result := []*models.NodeAddressingElement{}

	for _, addr := range n.IPAddresses {
		ipv4 := false
		if addr.IP.To4() != nil {
			ipv4 = true
		}
		if !n.isPrimaryAddress(addr, ipv4) {
			result = append(result, &models.NodeAddressingElement{
				IP: addr.IP.String(),
			})
		}
	}

	return result
}

// GetModel returns the API model representation of a node.
func (n *Node) GetModel() *models.NodeElement {
	return &models.NodeElement{
		Name:               n.Fullname(),
		PrimaryAddress:     n.getPrimaryAddress(),
		SecondaryAddresses: n.getSecondaryAddresses(),
	}
}

// Identity returns the identity of the node
func (n *Node) Identity() Identity {
	return Identity{
		Name:    n.Name,
		Cluster: n.Cluster,
	}
}

func getCluster() string {
	return option.Config.ClusterID
}

// IsLocal returns true if this is the node on which the agent itself is
// running on
func (n *Node) IsLocal() bool {
	return n != nil && n.Name == GetName() && n.Cluster == getCluster()
}

func (n *Node) GetIPv4AllocCIDRs() []*cidr.CIDR {
	result := make([]*cidr.CIDR, 0, len(n.IPv4SecondaryAllocCIDRs)+1)
	if n.IPv4AllocCIDR != nil {
		result = append(result, n.IPv4AllocCIDR)
	}
	if len(n.IPv4SecondaryAllocCIDRs) > 0 {
		result = append(result, n.IPv4SecondaryAllocCIDRs...)
	}
	return result
}

func (n *Node) GetIPv6AllocCIDRs() []*cidr.CIDR {
	result := make([]*cidr.CIDR, 0, len(n.IPv6SecondaryAllocCIDRs)+1)
	if n.IPv6AllocCIDR != nil {
		result = append(result, n.IPv6AllocCIDR)
	}
	if len(n.IPv4SecondaryAllocCIDRs) > 0 {
		result = append(result, n.IPv6SecondaryAllocCIDRs...)
	}
	return result
}

// GetKeyNodeName constructs the API name for the given cluster and node name.
func GetKeyNodeName(cluster, node string) string {
	// WARNING - STABLE API: Changing the structure of the key may break
	// backwards compatibility
	return path.Join(cluster, node)
}

// GetKeyName returns the kvstore key to be used for the node
func (n *Node) GetKeyName() string {
	return GetKeyNodeName(n.Cluster, n.Name)
}

// DeepKeyCopy creates a deep copy of the LocalKey
func (n *Node) DeepKeyCopy() *Node {
	return n.DeepCopy()
}

// Marshal returns the node object as JSON byte slice
func (n *Node) Marshal() ([]byte, error) {
	return json.Marshal(n)
}

// Unmarshal parses the JSON byte slice and updates the node receiver
func (n *Node) Unmarshal(data []byte) error {
	newNode := Node{}
	if err := json.Unmarshal(data, &newNode); err != nil {
		return err
	}

	*n = newNode

	return nil
}
