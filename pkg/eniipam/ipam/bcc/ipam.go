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
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/juju/ratelimit"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/clock"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/topology_spread"
	datastorev1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/ipcache"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	// minPrivateIPLifeTime is the lifetime of a private ip (from allocation to release), aim to trade off db slave delay
	minPrivateIPLifeTime = 5 * time.Second

	minIdleIPPoolSize = 1

	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5

	increasePoolSizePerNode = 10
)

func (ipam *IPAM) Ready(_ context.Context) bool {
	return ipam.cacheHasSynced
}

func NewIPAM(
	kubeClient kubernetes.Interface,
	crdClient versioned.Interface,
	kubeInformer informers.SharedInformerFactory,
	crdInformer crdinformers.SharedInformerFactory,
	bceClient cloud.Interface,
	cniMode types.ContainerNetworkMode,
	vpcID string,
	clusterID string,
	subnetSelectionPolicy SubnetSelectionPolicy,
	ipMutatingRate float64,
	ipMutatingBurst int64,
	idleIPPoolMinSize int,
	idleIPPoolMaxSize int,
	batchAddIPNum int,
	eniSyncPeriod time.Duration,
	gcPeriod time.Duration,
	_ bool,
) (ipamgeneric.Interface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	// build subsystem controller
	sbnController := subnet.NewSubnetController(crdInformer, crdClient, bceClient, eventBroadcaster)
	pstsController := topology_spread.NewTopologySpreadController(kubeInformer, crdInformer, crdClient, eventBroadcaster, sbnController)

	// min size of idle ip pool must >= 1
	if idleIPPoolMinSize < minIdleIPPoolSize {
		idleIPPoolMinSize = minIdleIPPoolSize
	}
	ipam := &IPAM{
		eventBroadcaster:        eventBroadcaster,
		eventRecorder:           recorder,
		kubeInformer:            kubeInformer,
		kubeClient:              kubeClient,
		crdInformer:             crdInformer,
		crdClient:               crdClient,
		cloud:                   bceClient,
		clock:                   clock.RealClock{},
		cniMode:                 cniMode,
		vpcID:                   vpcID,
		clusterID:               clusterID,
		subnetSelectionPolicy:   subnetSelectionPolicy,
		eniSyncPeriod:           eniSyncPeriod,
		gcPeriod:                gcPeriod,
		eniCache:                ipcache.NewCacheMapArray[*enisdk.Eni](),
		privateIPNumCache:       make(map[string]int),
		possibleLeakedIPCache:   make(map[eniAndIPAddrKey]time.Time),
		addIPBackoffCache:       ipcache.NewCacheMap[*wait.Backoff](),
		allocated:               ipcache.NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
		reusedIPs:               ipcache.NewReuseIPAndWepPool(),
		datastore:               datastorev1.NewDataStore(),
		bucket:                  ratelimit.NewBucketWithRate(ipMutatingRate, ipMutatingBurst),
		idleIPPoolMinSize:       idleIPPoolMinSize,
		idleIPPoolMaxSize:       idleIPPoolMaxSize,
		batchAddIPNum:           batchAddIPNum,
		buildDataStoreEventChan: make(map[string]chan *event),
		increasePoolEventChan:   make(map[string]chan *event),
		cacheHasSynced:          false,

		// subsystem
		sbnCtl: sbnController,
		tsCtl:  pstsController,
	}
	ipam.exclusiveSubnetCond = sync.NewCond(&ipam.lock)
	ipam.exclusiveSubnetFlag = make(map[string]bool)
	return ipam, nil
}

func (ipam *IPAM) Run(ctx context.Context, stopCh <-chan struct{}) error {
	defer func() {
		runtime.HandleCrash()
	}()

	log.Info(ctx, "Starting cce ipam controller for BCC")
	defer log.Info(ctx, "Shutting down cce ipam controller for BCC")

	log.Infof(ctx, "limit ip mutating rate to %v, burst to %v", ipam.bucket.Rate(), ipam.bucket.Capacity())

	nodeInformer := ipam.kubeInformer.Core().V1().Nodes().Informer()
	podInformer := ipam.kubeInformer.Core().V1().Pods().Informer()
	stsInformer := ipam.kubeInformer.Apps().V1().StatefulSets().Informer()
	wepInformer := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()
	ippoolInformer := ipam.crdInformer.Cce().V1alpha1().IPPools().Informer()
	subnetInformer := ipam.crdInformer.Cce().V1alpha1().Subnets().Informer()
	pstsInformer := ipam.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Informer()

	wepInformer.AddEventHandler(ipam.reusedIPs)

	ipam.kubeInformer.Start(stopCh)
	ipam.crdInformer.Start(stopCh)
	if !cache.WaitForNamedCacheSync(
		"cce-ipam",
		stopCh,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		stsInformer.HasSynced,
		wepInformer.HasSynced,
		ippoolInformer.HasSynced,
		subnetInformer.HasSynced,
		pstsInformer.HasSynced,
	) {
		log.Warning(ctx, "failed WaitForCacheSync, timeout")
		return nil
	} else {
		log.Info(ctx, "WaitForCacheSync done")
	}

	// rebuild allocated cache
	err := ipam.buildAllocatedCache(ctx)
	if err != nil {
		return err
	}
	// rebuild datastore cache
	err = ipam.buildAllocatedNodeCache(ctx)
	if err != nil {
		return err
	}

	group := &wait.Group{}

	group.Start(func() {
		if err := ipam.syncENI(stopCh); err != nil {
			log.Errorf(ctx, "failed to sync eni info: %v", err)
		}
	})

	group.Start(func() {
		if err := ipam.gc(stopCh); err != nil {
			log.Errorf(ctx, "failed to start ipam gc: %v", err)
		}
	})

	group.Start(func() {
		if err := ipam.checkIdleIPPoolPeriodically(); err != nil {
			log.Errorf(ctx, "failed to start checkIdleIPPoolPeriodically: %v", err)
		}
	})

	subsystemRun := func(sc subsystemController) {
		sc.Run(stopCh)
	}
	// run subsystem controller
	group.Start(func() {
		subsystemRun(ipam.sbnCtl.(subsystemController))
	})
	group.Start(func() {
		subsystemRun(ipam.tsCtl.(subsystemController))
	})
	group.Start(func() {
		if err := ipam.reconcileRelationOfWepEni(stopCh); err != nil {
			log.Errorf(ctx, "failed to start reconcileRelationOfWepEni: %v", err)
		}
	})
	// k8s resource and ip cache are synced
	ipam.cacheHasSynced = true
	group.Wait()
	<-stopCh
	return nil
}

