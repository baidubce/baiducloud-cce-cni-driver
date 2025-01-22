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

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	"github.com/baidubce/bce-sdk-go/services/bbc"
)

const (
	defaultBBCMaxIPsPerENI = 40
)

// bbcNetworkResourceSet is a wrapper of Node, which is used to distinguish bcc node
type bbcNetworkResourceSet struct {
	*bceNetworkResourceSet

	primaryENISubnetID string
	// bbceni is the eni of the node
	bbceni *ccev2.ENI
}

func newBBCNetworkResourceSet(super *bceNetworkResourceSet) *bbcNetworkResourceSet {
	node := &bbcNetworkResourceSet{
		bceNetworkResourceSet: super,
	}
	node.instanceType = string(metadata.InstanceTypeExBBC)
	node.tryRefreshBBCENI()
	return node
}

func (n *bbcNetworkResourceSet) tryRefreshBBCENI() *ccev2.ENI {
	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name, func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
		e, ok := iface.Resource.(*eniResource)
		if !ok {
			return nil
		}
		n.bbceni = &ccev2.ENI{
			TypeMeta:   e.TypeMeta,
			ObjectMeta: e.ObjectMeta,
			Spec:       e.Spec,
			Status:     e.Status,
		}
		n.primaryENISubnetID = e.Spec.SubnetID
		return nil
	})
	return n.bbceni
}

// createBBCENI means create a eni object for bbc node
// bbc node has only one eni, so we use bbc instance id as eni name
func (n *bbcNetworkResourceSet) createBBCENI(scopedLog *logrus.Entry) error {
	if n.bbceni != nil {
		return nil
	}
	bbceni, err := n.manager.bceclient.GetBBCInstanceENI(context.Background(), n.instanceID)
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to get instance bbc eni")
		return err
	}
	scopedLog.WithField("bbceni", logfields.Repr(bbceni)).Infof("get instance bbc eni success")
	err = n.refreshAvailableSubnets()
	if err != nil {
		n.appendAllocatedIPError(bbceni.Id, ccev2.NewCustomerErrorStatusChange(ccev2.ErrorCodeNoAvailableSubnet, "failed to refresh available subnets"))
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
				UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
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
				RouteTableOffset:          n.k8sObj.Spec.ENI.RouteTableOffset,
				InstallSourceBasedRouting: false,
			},
			Status: ccev2.ENIStatus{},
		}

		err = n.tryBorrowIPs(eni)
		if err != nil {
			return err
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

	if eni.Status.VPCStatus != ccev2.VPCENIStatusInuse {
		(&eni.Status).AppendVPCStatus(ccev2.VPCENIStatusInuse)
		_, err = k8s.CCEClient().CceV2().ENIs().UpdateStatus(ctx, eni, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update bbc ENI status: %w", err)
		}
		scopedLog.Infof("update bbc ENI status to inuse successed")
	}
	n.mutex.Lock()
	n.bbceni = eni
	n.primaryENISubnetID = bbceni.SubnetId
	n.mutex.Unlock()
	return n.updateNrsSubnetIfNeed([]string{bbceni.SubnetId})
}

func (n *bbcNetworkResourceSet) refreshENIQuota(scopeLog *logrus.Entry) (ENIQuotaManager, error) {
	scopeLog = scopeLog.WithField("nodeName", n.k8sObj.Name).WithField("method", "generateIPResourceManager")
	// default bbc ip quota
	eniQuota := newCustomerIPQuota(scopeLog, k8s.WatcherClient(), n.k8sObj.Name, n.instanceID, n.manager.bceclient)
	eniQuota.SetMaxENI(1)
	eniQuota.SetMaxIP(defaultBBCMaxIPsPerENI)

	return eniQuota, nil
}

