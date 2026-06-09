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
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma/client"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/nodeutils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaltRdmaEniNums       = 1
	defaultRdmaMaxIPsPerENI = 13
)

var validate = validator.New()

// rdmaNetResourceSetWrapper is a wrapper of NetResourceSet, which is used to distinguish no-RDMA NetResourceSet
type rdmaNetResourceSetWrapper struct {
	*bceRDMANetResourceSet

	// rdmaeni is the eni of the node
	rdmaENIName string

	// nextCreateENITime tracks cooling down period for ENI creation
	nextCreateENITime *time.Time

	// creatingENI tracks ENI being created
	creatingENI *ccev2.ENI

	// clientTokens stores client tokens for ERI creation and IP allocation idempotency
	clientTokens *bceutils.ClientTokenSet
}

func newRdmaNetResourceSetWrapper(super *bceRDMANetResourceSet) *rdmaNetResourceSetWrapper {
	node := &rdmaNetResourceSetWrapper{
		bceRDMANetResourceSet: super,
		clientTokens:          bceutils.NewClientTokenSet(),
	}
	node.instanceType = string(metadata.InstanceTypeExEHC)
	_, err := node.ensureRdmaENI()
	if err != nil {
		node.log.Errorf("failed to create eri or hpc eni: %v", err)
	}
	return node
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

// ensureRdmaENI means create a eni object for rdma interface
// rdma interface has only one eni, so we use rdma interface id as eni name
func (n *rdmaNetResourceSetWrapper) ensureRdmaENI() (*ccev2.ENI, error) {
	// Get the effective instance ID, which will be HPAS ID for HPAS instances.
	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		return nil, fmt.Errorf("failed to get effective instance ID: %w", err)
	}

	getOwnerReference := func() string {
		or := n.k8sObj.GetOwnerReferences()
		for _, ref := range or {
			if ref.Kind == "Node" {
				return ref.Name
			}
		}
		return ""
	}
	// If NRS is an ERI managed NRS, use special handling logic
	if n.isERIManagedNRS() {
		return n.handleERIManagedNRS()
	}

	if n.rdmaENIName != "" {
		eni, err := n.manager.eniLister.Get(n.rdmaENIName)
		if k8serrors.IsNotFound(err) {
			goto forceGetFromIaaS
		}
		eni = eni.DeepCopy()
		if err != nil {
			return nil, fmt.Errorf("failed to get rdma eni %s from lister", n.rdmaENIName)
		}

		isNeedUpdate := false
		if eni.Status.VPCStatus != ccev2.VPCENIStatusInuse {
			isNeedUpdate = true
		}

		if eni.Spec.InstanceID != effectiveInstanceID ||
			eni.Labels[k8s.LabelInstanceID] != effectiveInstanceID {
			eni.Spec.InstanceID = effectiveInstanceID
			if eni.Labels == nil {
				eni.Labels = make(map[string]string)
			}
			eni.Labels[k8s.LabelInstanceID] = effectiveInstanceID
			isNeedUpdate = true
		}

		if bceutils.IsCCERdmaNetRourceSetName(eni.Labels[k8s.LabelNodeName]) &&
			eni.Labels[k8s.LabelNodeName] != n.k8sObj.Name {
			// a labelSelectorValue's max length is 63 in kubernetes, so if nodeName's length is more than the max length like this:
			// 63 - len(string("-fa2700078302-elasticrdma")), we need to use node's InstanceID as node's identification to generate labelSelectorValue
			labelSelectorValue := bceutils.GetRdmaNrsLabelSelectorValueFromNetResourceSetName(n.k8sObj.Name,
				getOwnerReference(), n.k8sObj.Spec.InstanceID, eni.Spec.MacAddress, string(eni.Spec.Type))
			eni.Labels[k8s.LabelNodeName] = labelSelectorValue
			isNeedUpdate = true
		}

		if isNeedUpdate {
			n.log.Infof("force get eni %s from local", n.rdmaENIName)
			err = n.updateENIWithPoll(context.TODO(), eni, func(eni *ccev2.ENI) *ccev2.ENI {
				// do nothing
				return eni
			})
			if err != nil {
				return nil, fmt.Errorf("failed to update %s ENI %s status: %w", eni.Spec.Type, eni.Name, err)
			}
			n.log.Infof("update %s ENI %s status successed", eni.Spec.Type, eni.Name)
		}
		return eni, nil
	}

forceGetFromIaaS:
	n.log.Debugf("force get eni %s from iaas", n.rdmaENIName)
	// the hpc or eri api do not use vpcID, subnetID and zoneName
	vpcID := n.k8sObj.Spec.ENI.VpcID
	// the macAddress and vifFeatures is decided by the NetResourceSet's annotation
	macAddress := n.bceRDMANetResourceSet.k8sObj.Annotations[k8s.AnnotationRDMAInfoMacAddress]
	vifFeatures := n.bceRDMANetResourceSet.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]

	var rdmaEniId string
	// a labelSelectorValue's max length is 63 in kubernetes, so if nodeName's length is more than the max length like this:
	// 63 - len(string("-fa2700078302-elasticrdma")), we need to use node's InstanceID as node's identification to generate labelSelectorValue
	labelSelectorValue := bceutils.GetRdmaNrsLabelSelectorValueFromNetResourceSetName(n.k8sObj.Name,
		getOwnerReference(), n.k8sObj.Spec.InstanceID, macAddress, vifFeatures)
	requirement := metav1.LabelSelectorRequirement{
		Key:      k8s.LabelNodeName,
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{labelSelectorValue},
	}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{requirement},
	}
	// Select only the ENI of the local node
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		panic(fmt.Errorf("failed to create label selector: %v", err))
	}
	k8sRdmaEnis, err := n.manager.eniLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list enis: %w", err)
	}
	for _, rdmaEni := range k8sRdmaEnis {
		// only one RDMA ENI per RDMA NetworkResourceSet
		rdmaEniId = rdmaEni.Spec.ID
	}
	var rdmaEni *client.EniResult
	iaasClient := n.manager.getIaaSClient(vifFeatures)
	if rdmaEniId == "" {
		// Get effective instance ID based on instance type
		rdmaEni, err = n.findMatchedEniByMac(context.Background(), iaasClient, vpcID, effectiveInstanceID, vifFeatures, macAddress)
		if err != nil {
			n.log.WithError(err).Errorf("failed to get instance %s eni", vifFeatures)
			return nil, err
		}
		n.log.WithField("rdmaeni", logfields.Repr(rdmaEni)).Debugf("get instance %s eni success", vifFeatures)
		rdmaEniId = rdmaEni.Id
	}

	var ctx = context.Background()
	eni, err := n.manager.eniLister.Get(rdmaEniId)
	if k8serrors.IsNotFound(err) {
		// the hpc or eri do not use ensure subnet object

		var (
			ipv4IPSet, ipv6IPSet []*models.PrivateIP
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
		eni = &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				// use rdma interface id as eni name
				Name: rdmaEni.Id,
				Labels: map[string]string{
					k8s.LabelInstanceID: effectiveInstanceID,
					k8s.LabelNodeName:   labelSelectorValue,
					k8s.LabelENIType:    iaasClient.GetRDMAIntType(),
					k8s.VPCIDLabel:      vpcID,
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
					InstanceID:       effectiveInstanceID,
					PrivateIPSet:     ipv4IPSet,
					IPV6PrivateIPSet: ipv6IPSet,
					MacAddress:       macAddress,
				},
			},
			Status: ccev2.ENIStatus{},
		}
		eni, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create %s ENI: %w", vifFeatures, err)
		}
		n.log.Infof("create %s ENI %s resource successed", vifFeatures, eni.Name)
		// ERI managed RDMA NRS ENI does not need to update status to inuse here
		if !n.isERIManagedNRS() {
			(&eni.Status).AppendVPCStatus(ccev2.VPCENIStatusInuse)
			_, err = k8s.CCEClient().CceV2().ENIs().UpdateStatus(ctx, eni, metav1.UpdateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to update %s ENI %s status: %w", vifFeatures, eni.Name, err)
			}
			n.log.Infof("update %s ENI %s status successed", vifFeatures, eni.Name)
		} else {
			n.log.Infof("Skip updating ERI managed ENI %s status to inuse", eni.Name)
		}
	} else if err != nil {
		n.log.Errorf("failed to get %s ENI %s resource: %v", vifFeatures, eni.Name, err)
		return nil, err
	} else {
		n.log.Infof("update eni %s from iaas", eni.Name)
		if eni.Spec.InstanceID != effectiveInstanceID ||
			eni.Labels[k8s.LabelInstanceID] != effectiveInstanceID {
			eni.Spec.InstanceID = effectiveInstanceID
			eni.Labels[k8s.LabelInstanceID] = effectiveInstanceID
		}
		err = n.updateENIWithPoll(ctx, eni, func(eni *ccev2.ENI) *ccev2.ENI {
			// do nothing
			return eni
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update %s ENI %s status: %w", vifFeatures, eni.Name, err)
		}
		n.log.Infof("update %s ENI %s status successed", vifFeatures, eni.Name)
	}
	n.log.Debugf("got %s ENI %s resource successed", vifFeatures, eni.Name)
	n.rdmaENIName = eni.Name
	return eni, err
}

