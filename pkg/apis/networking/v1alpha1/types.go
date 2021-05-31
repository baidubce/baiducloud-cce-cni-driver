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
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type WorkloadEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []WorkloadEndpoint `json:"items"`
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
	ID               string `json:"id"`
	Name             string `json:"name"`
	AvailabilityZone string `json:"availabilityZone"`
	CIDR             string `json:"cidr"`
}

type SubnetStatus struct {
	AvailableIPNum int  `json:"availableIPNum"`
	Enable         bool `json:"enable"`
	HasNoMoreIP    bool `json:"hasNoMoreIP"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Subnet `json:"items"`
}

type ENISpec struct {
	VPCID            string   `json:"vpcID"`
	AvailabilityZone string   `json:"availabilityZone"`
	Subnets          []string `json:"subnets"`
	SecurityGroups   []string `json:"securityGroups"`
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
