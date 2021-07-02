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
	goerrors "errors"
	"fmt"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/juju/ratelimit"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	datastorev1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	// minPrivateIPLifeTime is the life time of a private ip (from allocation to release), aim to trade off db slave delay
	minPrivateIPLifeTime = 5 * time.Second

	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5
)

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	var ipResult = ""
	var addIPErrors []error
	var ipAddedENI *enisdk.Eni
	pod, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	// find out which enis are suitable to bind
	enis, err := ipam.findSuitableENIs(ctx, pod)
	if err != nil {
		log.Errorf(ctx, "failed to find a suitable eni for pod (%v %v): %v", namespace, name, err)
		return nil, err
	}
	suitableENINum := len(enis)

	wep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(namespace).Get(name)
	if err == nil {
		ipToAllocate := wep.Spec.IP
		if !isFixIPStatefulSetPod(pod) {
			log.Warningf(ctx, "pod (%v %v) still has wep, but is not a fix-ip sts pod", namespace, name)
			ipToAllocate = ""
		}
		if ipToAllocate != "" {
			log.Infof(ctx, "try to reuse fix IP %v for pod (%v %v)", ipToAllocate, namespace, name)
		}
		for _, eni := range enis {
			ipResult, err = ipam.tryAllocateIPForFixIPPod(ctx, eni, wep, ipToAllocate, ipamgeneric.CniTimeout/time.Duration(suitableENINum))
			if err == nil {
				ipAddedENI = eni
				break
			} else {
				addErr := fmt.Errorf("error eni: %v, %v", eni.EniId, err.Error())
				addIPErrors = append(addIPErrors, addErr)
			}
		}

		if ipAddedENI == nil {
			return nil, fmt.Errorf("all %d enis binded cannot add IP %v: %v", len(enis), wep.Spec.IP, utilerrors.NewAggregate(addIPErrors))
		}

		if wep.Labels == nil {
			wep.Labels = make(map[string]string)
		}
		wep.Spec.ContainerID = containerID
		wep.Spec.IP = ipResult
		wep.Spec.ENIID = ipAddedENI.EniId
		wep.Spec.Mac = ipAddedENI.MacAddress
		wep.Spec.Node = pod.Spec.NodeName
		wep.Spec.SubnetID = ipAddedENI.SubnetId
		wep.Spec.UpdateAt = metav1.Time{ipam.clock.Now()}
		wep.Labels[ipamgeneric.WepLabelSubnetIDKey] = ipAddedENI.SubnetId
		wep.Labels[ipamgeneric.WepLabelInstanceTypeKey] = string(metadata.InstanceTypeExBCC)
		if k8sutil.IsStatefulSetPod(pod) {
			wep.Labels[ipamgeneric.WepLabelStsOwnerKey] = util.GetStsName(wep)
		}
		if pod.Annotations != nil {
			wep.Spec.EnableFixIP = pod.Annotations[StsPodAnnotationEnableFixIP]
			wep.Spec.FixIPDeletePolicy = pod.Annotations[StsPodAnnotationFixIPDeletePolicy]
		}
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(wep)
		if err != nil {
			log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", namespace, name, err)
			time.Sleep(minPrivateIPLifeTime)
			if delErr := ipam.deletePrivateIP(ctx, ipResult, ipAddedENI.EniId); delErr != nil {
				log.Errorf(ctx, "rollback: error deleting private IP %v for pod (%v %v): %v", ipResult, namespace, name, delErr)
			}
			return nil, err
		}
		ipam.lock.Lock()
		ipam.allocated[ipResult] = wep
		ipam.lock.Unlock()
		return wep, nil
	} else {
		if !errors.IsNotFound(err) {
			log.Errorf(ctx, "failed to get wep of pod (%v %v): %v", pod.Namespace, pod.Name, err)
			return nil, err
		}
	}

	log.Infof(ctx, "try to allocate IP and create wep for pod (%v %v)", pod.Namespace, pod.Name)

	for _, eni := range enis {
		ipResult, err = ipam.tryAllocateIP(ctx, eni, pod, ipamgeneric.CniTimeout/time.Duration(suitableENINum))
		if err == nil {
			ipAddedENI = eni
			break
		} else {
			addErr := fmt.Errorf("error ENI: %v, %v", eni.EniId, err.Error())
			addIPErrors = append(addIPErrors, addErr)
		}
	}

	if ipAddedENI == nil {
		return nil, fmt.Errorf("all %d enis binded cannot add IP: %v", len(enis), utilerrors.NewAggregate(addIPErrors))
	}

	wep = &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels: map[string]string{
				ipamgeneric.WepLabelSubnetIDKey:     ipAddedENI.SubnetId,
				ipamgeneric.WepLabelInstanceTypeKey: string(metadata.InstanceTypeExBCC),
			},
			Finalizers: []string{ipamgeneric.WepFinalizer},
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ContainerID: containerID,
			IP:          ipResult,
			Type:        ipamgeneric.WepTypePod,
			Mac:         ipAddedENI.MacAddress,
			ENIID:       ipAddedENI.EniId,
			Node:        pod.Spec.NodeName,
			SubnetID:    ipAddedENI.SubnetId,
			UpdateAt:    metav1.Time{ipam.clock.Now()},
		},
	}

	if k8sutil.IsStatefulSetPod(pod) {
		wep.Spec.Type = ipamgeneric.WepTypeSts
		wep.Labels[ipamgeneric.WepLabelStsOwnerKey] = util.GetStsName(wep)
	}

	if pod.Annotations != nil {
		wep.Spec.EnableFixIP = pod.Annotations[StsPodAnnotationEnableFixIP]
		wep.Spec.FixIPDeletePolicy = pod.Annotations[StsPodAnnotationFixIPDeletePolicy]
	}

	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Create(wep)
	if err != nil {
		log.Errorf(ctx, "failed to create wep for pod (%v %v): %v", namespace, name, err)
		if delErr := ipam.deletePrivateIP(ctx, ipResult, ipAddedENI.EniId); delErr != nil {
			log.Errorf(ctx, "rollback: error deleting private IP %v for pod (%v %v): %v", ipResult, namespace, name, delErr)
		}
		ipam.lock.Lock()
		ipam.privateIPNumCache[ipAddedENI.EniId]--
		ipam.lock.Unlock()
		return nil, err
	}
	log.Infof(ctx, "create wep with spec %+v for pod (%v %v) successfully", wep.Spec, namespace, name)

	// update allocated pod cache
	ipam.lock.Lock()
	if _, ok := ipam.addIPBackoffCache[wep.Spec.ENIID]; ok {
		delete(ipam.addIPBackoffCache, wep.Spec.ENIID)
		log.Infof(ctx, "remove backoff for eni %v when handling pod (%v %v) due to successful ip allocate", wep.Spec.ENIID, namespace, name)
	}
	ipam.allocated[ipResult] = wep
	ipam.lock.Unlock()

	return wep, nil
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Release] releasing IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	wep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof(ctx, "wep of pod (%v %v) not found", namespace, name)
			return nil, nil
		}
		log.Errorf(ctx, "failed to get wep of pod (%v %v): %v", namespace, name, err)
		return nil, err
	}

	// this may be due to a pod migrate to another node
	if wep.Spec.ContainerID != containerID {
		log.Warningf(ctx, "pod (%v %v) may have switched to another node, ignore old cleanup", name, namespace)
		return nil, nil
	}

	if isFixIPStatefulSetPodWep(wep) {
		log.Infof(ctx, "release: sts pod (%v %v) will update wep but private IP won't release", namespace, name)
		wep.Spec.UpdateAt = metav1.Time{ipam.clock.Now()}
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(wep)
		if err != nil {
			log.Errorf(ctx, "failed to update sts pod (%v %v) status: %v", namespace, name, err)
		}
		log.Infof(ctx, "release: update wep for sts pod (%v %v) successfully", namespace, name)
		return wep, nil
	}

	// not sts pod, delete eni ip, delete fip crd
	log.Infof(ctx, "try to release private IP and wep for non-sts pod (%v %v)", namespace, name)
	err = ipam.deletePrivateIP(ctx, wep.Spec.IP, wep.Spec.ENIID)
	if err != nil {
		log.Errorf(ctx, "release: error deleting private IP %v for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
	} else {
		// ip was really on eni and deleted successfully, remove eni backoff and update privateIPNumCache
		ipam.lock.Lock()
		if _, ok := ipam.addIPBackoffCache[wep.Spec.ENIID]; ok {
			delete(ipam.addIPBackoffCache, wep.Spec.ENIID)
			log.Infof(ctx, "remove backoff for eni %v when handling pod (%v %v) due to successful ip release", wep.Spec.ENIID, namespace, name)
		}
		ipam.privateIPNumCache[wep.Spec.ENIID]--
		ipam.lock.Unlock()
	}
	if err != nil && !ipam.isErrorENIPrivateIPNotFound(err, wep) {
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
		return nil, err
	}
	log.Infof(ctx, "release private IP %v for pod (%v %v) successfully", wep.Spec.IP, namespace, name)

	// remove finalizers
	wep.Finalizers = nil
	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(wep)
	if err != nil {
		log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", namespace, name, err)
		return nil, err
	}
	// delete wep
	err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Delete(name, metav1.NewDeleteOptions(0))
	if err != nil {
		log.Errorf(ctx, "failed to delete wep for pod (%v %v): %v", namespace, name, err)
		return nil, err
	}
	log.Infof(ctx, "release wep for pod (%v %v) successfully", namespace, name)

	ipam.lock.Lock()
	delete(ipam.allocated, wep.Spec.IP)
	ipam.lock.Unlock()

	return wep, nil
}