func (ipam *IPAM) buildAllocatedCache(ctx context.Context) error {
	selector, err := wepListerSelector()
	if err != nil {
		log.Errorf(ctx, "error parsing requirement: %v", err)
		return err
	}

	wepList, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().List(selector)
	if err != nil {
		return err
	}
	for _, wep := range wepList {
		nwep := wep.DeepCopy()
		ipam.allocated.Add(wep.Spec.IP, nwep)
		log.Infof(ctx, "build allocated pod cache: found IP %v assigned to pod (%v %v)", wep.Spec.IP, wep.Namespace, wep.Name)
	}
	return nil
}

func (ipam *IPAM) allocateIPForFixedIPPod(ctx context.Context, node *v1.Node, pod *v1.Pod, containerID string,
	enis []*enisdk.Eni, wep *v1alpha1.WorkloadEndpoint, deadline time.Time) (*v1alpha1.WorkloadEndpoint, error) {
	var allocatedIP string
	var allocatedEni *enisdk.Eni
	var allocatedErr error

	// 1.1 try to allocate fixed ip
	allocatedIP, allocatedEni, allocatedErr = ipam.allocateFixedIPFromCloud(ctx, node, enis, wep, deadline)
	if allocatedErr != nil {
		log.Warningf(ctx, "allocate fixed ip %s for pod (%s %s) failed: %s",
			wep.Spec.IP, wep.Namespace, wep.Name, allocatedErr)

		// 1.2. if 1.1 failed, try to allocate new ip for sts pod
		if timeoutErr := assertDeadline(deadline); timeoutErr != nil {
			return nil, timeoutErr
		}
		log.Infof(ctx, "try to allocate new ip for pod (%s %s)", wep.Namespace, wep.Name)

		allocatedIP, allocatedEni, allocatedErr = ipam.tryToAllocateIPFromCache(ctx, node, enis, deadline)
		if allocatedErr != nil {
			log.Errorf(ctx, "allocate ip for pod (%s %s) failed: %s", wep.Namespace, wep.Name, allocatedErr)
			return nil, allocatedErr
		}
	}

	// 2. update wep
	newWep := ipam.fillFieldsToWep(wep, pod, containerID, allocatedIP, allocatedEni)

	_, updateErr := ipam.crdClient.CceV1alpha1().WorkloadEndpoints(newWep.Namespace).Update(ctx, newWep, metav1.UpdateOptions{})
	if updateErr != nil {
		log.Errorf(ctx, "failed to update wep for pod (%s %s): %s", pod.Namespace, pod.Name, updateErr)
		time.Sleep(minPrivateIPLifeTime)

		if rollbackErr := ipam.tryDeleteSubnetIPRetainAllocateCache(ctx, newWep); rollbackErr != nil {
			log.Errorf(ctx, "rollback wep for pod (%s %s) failed: %s", pod.Namespace, pod.Name, rollbackErr)
		}
		return nil, updateErr
	}
	log.Infof(ctx, "update wep with spec %+v for pod (%v %v) successfully", newWep.Spec, newWep.Namespace, newWep.Name)

	// 3. update cache ip and wep
	ipam.allocated.Add(allocatedIP, newWep)
	return newWep, nil
}

