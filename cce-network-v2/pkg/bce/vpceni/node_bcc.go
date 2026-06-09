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
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
)

const (
	// Retry constants for BCC API operations
	maxRetryAttempts = 3
	baseRetryDelay   = 100 * time.Millisecond
)

var validate = validator.New()

// retryOperation executes an operation with exponential backoff retry
func retryOperation(operation func() error, logger *logrus.Entry, operationName string) error {
	var err error
	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		if attempt < maxRetryAttempts-1 {
			delay := time.Duration(1<<uint(attempt)) * baseRetryDelay
			logger.WithError(err).Warnf("%s failed (attempt %d/%d), retrying in %v",
				operationName, attempt+1, maxRetryAttempts, delay)
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("%s failed after %d attempts: %w", operationName, maxRetryAttempts, err)
}

// bccNetworkResourceSet is a wrapper of bceNetworkResourceSet, which is used to distinguish bcc node
type bccNetworkResourceSet struct {
	*bceNetworkResourceSet

	// bcc instance info
	bccInfo *bccapi.InstanceModel
	bccLock sync.RWMutex

	// usePrimaryENIWithSecondaryMode primary eni with secondary IP mode only use by ebc instance
	usePrimaryENIWithSecondaryMode bool

	// instanceDeleted marks if the BCC instance has been deleted (NoSuchObject error)
	instanceDeleted      bool
	primaryENISubnetID   string
	haveCreatePrimaryENI bool
}

func newBCCNetworkResourceSet(super *bceNetworkResourceSet) *bccNetworkResourceSet {
	if super == nil {
		super = &bceNetworkResourceSet{}
	}
	node := &bccNetworkResourceSet{
		bceNetworkResourceSet: super,
	}
	node.instanceType = string(metadata.InstanceTypeExBCC)
	return node
}

// 当 ENI.UseMode 是 ccev2.ENIUseModePrimaryWithSecondaryIP，不关注 eniQuota，直接使用主网卡辅助 IP 模式
func (n *bccNetworkResourceSet) isForceUsePrimaryENIWithSecondaryMode() bool {
	if n != nil && n.k8sObj != nil && n.k8sObj.Spec.ENI != nil &&
		n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryWithSecondaryIP) {
		n.log.Info("force use primary eni with secondary ip mode")
		return true
	}
	return false
}

// refreshBCCInfo refresh bcc instance info
// caller should ensure be locked
func (n *bccNetworkResourceSet) refreshBCCInfo() (*bccapi.InstanceModel, error) {
	n.bccLock.RLock()
	// Check if instance is marked as deleted
	if n.instanceDeleted {
		n.bccLock.RUnlock()
		return nil, fmt.Errorf("instance %s has been deleted", n.instanceID)
	}
	if n.bccInfo != nil {
		n.bccLock.RUnlock()
		return n.bccInfo, nil
	}
	n.bccLock.RUnlock()

	bccInfo, err := n.bceclient.GetBCCInstanceDetail(context.TODO(), n.instanceID)
	if err != nil {
		// Check if instance not found (deleted by user)
		if cloud.IsErrorReasonNoSuchObject(err) {
			n.log.WithError(err).Warnf("BCC instance %s not found (deleted), marking as deleted", n.instanceID)
			n.bccLock.Lock()
			n.instanceDeleted = true
			n.bccLock.Unlock()
			return nil, fmt.Errorf("instance %s not found (deleted)", n.instanceID)
		}
		n.log.Errorf("faild to get bcc instance detail: %v", err)
		return nil, err
	}
	n.log.WithField("bccInfo", logfields.Repr(bccInfo)).WithField("node", n.k8sObj.Name).Infof("Get bcc instance detail response: %+v", bccInfo)
	// Validate response to detect API degradation (needs nicInfo)
	// Skip validation for HPAS instances as they don't return NicInfo
	if n.instanceType != string(metadata.InstanceTypeExHPAS) {
		if err := validate.Struct(bccInfo); err != nil {
			n.log.WithError(err).Error("BCC instance response validation failed (possible API degradation)")
			return nil, fmt.Errorf("BCC instance response validation failed: %w", err)
		}
	}

	if bccInfo.EniQuota == 0 || n.isForceUsePrimaryENIWithSecondaryMode() {
		n.usePrimaryENIWithSecondaryMode = true
	}

	n.bccLock.Lock()
	defer n.bccLock.Unlock()
	// add security group to node
	if operatorOption.Config.SecurityGroupSynerDuration > 0 {
		n.securityGroupIDs = bccInfo.NicInfo.SecurityGroups
		esgs, err := n.bceclient.ListEsg(context.TODO(), n.instanceID)
		if err != nil {
			n.log.WithError(err).Error("list esg failed")
		} else {
			n.enterpriseSecurityGroupIDs = make([]string, 0)
			for _, esg := range esgs {
				n.enterpriseSecurityGroupIDs = append(n.securityGroupIDs, esg.Id)
			}
		}
	}

	n.bccInfo = bccInfo
	return n.bccInfo, nil
}

