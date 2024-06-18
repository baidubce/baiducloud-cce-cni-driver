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
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/cache"
)

// bccNode is a wrapper of Node, which is used to distinguish bcc node
type bccNode struct {
	*bceNode

	// bcc instance info
	bccInfo *bccapi.InstanceModel

	// usePrimaryENIWithSecondaryMode primary eni with secondary IP mode only use by ebc instance
	usePrimaryENIWithSecondaryMode bool
}

func newBCCNode(super *bceNode) *bccNode {
	node := &bccNode{
		bceNode: super,
	}
	node.instanceType = string(metadata.InstanceTypeExBCC)
	return node
}

func (n *bccNode) refreshBCCInfo() error {
	if n.bccInfo != nil {
		return nil
	}
	bccInfo, err := n.manager.bceclient.GetBCCInstanceDetail(context.TODO(), n.instanceID)
	if err != nil {
		n.log.Errorf("faild to get bcc instance detail: %v", err)
		return err
	}
	n.log.WithField("bccInfo", logfields.Repr(bccInfo)).Infof("Get bcc instance detail")
	n.bccInfo = bccInfo

	// TODO delete this test code block after bcc instance eniQuota is fixed
	// The tentative plan is for April 15th
	{
		if n.bccInfo.Spec == "ebc.la2.c256m1024.2d" {
			bccInfo.EniQuota = 0
		}
	}

	if bccInfo.EniQuota == 0 {
		n.usePrimaryENIWithSecondaryMode = true
	}
	return nil
}

func (n *bccNode) refreshENIQuota(scopeLog *logrus.Entry) (ENIQuotaManager, error) {
	scopeLog = scopeLog.WithField("nodeName", n.k8sObj.Name).WithField("method", "getENIQuota")
	client := k8s.WatcherClient()
	if client == nil {
		scopeLog.Fatal("K8s client is nil")
	}
	k8sNode, err := client.Informers.Core().V1().Nodes().Lister().Get(n.k8sObj.Name)
	if err != nil {
		scopeLog.Errorf("Get node failed: %v", err)
		return nil, err
	}

	err = n.refreshBCCInfo()
	if err != nil {
		return nil, err
	}
	// if bcc instance is not created by cce-network-v2, there is no need to check IP resouce
	if n.bccInfo == nil {
		return nil, fmt.Errorf("bcc info instance is nil")
	}

	eniQuota := newCustomerIPQuota(scopeLog, client, k8sNode, n.instanceID, n.manager.bceclient)
	// default bbc ip quota
	defaltENINums, defaultIPs := getDefaultBCCEniQuota(k8sNode)

	// Expect all BCC models to support ENI
	// EBC models may not support eni, and there are also some non console created
	// BCCs that do not support this parameter
	if n.bccInfo.EniQuota != 0 || n.k8sObj.Spec.ENI.InstanceType == string(ccev2.ENIForBCC) {
		eniQuota.SetMaxENI(n.bccInfo.EniQuota)
		if n.bccInfo.EniQuota == 0 {
			eniQuota.SetMaxENI(defaltENINums)
		}
	}
	eniQuota.SetMaxIP(defaultIPs)

	// if node use primary ENI mode, there is no need to check IP resouce
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		eniQuota.SetMaxIP(0)
	}

	return eniQuota, nil
}

func getDefaultBCCEniQuota(k8sNode *corev1.Node) (int, int) {
	var (
		cpuNum, memGB int
	)
	if cpu, ok := k8sNode.Status.Capacity[corev1.ResourceCPU]; ok {
		cpuNum = int(cpu.ScaledValue(resource.Milli)) / 1000
	}
	if mem, ok := k8sNode.Status.Capacity[corev1.ResourceMemory]; ok {
		memGB = int(mem.Value() / 1024 / 1024 / 1024)
	}
	return calculateMaxENIPerNode(cpuNum), calculateMaxIPPerENI(memGB)
}

// PrepareIPAllocation is called to calculate the number of IPs that
// can be allocated on the node and whether a new network interface
// must be attached to the node.
func (n *bccNode) prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	err = n.refreshBCCInfo()
	if err != nil {
		scopedLog.Errorf("failed to refresh ebc info: %v", err)
		return nil, fmt.Errorf("failed to refresh ebc info")
	}
	return n.__prepareIPAllocation(scopedLog, true)
}

