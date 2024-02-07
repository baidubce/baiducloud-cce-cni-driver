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

package v2

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/addressing"
	pcbapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase/api"
)

type IPFamily string

const (
	IPv4Family IPFamily = "4"
	IPv6Family IPFamily = "6"
)

// AddressPair is is a par of IPv4 and/or IPv6 address.
type AddressPair struct {
	// Family is the kind of address. E.g. "4" or "6".
	Family    IPFamily `json:"family"`
	IP        string   `json:"ip,omitempty"`
	Interface string   `json:"interface,omitempty"`
	VPCID     string   `json:"vpcID,omitempty"`
	Subnet    string   `json:"subnet,omitempty"`
	// CIDRs is a list of all CIDRs to which the IP has direct access to.
	// This is primarily useful if the IP has been allocated out of a VPC
	// subnet range and the VPC provides routing to a set of CIDRs in which
	// the IP is routable.
	CIDRs   []string `json:"cidr,omitempty"`
	Gateway string   `json:"gateway,omitempty"`
}

// AddressPairList is a list of address pairs.
type AddressPairList []*AddressPair

func (list AddressPairList) ToIPsString() string {
	var ips []string
	for _, addr := range list {
		ips = append(ips, addr.IP)
	}
	return strings.Join(ips, ",")
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories={cce},singular="netresourceset",path="netresourcesets",scope="Cluster",shortName={nrs,nr}
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
//
// +kubebuilder:printcolumn:JSONPath=".spec.instance-id",description="",name="ID",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.eni.useMode",description="",name="MODE",type=string,priority=0
// +kubebuilder:printcolumn:JSONPath=".spec.eni.instance-type",description="",name="TYPE",type=string,priority=0
// NetResourceSet represents a node managed by CCE. It contains a specification
// to control various node specific configuration aspects and a status section
// to represent the status of the node.
type NetResourceSet struct {
	// +deepequal-gen=false
	metav1.TypeMeta `json:",inline"`
	// +deepequal-gen=false
	metav1.ObjectMeta `json:"metadata"`

	// Spec defines the desired specification/configuration of the node.
	Spec NetResourceSpec `json:"spec"`

	// Status defines the realized specification/configuration and status
	// of the node.
	//
	// +kubebuilder:validation:Optional
	Status NetResourceStatus `json:"status,omitempty"`
}

// NodeAddress is a node address.
type NodeAddress struct {
	// Type is the type of the node address
	Type addressing.AddressType `json:"type,omitempty"`

	// IP is an IP of a node
	IP string `json:"ip,omitempty"`
}

// NetResourceSpec is the configuration specific to a node.
type NetResourceSpec struct {
	// InstanceID is the identifier of the node. This is different from the
	// node name which is typically the FQDN of the node. The InstanceID
	// typically refers to the identifier used by the cloud provider or
	// some other means of identification.
	InstanceID string `json:"instance-id,omitempty"`

	// Addresses is the list of all node addresses.
	//
	// +kubebuilder:validation:Optional
	Addresses []NodeAddress `json:"addresses,omitempty"`

	// IPAM is the address management specification. This section can be
	// populated by a user or it can be automatically populated by an IPAM
	// operator.
	//
	// +kubebuilder:validation:Optional
	IPAM ipamTypes.IPAMSpec `json:"ipam,omitempty"`

	// ENI declaration expectation is used to describe what kind of ENI you want on a node
	// This field is applicable to eni mode
	//
	// +kubebuilder:validation:Optional
	ENI *api.ENISpec `json:"eni,omitempty"`
}

// HealthAddressingSpec is the addressing information required to do
// connectivity health checking.
type HealthAddressingSpec struct {
	// IPv4 is the IPv4 address of the IPv4 health endpoint.
	//
	// +kubebuilder:validation:Optional
	IPv4 string `json:"ipv4,omitempty"`

	// IPv6 is the IPv6 address of the IPv4 health endpoint.
	//
	// +kubebuilder:validation:Optional
	IPv6 string `json:"ipv6,omitempty"`
}

// EncryptionSpec defines the encryption relevant configuration of a node.
type EncryptionSpec struct {
	// Key is the index to the key to use for encryption or 0 if encryption is
	// disabled.
	//
	// +kubebuilder:validation:Optional
	Key int `json:"key,omitempty"`
}

// NetResourceStatus is the status of a node.
type NetResourceStatus struct {

	// IPAM is the IPAM status of the node.
	//
	// +kubebuilder:validation:Optional
	IPAM ipamTypes.IPAMStatus `json:"ipam,omitempty"`

	// ENIs The details of the ENI that has been bound on the node, including all the
	// IP lists of the ENI
	//
	// +kubebuilder:validation:Optional
	ENIs map[string]SimpleENIStatus `json:"enis,omitempty"`

	// PrivateCloudSubnet is the baidu private cloud base specific status of the node.
	//
	// +kubebuilder:validation:Optional
	PrivateCloudSubnet *pcbapi.Subnet `json:"privateCloudSubnet,omitempty"`
}

// SimpleENIStatus is the simple status of a ENI.
type SimpleENIStatus struct {
	ID        string `json:"id"`
	VPCStatus string `json:"vpcStatus"`
	CCEStatus string `json:"cceStatus"`

	// AvailableIPNum how many more IPs can be applied for on eni
	// This field only considers the ip quota of eni
	AvailableIPNum int `json:"availableIPNum,omitempty"`

	// AllocatedIPNum Number of IPs assigned to eni
	AllocatedIPNum int `json:"allocatedIPNum,omitempty"`

	// AllocatedCrossSubnetIPNum number of IPs assigned to eni across subnet
	AllocatedCrossSubnetIPNum int `json:"allocatedCrossSubnetIPNum,omitempty"`

	// SubnetID the subnet id of eni primary ip
	SubnetID string `json:"subnetId,omitempty"`

	// IsMoreAvailableIPInSubnet Are there more available IPs in the subnet
	IsMoreAvailableIPInSubnet bool `json:"isMoreAvailableIPInSubnet,omitempty"`

	LastAllocatedIPError *StatusChange `json:"lastAllocatedIPError,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +deepequal-gen=false

// NetResourceSetList is a list of NetResourceSet objects.
type NetResourceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is a list of NetResourceSet
	Items []NetResourceSet `json:"items"`
}