func (n *bccNetworkResourceSet) refreshENIQuota(scopeLog *logrus.Entry) (ENIQuotaManager, error) {
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

	bccInfo, err := n.refreshBCCInfo()
	if err != nil {
		return nil, err
	}

	eniQuota := newCustomerIPQuota(scopeLog, client, n.k8sObj.Name, n.instanceID, n.bceclient)
	// default bbc ip quota
	defaltENINums, defaultIPs := getDefaultBCCEniQuota(k8sNode)

	// Expect all BCC models to support ENI
	// EBC models may not support eni, and there are also some non console created
	// BCCs that do not support this parameter
	if bccInfo.EniQuota != 0 || n.k8sObj.Spec.ENI.InstanceType == string(ccev2.ENIForBCC) {
		eniQuota.SetMaxENI(bccInfo.EniQuota)
		if bccInfo.EniQuota == 0 {
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
func (n *bccNetworkResourceSet) prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	_, err = n.refreshBCCInfo()
	if err != nil {
		scopedLog.Errorf("failed to refresh ebc info: %v", err)
		return nil, fmt.Errorf("failed to refresh ebc info")
	}
	a, err = n.__prepareIPAllocation(scopedLog, true)
	if err != nil {
		return nil, err
	}

	if !n.usePrimaryENIWithSecondaryMode {
		return a, nil
	}

	// 主网卡辅助 ip 才需要执行创建主网卡逻辑，如果已经创建过主网卡，则不需要再创建
	if n.haveCreatePrimaryENI {
		return a, nil
	}

	if err = n.refreshPrimarySubnet(); err != nil {
		return a, err
	}

	if a.AvailableInterfaces == 0 && a.InterfaceID == "" {
		err := foreachInstanceReadyOnNode(n.manager, n.instanceID, n.k8sObj.Name,
			func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
				_, ok := iface.Resource.(*eniResource)
				if !ok {
					return nil
				}
				n.haveCreatePrimaryENI = true
				return nil
			})
		if err != nil {
			return a, err
		}
		if !n.haveCreatePrimaryENI {
			a.AvailableInterfaces = 1
		}
	}
	return a, err
}

func (n *bccNetworkResourceSet) refreshPrimarySubnet() error {
	if !n.usePrimaryENIWithSecondaryMode {
		return nil
	}
	bccInfo, err := n.refreshBCCInfo()
	if err != nil {
		return err
	}
	n.primaryENISubnetID = bccInfo.NicInfo.SubnetId
	subnets := n.FilterAvailableSubnetIds([]string{n.primaryENISubnetID}, n.GetMinimumAllocatableIPv4())
	n.availableSubnets = subnets
	return nil
}

func (n *bccNetworkResourceSet) createPrimaryENIOnCluster(ctx context.Context, scopedLog *logrus.Entry, resource *ccev2.NetResourceSet) error {
	bccInfo, err := n.refreshBCCInfo()
	if err != nil {
		return err
	}
	if err = n.refreshPrimarySubnet(); err != nil {
		return err
	}

	effectiveInstanceID, err := resource.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		return err
	}

	zone := api.TransAvailableZoneToZoneName(operatorOption.Config.BCECloudContry, operatorOption.Config.BCECloudRegion, resource.Spec.ENI.AvailabilityZone)
	eni, err := n.manager.enilister.Get(bccInfo.NicInfo.EniId)
	if kerrors.IsNotFound(err) {
		eni = &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					k8s.LabelInstanceID: effectiveInstanceID,
					k8s.LabelNodeName:   resource.Name,
					k8s.LabelENIType:    resource.Spec.ENI.InstanceType,
					k8s.LabelENIUseMode: string(ccev2.ENIUseModePrimaryWithSecondaryIP),
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: ccev2.SchemeGroupVersion.String(),
					Kind:       ccev2.NRSKindDefinition,
					Name:       resource.Name,
					UID:        resource.UID,
				}},
				Name: bccInfo.NicInfo.EniId,
			},
			Spec: ccev2.ENISpec{
				NodeName: resource.Name,
				UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
				ENI: models.ENI{
					ID:               bccInfo.NicInfo.EniId,
					Name:             bccInfo.NicInfo.Name,
					ZoneName:         zone,
					InstanceID:       n.instanceID,
					VpcID:            bccInfo.NicInfo.VpcId,
					SubnetID:         bccInfo.NicInfo.SubnetId,
					SecurityGroupIds: bccInfo.NicInfo.SecurityGroups,
					MacAddress:       bccInfo.NicInfo.MacAddress,
				},
				RouteTableOffset:          resource.Spec.ENI.RouteTableOffset,
				InstallSourceBasedRouting: false,
				Type:                      ccev2.ENIType(resource.Spec.ENI.InstanceType),
			},
		}

		for _, nicip := range bccInfo.NicInfo.Ips {
			eni.Spec.ENI.PrivateIPSet = append(eni.Spec.ENI.PrivateIPSet, &models.PrivateIP{
				SubnetID:         bccInfo.NicInfo.SubnetId,
				Primary:          nicip.Primary == "true",
				PrivateIPAddress: nicip.PrivateIp,
				PublicIPAddress:  nicip.Eip,
			})
		}

		eni.Finalizers = append(eni.Finalizers, bcesync.FinalizerENI)

		if eni, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{}); err != nil {
			scopedLog.Errorf("failed to create primary ENI %s with secondary IP: %v", eni.Name, err)
			return fmt.Errorf("failed to create primary ENI %s on k8s", eni.Name)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get primary ENI %s on k8s: %v", bccInfo.NicInfo.EniId, err)
	}

	if err = n.tryBorrowIPs(eni); err != nil {
		return err
	}

	if eni.Status.VPCStatus != ccev2.VPCENIStatusInuse {
		(&eni.Status).AppendVPCStatus(ccev2.VPCENIStatusInuse)
		if _, err = k8s.CCEClient().CceV2().ENIs().UpdateStatus(ctx, eni, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update primary ENI status: %w", err)
		}
		scopedLog.Infof("update primary ENI status to inuse successed")
	}
	n.haveCreatePrimaryENI = true
	return nil
}

