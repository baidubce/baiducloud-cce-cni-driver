/*
Copyright (c) 2023 Baidu, Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PodEIPBindStrategySpec defines the desired state of PodEIPBindStrategy
type PodEIPBindStrategySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	StaticEIPPool []string `json:"staticEIPPool,omitempty"`

	Selector *metav1.LabelSelector `json:"selector"`
}

// PodEIPBindStrategyStatus defines the observed state of PodEIPBindStrategy
type PodEIPBindStrategyStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	TotalCount     int                          `json:"totalCount"`
	AvailableCount int                          `json:"availableCount"`
	BindedCount    int                          `json:"bindedCount"`
	AvailableEIPs  map[string]map[string]string `json:"availableEIPs,omitempty"`
	BindedEIPs     map[string]map[string]string `json:"bindedEIPs,omitempty"`
	TotalEIPs      map[string]map[string]string `json:"totalEIPs,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=".status.totalCount",description="total EIP count",name="Total",type=string
// +kubebuilder:printcolumn:JSONPath=".status.availableCount",description="available EIP count",name="Available",type=string
// +kubebuilder:printcolumn:JSONPath=".status.bindedCount",description="Binded EIP count",name="Binded",type=string
//+kubebuilder:resource:shortName=pebs;pebss

// PodEIPBindStrategy is the Schema for the podeipbindstrategies API
type PodEIPBindStrategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PodEIPBindStrategySpec `json:"spec,omitempty"`

	// +kubebuilder:validation:Optional
	Status PodEIPBindStrategyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PodEIPBindStrategyList contains a list of PodEIPBindStrategy
type PodEIPBindStrategyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodEIPBindStrategy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodEIPBindStrategy{}, &PodEIPBindStrategyList{})
}
