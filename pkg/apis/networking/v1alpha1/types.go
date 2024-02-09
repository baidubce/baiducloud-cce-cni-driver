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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type WorkloadEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec WorkloadEndpointSpec `json:"spec,omitempty"`
}

const (
	WorkloadEndpointPhasePodRuning  = "podRunning"
	WorkloadEndpointPhasePodDeleted = "podDeleted"
)

type WorkloadEndpointSpec struct {
	ContainerID       string      `json:"containerID,omitempty"`
	IP                string      `json:"ip,omitempty"`
	IPv6              string      `json:"ipv6,omitempty"`
	Type              string      `json:"type,omitempty"`
	Mac               string      `json:"mac,omitempty"`
	Gw                string      `json:"gw,omitempty"`
	ENIID             string      `json:"eniID,omitempty"`
	Node              string      `json:"node,omitempty"`
	InstanceID        string      `json:"instanceID,omitempty"`
	SubnetID          string      `json:"subnetID,omitempty"`
	EnableFixIP       string      `json:"enableFixIP,omitempty"`
	FixIPDeletePolicy string      `json:"fixIPDeletePolicy,omitempty"`
	UpdateAt          metav1.Time `json:"updateAt,omitempty"`
	// subnet id of eni primary IP
	// This field is valid only when Eni applies for IP across subnets
	ENISubnetID string `json:"eniSubnetID,omitempty"`
	// PodSubnetTopologySpread object name referenced by pod
	SubnetTopologyReference string `json:"subnetTopologyReference,omitempty"`

	Phase string `json:"phase,omitempty"`
	// Release
	Release *EndpointRelease `json:"release,omitempty"`
}

// EndpointRelease status of ip reuse mode
type EndpointRelease struct {
	// delete wep after pod deleted for TTL
	TTL metav1.Duration `json:"TTL,omitempty"`

	PodDeletedTime *metav1.Time `json:"podDeletedTime,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type WorkloadEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []WorkloadEndpoint `json:"items"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type MultiIPWorkloadEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	NodeName          string                        `json:"nodeName"`
	InstanceID        string                        `json:"instanceID"`
	ContainerID       string                        `json:"containerID"`
	Type              string                        `json:"type"` //类型，取值：roce, eri
	Spec              []MultiIPWorkloadEndpointSpec `json:"spec,omitempty"`
}

type MultiIPWorkloadEndpointSpec struct {
	EniID       string      `json:"eniID"`
	ContainerID string      `json:"containerID"`
	IP          string      `json:"ip,omitempty"`
	Mac         string      `json:"mac,omitempty"`
	UpdateAt    metav1.Time `json:"updateAt"`
	Type        string      `json:"type"` //类型，取值：roce, eri
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type MultiIPWorkloadEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []MultiIPWorkloadEndpoint `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPPoolSpec   `json:"spec,omitempty"`
	Status IPPoolStatus `json:"status,omitempty"`
}

type IPPoolSpec struct {
	// NodeSelector allows IPPool to allocate for a specific node by label selector
	NodeSelector string `json:"nodeSelector"`

	// CreateSource indicates pool creation source
	CreationSource string `json:"creationSource"`

	// Priority is the priority value of IPPool
	Priority int32 `json:"priority,omitempty"`

	// Range spec for host-local allocator
	IPv4Ranges []Range `json:"ipv4Ranges,omitempty"`
	IPv6Ranges []Range `json:"ipv6Ranges,omitempty"`

	// ENI spec for ENI allocator
	ENI ENISpec `json:"eni,omitempty"`

	// PodSubnets spec for primary eni secondary ip modes
	PodSubnets []string `json:"podSubnets,omitempty"`
}

type IPPoolStatus struct {
	ENI ENIStatus `json:"eni,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IPPool `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Subnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubnetSpec   `json:"spec,omitempty"`
	Status SubnetStatus `json:"status,omitempty"`
}

type SubnetSpec struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name,omitempty"`
	AvailabilityZone string `json:"availabilityZone,omitempty"`
	CIDR             string `json:"cidr,omitempty"`

	// Exclusive subnet flag
	// Exclusive subnet only allows manual IP assignment
	// If a subnet is marked as a Exclusive subnet, it can no longer be
	// used as a subnet for automatic IP allocation
	//
	// WARN: Marking as an exclusive subnet only allows you to manually
	// allocate IP. If the default subnet for IPAM is marked as an exclusive
	// subnet, it may cause all pods to fail to allocate IP
	Exclusive bool `json:"exclusive,omitempty"`
}

