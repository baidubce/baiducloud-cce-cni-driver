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
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma/client"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
)

const (
	defaltRdmaEniNums       = 1
	defaultRdmaMaxIPsPerENI = 13
)

// rdmaNetResourceSetWrapper is a wrapper of NetResourceSet, which is used to distinguish no-RDMA NetResourceSet
type rdmaNetResourceSetWrapper struct {
	*bceRDMANetResourceSet

	// rdmaeni is the eni of the node
	rdmaeni *ccev2.ENI
}

func newRdmaNetResourceSetWrapper(super *bceRDMANetResourceSet) *rdmaNetResourceSetWrapper {
	node := &rdmaNetResourceSetWrapper{
		bceRDMANetResourceSet: super,
	}
	node.instanceType = string(metadata.InstanceTypeExEHC)
	err := node.createRdmaENI(super.log)
	if err != nil {
		super.log.Errorf("failed to create eri or hpc eni: %v", err)
	}
	return node
}

func (n *rdmaNetResourceSetWrapper) tryRefreshRDMAENI() *ccev2.ENI {
	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name, func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
		e, ok := iface.Resource.(*eniResource)
		if !ok {
			return nil
		}
		n.rdmaeni = &ccev2.ENI{
			TypeMeta:   e.TypeMeta,
			ObjectMeta: e.ObjectMeta,
			Spec:       e.Spec,
			Status:     e.Status,
		}
		return nil
	})
	return n.rdmaeni
}

// find eni by mac address, return matched eni.
func (n *rdmaNetResourceSetWrapper) findMatchedEniByMac(ctx context.Context, iaasClient client.IaaSClient,
	vpcID, instanceID, vifFeatures, macAddress string) (*client.EniResult, error) {
	log.Infof("start to find suitable %s eni by mac for instanceID %v/%v", vifFeatures, instanceID, macAddress)
	eniList, listErr := iaasClient.ListEnis(ctx, vpcID, instanceID)
	if listErr != nil {
		log.Errorf("failed to get %s eni: %v", vifFeatures, listErr)
		return nil, listErr
	}

	for index := range eniList {
		eniInfo := eniList[index]
		if strings.EqualFold(eniInfo.MacAddress, macAddress) {
			return &eniInfo, nil
		}
	}

	log.Errorf("macAddress %s mismatch, eniList: %v", macAddress, eniList)
	return nil, fmt.Errorf("macAddress %s mismatch, eniList: %v", macAddress, eniList)
}

// createRdmaENI means create a eni object for rdma interface
// rdma interface has only one eni, so we use rdma interface id as eni name
func (n *rdmaNetResourceSetWrapper) createRdmaENI(scopedLog *logrus.Entry) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	if n.rdmaeni != nil {
		return nil
	}

	// the hpc or eri api do not use vpcID, subnetID and zoneName
	vpcID := n.k8sObj.Spec.ENI.VpcID
	// the macAddress and vifFeatures is decided by the NetResourceSet's annotation
	macAddress := n.bceRDMANetResourceSet.k8sObj.Annotations[k8s.AnnotationRDMAInfoMacAddress]
	vifFeatures := n.bceRDMANetResourceSet.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]

	iaasClient := n.manager.getIaaSClient(vifFeatures)
	rdmaEni, err := n.findMatchedEniByMac(context.Background(), iaasClient, vpcID, n.instanceID, vifFeatures, macAddress)
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to get instance %s eni", vifFeatures)
		return err
	}
	scopedLog.WithField("rdmaeni", logfields.Repr(rdmaEni)).Debugf("get instance %s eni success", vifFeatures)

	// the hpc or eri do not use ensure subnet object

	var (
		ipv4IPSet, ipv6IPSet []*models.PrivateIP
		ctx                  = context.Background()
	)

	for _, v := range rdmaEni.PrivateIpSet {
		ipv4IPSet = append(ipv4IPSet, &models.PrivateIP{
			PrivateIPAddress: v.PrivateIpAddress,
			PublicIPAddress:  "",
			SubnetID:         rdmaEni.SubnetID,
			Primary:          v.Primary,
		})

		// rdma eni is not support ipv6
	}

	eni, err := n.manager.eniLister.Get(rdmaEni.Id)
	if errors.IsNotFound(err) {
		eni = &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				// use rdma interface id as eni name
				Name: rdmaEni.Id,
				Labels: map[string]string{
					k8s.LabelInstanceID: n.instanceID,
					k8s.LabelNodeName:   n.k8sObj.Name,
					k8s.LabelENIType:    iaasClient.GetRDMAIntType(),
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: ccev2.SchemeGroupVersion.String(),
					Kind:       ccev2.NRSKindDefinition,
					Name:       n.k8sObj.Name,
					UID:        n.k8sObj.UID,
				}},
			},
			Spec: ccev2.ENISpec{
				NodeName: n.k8sObj.Name,
				Type:     ccev2.ENIType(rdmaEni.Type),
				UseMode:  ccev2.ENIUseModeSecondaryIP,
				ENI: models.ENI{
					ID:               rdmaEni.Id,
					Name:             rdmaEni.Id, // RDMA ENI name is replaced by RDMA eni id
					SubnetID:         rdmaEni.SubnetID,
					VpcID:            vpcID,
					ZoneName:         rdmaEni.ZoneName,
					InstanceID:       n.instanceID,
					PrivateIPSet:     ipv4IPSet,
					IPV6PrivateIPSet: ipv6IPSet,
					MacAddress:       rdmaEni.MacAddress,
				},
			},
			Status: ccev2.ENIStatus{},
		}
		eni, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create %s ENI: %w", vifFeatures, err)
		}
		scopedLog.Infof("create %s ENI resource successed", vifFeatures)
	} else if err != nil {
		scopedLog.Errorf("failed to get %s ENI resource: %v", vifFeatures, err)
		return err
	}
	scopedLog.Debugf("got %s ENI resource successed", vifFeatures)
	n.rdmaeni = eni
	return err
}

