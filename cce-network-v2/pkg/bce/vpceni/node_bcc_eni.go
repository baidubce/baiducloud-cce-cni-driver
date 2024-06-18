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
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
)

const (
	eniSyncPeriod = 500 * time.Millisecond
	// max time to wait for creating a eni
	maxENISyncDuration = 60 * time.Second
	// create eni jitter back off time
	eniCreateJitter = 1 * time.Second
	// max create eni back off time
	maxENICreateBackoff = 20
)

type eniResource ccev2.ENI

// InterfaceID must return the identifier of the interface
func (eni *eniResource) InterfaceID() string {
	return eni.Spec.ENI.ID
}

// ForeachAddress must iterate over all addresses of the interface and
// call fn for each address
func (eni *eniResource) ForeachAddress(instanceID string, fn ipamTypes.AddressIterator) error {
	interfaceID := eni.Spec.ENI.ID

	// ipv4
	for i := 0; i < len(eni.Spec.ENI.PrivateIPSet); i++ {
		addr := eni.Spec.ENI.PrivateIPSet[i]
		err := fn(instanceID, interfaceID, addr.PrivateIPAddress, addr.SubnetID, &addr)
		if err != nil {
			return err
		}
	}

	// ipv6
	for i := 0; i < len(eni.Spec.ENI.IPV6PrivateIPSet); i++ {
		addr := eni.Spec.ENI.IPV6PrivateIPSet[i]
		err := fn(instanceID, interfaceID, addr.PrivateIPAddress, addr.SubnetID, &addr)
		if err != nil {
			return err
		}
	}

	return nil
}

// ForeachInstance will iterate over each instance inside `instances`, and call
// `fn`. This function is read-locked for the entire execution.
func (m *InstancesManager) ForeachInstance(instanceID, nodeName string, fn ipamTypes.InterfaceIterator) error {
	// Select only the ENI of the local node
	selector, _ := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelInstanceID: instanceID,
		k8s.LabelNodeName:   nodeName,
	}))
	enis, err := m.enilister.List(selector)
	if err != nil {
		return fmt.Errorf("list ENIs failed: %w", err)
	}
	for i := 0; i < len(enis); i++ {
		if enis[i].DeletionTimestamp != nil || enis[i].Status.VPCStatus == ccev2.VPCENIStatusDeleted {
			continue
		}
		fn(instanceID, enis[i].Spec.ENI.ID, ipamTypes.InterfaceRevision{
			Resource: (*eniResource)(enis[i]),
		})
	}
	return nil
}

// waitForENISynced wait for eni synced
// this method should not lock the mutex of bceNode before calling
func (n *bceNode) waitForENISynced(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	wait.PollImmediateUntilWithContext(ctx, 200*time.Millisecond, func(ctx context.Context) (done bool, err error) {
		haveSynced := true
		n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
			func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
				e, ok := iface.Resource.(*eniResource)
				if !ok {
					return nil
				}
				n.mutex.Lock()
				if version, ok := n.expiredVPCVersion[interfaceID]; ok {
					if e.Spec.VPCVersion == version {
						haveSynced = false
					} else {
						delete(n.expiredVPCVersion, interfaceID)
					}
				}
				n.mutex.Unlock()

				return nil
			})
		return haveSynced, nil
	})

}