func (ipam *IPAM) allocateFixedIPFromCloud(ctx context.Context, node *v1.Node, enis []*enisdk.Eni,
	wep *v1alpha1.WorkloadEndpoint, deadline time.Time) (string, *enisdk.Eni, error) {
	var namespace, name = wep.Namespace, wep.Name

	// Note: here DeletePrivateIP and AddPrivateIP should be atomic. we leverage a lock to do this
	// Ensures private ip not attached to other eni
	deleteErr := ipam.__tryDeleteIPByIPAndENI(ctx, wep.Spec.Node, wep.Spec.IP, wep.Spec.ENIID, true)
	if deleteErr != nil && !cloud.IsErrorENIPrivateIPNotFound(deleteErr) {
		log.Errorf(ctx, "error delete private IP %v for pod (%v %v): %v", wep.Spec.IP, namespace, name, deleteErr)
		if cloud.IsErrorRateLimit(deleteErr) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
	}

	// try to allocate fixed ip
	var ipToAllocate = wep.Spec.IP
	var backoff = ipamgeneric.CCECniTimeout / time.Duration(len(enis))
	var addIPErrors []error
	var successEni *enisdk.Eni
	var successIP string
	for _, oneEni := range enis {
		if timeoutErr := assertDeadline(deadline); timeoutErr != nil {
			return "", nil, timeoutErr
		}
		log.Infof(ctx, "try to add IP %v to %v", ipToAllocate, oneEni.EniId)
		ipResult, addErr := ipam.batchAddPrivateIP(ctx, []string{ipToAllocate}, 0, oneEni.EniId)
		if addErr != nil {
			addIPErrors = append(addIPErrors, addErr)
			ipam.handleAllocateError(ctx, addErr, oneEni, backoff)
			if cloud.IsErrorPrivateIPInUse(addErr) {
				break
			}

			// failed: continue to try other eni
			if cloud.IsErrorRateLimit(addErr) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
		} else {
			// success
			successEni = oneEni
			successIP = ipResult[0]
			break
		}
	}

	if successEni == nil {
		return "", nil, fmt.Errorf("all %d enis binded cannot add IP %s: %v",
			len(enis), wep.Spec.IP, utilerrors.NewAggregate(addIPErrors))
	}

	log.Infof(ctx, "add private IP %s for pod (%s %s) successfully", successIP, namespace, name)

	ipam.incPrivateIPNumCache(successEni.EniId, false)
	storeErr := ipam.datastore.AddPrivateIPToStore(node.Name, successEni.EniId, successIP, true)
	if storeErr != nil {
		log.Warningf(ctx, "add private IP %s to store failed: %s", successIP, storeErr)
	}
	metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID,
		successEni.SubnetId).Inc()

	return successIP, successEni, nil
}

func (ipam *IPAM) allocateIPForOrdinaryPod(ctx context.Context, node *v1.Node, pod *v1.Pod, containerID string,
	enis []*enisdk.Eni, wep *v1alpha1.WorkloadEndpoint, deadline time.Time) (*v1alpha1.WorkloadEndpoint, error) {
	// 1. allocate ip from cache
	allocatedIP, allocatedEni, allocatedErr := ipam.tryToAllocateIPFromCache(ctx, node, enis, deadline)
	if allocatedErr != nil {
		log.Errorf(ctx, "allocate ip for pod (%s %s) failed: %s", pod.Namespace, pod.Name, allocatedErr)
		return nil, allocatedErr
	}

	// 2. update or create wep
	newWep := ipam.fillFieldsToWep(wep, pod, containerID, allocatedIP, allocatedEni)
	if wep == nil {
		// create wep
		_, createErr := ipam.crdClient.CceV1alpha1().WorkloadEndpoints(newWep.Namespace).
			Create(ctx, newWep, metav1.CreateOptions{})
		if createErr != nil {
			if rollbackErr := ipam.tryDeleteIPByWep(ctx, newWep); rollbackErr != nil {
				log.Errorf(ctx, "rollback wep for pod (%s %s) failed: %s", pod.Namespace, pod.Name, rollbackErr)
			}
			return nil, createErr
		}
	} else {
		// update wep
		_, updateErr := ipam.crdClient.CceV1alpha1().WorkloadEndpoints(newWep.Namespace).
			Update(ctx, newWep, metav1.UpdateOptions{})
		if updateErr != nil {
			time.Sleep(minPrivateIPLifeTime)
			if rollbackErr := ipam.tryDeleteSubnetIPRetainAllocateCache(ctx, newWep); rollbackErr != nil {
				log.Errorf(ctx, "rollback wep for pod (%s %s) failed: %s", pod.Namespace, pod.Name, rollbackErr)
			}
			return nil, updateErr
		}
	}
	log.Infof(ctx, "update or create wep with spec %+v for pod (%v %v) successfully",
		newWep.Spec, newWep.Namespace, newWep.Name)

	// 3. update cache ip and wep
	ipam.allocated.Add(allocatedIP, newWep)
	return newWep, nil
}