func (n *rdmaNetResourceSetWrapper) refreshBCCInfo() (*bccapi.InstanceModel, error) {
	n.limiterLock.Lock()
	if n.bccInfo != nil {
		n.limiterLock.Unlock()
		return n.bccInfo, nil
	}
	n.limiterLock.Unlock()

	log := n.log.WithField("method", "refreshBCCInfo")
	log.Infof("refresh bcc info")
	// Always use base instance-id for BCC
	bccInfo, err := n.manager.bceClientd.GetBCCInstanceDetail(context.TODO(), n.instanceID)
	if err != nil {
		n.log.Errorf("faild to get bcc instance detail: %v", err)
		return nil, err
	}
	// In RDMA mode, do not validate bccInfo after retrieval.
	n.log.WithField("bccInfo", logfields.Repr(bccInfo)).Infof("Get bcc instance detail")

	n.limiterLock.Lock()
	defer n.limiterLock.Unlock()
	n.bccInfo = bccInfo

	return n.bccInfo, nil
}

func (n *rdmaNetResourceSetWrapper) refreshENIQuota(scopeLog *logrus.Entry) (RdmaEniQuotaManager, error) {
	scopeLog = scopeLog.WithField("netResourceSetName", n.k8sObj.Name).WithField("method", "generateRdmaIpResourceManager")
	client := k8s.WatcherClient()
	if client == nil {
		scopeLog.Fatal("K8s client is nil")
	}

	var ownerReferenceNodeName string
	or := n.k8sObj.GetOwnerReferences()
	for _, ref := range or {
		if ref.Kind == "Node" {
			ownerReferenceNodeName = ref.Name
			break
		}
	}
	nodeName := bceutils.GetNodeNameFromNetResourceSetName(n.k8sObj.Name, ownerReferenceNodeName, n.k8sObj.InstanceID())
	k8sNode, err := client.Informers.Core().V1().Nodes().Lister().Get(nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s node %s: %v", n.k8sObj.Name, err)
	}

	bccInfo, err := n.refreshBCCInfo()
	if err != nil {
		return nil, err
	}

	n.limiterLock.Lock()
	defer n.limiterLock.Unlock()

	eniQuota := newCustomerIPQuota(scopeLog, client, k8sNode, n.instanceID, n.manager.bceClientd)

	// Expect all BCC models to support ERI
	if bccInfo.EriQuota != 0 {
		eniQuota.SetMaxENI(bccInfo.EriQuota)
		if bccInfo.EriQuota == 0 {
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
		eni, err := n.ensureRdmaENI()
		if err != nil {
			return nil, nil, err
		}
		if eni == nil {
			return nil, nil, fmt.Errorf("eni is nil")
		}

		// Generate client token key for IPv4 batch allocation (ERI doesn't support IPv6 yet)
		tokenKey := bceutils.GenerateBatchAddIPClientTokenKey(eni.Spec.ID, ipv4ToAllocate, false)
		clientToken := n.clientTokens.GetOrCreateClientToken(tokenKey)

		scopedLog.WithFields(logrus.Fields{
			"clientTokenKey": tokenKey,
			"clientToken":    clientToken,
			"count":          ipv4ToAllocate,
		}).Debug("Using client token for BatchAddPrivateIP (ERI IPv4)")

		// Batch allocation as primary approach
		ips, err := iaasClient.BatchAddPrivateIPWithClientToken(ctx, eni.Spec.ID, []string{}, ipv4ToAllocate, clientToken)
		if err == nil {
			// Remove client token after successful allocation
			n.clientTokens.RemoveClientToken(tokenKey)
		}

		err = n.manager.HandlerVPCError(scopedLog, err, string(allocation.PoolID))
		if err != nil {
			// Check if this is a "no more IP" related error that might benefit from recovery
			if cloud.IsErrorSubnetHasNoMoreIP(err) {

				scopedLog.WithError(err).Info("RDMA ENI already has IPs in VPC, recovering all from VPC")
				// Try to recover IPs from VPC as fallback (for restart recovery)
				if recoveredIPs, err := n.tryRecoverIPsFromVPC(ctx, scopedLog, eni, allocation); err == nil {
					scopedLog.Info("Successfully recovered IPs from VPC after batch allocation failure")
					return recoveredIPs, ipv6PrivateIPSet, nil
				} else {
					return nil, nil, err
				}
			}
			// For other errors or if recovery failed, return the original error
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
	// Check if this is ERI management mode
	if n.isERIManagedNRS() {
		return n.createERIInterface(ctx, allocation, scopedLog)
	} else {
		// Traditional RDMA ENI creation logic
		_, err = n.ensureRdmaENI()
		if err != nil {
			return 0, "", err
		}
	}
	return 1, "", nil
}

// createERIInterface creates ERI interface using dedicated method, following BCC mode
func (n *rdmaNetResourceSetWrapper) createERIInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	n.mutex.RLock()
	resource := n.k8sObj
	n.mutex.RUnlock()

	// Check cooling time (following BCC mode)
	if n.nextCreateENITime != nil && n.nextCreateENITime.After(time.Now()) {
		msg = fmt.Sprintf("unable to create new ERI: cooling down, next available time is %v", n.nextCreateENITime.Format(time.RFC3339))
		err = errors.New(msg)
		return
	}

	var (
		rdmaEniQuota      = n.getRdmaEniQuota()
		availableERICount = 0
	)

	if rdmaEniQuota == nil {
		msg = "unable to get RDMA ENI quota"
		err = errors.New(msg)
		return
	}

	// Get effective instance ID based on instance type
	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		msg = fmt.Sprintf("unable to get effective instance ID: %v", err)
		err = errors.New(msg)
		return
	}

	// Count current available ERI numbers
	n.manager.ForeachInstance(effectiveInstanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			// Only count ERI type ENIs
			if e.Spec.Type == ccev2.ENIForERI {
				availableERICount++
			}
			return nil
		})

	// Check if maximum ERI limit is reached
	if availableERICount >= defaltRdmaEniNums {
		msg = "unable to create ERI: maximum ERI limit reached"
		err = errors.New(msg)
		return
	}

	// Check if currently creating ERI
	if n.creatingENI != nil {
		msg = "unable to create ERI: currently creating ERI"
		err = errors.New("currently creating ERI")
		return
	}

	vifFeatures := n.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]
	if vifFeatures != bceutils.OverlayRDMA {
		msg = "ERI managed NRS must have elastic_rdma vif features"
		err = errors.New(msg)
		return
	}

	vpcID := n.k8sObj.Spec.ENI.VpcID

	// Ensure all configured subnets (referencing node_super.go)
	var allSubnets []*bcesync.BorrowedSubnet
	for _, subnetID := range resource.Spec.ENI.SubnetIDs {
		sbn, err := bcesync.GlobalBSM().EnsureSubnet(vpcID, subnetID)
		if err != nil {
			continue
		}
		allSubnets = append(allSubnets, sbn)
	}

	// Filter subnets with detailed error information (referencing node_super.go)
	availableSubnets, filterErr := nodeutils.FilterAvailableSubnet(
		allSubnets,
		defaultRdmaMaxIPsPerENI,
		operatorOption.Config.BCECloudContry,
		operatorOption.Config.BCECloudRegion,
		resource.Spec.ENI.AvailabilityZone,
	)

	// Get the actual node name from NetResourceSet name (referencing ERI managed NRS)
	nodeName := strings.TrimSuffix(resource.Name, "-eri-managed")

	if len(availableSubnets) == 0 {
		// Categorize subnet error and set node condition (referencing node_super.go)
		errorCode, reason, errStr := nodeutils.CategorizeSubnetError(filterErr, resource.Spec.ENI.SubnetIDs, resource.Name)
		metrics.IPAMErrorCounter.WithLabelValues(errorCode, "NetResourceSet", resource.Name).Inc()
		watchers.SetNodeCondition(nodeName, watchers.CCESubnetUnavailable, corev1.ConditionTrue, reason, errStr, n.log)

		// Use filterErr if available, otherwise use default message (referencing node_super.go)
		if filterErr != nil {
			msg = filterErr.Error()
		} else {
			msg = fmt.Sprintf("no matching subnet available for ERI creation (VPC=%s AZ=%s SubnetIDs=%v)",
				vpcID,
				resource.Spec.ENI.AvailabilityZone,
				resource.Spec.ENI.SubnetIDs,
			)
		}
		err = errors.New(msg)
		return
	}

	// Set node condition to False when subnets are available (referencing node_super.go)
	subnetIDs := nodeutils.ExtractSubnetIDs(availableSubnets)
	successMsg := fmt.Sprintf("subnets [%v] are available for ERI creation on node %s, total %d subnets ready", subnetIDs, nodeName, len(availableSubnets))
	watchers.SetNodeCondition(nodeName, watchers.CCESubnetUnavailable, corev1.ConditionFalse, watchers.ReasonSubnetsAvailable, successMsg, n.log)

	// Select the best subnet
	bestSubnet := n.searchMaxAvailableSubnet(availableSubnets)
	if bestSubnet == nil {
		msg = "unable to find best subnet for ERI creation"
		err = errors.New(msg)
		return
	}

	scopedLog = scopedLog.WithFields(logrus.Fields{
		"vpcID":      vpcID,
		"instanceID": effectiveInstanceID,
		"node":       resource.Name,
		"subnetID":   bestSubnet.Name,
	})
	scopedLog.Info("No ERI available, creating new ERI")

	interfaceNum = 1
	err = n.createERIOnCluster(ctx, scopedLog, resource, vifFeatures, vpcID, bestSubnet)

	// Set cooling time (referencing BCC mode)
	n.mutex.Lock()
	if err != nil {
		// If creation fails, set cooling time
		next := time.Now().Add(time.Second * 5) // 5 seconds cooling time
		n.nextCreateENITime = &next
	} else {
		n.nextCreateENITime = nil
	}
	n.mutex.Unlock()

	allocation.PoolID = ipamTypes.PoolID(bestSubnet.Name)
	allocation.InterfaceID = n.rdmaENIName

	return
}

