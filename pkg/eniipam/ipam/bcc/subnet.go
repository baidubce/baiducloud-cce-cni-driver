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

package bcc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ktypes "k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	crdlisters "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8swatcher"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	// periodically update subnet cr status
	subnetSyncPeriod = 15 * time.Second
)

func (ipam *IPAM) syncSubnets(stopCh <-chan struct{}) {
	// ensure subnet cr is created if ippool changed.
	watcher := k8swatcher.NewIPPoolWatcher(ipam.crdInformer.Cce().V1alpha1().IPPools(), ipam.informerResyncPeriod)
	watcher.RegisterEventHandler(ipam)
	go watcher.Run(1, stopCh)

	err := wait.PollImmediateUntil(subnetSyncPeriod, func() (done bool, err error) {
		ctx := log.NewContext()
		_ = ipam.updateSubnetStatus(ctx)
		return false, nil
	}, stopCh)
	if err != nil {
		log.Errorf(context.TODO(), "failed to poll subnets: %v", err)
	}
}

// SyncIPPool ipam implements IPPoolHandler
func (ipam *IPAM) SyncIPPool(poolKey string, poolLister crdlisters.IPPoolLister) error {
	ctx := log.NewContext()

	namespace, name, err := cache.SplitMetaNamespaceKey(poolKey)
	if err != nil {
		return err
	}

	pool, err := poolLister.IPPools(namespace).Get(name)
	if err != nil {
		log.Errorf(ctx, "failed to get ippool %v: %v", poolKey, err)
		return err
	}

	var errs []error
	for _, s := range pool.Spec.ENI.Subnets {
		err := ipam.ensureSubnetCRExists(ctx, s)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (ipam *IPAM) updateSubnetStatus(ctx context.Context) error {
	subnetList, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).List(labels.Everything())
	if err != nil {
		log.Errorf(ctx, "failed to list subnet crd: %v", err)
		return err
	}

	for _, crd := range subnetList {
		resp, err := ipam.cloud.DescribeSubnet(ctx, crd.Spec.ID)
		if err != nil {
			log.Errorf(ctx, "failed to describe vpc subnet %v: %v", crd.Spec.ID, err)
			continue
		}

		hasNoMoreIP := resp.AvailableIp <= 0

		if crd.Status.AvailableIPNum != resp.AvailableIp || hasNoMoreIP != crd.Status.HasNoMoreIP {
			crd.Status.AvailableIPNum = resp.AvailableIp
			crd.Status.HasNoMoreIP = hasNoMoreIP
			if err := ipam.patchSubnetStatus(ctx, crd.Name, &crd.Status); err != nil {
				log.Errorf(ctx, "failed to patch subnet %v status: %v", crd.Spec.ID, err)
				continue
			}
		}
	}

	return nil
}

func (ipam *IPAM) ensureSubnetCRExists(ctx context.Context, name string) error {
	subnet, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(name)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		// subnet cr not found, start to create
		resp, err := ipam.cloud.DescribeSubnet(ctx, name)
		if err != nil {
			log.Errorf(ctx, "failed to describe vpc subnet %v: %v", name, err)
			return err
		}

		s := &v1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: v1.NamespaceDefault,
			},
			Spec: v1alpha1.SubnetSpec{
				ID:               resp.SubnetId,
				Name:             resp.Name,
				AvailabilityZone: resp.ZoneName,
				CIDR:             resp.Cidr,
			},
			Status: v1alpha1.SubnetStatus{
				AvailableIPNum: resp.AvailableIp,
				Enable:         true,
				HasNoMoreIP:    false,
			},
		}

		_, err = ipam.crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Create(s)
		if err != nil {
			log.Errorf(ctx, "failed to create subnet crd %v: %v", name, err)
			return err
		}

		return nil
	}

	// send event
	if isSubnetHasNoMoreIP(subnet) {
		ipam.eventRecorder.Eventf(&v1.ObjectReference{
			Kind: "Subnet",
			Name: "SubnetHasNoMoreIP",
		}, v1.EventTypeWarning, "SubnetHasNoMoreIP", "Subnet %v(%v) once has no more ip to allocate, consider disable it to prevent new eni creation", subnet.Spec.ID, subnet.Spec.CIDR)
	}

	return nil
}

func (ipam *IPAM) declareSubnetHasNoMoreIP(ctx context.Context, subnetID string, hasNoMoreIP bool) error {
	subnet, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(subnetID)
	if err != nil {
		log.Errorf(ctx, "failed to get subnet crd %v: %v", subnetID, err)
		return err
	}

	status := subnet.Status

	// the status is same, won't update
	if status.HasNoMoreIP == hasNoMoreIP {
		return nil
	}

	status.HasNoMoreIP = hasNoMoreIP
	err = ipam.patchSubnetStatus(ctx, subnet.Name, &status)
	if err != nil {
		return err
	}

	return nil
}

func (ipam *IPAM) patchSubnetStatus(ctx context.Context, name string, status *v1alpha1.SubnetStatus) error {
	json, err := json.Marshal(status)
	if err != nil {
		return err
	}
	patchData := []byte(fmt.Sprintf(`{"status":%s}`, json))
	_, err = ipam.crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Patch(name, ktypes.MergePatchType, patchData)
	if err != nil {
		log.Errorf(ctx, "failed to patch subnet crd %v: %v", name, err)
		return err
	}

	return nil
}