func (n *bccNode) __prepareIPAllocation(scopedLog *logrus.Entry, checkSubnet bool) (a *ipam.AllocationAction, err error) {
	a = &ipam.AllocationAction{}
	eniQuota := n.bceNode.getENIQuota()
	if eniQuota == nil {
		return nil, fmt.Errorf("eniQuota is nil, please retry later")
	}
	eniMax := eniQuota.GetMaxENI()
	eniCount := 0

	n.manager.ForeachInstance(n.instanceID,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			// available eni have been found
			if (a.AvailableForAllocationIPv4 > 0 || a.AvailableForAllocationIPv6 > 0) &&
				a.PoolID != "" &&
				a.InterfaceID != "" {
				return nil
			}

			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			eniCount++

			scopedLog = scopedLog.WithFields(logrus.Fields{
				"eniID":        interfaceID,
				"index":        e.Status.InterfaceIndex,
				"numAddresses": len(e.Spec.ENI.PrivateIPSet) - 1,
			})
			if e.Spec.UseMode == ccev2.ENIUseModePrimaryIP {
				return nil
			}

			// Eni that is not in an in use state should be ignored, as even if the VPC interface is called to apply for an IP,
			// the following error will be obtained
			// [Code: EniStatusException; Message: The eni status is not allowed
			//  to operate; RequestId: 0f4d190a-76af-4671-9f29-b954dbb47195]
			if e.Status.VPCStatus != ccev2.VPCENIStatusInuse {
				scopedLog.WithField("vpcStatus", e.Status.VPCStatus).Warnf("skip ENI which is not in use")
				return nil
			}
			// The limits include the primary IP, so we need to take it into account
			// when computing the effective number of available addresses on the ENI.
			effectiveLimits := n.k8sObj.Spec.ENI.MaxIPsPerENI
			scopedLog.WithFields(logrus.Fields{
				"addressLimit": effectiveLimits,
			}).Debug("Considering ENI for allocation")

			amap := ipamTypes.AllocationMap{}
			addAllocationIP := func(ips []*models.PrivateIP) {
				for _, addr := range ips {
					// filter cross subnet private ip
					if addr.SubnetID != e.Spec.ENI.SubnetID {
						amap[addr.PrivateIPAddress] = ipamTypes.AllocationIP{Resource: e.Spec.ENI.ID}
					}
				}
			}
			addAllocationIP(e.Spec.ENI.PrivateIPSet)

			var (
				availableIPv4OnENI = 0
				availableIPv6OnENI = 0
			)
			if operatorOption.Config.EnableIPv4 && operatorOption.Config.EnableIPv6 {
				// Align the total number of IPv4 and IPv6
				ipv6Diff := len(e.Spec.ENI.IPV6PrivateIPSet) - len(e.Spec.ENI.PrivateIPSet)
				if ipv6Diff > 0 {
					availableIPv4OnENI = math.IntMin(effectiveLimits-len(e.Spec.ENI.PrivateIPSet), ipv6Diff)
				} else if ipv6Diff < 0 {
					availableIPv6OnENI = math.IntMin(effectiveLimits-len(e.Spec.ENI.IPV6PrivateIPSet), -ipv6Diff)
				} else {
					availableIPv4OnENI = math.IntMax(effectiveLimits-len(e.Spec.ENI.PrivateIPSet), 0)
					availableIPv6OnENI = math.IntMax(effectiveLimits-len(e.Spec.ENI.IPV6PrivateIPSet), 0)
				}
			} else if operatorOption.Config.EnableIPv4 {
				availableIPv4OnENI = math.IntMax(effectiveLimits-len(e.Spec.ENI.PrivateIPSet), 0)
			} else if operatorOption.Config.EnableIPv6 {
				availableIPv6OnENI = math.IntMax(effectiveLimits-len(e.Spec.ENI.IPV6PrivateIPSet), 0)
			}

			if availableIPv4OnENI == 0 && availableIPv6OnENI == 0 {
				return nil
			} else {
				a.AvailableInterfaces++
			}

			scopedLog.WithFields(logrus.Fields{
				"eniID":              interfaceID,
				"availableIPv4OnENI": availableIPv4OnENI,
				"availableIPv6OnENI": availableIPv6OnENI,
			}).Debug("ENI has IPs available")

			if !checkSubnet {
				a.InterfaceID = interfaceID
				a.PoolID = ipamTypes.PoolID(e.Spec.ENI.SubnetID)
				a.AvailableForAllocationIPv4 = availableIPv4OnENI
				a.AvailableForAllocationIPv6 = availableIPv6OnENI
				return nil
			}
			if subnet, _ := n.manager.sbnlister.Get(e.Spec.ENI.SubnetID); subnet != nil {
				if subnet.Status.Enable && subnet.Status.AvailableIPNum > 0 && a.InterfaceID == "" {
					scopedLog.WithFields(logrus.Fields{
						"subnetID":           e.Spec.ENI.SubnetID,
						"availableAddresses": subnet.Status.AvailableIPNum,
					}).Debug("Subnet has IPs available")

					a.InterfaceID = interfaceID
					a.PoolID = ipamTypes.PoolID(subnet.Name)
					a.AvailableForAllocationIPv4 = math.IntMin(subnet.Status.AvailableIPNum, availableIPv4OnENI)
					// does not need to be checked for subnet with ipv6
					a.AvailableForAllocationIPv6 = availableIPv6OnENI
				}
			}
			return nil
		})
	a.AvailableInterfaces = math.IntMax(eniMax-eniCount+a.AvailableInterfaces, 0)
	return
}

