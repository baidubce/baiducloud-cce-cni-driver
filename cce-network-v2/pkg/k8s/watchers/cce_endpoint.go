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
	"fmt"
	"sync"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	cceclientv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/typed/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/tools/cache"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
)

// RegisterCCEEndpointSubscriber allows registration of subscriber.CCEEndpoint implementations.
// On CCEEndpoint events all registered subscriber.CCEEndpoint implementations will
// have their event handling methods called in order of registration.
func (k *K8sWatcher) RegisterCCEEndpointSubscriber(s subscriber.CCEEndpoint) {
	k.CCEEndpointChain.Register(s)
}

func (k *K8sWatcher) cceEndpointInit(cceClient *k8s.K8sCCEClient, asyncControllers *sync.WaitGroup) {
	// CCEEndpoint objects are used for node discovery until the key-value
	// store is connected
	var once sync.Once
	apiGroup := k8sAPIGroupCCEEndpointV2

	selector, _ := meta_v1.LabelSelectorAsSelector(meta_v1.SetAsLabelSelector(labels.Set{
		k8s.LabelNodeName: nodeTypes.GetName(),
	}))
	optionsModifier := func(options *meta_v1.ListOptions) {
		options.LabelSelector = selector.String()
	}

	cceEndpointStore, cceEndpointController := informer.NewInformer(
		cache.NewFilteredListWatchFromClient(cceClient.CceV2().RESTClient(),
			"cceendpoints", meta_v1.NamespaceAll, optionsModifier),
		&ccev2.CCEEndpoint{},
		10*time.Hour,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricCCEEndpoint, resources.MetricCreate, valid, equal) }()
				endpoint, ok := obj.(*ccev2.CCEEndpoint)
				if ok {
					valid = true
					errs := k.CCEEndpointChain.OnAddCCEEndpoint(endpoint)
					k.K8sEventProcessed(metricCCEEndpoint, resources.MetricCreate, errs == nil)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricCCEEndpoint, resources.MetricUpdate, valid, equal) }()
				oldEndpoint, ok := oldObj.(*ccev2.CCEEndpoint)
				if ok {
					newEndpoint, ok := newObj.(*ccev2.CCEEndpoint)
					if ok {
						valid = true
						errs := k.CCEEndpointChain.OnUpdateCCEEndpoint(oldEndpoint, newEndpoint)
						k.K8sEventProcessed(metricCCEEndpoint, resources.MetricUpdate, errs == nil)
					}

				}
			},
			DeleteFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricCCEEndpoint, resources.MetricDelete, valid, equal) }()
				CCEEndpoint, ok := obj.(*ccev2.CCEEndpoint)
				if ok {
					valid = true
					errs := k.CCEEndpointChain.OnDeleteCCEEndpoint(CCEEndpoint)
					k.K8sEventProcessed(metricCCEEndpoint, resources.MetricCreate, errs == nil)
				}
			},
		},
		nil,
	)
	go cceEndpointController.Run(k.stop)
	// once isConnected is closed, it will stop waiting on caches to be
	// synchronized.
	k.blockWaitGroupToSyncResources(k.stop, nil, cceEndpointController.HasSynced, apiGroup)
	k.cceEndpointStore = cceEndpointStore
	once.Do(func() {
		// Signalize that we have put node controller in the wait group
		// to sync resources.
		asyncControllers.Done()
	})
	k.k8sAPIGroups.AddAPI(apiGroup)
}

type CCEEndpointClient struct {
	k *K8sWatcher
}

func (k *K8sWatcher) NewCCEEndpointClient() *CCEEndpointClient {
	return &CCEEndpointClient{k: k}
}

func (client *CCEEndpointClient) Get(namespace, name string) (*ccev2.CCEEndpoint, error) {
	if client.k.cceEndpointStore == nil {
		return nil, fmt.Errorf("cache have not init")
	}
	obj, exist, err := client.k.cceEndpointStore.GetByKey(namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	CCEEndpoint, ok := obj.(*ccev2.CCEEndpoint)
	if !exist || !ok {
		return nil, errors.NewNotFound(schema.GroupResource{
			Group:    ccev2.SchemeGroupVersion.Group,
			Resource: "cceendpoints",
		}, name)
	}
	return CCEEndpoint, nil
}

func (client *CCEEndpointClient) List() ([]*ccev2.CCEEndpoint, error) {
	var CCEEndpointList []*ccev2.CCEEndpoint
	if client.k.cceEndpointStore == nil {
		return nil, fmt.Errorf("cache have not init")
	}
	for _, key := range client.k.cceEndpointStore.ListKeys() {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return CCEEndpointList, err
		}
		CCEEndpoint, err := client.Get(namespace, name)
		if err != nil {
			return CCEEndpointList, err
		}
		CCEEndpointList = append(CCEEndpointList, CCEEndpoint)
	}
	return CCEEndpointList, nil
}

func (client *CCEEndpointClient) CCEEndpoints(namespace string) cceclientv2.CCEEndpointInterface {
	return k8s.CCEClient().CceV2().CCEEndpoints(namespace)
}
