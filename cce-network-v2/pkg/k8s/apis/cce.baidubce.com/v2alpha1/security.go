package v2alpha1

import (
	"github.com/baidubce/bce-sdk-go/model"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=false
// +deepequal-gen=false
type SecurityGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []SecurityGroup `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +deepequal-gen=false
// +genclient:nonNamespaced
// +kubebuilder:resource:categories={cce},singular="securitygroup",path="securitygroups",shortName="sg",scope="Cluster"
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
//
// This columns will be printed by exec `kubectl get sgs`
// +kubebuilder:printcolumn:JSONPath=".spec.name",description="security group name",name="SGName",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.type",description="type of security group",name="Type",type=string
// +kubebuilder:printcolumn:JSONPath=".status.userRolesStr",description="roles of user",name="Roles",type=string
// +kubebuilder:printcolumn:JSONPath=".status.userCount",description="user count",name="UserCount",type=integer
// +kubebuilder:printcolumn:JSONPath=".status.constraintViolated",description="Is the Ingress constraint violated,",name="CV",type=integer
type SecurityGroup struct {
	// +deepequal-gen=false
	metav1.TypeMeta `json:",inline"`
	// +deepequal-gen=false
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecurityGroupSpec   `json:"spec,omitempty"`
	Status SecurityGroupStatus `json:"status,omitempty"`
}

type SecurityGroupType string
type SecurityGroupPolicy string

const (
	SecurityGroupTypeEnterprise SecurityGroupType = "enterprise"
	SecurityGroupTypeNormal     SecurityGroupType = "securitygroup"
	SecurityGroupTypeACL        SecurityGroupType = "acl"

	SecurityGroupPolicyAccept SecurityGroupPolicy = "accept"
	SecurityGroupPolicyDrop   SecurityGroupPolicy = "drop"

	SecurityGroupConstantAll   = "all"
	SecurityGroupConstantEmpty = ""

	SecurityGroupConstantIPv4 = "IPv4"
	SecurityGroupConstantIPv6 = "IPv6"
)

type SecurityGroupSpec struct {
	ID    string           `json:"id"`
	Name  string           `json:"name,omitempty"`
	Desc  string           `json:"desc,omitempty"`
	VpcId string           `json:"vpcId,omitempty"`
	Tags  []model.TagModel `json:"tags,omitempty"`

	// SeurityGroupType enterprise/normal/acl
	// normal is security group for vpc
	Type        SecurityGroupType  `json:"type"`
	IngressRule SecurityGroupRules `json:"ingressRule,omitempty"`
	EgressRule  SecurityGroupRules `json:"egressRule,omitempty"`
	Version     int64              `json:"vpcVersion,omitempty"`
}

type SecurityGroupRules struct {
	Allows []*bccapi.SecurityGroupRuleModel `json:"allows,omitempty"`
	Drops  []*bccapi.SecurityGroupRuleModel `json:"drops,omitempty"`
}

// SecurityGroupStatus desribes the status of security group
// Mainly explain which elements in the cluster are using this security group
type SecurityGroupStatus struct {
	Version      int64  `json:"version,omitempty"`
	UseStatus    bool   `json:"useStatus,omitempty"`
	UsedByMater  bool   `json:"usedByMater,omitempty"`
	UsedByNode   bool   `json:"usedByNode,omitempty"`
	UsedByPod    bool   `json:"usedByPod,omitempty"`
	UsedBySubnet bool   `json:"usedBySubnet,omitempty"`
	UsedByENI    bool   `json:"usedByENI,omitempty"`
	UserRolesStr string `json:"userRolesStr,omitempty"`
	MachineCount int    `json:"machineCount,omitempty"`
	ENICount     int    `json:"eniCount,omitempty"`
	UserCount    int    `json:"userCount,omitempty"`

	// ConstraintViolated is the Ingress constraint violated,
	// which may cause traffic to be unable to access the k8s cluster normally
	ConstraintViolated int `json:"constraintViolated,omitempty"`

	// LastAlterTime last alter time
	LastAlterTime *metav1.Time `json:"lastAlterTime,omitempty"`
}

type SecurityGroupUserRoles string

const (
	SecurityGroupUserRolesMaster                   SecurityGroupUserRoles = "master"
	SecurityGroupUserRolesNode                     SecurityGroupUserRoles = "node"
	SecurityGroupUserRolesPod                      SecurityGroupUserRoles = "pod"
	SecurityGroupUserRolesLbSecurityGroupUserRoles                        = "lb"
)

// SecurityReleationType desribes the type of releation between security group and other resources
type SecurityReleationType string

const (
	SecurityReleationTypeBCC    SecurityReleationType = "bcc"
	SecurityReleationTypeBBC                          = "bbc"
	SecurityReleationTypeEni    SecurityReleationType = "eni"
	SecurityReleationTypeSubnet SecurityReleationType = "subnet"
	SecurityReleationTypeBlb    SecurityReleationType = "blb"
)

// SecurityReleationType desribes the type of releation between security group and other resources
type SecurityReleation map[SecurityReleationType][]string