// CreateInterface create a new ENI
func (n *bccNode) createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	n.mutex.RLock()
	resource := n.k8sObj
	n.mutex.RUnlock()

	if n.nextCreateENITime != nil && n.nextCreateENITime.After(time.Now()) {
		return
	}

	var (
		eniQuota          = n.bceNode.getENIQuota()
		availableENICount = 0
		inuseENICount     = 0
	)
	n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
		func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
			e, ok := iface.Resource.(*eniResource)
			if !ok {
				return nil
			}
			availableENICount++
			if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
				// if use mode of ENI is primary, then counting available or inuse ENIs
				if e.Status.CCEStatus == ccev2.ENIStatusUsingInPod {
					inuseENICount++
				}
			} else if n.k8sObj.Spec.ENI.UseMode == string(ccev2.ENIUseModeSecondaryIP) {
				// if the length of private ip set is greater than the MaxIPPerENI, then the ENI is in use
				if len(e.Spec.ENI.PrivateIPSet) >= eniQuota.GetMaxIP() {
					inuseENICount++
				}
			}

			return nil
		})

	if availableENICount >= eniQuota.GetMaxENI() {
		msg = errUnableToDetermineLimits
		err = fmt.Errorf(msg)
		return
	}

	// The cache will be cleared in the function ResyncInterfacesAndIPs.
	if n.k8sObj.Spec.ENI.PreAllocateENI <= availableENICount && len(n.creatingEni.creatingENI) > 0 {
		msg = errUnableToCreateENI
		err = errors.New(errCrurrentlyCreatingENI)
		return
	}

	// find the bset subnet to create eni
	bestSubnet := searchMaxAvailableSubnet(n.availableSubnets)

	if bestSubnet == nil {
		msg = errUnableToFindSubnet
		err = fmt.Errorf(
			"no matching subnet available for interface creation (VPC=%s AZ=%s SubnetIDs=%v)",
			resource.Spec.ENI.VpcID,
			resource.Spec.ENI.AvailabilityZone,
			resource.Spec.ENI.SubnetIDs,
		)
		return
	}

	scopedLog = scopedLog.WithFields(logrus.Fields{
		"subnetID": bestSubnet.Name,
		"node":     resource.Name,
	})
	scopedLog.Info("No more IPs available, creating new ENI")

	interfaceNum = 1
	err = n.createENIOnCluster(ctx, scopedLog, resource, bestSubnet)

	n.mutex.Lock()
	if err != nil {
		next := time.Now().Add(wait.Jitter(eniCreateJitter, maxENICreateBackoff))
		n.nextCreateENITime = &next
	} else {
		n.nextCreateENITime = nil
	}
	n.mutex.Unlock()

	return
}

// createENIOnCluster The ENI object is created in the cluster and the ENI Synchronizer
// will automatically create the ENI object from VPC.
// The ENI object is created successfully and the node will record a status of “creating ENI”.
func (n *bccNode) createENIOnCluster(ctx context.Context, scopedLog *logrus.Entry, resource *ccev2.NetResourceSet, subnet *ccev1.Subnet) error {
	eniName := CreateNameForENI(option.Config.ClusterID, n.instanceID, resource.Name)

	newENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				k8s.LabelInstanceID: n.instanceID,
				k8s.LabelNodeName:   resource.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: ccev2.SchemeGroupVersion.String(),
				Kind:       ccev2.NRSKindDefinition,
				Name:       resource.Name,
				UID:        resource.UID,
			}},
		},
		Spec: ccev2.ENISpec{
			NodeName: resource.Name,
			UseMode:  ccev2.ENIUseMode(resource.Spec.ENI.UseMode),
			ENI: models.ENI{
				Name:                       eniName,
				ZoneName:                   subnet.Spec.AvailabilityZone,
				InstanceID:                 n.instanceID,
				VpcID:                      subnet.Spec.VPCID,
				SubnetID:                   subnet.Name,
				SecurityGroupIds:           resource.Spec.ENI.SecurityGroups,
				EnterpriseSecurityGroupIds: resource.Spec.ENI.EnterpriseSecurityGroupList,
			},
			RouteTableOffset:          resource.Spec.ENI.RouteTableOffset,
			InstallSourceBasedRouting: resource.Spec.ENI.InstallSourceBasedRouting,
			Type:                      ccev2.ENIType(resource.Spec.ENI.InstanceType),
		},
	}

	scopedLog = scopedLog.WithField("eniName", eniName).WithField("securityGroupIDs", resource.Spec.ENI.SecurityGroups)
	eniID, err := n.createENI(ctx, newENI, scopedLog)
	if err != nil {
		n.eventRecorder.Eventf(resource, corev1.EventTypeWarning, "FailedCreateENI", "failed to create ENI on nrs %s: %s", resource.Name, err)
		return err
	}
	n.eventRecorder.Eventf(resource, corev1.EventTypeNormal, "CreateENISuccess", "create new ENI %s on nrs %s success", eniID, resource.Name)
	scopedLog = scopedLog.WithField("eniID", eniID)
	newENI.Spec.ENI.ID = eniID
	newENI.Name = eniID

	n.creatingEni.addCreatingENI(newENI.Name, time.Now())

	_, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, newENI, metav1.CreateOptions{})
	if err != nil {
		n.creatingEni.removeCreatingENI(eniID)
		return fmt.Errorf("failed to create ENI: %w", err)
	}

	if err = wait.PollImmediateWithContext(ctx, eniSyncPeriod, maxENISyncDuration, func(context.Context) (done bool, err error) {
		ret, err := n.manager.enilister.Get(newENI.Name)
		if err != nil || ret == nil {
			return false, nil
		}
		if ret.Status.CCEStatus != ccev2.ENIStatusPending {
			return true, nil
		}
		return false, nil
	}); err != nil {
		scopedLog.WithError(err).Errorf("wait ENI to be ready failed")
	}
	scopedLog.Debugf("wait ENI to be ready")
	return err
}

