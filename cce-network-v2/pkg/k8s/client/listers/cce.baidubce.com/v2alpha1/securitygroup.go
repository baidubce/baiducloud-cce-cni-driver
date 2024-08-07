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

package v2alpha1

import (
	v2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// SecurityGroupLister helps list SecurityGroups.
// All objects returned here must be treated as read-only.
type SecurityGroupLister interface {
	// List lists all SecurityGroups in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v2alpha1.SecurityGroup, err error)
	// Get retrieves the SecurityGroup from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v2alpha1.SecurityGroup, error)
	SecurityGroupListerExpansion
}

// securityGroupLister implements the SecurityGroupLister interface.
type securityGroupLister struct {
	indexer cache.Indexer
}

// NewSecurityGroupLister returns a new SecurityGroupLister.
func NewSecurityGroupLister(indexer cache.Indexer) SecurityGroupLister {
	return &securityGroupLister{indexer: indexer}
}

// List lists all SecurityGroups in the indexer.
func (s *securityGroupLister) List(selector labels.Selector) (ret []*v2alpha1.SecurityGroup, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v2alpha1.SecurityGroup))
	})
	return ret, err
}

// Get retrieves the SecurityGroup from the index for a given name.
func (s *securityGroupLister) Get(name string) (*v2alpha1.SecurityGroup, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v2alpha1.Resource("securitygroup"), name)
	}
	return obj.(*v2alpha1.SecurityGroup), nil
}
