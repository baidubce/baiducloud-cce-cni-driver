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
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	crdlisters "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
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

	// periodically update subnet
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
	var errs []error
	var subnets []string

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

	// in hybrid mode, bbc and bcc ipam are both enabled.
	// so bcc ipam can sync all subnet crs.
	subnets = append(subnets, pool.Spec.ENI.Subnets...)
	subnets = append(subnets, pool.Spec.PodSubnets...)

	for _, s := range subnets {
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
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			continue
		}

		// update metrics
		metric.SubnetAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, resp.ZoneName, resp.SubnetId).Set(float64(resp.AvailableIp))

		// update crd
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

		log.Warningf(ctx, "subnet cr for %v is not found, will create...", name)

		if err := ipamgeneric.CreateSubnetCR(ctx, ipam.cloud, ipam.crdClient, name); err != nil {
			log.Errorf(ctx, "failed to create subnet cr for %v: %v", name, err)
			return err
		}

		log.Infof(ctx, "create subnet cr for %v successfully", name)

		return nil
	}

	// send event
	if isSubnetHasNoMoreIP(subnet) {
		ipam.eventRecorder.Eventf(&v1.ObjectReference{
			Kind: "Subnet",
			Name: "SubnetHasNoMoreIP",
		}, v1.EventTypeWarning, "SubnetHasNoMoreIP", "Subnet %v(%v) has no more ip to allocate, consider disable it to prevent new eni creation", subnet.Spec.ID, subnet.Spec.CIDR)
	}

	return nil
}

func (ipam *IPAM) declareSubnetHasNoMoreIP(ctx context.Context, subnetID string, hasNoMoreIP bool) error {
	return ipamgeneric.DeclareSubnetHasNoMoreIP(ctx, ipam.crdClient, ipam.crdInformer, subnetID, hasNoMoreIP)
}

func (ipam *IPAM) patchSubnetStatus(ctx context.Context, name string, status *v1alpha1.SubnetStatus) error {
	return ipamgeneric.PatchSubnetStatus(ctx, ipam.crdClient, name, status)
}
