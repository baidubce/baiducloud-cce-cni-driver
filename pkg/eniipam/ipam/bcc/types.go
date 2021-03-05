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

package bcc

import (
	"sync"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/juju/ratelimit"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
)

const (
	StsType   = "StatefulSet"
	PodType   = "Pod"
	OwnerKey  = "cce.io/owner"
	SubnetKey = "cce.io/subnet-id"

	StsPodAnnotationEnableFixIP       = "cce.io/sts-enable-fix-ip"
	EnableFixIPTrue                   = "True"
	StsPodAnnotationFixIPDeletePolicy = "cce.io/sts-pod-fix-ip-delete-policy"
	FixIPDeletePolicyNever            = "Never"
)

type SubnetSelectionPolicy string

const (
	SubnetSelectionPolicyLeastENI   SubnetSelectionPolicy = "LeastENI"
	SubnetSelectionPolicyMostFreeIP SubnetSelectionPolicy = "MostFreeIP"
)

type IPAM struct {
	lock sync.RWMutex
	// key is node name, value is list of enis attached
	eniCache map[string][]*enisdk.Eni
	// privateIPNumCache stores allocated IP num of each eni. key is eni id.
	privateIPNumCache map[string]int
	cacheHasSynced    bool
	// key is ip, value is wep
	allocated map[string]*v1alpha1.WorkloadEndpoint

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	kubeInformer informers.SharedInformerFactory
	kubeClient   kubernetes.Interface

	crdInformer crdinformers.SharedInformerFactory
	crdClient   versioned.Interface

	cloud cloud.Interface
	clock clock.Clock

	cniMode               types.ContainerNetworkMode
	vpcID                 string
	clusterID             string
	subnetSelectionPolicy SubnetSelectionPolicy
	bucket                *ratelimit.Bucket
	eniSyncPeriod         time.Duration
	informerResyncPeriod  time.Duration
	gcPeriod              time.Duration
}

var _ ipam.Interface = &IPAM{}
