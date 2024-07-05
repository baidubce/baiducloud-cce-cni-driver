package api

// ENISpec ENI declaration expectation is used to describe what kind of ENI
// you want on a node
type ENISpec struct {
	// UseMode usage mode of eni currently includes `Primary` and `Secondary`
	// +kubebuilder:default:=Secondary
	// +kubebuilder:validation:Enum=Secondary;Primary
	UseMode string `json:"useMode,omitempty"`

	// MaxAllocateENI maximum number of ENIs that can be applied for a single machine
	// +kubebuilder:validation:Minimum=0
	MaxAllocateENI int `json:"maxAllocateENI,omitempty"`

	// PreAllocateENI Number of ENIs pre-allocate
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=1
	PreAllocateENI int `json:"preAllocateENI,omitempty"`

	// BurstableMehrfachENI is the number of idle IPs with the minimum reserved ENI
	// IP capacity multiple. If 0, it means that the Burstable ENI mode is not used.
	// If it is 1, it means always ensuring that an ENI's IP address is in a ready
	// idle state (ready+IP capacity is full)
	// default is 1
	BurstableMehrfachENI int `json:"burstableMehrfachENI,omitempty"`

	// MaxIPsPerENI the maximum number of secondary IPs that can be
	// applied for per eni
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=1
	MaxIPsPerENI int `json:"maxIPsPerENI,omitempty"`

	// InstanceType is the BCE Compute instance type, e.g. BCC BBC
	//
	// +kubebuilder:validation:Optional
	InstanceType string `json:"instance-type,omitempty"`

	// FirstInterfaceIndex is the index of the first ENI to use for IP
	// allocation, e.g. if the node has eth0, eth1, eth2 and
	// FirstInterfaceIndex is set to 1, then only eth1 and eth2 will be
	// used for IP allocation, eth0 will be ignored for PodIP allocation.
	//
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Optional
	FirstInterfaceIndex *int `json:"first-interface-index,omitempty"`

	// VpcID is the VPC ID to use when allocating ENIs.
	//
	// +kubebuilder:validation:Optional
	VpcID string `json:"vpc-id,omitempty"`

	// SecurityGroups is the list of security groups to attach to any ENI
	// that is created and attached to the instance.
	//
	// +kubebuilder:validation:Optional
	SecurityGroups []string `json:"security-groups,omitempty"`

	// EnterpriseSecurityGroupList are enterprise security groups that bound to ENIs
	// e.g. esg-twh19p9zcuqr, esg-5yhyct307p98
	EnterpriseSecurityGroupList []string `json:"enterpriseSecurityGroupList,omitempty"`

	// SubnetIDs is the list of subnet ids to use when evaluating what BCE
	// subnets to use for ENI and IP allocation.
	//
	// +kubebuilder:validation:Optional
	SubnetIDs []string `json:"subnet-ids,omitempty"`

	// AvailabilityZone is the availability zone to use when allocating
	// ENIs.
	//
	// +kubebuilder:validation:Optional
	AvailabilityZone string `json:"availability-zone,omitempty"`

	// DeleteOnTermination defines that the ENI should be deleted when the
	// associated instance is terminated. If the parameter is not set the
	// default behavior is to delete the ENI on instance termination.
	//
	// +kubebuilder:validation:Optional
	DeleteOnTermination *bool `json:"delete-on-termination,omitempty"`

	// UsePrimaryAddress determines whether an ENI's primary address
	// should be available for allocations on the node
	//
	// +kubebuilder:validation:Optional
	UsePrimaryAddress *bool `json:"use-primary-address,omitempty"`

	// RouteTableOffset route policy offset, default 127
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=127
	RouteTableOffset int `json:"routeTableOffset"`

	// InstallSourceBasedRouting install source based routing, default false
	InstallSourceBasedRouting bool `json:"installSourceBasedRouting,omitempty"`
}
