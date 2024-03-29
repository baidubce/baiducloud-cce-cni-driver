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

// Code generated by lister-gen. DO NOT EDIT.

package v2

import (
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// PodSubnetTopologySpreadLister helps list PodSubnetTopologySpreads.
// All objects returned here must be treated as read-only.
type PodSubnetTopologySpreadLister interface {
	// List lists all PodSubnetTopologySpreads in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v2.PodSubnetTopologySpread, err error)
	// PodSubnetTopologySpreads returns an object that can list and get PodSubnetTopologySpreads.
	PodSubnetTopologySpreads(namespace string) PodSubnetTopologySpreadNamespaceLister
	PodSubnetTopologySpreadListerExpansion
}

// podSubnetTopologySpreadLister implements the PodSubnetTopologySpreadLister interface.
type podSubnetTopologySpreadLister struct {
	indexer cache.Indexer
}

// NewPodSubnetTopologySpreadLister returns a new PodSubnetTopologySpreadLister.
func NewPodSubnetTopologySpreadLister(indexer cache.Indexer) PodSubnetTopologySpreadLister {
	return &podSubnetTopologySpreadLister{indexer: indexer}
}

// List lists all PodSubnetTopologySpreads in the indexer.
func (s *podSubnetTopologySpreadLister) List(selector labels.Selector) (ret []*v2.PodSubnetTopologySpread, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v2.PodSubnetTopologySpread))
	})
	return ret, err
}

// PodSubnetTopologySpreads returns an object that can list and get PodSubnetTopologySpreads.
func (s *podSubnetTopologySpreadLister) PodSubnetTopologySpreads(namespace string) PodSubnetTopologySpreadNamespaceLister {
	return podSubnetTopologySpreadNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// PodSubnetTopologySpreadNamespaceLister helps list and get PodSubnetTopologySpreads.
// All objects returned here must be treated as read-only.
type PodSubnetTopologySpreadNamespaceLister interface {
	// List lists all PodSubnetTopologySpreads in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v2.PodSubnetTopologySpread, err error)
	// Get retrieves the PodSubnetTopologySpread from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v2.PodSubnetTopologySpread, error)
	PodSubnetTopologySpreadNamespaceListerExpansion
}

// podSubnetTopologySpreadNamespaceLister implements the PodSubnetTopologySpreadNamespaceLister
// interface.
type podSubnetTopologySpreadNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all PodSubnetTopologySpreads in the indexer for a given namespace.
func (s podSubnetTopologySpreadNamespaceLister) List(selector labels.Selector) (ret []*v2.PodSubnetTopologySpread, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v2.PodSubnetTopologySpread))
	})
	return ret, err
}

// Get retrieves the PodSubnetTopologySpread from the indexer for a given namespace and name.
func (s podSubnetTopologySpreadNamespaceLister) Get(name string) (*v2.PodSubnetTopologySpread, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v2.Resource("podsubnettopologyspread"), name)
	}
	return obj.(*v2.PodSubnetTopologySpread), nil
}
