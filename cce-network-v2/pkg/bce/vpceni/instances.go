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
package vpceni

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listv1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
	listv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
)

// InstancesManager maintains the list of instances. It must be kept up to date
// by calling resync() regularly.
type InstancesManager struct {
	mutex lock.RWMutex

	nrsGetterUpdater ipam.NetResourceSetGetterUpdater

	bceNetworkResourceSetMap map[string]*bceNetworkResourceSet

	// client to get k8s objects
	enilister listv2.ENILister
	sbnlister listv1.SubnetLister
	cepClient *watchers.CCEEndpointUpdaterImpl

	// bceClient to get bce objects
	bceclient cloud.Interface
	// tmp cache with bce

}

// newInstancesManager returns a new instances manager
func newInstancesManager(
	bceclient cloud.Interface,
	enilister listv2.ENILister, sbnlister listv1.SubnetLister,
	cepClient *watchers.CCEEndpointUpdaterImpl) *InstancesManager {

	return &InstancesManager{
		bceNetworkResourceSetMap: make(map[string]*bceNetworkResourceSet),

		enilister: enilister,
		sbnlister: sbnlister,
		bceclient: bceclient,
		cepClient: cepClient,
	}
}

func (m *InstancesManager) HPASWrapper() error {
	return m.bceclient.HPASWrapper(context.Background())
}

// CreateNetResource is called when the IPAM layer has learned about a new
// node which requires IPAM services. This function must return a
// NodeOperations implementation which will render IPAM services to the
// node context provided.
func (m *InstancesManager) CreateNetResource(obj *ccev2.NetResourceSet, node *ipam.NetResource) ipam.NetResourceOperations {
	bceNrs := NewBCENetworkResourceSet(node, obj, m)

	m.mutex.Lock()
	m.bceNetworkResourceSetMap[bceNrs.k8sObj.Name] = bceNrs
	m.mutex.Unlock()
	return bceNrs
}

// GetPoolQuota returns the number of available IPs in all IP pools
func (m *InstancesManager) GetPoolQuota() ipamTypes.PoolQuotaMap {
	pool := ipamTypes.PoolQuotaMap{}
	subnets, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().List(labels.Everything())
	if err != nil {
		return pool
	}
	for _, subnet := range subnets {
		pool[ipamTypes.PoolID(subnet.Name)] = ipamTypes.PoolQuota{
			AvailabilityZone: subnet.Spec.AvailabilityZone,
			AvailableIPs:     subnet.Status.AvailableIPNum,
		}
	}
	return pool
}

// HandlerVPCError handles the error returned by the VPC API
func (m *InstancesManager) HandlerVPCError(scopedLog *logrus.Entry, vpcError error, subnetID string) (retError error) {
	retError = vpcError
	if cloud.IsErrorSubnetHasNoMoreIP(vpcError) {
		scopedLog.WithError(vpcError).Error("subnet has no more IP")
		// update subnet status
		subnet, err := m.sbnlister.Get(subnetID)
		if err != nil {
			scopedLog.WithError(err).Error("failed to get subnet")
			return
		}
		subnet.Status.AvailableIPNum = 0
		_, err = k8s.CCEClient().CceV1().Subnets().UpdateStatus(context.TODO(), subnet, metav1.UpdateOptions{})
		if err != nil {
			scopedLog.WithError(err).Error("failed to update subnet status")
			return
		}
	}

	if cloud.IsErrorVmMemoryCanNotAttachMoreIpException(vpcError) {
		// TODO: we should to release the ip which is not used
	}
	return
}

// Resync is called periodically to give the IPAM implementation a
// chance to resync its own state with external APIs or systems. It is
// also called when the IPAM layer detects that state got out of sync.
func (m *InstancesManager) Resync(ctx context.Context) time.Time {
	return time.Now()
}

// NodeEndpoint implements endpoint.DirectIPAllocator
func (m *InstancesManager) NodeEndpoint(cep *ccev2.CCEEndpoint) (endpoint.DirectEndpointOperation, error) {
	nodeName := cep.Spec.Network.IPAllocation.NodeName
	_, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().Get(nodeName)
	if err != nil {
		return nil, err
	}

	m.mutex.Lock()
	bceNrs, ok := m.bceNetworkResourceSetMap[nodeName]
	m.mutex.Unlock()
	if !ok {
		return nil, errors.NewNotFound(v2.Resource("netresourceset"), nodeName)
	}
	return bceNrs, nil
}

// ResyncPool implements endpoint.DirectIPAllocator
func (*InstancesManager) ResyncPool(ctx context.Context, scopedLog *logrus.Entry) (map[string]ipamTypes.AllocationMap, error) {
	panic("unimplemented")
}

var (
	_ ipam.AllocationImplementation = &InstancesManager{}
	_ endpoint.DirectIPAllocator    = &InstancesManager{}
)
