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
	"context"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	cce_lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var CCEEndpointClient = &CCEEndpointUpdaterImpl{}

// EndpointEventHandler should implement the behavior to handle CCEEndpoint
type EndpointEventHandler interface {
	Create(resource *ccev2.CCEEndpoint) error
	Update(resource *ccev2.CCEEndpoint) error
	Delete(namespace, endpointName string) error
	Resync(context.Context) error
}

func StartSynchronizingCCEEndpoint(ctx context.Context, endpointManager EndpointEventHandler) error {
	log.Info("Starting to synchronize CCEEndpoint custom resources")

	// If both nodeManager and KVStore are nil, then we don't need to handle
	// any watcher events, but we will need to keep all NetResourceSets in
	// memory because 'netResourceSetStore' is used across the operator
	// to get the latest state of a CCEEndpoint.
	if endpointManager == nil {
		// Since we won't be handling any events we don't need to convert
		// objects.
		log.Warningf("no CCEEndpoint handler")
		return nil
	}

	// TODO: The operator is currently storing a full copy of the
	// CCEEndpoint resource, as the resource grows, we may want to consider
	// introducing a slim version of it.
	cceEndpointLister := k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Lister()

	endpointManagerSyncHandler := func(key string) error {
		log.Infof("Watching CCEEndpoint %v", key)
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			log.WithError(err).Error("Unable to process CCEEndpoint event")
			return err
		}
		obj, err := cceEndpointLister.CCEEndpoints(namespace).Get(name)

		// Delete handling
		if errors.IsNotFound(err) ||
			(obj != nil && obj.DeletionTimestamp != nil) {
			return endpointManager.Delete(namespace, name)
		}
		if err != nil {
			log.WithError(err).Warning("Unable to retrieve CCEEndpointCCEEndpoint from watcher store")
			return err
		}
		return endpointManager.Update(obj)
	}

	controller := cm.NewResyncController("cce-endpoint-watcher", int(operatorOption.Config.ResourceResyncWorkers), k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), endpointManagerSyncHandler)
	k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer().AddEventHandler(controller)
	controller.RunWithResync(operatorOption.Config.ResourceResyncInterval)

	return nil
}

type CCEEndpointUpdaterImpl struct {
}

func (c *CCEEndpointUpdaterImpl) Lister() cce_lister.CCEEndpointLister {
	return k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Lister()
}

func (c *CCEEndpointUpdaterImpl) Create(node *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	return k8s.CCEClient().CceV2().CCEEndpoints(node.Namespace).Create(context.TODO(), node, metav1.CreateOptions{})
}

func (c *CCEEndpointUpdaterImpl) Update(newResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	return k8s.CCEClient().CceV2().CCEEndpoints(newResource.Namespace).Update(context.TODO(), newResource, metav1.UpdateOptions{})
}

func (c *CCEEndpointUpdaterImpl) Delete(namespace, name string) error {
	return k8s.CCEClient().CceV2().CCEEndpoints(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (c *CCEEndpointUpdaterImpl) UpdateStatus(newResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	return k8s.CCEClient().CceV2().CCEEndpoints(newResource.Namespace).Update(context.TODO(), newResource, metav1.UpdateOptions{})
}

// GetByIP get CCEEndpoint by ip
func (c *CCEEndpointUpdaterImpl) GetByIP(ip string) ([]*ccev2.CCEEndpoint, error) {
	var data []*ccev2.CCEEndpoint
	endpointIndexer := k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer().GetIndexer()

	objs, err := endpointIndexer.ByIndex(IndexIPToEndpoint, ip)
	if err == nil {
		for _, obj := range objs {
			if cep, ok := obj.(*ccev2.CCEEndpoint); ok {
				data = append(data, cep)
			}
		}
	}
	return data, err
}
