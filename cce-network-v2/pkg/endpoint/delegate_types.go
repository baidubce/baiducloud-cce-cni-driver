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

	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccelister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// CCEEndpointGetterUpdater defines the interface used to interact with the k8s
// apiserver to retrieve and update the CCEEndpoint custom resource
type CCEEndpointGetterUpdater interface {
	Create(node *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error)
	Update(ewResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error)
	UpdateStatus(newResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error)
	Delete(namespace, name string) error
	Lister() ccelister.CCEEndpointLister
}

type DirectIPAction struct {
	// NodeName is identifier describing the ip on which node
	NodeName string

	Owner string

	// IP4/6 addresses assigned to this Endpoint
	// this field used to dynamic allocate ip
	// +optional
	SubnetID string

	// IP4/6 addresses assigned to this Endpoint
	Addressing ccev2.AddressPairList

	// Interface is the identifier describing the ip on which interface
	// this field used as param to release ip
	// +optional
	Interface string

	// A list of node selector requirements by node's labels.
	// +optional
	NodeSelectorRequirement []corev1.NodeSelectorRequirement `json:"matchExpressions,omitempty" protobuf:"bytes,1,rep,name=matchExpressions"`
}

// DirectIPAllocator is the interface an IPAM implementation must provide in order
// to provide IP allocation for a node. The structure implementing this API
// *must* be aware of the node connected to this implementation. This is
// achieved by considering the node context provided in
// AllocationImplementation.CreateNetResource() function and returning a
// NodeOperations implementation which performs operations in the context of
// that node.
type DirectIPAllocator interface {
	// ResyncPool is called to synchronize the latest list of interfaces and IPs
	ResyncPool(ctx context.Context, scopedLog *logrus.Entry) (map[string]ipamTypes.AllocationMap, error)

	// NodeEndpoint returns the NodeEndpointOperation
	NodeEndpoint(cep *ccev2.CCEEndpoint) (DirectEndpointOperation, error)
}

type DirectEndpointOperation interface {
	// FilterAvailableSubnet returns the best subnet for the endpoint
	FilterAvailableSubnet([]*ccev1.Subnet) []*ccev1.Subnet

	// AllocateIP allocates an IP for the endpoint
	// The meaning of reusing the IP address is to migrate the IP
	// address to be used by the pod from one node to another
	// if action.addressing is not nil, it means that the IP address is reused
	// if action.addressing is nil, it means that the IP address is newly allocated
	// Note: This method will be called repeatedly, and the implementer must support reentrant calls
	AllocateIP(ctx context.Context, action *DirectIPAction) error

	// DeleteIP When the fixed IP pod is recycled and the IP is no longer reused,
	// for example, the workload is permanently deleted. At this time, the fixed
	// the implement should only delete the IP address while its owner is euqal to action.owner
	// IP that should be retained should be recycled.
	DeleteIP(ctx context.Context, allocation *DirectIPAction) error
}

// DirectAllocatorStarter is the interface an IPAM implementation must provide in order
type DirectAllocatorStarter interface {
	StartEndpointManager(ctx context.Context, getterUpdater CCEEndpointGetterUpdater) (EndpointEventHandler, error)
}
