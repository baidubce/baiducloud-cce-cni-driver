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
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/limit"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// bccNode is a wrapper of Node, which is used to distinguish bcc node
type bbcNode struct {
	*bceNode

	// bbceni is the eni of the node
	bbceni *ccev2.ENI
}

func newBBCNode(super *bceNode) *bbcNode {
	node := &bbcNode{
		bceNode: super,
	}
	node.instanceType = string(metadata.InstanceTypeExBCC)
	err := node.createBBCENI(super.log)
	if err != nil {
		super.log.Errorf("failed to create bbc eni: %v", err)
	}
	return node
}

// createBBCENI means create a eni object for bbc node
// bbc node has only one eni, so we use bbc instance id as eni name
func (n *bbcNode) createBBCENI(scopedLog *logrus.Entry) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	if n.bbceni != nil {
		return nil
	}
	bbceni, err := n.manager.bceclient.GetBBCInstanceENI(context.Background(), n.instanceID)
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to get instance bbc eni")
		return err
	}
	scopedLog.WithField("bbceni", logfields.Repr(bbceni)).Debugf("get instance bbc eni success")

	_, err = bcesync.EnsureSubnet(bbceni.VpcId, bbceni.SubnetId)
	if err != nil {
		scopedLog.Errorf("failed to ensure subnet: %v", err)
		return err
	}

	var (
		ipv4IPSet, ipv6IPSet []*models.PrivateIP
		ctx                  = context.Background()
	)

	for _, v := range bbceni.PrivateIpSet {
		ipv4IPSet = append(ipv4IPSet, &models.PrivateIP{
			PrivateIPAddress: v.PrivateIpAddress,
			PublicIPAddress:  v.PublicIpAddress,
			SubnetID:         v.SubnetId,
			Primary:          v.Primary,
		})

		// bbc eni support ipv6
		if v.Ipv6Address != "" {
			ipv6IPSet = append(ipv6IPSet, &models.PrivateIP{
				PrivateIPAddress: v.Ipv6Address,
				SubnetID:         v.SubnetId,
				Primary:          v.Primary,
			})
		}
	}

	eni, err := n.manager.enilister.Get(bbceni.Id)
	if errors.IsNotFound(err) {
		eni = &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				// use bbc instance id as eni name
				Name: bbceni.Id,
				Labels: map[string]string{
					k8s.LabelInstanceID: n.instanceID,
					k8s.LabelNodeName:   n.k8sObj.Name,
					k8s.LabelENIType:    string(ccev2.ENIForBBC),
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
				Type:     ccev2.ENIForBBC,
				UseMode:  ccev2.ENIUseModeSecondaryIP,
				ENI: models.ENI{
					ID:               bbceni.Id,
					Name:             bbceni.Name,
					SubnetID:         bbceni.SubnetId,
					VpcID:            bbceni.VpcId,
					ZoneName:         bbceni.ZoneName,
					InstanceID:       n.instanceID,
					PrivateIPSet:     ipv4IPSet,
					IPV6PrivateIPSet: ipv6IPSet,
					MacAddress:       bbceni.MacAddress,
				},
			},
			Status: ccev2.ENIStatus{},
		}
		eni, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bbc ENI: %w", err)
		}
		scopedLog.Infof("create bbc ENI resource successed")
	} else if err != nil {
		scopedLog.Errorf("failed to get bbc ENI resource: %v", err)
		return err
	}
	scopedLog.Debugf("got bbc ENI resource successed")
	n.bbceni = eni
	return err
}

func (n *bbcNode) calculateLimiter(scopeLog *logrus.Entry) (limit.IPResourceManager, error) {
	scopeLog = scopeLog.WithField("nodeName", n.k8sObj.Name).WithField("method", "generateIPResourceManager")
	client := k8s.WatcherClient()
	if client == nil {
		scopeLog.Fatal("K8s client is nil")
	}
	k8sNode, err := client.Informers.Core().V1().Nodes().Lister().Get(n.k8sObj.Name)
	if err != nil {
		scopeLog.Errorf("Get node failed: %v", err)
		return nil, err
	}

	resourceManger := limit.NewBBCIPResourceManager(client, k8sNode)
	n.capacity = resourceManger.CalaculateCapacity()
	return resourceManger, nil
}