// searchMaxAvailableSubnet searches for the best available subnet
func (n *rdmaNetResourceSetWrapper) searchMaxAvailableSubnet(subnets []*bcesync.BorrowedSubnet) *bcesync.BorrowedSubnet {
	var bestSubnet *bcesync.BorrowedSubnet
	for _, sbn := range subnets {
		if sbn.Spec.Exclusive {
			continue
		}
		if bestSubnet == nil || bestSubnet.BorrowedAvailableIPsCount < sbn.BorrowedAvailableIPsCount {
			bestSubnet = sbn
		}
	}
	return bestSubnet
}

// createERIOnCluster creates an ERI in the cluster
func (n *rdmaNetResourceSetWrapper) createERIOnCluster(ctx context.Context, scopedLog *logrus.Entry, resource *ccev2.NetResourceSet, vifFeatures, vpcID string, bestSubnet *bcesync.BorrowedSubnet) error {
	iaasClient := n.manager.getIaaSClient(vifFeatures)

	// First check if ERI already exists
	idToUse := n.instanceID

	if effID, e := resource.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd); e == nil {
		idToUse = effID
	} else {
		scopedLog.WithError(e).Warn("failed to get effective instance ID for ERI; falling back to base instanceID")
		return e
	}

	eriList, err := iaasClient.ListEnis(ctx, vpcID, idToUse)
	if err != nil {
		scopedLog.WithError(err).Error("Failed to list existing ERIs")
		return fmt.Errorf("failed to list existing ERIs: %w", err)
	}

	var rdmaEni *client.EniResult
	if len(eriList) == 0 {
		// 1. First build the spec for ENI CR (based on known information)
		eniSpec, err := n.buildENISpecForERI(resource, vifFeatures, vpcID, bestSubnet)
		if err != nil {
			scopedLog.WithError(err).Error("Failed to build ENI spec for ERI")
			return fmt.Errorf("failed to build ENI spec for ERI: %w", err)
		}

		// 2. Actually create the ERI, return ERI ID if successful
		scopedLog.Info("Creating new ERI via IaaS API")
		// Get effective instance ID for ERI creation
		effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
		if err != nil {
			scopedLog.WithError(err).Error("Failed to get effective instance ID for ERI creation")
			return fmt.Errorf("failed to get effective instance ID for ERI creation: %w", err)
		}
		eriID, err := n.createERI(ctx, vpcID, effectiveInstanceID, vifFeatures, bestSubnet)
		if err != nil {
			scopedLog.WithError(err).Error("Failed to create ERI")
			return fmt.Errorf("failed to create ERI: %w", err)
		}
		scopedLog.WithField("eriID", eriID).Info("Successfully created new ERI")

		// 3. Directly use the ERI ID and known information to create ENI CR, avoiding re-reading
		err = n.createENICRDirectly(ctx, scopedLog, eniSpec, eriID, resource, vifFeatures, vpcID)
		if err != nil {
			scopedLog.WithError(err).Error("Failed to create ENI CR for ERI")
			return fmt.Errorf("failed to create ENI CR for ERI: %w", err)
		}
	} else {
		// Use existing ERI (in this case, real data needs to be used)
		rdmaEni = &eriList[0]
		scopedLog.WithField("eriID", rdmaEni.Id).Info("Using existing ERI")

		err = n.createENICRForExistingERI(ctx, scopedLog, resource, rdmaEni, vifFeatures, vpcID)
		if err != nil {
			scopedLog.WithError(err).Error("Failed to create ENI CR for existing ERI")
			return fmt.Errorf("failed to create ENI CR for existing ERI: %w", err)
		}
	}

	scopedLog.Info("Successfully created ERI interface")
	return nil
}