// allocateIPs implements realNodeInf
func (n *bbcNetworkResourceSet) allocateIPs(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	var ips *bbc.BatchAddIpResponse
	if ipv4ToAllocate > 0 {
		// allocate ip to bbc eni
		if allocation.PoolID == ipamTypes.PoolID(n.primaryENISubnetID) {
			ips, err = n.manager.bceclient.BBCBatchAddIP(ctx, &bbc.BatchAddIpArgs{
				InstanceId:                     n.instanceID,
				SecondaryPrivateIpAddressCount: ipv4ToAllocate,
			})
		} else {
			ips, err = n.manager.bceclient.BBCBatchAddIPCrossSubnet(ctx, &bbc.BatchAddIpCrossSubnetArgs{
				InstanceId: n.instanceID,
				SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
					{
						EniId:                          allocation.InterfaceID,
						SubnetId:                       string(allocation.PoolID),
						SecondaryPrivateIpAddressCount: ipv4ToAllocate,
					},
				},
			})
		}

		err = n.manager.HandlerVPCError(scopedLog, err, string(allocation.PoolID))
		if err != nil {
			return nil, nil, fmt.Errorf("allocate %s ip to bbc eni %s failed: %v",
				string(allocation.PoolID), allocation.InterfaceID, err)
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
func (n *bbcNetworkResourceSet) createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	err = n.createBBCENI(scopedLog)
	if err != nil {
		return 0, "", err
	}
	return 1, "", nil
}

// releaseIPs implements realNodeInf
func (n *bbcNetworkResourceSet) releaseIPs(ctx context.Context, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
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
func (n *bbcNetworkResourceSet) prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	// Calculate the number of IPs that can be allocated on the node
	allocation := &ipam.AllocationAction{}

	if n.tryRefreshBBCENI() == nil {
		allocation.AvailableInterfaces = 1
		return allocation, nil
	}

	eniQuota := n.getENIQuota()
	if eniQuota != nil {
		n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name, func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			allocation.AvailableForAllocationIPv4 = eniQuota.GetMaxIP() - len(e.Spec.PrivateIPSet)
			allocation.InterfaceID = e.Name

			if n.enableNodeAnnotationSubnet() {
				sbn := searchMaxAvailableSubnet(n.availableSubnets)
				if sbn == nil {
					err = fmt.Errorf("can not find available subnet for bbc instance %s", n.instanceID)
					n.appendAllocatedIPError(e.Name, ccev2.NewErrorStatusChange(err.Error()))
					return fmt.Errorf("can not find available subnet for bbc instance %s", n.instanceID)
				}
				allocation.PoolID = ipamTypes.PoolID(sbn.Name)
			} else {
				allocation.PoolID = ipamTypes.PoolID(e.Spec.SubnetID)
			}

			sbn, err := n.manager.sbnlister.Get(string(allocation.PoolID))
			if err != nil {
				err = fmt.Errorf("get subnet %s failed: %v", e.Spec.SubnetID, err)
				n.appendAllocatedIPError(e.Name, ccev2.NewErrorStatusChange(err.Error()))
				return err
			}
			allocation.AvailableForAllocationIPv4 = math.IntMin(allocation.AvailableForAllocationIPv4, sbn.Status.AvailableIPNum)

			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return allocation, nil
}

// AllocateIPCrossSubnet implements realNodeInf
// use for scene 1: bbc node use cross subnet to allocate ip
// use for scene 2: psts
// Note that scene 1 and scene 2 cannot be mixed
func (n *bbcNetworkResourceSet) allocateIPCrossSubnet(ctx context.Context, sbnID string) ([]*models.PrivateIP, string, error) {
	if n.tryRefreshBBCENI() == nil {
		return nil, "", fmt.Errorf("bbc eni %s is not ready", n.instanceID)
	}

	ipv4, _, err := n.allocateIPs(ctx, n.log, &ipam.AllocationAction{
		PoolID:      ipamTypes.PoolID(sbnID),
		InterfaceID: n.bbceni.Name,
	}, 1, 0)
	return ipv4, "", err
}

// ReuseIPs implements realNodeInf
func (n *bbcNetworkResourceSet) reuseIPs(ctx context.Context, ips []*models.PrivateIP, owner string) (eniID string, ipDeletedFromoldEni bool, ipsReleased []string, err error) {
	if n.tryRefreshBBCENI() == nil {
		return "", false, ipsReleased, fmt.Errorf("bbc eni %s is not ready", n.instanceID)
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(owner)
	if err != nil {
		return "", false, ipsReleased, fmt.Errorf("invalid owner %s: %v", owner, err)
	}

	scopeLog := n.log.WithFields(logrus.Fields{
		"func":      "reuseIPs",
		"namespace": namespace,
		"name":      name,
		"ips":       logfields.Repr(ips),
	})

	// release ip from old bcc/ebc/bbc eni before reuse ip if necessary
	// check ip conflict
	// should to delete ip from the old eni
	isLocalIP, ipsReleased, err := n.rleaseOldIP(ctx, scopeLog, ips, namespace, name, func(ctx context.Context, scopedLog *logrus.Entry, eniID string, toReleaseIPs []string) error {
		eni, err := n.manager.enilister.Get(eniID)
		if err != nil {
			return fmt.Errorf("fail to release old ip. get eni %s failed: %v", eniID, err)
		}
		instanceID := eni.Spec.InstanceID
		scopedLog.WithFields(logrus.Fields{
			"oldENI":       eniID,
			"instanceID":   instanceID,
			"toReleaseIPs": toReleaseIPs,
		})
		err = n.manager.bceclient.BBCBatchDelIP(ctx, &bbc.BatchDelIpArgs{
			InstanceId: instanceID,
			PrivateIps: toReleaseIPs,
		})
		if err != nil {
			scopedLog.Warnf("release old ip from bbc eni %s failed: %v", n.instanceID, err)
		} else {
			scopedLog.Info("release old ip from bbc eni successfully")
		}
		return err
	})
	if err != nil {
		return "", false, ipsReleased, err
	}
	if isLocalIP {
		scopeLog.Info("ip is local, no need to release ip from bbc eni")
		return n.bbceni.Name, false, ipsReleased, nil
	} else {
		if len(ipsReleased) > 0 {
			scopeLog.Info("ip is not local, need to release ip from bbc eni")
			ipDeletedFromoldEni = true
		}
	}
	// check if all ips are released from old bcc/ebc/bbc eni before reuse ip
	if ipDeletedFromoldEni && (len(ips) != len(ipsReleased)) {
		scopeLog.Warnf("ip is not local, but only some ips (%v) are released from bbc eni", ipsReleased)
		return "", ipDeletedFromoldEni, ipsReleased, fmt.Errorf("ip is not local, but only some ips (%v) are released from bbc eni", ipsReleased)
	}
	var ipAndSubnets []bbc.IpAndSubnet
	for _, pip := range ips {
		ipAndSubnets = append(ipAndSubnets, bbc.IpAndSubnet{
			PrivateIp: pip.PrivateIPAddress,
			SubnetId:  pip.SubnetID,
		})
	}
	resp, err := n.manager.bceclient.BBCBatchAddIPCrossSubnet(ctx, &bbc.BatchAddIpCrossSubnetArgs{
		InstanceId: n.instanceID,
		SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
			{
				EniId:        n.bbceni.Name,
				IpAndSubnets: ipAndSubnets,
			},
		},
	})

	if err != nil {
		scopeLog.WithError(err).Error("failed to reuse ip cross subnet")
		return "", ipDeletedFromoldEni, ipsReleased, err
	} else if len(resp.PrivateIps) == 0 {
		scopeLog.Error("failed to reuse ip cross subnet without any error")
		return "", ipDeletedFromoldEni, ipsReleased, fmt.Errorf("failed to reuse ip cross subnet without any error")
	}
	scopeLog.WithField("ips", logfields.Repr(ips)).Info("failed to reuse ip cross subnet")

	return n.bbceni.Name, ipDeletedFromoldEni, ipsReleased, nil
}

var _ realNodeInf = &bbcNetworkResourceSet{}
