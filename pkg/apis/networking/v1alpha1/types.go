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

type WorkloadEndpointSpec struct {
	ContainerID       string      `json:"containerID"`
	IP                string      `json:"ip"`
	IPv6              string      `json:"ipv6"`
	Type              string      `json:"type"`
	Mac               string      `json:"mac"`
	Gw                string      `json:"gw"`
	ENIID             string      `json:"eniID"`
	Node              string      `json:"node"`
	InstanceID        string      `json:"instanceID"`
	SubnetID          string      `json:"subnetID"`
	EnableFixIP       string      `json:"enableFixIP"`
	FixIPDeletePolicy string      `json:"fixIPDeletePolicy"`
	UpdateAt          metav1.Time `json:"updateAt"`
	// subnet id of eni primary IP
	// This field is valid only when Eni applies for IP across subnets
	ENISubnetID string `json:"eniSubnetID,omitempty"`
	// PodSubnetTopologySpread object name referenced by pod
	SubnetTopologyReference string `json:"subnetTopologyReference,omitempty"`
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
	Type              string                        `json:"type"` //类型，目前只有一种：Roce
	Spec              []MultiIPWorkloadEndpointSpec `json:"spec,omitempty"`
}

type MultiIPWorkloadEndpointSpec struct {
	EniID       string      `json:"eniID"`
	ContainerID string      `json:"containerID"`
	IP          string      `json:"ip,omitempty"`
	Mac         string      `json:"mac,omitempty"`
	UpdateAt    metav1.Time `json:"updateAt"`
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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=psts

// PodSubnetTopologySpread describes how to distribute pods in the scenario of sub customized subnets
type PodSubnetTopologySpread struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodSubnetTopologySpreadSpec   `json:"spec,omitempty"`
	Status PodSubnetTopologySpreadStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodSubnetTopologySpreadList contains a list of PodSubnetTopologySpread
type PodSubnetTopologySpreadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodSubnetTopologySpread `json:"items"`
}

type PodSubnetTopologySpreadSpec struct {
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:MinProperties:=1

	// Subnets for the subnet used by the object, each subnet topology constraint
	// object must specify at least one available subnet.
	// The subnet must be the subnet ID of the same VPC as the current cluster.
	// The format is `sbn-*` for example, sbn-ccfud13pwcqf
	// If a dedicated subnet is used, the user should confirm that the subnet
	// is only used by the current CCE cluster
	Subnets map[string]SubnetAllocation `json:"subnets"`

	// A label query over pods that are managed by the daemon set.
	// Must match in order to be controlled.
	// It must match the pod template's labels.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// +kubebuilder:validation:Minimum:=0

	// Priority describes which object the target pod should use when multiple
	// objects affect a pod at the same time. The higher the priority value,
	// the earlier the object is configured. When multiple objects have the same
	// priority value, only the configuration of the first object is taken.
	Priority int32 `json:"priority,omitempty"`

	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=0

	// MaxSkew describes the degree to which pods may be unevenly distributed.
	// It's the maximum permitted difference between the number of matching pods in
	// any two topology domains of a given topology type.
	// For example, in a 3-zone cluster, MaxSkew is set to 1, and pods with the same
	// labelSelector spread as 1/1/0:
	// +-------+-------+-------+
	// | zone1 | zone2 | zone3 |
	// +-------+-------+-------+
	// |   P   |   P   |       |
	// +-------+-------+-------+
	// - if MaxSkew is 1, incoming pod can only be scheduled to zone3 to become 1/1/1;
	// scheduling it onto zone1(zone2) would make the ActualSkew(2-0) on zone1(zone2)
	// violate MaxSkew(1).
	// - if MaxSkew is 2, incoming pod can be scheduled onto any zone.
	// It's a required field. Default value is 1 and 0 is not allowed.
	MaxSkew int32 `json:"maxSkew,omitempty"`

	// +kubebuilder:default:=DoNotSchedule

	// WhenUnsatisfiable indicates how to deal with a pod if it doesn't satisfy
	// the spread constraint.
	// - DoNotSchedule (default) tells the scheduler not to schedule it
	// - ScheduleAnyway tells the scheduler to still schedule it
	// It's considered as "Unsatisfiable" if and only if placing incoming pod on any
	// topology violates "MaxSkew".
	// For example, in a 3-zone cluster, MaxSkew is set to 1, and pods with the same
	// labelSelector spread as 3/1/1:
	// +-------+-------+-------+
	// | zone1 | zone2 | zone3 |
	// +-------+-------+-------+
	// | P P P |   P   |   P   |
	// +-------+-------+-------+
	// If WhenUnsatisfiable is set to DoNotSchedule, incoming pod can only be scheduled
	// to zone2(zone3) to become 3/2/1(3/1/2) as ActualSkew(2-1) on zone2(zone3) satisfies
	// MaxSkew(1). In other words, the cluster can still be imbalanced, but scheduler
	// won't make it *more* imbalanced.
	// It's a required field.
	WhenUnsatisfiable UnsatisfiableConstraintAction `json:"whenUnsatisfiable,omitempty"`

	// +kubebuilder:default:=true

	// Whether the IP address allocation details under each subnet are displayed
	// in the status. When this attribute is enabled, the pod to which each IP
	// address under the current subnet is historically assigned will be recorded.
	// Note: recording IP address allocation status may produce large objects,
	// which may affect etcd performance
	EnableIPAllocationStatus bool `json:"nableIPAllocationStatus,omitempty"`

	EnablePodTopologySpread bool `json:"enablePodTopologySpread,omitempty"`
}