func (ipam *IPAM) Ready(ctx context.Context) bool {
	return ipam.cacheHasSynced
}

func NewIPAM(
	kubeClient kubernetes.Interface,
	crdClient versioned.Interface,
	bceClient cloud.Interface,
	cniMode types.ContainerNetworkMode,
	vpcID string,
	clusterID string,
	subnetSelectionPolicy SubnetSelectionPolicy,
	ipMutatingRate float64,
	ipMutatingBurst int64,
	informerResyncPeriod time.Duration,
	eniSyncPeriod time.Duration,
	gcPeriod time.Duration,
) (ipamgeneric.Interface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, informerResyncPeriod)
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, informerResyncPeriod)

	ipam := &IPAM{
		eventBroadcaster:      eventBroadcaster,
		eventRecorder:         recorder,
		kubeInformer:          kubeInformer,
		kubeClient:            kubeClient,
		crdInformer:           crdInformer,
		crdClient:             crdClient,
		cloud:                 bceClient,
		clock:                 clock.RealClock{},
		cniMode:               cniMode,
		vpcID:                 vpcID,
		clusterID:             clusterID,
		subnetSelectionPolicy: subnetSelectionPolicy,
		eniSyncPeriod:         eniSyncPeriod,
		informerResyncPeriod:  informerResyncPeriod,
		gcPeriod:              gcPeriod,
		eniCache:              make(map[string][]*enisdk.Eni),
		privateIPNumCache:     make(map[string]int),
		addIPBackoffCache:     make(map[string]*wait.Backoff),
		allocated:             make(map[string]*v1alpha1.WorkloadEndpoint),
		datastore:             datastorev1.NewDataStore(),
		bucket:                ratelimit.NewBucketWithRate(ipMutatingRate, ipMutatingBurst),
		cacheHasSynced:        false,
	}
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
	) {
		log.Warning(ctx, "failed WaitForCacheSync, timeout")
		return nil
	} else {
		log.Info(ctx, "WaitForCacheSync done")
	}

	err := ipam.buildAllocatedCache(ctx)
	if err != nil {
		return err
	}

	// k8sr resource and ip cache are synced
	ipam.cacheHasSynced = true

	go func() {
		if err := ipam.syncENI(stopCh); err != nil {
			log.Errorf(ctx, "failed to sync eni info: %v", err)
		}
	}()

	go func() {
		if err := ipam.gc(stopCh); err != nil {
			log.Errorf(ctx, "failed to start ipam gc: %v", err)
		}
	}()

	go ipam.syncSubnets(stopCh)

	<-stopCh
	return nil
}