func (n *rdmaNetResourceSetWrapper) refreshBCCInfo() error {
	if n.bccInfo != nil {
		return nil
	}
	bccInfo, err := n.manager.bceClient.GetBCCInstanceDetail(context.TODO(), n.instanceID)
	if err != nil {
		n.log.Errorf("faild to get bcc instance detail: %v", err)
		return err
	}
	n.log.WithField("bccInfo", logfields.Repr(bccInfo)).Infof("Get bcc instance detail")
	n.bccInfo = bccInfo

	return nil
}

func (n *rdmaNetResourceSetWrapper) refreshENIQuota(scopeLog *logrus.Entry) (RdmaEniQuotaManager, error) {
	scopeLog = scopeLog.WithField("netResourceSetName", n.k8sObj.Name).WithField("method", "generateRdmaIpResourceManager")
	client := k8s.WatcherClient()
	if client == nil {
		scopeLog.Fatal("K8s client is nil")
	}
	nodeName := bceutils.GetNodeNameFromNetResourceSetName(n.k8sObj.Name)
	k8sNode, err := client.Informers.Core().V1().Nodes().Lister().Get(nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s node %s: %v", n.k8sObj.Name, err)
	}

	err = n.refreshBCCInfo()
	if err != nil {
		return nil, err
	}
	// if bcc instance is not created by cce-network-v2, there is no need to check IP resouce
	if n.bccInfo == nil {
		return nil, fmt.Errorf("bcc info instance is nil")
	}

	// default rdma ip quota
	eniQuota := newCustomerIPQuota(scopeLog, client, k8sNode, n.instanceID, n.manager.bceClient)

	// Expect all BCC models to support ERI
	if n.bccInfo.EriQuota != 0 {
		eniQuota.SetMaxENI(n.bccInfo.EriQuota)
		if n.bccInfo.EriQuota == 0 {
			eniQuota.SetMaxENI(defaltRdmaEniNums)
		}
	}
	eniQuota.SetMaxIP(defaultRdmaMaxIPsPerENI)

	return eniQuota, nil
}

// allocateIPs implements realNodeInf
func (n *rdmaNetResourceSetWrapper) allocateIPs(ctx context.Context, scopedLog *logrus.Entry, iaasClient client.IaaSClient, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	if ipv4ToAllocate > 0 {
		// allocate ips
		ips, err := iaasClient.BatchAddPrivateIP(ctx, n.rdmaeni.Spec.ID, []string{}, ipv4ToAllocate)
		err = n.manager.HandlerVPCError(scopedLog, err, string(allocation.PoolID))
		if err != nil {
			if len(ips) == 0 {
				return nil, nil, fmt.Errorf("allocate ips to rdma eni %s failed: %v", allocation.InterfaceID, err)
			}
			scopedLog.Errorf("allocate ips to rdma eni %s failed: %v", allocation.InterfaceID, err)
		}
		scopedLog.WithField("ips", ips).Debugf("allocate %d ips to rdma eni success", len(ips))

		for _, ipstring := range ips {
			ipv4PrivateIPSet = append(ipv4PrivateIPSet, &models.PrivateIP{
				PrivateIPAddress: ipstring,
				SubnetID:         string(allocation.PoolID),
			})
		}
	}

	// TODO: rdma not support allocate ipv6

	return
}

