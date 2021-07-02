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

package bbc

import (
	"sync"
	"time"

	"github.com/juju/ratelimit"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	datastorev2 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/keymutex"
)

const (
	PodAnnotationSpecificSubnets = "cce.io/subnets"
)

// subnet is a helper struct
type subnet struct {
	subnetID     string
	availableCnt int
}

type IPAM struct {
	lock     sync.RWMutex
	nodeLock keymutex.KeyMutex

	datastore         *datastorev2.DataStore
	addIPBackoffCache map[string]*wait.Backoff
	allocated         map[string]*v1alpha1.WorkloadEndpoint
	cacheHasSynced    bool

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	kubeInformer informers.SharedInformerFactory
	kubeClient   kubernetes.Interface

	crdInformer crdinformers.SharedInformerFactory
	crdClient   versioned.Interface

	cloud cloud.Interface
	clock clock.Clock

	cniMode   types.ContainerNetworkMode
	vpcID     string
	clusterID string

	bucket        *ratelimit.Bucket
	batchAddIPNum int

	// nodeENIMap is a map whose key is node name and value is eni Id
	nodeENIMap map[string]string

	gcPeriod time.Duration
}

var _ ipam.Interface = &IPAM{}