// tryToAllocateIPFromCache get ip from local cache
// increasing the pool if datastore has no available ip
func (ipam *IPAM) tryToAllocateIPFromCache(ctx context.Context, node *v1.Node, enis []*enisdk.Eni, deadline time.Time) (
	ipResult string, eni *enisdk.Eni, err error) {
	firstEvent := true

	// it takes 1s to allocate ip from cloud, retry 5 and wait 200ms+ per times
	backoff := wait.Backoff{
		Steps:    5,
		Duration: 200 * time.Millisecond,
		Factor:   1.0,
		Jitter:   0.1,
	}

	err = wait.ExponentialBackoff(backoff, func() (done bool, err error) {
		if timeoutErr := assertDeadline(deadline); timeoutErr != nil {
			return false, timeoutErr
		}
		if ipam.canAllocateIP(ctx, node.Name, enis) {
			return true, nil
		}

		if err := ipam.assertNodeCanIncreasePool(ctx, node, enis); err != nil {
			return true, err
		}
		// only send one increase pool event for one ctx
		if firstEvent {
			ipam.sendIncreasePoolEvent(ctx, node, enis, true)
			firstEvent = false
		}
		return false, nil
	})
	if err == wait.ErrWaitTimeout {
		return "", nil, fmt.Errorf("allocate ip for node %v error", node.Name)
	}
	if err != nil {
		return "", nil, err
	}

	return ipam.allocateIPFromCache(ctx, node.Name, enis)
}

func (ipam *IPAM) canAllocateIP(_ context.Context, nodeName string, enis []*enisdk.Eni) bool {
	var (
		total = 0
		used  = 0
	)

	for _, eni := range enis {
		t, u, err := ipam.datastore.GetENIStats(nodeName, eni.EniId)
		if err == nil {
			total += t
			used += u
		}
	}

	return total > used
}

func (ipam *IPAM) allocateIPFromCache(ctx context.Context, node string, enis []*enisdk.Eni) (string, *enisdk.Eni, error) {
	var (
		failedEniList []string
	)

	ipam.sortENIByDatastoreStats(node, enis, false)

	// iterate each subnet, try to allocate
	for _, eni := range enis {
		ipResult, err := ipam.datastore.AllocatePodPrivateIPByENI(node, eni.EniId)
		if err == nil {
			// allocate one successfully
			return ipResult, eni, nil
		}
		log.Warningf(ctx, "datastore try allocate ip for node %v in eni %v failed: %v", node, eni.EniId, err)
		failedEniList = append(failedEniList, eni.EniId)
	}

	return "", nil, fmt.Errorf("no available ip address in datastore for node %v in enis: %v", node, failedEniList)
}

func (ipam *IPAM) batchAllocateIPFromCloud(
	ctx context.Context,
	eni *enisdk.Eni,
	node *v1.Node,
	batchAddNum int,
	backoffCap time.Duration,
) ([]string, error) {
	var ipResult []string
	var err error

	for batchAddNum > 0 {
		if err := ipam.assertEniCanIncreasePool(ctx, node, eni); err != nil {
			break
		}

		ipResult, err = ipam.batchAllocateIPWithBackoff(ctx, batchAddNum, eni)
		if err == nil {
			log.Infof(ctx, "batch add %d private ip(s) for %s on eni %s successfully, %v",
				batchAddNum, node.Name, eni.EniId, ipResult)
			break
		}

		if err != nil {
			// retry condition: batchAddNum > 1 && isErrorNeedExponentialBackoff
			log.Warningf(ctx, "batch add %s private ip(s) for node %s failed: %s", batchAddNum, node.Name, err)

			if batchAddNum == 1 {
				ipam.handleAllocateError(ctx, err, eni, backoffCap)
			}

			if isErrorNeedExponentialBackoff(err) {
				// decrease batchAddNum then retry
				batchAddNum = batchAddNum >> 1
				continue
			}

			// don't retry for other errors
			break
		}
	}

	if err != nil {
		return nil, err
	}

	for _, allocatedIP := range ipResult {
		ipam.incPrivateIPNumCache(eni.EniId, false)
		err := ipam.datastore.AddPrivateIPToStore(node.Name, eni.EniId, allocatedIP, false)
		if err != nil {
			log.Errorf(ctx, "add private ip %s to datastore failed: %v", allocatedIP, err)
		}
		metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, eni.SubnetId).Inc()
	}

	return ipResult, nil
}

// handleAllocateError
//
//	isErrorSubnetHasNoMoreIP: declare subnet = no more ip
//	isErrorNeedExponentialBackoff: add backoff to addIPBackoffCache
func (ipam *IPAM) handleAllocateError(ctx context.Context, allocateErr error, eni *enisdk.Eni, backoffCap time.Duration) {
	if cloud.IsErrorSubnetHasNoMoreIP(allocateErr) {
		if e := ipam.sbnCtl.DeclareSubnetHasNoMoreIP(ctx, eni.SubnetId, true); e != nil {
			log.Errorf(ctx, "failed to patch subnet %v that has no more ip: %v", eni.SubnetId, e)
		}
	}
	if isErrorNeedExponentialBackoff(allocateErr) {
		if _, ok := ipam.addIPBackoffCache.Get(eni.EniId); !ok {
			ipam.addIPBackoffCache.Add(eni.EniId, util.NewBackoffWithCap(backoffCap))
			log.Infof(ctx, "add backoff with cap %s for eni %s due to error: %v",
				backoffCap, eni.EniId, allocateErr)
		}
	}
}