// buildENISpecForERI builds the basic spec for ENI CR of ERI
func (n *rdmaNetResourceSetWrapper) buildENISpecForERI(resource *ccev2.NetResourceSet, vifFeatures, vpcID string, bestSubnet *bcesync.BorrowedSubnet) (*ccev2.ENISpec, error) {
	// Get effective instance ID based on instance type
	effectiveInstanceID, err := resource.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		return nil, fmt.Errorf("failed to get effective instance ID: %w", err)
	}

	return &ccev2.ENISpec{
		NodeName: resource.Name,
		Type:     ccev2.ENIForERI,
		UseMode:  ccev2.ENIUseModeSecondaryIP,
		ENI: models.ENI{
			// Fill in with known information to avoid subsequent re-reading
			VpcID:      vpcID,
			InstanceID: effectiveInstanceID,
			SubnetID:   bestSubnet.Name,
			ZoneName:   bestSubnet.Spec.AvailabilityZone,
			// ID, Name, MacAddress, PrivateIPSet will be filled after ERI creation
		},
		// Other ENI spec fields
		RouteTableOffset:          resource.Spec.ENI.RouteTableOffset,
		InstallSourceBasedRouting: resource.Spec.ENI.InstallSourceBasedRouting,
	}, nil
}

// ENICRCreateOptions encapsulates the parameters for creating ENI CR
type ENICRCreateOptions struct {
	EniID         string
	EniSpec       *ccev2.ENISpec        // Optional, create default spec if nil
	RdmaEni       *client.EniResult     // Optional, used to fill in real ERI data
	Resource      *ccev2.NetResourceSet // Used to generate OwnerReference
	VifFeatures   string
	VpcID         string
	MacAddress    string // Used to generate labelSelectorValue
	CheckExisting bool   // Whether to check existing ENI CR
}

