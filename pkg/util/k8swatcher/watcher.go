/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package k8swatcher

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions/networking/v1alpha1"
	crdlisters "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

// NodeHandler is an abstract interface of objects which receive
// notifications about node object changes.
type NodeHandler interface {
	SyncNode(nodeKey string, nodeLister corelisters.NodeLister) error
}

// NodeWatcher tracks a set of node events.
type NodeWatcher struct {
	listerSynced  cache.InformerSynced
	lister        corelisters.NodeLister
	eventHandlers []NodeHandler
	workqueue     workqueue.RateLimitingInterface
}

// NewNodeWatcher creates a new NodeWatcher.
func NewNodeWatcher(nodeInformer coreinformers.NodeInformer, resyncPeriod time.Duration) *NodeWatcher {
	result := &NodeWatcher{
		listerSynced: nodeInformer.Informer().HasSynced,
		lister:       nodeInformer.Lister(),
		workqueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "node"),
	}

	nodeInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    result.handleAddNode,
			UpdateFunc: result.handleUpdateNode,
			DeleteFunc: result.handleDeleteNode,
		},
		resyncPeriod,
	)

	return result
}

// RegisterEventHandler registers a handler which is called on every node change.
func (c *NodeWatcher) RegisterEventHandler(handler NodeHandler) {
	c.eventHandlers = append(c.eventHandlers, handler)
}

func (c *NodeWatcher) SyncCache(stopCh <-chan struct{}) error {
	if !cache.WaitForNamedCacheSync("node watcher", stopCh, c.listerSynced) {
		return fmt.Errorf("failed to WaitForNamedCacheSync")
	}
	return nil
}

// Run starts the goroutine responsible for calling registered handlers.
func (c *NodeWatcher) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	log.Info(context.TODO(), "Starting node watcher")

	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *NodeWatcher) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *NodeWatcher) processNextWorkItem() bool {
	var errs []error

	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	defer c.workqueue.Done(obj)

	for _, handler := range c.eventHandlers {
		if err := handler.SyncNode(obj.(string), c.lister); err != nil {
			errs = append(errs, err)
		}
	}

	err := utilerrors.NewAggregate(errs)
	if err == nil {
		c.workqueue.Forget(obj)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("error processing node %v (will retry): %v", obj, err))
	c.workqueue.AddRateLimited(obj)

	return true
}

func (c *NodeWatcher) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", obj, err))
		return
	}

	c.workqueue.AddRateLimited(key)
}

func (c *NodeWatcher) handleAddNode(obj interface{}) {
	_, ok := obj.(*v1.Node)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
		return
	}
	c.enqueue(obj)
}

func (c *NodeWatcher) handleUpdateNode(oldObj, newObj interface{}) {
	_, ok := oldObj.(*v1.Node)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", oldObj))
		return
	}
	_, ok = newObj.(*v1.Node)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", newObj))
		return
	}

	c.enqueue(newObj)
}

func (c *NodeWatcher) handleDeleteNode(obj interface{}) {
	_, ok := obj.(*v1.Node)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
			return
		}
		if _, ok := tombstone.Obj.(*v1.Node); !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
			return
		}
	}
	c.enqueue(obj)
}

// IPPoolHandler is an abstract interface of objects which receive
// notifications about ippool object changes.
type IPPoolHandler interface {
	SyncIPPool(poolKey string, poolLister crdlisters.IPPoolLister) error
}

// IPPoolWatcher tracks a set of ippool configurations.
type IPPoolWatcher struct {
	listerSynced  cache.InformerSynced
	lister        crdlisters.IPPoolLister
	eventHandlers []IPPoolHandler
	workqueue     workqueue.RateLimitingInterface
}

// NewIPPoolWatcher creates a new IPPoolWatcher.
func NewIPPoolWatcher(poolInformer crdinformers.IPPoolInformer, resyncPeriod time.Duration) *IPPoolWatcher {
	result := &IPPoolWatcher{
		listerSynced: poolInformer.Informer().HasSynced,
		lister:       poolInformer.Lister(),
		workqueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ippool"),
	}

	poolInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    result.handleAddIPPool,
			UpdateFunc: result.handleUpdateIPPool,
			DeleteFunc: result.handleDeleteIPPool,
		},
		resyncPeriod,
	)

	return result
}

// RegisterEventHandler registers a handler which is called on every ipool change.
func (c *IPPoolWatcher) RegisterEventHandler(handler IPPoolHandler) {
	c.eventHandlers = append(c.eventHandlers, handler)
}

// SyncCahe wait cache sync ready
func (c *IPPoolWatcher) SyncCache(stopCh <-chan struct{}) bool {
	return cache.WaitForNamedCacheSync("ippool watcher", stopCh, c.listerSynced)
}

// Run starts the goroutine responsible for calling registered handlers.
func (c *IPPoolWatcher) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	log.Info(context.TODO(), "Starting ippool watcher")

	if !cache.WaitForNamedCacheSync("ippool watcher", stopCh, c.listerSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *IPPoolWatcher) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *IPPoolWatcher) processNextWorkItem() bool {
	var errs []error

	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	defer c.workqueue.Done(obj)

	for _, handler := range c.eventHandlers {
		if err := handler.SyncIPPool(obj.(string), c.lister); err != nil {
			errs = append(errs, err)
		}
	}

	err := utilerrors.NewAggregate(errs)
	if err == nil {
		c.workqueue.Forget(obj)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("error processing ippool %v (will retry): %v", obj, err))
	c.workqueue.AddRateLimited(obj)

	return true
}

func (c *IPPoolWatcher) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", obj, err))
		return
	}

	c.workqueue.AddRateLimited(key)
}

func (c *IPPoolWatcher) handleAddIPPool(obj interface{}) {
	_, ok := obj.(*v1alpha1.IPPool)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
		return
	}
	c.enqueue(obj)
}

func (c *IPPoolWatcher) handleUpdateIPPool(oldObj, newObj interface{}) {
	_, ok := oldObj.(*v1alpha1.IPPool)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", oldObj))
		return
	}
	_, ok = newObj.(*v1alpha1.IPPool)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", newObj))
		return
	}

	c.enqueue(newObj)
}

func (c *IPPoolWatcher) handleDeleteIPPool(obj interface{}) {
	_, ok := obj.(*v1alpha1.IPPool)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
			return
		}
		if _, ok := tombstone.Obj.(*v1alpha1.IPPool); !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
			return
		}
	}
	c.enqueue(obj)
}
