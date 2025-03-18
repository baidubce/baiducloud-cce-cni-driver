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
package endpoint

import (
	"context"

	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/sirupsen/logrus"
)

type reuseIPAllocatorProvider struct {
	*EndpointManager
}

// AllocateIP implements EndpointMunalAllocatorProvider
func (provider *reuseIPAllocatorProvider) AllocateIP(ctx context.Context, log *logrus.Entry, resource *v2.CCEEndpoint) error {
	var (
		owner      = resource.Namespace + "/" + resource.Name
		newStatus  = &resource.Status
		allocation = &DirectIPAction{
			NodeName: resource.Spec.Network.IPAllocation.NodeName,
			Owner:    owner,
		}
	)
	operation, err := provider.directIPAllocator.NodeEndpoint(resource)
	if err != nil {
		log.Errorf("failed to get node endpoint %v", err)
		return err
	}
	// allocate new IP
	if len(newStatus.Networking.Addressing) > 0 {
		// reuse IP
		allocation.Addressing = newStatus.Networking.Addressing
		allocation.SubnetID = newStatus.Networking.Addressing[0].Subnet
		allocation.Interface = newStatus.Networking.Addressing[0].Interface
	}
	err = operation.AllocateIP(ctx, allocation)
	if err != nil {
		log.Errorf("allocated fixed error %v", err)
		return err
	}
	newStatus.Networking.Addressing = allocation.Addressing
	newStatus.NodeSelectorRequirement = allocation.NodeSelectorRequirement

	return nil
}

var _ EndpointMunalAllocatorProvider = &reuseIPAllocatorProvider{}