func (ipam *IPAM) handleIncreasePoolEvent(ctx context.Context, nodeName string, ch chan *event) {
	log.Infof(ctx, "start increase pool goroutine for node %v", nodeName)
	for e := range ch {
		var (
			enis = e.enis
			node = e.node
			ctx  = e.ctx
			err  error
		)

		if len(enis) == 0 {
			log.Warningf(ctx, "no eni in node %s, skip increase pool", nodeName)
			continue
		}
		if ipam.canIgnoreIncreasingEvent(ctx, e) {
			continue
		}

		ipam.sortENIByDatastoreStats(node.Name, enis, true)

		backoff := ipamgeneric.CCECniTimeout / time.Duration(len(enis))
		for _, eni := range enis {
			_, err = ipam.batchAllocateIPFromCloud(ctx, eni, node, ipam.batchAddIPNum, backoff)
			if err == nil {
				break
			}
			if cloud.IsErrorRateLimit(err) {
				// wait to try other eni
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			log.Warningf(ctx, "failed to batch add ip on eni %s: %s", eni.EniId, err)
		}
	}
	log.Infof(ctx, "closed channel for node %s, exit...", nodeName)
}

func (ipam *IPAM) canIgnoreIncreasingEvent(ctx context.Context, evt *event) bool {
	if evt.passive && ipam.canAllocateIP(ctx, evt.node.Name, evt.enis) {
		return true
	}

	if !evt.passive && ipam.idleIPNum(evt.node.Name) >= ipam.idleIPPoolMinSize {
		return true
	}
	return false
}

func (ipam *IPAM) buildAllocatedNodeCache(ctx context.Context) error {
	var (
		wg sync.WaitGroup
		ch = make(chan struct{}, 10)
	)

	bccNodeSelector, _ := bccNodeListerSelector()
	nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(bccNodeSelector)
	if err != nil {
		log.Errorf(ctx, "failed to list bcc nodes: %v", err)
		return err
	}

	for _, node := range nodes {
		ch <- struct{}{}
		wg.Add(1)

		go func(node *v1.Node) {
			defer func() {
				wg.Done()
				<-ch
			}()

			ctx := log.NewContext()
			instanceID, err := util.GetInstanceIDFromNode(node)
			if err != nil {
				return
			}

			err = ipam.rebuildNodeDataStoreCache(ctx, node, instanceID)
			if err != nil {
				log.Errorf(ctx, "rebuild cache for node %v error: %v", node.Name, err)
			}
		}(node)
	}

	wg.Wait()

	return nil
}

func (ipam *IPAM) rebuildNodeDataStoreCache(ctx context.Context, node *v1.Node, instanceID string) error {
	listArgs := enisdk.ListEniArgs{
		VpcId:      ipam.vpcID,
		InstanceId: instanceID,
		Name:       fmt.Sprintf("%s/", ipam.clusterID),
	}

	var enis []enisdk.Eni

	// list eni with backoff
	err := wait.ExponentialBackoff(retry.DefaultBackoff, func() (bool, error) {
		var err error
		enis, err = ipam.cloud.ListENIs(ctx, listArgs)
		if err != nil {
			log.Errorf(ctx, "failed to list enis attached to instance %s: %v", instanceID, err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		log.Errorf(ctx, "failed to backoff list enis attached to instance %s: %v", instanceID, err)
		return err
	}

	err = ipam.datastore.AddNodeToStore(node.Name, instanceID)
	if err != nil {
		log.Errorf(ctx, "failed to add node to datastore: %v", err)
		return err
	}

	ipam.lock.Lock()
	defer ipam.lock.Unlock()
	// add ip to store
	for _, eni := range enis {
		ipam.removeAddIPBackoffCache(eni.EniId, true)
		if err := ipam.datastore.AddENIToStore(node.Name, eni.EniId); err == nil {
			log.Infof(ctx, "add eni %v from node %v to datastore successfully", eni.EniId, node.Name)
		}
		sbn, err := ipam.sbnCtl.Get(eni.SubnetId)
		if err != nil {
			log.Errorf(ctx, "get subnet %s by eni %s on node %s error %v", eni.SubnetId, eni.EniId, node.Name, err)
			continue
		}
		// validate ipv4 in CIDR
		_, ipNet, err := net.ParseCIDR(sbn.Spec.CIDR)
		if err != nil {
			log.Errorf(ctx, "parse subnet %s CIDR error by eni %s on node %s error %v", eni.SubnetId, eni.EniId, node.Name, err)
			continue
		}

		for _, ip := range eni.PrivateIpSet {
			if !ip.Primary {
				// ipam will sync all weps to build allocated cache when it starts
				assigned := ipam.allocated.Exists(ip.PrivateIpAddress)

				// If the private IP is not in the CIDR of Eni, it means that it is a cross subnet IP
				crossSubnet := !ipNet.Contains(net.ParseIP(ip.PrivateIpAddress))
				if crossSubnet {
					assigned = true
				}

				syncErr := ipam.datastore.Synchronized(func() error {
					err := ipam.datastore.AddPrivateIPToStoreUnsafe(node.Name, eni.EniId, ip.PrivateIpAddress, assigned, crossSubnet)
					if err != nil {
						msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip.PrivateIpAddress, err)
						log.Error(ctx, msg)
					} else {
						log.Infof(ctx, "add private ip %v(assigned: %v) from eni %v to node %v datastore successfully", ip.PrivateIpAddress, assigned, eni.EniId, node.Name)
					}
					return err
				})
				if syncErr != nil {
					log.Warningf(ctx, "datastore Synchronized failed: %s", syncErr)
				}
			}
		}
	}

	total, assigned, err := ipam.datastore.GetNodeStats(node.Name)
	if err == nil {
		log.Infof(ctx, "total, used at initialization for node %v: %v %v", node.Name, total, assigned)
	}

	return nil
}

func (ipam *IPAM) handleRebuildNodeDatastoreEvent(ctx context.Context, node *v1.Node, ch chan *event) error {
	var (
		nodeName = node.Name
	)

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				ctx := log.NewContext()
				log.Infof(ctx, "closed channel for node %v, exit...", nodeName)
				return nil
			}

			log.Infof(ctx, "start rebuilding datastore for node %v", e.node.Name)
			instanceID, err := util.GetInstanceIDFromNode(e.node)
			if err != nil {
				return err
			}

			ctx = e.ctx

			if !ipam.datastore.NodeExistsInStore(nodeName) {
				err := ipam.rebuildNodeDataStoreCache(ctx, e.node, instanceID)
				if err != nil {
					log.Errorf(ctx, "rebuild cache for node %v error: %v", nodeName, err)
				} else {
					log.Infof(ctx, "rebuild cache for node %v done", nodeName)
				}
			}
		default:
			ctx = log.NewContext()
			if ipam.datastore.NodeExistsInStore(nodeName) {
				log.Infof(ctx, "node %v already in datastore", nodeName)
				return nil
			}
		}
	}
}

