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
	eipmodel "github.com/baidubce/bce-sdk-go/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type EIPModeType string

const (
	EIPModeTypeNAT    EIPModeType = "NAT"
	EIPModeTypeDirect EIPModeType = "Direct"
)

type TagModel struct {
	TagKey   string `json:"tagKey"`
	TagValue string `json:"tagValue"`
}

// EIPSpec defines the desired state of EIP
type EIPSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// CCE defined filed
	CreatedByCCE bool        `json:"createdByCCE"`
	Mode         EIPModeType `json:"mode"`

	// VPC defined field
	Name            string              `json:"name,omitempty"`
	EIP             string              `json:"eip,omitempty"`
	EIPID           string              `json:"eipID,omitempty"`
	Status          string              `json:"status,omitempty"`
	EIPInstanceType string              `json:"eipInstanceType,omitempty"`
	ShareGroupID    string              `json:"shareGroupID,omitempty"`
	ClusterID       string              `json:"clusterID,omitempty"`
	BandWidthInMbps int                 `json:"bandwidthInMbps,omitempty"`
	PaymentTiming   string              `json:"paymentTiming,omitempty"`
	BillingMethod   string              `json:"billingMethod,omitempty"`
	CreateTime      string              `json:"createTime,omitempty"`
	Tags            []eipmodel.TagModel `json:"tags,omitempty"`
}

type EIPStatusType string

const (
	EIPStatusTypeEmpty     EIPStatusType = ""
	EIPStatusTypeAvailable EIPStatusType = "Available"
	EIPStatusTypeBinding   EIPStatusType = "Binding"
	EIPStatusTypeBinded    EIPStatusType = "Binded"
	EIPStatusTypeUnBinding EIPStatusType = "UnBinding"
	EIPStatusTypeUnBinded  EIPStatusType = "UnBinded"
)

// EIPStatus defines the observed state of EIP
type EIPStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// CCE defined filed
	Mode       EIPModeType   `json:"mode"`
	Status     EIPStatusType `json:"status"`
	Endpoint   string        `json:"endpoint,omitempty"`
	InstanceID string        `json:"instanceID,omitempty"`
	PrivateIP  string        `json:"privateIP,omitempty"`

	// VPC defined filed
	StatusInVPC  string `json:"statusInVPC,omitempty"`
	InstanceType string `json:"instanceType,omitempty"`
	ExpireTime   string `json:"expireTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=".status.status",description="ENI Status",name="Status",type=string
// +kubebuilder:printcolumn:JSONPath=".status.statusInVPC",description="ENI Status in VPC",name="VPCStatus",type=string
// +kubebuilder:printcolumn:JSONPath=".status.mode",description="ENI Mode",name="Mode",type=string
// +kubebuilder:printcolumn:JSONPath=".status.endpoint",description="ns/pod-name",name="Endpoint",type=string
// +kubebuilder:printcolumn:JSONPath=".status.instanceID",description="ENI InstanceID the EIP Binded",name="ENI",type=string
// +kubebuilder:printcolumn:JSONPath=".status.privateIP",description="ENI PrivateIP",name="PrivateIP",type=string

// EIP is the Schema for the eips API
type EIP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EIPSpec   `json:"spec,omitempty"`
	Status EIPStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// EIPList contains a list of EIP
type EIPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EIP `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EIP{}, &EIPList{})
}
