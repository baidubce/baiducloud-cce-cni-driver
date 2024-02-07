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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeENIs implements ENIInterface
type FakeENIs struct {
	Fake *FakeCceV2
}

var enisResource = schema.GroupVersionResource{Group: "cce.baidubce.com", Version: "v2", Resource: "enis"}

var enisKind = schema.GroupVersionKind{Group: "cce.baidubce.com", Version: "v2", Kind: "ENI"}

// Get takes name of the eNI, and returns the corresponding eNI object, and an error if there is any.
func (c *FakeENIs) Get(ctx context.Context, name string, options v1.GetOptions) (result *v2.ENI, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(enisResource, name), &v2.ENI{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2.ENI), err
}

// List takes label and field selectors, and returns the list of ENIs that match those selectors.
func (c *FakeENIs) List(ctx context.Context, opts v1.ListOptions) (result *v2.ENIList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(enisResource, enisKind, opts), &v2.ENIList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v2.ENIList{ListMeta: obj.(*v2.ENIList).ListMeta}
	for _, item := range obj.(*v2.ENIList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested eNIs.
func (c *FakeENIs) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(enisResource, opts))
}

// Create takes the representation of a eNI and creates it.  Returns the server's representation of the eNI, and an error, if there is any.
func (c *FakeENIs) Create(ctx context.Context, eNI *v2.ENI, opts v1.CreateOptions) (result *v2.ENI, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(enisResource, eNI), &v2.ENI{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2.ENI), err
}

// Update takes the representation of a eNI and updates it. Returns the server's representation of the eNI, and an error, if there is any.
func (c *FakeENIs) Update(ctx context.Context, eNI *v2.ENI, opts v1.UpdateOptions) (result *v2.ENI, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(enisResource, eNI), &v2.ENI{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2.ENI), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeENIs) UpdateStatus(ctx context.Context, eNI *v2.ENI, opts v1.UpdateOptions) (*v2.ENI, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(enisResource, "status", eNI), &v2.ENI{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2.ENI), err
}

// Delete takes name of the eNI and deletes it. Returns an error if one occurs.
func (c *FakeENIs) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(enisResource, name, opts), &v2.ENI{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeENIs) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(enisResource, listOpts)

	_, err := c.Fake.Invokes(action, &v2.ENIList{})
	return err
}

// Patch applies the patch and returns the patched eNI.
func (c *FakeENIs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v2.ENI, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(enisResource, name, pt, data, subresources...), &v2.ENI{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2.ENI), err
}