type SubnetStatus struct {
	AvailableIPNum int    `json:"availableIPNum,omitempty"`
	Enable         bool   `json:"enable,omitempty"`
	HasNoMoreIP    bool   `json:"hasNoMoreIP,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Subnet `json:"items"`
}

type ENISpec struct {
	VPCID                    string   `json:"vpcID"`
	AvailabilityZone         string   `json:"availabilityZone"`
	Subnets                  []string `json:"subnets"`
	SecurityGroups           []string `json:"securityGroups"`
	EnterpriseSecurityGroups []string `json:"enterpriseSecurityGroups"`
}

type ENIStatus struct {
	ENIs map[string]ENI `json:"enis,omitempty"`
}

type ENI struct {
	ID               string      `json:"id,omitempty"`
	MAC              string      `json:"mac,omitempty"`
	AvailabilityZone string      `json:"availabilityZone,omitempty"`
	Description      string      `json:"description,omitempty"`
	InterfaceIndex   int         `json:"interfaceIndex,omitempty"`
	Subnet           string      `json:"subnet,omitempty"`
	VPC              string      `json:"vpc,omitempty"`
	PrivateIPSet     []PrivateIP `json:"addresses,omitempty"`
	SecurityGroups   []string    `json:"securityGroups,omitempty"`
}

type PrivateIP struct {
	IsPrimary        bool   `json:"isPrimary"`
	PublicIPAddress  string `json:"publicIPAddress,omitempty"`
	PrivateIPAddress string `json:"privateIPAddress"`
}

type Range struct {
	// Version is ip version: 4 or 6
	Version    int      `json:"version"`
	CIDR       string   `json:"cidr"`
	RangeStart string   `json:"rangeStart"`
	RangeEnd   string   `json:"rangeEnd"`
	Excludes   []string `json:"excludes"`
	Gateway    string   `json:"gateway"`
	Routes     []Route  `json:"routes"`
	VlanID     int      `json:"vlanID"`
}

type Route struct {
	Dst string `json:"dst"`
	Gw  string `json:"gw"`
}

type EniStatus string

const (
	EniStatusPending   = "pending"
	EniStatusCreated   = "created"
	EniStatusAttaching = "attaching"
	EniStatusInuse     = "inuse"
	EniStatusDetaching = "detaching"
	EniStatusDetached  = "detached"
	EniStatusDeleted   = "deleted"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CrossVPCEni struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CrossVPCEniSpec   `json:"spec,omitempty"`
	Status CrossVPCEniStatus `json:"status,omitempty"`
}

type CrossVPCEniSpec struct {
	// UserID is the id of the user which this eni belongs to
	UserID string `json:"userID,omitempty"`

	// SubnetID is id of the subnet where this eni is created
	SubnetID string `json:"subnetID,omitempty"`

	// SecurityGroupIDs is the list of security groups that are bound to eni
	SecurityGroupIDs []string `json:"securityGroupIDs,omitempty"`

	// VPCCIDR is cidr of the vpc where this eni is created
	VPCCIDR string `json:"vpcCidr,omitempty"`

	// PrivateIPAddress is the private ip address to create an eni
	PrivateIPAddress string `json:"privateIPAddress,omitempty"`

	// BoundInstanceID is instance id of the node where this eni will be attached
	BoundInstanceID string `json:"boundInstanceID,omitempty"`

	// DefaultRouteInterfaceDelegation specifies the default route interface type
	//
	// +kubebuilder:validation:Enum=eni
	// +kubebuilder:validation:Optional
	DefaultRouteInterfaceDelegation string `json:"defaultRouteInterfaceDelegation,omitempty"`

	// DefaultRouteExcludedCidrs is the cidrs excluded from default route
	//
	// +kubebuilder:validation:Optional
	DefaultRouteExcludedCidrs []string `json:"defaultRouteExcludedCidrs,omitempty"`
}

// CrossVPCEniStatus defines the observed state of CrossVPCEni
type CrossVPCEniStatus struct {
	// EniID is id of this eni
	EniID string `json:"eniID,omitempty"`

	// EniStatus is the status of this eni
	EniStatus EniStatus `json:"eniStatus,omitempty"`

	// PrimaryIPAddress is the primary ip address of this eni
	PrimaryIPAddress string `json:"primaryIPAddress,omitempty"`

	// MacAddress is the hardware address of this eni
	MacAddress string `json:"macAddress,omitempty"`

	// VPCID is the vpc id of this eni
	VPCID string `json:"vpcID,omitempty"`

	//  InvolvedContainerID is the infra container id of this eni
	InvolvedContainerID string `json:"involvedContainerID,omitempty"`

	// Conditions is the conditions of this eni
	//
	// +kubebuilder:validation:MaxItems=10
	Conditions []CrossVPCEniCondition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CrossVPCEniList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CrossVPCEni `json:"items"`
}

type CrossVPCEniCondition struct {
	EniStatus          EniStatus   `json:"eniStatus,omitempty"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}
