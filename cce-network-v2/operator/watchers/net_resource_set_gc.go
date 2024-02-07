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
	"time"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// netResourceSetGCCandidate keeps track of cce nodes, which are candidate for GC.
// Underlying there is a map with node name as key, and last marked timestamp as value.
type netResourceSetGCCandidate struct {
	lock          lock.RWMutex
	nodesToRemove map[string]time.Time
}

func newNetResourceSetGCCandidate() *netResourceSetGCCandidate {
	return &netResourceSetGCCandidate{
		nodesToRemove: map[string]time.Time{},
	}
}

func (c *netResourceSetGCCandidate) Get(nodeName string) (time.Time, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	val, exists := c.nodesToRemove[nodeName]
	return val, exists
}

func (c *netResourceSetGCCandidate) Add(nodeName string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.nodesToRemove[nodeName] = time.Now()
}

func (c *netResourceSetGCCandidate) Delete(nodeName string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.nodesToRemove, nodeName)
}

// RunNetResourceSetGC performs garbage collector for cce node resource
func RunNetResourceSetGC(ctx context.Context, interval time.Duration) {
	nodesInit()
	WaitForCacheSync(ctx.Done())

	log.Info("Starting to garbage collect stale NetResourceSet custom resources")

	candidateStore := newNetResourceSetGCCandidate()
	// create the controller to perform mark and sweep operation for cce nodes
	ctrlMgr.UpdateController("network-resource-set-gc",
		controller.ControllerParams{
			Context: ctx,
			DoFunc: func(ctx context.Context) error {
				return performNetResourceSetGC(ctx, interval, candidateStore)
			},
			RunInterval: interval,
		},
	)
}

func performNetResourceSetGC(ctx context.Context, interval time.Duration, candidateStore *netResourceSetGCCandidate) error {
	netresourcesets, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().List(labels.Everything())
	if err != nil {
		return err
	}
	for _, netresourceset := range netresourcesets {
		scopedLog := log.WithField(logfields.NodeName, netresourceset.Name)
		_, err := NodeClient.Lister().Get(netresourceset.Name)
		if err == nil {
			scopedLog.Debugf("NetResourceSet is valid, no gargage collection required")
			continue
		}

		if !k8serrors.IsNotFound(err) {
			scopedLog.WithError(err).Error("Unable to fetch k8s node from store")
			return err
		}

		// if there is owner references, let k8s handle garbage collection
		if len(netresourceset.GetOwnerReferences()) > 0 {
			continue
		}

		lastMarkedTime, exists := candidateStore.Get(netresourceset.Name)
		if !exists {
			scopedLog.Info("Add NetResourceSet to garbage collector candidates")
			candidateStore.Add(netresourceset.Name)
			continue
		}

		// only remove the node if last marked time is more than running interval
		if lastMarkedTime.Before(time.Now().Add(-interval)) {
			scopedLog.Info("Perform GC for invalid NetResourceSet")
			err = k8s.CCEClient().CceV2().NetResourceSets().Delete(ctx, netresourceset.Name, metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				scopedLog.WithError(err).Error("Failed to delete invalid NetResourceSet")
				return err
			}
			scopedLog.Info("NetResourceSet is garbage collected successfully")
			candidateStore.Delete(netresourceset.Name)
		}
	}
	return nil
}
