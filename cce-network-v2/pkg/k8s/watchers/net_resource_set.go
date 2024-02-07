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
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	cce_v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
)

// RegisterNetResourceSetSubscriber allows registration of subscriber.NetResourceSet implementations.
// On NetResourceSet events all registered subscriber.NetResourceSet implementations will
// have their event handling methods called in order of registration.
func (k *K8sWatcher) RegisterNetResourceSetSubscriber(s subscriber.NetResourceSet) {
	k.NetResourceSetChain.Register(s)

	// If the NetResourceSet store is already initialized, we need to send the cahced objects to subscriber
	if k.netResourceSetStore != nil {
		for _, obj := range k.netResourceSetStore.List() {
			if netResourceSet := k8s.ObjToNetResourceSet(obj); netResourceSet != nil {
				s.OnUpdateNetResourceSet(nil, netResourceSet, nil)
			}
		}
	}
}

func (k *K8sWatcher) netResourceSetInit(cceNPClient *k8s.K8sCCEClient, asyncControllers *sync.WaitGroup) {
	// NetResourceSet objects are used for node discovery until the key-value
	// store is connected
	var once sync.Once
	apiGroup := k8sAPIGroupNetResourceSetV2
	swgNodes := lock.NewStoppableWaitGroup()
	netResourceSetStore, netResourceSetInformer := informer.NewInformer(
		cache.NewListWatchFromClient(cceNPClient.CceV2().RESTClient(),
			cce_v2.NRSPluralName, v1.NamespaceAll, fields.Everything()),
		&cce_v2.NetResourceSet{},
		10*time.Hour,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricNetResourceSet, resources.MetricCreate, valid, equal) }()
				if netResourceSet := k8s.ObjToNetResourceSet(obj); netResourceSet != nil {
					valid = true
					n := nodeTypes.ParseNetResourceSet(netResourceSet)
					errs := k.NetResourceSetChain.OnAddNetResourceSet(netResourceSet, swgNodes)
					if n.IsLocal() {
						return
					}
					k.K8sEventProcessed(metricNetResourceSet, resources.MetricCreate, errs == nil)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricNetResourceSet, resources.MetricUpdate, valid, equal) }()
				if oldCN := k8s.ObjToNetResourceSet(oldObj); oldCN != nil {
					if netResourceSet := k8s.ObjToNetResourceSet(newObj); netResourceSet != nil {
						valid = true
						isLocal := k8s.IsLocalNetResourceSet(netResourceSet)

						errs := k.NetResourceSetChain.OnUpdateNetResourceSet(oldCN, netResourceSet, swgNodes)
						if isLocal {
							return
						}
						k.K8sEventProcessed(metricNetResourceSet, resources.MetricUpdate, errs == nil)
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricNetResourceSet, resources.MetricDelete, valid, equal) }()
				netResourceSet := k8s.ObjToNetResourceSet(obj)
				if netResourceSet == nil {
					return
				}
				valid = true
				errs := k.NetResourceSetChain.OnDeleteNetResourceSet(netResourceSet, swgNodes)
				if errs != nil {
					valid = false
				}
			},
		},
		nil,
	)

	log.Infof("Starting NetResourceSet controller")
	// once isConnected is closed, it will stop waiting on caches to be
	// synchronized.
	k.blockWaitGroupToSyncResources(k.stop, swgNodes, netResourceSetInformer.HasSynced, apiGroup)

	k.netResourceSetStoreMU.Lock()
	k.netResourceSetStore = netResourceSetStore
	k.netResourceSetStoreMU.Unlock()

	once.Do(func() {
		// Signalize that we have put node controller in the wait group
		// to sync resources.
		asyncControllers.Done()
	})
	k.k8sAPIGroups.AddAPI(apiGroup)
	netResourceSetInformer.Run(k.stop)

}