func (ipam *IPAM) tryAllocateIPForFixIPPod(ctx context.Context, eni *enisdk.Eni, wep *v1alpha1.WorkloadEndpoint, ipToAllocate string, backoffCap time.Duration) (string, error) {
	var namespace, name string = wep.Namespace, wep.Name
	var ipResult string
	var err error

	// Note: here DeletePrivateIP and AddPrivateIP should be atomic. we leverage a lock to do this
	// ensure private ip not attached to other eni
	log.Infof(ctx, "try to delete IP %v from %v", wep.Spec.IP, wep.Spec.ENIID)
	ipam.lock.Lock()
	if err := ipam.deletePrivateIP(ctx, wep.Spec.IP, wep.Spec.ENIID); err != nil && !cloud.IsErrorENIPrivateIPNotFound(err) {
		log.Errorf(ctx, "error delete private IP %v for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
	}
	allocIPMaxTry := 3
	for i := 0; i < allocIPMaxTry; i++ {
		log.Infof(ctx, "try to add IP %v to %v", ipToAllocate, eni.EniId)
		ipResult, err = ipam.addPrivateIP(ctx, ipToAllocate, eni.EniId)
		if err != nil {
			log.Errorf(ctx, "error add private IP %v for pod (%v %v): %v", ipToAllocate, namespace, name, err)
			if cloud.IsErrorSubnetHasNoMoreIP(err) {
				if e := ipam.declareSubnetHasNoMoreIP(ctx, eni.SubnetId, true); e != nil {
					log.Errorf(ctx, "failed to patch subnet %v that has no more ip: %v", eni.SubnetId, e)
				}
			}
			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			if cloud.IsErrorPrivateIPInUse(err) {
				log.Warningf(ctx, "fix ip %v has been mistakenly allocated to somewhere else for pod (%v %v)", ipToAllocate, namespace, name)
				ipToAllocate = ""
				continue
			}
			if isErrorNeedExponentialBackoff(err) {
				if _, ok := ipam.addIPBackoffCache[eni.EniId]; !ok {
					ipam.addIPBackoffCache[eni.EniId] = util.NewBackoffWithCap(backoffCap)
					log.Infof(ctx, "add backoff with cap %v for eni %v when handling pod (%v %v) due to error: %v", backoffCap, eni.EniId, namespace, name, err)
				}
			}
			ipam.lock.Unlock()
			return "", err
		} else if err == nil {
			ipam.lock.Unlock()
			break
		}
	}

	log.Infof(ctx, "add private IP %v for pod (%v %v) successfully", ipResult, namespace, name)
	ipam.lock.Lock()
	ipam.privateIPNumCache[eni.EniId]++
	ipam.lock.Unlock()

	return ipResult, nil
}

func (ipam *IPAM) tryAllocateIP(ctx context.Context, eni *enisdk.Eni, pod *v1.Pod, backoffCap time.Duration) (string, error) {
	var ipResult string
	var err error
	var allocIPMaxTry int = 3
	var namespace, name string = pod.Namespace, pod.Name

	for i := 0; i < allocIPMaxTry; i++ {
		ipResult, err = ipam.addPrivateIPWithExponentialBackoff(ctx, ipResult, eni, pod)
		if err != nil {
			log.Errorf(ctx, "failed to add private IP to %v for pod (%v %v): %v", eni.EniId, namespace, name, err)
			if cloud.IsErrorSubnetHasNoMoreIP(err) {
				if e := ipam.declareSubnetHasNoMoreIP(ctx, eni.SubnetId, true); e != nil {
					log.Errorf(ctx, "failed to patch subnet %v that has no more ip: %v", eni.SubnetId, e)
				}
			}
			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			if isErrorNeedExponentialBackoff(err) {
				ipam.lock.Lock()
				if _, ok := ipam.addIPBackoffCache[eni.EniId]; !ok {
					ipam.addIPBackoffCache[eni.EniId] = util.NewBackoffWithCap(backoffCap)
					log.Infof(ctx, "add backoff with cap %v for eni %v when handling pod (%v %v) due to error: %v", backoffCap, eni.EniId, namespace, name, err)
				}
				ipam.lock.Unlock()
			}
			return "", err
		}
		ipam.lock.RLock()
		wep, ok := ipam.allocated[ipResult]
		ipam.lock.RUnlock()
		if !ok {
			break
		}
		log.Warningf(ctx, "IP %s has been allocated to pod (%v %v), will try next...", ipResult, wep.Namespace, wep.Name)
		ipResult = ""
	}

	if ipResult == "" {
		msg := fmt.Sprintf("failed to add private IP for pod (%v %v) after retrying %d times", namespace, name, allocIPMaxTry)
		log.Error(ctx, msg)
		return "", goerrors.New(msg)
	}

	log.Infof(ctx, "assign private IP %v for pod (%v %v) successfully", ipResult, namespace, name)
	ipam.lock.Lock()
	ipam.privateIPNumCache[eni.EniId]++
	ipam.lock.Unlock()

	return ipResult, nil
}

func (ipam *IPAM) buildAllocatedCache(ctx context.Context) error {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

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
		ipam.allocated[wep.Spec.IP] = nwep
		log.Infof(ctx, "build allocated pod cache: found IP %v assigned to pod (%v %v)", wep.Spec.IP, wep.Namespace, wep.Name)
	}
	return nil
}