// __prepareIPAllocation 函数用于准备IP分配，返回一个ipam.AllocationAction类型的指针和错误信息。
// 参数：
//   - scopedLog (*logrus.Entry)：日志记录器的入口点，用于记录日志。
//   - checkSubnet (bool)：是否检查子网，默认为false。
//
// 返回值：
//   - a (*ipam.AllocationAction)：包含可用IP分配信息的ipam.AllocationAction类型的指针。
//     AvailableForAllocationIPv4、AvailableForAllocationIPv6和PoolID字段将被设置为可用的IP数量和池ID。
//     InterfaceID字段将被设置为可用的接口ID。
//     AvailableInterfaces字段将被设置为可用的接口数量。
//   - err (error)：如果发生错误，则为非nil错误；否则为nil。
func (n *bccNetworkResourceSet) __prepareIPAllocation(scopedLog *logrus.Entry, checkSubnet bool) (a *ipam.AllocationAction, err error) {
	a = &ipam.AllocationAction{}
	eniQuota := n.bceNetworkResourceSet.getENIQuota()
	if eniQuota == nil {
		return nil, fmt.Errorf("eniQuota is nil, please retry later")
	}
	eniCount := 0

	err = foreachInstanceReadyOnNode(n.manager, n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			// available eni have been found
			eniCount++

			if (a.AvailableForAllocationIPv4 > 0 || a.AvailableForAllocationIPv6 > 0) &&
				a.PoolID != "" &&
				a.InterfaceID != "" {
				return nil
			}

			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}

			scopedLog = scopedLog.WithFields(logrus.Fields{
				"eniID":        interfaceID,
				"index":        e.Status.InterfaceIndex,
				"numAddresses": len(e.Spec.ENI.PrivateIPSet) - 1,
			})
			if e.Spec.UseMode == ccev2.ENIUseModePrimaryIP {
				return nil
			}

			// Eni that is not in an in use state should not be ignored.
			// Otherwise, this will result in the creation of a new eni.
			if e.Status.CCEStatus != ccev2.ENIStatusReadyOnNode {
				return fmt.Errorf("can not prepare ip allocation. eni %s status is %s on vpc, please try again later",
					interfaceID, e.Status.CCEStatus)
			}
			// The limits include the primary IP, so we need to take it into account
			// when computing the effective number of available addresses on the ENI.
			effectiveLimits := eniQuota.GetMaxIP()
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

			if subnet, err := bcesync.GlobalBSM().GetSubnet(e.Spec.ENI.SubnetID); err == nil {
				if subnet.GetBorrowedIPNum(interfaceID) > 0 {
					a.AvailableForAllocationIPv4 = math.IntMin(subnet.GetBorrowedIPNum(interfaceID), availableIPv4OnENI)
				} else {
					a.AvailableForAllocationIPv4 = math.IntMin(subnet.BorrowedAvailableIPsCount, availableIPv4OnENI)
				}
				a.InterfaceID = interfaceID
				a.PoolID = ipamTypes.PoolID(subnet.Name)
				// does not need to be checked for subnet with ipv6
				a.AvailableForAllocationIPv6 = availableIPv6OnENI
			} else {
				return fmt.Errorf("get subnet by id %s failed: %v", e.Spec.ENI.SubnetID, err)
			}
			return nil
		})
	a.AvailableInterfaces = math.IntMax(eniQuota.GetMaxENI()-eniCount, 0)
	return
}

