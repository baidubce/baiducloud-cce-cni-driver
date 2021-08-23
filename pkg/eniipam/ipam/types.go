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

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

const (
	WepTypeSts              = "StatefulSet"
	WepTypePod              = "Pod"
	WepLabelStsOwnerKey     = "cce.io/owner"
	WepLabelSubnetIDKey     = "cce.io/subnet-id"
	WepLabelInstanceTypeKey = "cce.io/instance-type"
	WepFinalizer            = "cce-cni.cce.io"

	IPPoolCreationSourceCNI = "cce-cni"

	// CniTimeout set to be slightly less than 220 sec in kubelet
	// Ref: https://github.com/kubernetes/kubernetes/pull/71653
	CniTimeout = 200 * time.Second
)

type Interface interface {
	Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error)
	Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error)
	Ready(ctx context.Context) bool
	Run(ctx context.Context, stopCh <-chan struct{}) error
}