// AllocateIPs is called after invoking PrepareIPAllocation and needs
// to perform the actual allocation.
func (n *bccNode) allocateIPs(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	var ips []string
	if ipv4ToAllocate > 0 {
		// allocate ip
		ips, err = n.manager.bceclient.BatchAddPrivateIP(ctx, []string{}, ipv4ToAllocate, allocation.InterfaceID, false)
		err = n.manager.HandlerVPCError(scopedLog, err, string(allocation.PoolID))
		if err != nil {
			return nil, nil, fmt.Errorf("allocate ip to eni %s failed: %v", allocation.InterfaceID, err)
		}
		scopedLog.WithField("ips", ips).Debug("allocate ip to eni success")

		for _, ipstring := range ips {
			ipv4PrivateIPSet = append(ipv4PrivateIPSet, &models.PrivateIP{
				PrivateIPAddress: ipstring,
				SubnetID:         string(allocation.PoolID),
			})
		}
	}

	// allocate ipv6
	if ipv6ToAllocate > 0 {
		// allocate ipv6
		ips, err = n.manager.bceclient.BatchAddPrivateIP(ctx, []string{}, ipv6ToAllocate, allocation.InterfaceID, true)
		err = n.manager.HandlerVPCError(scopedLog, err, string(allocation.PoolID))
		if err != nil {
			err = fmt.Errorf("allocate ipv6 to eni %s failed: %v", allocation.InterfaceID, err)
			return
		}
		for _, ipstring := range ips {
			ipv6PrivateIPSet = append(ipv6PrivateIPSet, &models.PrivateIP{
				PrivateIPAddress: ipstring,
				SubnetID:         string(allocation.PoolID),
			})
		}
		scopedLog.WithField("ips", ips).Debug("allocate ipv6 to eni success")
	}
	return
}

// ReleaseIPs is called after invoking PrepareIPRelease and needs to
// perform the release of IPs.
func (n *bccNode) releaseIPs(ctx context.Context, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	if len(ipv4ToRelease) > 0 {
		err := n.manager.bceclient.BatchDeletePrivateIP(ctx, ipv4ToRelease, release.InterfaceID, false)
		if err != nil {
			return fmt.Errorf("release ipv4 %v from eni %s failed: %v", ipv4ToRelease, release.InterfaceID, err)
		}
	}
	if len(ipv6ToRelease) > 0 {
		err := n.manager.bceclient.BatchDeletePrivateIP(ctx, ipv6ToRelease, release.InterfaceID, true)
		if err != nil {
			return fmt.Errorf("release ipv6 %v from eni %s failed: %v", ipv6ToRelease, release.InterfaceID, err)
		}
	}
	return nil
}

