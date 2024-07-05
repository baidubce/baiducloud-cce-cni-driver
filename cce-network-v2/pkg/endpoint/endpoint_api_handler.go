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
	"reflect"
	"strconv"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/bandwidth"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/qos"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint/event"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	apiHandlerLog = logging.NewSubysLogger("endpoint api handler")
)

// EndpointAPIHandler is the handler for the endpoint API.
type EndpointAPIHandler struct {
	cceEndpointClient *watchers.CCEEndpointClient
}

func NewEndpointAPIHandler(watcher *watchers.K8sWatcher) *EndpointAPIHandler {
	return &EndpointAPIHandler{
		cceEndpointClient: watcher.NewCCEEndpointClient(),
	}
}

// GetEndpointExtpluginStatus returns the status of the extplugin.
// this method will block until all external plugin status have been ready.
func (handler *EndpointAPIHandler) GetEndpointExtpluginStatus(ctx context.Context, namespace, name, containerID string) (models.ExtFeatureData, error) {
	var (
		err            error
		log            = apiHandlerLog.WithField("method", "GetEndpointExtpluginStatus")
		extFeatureData = make(models.ExtFeatureData)

		specid   string
		statusid string
	)

	err = wait.PollImmediateUntilWithContext(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		cep, err := handler.cceEndpointClient.Get(namespace, name)
		if err != nil {
			return false, err
		}

		// check containerID
		if cep.Spec.ExternalIdentifiers != nil {
			specid = cep.Spec.ExternalIdentifiers.ContainerID
		}
		if specid != containerID {
			return false, fmt.Errorf("containerID not match, specid: %s, containerID: %s", specid, containerID)
		}
		if cep.Status.ExternalIdentifiers != nil {
			statusid = cep.Status.ExternalIdentifiers.ContainerID
		}
		if specid != statusid {
			return false, nil
		}

		// check extFeatureStatus
		if len(cep.Spec.ExtFeatureGates) == 0 {
			return true, nil
		}
		if len(cep.Spec.ExtFeatureGates) != len(cep.Status.ExtFeatureStatus) {
			return false, nil
		}

		for _, extFeature := range cep.Spec.ExtFeatureGates {
			if extStatus, ok := cep.Status.ExtFeatureStatus[extFeature]; ok {
				if !extStatus.Ready || extStatus.ContainerID != containerID {
					return false, nil
				}
				extFeatureData[extFeature] = extStatus.Data
			}
		}
		return true, nil
	})
	if err != nil {
		log.WithError(err).Error("get endpoint extplugin status failed")
		return nil, fmt.Errorf("get endpoint extplugin status failed: %v", err)
	}
	if len(extFeatureData) > 0 {
		log.WithField("extFeatureData", extFeatureData).Info("get endpoint extplugin status success")
	}
	return extFeatureData, nil
}

func (handler *EndpointAPIHandler) PutEndpointProbe(ctx context.Context, namespace, name, containerID, driver string) (*models.EndpointProbeResponse, error) {
	var (
		err error
		cep *ccev2.CCEEndpoint

		log    = apiHandlerLog.WithField("method", "PutEndpointProbe")
		result = &models.EndpointProbeResponse{}
	)

	err = wait.PollImmediateUntilWithContext(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		cep, err = handler.cceEndpointClient.Get(namespace, name)
		if err != nil {
			return false, nil
		}
		if cep.Spec.ExternalIdentifiers == nil || cep.Spec.ExternalIdentifiers.ContainerID != containerID {
			return false, nil
		}
		cep = cep.DeepCopy()
		if cep.Spec.ExternalIdentifiers.Cnidriver == "" {
			cep.Spec.ExternalIdentifiers.Cnidriver = driver
		}
		shouldUpdate, err := shouldUpdateSpec(cep)
		if err != nil {
			return false, err
		}

		if shouldUpdate {
			cep, err = handler.cceEndpointClient.CCEEndpoints(namespace).Update(ctx, cep, metav1.UpdateOptions{})
			if err != nil {
				log.WithError(err).Error("update endpoint failed")
				return false, nil
			}
		}
		return true, nil
	})

	if err == nil && cep.Spec.Network.Bindwidth != nil {
		result.BandWidth = &models.BandwidthOption{
			Mode:    string(cep.Spec.Network.Bindwidth.Mode),
			Ingress: cep.Spec.Network.Bindwidth.Ingress,
			Egress:  cep.Spec.Network.Bindwidth.Egress,
		}
	}
	return result, err
}

func shouldUpdateSpec(resource *ccev2.CCEEndpoint) (bool, error) {
	if resource.Status.ExtFeatureStatus == nil {
		resource.Status.ExtFeatureStatus = make(map[string]*ccev2.ExtFeatureStatus)
	}
	if resource.Spec.ExternalIdentifiers == nil {
		return false, fmt.Errorf("external identifiers is nil")
	}

	var shouldUpdateSpec bool

	if option.Config.EnableBandwidthManager {
		// update endpoint spec
		bandWidthOpt, err := bandwidth.GetPodBandwidth(resource.Annotations, resource.Spec.ExternalIdentifiers.Cnidriver)
		if err != nil {
			return false, fmt.Errorf("failed to get pod bandwidth: %v", err)
		}

		if bandWidthOpt.IsValid() {
			if !reflect.DeepEqual(bandWidthOpt, resource.Spec.Network.Bindwidth) {
				shouldUpdateSpec = true
				// this feature will implement by cni
				resource.Spec.Network.Bindwidth = bandWidthOpt
				now := metav1.Now()
				resource.Status.ExtFeatureStatus[event.EndpointProbeEventBandwidth] = &ccev2.ExtFeatureStatus{
					Ready:       true,
					ContainerID: resource.Spec.ExternalIdentifiers.ContainerID,
					UpdateTime:  &now,
					Data: map[string]string{
						"mode":    string(bandWidthOpt.Mode),
						"ingress": strconv.Itoa(int(bandWidthOpt.Ingress)),
						"egress":  strconv.Itoa(int(bandWidthOpt.Egress)),
					},
				}
			}
		}
	}

	// append egress priority to spec and status
	if egressPriority := qos.GetEgressPriorityFromPod(resource.Annotations); egressPriority != nil {
		shouldUpdateSpec = true
		resource.Spec.Network.EgressPriority = egressPriority

		qosStatus, err := qos.GlobalManager.Handle(&event.EndpointProbeEvent{
			ID:   resource.Namespace + "/" + resource.Name,
			Obj:  resource,
			Type: event.EndpointProbeEventEgressPriority,
		})
		if err != nil {
			return false, fmt.Errorf("handle egress priority failed: %v", err)
		}
		resource.Status.ExtFeatureStatus[event.EndpointProbeEventEgressPriority] = qosStatus
	}
	return shouldUpdateSpec, nil
}
