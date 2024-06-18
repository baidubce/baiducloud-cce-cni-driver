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

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	cceclientv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/typed/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
)

// RegisterENISubscriber allows registration of subscriber.ENI implementations.
// On ENI events all registered subscriber.ENI implementations will
// have their event handling methods called in order of registration.
func (k *K8sWatcher) RegisterENISubscriber(s subscriber.ENI) {
	k.ENIChain.Register(s)
}

func (k *K8sWatcher) eniInit(cceClient *k8s.K8sCCEClient, asyncControllers *sync.WaitGroup) {
	// ENI objects are used for node discovery until the key-value
	// store is connected
	var once sync.Once
	apiGroup := k8sAPIGroupCCEENIV2

	// Select only the ENI of the local node,
	// contains Ethernet NetResourceSet and RDMA NetResourceSet objects
	values := []string{nodeTypes.GetName()}
	rii, _ := bceutils.GetRdmaIFsInfo(nodeTypes.GetName(), nil)
	for _, v := range rii {
		values = append(values, v.NetResourceSetName)
	}
	requirement := metav1.LabelSelectorRequirement{
		Key:      k8s.LabelNodeName,
		Operator: metav1.LabelSelectorOpIn,
		Values:   values,
	}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{requirement},
	}
	// Select only the ENI of the local node
	selector, _ := metav1.LabelSelectorAsSelector(labelSelector)
	optionsModifier := func(options *metav1.ListOptions) {
		options.LabelSelector = selector.String()
	}

	eniStore, eniController := informer.NewInformer(
		cache.NewFilteredListWatchFromClient(cceClient.CceV2().RESTClient(),
			"enis", metav1.NamespaceAll, optionsModifier),
		&ccev2.ENI{},
		option.Config.ResourceResyncInterval,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricENI, resources.MetricUpdate, valid, equal) }()
				oldEndpoint, ok := oldObj.(*ccev2.ENI)
				if ok {
					newEndpoint, ok := newObj.(*ccev2.ENI)
					if ok {
						valid = true
						errs := k.ENIChain.OnUpdateENI(oldEndpoint, newEndpoint)
						k.K8sEventProcessed(metricENI, resources.MetricUpdate, errs == nil)
					}

				}
			},
			DeleteFunc: func(obj interface{}) {
				var valid, equal bool
				defer func() { k.K8sEventReceived(apiGroup, metricENI, resources.MetricDelete, valid, equal) }()
				ENI, ok := obj.(*ccev2.ENI)
				if ok {
					valid = true
					errs := k.ENIChain.OnDeleteENI(ENI)
					k.K8sEventProcessed(metricENI, resources.MetricCreate, errs == nil)
				}
			},
		},
		nil,
	)
	go eniController.Run(k.stop)
	// once isConnected is closed, it will stop waiting on caches to be
	// synchronized.
	k.blockWaitGroupToSyncResources(k.stop, nil, eniController.HasSynced, apiGroup)
	k.eniStore = eniStore
	once.Do(func() {
		// Signalize that we have put node controller in the wait group
		// to sync resources.
		asyncControllers.Done()
	})
	k.k8sAPIGroups.AddAPI(apiGroup)
}

type ENIClient struct {
	k *K8sWatcher
}

func (k *K8sWatcher) NewENIClient() *ENIClient {
	return &ENIClient{k: k}
}

func (client *ENIClient) Get(name string) (*ccev2.ENI, error) {
	if client.k.eniStore == nil {
		return nil, fmt.Errorf("cache have not init")
	}
	obj, exist, err := client.k.eniStore.GetByKey(name)
	if err != nil {
		return nil, err
	}
	ENI, ok := obj.(*ccev2.ENI)
	if !exist || !ok {
		return nil, errors.NewNotFound(schema.GroupResource{
			Group:    ccev2.SchemeGroupVersion.Group,
			Resource: "enis",
		}, name)
	}
	return ENI, nil
}

func (client *ENIClient) List() ([]*ccev2.ENI, error) {
	var ENIList []*ccev2.ENI
	if client.k.eniStore == nil {
		return nil, fmt.Errorf("cache have not init")
	}
	for _, name := range client.k.eniStore.ListKeys() {
		ENI, err := client.Get(name)
		if err != nil {
			return ENIList, err
		}
		ENIList = append(ENIList, ENI)
	}
	return ENIList, nil
}

func (client *ENIClient) ENIs() cceclientv2.ENIInterface {
	return k8s.CCEClient().CceV2().ENIs()
}