// AllocateIPs is called after invoking PrepareIPAllocation and needs
// to perform the actual allocation.
func (n *bccNetworkResourceSet) allocateIPs(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	if n.usePrimaryENIWithSecondaryMode {
		return n.allocateIPsPrimaryENIMode(ctx, scopedLog, allocation, ipv4ToAllocate, ipv6ToAllocate)
	}
	return n.allocateIPsSecondaryENIMode(ctx, scopedLog, allocation, ipv4ToAllocate, ipv6ToAllocate)
}

func (n *bccNetworkResourceSet) allocateIPsPrimaryENIMode(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	if ipv4ToAllocate > 0 {
		resp, apiErr := n.bceclient.BCCBatchAddIP(ctx, &bccapi.BatchAddIpArgs{
			InstanceId:                     n.instanceID,
			SecondaryPrivateIpAddressCount: ipv4ToAllocate,
			AllocateMultiIpv6Addr:          ipv6ToAllocate > 0,
		})
		err = n.manager.HandlerVPCError(scopedLog, apiErr, string(allocation.PoolID))
		if err != nil {
			return nil, nil, fmt.Errorf("allocate ip to eni %s failed: %v", allocation.InterfaceID, err)
		}
		scopedLog.WithField("ips", resp.PrivateIps).Debug("allocate ip to eni success")
		for _, ipstring := range resp.PrivateIps {
			ip := net.ParseIP(ipstring)
			if ip.To4() == nil {
				ipv6PrivateIPSet = append(ipv6PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: ipstring,
					SubnetID:         string(allocation.PoolID),
				})
			} else {
				ipv4PrivateIPSet = append(ipv4PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: ipstring,
					SubnetID:         string(allocation.PoolID),
				})
			}
		}
	}
	return
}