func (ipam *IPAM) syncENI(stopCh <-chan struct{}) error {
	err := wait.PollImmediateUntil(ipam.eniSyncPeriod, func() (bool, error) {
		ctx := log.NewContext()

		// list vpc enis
		enis, err := ipam.cloud.ListENIs(ctx, ipam.vpcID)
		if err != nil {
			log.Errorf(ctx, "failed to list enis when syncing eni info: %v", err)
			return false, nil
		}

		bccSelector, err := bccNodeListerSelector()
		if err != nil {
			log.Errorf(ctx, "error parsing requirement: %v", err)
			return false, nil
		}
		// list nodes whose instance type is BCC
		nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(bccSelector)
		if err != nil {
			log.Errorf(ctx, "failed to list nodes when syncing eni info: %v", err)
			return false, nil
		}

		// build eni cache of bcc nodes
		err = ipam.buildENICache(ctx, nodes, enis)
		if err != nil {
			log.Errorf(ctx, "failed to build eni cache: %v", err)
			return false, nil
		}

		// update ippool status of bcc nodes
		for _, node := range nodes {
			err = ipam.updateIPPoolStatus(ctx, node, enis)
			if err != nil {
				log.Errorf(ctx, "failed to update ippool status for node %v: %v", node.Name, err)
			}
		}

		// check each node, determine whether to add new eni
		err = ipam.increaseENIIfRequired(ctx, nodes)
		if err != nil {
			log.Errorf(ctx, "failed to increase eni: %v", err)
		}

		return false, nil
	}, stopCh)
	if err != nil {
		return err
	}

	return nil
}

