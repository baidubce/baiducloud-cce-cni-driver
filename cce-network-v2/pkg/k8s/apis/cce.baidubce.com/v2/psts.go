package v2

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// DefaultReuseIPTTL default timeout for reusing IP addresses.
	// If this time is not set, the default value is 7 days
	DefaultReuseIPTTL = &metav1.Duration{Duration: time.Hour * 24 * 7}
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=psts
// +kubebuilder:storageversion

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
	Subnets map[string]CustomAllocationList `json:"subnets"`

	// Strategy IP allocate strategy, which is a global ip application strategy.
	// If the subnet also sets these fields, the subnet will override the global configuration
	// If no global policy is defined, the policy of the first subnet is the global policy by default
	Strategy *IPAllocationStrategy `json:"strategy,omitempty"`

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
}

type CustomAllocationList []CustomAllocation

type UnsatisfiableConstraintAction string

const (
	// DoNotSchedule instructs the scheduler not to schedule the pod
	// when constraints are not satisfied.
	DoNotSchedule UnsatisfiableConstraintAction = "DoNotSchedule"
	// ScheduleAnyway instructs the scheduler to schedule the pod
	// even if constraints are not satisfied.
	ScheduleAnyway UnsatisfiableConstraintAction = "ScheduleAnyway"
)

// IPAllocationStrategy The policy determines whether to use fixed IP, Elastic IP or custom mode
type IPAllocationStrategy struct {
	// If the type is empty, the subnet type is used
	Type IPAllocType `json:"type,omitempty"`

	// +kubebuilder:default:=TTL

	// IP address recycling policy
	// TTL: represents the default dynamic IP address recycling policy,default.
	// Never: this policy can only be used in fixed IP scenarios
	ReleaseStrategy ReleaseStrategy `json:"releaseStrategy,omitempty"`

	// ReuseIPAddress Whether to enable address reuse with the same pod name
	EnableReuseIPAddress bool `json:"enableReuseIPAddress,omitempty"`

	// TTL How long after the pod is deleted, the IP will be deleted, regardless of whether the IP reuse mode is enabled
	TTL *metav1.Duration `json:"ttl,omitempty"`
}

// CustomAllocation User defined IP address management policy
type CustomAllocation struct {
	// +kubebuilder:default:=4
	// Family of IP Address. 4 or 6
	Family IPFamily `json:"family,omitempty"`
	// Range User defined IP address range. Note that this range must be smaller than the subnet range
	// Note that the definitions of multiple ranges cannot be duplicate
	Range []CustomIPRange `json:"range,omitempty"`
}

// CustomIPRange User defined IP address range. Note that this range must be smaller than the subnet range
type CustomIPRange struct {
	Start string `json:"start"`
	// End end address must be greater than or equal to the start address
	End string `json:"end"`
}

// +kubebuilder:validation:Enum=Elastic;Fixed;Manual;Custom;IPAllocTypeNil;PrimaryENI

// IPAllocType is the type for ip alloc strategy
type IPAllocType string

// IPAllocType
const (
	IPAllocTypeNil        IPAllocType = ""
	IPAllocTypeElastic    IPAllocType = "Elastic"
	IPAllocTypeFixed      IPAllocType = "Fixed"
	IPAllocTypeENIPrimary IPAllocType = "PrimaryENI"
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
	IPv6CIDR         string `json:"ipv6Cidr,omitempty"`
}