// CreateNameForENI creates name for newly created eni
func CreateNameForENI(clusterID, instanceID, nodeName string) string {
	hash := sha1.Sum([]byte(time.Now().String()))
	suffix := hex.EncodeToString(hash[:])

	// eni name length is 64
	name := fmt.Sprintf("%s/%s/%s", clusterID, instanceID, nodeName)
	if len(name) > 57 {
		name = name[:57]
	}
	return fmt.Sprintf("%s/%s", name, suffix[:6])
}

// createENI create ENI with a given name and param
func (n *bceNode) createENI(ctx context.Context, resource *ccev2.ENI, scopedLog *logrus.Entry) (string, error) {
	createENIArgs := &enisdk.CreateEniArgs{
		Name:                       resource.Spec.ENI.Name,
		SubnetId:                   resource.Spec.ENI.SubnetID,
		InstanceId:                 resource.Spec.ENI.InstanceID,
		SecurityGroupIds:           resource.Spec.ENI.SecurityGroupIds,
		EnterpriseSecurityGroupIds: resource.Spec.ENI.EnterpriseSecurityGroupIds,
		Description:                defaults.DefaultENIDescription,
		PrivateIpSet: []enisdk.PrivateIp{{
			Primary: true,
		}},
	}
	if createENIArgs.EnterpriseSecurityGroupIds == nil {
		createENIArgs.EnterpriseSecurityGroupIds = []string{}
	}

	// use the given ip to create a new ENI
	var privateIPs []enisdk.PrivateIp
	for i := 0; i < len(resource.Spec.ENI.PrivateIPSet); i++ {
		privateIPs = append(privateIPs, enisdk.PrivateIp{
			PublicIpAddress:  resource.Spec.ENI.PrivateIPSet[i].PublicIPAddress,
			Primary:          resource.Spec.ENI.PrivateIPSet[i].Primary,
			PrivateIpAddress: resource.Spec.ENI.PrivateIPSet[i].PrivateIPAddress,
		})
	}
	if len(privateIPs) == 0 {
		privateIPs = append(privateIPs, enisdk.PrivateIp{
			Primary: true,
		})
	}
	createENIArgs.PrivateIpSet = privateIPs

	eniID, err := n.manager.bceclient.CreateENI(ctx, createENIArgs)
	if err != nil {
		scopedLog.WithField("request", logfields.Json(createENIArgs)).
			WithContext(ctx).
			WithError(err).Errorf("create eni failed")
		return "", err
	}
	scopedLog.WithField("request", logfields.Json(createENIArgs)).
		WithContext(ctx).Debugf("sync eni %s success", resource.Name)
	return eniID, nil
}
