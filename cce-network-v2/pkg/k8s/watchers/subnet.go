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
	"sync"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (k *K8sWatcher) subnetInit(cceClient *k8s.K8sCCEClient, asyncControllers *sync.WaitGroup) {
	// CCESubnet objects are used for node discovery until the key-value
	// store is connected
	var once sync.Once
	once.Do(func() {
		inf := cceClient.Informers.Cce().V1().Subnets().Informer()
		cceClient.Informers.Start(wait.NeverStop)
		// once isConnected is closed, it will stop waiting on caches to be
		// synchronized.
		k.blockWaitGroupToSyncResources(k.stop, nil, inf.HasSynced, k8sAPIGroupCCESubnetV1)
		// Signalize that we have put node controller in the wait group
		// to sync resources.
		asyncControllers.Done()
	})
	k.k8sAPIGroups.AddAPI(k8sAPIGroupCCESubnetV1)
}