// createInterface implements realNodeInf
func (n *rdmaNetResourceSetWrapper) createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	err = n.createRdmaENI(scopedLog)
	if err != nil {
		return 0, "", err
	}
	return 1, "", nil
}

// releaseIPs implements realNodeInf
func (n *rdmaNetResourceSetWrapper) releaseIPs(ctx context.Context, iaasClient client.IaaSClient, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	if len(ipv4ToRelease) > 0 {
		// release ips
		err := iaasClient.BatchDeletePrivateIP(ctx, n.rdmaeni.Spec.ID, ipv4ToRelease)
		if err != nil {
			return fmt.Errorf("release the ips(%s) from hpc eni %s failed: %v", ipv4ToRelease, n.instanceID, err)
		}
	}
	return nil
}

// PrepareIPAllocation is called to calculate the number of IPs that
// can be allocated on the node and whether a new network interface
// must be attached to the node.
func (n *rdmaNetResourceSetWrapper) prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	// Calculate the number of IPs that can be allocated on the node
	allocation := &ipam.AllocationAction{}

	if n.tryRefreshRDMAENI() == nil {
		allocation.AvailableInterfaces = 1
		return allocation, nil
	}

	findEni := false
	rdmaEniQuota := n.getRdmaEniQuota()
	if rdmaEniQuota != nil {
		n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name, func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}

			findEni = true
			allocation.AvailableForAllocationIPv4 = rdmaEniQuota.GetMaxIP() - len(e.Spec.PrivateIPSet)
			allocation.InterfaceID = e.Name
			allocation.PoolID = ipamTypes.PoolID(e.Spec.SubnetID)

			if n.k8sObj.Spec.ENI.InstanceType == bceutils.OverlayRDMA {
				sbn, err := n.manager.sbnLister.Get(string(allocation.PoolID))
				if err != nil {
					err = fmt.Errorf("get subnet %s failed: %v", e.Spec.SubnetID, err)
					n.appendAllocatedIPError(e.Name, ccev2.NewErrorStatusChange(err.Error()))
					return err
				}

				allocation.AvailableForAllocationIPv4 = math.IntMin(allocation.AvailableForAllocationIPv4, sbn.Status.AvailableIPNum)
			} else {
				allocation.AvailableForAllocationIPv4 = math.IntMin(allocation.AvailableForAllocationIPv4, n.getMaximumAllocatable(rdmaEniQuota))
			}
			return nil
		})

	}

	if !findEni {
		return nil, fmt.Errorf("can not find eri for hpc instance %s", n.instanceID)
	}

	return allocation, nil
}

// GetMaximumAllocatable implements realNodeInf
func (*rdmaNetResourceSetWrapper) getMaximumAllocatable(eniQuota RdmaEniQuotaManager) int {
	return eniQuota.GetMaxIP() - 1
}

// GetMinimumAllocatable implements realNodeInf
func (n *rdmaNetResourceSetWrapper) getMinimumAllocatable() int {
	min := n.k8sObj.Spec.IPAM.MinAllocate
	if min == 0 {
		min = defaults.IPAMPreAllocation
	}
	return min
}

// AllocateIPCrossSubnet implements realNodeInf
func (*rdmaNetResourceSetWrapper) allocateIPCrossSubnet(ctx context.Context, sbnID string) ([]*models.PrivateIP, string, error) {
	return nil, "", fmt.Errorf("rdma not support cross subnet")
}

// ReuseIPs implements realNodeInf
func (*rdmaNetResourceSetWrapper) reuseIPs(ctx context.Context, ips []*models.PrivateIP, Owner string) (string, error) {
	return "", fmt.Errorf("rdma not support cross subnet")
}

var _ realNetResourceSetInf = &rdmaNetResourceSetWrapper{}

// AllocateIP implements endpoint.DirectEndpointOperation
func (*rdmaNetResourceSetWrapper) AllocateIP(ctx context.Context, action *endpoint.DirectIPAction) error {
	return fmt.Errorf("rdma not support direct allocate ip")
}

// DeleteIP implements endpoint.DirectEndpointOperation
func (*rdmaNetResourceSetWrapper) DeleteIP(ctx context.Context, allocation *endpoint.DirectIPAction) error {
	return fmt.Errorf("rdma not support direct delete ip")
}