// allocateIPs implements realNodeInf
func (n *bbcNode) allocateIPs(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	var ips *bbc.BatchAddIpResponse
	if ipv4ToAllocate > 0 {
		// allocate ip
		ips, err = n.manager.bceclient.BBCBatchAddIP(ctx, &bbc.BatchAddIpArgs{
			InstanceId:                     n.instanceID,
			SecondaryPrivateIpAddressCount: ipv4ToAllocate,
		})
		err = n.manager.HandlerVPCError(scopedLog, err, string(allocation.PoolID))
		if err != nil {
			return nil, nil, fmt.Errorf("allocate ip to bbc eni %s failed: %v", allocation.InterfaceID, err)
		}
		scopedLog.WithField("ips", ips).Debug("allocate ip to bbc eni success")

		for _, ipstring := range ips.PrivateIps {
			ipv4PrivateIPSet = append(ipv4PrivateIPSet, &models.PrivateIP{
				PrivateIPAddress: ipstring,
				SubnetID:         string(allocation.PoolID),
			})
		}
	}

	// TODO: bbc not support allocate ipv6

	return
}

// createInterface implements realNodeInf
func (n *bbcNode) createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	err = n.createBBCENI(scopedLog)
	if err != nil {
		return 0, "", err
	}
	return 1, "", nil
}

// releaseIPs implements realNodeInf
func (n *bbcNode) releaseIPs(ctx context.Context, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	if len(ipv4ToRelease) > 0 {
		err := n.manager.bceclient.BBCBatchDelIP(ctx, &bbc.BatchDelIpArgs{
			InstanceId: n.instanceID,
			PrivateIps: ipv4ToRelease,
		})
		if err != nil {
			return fmt.Errorf("release ip from bbc eni %s failed: %v", n.instanceID, err)
		}
	}
	return nil
}

// PrepareIPAllocation is called to calculate the number of IPs that
// can be allocated on the node and whether a new network interface
// must be attached to the node.
func (n *bbcNode) prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	// Calculate the number of IPs that can be allocated on the node
	allocation := &ipam.AllocationAction{}
	findEni := false
	if n.capacity != nil {
		n.manager.ForeachInstance(n.instanceID, func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}

			findEni = true
			allocation.AvailableForAllocationIPv4 = n.capacity.MaxIPPerENI - len(e.Spec.PrivateIPSet)
			allocation.InterfaceID = e.Name
			allocation.PoolID = ipamTypes.PoolID(e.Spec.SubnetID)
			return nil
		})
	}
	if !findEni {
		return nil, fmt.Errorf("can not find eni for bbc instance %s", n.instanceID)
	}

	return allocation, nil
}

// GetMaximumAllocatable implements realNodeInf
func (*bbcNode) getMaximumAllocatable(capacity *limit.NodeCapacity) int {
	return capacity.MaxIPPerENI - 1
}

// GetMinimumAllocatable implements realNodeInf
func (n *bbcNode) getMinimumAllocatable() int {
	min := n.k8sObj.Spec.IPAM.MinAllocate
	if min == 0 {
		min = defaults.IPAMPreAllocation
	}
	return min
}

// AllocateIPCrossSubnet implements realNodeInf
func (*bbcNode) allocateIPCrossSubnet(ctx context.Context, sbnID string) ([]*models.PrivateIP, string, error) {
	return nil, "", fmt.Errorf("bbc not support cross subnet")
}

// ReuseIPs implements realNodeInf
func (*bbcNode) reuseIPs(ctx context.Context, ips []*models.PrivateIP, Owner string) (string, error) {
	return "", fmt.Errorf("bbc not support cross subnet")
}

var _ realNodeInf = &bbcNode{}

// AllocateIP implements endpoint.DirectEndpointOperation
func (*bccNode) AllocateIP(ctx context.Context, action *endpoint.DirectIPAction) error {
	return fmt.Errorf("bbc not support direct allocate ip")
}

// DeleteIP implements endpoint.DirectEndpointOperation
func (*bccNode) DeleteIP(ctx context.Context, allocation *endpoint.DirectIPAction) error {
	return fmt.Errorf("bbc not support direct delete ip")
}