func (ipam *IPAM) buildENICache(ctx context.Context, nodes []*v1.Node, enis []enisdk.Eni) error {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	// build eni cache
	ipam.eniCache = make(map[string][]*enisdk.Eni)
	ipam.privateIPNumCache = make(map[string]int)

	instanceIdToNodeNameMap := buildInstanceIdToNodeNameMap(ctx, nodes)
	// init eni cache
	for _, n := range nodes {
		ipam.eniCache[n.Name] = make([]*enisdk.Eni, 0)
	}

	for idx, eni := range enis {
		if eni.Status != utileni.ENIStatusInuse || !utileni.ENICreatedByCCE(&eni) {
			continue
		}

		if nodeName, ok := instanceIdToNodeNameMap[eni.InstanceId]; ok {
			ipam.eniCache[nodeName] = append(ipam.eniCache[nodeName], &enis[idx])
		}

		// update private ip num of enis
		ipam.privateIPNumCache[eni.EniId] = len(eni.PrivateIpSet)
	}

	return nil
}

func (ipam *IPAM) updateIPPoolStatus(ctx context.Context, node *v1.Node, enis []enisdk.Eni) error {
	eniStatus := map[string]v1alpha1.ENI{}
	instanceID, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		return err
	}
	ippoolName := utilippool.GetNodeIPPoolName(node.Name)
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

		_, updateErr := ipam.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(result)
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

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	err := wait.PollImmediateUntil(ipam.gcPeriod, func() (bool, error) {
		ctx := log.NewContext()

		stsList, err := ipam.kubeInformer.Apps().V1().StatefulSets().Lister().List(labels.Everything())
		if err != nil {
			log.Errorf(ctx, "gc: error list sts in cluster: %v", err)
			return false, nil
		}

		wepSelector, err := wepListerSelector()
		if err != nil {
			log.Errorf(ctx, "error parsing requirement: %v", err)
			return false, nil
		}

		wepList, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().List(wepSelector)
		if err != nil {
			log.Errorf(ctx, "gc: error list wep in cluster: %v", err)
			return false, nil
		}

		// release wep if sts is deleted
		err = ipam.gcDeletedSts(ctx, wepList)
		if err != nil {
			return false, nil
		}

		// release wep if sts scale down
		err = ipam.gcScaledDownSts(ctx, stsList)
		if err != nil {
			return false, nil
		}

		// release non-sts wep if pod not found
		err = ipam.gcLeakedPod(ctx, wepList)
		if err != nil {
			return false, nil
		}

		return false, nil
	}, stopCh)

	if err != nil {
		return err
	}
	return nil
}

