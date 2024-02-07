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
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/comparator"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
)

var (
	// onceNodeInitStart is used to guarantee that only one function call of
	// NodesInit is executed.
	onceNodeInitStart sync.Once
)

// RegisterNodeSubscriber allows registration of subscriber.Node implementations.
// On k8s Node events all registered subscriber.Node implementations will
// have their event handling methods called in order of registration.
func (k *K8sWatcher) RegisterNodeSubscriber(s subscriber.Node) {
	k.NodeChain.Register(s)
}

func nodeEventsAreEqual(oldNode, newNode *v1.Node) bool {
	if !comparator.MapStringEquals(oldNode.GetLabels(), newNode.GetLabels()) {
		return false
	}

	return true
}

func (k *K8sWatcher) NodesInit(k8sClient *k8s.K8sClient) {
	apiGroup := k8sAPIGroupNodeV1Core
	onceNodeInitStart.Do(func() {
		swg := lock.NewStoppableWaitGroup()

		nodeStore, nodeController := informer.NewInformer(
			cache.NewListWatchFromClient(k8sClient.CoreV1().RESTClient(),
				"nodes", v1.NamespaceAll, fields.ParseSelectorOrDie("metadata.name="+nodeTypes.GetName())),
			&v1.Node{},
			0,
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					var valid bool
					if node := k8s.ObjToV1Node(obj); node != nil {
						valid = true
						errs := k.NodeChain.OnAddNode(node, swg)
						k.K8sEventProcessed(metricNode, resources.MetricCreate, errs == nil)
					}
					k.K8sEventReceived(apiGroup, metricNode, resources.MetricCreate, valid, false)
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					var valid, equal bool
					if oldNode := k8s.ObjToV1Node(oldObj); oldNode != nil {
						valid = true
						if newNode := k8s.ObjToV1Node(newObj); newNode != nil {
							equal = nodeEventsAreEqual(oldNode, newNode)
							if !equal {
								errs := k.NodeChain.OnUpdateNode(oldNode, newNode, swg)
								k.K8sEventProcessed(metricNode, resources.MetricCreate, errs == nil)
							}
						}
					}
					k.K8sEventReceived(apiGroup, metricNode, resources.MetricCreate, valid, false)
				},
				DeleteFunc: func(obj interface{}) {
				},
			},
			nil,
		)

		k.nodeStore = nodeStore

		k.blockWaitGroupToSyncResources(wait.NeverStop, swg, nodeController.HasSynced, k8sAPIGroupNodeV1Core)
		go nodeController.Run(k.stop)
		k.k8sAPIGroups.AddAPI(apiGroup)
	})
}

// GetK8sNode returns the *local Node* from the local store.
func (k *K8sWatcher) GetK8sNode(_ context.Context, nodeName string) (*v1.Node, error) {
	k.WaitForCacheSync(k8sAPIGroupNodeV1Core)
	pName := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}
	nodeInterface, exists, err := k.nodeStore.Get(pName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, k8sErrors.NewNotFound(schema.GroupResource{
			Group:    "core",
			Resource: "Node",
		}, nodeName)
	}
	return nodeInterface.(*v1.Node).DeepCopy(), nil
}

// netResourceSetUpdater implements the subscriber.Node interface and is used
// to keep NetResourceSet objects in sync with the node ones.
type netResourceSetUpdater struct {
	k8sWatcher *K8sWatcher
}

func NewNetResourceSetUpdater(k8sWatcher *K8sWatcher) *netResourceSetUpdater {
	return &netResourceSetUpdater{
		k8sWatcher: k8sWatcher,
	}
}

func (u *netResourceSetUpdater) OnAddNode(newNode *v1.Node, swg *lock.StoppableWaitGroup) error {
	u.updateNetResourceSet(newNode)

	return nil
}

func (u *netResourceSetUpdater) OnUpdateNode(oldNode, newNode *v1.Node, swg *lock.StoppableWaitGroup) error {
	u.updateNetResourceSet(newNode)

	return nil
}

func (u *netResourceSetUpdater) OnDeleteNode(*v1.Node, *lock.StoppableWaitGroup) error {
	return nil
}

func (u *netResourceSetUpdater) updateNetResourceSet(node *v1.Node) {
	var (
		controllerName = fmt.Sprintf("sync-node-with-netresourceset (%v)", node.Name)
	)

	doFunc := func(ctx context.Context) (err error) {
		{
			u.k8sWatcher.netResourceSetStoreMU.RLock()
			defer u.k8sWatcher.netResourceSetStoreMU.RUnlock()

			if u.k8sWatcher.netResourceSetStore == nil {
				return errors.New("NetResourceSet cache store not yet initialized")
			}

			netResourceSetInterface, exists, err := u.k8sWatcher.netResourceSetStore.GetByKey(node.Name)
			if err != nil {
				return fmt.Errorf("failed to get NetResourceSet resource from cache store: %w", err)
			}
			if !exists {
				return nil
			}

			netResourceSet := netResourceSetInterface.(*ccev2.NetResourceSet).DeepCopy()

			netResourceSet.Labels = node.GetLabels()

			// If the labels are the same, there is no need to update the NetResourceSet.
			if reflect.DeepEqual(netResourceSet.Labels, netResourceSetInterface.(*ccev2.NetResourceSet).Labels) {
				return nil
			}

			if _, err = k8s.CCEClient().CceV2().NetResourceSets().Update(ctx, netResourceSet, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed to update NetResourceSet labels: %w", err)
			}
		}

		return nil
	}

	k8sCM.UpdateController(controllerName,
		controller.ControllerParams{
			ErrorRetryBaseDuration: time.Second * 30,
			DoFunc:                 doFunc,
		})
}
