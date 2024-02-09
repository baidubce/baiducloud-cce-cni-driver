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

// Code generated by informer-gen. DO NOT EDIT.

package v2

import (
	"context"
	time "time"

	ccebaidubcecomv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	versioned "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned"
	internalinterfaces "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/informers/externalversions/internalinterfaces"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// CCEEndpointInformer provides access to a shared informer and lister for
// CCEEndpoints.
type CCEEndpointInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v2.CCEEndpointLister
}

type cCEEndpointInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewCCEEndpointInformer constructs a new informer for CCEEndpoint type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewCCEEndpointInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredCCEEndpointInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredCCEEndpointInformer constructs a new informer for CCEEndpoint type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredCCEEndpointInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CceV2().CCEEndpoints(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CceV2().CCEEndpoints(namespace).Watch(context.TODO(), options)
			},
		},
		&ccebaidubcecomv2.CCEEndpoint{},
		resyncPeriod,
		indexers,
	)
}

func (f *cCEEndpointInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredCCEEndpointInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *cCEEndpointInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&ccebaidubcecomv2.CCEEndpoint{}, f.defaultInformer)
}

func (f *cCEEndpointInformer) Lister() v2.CCEEndpointLister {
	return v2.NewCCEEndpointLister(f.Informer().GetIndexer())
}