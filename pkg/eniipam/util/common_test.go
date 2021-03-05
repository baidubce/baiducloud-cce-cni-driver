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

package util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

func TestGetInstanceID(t *testing.T) {
	type args struct {
		node *v1.Node
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			args: args{
				node: &v1.Node{
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				},
			},
			want: "i-xxxxx",
		},
		{
			args: args{
				node: &v1.Node{
					Spec: v1.NodeSpec{
						ProviderID: "",
					},
				},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetInstanceIDFromNode(tt.args.node); got != tt.want {
				t.Errorf("GetInstanceIDFromNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetStsName(t *testing.T) {
	type args struct {
		wep *v1alpha1.WorkloadEndpoint
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			args: args{
				wep: &v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-sts-0",
					},
				},
			},
			want: "test-sts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetStsName(tt.args.wep); got != tt.want {
				t.Errorf("GetStsName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetStsPodIndex(t *testing.T) {
	type args struct {
		wep *v1alpha1.WorkloadEndpoint
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			args: args{
				wep: &v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-sts-10",
					},
				},
			},
			want: 10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetStsPodIndex(tt.args.wep); got != tt.want {
				t.Errorf("GetStsPodIndex() = %v, want %v", got, tt.want)
			}
		})
	}
}
