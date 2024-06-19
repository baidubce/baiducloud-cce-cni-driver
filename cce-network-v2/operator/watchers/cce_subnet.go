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
	"fmt"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev1lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var CCESubnetClient = &subnetUpdaterImpl{}

func StartSynchronizingSubnet(ctx context.Context, subnetManager syncer.SubnetEventHandler) error {
	log.Info("Starting to synchronize Subnet custom resources")

	if subnetManager == nil {
		// Since we won't be handling any events we don't need to convert
		// objects.
		log.Warningf("no Subnet handler")
		return nil
	}

	subnetsLister := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister()

	var endpointManagerSyncHandler = func(key string) error {
		obj, err := subnetsLister.Get(key)

		// Delete handling
		if (err == nil || errors.IsNotFound(err)) && obj != nil && obj.DeletionTimestamp != nil {
			return subnetManager.Delete(key)
		}
		if err != nil {
			log.WithError(err).Warning("Unable to retrieve Subnet from watcher store")
			return err
		}
		return subnetManager.Update(obj)
	}

	resyncPeriod := subnetManager.ResyncSubnet(ctx)
	controller := cm.NewResyncController("cce-subnet-controller", int(operatorOption.Config.SubnetResourceResyncWorkers),
		k8s.CCEClient().Informers.Cce().V1().Subnets().Informer(),
		endpointManagerSyncHandler)
	controller.RunWithResync(resyncPeriod)
	return nil
}

type subnetUpdaterImpl struct{}

func (c *subnetUpdaterImpl) Lister() ccev1lister.SubnetLister {
	return k8s.CCEClient().Informers.Cce().V1().Subnets().Lister()
}

func (c *subnetUpdaterImpl) Create(subnet *ccev1.Subnet) (*ccev1.Subnet, error) {
	return k8s.CCEClient().CceV1().Subnets().Create(context.TODO(), subnet, metav1.CreateOptions{})
}

func (c *subnetUpdaterImpl) Update(newResource *ccev1.Subnet) (*ccev1.Subnet, error) {
	return k8s.CCEClient().CceV1().Subnets().Update(context.TODO(), newResource, metav1.UpdateOptions{})
}

func (c *subnetUpdaterImpl) Delete(name string) error {
	return k8s.CCEClient().CceV1().Subnets().Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (c *subnetUpdaterImpl) UpdateStatus(newResource *ccev1.Subnet) (*ccev1.Subnet, error) {
	return k8s.CCEClient().CceV1().Subnets().UpdateStatus(context.TODO(), newResource, metav1.UpdateOptions{})
}

func (c *subnetUpdaterImpl) EnsureSubnet(vpcID, sbnID string, exclusive bool) (*ccev1.Subnet, error) {
	sbn, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(sbnID)
	if err != nil {
		newSubnet := ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: sbnID,
			},
			Spec: ccev1.SubnetSpec{
				ID:        sbnID,
				VPCID:     vpcID,
				Exclusive: exclusive,
			},
			Status: ccev1.SubnetStatus{
				Enable: false,
			},
		}
		sbn, err = k8s.CCEClient().CceV1().Subnets().Create(context.TODO(), &newSubnet, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}
	if sbn != nil && sbn.Spec.Exclusive != exclusive {
		return nil, fmt.Errorf("subnet %s exclusive %t not match", sbnID, exclusive)
	}
	return sbn, nil
}
