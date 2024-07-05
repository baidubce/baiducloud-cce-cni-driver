package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ENISubnetIDsConfigKey                   = "eni-subnet-ids"
	EnableRDMAConfigKey                     = "enable-rdma"
	ManualMTUConfigKey                      = "mtu"
	ElasticNetworkInterfaceUseModeConfigKey = "eni-use-mode"
	EniSecurityGroupIdsConfigKey            = "eni-security-group-ids"
	EniEnterpriseSecurityGroupIdsConfigKey  = "eni-enterprise-security-group-ids"
	BurstableMehrfachENIConfigKey           = "burstable-mehrfach-eni"
	ReleaseExcessIPsConfigKey               = "release-excess-ips"
	ExtCNIPluginsConfigKey                  = "ext-cni-plugins"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// NetResourceConfigSet contains a list of NetResourceConfigSet
type NetResourceConfigSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetResourceConfigSet `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={cce},singular="netresourceconfigset",path="netresourceconfigsets",scope="Cluster",shortName={nrcs}
// +kubebuilder:storageversion

// NetResourceConfigSet describes how to distribute network resources configuration to nodes.
type NetResourceConfigSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetResourceConfigSetSpec   `json:"spec,omitempty"`
	Status NetResourceConfigSetStatus `json:"status,omitempty"`
}

type NetResourceConfigSetSpec struct {
	// Selector is a label query over node that should match the node count.
	// +kubebuilder:validation:Required
	Selector *metav1.LabelSelector `json:"selector"`

	// +kubebuilder:validation:Minimum:=0

	// Priority describes which object the target pod should use when multiple
	// objects affect a pod at the same time. The higher the priority value,
	// the earlier the object is configured. When multiple objects have the same
	// priority value, only the configuration of the first object is taken.
	Priority int32 `json:"priority,omitempty"`

	// Agent is the configuration of the agent.
	//
	// +kubebuilder:prunning:PreserveUnknownFields
	AgentConfig AgentConfig `json:"agent,omitempty"`
}

type NetResourceConfigSetStatus struct {
	// AgentVersion is the config version of agent.
	// key is name of node
	// value is resiversion of nrcs
	AgentVersion map[string]string `json:"agentVersion,omitempty"`

	// NodeCount is the number of nodes was selected by this NRCS.
	NodeCount int32 `json:"nodeCount,omitempty"`
}

type AgentConfig struct {
	EniSubnetIDs                  []string `json:"eni-subnet-ids,omitempty"`
	EnableRDMA                    *bool    `json:"enable-rdma,omitempty"`
	ManualMTU                     *int     `json:"mtu,omitempty"`
	EniUseMode                    *string  `json:"eni-use-mode,omitempty"`
	EniSecurityGroupIds           []string `json:"eni-security-group-ids,omitempty"`
	EniEnterpriseSecurityGroupIds []string `json:"eni-enterprise-security-group-id,omitempty"`
	BurstableMehrfachENI          *int     `json:"burstable-mehrfach-eni,omitempty"`
	ReleaseExcessIPs              *bool    `json:"release-excess-ips,omitempty"`
	ExtCniPlugins                 []string `json:"ext-cni-plugins,omitempty"`
	UsePrimaryAddress             *bool    `json:"use-eni-primary-address,omitempty"`
	IPPoolPreAllocateENI          *int     `json:"ippool-pre-allocate-eni,omitempty"`
	IPPoolMinAllocate             *int     `json:"ippool-min-allocate,omitempty"`
	IPPoolPreAllocate             *int     `json:"ippool-pre-allocate,omitempty"`
	IPPoolMaxAboveWatermark       *int     `json:"ippool-max-above-watermark,omitempty"`
	RouteTableOffset              *int     `json:"route-table-offset,omitempty"`
}