// GetMaximumAllocatable Impl
func (n *bccNode) getMaximumAllocatable(eniQuota ENIQuotaManager) int {
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return 0
	}

	if eniQuota.GetMaxENI() == 0 {
		return eniQuota.GetMaxIP() - 1
	}
	max := eniQuota.GetMaxENI() * (eniQuota.GetMaxIP() - 1)
	if operatorOption.Config.EnableIPv6 {
		max = max * 2
	}
	return max
}

// GetMinimumAllocatable impl
func (n *bccNode) getMinimumAllocatable() int {
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return 0
	}
	min := n.k8sObj.Spec.IPAM.MinAllocate
	if min == 0 {
		min = defaults.IPAMPreAllocation
	}

	if operatorOption.Config.EnableIPv6 {
		min = min * 2
	}
	return min
}

// AllocateIPCrossSubnet implements realNodeInf
func (n *bccNode) allocateIPCrossSubnet(ctx context.Context, sbnID string) (result []*models.PrivateIP, eniID string, err error) {
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return nil, "", fmt.Errorf("allocate ip cross subnet not support primary ip mode")
	}
	scopedLog := n.log.WithField("subnet", sbnID).WithField("action", "allocateIPCrossSubnet")

	// get eni
	action, err := n.__prepareIPAllocation(scopedLog, false)
	if err != nil {
		return nil, "", err
	}
	if action.AvailableForAllocationIPv4 == 0 && action.AvailableForAllocationIPv6 == 0 {
		if action.AvailableInterfaces == 0 {
			return nil, "", fmt.Errorf("no available ip for allocation on node %s", n.k8sObj.Name)
		}
		_, eniID, err = n.createInterface(ctx, action, scopedLog)
		if err != nil {
			return nil, "", fmt.Errorf("create interface failed: %v", err)
		}
		return nil, "", fmt.Errorf("no available eni for allocation on node %s, try to create new eni %s", n.k8sObj.Name, eniID)
	}
	eniID = action.InterfaceID
	scopedLog = scopedLog.WithField("eni", eniID)
	scopedLog.Debug("prepare allocate ip cross subnet for eni")

	sbn, err := n.manager.sbnlister.Get(sbnID)
	if err != nil {
		return nil, eniID, fmt.Errorf("get subnet %s failed: %v", sbnID, err)
	}

	var (
		ipstr      []string
		ipv4Result *models.PrivateIP
		ipv6Result *models.PrivateIP
	)

	defer func() {
		// to roll back ip
		if err != nil {
			var ipsToRelease []string
			if ipv4Result != nil {
				ipsToRelease = append(ipsToRelease, ipv4Result.PrivateIPAddress)
			}
			if ipv6Result != nil {
				ipsToRelease = append(ipsToRelease, ipv6Result.PrivateIPAddress)
			}
			if len(ipsToRelease) > 0 {
				err2 := n.manager.bceclient.BatchDeletePrivateIP(ctx, ipsToRelease, eniID, true)
				if err2 != nil {
					scopedLog.WithError(err2).Error("release ip failed")
				}
			}
		}
	}()

	// allocate ipv4
	ipstr, err = n.manager.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, sbnID, []string{}, 1, false)
	if n.manager.HandlerVPCError(scopedLog, err, sbnID) != nil {
		scopedLog.WithError(err).Error("allocate ip cross subnet failed")
		return
	}
	if len(ipstr) != 1 {
		err = fmt.Errorf("allocate ip cross subnet failed, ipstr len %d", len(ipstr))
		scopedLog.WithError(err).Error("allocate ip cross subnet failed")
		return
	}
	ipv4Result = &models.PrivateIP{
		PrivateIPAddress: ipstr[0],
		SubnetID:         sbnID,
		Primary:          false,
	}
	result = append(result, ipv4Result)
	scopedLog.WithField("ipv4", ipstr).Debug("allocate ipv4 cross subnet success")

	if operatorOption.Config.EnableIPv6 && sbn.Spec.IPv6CIDR != "" {
		ipstr, err = n.manager.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, sbnID, []string{}, 1, true)
		if n.manager.HandlerVPCError(scopedLog, err, sbnID) != nil {
			scopedLog.WithError(err).Error("allocate ipv6 cross subnet failed")
			return
		}
		if len(ipstr) != 1 {
			err = fmt.Errorf("allocate ipv6 cross subnet failed, ipstr len %d", len(ipstr))
			scopedLog.WithError(err).Error("allocate ipv6 cross subnet failed")
			return
		}
		ipv6Result = &models.PrivateIP{
			PrivateIPAddress: ipstr[0],
			SubnetID:         sbnID,
			Primary:          false,
		}
		result = append(result, ipv6Result)
		scopedLog.WithField("ipv6", ipstr).Debug("allocate ipv6 cross subnet success")
	}
	return
}

