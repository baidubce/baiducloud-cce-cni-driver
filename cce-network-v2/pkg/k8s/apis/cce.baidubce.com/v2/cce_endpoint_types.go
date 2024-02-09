package v2

import (
	"sort"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=false
// +deepequal-gen=false
// +kubebuilder:resource:categories={cce},singular="cceendpoint",path="cceendpoints",scope="Namespaced",shortName={cep,ccep}
// +kubebuilder:printcolumn:JSONPath=".spec.network.ipAllocation.type",description="ip type",name="Type",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.network.ipAllocation.releaseStrategy",description="ip release type",name="Release",type=string
// +kubebuilder:printcolumn:JSONPath=".status.state",description="Endpoint current state",name="State",type=string
// +kubebuilder:printcolumn:JSONPath=".status.networking.ips",description="Endpoint ip address",name="IPS",type=string
// +kubebuilder:printcolumn:JSONPath=".status.networking.node",description="Endpoint runing on the node",name="Node",type=string
// +kubebuilder:storageversion

// CCEEndpoint is the status of pod
type CCEEndpoint struct {
	// +deepequal-gen=false
	metav1.TypeMeta `json:",inline"`
	// +deepequal-gen=false
	metav1.ObjectMeta `json:"metadata"`

	Spec EndpointSpec `json:"spec"`
	// +kubebuilder:validation:Optional
	Status EndpointStatus `json:"status"`
}

type EndpointSpec struct {
	// ExternalIdentifiers is a set of identifiers to identify the endpoint
	// apart from the pod name. This includes container runtime IDs.
	ExternalIdentifiers *models.EndpointIdentifiers `json:"external-identifiers,omitempty"`
	Network             EndpointNetworkSpec         `json:"network,omitempty"`

	// ExtFeatureGates is a set of feature gates to enable or disable specific features like publicIP
	// every feature gate will have its own .status.extFeatureStatus
	ExtFeatureGates []string `json:"extFeatureGates,omitempty"`
}

// EndpointNetworkSpec Network config for CCE Endpoint
type EndpointNetworkSpec struct {
	IPAllocation   *IPAllocation      `json:"ipAllocation,omitempty"`
	Bindwidth      *BindwidthOption   `json:"bindwidth,omitempty"`
	EgressPriority *EgressPriorityOpt `json:"egressPriority,omitempty"`
}

// EndpointStatus is the status of a CCE endpoint.
type EndpointStatus struct {
	// Controllers is the list of failing controllers for this endpoint.
	Controllers ControllerList `json:"controllers,omitempty"`

	// ExternalIdentifiers is a set of identifiers to identify the endpoint
	// apart from the pod name. This includes container runtime IDs.
	ExternalIdentifiers *models.EndpointIdentifiers `json:"external-identifiers,omitempty"`

	// Log is the list of the last few warning and error log entries
	Log []*models.EndpointStatusChange `json:"log,omitempty"`

	// Networking is the networking properties of the endpoint.
	//
	// +kubebuilder:validation:Optional
	Networking *EndpointNetworking `json:"networking,omitempty"`

	// A list of node selector requirements by node's labels.
	// +optional
	NodeSelectorRequirement []corev1.NodeSelectorRequirement `json:"matchExpressions,omitempty" protobuf:"bytes,1,rep,name=matchExpressions"`

	// State is the state of the endpoint.
	State string `json:"state,omitempty"`

	// ExtFeatureStatus is a set of feature status to indicate the status of specific features like publicIP
	ExtFeatureStatus map[string]*ExtFeatureStatus `json:"extFeatureStatus,omitempty"`
}

// EndpointNetworking is the addressing information of an endpoint.
type EndpointNetworking struct {
	// IP4/6 addresses assigned to this Endpoint
	Addressing AddressPairList `json:"addressing"`

	// NodeIP is the IP of the node the endpoint is running on. The IP must
	// be reachable between nodes.
	NodeIP string `json:"node,omitempty"`

	// IPs is the list of IP addresses assigned to this Endpoint
	IPs string `json:"ips,omitempty"`
}

type IPAllocation struct {
	DirectIPAllocation `json:",inline"`
	// PSTSName is the name of the PSTS
	// This field is only valid in the PSTS mode
	PSTSName string `json:"pstsName,omitempty"`

	// NodeIP is the IP of the node the endpoint is running on. The IP must
	// be reachable between nodes.
	NodeIP string `json:"node,omitempty"`

	UseIPV4 bool `json:"useIPV4,omitempty"`
	UseIPV6 bool `json:"useIPV6,omitempty"`
}

// DirectIPAllocation is the addressing information of an endpoint.
type DirectIPAllocation struct {
	// +kubebuilder:default:=Elastic
	// this filed is only valid then the pstsName is empty
	Type IPAllocType `json:"type,omitempty"`

	// +kubebuilder:default:=TTL
	// IP address recycling policy
	// TTL: represents the default dynamic IP address recycling policy,default.
	// Never: this policy can only be used in fixed IP scenarios
	// this filed is only valid when the pstsName is empty
	ReleaseStrategy ReleaseStrategy `json:"releaseStrategy,omitempty"`

	// TTLSecondsAfterFinished is the TTL duration after this pod has been deleted when using fixed IP mode.
	// This field is only valid in the Fixed mode and the ReleaseStrategy is TTL
	// default is 7d
	// this filed is only valid when the pstsName is empty
	TTLSecondsAfterDeleted *int64 `json:"ttlSecondsAfterDeleted,omitempty"`
}

// ReleaseStrategy
const (
	// DefaultPodIPTTLSeconds If the fixed IP is stuck for a long time when the pod fails, IP recycling will be triggered
	DefaultPodIPTTLSeconds int64 = 7 * 24 * 3600
)

// ControllerList is a list of ControllerStatus.
type ControllerList []ControllerStatus

// Sort sorts the ControllerList by controller name
func (c ControllerList) Sort() {
	sort.Slice(c, func(i, j int) bool { return c[i].Name < c[j].Name })
}

// ControllerStatus is the status of a failing controller.
type ControllerStatus struct {
	// Name is the name of the controller
	Name string `json:"name,omitempty"`

	// Configuration is the controller configuration
	Configuration *models.ControllerStatusConfiguration `json:"configuration,omitempty"`

	// Status is the status of the controller
	Status ControllerStatusStatus `json:"status,omitempty"`

	// UUID is the UUID of the controller
	UUID string `json:"uuid,omitempty"`
}

// ControllerStatusStatus is the detailed status section of a controller.
type ControllerStatusStatus struct {
	ConsecutiveFailureCount int64  `json:"consecutive-failure-count,omitempty"`
	FailureCount            int64  `json:"failure-count,omitempty"`
	LastFailureMsg          string `json:"last-failure-msg,omitempty"`
	LastFailureTimestamp    string `json:"last-failure-timestamp,omitempty"`
	LastSuccessTimestamp    string `json:"last-success-timestamp,omitempty"`
	SuccessCount            int64  `json:"success-count,omitempty"`
}

// ExtFeatureStatus is the status of external feature
type ExtFeatureStatus struct {
	// Ready the external feature is ready to use
	// External features are only considered ready when both `container-id`` and `ready` are in place
	// ready is only valid when the container-id is equals to `.spec.external-identifiers.container-id`
	Ready bool `json:"ready"`

	// ID assigned by container runtime
	ContainerID string `json:"container-id"`

	Msg string `json:"msg,omitempty"`

	// UpdateTime is the time when the status was last updated
	UpdateTime *metav1.Time `json:"updateTime,omitempty"`

	// Data is a set of key-value pairs that can be used to store additional information
	Data map[string]string `json:"data,omitempty"`
}

const (
	BindwidthModeEDT  = "edt"
	BindwidthModeTC   = "tc"
	BindwidthModeNone = ""
)

type BindwidthMode string

// BindwidthOption is the option of bindwidth
type BindwidthOption struct {
	Mode    BindwidthMode `json:"mode,omitempty"`
	Ingress int64         `json:"ingress,omitempty"`
	Egress  int64         `json:"egress,omitempty"`
}

func (opt *BindwidthOption) IsValid() bool {
	return opt.Ingress > 0 || opt.Egress > 0
}

type EgressPriority uint32
type EgressDSCP uint32

var (
	EgressPriorityGuaranteed EgressPriority = 1
	EgressPriorityBestEffort EgressPriority = 2
	EgressPriorityBurstable  EgressPriority = 3

	EgressDSCPGuaranteed EgressDSCP = 17
	EgressDSCPBurstable  EgressDSCP = 1
	EgressDSCPBestEffort EgressDSCP = 9
)

type EgressPriorityOpt struct {
	Bands    EgressPriority `json:"bands,omitempty"`
	DSCP     EgressDSCP     `json:"dscp,omitempty"`
	Priority string         `json:"priority,omitempty"`
}

func NewEngressPriorityOpt(priority string) *EgressPriorityOpt {
	var opt EgressPriorityOpt
	opt.Priority = priority
	switch priority {
	case "Guaranteed":
		opt.Bands = EgressPriorityGuaranteed
		opt.DSCP = EgressDSCPGuaranteed
	case "Burstable":
		opt.Bands = EgressPriorityBurstable
		opt.DSCP = EgressDSCPBurstable
	case "BestEffort":
		opt.Bands = EgressPriorityBestEffort
		opt.DSCP = EgressDSCPBestEffort
	default:
		return nil
	}
	return &opt
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=false
// +deepequal-gen=false

// CCEEndpointList is a list of CCEEndpoint objects.
type CCEEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is a list of CCEEndpoint
	Items []CCEEndpoint `json:"items"`
}

func (ep *CCEEndpoint) AsObjectReference() *ObjectReference {
	return &ObjectReference{
		Namespace: ep.Namespace,
		Name:      ep.Name,
		UID:       string(ep.UID),
	}
}
