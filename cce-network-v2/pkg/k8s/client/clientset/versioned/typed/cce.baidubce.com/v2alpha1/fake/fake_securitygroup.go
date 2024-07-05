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

	v2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeSecurityGroups implements SecurityGroupInterface
type FakeSecurityGroups struct {
	Fake *FakeCceV2alpha1
}

var securitygroupsResource = schema.GroupVersionResource{Group: "cce.baidubce.com", Version: "v2alpha1", Resource: "securitygroups"}

var securitygroupsKind = schema.GroupVersionKind{Group: "cce.baidubce.com", Version: "v2alpha1", Kind: "SecurityGroup"}

// Get takes name of the securityGroup, and returns the corresponding securityGroup object, and an error if there is any.
func (c *FakeSecurityGroups) Get(ctx context.Context, name string, options v1.GetOptions) (result *v2alpha1.SecurityGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(securitygroupsResource, name), &v2alpha1.SecurityGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha1.SecurityGroup), err
}

// List takes label and field selectors, and returns the list of SecurityGroups that match those selectors.
func (c *FakeSecurityGroups) List(ctx context.Context, opts v1.ListOptions) (result *v2alpha1.SecurityGroupList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(securitygroupsResource, securitygroupsKind, opts), &v2alpha1.SecurityGroupList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v2alpha1.SecurityGroupList{ListMeta: obj.(*v2alpha1.SecurityGroupList).ListMeta}
	for _, item := range obj.(*v2alpha1.SecurityGroupList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested securityGroups.
func (c *FakeSecurityGroups) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(securitygroupsResource, opts))
}

// Create takes the representation of a securityGroup and creates it.  Returns the server's representation of the securityGroup, and an error, if there is any.
func (c *FakeSecurityGroups) Create(ctx context.Context, securityGroup *v2alpha1.SecurityGroup, opts v1.CreateOptions) (result *v2alpha1.SecurityGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(securitygroupsResource, securityGroup), &v2alpha1.SecurityGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha1.SecurityGroup), err
}

// Update takes the representation of a securityGroup and updates it. Returns the server's representation of the securityGroup, and an error, if there is any.
func (c *FakeSecurityGroups) Update(ctx context.Context, securityGroup *v2alpha1.SecurityGroup, opts v1.UpdateOptions) (result *v2alpha1.SecurityGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(securitygroupsResource, securityGroup), &v2alpha1.SecurityGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha1.SecurityGroup), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeSecurityGroups) UpdateStatus(ctx context.Context, securityGroup *v2alpha1.SecurityGroup, opts v1.UpdateOptions) (*v2alpha1.SecurityGroup, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(securitygroupsResource, "status", securityGroup), &v2alpha1.SecurityGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha1.SecurityGroup), err
}

// Delete takes name of the securityGroup and deletes it. Returns an error if one occurs.
func (c *FakeSecurityGroups) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(securitygroupsResource, name, opts), &v2alpha1.SecurityGroup{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeSecurityGroups) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(securitygroupsResource, listOpts)

	_, err := c.Fake.Invokes(action, &v2alpha1.SecurityGroupList{})
	return err
}

// Patch applies the patch and returns the patched securityGroup.
func (c *FakeSecurityGroups) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v2alpha1.SecurityGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(securitygroupsResource, name, pt, data, subresources...), &v2alpha1.SecurityGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha1.SecurityGroup), err
}
