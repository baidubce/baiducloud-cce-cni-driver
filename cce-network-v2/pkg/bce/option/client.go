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
package option

import (
	"os"
	"sync"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/ccegateway"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

var (
	mutex         sync.Mutex
	defaultClient cloud.Interface
	log           = logging.NewSubysLogger("bce-client")
)

// BCEClient create a client to BCE Cloud
func BCEClient() cloud.Interface {
	mutex.Lock()
	if defaultClient != nil {
		mutex.Unlock()
		return defaultClient
	}
	mutex.Unlock()

	if endpoint := operatorOption.Config.BCECloudBaseHost; endpoint != "" {
		os.Setenv(ccegateway.EndpointOverrideEnv, endpoint)
	}

	if operatorOption.Config.BCECloudRegion == "" {
		log.Fatal("[InitBCEClient] Region is nil")
	}

	if operatorOption.Config.BCECloudAccessKey == "" && operatorOption.Config.BCECloudSecureKey == "" {
		if operatorOption.Config.CCEClusterID == "" {
			log.Fatalf("[InitBCEClient] ClusterID is required when ak/sk are not set")
		}
	}

	c, err := cloud.New(operatorOption.Config.BCECloudRegion,
		operatorOption.Config.CCEClusterID,
		operatorOption.Config.BCECloudAccessKey,
		operatorOption.Config.BCECloudSecureKey,
		operatorOption.Config.BCEForceViaCCEGateway,
		k8s.Client(),
		option.Config.Debug, operatorOption.Config.DefaultAPITimeoutLimit)
	if err != nil {
		log.Fatalf("[InitBCEClient] failed to init bce client %v", err)
	}
	c, err = cloud.NewFlowControlClient(c,
		operatorOption.Config.DefaultAPIQPSLimit,
		operatorOption.Config.DefaultAPIBurst,
		operatorOption.Config.DefaultAPITimeoutLimit)
	if err != nil {
		log.Fatalf("[InitBCEClient] failed to init bce client with flow control %v", err)
	}

	mutex.Lock()
	defaultClient = c
	mutex.Unlock()
	return defaultClient
}
