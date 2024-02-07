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

package eni

import (
	"strings"
	"testing"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

func TestGetMaxIPPerENI(t *testing.T) {
	type fields struct {
		memoryCapacityInGB int
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{
			name: "Negative Memory",
			fields: fields{
				memoryCapacityInGB: -1,
			},
			want: 0,
		},
		{
			name: "Zero Memory",
			fields: fields{
				memoryCapacityInGB: 0,
			},
			want: 0,
		},
		{
			name: "Big Number",
			fields: fields{
				memoryCapacityInGB: 312321,
			},
			want: 40,
		},
		{
			name: "Level 1",
			fields: fields{
				memoryCapacityInGB: 1,
			},
			want: 2,
		},
		{
			name: "Level 2",
			fields: fields{
				memoryCapacityInGB: 4,
			},
			want: 8,
		},
		{
			name: "Level 3",
			fields: fields{
				memoryCapacityInGB: 16,
			},
			want: 16,
		},
		{
			name: "Level 3 - Border",
			fields: fields{
				memoryCapacityInGB: 32,
			},
			want: 16,
		},
		{
			name: "Level 4",
			fields: fields{
				memoryCapacityInGB: 33,
			},
			want: 30,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetMaxIPPerENI(tt.fields.memoryCapacityInGB); got != tt.want {
				t.Errorf("GetMaxIPPerENI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMaxENIPerNode(t *testing.T) {
	type fields struct {
		CPUCount int
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{
			name: "Negative Memory",
			fields: fields{
				CPUCount: -1,
			},
			want: 0,
		},
		{
			name: "Zero Memory",
			fields: fields{
				CPUCount: 0,
			},
			want: 0,
		},
		{
			name: "Big Number",
			fields: fields{
				CPUCount: 312321,
			},
			want: 8,
		},
		{
			name: "Level 1",
			fields: fields{
				CPUCount: 4,
			},
			want: 4,
		},

		{
			name: "Level 2",
			fields: fields{
				CPUCount: 132,
			},
			want: 8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetMaxENIPerNode(tt.fields.CPUCount); got != tt.want {
				t.Errorf("GetMaxENIPerNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateNameForENI(t *testing.T) {
	type fields struct {
		clusterID  string
		instanceID string
		nodeName   string
	}
	tests := []struct {
		name       string
		fields     fields
		wantPrefix string
	}{
		{
			name: "Normal",
			fields: fields{
				clusterID:  "cluster-1",
				instanceID: "instance-1",
				nodeName:   "node-1",
			},
			wantPrefix: "cluster-1/instance-1/node-1",
		},
		{
			name: "Empty ClusterID",
			fields: fields{
				clusterID:  "",
				instanceID: "instance-1",
				nodeName:   "node-1",
			},
			wantPrefix: "/instance-1/node-1",
		},
		{
			name: "Empty InstanceID",
			fields: fields{
				clusterID:  "cluster-1",
				instanceID: "",
				nodeName:   "node-1",
			},
			wantPrefix: "cluster-1//node-1",
		},
		{
			name: "Empty Node Name",
			fields: fields{
				clusterID:  "cluster-1",
				instanceID: "instance-1",
				nodeName:   "",
			},
			wantPrefix: "cluster-1/instance-1/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CreateNameForENI(tt.fields.clusterID, tt.fields.instanceID, tt.fields.nodeName); !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("CreateNameForENI() = %v, want prefix %v", got, tt.wantPrefix)
			}
		})
	}
}

func TestENICreatedByCCE(t *testing.T) {
	type fields struct {
		eni *enisdk.Eni
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "Normal Prefix:c-",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "c-12345678/i-12345678/nodename/123456",
				},
			},
			want: true,
		},
		{
			name: "Normal Prefix:cce-",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "cce-12345678/i-12345678/nodename/123456",
				},
			},
			want: true,
		},
		{
			name: "False Prefix:c-",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "c-12345678/12345678/nodename/123456",
				},
			},
			want: false,
		},
		{
			name: "False Prefix:cce-",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "cce-12345678/12345678/nodename/123456",
				},
			},
			want: false,
		},
		{
			name: "Nil",
			fields: fields{
				eni: nil,
			},
			want: false,
		},
		{
			name: "Name Format",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "formaterror",
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ENICreatedByCCE(tt.fields.eni); got != tt.want {
				t.Errorf("ENICreatedByCCE() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestENIOwnedByNode(t *testing.T) {
	type fields struct {
		eni        *enisdk.Eni
		clusterID  string
		instanceID string
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "Normal",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "cce-12345678/i-12345678/nodename/123456",
				},
				clusterID:  "cce-12345678",
				instanceID: "i-12345678",
			},
			want: true,
		},
		{
			name: "Cluster Not Match",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "cce-12345678/i-12345678/nodename/123456",
				},
				clusterID:  "cce-not-match",
				instanceID: "i-12345678",
			},
			want: false,
		},
		{
			name: "Instance Not Match",
			fields: fields{
				eni: &enisdk.Eni{
					Name: "cce-12345678/i-12345678/nodename/123456",
				},
				clusterID:  "cce-12345678",
				instanceID: "i-not-match",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ENIOwnedByNode(tt.fields.eni, tt.fields.clusterID, tt.fields.instanceID); got != tt.want {
				t.Errorf("ENIOwnedByNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPrivateIPSet(t *testing.T) {
	type fields struct {
		eni *enisdk.Eni
	}
	tests := []struct {
		name   string
		fields fields
		want   []v1alpha1.PrivateIP
	}{
		{
			name: "Normal",
			fields: fields{
				eni: &enisdk.Eni{
					PrivateIpSet: []enisdk.PrivateIp{{PrivateIpAddress: "10.1.1.2"}, {PrivateIpAddress: "10.1.1.3"}},
				},
			},
			want: []v1alpha1.PrivateIP{{PrivateIPAddress: "10.1.1.2"}, {PrivateIPAddress: "10.1.1.3"}},
		},
		{
			name: "Nil ENI",
			fields: fields{
				eni: nil,
			},
			want: nil,
		},
		{
			name: "Empty Set",
			fields: fields{
				eni: &enisdk.Eni{
					PrivateIpSet: []enisdk.PrivateIp{},
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPrivateIPSet(tt.fields.eni)
			if len(got) != len(tt.want) {
				t.Errorf("GetPrivateIPSet() = %v, want %v", got, tt.want)
				return
			}
			for i := 0; i < len(got); i++ {
				if got[i].PrivateIPAddress != tt.want[i].PrivateIPAddress {
					t.Errorf("GetPrivateIPSet() = %v, want %v", got, tt.want)
					return
				}
			}
		})
	}
}

func TestGetMaxIPPerENIFromNodeAnnotations(t *testing.T) {
	type fields struct {
		node *v1.Node
	}
	var tests = []struct {
		name    string
		fields  fields
		want    int
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/max-ip-per-eni": "100",
						},
					},
				},
			},
			want:    100,
			wantErr: false,
		},
		{
			name: "Annotation Not Exist",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"TargetAnnotation": "100",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "Annotation Not Int",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/max-ip-per-eni": "notint",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := GetMaxIPPerENIFromNodeAnnotations(tt.fields.node)
			if got != tt.want {
				t.Errorf("GetMaxIPPerENIFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("GetMaxIPPerENIFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
		})
	}
}