func (ipam *IPAM) gcDeletedSts(ctx context.Context, wepList []*v1alpha1.WorkloadEndpoint) error {
	for _, wep := range wepList {
		// only delete ip if sts requires fix ip
		if !isFixIPStatefulSetPodWep(wep) {
			continue
		}
		// don't delete ip if policy is Never
		if wep.Spec.FixIPDeletePolicy == FixIPDeletePolicyNever {
			continue
		}
		stsName := util.GetStsName(wep)
		_, err := ipam.kubeInformer.Apps().V1().StatefulSets().Lister().StatefulSets(wep.Namespace).Get(stsName)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof(ctx, "gc: sts (%v %v) has been deleted, will release private IP and clean up orphaned wep ", wep.Namespace, stsName)
				err := ipam.deletePrivateIP(context.Background(), wep.Spec.IP, wep.Spec.ENIID)
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete private IP %v on %v for orphaned pod (%v %v): %v", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name, err)
				} else {
					log.Infof(ctx, "gc: delete private IP %v on %v for orphaned pod (%v %v) successfully", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name)
				}
				if err != nil && !(ipam.isErrorENIPrivateIPNotFound(err, wep) || cloud.IsErrorENINotFound(err)) {
					log.Errorf(ctx, "gc: stop delete wep for orphaned pod (%v %v), try next round", wep.Namespace, wep.Name)
					// we cannot continue to delete wep, otherwise this IP will not gc in the next round, thus leaked
					continue
				}

				// remove finalizers
				wep.Finalizers = nil
				_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(wep)
				if err != nil {
					log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
					continue
				}
				// delete wep
				if err := ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Delete(wep.Name, metav1.NewDeleteOptions(0)); err != nil {
					log.Errorf(ctx, "gc: failed to delete wep for orphaned pod (%v %v): %v", wep.Namespace, wep.Name, err)
				} else {
					log.Infof(ctx, "gc: delete wep for orphaned pod (%v %v) successfully", wep.Namespace, wep.Name)
					ipam.lock.Lock()
					delete(ipam.allocated, wep.Spec.IP)
					ipam.lock.Unlock()
				}

			} else {
				log.Errorf(ctx, "gc: failed to get sts (%v %v): %v", wep.Namespace, stsName, err)
			}
		}
	}

	return nil
}