func (n *bccNetworkResourceSet) allocateIPsSecondaryENIMode(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	var ips []string
	if ipv4ToAllocate > 0 {
		// Generate client token key for IPv4 batch allocation
		tokenKey := bceutils.GenerateBatchAddIPClientTokenKey(allocation.InterfaceID, ipv4ToAllocate, false)
		clientToken := n.clientTokens.GetOrCreateClientToken(tokenKey)

		scopedLog.WithFields(logrus.Fields{
			"clientTokenKey": tokenKey,
			"clientToken":    clientToken,
			"count":          ipv4ToAllocate,
		}).Debug("Using client token for BatchAddPrivateIP (IPv4)")

		// allocate ip
		ips, err = n.bceclient.BatchAddPrivateIPWithClientToken(ctx, []string{}, ipv4ToAllocate, allocation.InterfaceID, false, clientToken)
		if err == nil {
			// Remove client token after successful allocation
			n.clientTokens.RemoveClientToken(tokenKey)
		}

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
		// Generate client token key for IPv6 batch allocation
		tokenKey := bceutils.GenerateBatchAddIPClientTokenKey(allocation.InterfaceID, ipv6ToAllocate, true)
		clientToken := n.clientTokens.GetOrCreateClientToken(tokenKey)

		scopedLog.WithFields(logrus.Fields{
			"clientTokenKey": tokenKey,
			"clientToken":    clientToken,
			"count":          ipv6ToAllocate,
		}).Debug("Using client token for BatchAddPrivateIP (IPv6)")

		// allocate ipv6
		ips, err = n.bceclient.BatchAddPrivateIPWithClientToken(ctx, []string{}, ipv6ToAllocate, allocation.InterfaceID, true, clientToken)
		if err == nil {
			// Remove client token after successful allocation
			n.clientTokens.RemoveClientToken(tokenKey)
		}

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
func (n *bccNetworkResourceSet) releaseIPs(ctx context.Context, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	if n.usePrimaryENIWithSecondaryMode {
		return n.releaseIPsPrimaryENIMode(ctx, release, ipv4ToRelease, ipv6ToRelease)
	}

	if len(ipv4ToRelease) > 0 {
		err := n.bceclient.BatchDeletePrivateIP(ctx, ipv4ToRelease, release.InterfaceID, false)
		if err != nil {
			return fmt.Errorf("release ipv4 %v from eni %s failed: %v", ipv4ToRelease, release.InterfaceID, err)
		}
	}
	if len(ipv6ToRelease) > 0 {
		err := n.bceclient.BatchDeletePrivateIP(ctx, ipv6ToRelease, release.InterfaceID, true)
		if err != nil {
			return fmt.Errorf("release ipv6 %v from eni %s failed: %v", ipv6ToRelease, release.InterfaceID, err)
		}
	}
	return nil
}

// releaseIPsPrimaryENIMode releases IPs via the instance-level API when the node
// runs in primary ENI with secondary IP mode.
func (n *bccNetworkResourceSet) releaseIPsPrimaryENIMode(ctx context.Context, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	if len(ipv4ToRelease) > 0 {
		if err := n.bceclient.BCCBatchDelIP(ctx, &bccapi.BatchDelIpArgs{
			InstanceId: n.instanceID,
			PrivateIps: ipv4ToRelease,
		}); err != nil {
			return fmt.Errorf("release ipv4 %v from eni %s failed: %v", ipv4ToRelease, release.InterfaceID, err)
		}
	}
	if len(ipv6ToRelease) > 0 {
		if err := n.bceclient.BCCBatchDelIP(ctx, &bccapi.BatchDelIpArgs{
			InstanceId: n.instanceID,
			PrivateIps: ipv6ToRelease,
		}); err != nil {
			return fmt.Errorf("release ipv6 %v from eni %s failed: %v", ipv6ToRelease, release.InterfaceID, err)
		}
	}
	return nil
}

// AllocateIPCrossSubnet implements realNodeInf
func (n *bccNetworkResourceSet) allocateIPCrossSubnet(ctx context.Context, sbnID string) (result []*models.PrivateIP, eniID string, err error) {
	if n.usePrimaryENIWithSecondaryMode {
		return nil, "", fmt.Errorf("primary interface with secondary IP mode not support allocate ip cross subnet")
	}
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
		_, eniID, err = n.CreateInterface(ctx, action, scopedLog)
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
				err2 := retryOperation(func() error {
					deleteErr := n.bceclient.BatchDeletePrivateIP(ctx, ipsToRelease, eniID, true)
					if deleteErr != nil && strings.Contains(deleteErr.Error(), "PrivateIpInvalid") {
						// PrivateIpInvalid error is treated as success
						return nil
					}
					return deleteErr
				}, scopedLog, "BatchDeletePrivateIP")
				if err2 != nil {
					scopedLog.WithError(err2).Error("release ip failed")
				}
			}
		}
	}()

	// allocate ipv4
	ipstr, err = n.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, sbnID, []string{}, 1, false)
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
		ipstr, err = n.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, sbnID, []string{}, 1, true)
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
func (n *bccNetworkResourceSet) reuseIPs(ctx context.Context, ips []*models.PrivateIP, owner string) (eniID string, err error) {
	if n.usePrimaryENIWithSecondaryMode {
		return "", fmt.Errorf("primary interface with secondary IP mode not support reuse ip")
	}
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
	scopedLog.Debugf("try to reuseIPs ipv4Result: %v, ipv6Result: %v", ipv4Result, ipv6Result)

	// step1: release ip from old bcc/ebc/bbc eni before reuse ip if necessary
	// release ip from old bcc/ebc/bbc eni before reuse ip if necessary
	// check ip conflict
	// should to delete ip from the old eni
	isLocalIP, eniID, err := n.releaseOldIP(ctx, scopedLog, ips, namespace, name, func(ctx context.Context, scopedLog *logrus.Entry, eniID string, toReleaseIPs []string) error {
		scopedLog.WithField("oldENI", eniID).WithField("toReleaseIPs", toReleaseIPs)
		err = n.bceclient.BatchDeletePrivateIP(ctx, toReleaseIPs, eniID, false)
		if err != nil && !strings.Contains(err.Error(), "PrivateIpInvalid") {
			scopedLog.Warnf("release ip %s from eni %s failed: %v", toReleaseIPs, eniID, err)
		} else {
			err = nil
			scopedLog.Debugf("release ip %s from eni %s success", toReleaseIPs, eniID)
		}
		return err
	})
	if err != nil {
		return eniID, err
	}
	if isLocalIP {
		scopedLog.Infof("ip %s is local ip, directly reusable, eniID: %s", ips[0].PrivateIPAddress, eniID)
		return eniID, nil
	}

	scopedLog.Infof("ip %s is not local ip, try to reuse ip by choose available eni", ips[0].PrivateIPAddress)

	//step2: try to prepare ip allocation for reuse ip
	action, err := n.__prepareIPAllocation(scopedLog, false)
	if err != nil {
		return eniID, err
	}
	if action.AvailableForAllocationIPv4 == 0 && action.AvailableForAllocationIPv6 == 0 {
		if action.AvailableInterfaces == 0 {
			return "", fmt.Errorf("no available ip for allocation on node %s", n.k8sObj.Name)
		}
		_, eniID, err = n.CreateInterface(ctx, action, scopedLog)
		if err != nil {
			return "", fmt.Errorf("create interface failed: %v", err)
		}
		return "", fmt.Errorf("no available eni for allocation on node %s, try to create new eni %s", n.k8sObj.Name, eniID)
	}
	eniID = action.InterfaceID
	scopedLog.Infof("found available eni %s, prepare allocate ipv4 %s cross subnet", eniID, ips[0].PrivateIPAddress)

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
				err2 := retryOperation(func() error {
					deleteErr := n.bceclient.BatchDeletePrivateIP(ctx, ipsToRelease, eniID, true)
					if deleteErr != nil && strings.Contains(deleteErr.Error(), "PrivateIpInvalid") {
						// PrivateIpInvalid error is treated as success
						return nil
					}
					return deleteErr
				}, scopedLog, "BatchDeletePrivateIP")
				if err2 != nil {
					scopedLog.WithError(err2).Error("release ip failed")
				}
			}
		}
	}()

	if ipv4Result != nil {
		// allocate ipv4
		var tempIpstr []string
		allocateErr := retryOperation(func() error {
			var retryErr error
			tempIpstr, retryErr = n.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, ipv4Result.SubnetID, []string{ipv4Result.PrivateIPAddress}, 1, false)
			if retryErr != nil && strings.Contains(retryErr.Error(), "EniPrivateIpExistException") {
				// EniPrivateIpExistException error is treated as success
				return nil
			}
			return retryErr
		}, scopedLog, "BatchAddPrivateIpCrossSubnet")

		if allocateErr != nil {
			scopedLog.WithError(allocateErr).Errorf("allocate ip cross subnet failed for eniID %s with ipv4Result %v", eniID, ipv4Result)
			return eniID, allocateErr
		}
		if len(tempIpstr) != 1 {
			scopedLog.Warnf("IP %s is already allocated in eni %s", ipv4Result.PrivateIPAddress, eniID)
		}
		scopedLog.WithField("ipv4", tempIpstr).Debugf("reuse ip for owner %s in eni %s success", owner, eniID)
	}

	if operatorOption.Config.EnableIPv6 && ipv6Result != nil {
		ipstr, err = n.bceclient.BatchAddPrivateIpCrossSubnet(ctx, eniID, ipv6Result.SubnetID, []string{ipv6Result.PrivateIPAddress}, 1, true)
		if n.manager.HandlerVPCError(scopedLog, err, ipv6Result.SubnetID) != nil {
			scopedLog.WithError(err).Errorf("allocate ipv6 cross subnet failed for eniID %s with ipv6Result %v", eniID, ipv6Result)
			return eniID, err
		}
		if len(ipstr) != 1 {
			scopedLog.Warnf("IP %s is already allocated in eni %s", ipv6Result.PrivateIPAddress, eniID)
		}
		scopedLog.WithField("ipv6", ipstr).Debugf("reuse ip for owner %s in eni %s success", owner, eniID)
	}
	scopedLog.Infof("reuse ip for owner %s success", owner)
	return eniID, nil
}

var _ realNodeInf = &bccNetworkResourceSet{}
