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

package v2alpha1

import (
	"context"
	time "time"

	ccebaidubcecomv2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	versioned "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned"
	internalinterfaces "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/informers/externalversions/internalinterfaces"
	v2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// ClusterPodSubnetTopologySpreadInformer provides access to a shared informer and lister for
// ClusterPodSubnetTopologySpreads.
type ClusterPodSubnetTopologySpreadInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v2alpha1.ClusterPodSubnetTopologySpreadLister
}

type clusterPodSubnetTopologySpreadInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewClusterPodSubnetTopologySpreadInformer constructs a new informer for ClusterPodSubnetTopologySpread type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewClusterPodSubnetTopologySpreadInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredClusterPodSubnetTopologySpreadInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredClusterPodSubnetTopologySpreadInformer constructs a new informer for ClusterPodSubnetTopologySpread type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredClusterPodSubnetTopologySpreadInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				options.AllowWatchBookmarks = true
				return client.CceV2alpha1().ClusterPodSubnetTopologySpreads().List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				options.AllowWatchBookmarks = true
				return client.CceV2alpha1().ClusterPodSubnetTopologySpreads().Watch(context.TODO(), options)
			},
		},
		&ccebaidubcecomv2alpha1.ClusterPodSubnetTopologySpread{},
		resyncPeriod,
		indexers,
	)
}

func (f *clusterPodSubnetTopologySpreadInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredClusterPodSubnetTopologySpreadInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *clusterPodSubnetTopologySpreadInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&ccebaidubcecomv2alpha1.ClusterPodSubnetTopologySpread{}, f.defaultInformer)
}

func (f *clusterPodSubnetTopologySpreadInformer) Lister() v2alpha1.ClusterPodSubnetTopologySpreadLister {
	return v2alpha1.NewClusterPodSubnetTopologySpreadLister(f.Informer().GetIndexer())
}