// ReuseIPs implements realNodeInf
func (n *bccNode) reuseIPs(ctx context.Context, ips []*models.PrivateIP, owner string) (eniID string, err error) {
	if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		return "", fmt.Errorf("allocate ip cross subnet not support primary ip mode")
	}
	scopedLog := n.log.WithField("action", "reuseIPs")
	if len(ips) == 0 {
		return "", fmt.Errorf("no ip to reuse")
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(owner)
	if err != nil {
		err = fmt.Errorf("invalid owner %s: %v", owner, err)
		return
	}
	scopedLog = scopedLog.WithField("owner", owner)
	var (
		ipstr      []string
		ipv4Result *models.PrivateIP
		ipv6Result *models.PrivateIP
	)
	for _, privateIP := range ips {
		family := ccev2.IPFamilyByIP(privateIP.PrivateIPAddress)
		if family == ccev2.IPv4Family {
			ipv4Result = &models.PrivateIP{
				PrivateIPAddress: privateIP.PrivateIPAddress,
				SubnetID:         privateIP.SubnetID,
				Primary:          privateIP.Primary,
			}
		} else if family == ccev2.IPv6Family {
			ipv6Result = &models.PrivateIP{
				PrivateIPAddress: privateIP.PrivateIPAddress,
				SubnetID:         privateIP.SubnetID,
				Primary:          privateIP.Primary,
			}
		}
	}
	// get eni
	action, err := n.__prepareIPAllocation(scopedLog, false)
	if err != nil {
		return
	}
	if action.AvailableForAllocationIPv4 == 0 && action.AvailableForAllocationIPv6 == 0 {
		if action.AvailableInterfaces == 0 {
			return "", fmt.Errorf("no available ip for allocation on node %s", n.k8sObj.Name)
		}
		_, eniID, err = n.createInterface(ctx, action, scopedLog)
		if err != nil {
			return "", fmt.Errorf("create interface failed: %v", err)
		}
		return "", fmt.Errorf("no available eni for allocation on node %s, try to create new eni %s", n.k8sObj.Name, eniID)
	}
	eniID = action.InterfaceID
	scopedLog = scopedLog.WithField("eni", eniID)
	scopedLog.Debug("prepare allocate ip cross subnet for eni")

	// check ip conflict
	// should to delete ip from the old eni
	isLocalIP, err := n.rleaseOldIP(ctx, scopedLog, ips, namespace, name)
	if err != nil {
		return
	}
	if isLocalIP {
		enis, _ := watchers.ENIClient.GetByIP(ipv4Result.PrivateIPAddress)
		eniID = enis[0].Name
		scopedLog.Infof("ip %s is local ip, directly reusable", ips[0].PrivateIPAddress)
		return
	}

	defer func() {
		// to roll back ip
		if err != nil {
			var ipsToRelease []string
			if ipv4Result != nil {
				ipsToRelease = append(ipsToRelease, ipv4Result.PrivateIPAddress)
			}
			if ipv6Result != nil {
				ipsToRelease = append(ipsToRelease, ipv6Result.PrivateIPAddress)
			}
			if len(ipsToRelease) > 0 {
				err2 := n.manager.bceclient.BatchDeletePrivateIP(ctx, ipsToRelease, eniID, true)
				if err2 != nil {
					scopedLog.WithError(err2).Error("release ip failed")
				}
			}
		}
	}()

	// allocate ipv4
	ipstr, err = n.manager.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, ipv4Result.SubnetID, []string{ipv4Result.PrivateIPAddress}, 1, false)
	if err != nil {
		ipv4Result = nil
		scopedLog.WithError(err).Error("allocate ip cross subnet failed")
		return
	}
	if len(ipstr) != 1 {
		ipv4Result = nil
		err = fmt.Errorf("allocate ip cross subnet failed, ipstr len %d", len(ipstr))
		scopedLog.WithError(err).Error("allocate ip cross subnet failed")
		return
	}
	scopedLog.WithField("ipv4", ipstr).Debug("allocate ipv4 cross subnet success")

	if operatorOption.Config.EnableIPv6 && ipv6Result != nil {
		ipstr, err = n.manager.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, ipv6Result.SubnetID, []string{ipv6Result.PrivateIPAddress}, 1, true)
		if n.manager.HandlerVPCError(scopedLog, err, ipv6Result.SubnetID) != nil {
			scopedLog.WithError(err).Error("allocate ipv6 cross subnet failed")
			return
		}
		if len(ipstr) != 1 {
			err = fmt.Errorf("allocate ipv6 cross subnet failed, ipstr len %d", len(ipstr))
			scopedLog.WithError(err).Error("allocate ipv6 cross subnet failed")
			return
		}
		scopedLog.WithField("ipv6", ipstr).Debug("allocate ipv6 cross subnet success")
	}
	scopedLog.Debug("update eni success")
	return

}

