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
	"context"
	"reflect"
	"testing"

	bbcapi "github.com/baidubce/bce-sdk-go/services/bbc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/golang/mock/gomock"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
)

func TestController_findSameZoneSubnets(t *testing.T) {
	type fields struct {
		ctrl                *gomock.Controller
		kubeClient          kubernetes.Interface
		crdClient           clientset.Interface
		metaClient          metadata.Interface
		eventRecorder       record.EventRecorder
		ippoolName          string
		cniMode             types.ContainerNetworkMode
		instanceID          string
		instanceType        metadata.InstanceTypeEx
		nodeName            string
		bccInstance         *bccapi.InstanceModel
		bbcInstance         *bbcapi.GetInstanceEniResult
		cloudClient         cloud.Interface
		eniSubnetCandidates []string
		eniSecurityGroups   []string
		preAttachedENINum   int
		podSubnetCandidates []string
		subnetZoneCache     map[string]string
	}
	type args struct {
		ctx     context.Context
		subnets []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []string
	}{
		{
			name: "normal case",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				cloudClient := mockcloud.NewMockInterface(ctrl)

				gomock.InOrder(
					cloudClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-a1").Return(&vpc.Subnet{ZoneName: "zoneA"}, nil),
					cloudClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-a2").Return(&vpc.Subnet{ZoneName: "zoneA"}, nil),
					cloudClient.EXPECT().DescribeSubnet(gomock.Any(), "sbn-b1").Return(&vpc.Subnet{ZoneName: "zoneB"}, nil),
				)

				return fields{
					ctrl: ctrl,
					bbcInstance: &bbcapi.GetInstanceEniResult{
						ZoneName:     "zoneB",
						PrivateIpSet: []bbcapi.PrivateIP{},
					},
					cloudClient:         cloudClient,
					eniSubnetCandidates: []string{},
					eniSecurityGroups:   []string{},
					preAttachedENINum:   0,
					podSubnetCandidates: []string{},
					subnetZoneCache:     map[string]string{},
				}
			}(),
			args: args{
				ctx:     context.TODO(),
				subnets: []string{"sbn-a1", "sbn-a2", "sbn-b1"},
			},
			want: []string{"sbn-b1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			c := &Controller{
				kubeClient:          tt.fields.kubeClient,
				crdClient:           tt.fields.crdClient,
				metaClient:          tt.fields.metaClient,
				eventRecorder:       tt.fields.eventRecorder,
				ippoolName:          tt.fields.ippoolName,
				cniMode:             tt.fields.cniMode,
				instanceID:          tt.fields.instanceID,
				instanceType:        tt.fields.instanceType,
				nodeName:            tt.fields.nodeName,
				bccInstance:         tt.fields.bccInstance,
				bbcInstance:         tt.fields.bbcInstance,
				cloudClient:         tt.fields.cloudClient,
				eniSubnetCandidates: tt.fields.eniSubnetCandidates,
				eniSecurityGroups:   tt.fields.eniSecurityGroups,
				preAttachedENINum:   tt.fields.preAttachedENINum,
				podSubnetCandidates: tt.fields.podSubnetCandidates,
				subnetZoneCache:     tt.fields.subnetZoneCache,
			}
			if got := c.findSameZoneSubnets(tt.args.ctx, tt.args.subnets); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Controller.findSameZoneSubnets() = %v, want %v", got, tt.want)
			}
		})
	}
}
