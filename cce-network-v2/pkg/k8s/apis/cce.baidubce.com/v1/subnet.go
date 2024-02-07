package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +kubebuilder:resource:categories={cce},singular="subnet",path="subnets",scope="Cluster",shortName={sbn}
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
//
// +kubebuilder:printcolumn:JSONPath=".spec.availabilityZone",description="",name="AZ",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.cidr",description="ipv4 cidr",name="CIDR4",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.ipv6CIDR",description="ipv6 cidr",name="CIDR6",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.exclusive",description="Exclusive",name="Exclu",type=boolean
// +kubebuilder:printcolumn:JSONPath=".status.availableIPNum",description="available ip number",name="AIP",type=integer

type Subnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubnetSpec   `json:"spec,omitempty"`
	Status SubnetStatus `json:"status,omitempty"`
}

type SubnetSpec struct {
	ID               string            `json:"id,omitempty"`
	Name             string            `json:"name,omitempty"`
	AvailabilityZone string            `json:"availabilityZone,omitempty"`
	CIDR             string            `json:"cidr,omitempty"`
	IPv6CIDR         string            `json:"ipv6CIDR,omitempty"`
	VPCID            string            `json:"vpcId,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
	Description      string            `json:"description,omitempty"`
	SubnetType       string            `json:"subnetType,omitempty"`
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