func (ipam *IPAM) addIPBackoff(eniID string, cap time.Duration) {
	ipam.addIPBackoffCache.AddIfNotExists(eniID, util.NewBackoffWithCap(cap))
}

func (ipam *IPAM) removeAddIPBackoffCache(eniID string, _ bool) bool {
	return ipam.addIPBackoffCache.Delete(eniID)
}

func (ipam *IPAM) updateIPPoolStatus(ctx context.Context, node *v1.Node, instanceID string, enis []enisdk.Eni) error {

	var (
		eniStatus  = map[string]v1alpha1.ENI{}
		ippoolName = utilippool.GetNodeIPPoolName(node.Name)
	)

	for _, eni := range enis {
		if eni.Status != utileni.ENIStatusInuse || !utileni.ENIOwnedByNode(&eni, ipam.clusterID, instanceID) {
			continue
		}

		ippool, err := ipam.crdInformer.Cce().V1alpha1().IPPools().Lister().IPPools(v1.NamespaceDefault).Get(ippoolName)
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", ippoolName, err)
			return err
		}

		// interfaceIndex cannot be fetched by eni-ipam, node-agent will update it
		// we get it from origin crd and store it
		var linkIndex int
		if _, ok := ippool.Status.ENI.ENIs[eni.EniId]; ok {
			linkIndex = ippool.Status.ENI.ENIs[eni.EniId].InterfaceIndex
		} else {
			linkIndex = -1
		}

		eniStatus[eni.EniId] = v1alpha1.ENI{
			ID:               eni.EniId,
			MAC:              eni.MacAddress,
			AvailabilityZone: eni.ZoneName,
			Description:      eni.Description,
			InterfaceIndex:   linkIndex,
			Subnet:           eni.SubnetId,
			PrivateIPSet:     utileni.GetPrivateIPSet(&eni),
			VPC:              eni.VpcId,
		}
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := ipam.crdInformer.Cce().V1alpha1().IPPools().Lister().IPPools(v1.NamespaceDefault).Get(ippoolName)
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", ippoolName, err)
			return err
		}
		result.Status.ENI.ENIs = eniStatus

		_, updateErr := ipam.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(ctx, result, metav1.UpdateOptions{})
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v status: %v", ippoolName, updateErr)
			return updateErr
		}
		return nil
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v status: %v", ippoolName, retryErr)
		return retryErr
	}

	return nil
}

func (ipam *IPAM) checkIdleIPPool() (bool, error) {
	var (
		ctx = log.NewContext()
		wg  sync.WaitGroup
		ch  = make(chan struct{}, 10)
	)

	bccNodeSelector, _ := bccNodeListerSelector()
	nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(bccNodeSelector)
	if err != nil {
		log.Errorf(ctx, "failed to list bbc nodes: %v", err)
		return false, nil
	}

	for _, node := range nodes {
		ch <- struct{}{}
		wg.Add(1)

		go func(node *v1.Node) {
			defer func() {
				wg.Done()
				<-ch
			}()

			ctx = log.NewContext()

			if !ipam.datastore.NodeExistsInStore(node.Name) {
				return
			}

			idle := ipam.idleIPNum(node.Name)
			if idle < ipam.idleIPPoolMinSize {
				log.Infof(ctx, "ipam will increase pool due to idle ip num %v less than --idle-ip-pool-min-size %v", idle, ipam.idleIPPoolMinSize)

				enis, ok := ipam.eniCache.Get(node.Name)
				if !ok {
					log.Warningf(ctx, "no eni in node %s", node.Name)
					return
				}
				ipam.sendIncreasePoolEvent(ctx, node, enis, false)
			}

		}(node)
	}

	wg.Wait()

	return false, nil
}

