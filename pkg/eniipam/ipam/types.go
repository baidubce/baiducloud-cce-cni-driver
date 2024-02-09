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

package ipam

import (
	"context"
	"time"

	"k8s.io/kubernetes/pkg/kubelet/dockershim/network"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
)

const (
	WepTypeSts        = "StatefulSet"
	WepTypePod        = "Pod"
	WepTypeReuseIPPod = "ReuseIPPod"

	WepLabelStsOwnerKey     = "cce.io/owner"
	WepLabelSubnetIDKey     = "cce.io/subnet-id"
	WepLabelInstanceTypeKey = "cce.io/instance-type"
	WepFinalizer            = "cce-cni.cce.io"

	IPPoolCreationSourceCNI  = "cce-cni"
	MwepTypeRoce             = "roce"
	MwepTypeERI              = "eri"
	MwepLabelInstanceTypeKey = "cce.io/instance-type"
	MwepFinalizer            = "cce-cni-roce.cce.io"

	// RDMANodeLabelAvailableKey = true and RDMANodeLabelCapableKey = true is node selector of rdma-device-plugin
	RDMANodeLabelAvailableKey = "feature.node.kubernetes.io/custom-rdma.available"
	RDMANodeLabelCapableKey   = "feature.node.kubernetes.io/custom-rdma.capable"

	// Ref: https://github.com/kubernetes/kubernetes/pull/71653
	KubeletCniTimeout = network.CNITimeoutSec * time.Second

	NodeInstanceType = "node.kubernetes.io/instance-type"

	// CCECniTimeout set to be much less than kubelet cni timeout
	CCECniTimeout = 60 * time.Second

	// Ref: https://github.com/kubernetes/kubernetes/blob/v1.18.9/pkg/kubelet/pod_workers.go#L269-L271
	CniRetryTimeout = 2 * time.Second

	LeakedPrivateIPExpiredTimeout = 10 * time.Minute
)

type Interface interface {
	Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error)
	Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error)
	Ready(ctx context.Context) bool
	Run(ctx context.Context, stopCh <-chan struct{}) error
}

type ExclusiveEniInterface interface {
	Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.CrossVPCEni, error)
	Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.CrossVPCEni, error)
	Run(ctx context.Context, stopCh <-chan struct{}) error
}
type RoceInterface interface {
	Allocate(ctx context.Context, name, namespace, containerID string, mac string,
		ipType rpc.IPType) (*v1alpha1.WorkloadEndpoint, error)
	Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error)
	Run(ctx context.Context, stopCh <-chan struct{}) error
	Ready(ctx context.Context) bool
}