// createENICR unified method for creating ENI CR, merges the logic of the previous three methods
func (n *rdmaNetResourceSetWrapper) createENICR(ctx context.Context, scopedLog *logrus.Entry, opts ENICRCreateOptions) error {
	// Build ENI Spec
	var finalSpec ccev2.ENISpec
	if opts.EniSpec != nil {
		finalSpec = *opts.EniSpec // Copy the passed-in spec
	} else {
		// Get effective instance ID based on instance type
		effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
		if err != nil {
			return fmt.Errorf("failed to get effective instance ID: %w", err)
		}

		// Create default spec
		finalSpec = ccev2.ENISpec{
			NodeName: n.k8sObj.Name,
			Type:     ccev2.ENIForERI,
			UseMode:  ccev2.ENIUseModeSecondaryIP,
			ENI: models.ENI{
				VpcID:      opts.VpcID,
				InstanceID: effectiveInstanceID,
			},
		}
	}

	// Fill in ENI basic information
	finalSpec.ENI.ID = opts.EniID
	finalSpec.ENI.Name = opts.EniID

	// Handle IP set and ERI detailed information
	if opts.RdmaEni != nil {
		// If there is complete ERI information, fill in the real data
		var ipv4IPSet []*models.PrivateIP
		for _, v := range opts.RdmaEni.PrivateIpSet {
			ipv4IPSet = append(ipv4IPSet, &models.PrivateIP{
				PrivateIPAddress: v.PrivateIpAddress,
				PublicIPAddress:  "",
				SubnetID:         opts.RdmaEni.SubnetID,
				Primary:          v.Primary,
			})
		}
		finalSpec.ENI.SubnetID = opts.RdmaEni.SubnetID
		finalSpec.ENI.ZoneName = opts.RdmaEni.ZoneName
		finalSpec.ENI.PrivateIPSet = ipv4IPSet
		finalSpec.ENI.IPV6PrivateIPSet = []*models.PrivateIP{}
		finalSpec.ENI.MacAddress = opts.RdmaEni.MacAddress
		finalSpec.Type = ccev2.ENIType(opts.RdmaEni.Type)
		opts.MacAddress = opts.RdmaEni.MacAddress // Use real MAC address
	} else {
		// Only ERI ID, create an empty IP set
		finalSpec.ENI.PrivateIPSet = []*models.PrivateIP{}
		finalSpec.ENI.IPV6PrivateIPSet = []*models.PrivateIP{}
		finalSpec.ENI.MacAddress = opts.MacAddress
	}

	// Generate label selector value
	getOwnerReference := func() string {
		or := opts.Resource.GetOwnerReferences()
		for _, ref := range or {
			if ref.Kind == "Node" {
				return ref.Name
			}
		}
		return ""
	}

	labelSelectorValue := bceutils.GetRdmaNrsLabelSelectorValueFromNetResourceSetName(opts.Resource.Name,
		getOwnerReference(), opts.Resource.Spec.InstanceID, "", opts.VifFeatures)

	// Check if existing ENI CR already exists (if needed)
	if opts.CheckExisting {
		existingEni, err := n.manager.eniLister.Get(opts.EniID)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to check existing ENI CR from cache: %w", err)
		}

		if err == nil && existingEni != nil {
			// ENI CR already exists, check if labelSelectorValue matches
			if existingEni.Labels[k8s.LabelNodeName] != labelSelectorValue {
				return fmt.Errorf("existing ENI CR %s has mismatched labelSelectorValue: expected %s, got %s",
					existingEni.Name, labelSelectorValue, existingEni.Labels[k8s.LabelNodeName])
			}

			// labelSelectorValue matches, set rdmaENIName and return
			n.mutex.Lock()
			n.rdmaENIName = existingEni.Name
			n.mutex.Unlock()

			scopedLog.WithField("eniCR", existingEni.Name).Info("Found existing ENI CR with matching labelSelectorValue")
			return nil
		}
	}

	// Get effective instance ID for labels
	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		return fmt.Errorf("failed to get effective instance ID for labels: %w", err)
	}

	// Create new ENI CR
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: opts.EniID,
			Labels: map[string]string{
				k8s.LabelInstanceID: effectiveInstanceID,
				k8s.LabelNodeName:   labelSelectorValue,
				k8s.LabelENIType:    n.manager.getIaaSClient(opts.VifFeatures).GetRDMAIntType(),
				k8s.VPCIDLabel:      opts.VpcID,
			},
			Annotations: map[string]string{
				k8s.AnnotationERIManaged: "true",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: ccev2.SchemeGroupVersion.String(),
				Kind:       ccev2.NRSKindDefinition,
				Name:       opts.Resource.Name,
				UID:        opts.Resource.UID,
			}},
		},
		Spec:   finalSpec,
		Status: ccev2.ENIStatus{},
	}

	// Manage creation status
	n.mutex.Lock()
	n.creatingENI = eni
	n.mutex.Unlock()

	defer func() {
		n.mutex.Lock()
		n.creatingENI = nil
		n.mutex.Unlock()
	}()

	// Create ENI CR
	createdEni, err := k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create ERI managed ENI CR: %w", err)
	}

	// Set ENI name
	n.mutex.Lock()
	n.rdmaENIName = createdEni.Name
	n.mutex.Unlock()

	scopedLog.WithField("eniCR", createdEni.Name).Info("Successfully created ENI CR")
	return nil
}

