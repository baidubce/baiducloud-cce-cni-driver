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
	v1 "k8s.io/api/core/v1"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	corev1 "k8s.io/api/core/v1"
)

func (k *K8sWatcher) servicesInit(k8sClient kubernetes.Interface, swgSvcs *lock.StoppableWaitGroup, optsModifier func(*v1meta.ListOptions)) {
	apiGroup := resources.K8sAPIGroupServiceV1Core
	_, svcController := informer.NewInformer(
		cache.NewFilteredListWatchFromClient(k8sClient.CoreV1().RESTClient(),
			"services", v1.NamespaceAll, optsModifier),
		&corev1.Service{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() {
					k.K8sEventReceived(apiGroup, resources.MetricService, resources.MetricCreate, valid, equal)
				}()
				if k8sSvc := k8s.ObjToV1Services(obj); k8sSvc != nil {
					valid = true
					err := k.addK8sServiceV1(k8sSvc, swgSvcs)
					k.K8sEventProcessed(resources.MetricService, resources.MetricCreate, err == nil)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, resources.MetricService, resources.MetricUpdate, valid, equal) }()
				if oldk8sSvc := k8s.ObjToV1Services(oldObj); oldk8sSvc != nil {
					if newk8sSvc := k8s.ObjToV1Services(newObj); newk8sSvc != nil {
						valid = true

						err := k.updateK8sServiceV1(oldk8sSvc, newk8sSvc, swgSvcs)
						k.K8sEventProcessed(resources.MetricService, resources.MetricUpdate, err == nil)
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, resources.MetricService, resources.MetricDelete, valid, equal) }()
				k8sSvc := k8s.ObjToV1Services(obj)
				if k8sSvc == nil {
					return
				}

				valid = true
				err := k.deleteK8sServiceV1(k8sSvc, swgSvcs)
				k.K8sEventProcessed(resources.MetricService, resources.MetricDelete, err == nil)
			},
		},
		nil,
	)
	k.blockWaitGroupToSyncResources(k.stop, swgSvcs, svcController.HasSynced, resources.K8sAPIGroupServiceV1Core)
	go svcController.Run(k.stop)
	k.k8sAPIGroups.AddAPI(apiGroup)
}

func (k *K8sWatcher) addK8sServiceV1(svc *corev1.Service, swg *lock.StoppableWaitGroup) error {
	return nil
}

func (k *K8sWatcher) updateK8sServiceV1(oldSvc, newSvc *corev1.Service, swg *lock.StoppableWaitGroup) error {
	return k.addK8sServiceV1(newSvc, swg)
}

func (k *K8sWatcher) deleteK8sServiceV1(svc *corev1.Service, swg *lock.StoppableWaitGroup) error {

	return nil
}