// rleaseOldIP release old ip if it is not used by other endpoint
// return true: ip is used by current endpoint, do not need to release
// return false: ip is used by other endpoint, need to release
func (n *bccNode) rleaseOldIP(ctx context.Context, scopedLog *logrus.Entry, ips []*models.PrivateIP, namespace string, name string) (bool, error) {
	for _, privateIP := range ips {
		ceps, err := n.manager.cepClient.GetByIP(privateIP.PrivateIPAddress)
		if err == nil && len(ceps) > 0 {
			for _, cep := range ceps {
				if cep.Namespace != namespace || cep.Name != name {
					return false, fmt.Errorf("ip %s has been used by other endpoint %s/%s", privateIP.PrivateIPAddress, cep.Namespace, cep.Name)
				}
			}
		}
	}

	// if ip is local eni, do not need to release
	isLocalENI := false
	for _, privateIP := range ips {
		enis, err := watchers.ENIClient.GetByIP(privateIP.PrivateIPAddress)
		if err != nil && len(enis) == 0 {
			isLocalENI = false
			break
		}
		if err == nil {
			for _, eni := range enis {
				if eni.Spec.NodeName == n.k8sObj.Name {
					isLocalENI = true
				} else {
					isLocalENI = false
					break
				}
			}
		}
	}
	if isLocalENI {
		return true, nil
	}

	cep, err := n.manager.cepClient.Lister().CCEEndpoints(namespace).Get(name)
	if err != nil {
		return false, fmt.Errorf("get endpoint %s/%s failed: %v", namespace, name, err)
	}
	if cep.Status.Networking == nil || cep.Status.Networking.NodeIP != n.k8sObj.Name {
		var (
			oldENI       string
			toReleaseIPs []string
		)
		scopedLog.WithField("namespace", namespace).WithField("name", name).Debug("try to clean ip from old eni")

		if cep.Status.Networking != nil {
			for _, addr := range cep.Status.Networking.Addressing {
				oldENI = addr.Interface
				toReleaseIPs = append(toReleaseIPs, addr.IP)
			}
		}
		if len(toReleaseIPs) > 0 {
			releaseLog := scopedLog.WithField("oldENI", oldENI).WithField("toReleaseIPs", toReleaseIPs)
			err = n.manager.bceclient.BatchDeletePrivateIP(ctx, toReleaseIPs, oldENI, false)
			if err != nil {
				releaseLog.Warnf("delete ip %s from eni %s failed: %v", toReleaseIPs, oldENI, err)

			} else {
				releaseLog.Info("clean ip from old eni success")
			}
		}
	}
	return false, nil
}

var _ realNodeInf = &bccNode{}
