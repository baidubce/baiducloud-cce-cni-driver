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

package subnet

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	crdlisters "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8swatcher"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	// periodically update subnet cr status
	subnetSyncPeriod = 15 * time.Second

	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5
)

type SubnetController struct {
	crdInformer crdinformers.SharedInformerFactory
	crdClient   versioned.Interface

	cloud cloud.Interface

	informerResyncPeriod time.Duration
	eventRecorder        record.EventRecorder
}

func NewSubnetController(
	crdInformer crdinformers.SharedInformerFactory,
	crdClient versioned.Interface,
	cloud cloud.Interface,
	broadcaster record.EventBroadcaster) *SubnetController {
	eventRecorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam-subnet-controller"})
	sbnc := &SubnetController{
		crdInformer:   crdInformer,
		crdClient:     crdClient,
		cloud:         cloud,
		eventRecorder: eventRecorder,
	}
	return sbnc

}

func (sbnc *SubnetController) Run(stopCh <-chan struct{}) {
	group := &wait.Group{}
	group.Start(func() {
		// ensure subnet cr is created if ippool changed
		watcher := k8swatcher.NewIPPoolWatcher(sbnc.crdInformer.Cce().V1alpha1().IPPools(), sbnc.informerResyncPeriod)
		watcher.RegisterEventHandler(sbnc)
		watcher.Run(1, stopCh)
	})
	group.Start(func() {
		// periodically update subnet
		wait.Until(sbnc.updateSubnetStatus, subnetSyncPeriod, stopCh)
	})
	group.Wait()
}

// SyncIPPool ipam implements IPPoolHandler
func (sbnc *SubnetController) SyncIPPool(poolKey string, poolLister crdlisters.IPPoolLister) error {
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
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// in hybrid mode, bbc and bcc ipam are both enabled.
	// so bcc ipam can sync all subnet crs.
	subnets = append(subnets, pool.Spec.ENI.Subnets...)
	subnets = append(subnets, pool.Spec.PodSubnets...)

	for _, s := range subnets {
		err := sbnc.EnsureSubnetCRExists(ctx, s)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (sbnc *SubnetController) updateSubnetStatus() {
	ctx := log.NewContext()
	subnetList, err := sbnc.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).List(labels.Everything())
	if err != nil {
		log.Errorf(ctx, "failed to list subnet crd: %v", err)
		return
	}
	log.V(5).Infof(ctx, "start to resync subnets with len %d", len(subnetList))

	for _, crd := range subnetList {
		resp, err := sbnc.cloud.DescribeSubnet(ctx, crd.Spec.ID)
		if err != nil {
			log.Errorf(ctx, "failed to describe vpc subnet %v: %v", crd.Spec.ID, err)
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			continue
		}
		log.V(6).Infof(ctx, "resync subnet %s with cloud %s", log.ToJson(crd), log.ToJson(resp))
		// update metrics
		metric.SubnetAvailableIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, resp.ZoneName, resp.SubnetId).Set(float64(resp.AvailableIp))

		// update crd
		hasNoMoreIP := resp.AvailableIp <= 0

		if crd.Status.AvailableIPNum != resp.AvailableIp || hasNoMoreIP != crd.Status.HasNoMoreIP {
			sbn := crd.DeepCopy()
			sbn.Status.AvailableIPNum = resp.AvailableIp
			sbn.Status.HasNoMoreIP = hasNoMoreIP
			if err := sbnc.UpdateSbn(ctx, sbn); err != nil {
				log.Errorf(ctx, "failed to patch subnet %v status: %v", sbn.Spec.ID, err)
			}
		}
	}
}

func (sbnc *SubnetController) EnsureSubnetCRExists(ctx context.Context, name string) error {
	sbn, err := sbnc.Get(name)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		log.Warningf(ctx, "subnet cr for %v is not found, will create...", name)
		return sbnc.Create(ctx, name)
	}

	// send event
	if sbn.Status.HasNoMoreIP {
		sbnc.eventRecorder.Eventf(&v1.ObjectReference{
			Kind: "Subnet",
			Name: "SubnetHasNoMoreIP",
		}, v1.EventTypeWarning, "SubnetHasNoMoreIP", "Subnet %v(%v) has no more ip to allocate, consider disable it to prevent new eni creation", sbn.Spec.ID, sbn.Spec.CIDR)
	}

	return nil
}

func (sbnc *SubnetController) Get(name string) (*v1alpha1.Subnet, error) {
	return sbnc.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(name)
}

func (sbnc *SubnetController) Create(ctx context.Context, name string) error {
	if err := CreateSubnetCR(ctx, sbnc.cloud, sbnc.crdClient, name); err != nil {
		log.Errorf(ctx, "failed to create subnet cr for %v: %v", name, err)
		return err
	}

	log.Infof(ctx, "create subnet cr for %v successfully", name)
	return nil
}

func (sbnc *SubnetController) DeclareSubnetHasNoMoreIP(ctx context.Context, subnetID string, hasNoMoreIP bool) error {
	sbn, err := sbnc.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(subnetID)
	if err != nil {
		return err
	}
	sbnCopy := sbn.DeepCopy()
	if sbnCopy.Status.HasNoMoreIP != hasNoMoreIP {
		sbnCopy.Status.HasNoMoreIP = hasNoMoreIP
		return UpdateSubnet(ctx, sbnc.crdClient, sbn)
	}
	return nil
}

func (sbnc *SubnetController) UpdateSbn(ctx context.Context, sbn *v1alpha1.Subnet) error {
	return UpdateSubnet(ctx, sbnc.crdClient, sbn)
}
