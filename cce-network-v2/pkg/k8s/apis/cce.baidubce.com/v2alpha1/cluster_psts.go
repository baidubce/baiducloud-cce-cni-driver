package v2alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

var (
	// DefaultReuseIPTTL default timeout for reusing IP addresses.
	// If this time is not set, the default value is 7 days
	DefaultReuseIPTTL = &metav1.Duration{Duration: time.Hour * 24 * 7}
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={cce},singular="clusterpodsubnettopologyspread",path="clusterpodsubnettopologyspreads",scope="Cluster",shortName={cpsts}
// +kubebuilder:storageversion

// ClusterPodSubnetTopologySpread describes how to distribute pods in the scenario of sub customized subnets
type ClusterPodSubnetTopologySpread struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterPodSubnetTopologySpreadSpec   `json:"spec,omitempty"`
	Status ClusterPodSubnetTopologySpreadStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterPodSubnetTopologySpreadList contains a list of PodSubnetTopologySpread
type ClusterPodSubnetTopologySpreadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterPodSubnetTopologySpread `json:"items"`
}

type ClusterPodSubnetTopologySpreadSpec struct {
	ccev2.PodSubnetTopologySpreadSpec `json:",inline"`
	NamespaceSelector                 *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

type ClusterPodSubnetTopologySpreadStatus struct {
	SubStatus         map[string]ccev2.PodSubnetTopologySpreadStatus `json:"subStatus,omitempty"`
	SubObjectCount    int                                            `json:"subobject-count,omitempty"`
	ExpectObjectCount int                                            `json:"expect-object-count,omitempty"`
	Status            ccev2.PodSubnetTopologySpreadStatus            `json:"status,omitempty"`
}
