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

package ippool

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

func TestGetDefaultIPPoolName(t *testing.T) {
	type args struct {
		nodeName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			args: args{nodeName: "10.0.3.187"},
			want: "ippool-10-0-3-187",
		},
		{
			args: args{nodeName: "kube135"},
			want: "ippool-kube135",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetNodeIPPoolName(tt.args.nodeName); got != tt.want {
				t.Errorf("GetNodeIPPoolName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNodeMatchesIPPool(t *testing.T) {
	type args struct {
		node *v1.Node
		pool *v1alpha1.IPPool
	}

	node := &v1.Node{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"kubernetes.io/arch":     "amd64",
				"kubernetes.io/hostname": "172.16.38.206",
				"kubernetes.io/os":       "linux",
			},
		},
	}

	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "no label matches everything",
			args: args{
				node: node,
				pool: &v1alpha1.IPPool{
					Spec: v1alpha1.IPPoolSpec{
						NodeSelector: "",
					},
					Status: v1alpha1.IPPoolStatus{},
				},
			},
			want: true,
		},
		{
			name: "label matches",
			args: args{
				node: node,
				pool: &v1alpha1.IPPool{
					Spec: v1alpha1.IPPoolSpec{
						NodeSelector: "kubernetes.io/os=linux",
					},
					Status: v1alpha1.IPPoolStatus{},
				},
			},
			want: true,
		},
		{
			name: "label matches",
			args: args{
				node: node,
				pool: &v1alpha1.IPPool{
					Spec: v1alpha1.IPPoolSpec{
						NodeSelector: "kubernetes.io/os=linux,kubernetes.io/arch=amd64",
					},
					Status: v1alpha1.IPPoolStatus{},
				},
			},
			want: true,
		},
		{
			name: "label not match",
			args: args{
				node: node,
				pool: &v1alpha1.IPPool{
					Spec: v1alpha1.IPPoolSpec{
						NodeSelector: "kubernetes.io/os=windows",
					},
					Status: v1alpha1.IPPoolStatus{},
				},
			},
			want: false,
		},
		{
			name: "selector error",
			args: args{
				node: node,
				pool: &v1alpha1.IPPool{
					Spec: v1alpha1.IPPoolSpec{
						NodeSelector: "kubernetes.io/os===windows",
					},
					Status: v1alpha1.IPPoolStatus{},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NodeMatchesIPPool(tt.args.node, tt.args.pool)
			if (err != nil) != tt.wantErr {
				t.Errorf("NodeMatchesIPPool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("NodeMatchesIPPool() got = %v, want %v", got, tt.want)
			}
		})
	}
}