func TestGetMaxENINumFromNodeAnnotations(t *testing.T) {
	type fields struct {
		node *v1.Node
	}
	var tests = []struct {
		name    string
		fields  fields
		want    int
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/max-eni-num": "100",
						},
					},
				},
			},
			want:    100,
			wantErr: false,
		},
		{
			name: "Annotation Not Exist",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"TargetAnnotation": "100",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "Annotation Not Int",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/max-eni-num": "notint",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := GetMaxENINumFromNodeAnnotations(tt.fields.node)
			if got != tt.want {
				t.Errorf("GetMaxENINumFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("GetMaxENINumFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
		})
	}
}

func TestGetPreAttachedENINumFromNodeAnnotations(t *testing.T) {
	type fields struct {
		node *v1.Node
	}
	var tests = []struct {
		name    string
		fields  fields
		want    int
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/pre-attached-eni-num": "100",
						},
					},
				},
			},
			want:    100,
			wantErr: false,
		},
		{
			name: "Annotation Not Exist",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"TargetAnnotation": "100",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "Annotation Not Int",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/pre-attached-eni-num": "notint",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := GetPreAttachedENINumFromNodeAnnotations(tt.fields.node)
			if got != tt.want {
				t.Errorf("GetPreAttachedENINumFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("GetPreAttachedENINumFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
		})
	}
}

func TestGetWarmIPTargetFromNodeAnnotations(t *testing.T) {
	type fields struct {
		node *v1.Node
	}
	var tests = []struct {
		name    string
		fields  fields
		want    int
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/warm-ip-target": "100",
						},
					},
				},
			},
			want:    100,
			wantErr: false,
		},
		{
			name: "Annotation Not Exist",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"TargetAnnotation": "100",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "Annotation Not Int",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"cce.io/warm-ip-target": "notint",
						},
					},
				},
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := GetWarmIPTargetFromNodeAnnotations(tt.fields.node)
			if got != tt.want {
				t.Errorf("GetWarmIPTargetFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("GetWarmIPTargetFromNodeAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
		})
	}
}

func TestGetIntegerFromAnnotations(t *testing.T) {
	type fields struct {
		node       *v1.Node
		annotation string
	}
	var tests = []struct {
		name    string
		fields  fields
		want    int
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"TargetAnnotation": "100",
						},
					},
				},
				annotation: "TargetAnnotation",
			},
			want:    100,
			wantErr: false,
		},
		{
			name: "Node Nil",
			fields: fields{
				node:       nil,
				annotation: "TargetAnnotation",
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "Node Nil Annotation",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: nil,
					},
				},
				annotation: "TargetAnnotation",
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "Annotation Not Exist",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"TargetAnnotation": "100",
						},
					},
				},
				annotation: "AnnotationNotExist",
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "Annotation Not Int",
			fields: fields{
				node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"TargetAnnotation": "notint",
						},
					},
				},
				annotation: "AnnotationNotExist",
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := getIntegerFromAnnotations(tt.fields.node, tt.fields.annotation)
			if got != tt.want {
				t.Errorf("getIntegerFromAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("getIntegerFromAnnotations() = %v %v, want %v %v", got, gotErr, tt.want, tt.wantErr)
				return
			}
		})
	}
}
