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
package endpoint

import (
	"context"
	"fmt"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// GetEndpointCrossCache try get endpoint from cache
// if endpoint is missing the try get from apiserver
func GetEndpointCrossCache(ctx context.Context, cceEndpointClient *watchers.CCEEndpointClient, namespace string, name string) (*ccev2.CCEEndpoint, error) {
	oldEP, err := cceEndpointClient.Get(namespace, name)

	if kerrors.IsNotFound(err) {
		return nil, nil
	}
	return oldEP, err
}

// NewEndpointTemplate create a Elastic CCE Endpoint
func NewEndpointTemplate(containerID, netnsPath string, pod *corev1.Pod) *ccev2.CCEEndpoint {
	namespace := pod.Namespace
	name := pod.Name
	newEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    pod.Labels,
		},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sNamespace: namespace,
				K8sPodName:   name,
				ContainerID:  containerID,
				K8sObjectID:  string(pod.UID),
				PodName:      name,
				Netns:        netnsPath,
			},
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{
					DirectIPAllocation: ccev2.DirectIPAllocation{
						Type:            ccev2.IPAllocTypeElastic,
						ReleaseStrategy: ccev2.ReleaseStrategyTTL,
					},
					NodeIP: nodeTypes.GetName(),
				},
			},
		},
	}
	if newEP.Labels == nil {
		newEP.Labels = make(map[string]string)
	}
	newEP.Labels[k8s.LabelNodeName] = nodeTypes.GetName()
	return newEP
}

// IsSameContainerID checks if two container ids are the same or not
func IsSameContainerID(ep *ccev2.CCEEndpoint, containerID string) bool {
	return ep != nil &&
		ep.Spec.ExternalIdentifiers != nil &&
		ep.Spec.ExternalIdentifiers.ContainerID == containerID
}

// AppendEndpointStatus appends node status to given pod endpoint status
func AppendEndpointStatus(newStatus *ccev2.EndpointStatus, status models.EndpointState, code string) bool {
	update := newStatus.State != string(status)
	// update state and log
	newStatus.State = string(status)
	newLog := &models.EndpointStatusChange{
		Code:      code,
		State:     status,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if len(newStatus.Log) == 0 {
		newStatus.Log = append(newStatus.Log, newLog)
		return update
	}
	if len(newStatus.Log) > 8 {
		newStatus.Log = newStatus.Log[5:]
		update = true
	}
	lastLog := newStatus.Log[len(newStatus.Log)-1]

	if lastLog.State != status || lastLog.Code != code {
		newStatus.Log = append(newStatus.Log, newLog)
	} else {
		newStatus.Log[len(newStatus.Log)-1] = newLog
		update = true
	}
	return update
}

func IsFixedIPEndpoint(resource *ccev2.CCEEndpoint) bool {
	return resource.Spec.Network.IPAllocation != nil && resource.Spec.Network.IPAllocation.Type == ccev2.IPAllocTypeFixed
}

func IsPSTSEndpoint(resource *ccev2.CCEEndpoint) bool {
	return resource.Spec.Network.IPAllocation != nil && resource.Spec.Network.IPAllocation.PSTSName != ""
}

// DeleteEndpointAndWait delete endpoint and wait for it to be deleted
func DeleteEndpointAndWait(ctx context.Context, cceEndpointClient *watchers.CCEEndpointClient, oldEP *ccev2.CCEEndpoint) (err error) {
	if oldEP == nil {
		return nil
	}
	err = cceEndpointClient.CCEEndpoints(oldEP.Namespace).Delete(ctx, oldEP.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete endpoint error: %w", err)
	}
	err = wait.PollImmediateUntilWithContext(ctx, time.Second/2, func(context.Context) (done bool, err error) {
		ep, err := cceEndpointClient.Get(oldEP.Namespace, oldEP.Name)
		if !kerrors.IsNotFound(err) || ep != nil {
			return false, nil
		}
		return true, nil
	})
	return err
}

func ConverteIPAllocation2EndpointAddress(result *ipam.AllocationResult, family ccev2.IPFamily) *ccev2.AddressPair {
	if result == nil {
		return nil
	}
	return &ccev2.AddressPair{
		IP:        result.IP.String(),
		Family:    family,
		Interface: result.InterfaceNumber,
		Gateway:   result.GatewayIP,
		CIDRs:     result.CIDRs,
	}
}