// createENICRDirectly creates ENI CR directly using ERI ID and known information
func (n *rdmaNetResourceSetWrapper) createENICRDirectly(ctx context.Context, scopedLog *logrus.Entry, eniSpec *ccev2.ENISpec, eriID string, resource *ccev2.NetResourceSet, vifFeatures, vpcID string) error {
	return n.createENICR(ctx, scopedLog, ENICRCreateOptions{
		EniID:         eriID,
		EniSpec:       eniSpec,
		Resource:      resource,
		VifFeatures:   vifFeatures,
		VpcID:         vpcID,
		MacAddress:    "", // New created ERI, MAC address temporarily empty
		CheckExisting: false,
	})
}

// createENICRForExistingERI creates ENI CR for an existing ERI
func (n *rdmaNetResourceSetWrapper) createENICRForExistingERI(ctx context.Context, scopedLog *logrus.Entry, resource *ccev2.NetResourceSet, rdmaEni *client.EniResult, vifFeatures, vpcID string) error {
	return n.createENICR(ctx, scopedLog, ENICRCreateOptions{
		EniID:         rdmaEni.Id,
		RdmaEni:       rdmaEni,
		Resource:      n.k8sObj, // Use current NRS
		VifFeatures:   n.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures],
		VpcID:         vpcID,
		CheckExisting: true, // Check existing ENI CR
	})
}

