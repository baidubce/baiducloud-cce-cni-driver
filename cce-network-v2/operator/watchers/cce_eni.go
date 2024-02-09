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

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccev2lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ENIClient = &eniUpdaterImpl{}

func StartSynchronizingENI(ctx context.Context, eniManager syncer.ENIEventHandler) error {
	log.Info("Starting to synchronize ENI custom resources")

	if eniManager == nil {
		// Since we won't be handling any events we don't need to convert
		// objects.
		log.Warningf("no ENI handler")
		return nil
	}

	enisLister := k8s.CCEClient().Informers.Cce().V2().ENIs().Lister()

	var eniSyncHandler = func(key string) error {
		obj, err := enisLister.Get(key)

		// Delete handling
		if errors.IsNotFound(err) {
			return eniManager.Delete(key)
		}
		if err != nil {
			log.WithError(err).Warning("Unable to retrieve ENI from watcher store")
			return err
		}
		return eniManager.Update(obj)
	}

	resyncPeriod := eniManager.ResyncENI(ctx)
	controller := cm.NewResyncController("cce-eni-controller", int(operatorOption.Config.ResourceResyncWorkers),
		k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(),
		eniSyncHandler)
	controller.RunWithResync(resyncPeriod)
	return nil
}

type eniUpdaterImpl struct{}

func (c *eniUpdaterImpl) Lister() ccev2lister.ENILister {
	return k8s.CCEClient().Informers.Cce().V2().ENIs().Lister()
}

func (c *eniUpdaterImpl) Create(eni *ccev2.ENI) (*ccev2.ENI, error) {
	return k8s.CCEClient().CceV2().ENIs().Create(context.TODO(), eni, metav1.CreateOptions{})
}

func (c *eniUpdaterImpl) Update(newResource *ccev2.ENI) (*ccev2.ENI, error) {
	return k8s.CCEClient().CceV2().ENIs().Update(context.TODO(), newResource, metav1.UpdateOptions{})
}

func (c *eniUpdaterImpl) Delete(name string) error {
	return k8s.CCEClient().CceV2().ENIs().Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (c *eniUpdaterImpl) UpdateStatus(newResource *ccev2.ENI) (*ccev2.ENI, error) {
	return k8s.CCEClient().CceV2().ENIs().UpdateStatus(context.TODO(), newResource, metav1.UpdateOptions{})
}

// GetByIP get CCEEndpoint by ip
func (c *eniUpdaterImpl) GetByIP(ip string) ([]*ccev2.ENI, error) {
	var data []*ccev2.ENI
	eniIndexer := k8s.CCEClient().Informers.Cce().V2().ENIs().Informer().GetIndexer()

	objs, err := eniIndexer.ByIndex(IndexIPToENI, ip)
	if err == nil {
		for _, obj := range objs {
			if eni, ok := obj.(*ccev2.ENI); ok {
				data = append(data, eni)
			}
		}
	}
	return data, err
}

type ENIUpdater interface {
	Lister() ccev2lister.ENILister
	Create(eni *ccev2.ENI) (*ccev2.ENI, error)
	Update(newResource *ccev2.ENI) (*ccev2.ENI, error)
	Delete(name string) error
	UpdateStatus(newResource *ccev2.ENI) (*ccev2.ENI, error)
	GetByIP(ip string) ([]*ccev2.ENI, error)
}

var _ ENIUpdater = &eniUpdaterImpl{}
