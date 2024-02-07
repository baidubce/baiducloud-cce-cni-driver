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
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	corev1 "k8s.io/api/core/v1"
)

const (
	tlsCrtAttribute = "tls.crt"
	tlsKeyAttribute = "tls.key"

	tlsFieldSelector = "type=kubernetes.io/tls"
)

func (k *K8sWatcher) tlsSecretInit(k8sClient kubernetes.Interface, namespace string, swgSecrets *lock.StoppableWaitGroup) {
	secretOptsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = tlsFieldSelector
	}

	apiGroup := resources.K8sAPIGroupSecretV1Core
	_, secretController := informer.NewInformer(
		cache.NewFilteredListWatchFromClient(k8sClient.CoreV1().RESTClient(),
			"secrets", namespace,
			secretOptsModifier,
		),
		&corev1.Secret{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() {
					k.K8sEventReceived(apiGroup, metricSecret, resources.MetricCreate, valid, equal)
				}()
				if k8sSecret := k8s.ObjToV1Secret(obj); k8sSecret != nil {
					valid = true
					err := k.addK8sSecretV1(k8sSecret)
					k.K8sEventProcessed(metricSecret, resources.MetricCreate, err == nil)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricSecret, resources.MetricUpdate, valid, equal) }()
				if oldSecret := k8s.ObjToV1Secret(oldObj); oldSecret != nil {
					if newSecret := k8s.ObjToV1Secret(newObj); newSecret != nil {
						valid = true
						if reflect.DeepEqual(oldSecret, newSecret) {
							equal = true
							return
						}
						err := k.updateK8sSecretV1(oldSecret, newSecret)
						k.K8sEventProcessed(metricSecret, resources.MetricUpdate, err == nil)
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() {
					k.K8sEventReceived(apiGroup, metricSecret, resources.MetricDelete, valid, equal)
				}()
				k8sSecret := k8s.ObjToV1Secret(obj)
				if k8sSecret == nil {
					return
				}
				valid = true
				err := k.deleteK8sSecretV1(k8sSecret)
				k.K8sEventProcessed(metricSecret, resources.MetricDelete, err == nil)
			},
		},
		nil,
	)
	k.blockWaitGroupToSyncResources(k.stop, swgSecrets, secretController.HasSynced, resources.K8sAPIGroupSecretV1Core)
	go secretController.Run(k.stop)
	k.k8sAPIGroups.AddAPI(apiGroup)
}

// addK8sSecretV1 performs Envoy upsert operation for newly added secret.
func (k *K8sWatcher) addK8sSecretV1(secret *corev1.Secret) error {
	return nil
}

// updateK8sSecretV1 performs Envoy upsert operation for updated secret (if required).
func (k *K8sWatcher) updateK8sSecretV1(oldSecret, newSecret *corev1.Secret) error {

	return nil
}

// deleteK8sSecretV1 makes sure the related secret values in Envoy SDS is removed.
func (k *K8sWatcher) deleteK8sSecretV1(secret *corev1.Secret) error {

	return nil
}
