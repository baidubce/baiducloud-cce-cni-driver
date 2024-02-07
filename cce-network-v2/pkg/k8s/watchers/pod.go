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
	"fmt"
	"reflect"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	corev1 "k8s.io/api/core/v1"
)

func (k *K8sWatcher) createPodController(getter cache.Getter, fieldSelector fields.Selector) (cache.Store, cache.Controller) {
	apiGroup := resources.K8sAPIGroupPodV1Core
	return informer.NewInformer(
		cache.NewListWatchFromClient(getter,
			"pods", v1.NamespaceAll, fieldSelector),
		&corev1.Pod{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				var valid bool
				if pod := k8s.ObjTov1Pod(obj); pod != nil {
					valid = true
					metrics.EventLagK8s.Set(0)
					err := k.addK8sPodV1(pod)
					k.K8sEventProcessed(metricPod, resources.MetricCreate, err == nil)
				}
				k.K8sEventReceived(apiGroup, metricPod, resources.MetricCreate, valid, false)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				var valid, equal bool
				if oldPod := k8s.ObjTov1Pod(oldObj); oldPod != nil {
					if newPod := k8s.ObjTov1Pod(newObj); newPod != nil {
						valid = true
						if reflect.DeepEqual(oldPod, newPod) {
							equal = true
						} else {
							err := k.updateK8sPodV1(oldPod, newPod)
							k.K8sEventProcessed(metricPod, resources.MetricUpdate, err == nil)
						}
					}
				}
				k.K8sEventReceived(apiGroup, metricPod, resources.MetricUpdate, valid, equal)
			},
			DeleteFunc: func(obj interface{}) {
				var valid bool
				if pod := k8s.ObjTov1Pod(obj); pod != nil {
					valid = true
					k.K8sEventProcessed(metricPod, resources.MetricDelete, false)
				}
				k.K8sEventReceived(apiGroup, metricPod, resources.MetricDelete, valid, false)
			},
		},
		nil,
	)
}

func (k *K8sWatcher) podsInit(k8sClient *k8s.K8sClient, asyncControllers *sync.WaitGroup) {
	var once sync.Once
	watchNodePods := func() chan struct{} {
		// Only watch for pod events for our node.
		podStore, podController := informer.NewInformer(
			cache.NewListWatchFromClient(k8sClient.CoreV1().RESTClient(),
				"pods", v1.NamespaceAll, fields.ParseSelectorOrDie("spec.nodeName="+nodeTypes.GetName())),
			&v1.Pod{},
			10*time.Hour,
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					var valid bool
					if pod := k8s.ObjTov1Pod(obj); pod != nil {
						valid = true
						metrics.EventLagK8s.Set(0)
						err := k.addK8sPodV1(pod)
						k.K8sEventProcessed(metricPod, resources.MetricCreate, err == nil)
					}
					k.K8sEventReceived(resources.K8sAPIGroupPodV1Core, metricPod, resources.MetricCreate, valid, false)
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					var valid, equal bool
					if oldPod := k8s.ObjTov1Pod(oldObj); oldPod != nil {
						if newPod := k8s.ObjTov1Pod(newObj); newPod != nil {
							valid = true
							if reflect.DeepEqual(oldPod, newPod) {
								equal = true
							} else {
								err := k.updateK8sPodV1(oldPod, newPod)
								k.K8sEventProcessed(metricPod, resources.MetricUpdate, err == nil)
							}
						}
					}
					k.K8sEventReceived(resources.K8sAPIGroupPodV1Core, metricPod, resources.MetricUpdate, valid, equal)
				},
				DeleteFunc: func(obj interface{}) {
					var valid bool
					if pod := k8s.ObjTov1Pod(obj); pod != nil {
						valid = true
						k.K8sEventProcessed(metricPod, resources.MetricDelete, false)
					}
					k.K8sEventReceived(resources.K8sAPIGroupPodV1Core, metricPod, resources.MetricDelete, valid, false)
				},
			},
			nil,
		)

		isConnected := make(chan struct{})
		k.podStoreMU.Lock()
		k.podStore = podStore
		k.podStoreMU.Unlock()
		k.podStoreOnce.Do(func() {
			close(k.podStoreSet)
		})

		k.blockWaitGroupToSyncResources(isConnected, nil, podController.HasSynced, resources.K8sAPIGroupPodV1Core)
		once.Do(func() {
			asyncControllers.Done()
			k.k8sAPIGroups.AddAPI(resources.K8sAPIGroupPodV1Core)
		})
		go func() {
			podController.Run(isConnected)
		}()
		return isConnected
	}

	// We will watch for pods on th entire cluster to keep existing
	// functionality untouched. If we are running with CCEEndpoint CRD
	// enabled then it means that we can simply watch for pods that are created
	// for this node.
	if !option.Config.DisableCCEEndpointCRD {
		watchNodePods()
		return
	}

	// If CCEEndpointCRD is disabled, we will fallback on watching all pods
	// and then watching on the pods created for this node if the
	// K8sEventHandover is enabled.
	for {
		podStore, podController := k.createPodController(
			k8sClient.CoreV1().RESTClient(),
			fields.Everything())

		isConnected := make(chan struct{})
		// once isConnected is closed, it will stop waiting on caches to be
		// synchronized.
		k.blockWaitGroupToSyncResources(isConnected, nil, podController.HasSynced, resources.K8sAPIGroupPodV1Core)
		once.Do(func() {
			asyncControllers.Done()
			k.k8sAPIGroups.AddAPI(resources.K8sAPIGroupPodV1Core)
		})
		go podController.Run(isConnected)

		k.podStoreMU.Lock()
		k.podStore = podStore
		k.podStoreMU.Unlock()
		k.podStoreOnce.Do(func() {
			close(k.podStoreSet)
		})

		if !option.Config.K8sEventHandover {
			return
		}

		close(isConnected)

		log.WithField(logfields.Node, nodeTypes.GetName()).Info("Connected to KVStore, watching for pod events on node")
		isConnected = watchNodePods()

		close(isConnected)
		log.Info("Disconnected from KVStore, watching for pod events all nodes")
	}
}

func (k *K8sWatcher) addK8sPodV1(pod *corev1.Pod) error {
	return nil
}

func (k *K8sWatcher) updateK8sPodV1(oldK8sPod, newK8sPod *corev1.Pod) error {
	if oldK8sPod == nil || newK8sPod == nil {
		return nil
	}

	return nil
}

type PodClient struct {
	k *K8sWatcher
}

func (k *K8sWatcher) NewPodClient() *PodClient {
	return &PodClient{k: k}
}

func (client *PodClient) Get(namespace, name string) (*corev1.Pod, error) {
	if client.k.podStore == nil {
		return nil, fmt.Errorf("cache have not init")
	}
	obj, exist, err := client.k.podStore.GetByKey(namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	pod, ok := obj.(*corev1.Pod)
	if !exist || !ok {
		return nil, errors.NewNotFound(schema.GroupResource{
			Group:    corev1.SchemeGroupVersion.Group,
			Resource: "Pod",
		}, name)
	}
	return pod, nil
}

func (client *PodClient) List() ([]*corev1.Pod, error) {
	var podList []*corev1.Pod
	if client.k.podStore == nil {
		return nil, fmt.Errorf("cache have not init")
	}
	for _, key := range client.k.podStore.ListKeys() {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return podList, err
		}
		pod, err := client.Get(namespace, name)
		if err != nil {
			return podList, err
		}
		podList = append(podList, pod)
	}
	return podList, nil
}
