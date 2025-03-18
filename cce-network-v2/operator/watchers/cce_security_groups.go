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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	ccev2lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
)

var SecurityGroupClient = &sgUpdaterImpl{}

func StartSynchronizingSecurityGroup(ctx context.Context, sgManager syncer.SecurityGroupEventHandler) error {
	log.Info("Starting to synchronize securityGroup custom resources")

	if sgManager == nil {
		// Since we won't be handling any events we don't need to convert
		// objects.
		log.Warningf("no securityGroup handler")
		return nil
	}

	sgsLister := k8s.CCEClient().Informers.Cce().V2alpha1().SecurityGroups().Lister()

	var sgSyncHandler = func(key string) error {
		obj, err := sgsLister.Get(key)

		// Delete handling
		if errors.IsNotFound(err) {
			return sgManager.Delete(key)
		}
		if err != nil {
			log.WithError(err).Warning("Unable to retrieve securityGroup from watcher store")
			return err
		}
		return sgManager.Update(obj)
	}
	informer := k8s.CCEClient().Informers.Cce().V2alpha1().SecurityGroups().Informer()
	resyncPeriod := sgManager.ResyncSecurityGroup(ctx)
	controller := cm.NewResyncController("cce-sg-controller", int(operatorOption.Config.ResourceResyncWorkers), k8s.GetQPS(), k8s.GetBurst(),
		informer, sgSyncHandler)
	controller.RunWithResync(resyncPeriod)
	return nil
}

type sgUpdaterImpl struct{}

func (c *sgUpdaterImpl) Lister() ccev2lister.SecurityGroupLister {
	return k8s.CCEClient().Informers.Cce().V2alpha1().SecurityGroups().Lister()
}

func (c *sgUpdaterImpl) Create(sg *ccev2.SecurityGroup) (*ccev2.SecurityGroup, error) {
	return k8s.CCEClient().CceV2alpha1().SecurityGroups().Create(context.TODO(), sg, metav1.CreateOptions{})
}

func (c *sgUpdaterImpl) Update(newResource *ccev2.SecurityGroup) (*ccev2.SecurityGroup, error) {
	return k8s.CCEClient().CceV2alpha1().SecurityGroups().Update(context.TODO(), newResource, metav1.UpdateOptions{})
}

func (c *sgUpdaterImpl) Delete(name string) error {
	return k8s.CCEClient().CceV2alpha1().SecurityGroups().Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (c *sgUpdaterImpl) UpdateStatus(newResource *ccev2.SecurityGroup) (*ccev2.SecurityGroup, error) {
	return k8s.CCEClient().CceV2alpha1().SecurityGroups().UpdateStatus(context.TODO(), newResource, metav1.UpdateOptions{})
}

type securityGroupUpdater interface {
	Lister() ccev2lister.SecurityGroupLister
	Create(sg *ccev2.SecurityGroup) (*ccev2.SecurityGroup, error)
	Update(newResource *ccev2.SecurityGroup) (*ccev2.SecurityGroup, error)
	Delete(name string) error
	UpdateStatus(newResource *ccev2.SecurityGroup) (*ccev2.SecurityGroup, error)
}

var _ securityGroupUpdater = &sgUpdaterImpl{}