type UnsatisfiableConstraintAction string

const (
	// DoNotSchedule instructs the scheduler not to schedule the pod
	// when constraints are not satisfied.
	DoNotSchedule UnsatisfiableConstraintAction = "DoNotSchedule"
	// ScheduleAnyway instructs the scheduler to schedule the pod
	// even if constraints are not satisfied.
	ScheduleAnyway UnsatisfiableConstraintAction = "ScheduleAnyway"
)

// SubnetAllocation describe how the IP address under the subnet should be allocated
type SubnetAllocation struct {
	// +kubebuilder:default:=Elastic
	Type IPAllocType `json:"type,omitempty"`

	// +kubebuilder:default:=TTL

	// IP address recycling policy
	// TTL: represents the default dynamic IP address recycling policy,default.
	// Never: this policy can only be used in fixed IP scenarios
	ReleaseStrategy ReleaseStrategy `json:"releaseStrategy,omitempty"`

	IPv4      []string `json:"ipv4,omitempty"`
	IPv6      []string `json:"ipv6,omitempty"`
	IPv4Range []string `json:"ipv4Range,omitempty"`
	IPv6Range []string `json:"ipv6Range,omitempty"`
}

// +kubebuilder:validation:Enum=Elastic;Fixed;Manual

// IPAllocType is the type for ip alloc strategy
type IPAllocType string

// IPAllocType
const (
	IPAllocTypeElastic IPAllocType = "Elastic"
	IPAllocTypeFixed   IPAllocType = "Fixed"
	IPAllocTypeManual  IPAllocType = "Manual"
)

// +kubebuilder:validation:Enum=TTL;Never

// ReleaseStrategy is the type for ip release strategy
type ReleaseStrategy string

// ReleaseStrategy
const (
	ReleaseStrategyTTL   ReleaseStrategy = "TTL"
	ReleaseStrategyNever ReleaseStrategy = "Never"
)

type PodSubnetTopologySpreadStatus struct {
	Name                    string                     `json:"name,omitempty"`
	SchedulableSubnetsNum   int32                      `json:"availableSubnetsNum,omitempty"`
	UnSchedulableSubnetsNum int32                      `json:"unavailableSubnetsNum,omitempty"`
	AvailableSubnets        map[string]SubnetPodStatus `json:"availableSubnets,omitempty"`
	// total number of pods match label selector
	PodMatchedCount int32 `json:"podMatchedCount,omitempty"`
	// Total pod expected to be affected
	PodAffectedCount   int32                      `json:"podAffectedCount,omitempty"`
	UnavailableSubnets map[string]SubnetPodStatus `json:"unavailableSubnets,omitempty"`
}

type SubnetPodStatus struct {
	SubenetDetail `json:",inline"`
	// total number of pods under this subnet
	PodCount int32 `json:"podCount,omitempty"`
	// error message for subnets
	Message string `json:"message,omitempty"`

	// IP address allocation details under the subnet
	// KEY: ip address
	// VALUE: pod name
	// Only when the `PodSubnetTopologySpread.spec.enableIPAllocationStatus` spec value is true,
	// the IP address allocation information will be recorded
	IPAllocations map[string]string `json:"ipAllocations,omitempty"`
}

type SubenetDetail struct {
	AvailableIPNum   int    `json:"availableIPNum,omitempty"`
	Enable           bool   `json:"enable,omitempty"`
	HasNoMoreIP      bool   `json:"hasNoMoreIP,omitempty"`
	ID               string `json:"id,omitempty"`
	Name             string `json:"name,omitempty"`
	AvailabilityZone string `json:"availabilityZone,omitempty"`
	CIDR             string `json:"cidr,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pstt
//
// PodSubnetTopologySpreadTable describes organizational relationships among multiple psts
type PodSubnetTopologySpreadTable struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   []PodSubnetTopologySpreadSpec   `json:"spec,omitempty"`
	Status []PodSubnetTopologySpreadStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodSubnetTopologySpreadTableList contains a list of PodSubnetTopologySpreadTable
type PodSubnetTopologySpreadTableList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodSubnetTopologySpreadTable `json:"items"`
}
