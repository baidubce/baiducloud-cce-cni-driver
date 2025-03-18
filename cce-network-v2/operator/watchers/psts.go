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

package watchers

import (
	"context"
	"fmt"
	"reflect"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	cce_lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

var (
	pstsClient = &pstsUpdaterImpl{}
	pstsLog    = logging.NewSubysLogger("psts-watcher")
)

// PSTSEventHandler should implement the behavior to handle PSTS
type PSTSEventHandler interface {
	Update(resource *ccev2.PodSubnetTopologySpread) error
	Delete(namespace, pstsName string) error
}

func StartSynchronizingPSTS(ctx context.Context) error {
	var (
		pstsManagerSyncHandler func(key string) error
		pstsManager            = &pstsSyncher{}
	)

	log.Info("Starting to synchronize PSTS custom resources")

	// TODO: The operator is currently storing a full copy of the
	// PSTS resource, as the resource grows, we may want to consider
	// introducing a slim version of it.
	PSTSLister := k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister()

	if pstsManagerSyncHandler == nil {
		pstsManagerSyncHandler = func(key string) error {
			namespace, name, err := cache.SplitMetaNamespaceKey(key)
			if err != nil {
				log.WithError(err).Error("Unable to process PSTS event")
				return err
			}
			obj, err := PSTSLister.PodSubnetTopologySpreads(namespace).Get(name)

			// Delete handling
			if errors.IsNotFound(err) ||
				(obj != nil && obj.DeletionTimestamp != nil) {
				return pstsManager.Delete(namespace, name)
			}
			if err != nil {
				log.WithError(err).Warning("Unable to retrieve PSTS from watcher store")
				return err
			}
			return pstsManager.Update(obj)
		}
	}

	controller := cm.NewResyncController("cce-psts-controller", int(operatorOption.Config.ResourceResyncWorkers), k8s.GetQPS(), k8s.GetBurst(),
		k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Informer(), pstsManagerSyncHandler)
	controller.RunWithResync(operatorOption.Config.ResourceResyncInterval)
	return nil
}

type pstsUpdaterImpl struct {
}

func (c *pstsUpdaterImpl) Lister() cce_lister.PodSubnetTopologySpreadLister {
	return k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister()
}

func (c *pstsUpdaterImpl) Create(psts *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(psts.Namespace).Create(context.TODO(), psts, metav1.CreateOptions{})
}

func (c *pstsUpdaterImpl) Update(newResource *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(newResource.Namespace).Update(context.TODO(), newResource, metav1.UpdateOptions{})
}

func (c *pstsUpdaterImpl) Delete(namespace, name string) error {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (c *pstsUpdaterImpl) UpdateStatus(newResource *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
	return k8s.CCEClient().CceV2().PodSubnetTopologySpreads(newResource.Namespace).UpdateStatus(context.TODO(), newResource, metav1.UpdateOptions{})
}

type pstsSyncher struct {
	recorder record.EventRecorder
}

func (syncher *pstsSyncher) Update(resource *ccev2.PodSubnetTopologySpread) error {
	var (
		err       error
		scopedLog = pstsLog.WithFields(logrus.Fields{
			"namespace": resource.Namespace,
			"name":      resource.Name,
		})
		sbn *ccev1.Subnet

		// new status to be updated to PSTS
		newStatus = ccev2.PodSubnetTopologySpreadStatus{
			Name:               resource.Name,
			AvailableSubnets:   make(map[string]ccev2.SubnetPodStatus),
			UnavailableSubnets: make(map[string]ccev2.SubnetPodStatus),
		}

		// selector is used to select pods that are matched by the PSTS
		selector         labels.Selector      = labels.Everything()
		matchedEndpoints []*ccev2.CCEEndpoint = make([]*ccev2.CCEEndpoint, 0)
	)

	isExclusive := resource.Spec.Strategy != nil && resource.Spec.Strategy.EnableReuseIPAddress
	if resource.Spec.Selector != nil {
		selector = labels.Set(resource.Spec.Selector.MatchLabels).AsSelector()
	}

	allEndpoints, err := CCEEndpointClient.Lister().List(selector)
	if err != nil {
		scopedLog.Errorf("Failed to list endpoints: %v", err)
		allEndpoints = []*ccev2.CCEEndpoint{}
	}
	for i := range allEndpoints {
		if allEndpoints[i].Spec.Network.IPAllocation != nil && allEndpoints[i].Spec.Network.IPAllocation.PSTSName == resource.Name {
			matchedEndpoints = append(matchedEndpoints, allEndpoints[i])
		}
	}
	newStatus.PodMatchedCount = int32(len(allEndpoints))
	newStatus.PodAffectedCount = int32(len(matchedEndpoints))

	for sbnID := range resource.Spec.Subnets {
		sbn, err = CCESubnetClient.Lister().Get(sbnID)
		if k8serrors.IsNotFound(err) {
			sbn, err = CCESubnetClient.EnsureSubnet("", sbnID, isExclusive)
			if err != nil {
				newStatus.UnavailableSubnets[sbnID] = ccev2.SubnetPodStatus{
					Message: fmt.Sprintf("Failed to ensure subnet %s: %v", sbnID, err),
				}
				continue
			}
		}
		if sbn == nil {
			newStatus.UnavailableSubnets[sbnID] = ccev2.SubnetPodStatus{
				Message: fmt.Sprintf("Subnet %s not found", sbnID),
			}
			continue
		}

		// fill in subnet status
		subnetStatus := ccev2.SubnetPodStatus{
			SubenetDetail: ccev2.SubenetDetail{
				AvailableIPNum:   sbn.Status.AvailableIPNum,
				Enable:           sbn.Status.Enable,
				HasNoMoreIP:      sbn.Status.HasNoMoreIP,
				ID:               sbn.Spec.ID,
				Name:             sbn.Spec.Name,
				AvailabilityZone: sbn.Spec.AvailabilityZone,
				CIDR:             sbn.Spec.CIDR,
				IPv6CIDR:         sbn.Spec.IPv6CIDR,
			},
			IPAllocations: make(map[string]string),
		}
		for _, ep := range matchedEndpoints {
			if ep.Status.Networking != nil {
				for _, addr := range ep.Status.Networking.Addressing {
					if addr.Subnet == sbnID {
						subnetStatus.IPAllocations[addr.IP] = ep.Namespace + "/" + ep.Name
					}
				}
			}
		}
		subnetStatus.PodCount = int32(len(subnetStatus.IPAllocations))

		if !sbn.Status.Enable || sbn.Status.AvailableIPNum == 0 {
			newStatus.UnavailableSubnets[sbnID] = subnetStatus
		} else {
			newStatus.AvailableSubnets[sbnID] = subnetStatus
		}
	}
	newStatus.SchedulableSubnetsNum = int32(len(newStatus.AvailableSubnets))
	newStatus.UnSchedulableSubnetsNum = int32(len(newStatus.UnavailableSubnets))
	if !reflect.DeepEqual(resource.Status, newStatus) {
		resource.Status = newStatus
		_, err = pstsClient.UpdateStatus(resource)
		if err != nil {
			scopedLog.Errorf("Failed to update status: %v", err)
		}
	}
	return nil
}

func (p *pstsSyncher) Delete(namespace, pstsName string) error {
	return nil
}