// sendIncreasePoolEvent send increase poll event to cache chan
// create chan if not exists
func (ipam *IPAM) sendIncreasePoolEvent(ctx context.Context, node *v1.Node, enis []*enisdk.Eni, passive bool) {
	var (
		evt = &event{
			node:    node,
			enis:    enis,
			passive: passive,
			ctx:     ctx,
		}
		ch chan *event
		ok bool
	)

	ipam.lock.Lock()
	ch, ok = ipam.increasePoolEventChan[evt.node.Name]
	if !ok {
		ch = make(chan *event, increasePoolSizePerNode)
		ipam.increasePoolEventChan[node.Name] = ch
		go ipam.handleIncreasePoolEvent(ctx, node.Name, ch)
	}
	ipam.lock.Unlock()

	select {
	case ch <- evt:
		return
	default:
		log.Warningf(ctx, "node %s increase pool is full", node.Name)
		return
	}
}

func (ipam *IPAM) checkIdleIPPoolPeriodically() error {
	return wait.PollImmediateInfinite(30*time.Second, ipam.checkIdleIPPool)
}

func (ipam *IPAM) batchAddPrivateIP(ctx context.Context, privateIPs []string, batchAddNum int, eniID string) ([]string, error) {
	ipam.bucket.Wait(1)
	return ipam.cloud.BatchAddPrivateIP(ctx, privateIPs, batchAddNum, eniID)
}

// batchAllocateIPWithBackoff allocate ip by eni from cloud with backoff if exists
func (ipam *IPAM) batchAllocateIPWithBackoff(ctx context.Context, batchAddNum int, eni *enisdk.Eni) ([]string, error) {
	var backoffWaitPeriod time.Duration
	var backoff *wait.Backoff
	var ok bool
	const waitPeriodNum = 10

	ipam.lock.Lock()
	backoff, ok = ipam.addIPBackoffCache.Get(eni.EniId)
	if backoff != nil && backoff.Steps >= 0 {
		backoffWaitPeriod = backoff.Step()
	}
	ipam.lock.Unlock()

	// backoff wait
	if backoffWaitPeriod != 0 {
		log.Infof(ctx, "backoff: wait %v to allocate private ip on %v", backoffWaitPeriod, eni.EniId)

		// instead of sleep a large amount of time, we divide sleep time into small parts to check backoff cache.
		for i := 0; i < waitPeriodNum; i++ {
			time.Sleep(backoffWaitPeriod / waitPeriodNum)
			ipam.lock.RLock()
			backoff, ok = ipam.addIPBackoffCache.Get(eni.EniId)
			ipam.lock.RUnlock()
			if !ok {
				log.Warningf(ctx, "found backoff on eni %v removed", eni.EniId)
				break
			}
		}
	}

	ipResults, batchErr := ipam.batchAddPrivateIP(ctx, []string{}, batchAddNum, eni.EniId)
	if batchErr == nil {
		ipam.removeAddIPBackoffCache(eni.EniId, false)
	}
	return ipResults, batchErr
}

func (ipam *IPAM) sortENIByDatastoreStats(node string, enis []*enisdk.Eni, byTotal bool) {
	sort.Slice(enis, func(i, j int) bool {
		total1, assigned1, _ := ipam.datastore.GetENIStats(node, enis[i].EniId)
		total2, assigned2, _ := ipam.datastore.GetENIStats(node, enis[j].EniId)

		if !byTotal {
			idle1, idle2 := total1-assigned1, total2-assigned2
			return idle1 >= idle2
		}

		return total1 < total2
	})
}

func (ipam *IPAM) incPrivateIPNumCache(eniID string, lockless bool) {
	if !lockless {
		ipam.lock.Lock()
		defer ipam.lock.Unlock()
	}
	ipam.privateIPNumCache[eniID]++
}

func (ipam *IPAM) decPrivateIPNumCache(eniID string, lockless bool) {
	if !lockless {
		ipam.lock.Lock()
		defer ipam.lock.Unlock()
	}
	ipam.privateIPNumCache[eniID]--
}

func (ipam *IPAM) idleIPNum(node string) int {
	total, used, err := ipam.datastore.GetNodeStats(node)
	idle := total - used
	if err == nil && idle >= 0 {
		return idle
	}
	return 0
}

func isErrorNeedExponentialBackoff(err error) bool {
	return cloud.IsErrorVmMemoryCanNotAttachMoreIpException(err) || cloud.IsErrorSubnetHasNoMoreIP(err)
}