// releaseIPs implements realNodeInf
func (n *rdmaNetResourceSetWrapper) releaseIPs(ctx context.Context, iaasClient client.IaaSClient, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	if len(ipv4ToRelease) > 0 {
		eni, err := n.ensureRdmaENI()
		if err != nil {
			return err
		}
		// release ips
		err = iaasClient.BatchDeletePrivateIP(ctx, eni.Spec.ID, ipv4ToRelease)
		if err != nil {
			return fmt.Errorf("release the ips(%s) from hpc eni %s failed: %v", eni.Spec.ID, ipv4ToRelease, err)
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

	eni, err := n.ensureRdmaENI()
	if err != nil {
		return allocation, err
	}
	if eni == nil {
		allocation.AvailableInterfaces = 1
		return allocation, nil
	}

	rdmaEniQuota := n.getRdmaEniQuota()
	allocation.AvailableForAllocationIPv4 = rdmaEniQuota.GetMaxIP() - len(eni.Spec.PrivateIPSet)
	allocation.InterfaceID = eni.Name
	allocation.PoolID = ipamTypes.PoolID(eni.Spec.SubnetID)

	if n.k8sObj.Spec.ENI.InstanceType == bceutils.OverlayRDMA {
		sbn, err := n.manager.sbnLister.Get(string(allocation.PoolID))
		if err != nil {
			err = fmt.Errorf("get subnet %s failed: %v", eni.Spec.SubnetID, err)
			n.appendAllocatedIPError(eni.Name, ccev2.NewErrorStatusChange(err.Error()))
			return allocation, err
		}

		allocation.AvailableForAllocationIPv4 = math.IntMin(allocation.AvailableForAllocationIPv4, sbn.Status.AvailableIPNum)
	} else {
		allocation.AvailableForAllocationIPv4 = math.IntMin(allocation.AvailableForAllocationIPv4, n.getMaximumAllocatable(rdmaEniQuota))
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

// tryRecoverIPsFromVPC attempts to recover IPs from VPC as fallback

// this function is designed espacially for the scenario below:
// The operator has already performed IP allocation for this RDMA network interface, and IP has been allocated on the RDMA ENI on the VPC side.
// However, an unexpected operator restart (or similar process interruptions) occurred before the ENI CR on the K8S side was recorded.
// Therefore, the rdma nrs control loop of the operator will initiate another request to allocate IPs to this rdma network interface.
// At this time, due to the VPC side's regulation that a single rdma network interface can only mount 13 IPs (which have already been mounted),
// this request to mount IPs will fail, resulting in the continued inability of this nrs to be ready.
// This function is used as a fallback mechanism when batch allocation fails due to operator restart.
// It directly queries VPC to get the current state of the ENI and returns whatever IPs are found.
// The caller is responsible for determining whether the recovered IPs are sufficient.
func (n *rdmaNetResourceSetWrapper) tryRecoverIPsFromVPC(ctx context.Context, scopedLog *logrus.Entry, eni *ccev2.ENI, allocation *ipam.AllocationAction) ([]*models.PrivateIP, error) {
	// For RDMA ENI, use the appropriate IaaS client to list ENIs and find the matching one
	vifFeatures := n.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]
	iaasClient := n.manager.getIaaSClient(vifFeatures)

	// List ENIs for this instance and find the matching one by ID
	idToUse := n.instanceID

	if effID, e := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd); e == nil {
		idToUse = effID
	} else {
		scopedLog.WithError(e).Warn("failed to get effective instance ID for ERI")
		return nil, e
	}

	eniList, err := iaasClient.ListEnis(ctx, eni.Spec.VpcID, idToUse)
	if err != nil {
		scopedLog.WithError(err).Warning("Failed to list ENIs from VPC during recovery")
		return nil, err
	}

	// Find the matching ENI by ID
	var matchedEni *client.EniResult
	for i := range eniList {
		if eniList[i].Id == eni.Spec.ID {
			matchedEni = &eniList[i]
			break
		}
	}

	if matchedEni == nil {
		scopedLog.Warning("ENI not found in VPC during recovery")
		return nil, fmt.Errorf("eni %s not found in vpc", eni.Spec.ID)
	}

	scopedLog.WithField("vpcIPCount", len(matchedEni.PrivateIpSet)).Info("Querying VPC for existing IPs on RDMA ENI")

	// Convert all VPC IPs to models.PrivateIP - return whatever is found
	var recoveredIPs []*models.PrivateIP
	var ipAddresses []string
	for _, vpcIP := range matchedEni.PrivateIpSet {
		recoveredIPs = append(recoveredIPs, &models.PrivateIP{
			PrivateIPAddress: vpcIP.PrivateIpAddress,
			SubnetID:         string(allocation.PoolID),
			Primary:          vpcIP.Primary,
		})
		ipAddresses = append(ipAddresses, vpcIP.PrivateIpAddress)
	}

	if len(recoveredIPs) > 0 {
		scopedLog.WithFields(logrus.Fields{
			"recoveredIPCount": len(recoveredIPs),
			"recoveredIPs":     ipAddresses,
		}).Info("Found existing IPs in VPC during recovery")
	} else {
		scopedLog.Info("No existing IPs found in VPC during recovery")
	}

	return recoveredIPs, nil
}

// isERIManagedNRS checks whether the current NRS is an ERI managed NRS
func (n *rdmaNetResourceSetWrapper) isERIManagedNRS() bool {
	// Check if the NRS has the annotation for ERI management
	if value, exists := n.k8sObj.Annotations[k8s.AnnotationERIManaged]; exists && value == "true" {
		return true
	}
	return false
}

// handleERIManagedNRS handles the special logic for ERI managed NRS
func (n *rdmaNetResourceSetWrapper) handleERIManagedNRS() (*ccev2.ENI, error) {
	// Get VIF features
	vifFeatures := n.bceRDMANetResourceSet.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]
	if vifFeatures != "elastic_rdma" {
		return nil, fmt.Errorf("ERI managed NRS must have elastic_rdma vif features")
	}

	// Generate label selector value
	getOwnerReference := func() string {
		or := n.k8sObj.GetOwnerReferences()
		for _, ref := range or {
			if ref.Kind == "Node" {
				return ref.Name
			}
		}
		return ""
	}

	labelSelectorValue := bceutils.GetRdmaNrsLabelSelectorValueFromNetResourceSetName(n.k8sObj.Name,
		getOwnerReference(), n.k8sObj.Spec.InstanceID, "", vifFeatures)

	// First check if the corresponding ENI CR already exists
	requirement := metav1.LabelSelectorRequirement{
		Key:      k8s.LabelNodeName,
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{labelSelectorValue},
	}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{requirement},
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to create label selector: %v", err)
	}

	k8sRdmaEnis, err := n.manager.eniLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list enis: %w", err)
	}

	// If ENI CR already exists, return directly
	if len(k8sRdmaEnis) > 0 {
		existingEni := k8sRdmaEnis[0]
		n.rdmaENIName = existingEni.Name
		n.log.Infof("Found existing ERI managed ENI %s", existingEni.Name)
		return existingEni, nil
	}
	return nil, nil
}

