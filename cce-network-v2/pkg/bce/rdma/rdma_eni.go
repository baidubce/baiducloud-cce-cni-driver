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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
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
func (m *rdmaInstancesManager) ForeachInstance(instanceID, nodeName string, fn ipamTypes.InterfaceIterator) error {
	m.mutex.Lock()
	n := m.nodeMap[nodeName]
	m.mutex.Unlock()
	getOwnerReference := func() string {
		or := n.k8sObj.GetOwnerReferences()
		for _, ref := range or {
			if ref.Kind == "Node" {
				return ref.Name
			}
		}
		return ""
	}
	// the macAddress and vifFeatures is decided by the NetResourceSet's annotation
	macAddress := n.k8sObj.Annotations[k8s.AnnotationRDMAInfoMacAddress]
	vifFeatures := n.k8sObj.Annotations[k8s.AnnotationRDMAInfoVifFeatures]
	// a labelSelectorValue's max length is 63 in kubernetes, so if nodeName's length is more than the max length like this:
	// 63 - len(string("-fa2700078302-elasticrdma")), we need to use node's InstanceID as node's identification to generate labelSelectorValue
	labelSelectorValue := bceutils.GetRdmaNrsLabelSelectorValueFromNetResourceSetName(n.k8sObj.Name,
		getOwnerReference(), n.k8sObj.Spec.InstanceID, macAddress, vifFeatures)
	// Select only the ENI of the local node's rdma interface
	selector, err := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelInstanceID: instanceID,
		k8s.LabelNodeName:   labelSelectorValue,
	}))
	if err != nil {
		panic(fmt.Errorf("failed to create label selector: %v", err))
	}
	enis, err := m.eniLister.List(selector)
	if err != nil {
		return fmt.Errorf("list ENIs failed: %w", err)
	}
	for i := 0; i < len(enis); i++ {
		eni := enis[i]
		vpcStatus := eni.Status.VPCStatus
		// Do not process the RDMA ENI which is being deleted or in attaching/detaching status.
		// It is not useful to process it, because the IPs are not assigned to the RDMA ENI.
		if enis[i].DeletionTimestamp != nil || vpcStatus == ccev2.VPCENIStatusAttaching ||
			vpcStatus == ccev2.VPCENIStatusDetaching || vpcStatus == ccev2.VPCENIStatusDeleted {
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
func (n *bceRDMANetResourceSet) waitForENISynced(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