func (ipam *IPAM) gcScaledDownSts(ctx context.Context, stsList []*appv1.StatefulSet) error {
	for _, sts := range stsList {
		replicas := int(*sts.Spec.Replicas)
		requirement, err := labels.NewRequirement(ipamgeneric.WepLabelStsOwnerKey, selection.Equals, []string{sts.Name})
		if err != nil {
			log.Errorf(ctx, "gc: error parsing requirement: %v", err)
			return err
		}
		selector := labels.NewSelector().Add(*requirement)

		// find wep whose owner is sts
		weps, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(sts.Namespace).List(selector)
		if err != nil {
			log.Errorf(ctx, "gc: failed to list wep with selector: %v: %v", selector.String(), err)
			continue
		}
		if replicas < len(weps) {
			podTemplateAnnotations := sts.Spec.Template.ObjectMeta.Annotations
			if podTemplateAnnotations == nil || podTemplateAnnotations[StsPodAnnotationFixIPDeletePolicy] != FixIPDeletePolicyNever {
				log.Infof(ctx, "gc: sts (%v %v) has scaled down from %v to %v, will release private IP and clean up orphaned wep", sts.Namespace, sts.Name, len(weps), replicas)
			}
			for _, wep := range weps {
				// only delete ip if sts requires fix ip
				if !isFixIPStatefulSetPodWep(wep) {
					continue
				}
				// don't delete ip if policy is Never
				if wep.Spec.FixIPDeletePolicy == FixIPDeletePolicyNever {
					continue
				}
				index := util.GetStsPodIndex(wep)
				if index < 0 || index < replicas {
					continue
				}
				stsPodName := fmt.Sprintf("%s-%d", sts.Name, index)
				log.Infof(ctx, "gc: try to release orphaned wep (%v %v)", sts.Namespace, stsPodName)
				wep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(sts.Namespace).Get(stsPodName)
				if err != nil {
					log.Errorf(ctx, "gc: failed to get wep (%v %v): %v", sts.Namespace, stsPodName, err)
					continue
				}
				err = ipam.deletePrivateIP(context.Background(), wep.Spec.IP, wep.Spec.ENIID)
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete private IP %v on %v for orphaned pod (%v %v): %v", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name, err)
				} else {
					log.Infof(ctx, "gc: delete private IP %v on %v for orphaned pod (%v %v) successfully", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name)
				}
				if err != nil && !(ipam.isErrorENIPrivateIPNotFound(err, wep) || cloud.IsErrorENINotFound(err)) {
					log.Errorf(ctx, "gc: stop delete wep for orphaned pod (%v %v), try next round", wep.Namespace, wep.Name)
					// we cannot continue to delete wep, otherwise this IP will not gc in the next round, thus leaked
					continue
				}
				// remove finalizers
				wep.Finalizers = nil
				_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(wep)
				if err != nil {
					log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
					continue
				}
				// delete wep
				err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(sts.Namespace).Delete(stsPodName, metav1.NewDeleteOptions(0))
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete wep for orphaned pod (%v %v): %v", wep.Namespace, wep.Name, err)
				} else {
					log.Infof(ctx, "gc: delete wep for orphaned pod (%v %v) successfully", wep.Namespace, wep.Name)
					ipam.lock.Lock()
					delete(ipam.allocated, wep.Spec.IP)
					ipam.lock.Unlock()
				}
			}
		}
	}

	return nil
}

func (ipam *IPAM) gcLeakedPod(ctx context.Context, wepList []*v1alpha1.WorkloadEndpoint) error {
	for _, wep := range wepList {
		if wep.Spec.Type != ipamgeneric.WepTypePod {
			continue
		}
		_, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(wep.Namespace).Get(wep.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("gc: try to release leaked wep (%v %v)", wep.Namespace, wep.Name)
				log.Info(ctx, msg)
				ipam.eventRecorder.Event(&v1.ObjectReference{
					Kind: "wep",
					Name: fmt.Sprintf("%v %v", wep.Namespace, wep.Name),
				}, v1.EventTypeWarning, "PodLeaked", msg)

				err = ipam.deletePrivateIP(context.Background(), wep.Spec.IP, wep.Spec.ENIID)
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete private IP %v on %v for leaked pod (%v %v): %v", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name, err)
				} else {
					log.Infof(ctx, "gc: delete private IP %v on %v for leaked pod (%v %v) successfully", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name)
				}
				if err != nil && !(ipam.isErrorENIPrivateIPNotFound(err, wep) || cloud.IsErrorENINotFound(err)) {
					log.Errorf(ctx, "gc: stop delete wep for leaked pod (%v %v), try next round", wep.Namespace, wep.Name)
					// we cannot continue to delete wep, otherwise this IP will not gc in the next round, thus leaked
					continue
				}

				// remove finalizers
				wep.Finalizers = nil
				_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(wep)
				if err != nil {
					log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
					continue
				}
				// delete wep
				err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Delete(wep.Name, metav1.NewDeleteOptions(0))
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete wep for leaked pod (%v %v): %v", wep.Namespace, wep.Name, err)
				} else {
					ipam.lock.Lock()
					delete(ipam.allocated, wep.Spec.IP)
					ipam.lock.Unlock()
					msg := fmt.Sprintf("gc: delete wep for leaked pod (%v %v) successfully", wep.Namespace, wep.Name)
					log.Info(ctx, msg)
					ipam.eventRecorder.Event(&v1.ObjectReference{
						Kind: "wep",
						Name: fmt.Sprintf("%v %v", wep.Namespace, wep.Name),
					}, v1.EventTypeWarning, "PodLeaked", msg)
				}
			} else {
				log.Errorf(ctx, "gc: failed to get pod (%v %v): %v", wep.Namespace, wep.Name, err)
			}
		}
	}

	return nil
}