// createERI creates an ERI interface and returns the created ERI ID
func (n *rdmaNetResourceSetWrapper) createERI(ctx context.Context, vpcID, instanceID, vifFeatures string, bestSubnet *bcesync.BorrowedSubnet) (string, error) {
	// 1. Generate ERI name (referencing ENI naming rules)
	eriName := n.createNameForERI(option.Config.ClusterID, instanceID, n.k8sObj.Name)

	// 2. Construct ERI creation parameters (referencing ENI creation parameters)
	createArgs := &enisdk.CreateEniArgs{
		Name:                        eriName,
		SubnetId:                    bestSubnet.Name,
		InstanceId:                  instanceID,
		SecurityGroupIds:            n.k8sObj.Spec.ENI.SecurityGroups,
		EnterpriseSecurityGroupIds:  n.k8sObj.Spec.ENI.EnterpriseSecurityGroupList,
		Description:                 defaults.DefaultENIDescription,
		NetworkInterfaceTrafficMode: "highPerformance", // Specify as ERI type (highPerformance means creating ERI, standard means creating ENI)
		PrivateIpSet: []enisdk.PrivateIp{{
			Primary: true,
		}},
	}

	// Ensure enterprise security group ID is not nil
	if createArgs.EnterpriseSecurityGroupIds == nil {
		createArgs.EnterpriseSecurityGroupIds = []string{}
	}

	// Get effective instance ID based on instance type
	effectiveInstanceID, err := n.k8sObj.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd)
	if err != nil {
		return "", fmt.Errorf("failed to get effective instance ID: %w", err)
	}

	// Update createArgs with effective instance ID
	createArgs.InstanceId = effectiveInstanceID

	// Generate client token key for ERI creation
	clientTokenKey := bceutils.GenerateENIClientTokenKey(
		effectiveInstanceID,
		bestSubnet.Name,
		createArgs.SecurityGroupIds,
		createArgs.EnterpriseSecurityGroupIds,
	)
	clientToken := n.clientTokens.GetOrCreateClientToken(clientTokenKey)
	createArgs.ClientToken = clientToken

	// 3. Call cloud API to create ERI
	scopedLog := n.log.WithFields(logrus.Fields{
		"eriName":        eriName,
		"subnetID":       bestSubnet.Name,
		"instanceID":     instanceID,
		"vpcID":          vpcID,
		"securityGroups": createArgs.SecurityGroupIds,
		"clientTokenKey": clientTokenKey,
		"clientToken":    clientToken,
	})

	scopedLog.Info("Creating ERI via cloud API")

	var eriID string
	nrsType, err := bceutils.GetNrsTypeByInstanceID(n.instanceID)
	// HPAS 标准方案则使用bceClient + hpasid创建ENI，
	//否则使用hpasClient带 header + instance-id创建ENI
	if err != nil {
		return "", fmt.Errorf("failed to get NRS type: %w", err)
	}
	if nrsType == string(metadata.InstanceTypeExHPAS) &&
		!operatorOption.Config.HPASStanderd {
		eriID, err = n.manager.hpasClient.CreateENI(ctx, createArgs)
	} else {
		eriID, err = n.manager.bceClientd.CreateENI(ctx, createArgs)
	}

	if err != nil {
		scopedLog.WithError(err).Error("Failed to create ERI")
		// Keep clientToken in map for retry with same token
		return "", fmt.Errorf("failed to create ERI: %w", err)
	}

	// Remove clientToken after successful creation
	n.clientTokens.RemoveClientToken(clientTokenKey)

	scopedLog.WithField("eriID", eriID).Info("ERI created successfully")
	return eriID, nil
}

// createNameForERI creates a name for ERI (referencing ENI naming rules)
func (n *rdmaNetResourceSetWrapper) createNameForERI(clusterID, instanceID, nodeName string) string {
	hash := sha1.Sum([]byte(time.Now().String()))
	suffix := hex.EncodeToString(hash[:])
	// ERI name length is 64, similar to ENI
	name := fmt.Sprintf("%s/%s/%s-eri", clusterID, instanceID, nodeName)
	if len(name) > 57 {
		name = name[:57]
	}
	return fmt.Sprintf("%s/%s", name, suffix[:6])
}

// deleteERI deletes ERI (referencing ENI deletion logic)
func (n *rdmaNetResourceSetWrapper) deleteERI(ctx context.Context, eriID string, scopedLog *logrus.Entry) {
	nrsType, err := bceutils.GetNrsTypeByInstanceID(n.instanceID)
	if err != nil {
		scopedLog.WithError(err).Error("Failed to get NRS type")
		return
	}
	// HPAS 非标方案带 header，使用hpasClient删除ENI
	if nrsType == string(metadata.InstanceTypeExHPAS) &&
		!operatorOption.Config.HPASStanderd {
		err = n.manager.hpasClient.DeleteENI(ctx, eriID)
	} else {
		err = n.manager.bceClientd.DeleteENI(ctx, eriID)
	}
	if err != nil {
		scopedLog.WithField("eriID", eriID).
			WithError(err).Error("Failed to delete ERI")
	} else {
		scopedLog.WithField("eriID", eriID).
			Info("Successfully deleted ERI")
	}
}