func buildInstanceIdToNodeNameMap(ctx context.Context, nodes []*v1.Node) map[string]string {
	instanceIdToNodeNameMap := make(map[string]string, len(nodes))
	for _, n := range nodes {
		instanceId, err := util.GetInstanceIDFromNode(n)
		if err != nil {
			log.Warningf(ctx, "warning: cannot get instanceID of node %v", n.Name)
			continue
		}
		instanceIdToNodeNameMap[instanceId] = n.Name
	}
	return instanceIdToNodeNameMap
}

func wepListerSelector() (labels.Selector, error) {
	// for wep owned by bcc, use selector "cce.io/subnet-id", to be compatible with old versions.
	requirement, err := labels.NewRequirement(ipamgeneric.WepLabelSubnetIDKey, selection.Exists, nil)
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}

func bccNodeListerSelector() (labels.Selector, error) {
	requirement, err := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BCC", "GPU", "DCC"})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}

func (ipam *IPAM) fillFieldsToWep(wep *v1alpha1.WorkloadEndpoint, pod *v1.Pod, containerID,
	allocatedIP string, allocatedEni *enisdk.Eni) *v1alpha1.WorkloadEndpoint {
	if wep == nil {
		wep = &v1alpha1.WorkloadEndpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name:       pod.Name,
				Namespace:  pod.Namespace,
				Finalizers: []string{ipamgeneric.WepFinalizer},
			},
			Spec: v1alpha1.WorkloadEndpointSpec{
				Type: ipamgeneric.WepTypePod,
			},
		}
	}
	if wep.Labels == nil {
		wep.Labels = make(map[string]string)
	}
	wep.Spec.ContainerID = containerID
	wep.Spec.IP = allocatedIP
	wep.Spec.ENIID = allocatedEni.EniId
	wep.Spec.Mac = allocatedEni.MacAddress
	wep.Spec.Node = pod.Spec.NodeName
	wep.Spec.SubnetID = allocatedEni.SubnetId
	wep.Spec.UpdateAt = metav1.Time{Time: time.Unix(0, 0)}
	wep.Labels[ipamgeneric.WepLabelSubnetIDKey] = allocatedEni.SubnetId
	wep.Labels[ipamgeneric.WepLabelInstanceTypeKey] = string(metadata.InstanceTypeExBCC)
	if k8sutil.IsStatefulSetPod(pod) {
		wep.Spec.Type = ipamgeneric.WepTypeSts
		wep.Labels[ipamgeneric.WepLabelStsOwnerKey] = util.GetStsName(wep)
	}
	if pod.Annotations != nil {
		wep.Spec.EnableFixIP = pod.Annotations[StsPodAnnotationEnableFixIP]
		wep.Spec.FixIPDeletePolicy = pod.Annotations[StsPodAnnotationFixIPDeletePolicy]
	}
	return wep
}

func (ipam *IPAM) assertNodeCanIncreasePool(ctx context.Context, node *v1.Node, enis []*enisdk.Eni) error {
	var lastErr error
	canIncrease := false

	for _, eni := range enis {
		oneErr := ipam.assertEniCanIncreasePool(ctx, node, eni)
		if oneErr == nil {
			canIncrease = true
			break
		}
		lastErr = oneErr
	}
	if canIncrease {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("node doesn't have eni")
}

func (ipam *IPAM) assertEniCanIncreasePool(ctx context.Context, node *v1.Node, eni *enisdk.Eni) error {
	// 1. check if subnet still has available ip
	subnetID := eni.SubnetId
	subnetInfo, err := ipam.cloud.DescribeSubnet(ctx, subnetID)
	if err == nil && subnetInfo.AvailableIp <= 0 {
		log.Warningf(ctx, "assertEniCanIncreasePool: subnet %s has no available ip", subnetID)
		return fmt.Errorf("subnet %s has no available ip", subnetID)
	}
	if err != nil {
		log.Warningf(ctx, "assertEniCanIncreasePool: failed to describe subnet %s: %s", subnetID, err)
	}

	// 2. check if node cannot attach more ip due to memory
	nodeFromKube, nodeErr := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(node.Name)
	if nodeErr != nil {
		log.Warningf(ctx, "assertEniCanIncreasePool: failed to get node %s: %s", node.Name, nodeErr)
		return nil
	}

	maxIPPerENI, annoErr := utileni.GetMaxIPPerENIFromNodeAnnotations(nodeFromKube)
	if annoErr != nil {
		log.Warningf(ctx, "assertEniCanIncreasePool: failed to get MaxIPPerENI of node %s: %s", node.Name, annoErr)
		return nil
	}

	resp, eniErr := ipam.cloud.StatENI(ctx, eni.EniId)
	if eniErr != nil {
		log.Errorf(ctx, "assertEniCanIncreasePool: failed to get stat eni %v: %v", eni.EniId, eniErr)
		return nil
	}

	if len(resp.PrivateIpSet) >= maxIPPerENI {
		log.Warningf(ctx, "assertEniCanIncreasePool: eni %s cannot add more ip due to memory", eni.EniId)
		return fmt.Errorf("eni %s cannot add more ip due to memory", eni.EniId)
	}
	return nil
}

func assertDeadline(deadline time.Time) error {
	if time.Now().After(deadline) {
		return fmt.Errorf(errorMsgAllocateTimeout)
	}
	return nil
}
