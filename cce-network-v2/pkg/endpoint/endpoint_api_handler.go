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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
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
