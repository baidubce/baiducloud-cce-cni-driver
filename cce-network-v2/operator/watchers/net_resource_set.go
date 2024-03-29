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
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listerv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
)

var (
	NetResourceSetClient = &NetResourceSetUpdateImplementation{}
)

// NodeEventHandler should implement the behavior to handle NetResourceSet
type NodeEventHandler interface {
	Create(resource *ccev2.NetResourceSet) error
	Update(resource *ccev2.NetResourceSet) error
	Delete(nodeName string) error
	Resync(context.Context, time.Time)
}

func StartSynchronizingNetResourceSets(ctx context.Context, nodeManager NodeEventHandler) error {
	log.Info("Starting to synchronize NetResourceSet custom resources")

	if nodeManager == nil {
		return nil
	}
	nodeManagerSyncHandler := syncHandlerConstructor(
		func(name string) error {
			return nodeManager.Delete(name)
		},
		func(node *ccev2.NetResourceSet) error {
			// node is deep copied before it is stored in pkg/aws/eni
			return nodeManager.Update(node)
		})

	controller := cm.NewWorkqueueController("network-resource-set-controller", 10, nodeManagerSyncHandler)
	controller.Run()
	k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Informer().AddEventHandler(controller)

	return nil
}

func syncHandlerConstructor(notFoundHandler func(name string) error, foundHandler func(node *ccev2.NetResourceSet) error) func(key string) error {
	return func(key string) error {
		_, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			log.WithError(err).Error("Unable to process NetResourceSet event")
			return err
		}
		obj, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().Get(name)

		// Delete handling
		if errors.IsNotFound(err) {
			return notFoundHandler(name)
		}
		if err != nil {
			log.WithError(err).Warning("Unable to retrieve NetResourceSet from watcher store")
			return err
		}
		return foundHandler(obj)
	}
}

type NetResourceSetUpdateImplementation struct{}

func (c *NetResourceSetUpdateImplementation) Create(node *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error) {
	return k8s.CCEClient().CceV2().NetResourceSets().Create(context.TODO(), node, meta_v1.CreateOptions{})
}

func (c *NetResourceSetUpdateImplementation) Get(node string) (*ccev2.NetResourceSet, error) {
	return k8s.CCEClient().CceV2().NetResourceSets().Get(context.TODO(), node, meta_v1.GetOptions{})
}

func (c *NetResourceSetUpdateImplementation) UpdateStatus(origNode, node *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error) {
	if origNode == nil || !reflect.DeepEqual(origNode.Status, node.Status) {
		return k8s.CCEClient().CceV2().NetResourceSets().UpdateStatus(context.TODO(), node, meta_v1.UpdateOptions{})
	}
	return nil, nil
}

func (c *NetResourceSetUpdateImplementation) Update(origNode, node *ccev2.NetResourceSet) (*ccev2.NetResourceSet, error) {
	if origNode == nil || !reflect.DeepEqual(origNode.Spec, node.Spec) || !reflect.DeepEqual(origNode.ObjectMeta, node.ObjectMeta) {
		return k8s.CCEClient().CceV2().NetResourceSets().Update(context.TODO(), node, meta_v1.UpdateOptions{})
	}
	return nil, nil
}

func (c *NetResourceSetUpdateImplementation) Lister() listerv2.NetResourceSetLister {
	return k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister()
}