func (ipam *IPAM) addPrivateIP(ctx context.Context, privateIP string, eniID string) (string, error) {
	ipam.bucket.Wait(1)
	return ipam.cloud.AddPrivateIP(ctx, privateIP, eniID)
}

func (ipam *IPAM) addPrivateIPWithExponentialBackoff(ctx context.Context, privateIP string, eni *enisdk.Eni, pod *v1.Pod) (string, error) {
	var backoffWaitPeriod time.Duration
	var backoff *wait.Backoff
	var ok bool
	const waitPeriodNum = 10

	ipam.lock.Lock()
	backoff = ipam.addIPBackoffCache[eni.EniId]
	if backoff != nil && backoff.Steps >= 0 {
		backoffWaitPeriod = backoff.Step()
	}
	ipam.lock.Unlock()

	if backoffWaitPeriod != 0 {
		log.Infof(ctx, "backoff: wait %v to allocate private ip on %v", backoffWaitPeriod, eni.EniId)

		// instead of sleep a large amount of time, we divide sleep time into small parts to check backoff cache.
		for i := 0; i < waitPeriodNum; i++ {
			time.Sleep(backoffWaitPeriod / waitPeriodNum)
			ipam.lock.RLock()
			backoff, ok = ipam.addIPBackoffCache[eni.EniId]
			ipam.lock.RUnlock()
			if !ok {
				log.Warningf(ctx, "found backoff on eni %v removed", eni.EniId)
				break
			}
		}
	}

	// if have reached backoff cap, first check then add ip
	if backoff != nil && backoffWaitPeriod >= backoff.Cap {
		// 1. check if subnet still has available ip
		subnetID := eni.SubnetId
		subnet, err := ipam.cloud.DescribeSubnet(ctx, subnetID)
		if err == nil && subnet.AvailableIp <= 0 {
			msg := fmt.Sprintf("backoff short-circuit: subnet %v has no available ip", subnetID)
			log.Warning(ctx, msg)
			return "", goerrors.New(msg)
		}
		if err != nil {
			log.Errorf(ctx, "failed to describe subnet %v: %v", subnetID, err)
		}

		// 2. check if node cannot attach more ip due to memory
		node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
		if err == nil {
			maxIPPerENI, err := utileni.GetMaxIPPerENIFromNodeAnnotations(node)
			if err == nil {
				resp, err := ipam.cloud.StatENI(ctx, eni.EniId)
				if err == nil && len(resp.PrivateIpSet) >= maxIPPerENI {
					msg := fmt.Sprintf("backoff short-circuit: eni %v cannot add more ip due to memory", eni.EniId)
					log.Warning(ctx, msg)
					return "", goerrors.New(msg)
				}

				if err != nil {
					log.Errorf(ctx, "failed to get stat eni %v: %v", eni.EniId, err)
				}
			}
		}
	}

	return ipam.addPrivateIP(ctx, privateIP, eni.EniId)
}

func (ipam *IPAM) deletePrivateIP(ctx context.Context, privateIP string, eniID string) error {
	ipam.bucket.Wait(1)
	return ipam.cloud.DeletePrivateIP(ctx, privateIP, eniID)
}

func (ipam *IPAM) isErrorENIPrivateIPNotFound(err error, wep *v1alpha1.WorkloadEndpoint) bool {
	return cloud.IsErrorENIPrivateIPNotFound(err) && ipam.clock.Since(wep.Spec.UpdateAt.Time) >= minPrivateIPLifeTime
}

func isErrorNeedExponentialBackoff(err error) bool {
	return cloud.IsErrorVmMemoryCanNotAttachMoreIpException(err) || cloud.IsErrorSubnetHasNoMoreIP(err)
}

func isFixIPStatefulSetPodWep(wep *v1alpha1.WorkloadEndpoint) bool {
	return wep.Spec.Type == ipamgeneric.WepTypeSts && wep.Spec.EnableFixIP == EnableFixIPTrue
}

func isFixIPStatefulSetPod(pod *v1.Pod) bool {
	if pod.Annotations == nil || !k8sutil.IsStatefulSetPod(pod) {
		return false
	}
	return pod.Annotations[StsPodAnnotationEnableFixIP] == EnableFixIPTrue
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
	requirement, err := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BCC", "GPU"})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}
