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
package rdma

import (
	"context"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listv1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
	listv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
)

// rdmaInstancesManager maintains the list of instances. It must be kept up to date
// by calling resync() regularly.
type rdmaInstancesManager struct {
	mutex lock.RWMutex

	nrsGetterUpdater ipam.NetResourceSetGetterUpdater

	nodeMap map[string]*bceRDMANetResourceSet

	// client to get k8s objects
	eniLister listv2.ENILister
	sbnLister listv1.SubnetLister
	cepClient *watchers.CCEEndpointUpdaterImpl

	// bceClient to get bce objects
	bceClient cloud.Interface
	// tmp cache with bce

	//iaasClient client.IaaSClient
	eriClient *client.EriClient
	hpcClient *client.HpcClient
}

// newInstancesManager returns a new instances manager
func newInstancesManager(
	bceClient cloud.Interface,
	eriClient *client.EriClient,
	roceClient *client.HpcClient,
	eniLister listv2.ENILister, sbnLister listv1.SubnetLister,
	cepClient *watchers.CCEEndpointUpdaterImpl) *rdmaInstancesManager {

	return &rdmaInstancesManager{
		nodeMap: make(map[string]*bceRDMANetResourceSet),

		eniLister: eniLister,
		sbnLister: sbnLister,
		bceClient: bceClient,
		eriClient: eriClient,
		hpcClient: roceClient,
		cepClient: cepClient,
	}
}

// getIaaSClient returns an IaaSClient interface based on the intType parameter
func (m *rdmaInstancesManager) getIaaSClient(intType string) client.IaaSClient {
	if strings.EqualFold(intType, m.eriClient.GetRDMAIntType()) {
		return m.eriClient
	}
	return m.hpcClient
}

// CreateNetResource is called when the IPAM layer has learned about a new
// node which requires IPAM services. This function must return a
// NodeOperations implementation which will render IPAM services to the
// node context provided.
func (m *rdmaInstancesManager) CreateNetResource(obj *ccev2.NetResourceSet, node *ipam.NetResource) ipam.NetResourceOperations {
	np := NewNetResourceSet(node, obj, m)

	m.mutex.Lock()
	m.nodeMap[np.k8sObj.Name] = np
	m.mutex.Unlock()
	return np
}

// GetPoolQuota returns the number of available IPs in all IP pools
func (m *rdmaInstancesManager) GetPoolQuota() ipamTypes.PoolQuotaMap {
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

// FindSubnetByIDs returns the subnet with the most addresses matching VPC ID,
// availability zone within a provided list of subnet ids
//
// The returned subnet is immutable so it can be safely accessed
func (m *rdmaInstancesManager) FindSubnetByIDs(vpcID, availabilityZone string, subnetIDs []string) (bestSubnet *ccev1.Subnet) {
	for _, subnetID := range subnetIDs {
		sbn, err := bcesync.EnsureSubnet(vpcID, subnetID)
		if err != nil {
			continue
		}
		if !strings.Contains(sbn.Spec.AvailabilityZone, availabilityZone) {
			continue
		}
		// filter out ipv6 subnet if ipv6 is not enabled
		if operatorOption.Config.EnableIPv6 {
			if sbn.Spec.IPv6CIDR == "" {
				continue
			}
		}

		if sbn.Spec.Exclusive {
			continue
		}
		if bestSubnet == nil || bestSubnet.Status.AvailableIPNum < sbn.Status.AvailableIPNum {
			bestSubnet = sbn
		}

	}
	return
}

// HandlerVPCError handles the error returned by the VPC API
func (m *rdmaInstancesManager) HandlerVPCError(scopedLog *logrus.Entry, vpcError error, subnetID string) (retError error) {
	retError = vpcError
	if cloud.IsErrorSubnetHasNoMoreIP(vpcError) {
		scopedLog.WithError(vpcError).Error("subnet has no more IP")
		// update subnet status
		subnet, err := m.sbnLister.Get(subnetID)
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
func (m *rdmaInstancesManager) Resync(ctx context.Context) time.Time {
	return time.Now()
}

var (
	_ ipam.AllocationImplementation = &rdmaInstancesManager{}
)
