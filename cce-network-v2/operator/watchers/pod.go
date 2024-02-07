/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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

package watchers

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const PodNodeNameIndex = "pod-node"

var (
	// PodStore has a minimal copy of all pods running in the cluster.
	// Warning: The pods stored in the cache are not intended to be used for Update
	// operations in k8s as some of its fields are not populated.
	PodStore cache.Store

	// PodClient cache client to use pod updater
	PodClient = &PodUpdater{}
)

func initNodePodStore() {
	podInformer := k8s.WatcherClient().Informers.Core().V1().Pods().Informer()
	err := podInformer.AddIndexers(cache.Indexers{PodNodeNameIndex: podNodeNameIndexFunc})
	if err != nil {
		log.Fatal("init pod store with node indexer error")
	}

	PodStore = podInformer.GetStore()
}

type PodUpdater struct {
}

func (*PodUpdater) Lister() listv1.PodLister {
	return k8s.WatcherClient().Informers.Core().V1().Pods().Lister()
}

func (*PodUpdater) PodInterface(namespace string) typev1.PodInterface {
	return k8s.WatcherClient().CoreV1().Pods(namespace)
}

// podNodeNameIndexFunc indexes pods by node name
func podNodeNameIndexFunc(obj interface{}) ([]string, error) {
	pod := obj.(*corev1.Pod)
	if pod.Spec.NodeName != "" {
		return []string{pod.Spec.NodeName}, nil
	}
	return []string{}, nil
}
