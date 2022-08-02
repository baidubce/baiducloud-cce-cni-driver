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
	"net"
	"sort"
	"sync"
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
	"k8s.io/apimachinery/pkg/util/sets"
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
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
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

	// get node
	node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
	if err != nil {
		return nil, err
	}

	node = node.DeepCopy()

	// check datastore node status
	err = wait.ExponentialBackoff(retry.DefaultRetry, func() (done bool, err error) {
		if !ipam.datastore.NodeExistsInStore(node.Name) {
			var (
				evt = &event{
					node: node,
					ctx:  ctx,
				}
				ch = make(chan *event)
			)

			ipam.lock.Lock()
			_, ok := ipam.buildDataStoreEventChan[evt.node.Name]
			if !ok {
				ipam.buildDataStoreEventChan[node.Name] = ch
				go ipam.handleRebuildNodeDatastoreEvent(ctx, evt.node, ch)
			}
			ch = ipam.buildDataStoreEventChan[evt.node.Name]
			ipam.lock.Unlock()

			ch <- evt
			return false, nil
		} else {
			return true, nil
		}
	})
	if err == wait.ErrWaitTimeout {
		return nil, fmt.Errorf("init node %v datastore error", node.Name)
	}

	// get node stats from store, to further check if pool is corrupted
	total, used, err := ipam.datastore.GetNodeStats(node.Name)
	if err != nil {
		msg := fmt.Sprintf("get node %v stats in datastore failed: %v", node, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	log.Infof(ctx, "total, used before allocate for node %v: %v %v", node.Name, total, used)

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
			if ipToAllocate == "" {
				ipResult, err = ipam.datastore.AllocatePodPrivateIPByENI(node.Name, eni.EniId)
				if err == nil {
					ipAddedENI = eni
					break
				}
			}

			ipResult, err = ipam.tryAllocateIPForFixIPPod(ctx, eni, wep, ipToAllocate, node, ipamgeneric.CCECniTimeout/time.Duration(suitableENINum))
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
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(ctx, wep, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", namespace, name, err)
			time.Sleep(minPrivateIPLifeTime)
			if delErr := ipam.deletePrivateIP(ctx, ipResult, ipAddedENI.EniId); delErr != nil {
				log.Errorf(ctx, "rollback: error deleting private IP %v for pod (%v %v): %v", ipResult, namespace, name, delErr)
			}
			if delErr := ipam.datastore.ReleasePodPrivateIP(node.Name, ipAddedENI.EniId, ipResult); delErr != nil {
				log.Errorf(ctx, "rollback: error releasing private IP %v from datastore for pod (%v %v): %v", ipResult, namespace, name, delErr)
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

	idleIPs, _ := ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	log.Infof(ctx, "idle ip in datastore before allocate for node %v: %v", node.Name, idleIPs)

	// datastore has no available ip
	if !ipam.canAllocateIP(ctx, node.Name, enis) {
		err = wait.ExponentialBackoff(retry.DefaultRetry, func() (done bool, err error) {
			if ipam.canAllocateIP(ctx, node.Name, enis) {
				return true, nil
			}
			var (
				evt = &event{
					node:    node,
					enis:    enis,
					passive: true,
					ctx:     ctx,
				}
				ch = make(chan *event)
			)

			ipam.lock.Lock()
			_, ok := ipam.increasePoolEventChan[evt.node.Name]
			if !ok {
				ipam.increasePoolEventChan[node.Name] = ch
				go ipam.handleIncreasePoolEvent(ctx, evt.node, ch)
			}
			ch = ipam.increasePoolEventChan[evt.node.Name]
			ipam.lock.Unlock()

			ch <- evt
			return false, nil
		})
	}
	if err == wait.ErrWaitTimeout {
		return nil, fmt.Errorf("allocate ip for node %v error", node.Name)
	}

	ipResult, ipAddedENI, err = ipam.tryAllocateIPByENIs(ctx, node.Name, enis)
	if err != nil {
		msg := fmt.Sprintf("error allocate private IP for pod (%v %v): %v", namespace, name, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
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

	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Create(ctx, wep, metav1.CreateOptions{})
	if err != nil {
		log.Errorf(ctx, "failed to create wep for pod (%v %v): %v", namespace, name, err)
		if delErr := ipam.datastore.ReleasePodPrivateIP(node.Name, ipAddedENI.EniId, ipResult); delErr != nil {
			log.Errorf(ctx, "rollback: error deleting private IP %v for pod (%v %v): %v", ipResult, namespace, name, delErr)
		}
		return nil, err
	}
	log.Infof(ctx, "create wep with spec %+v for pod (%v %v) successfully", wep.Spec, namespace, name)

	// update allocated pod cache
	ipam.lock.Lock()
	if ipam.removeAddIPBackoffCache(wep.Spec.ENIID, true) {
		log.Infof(ctx, "remove backoff for eni %v when handling pod (%v %v) due to successful ip allocate", wep.Spec.ENIID, namespace, name)
	}
	ipam.allocated[ipResult] = wep
	ipam.lock.Unlock()

	total, used, err = ipam.datastore.GetNodeStats(node.Name)
	if err == nil {
		log.Infof(ctx, "total, used after allocate for node %v: %v %v", node.Name, total, used)
	}
	idleIPs, err = ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	if err == nil {
		log.Infof(ctx, "idle ip in datastore after allocate for node %v: %v", node.Name, idleIPs)
	}

	return wep, nil
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Release] releasing IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	// new a wep, avoid data racing
	tmpWep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof(ctx, "wep of pod (%v %v) not found", namespace, name)
			return nil, nil
		}
		log.Errorf(ctx, "failed to get wep of pod (%v %v): %v", namespace, name, err)
		return nil, err
	}

	wep := tmpWep.DeepCopy()

	// this may be due to a pod migrate to another node
	if wep.Spec.ContainerID != containerID {
		log.Warningf(ctx, "pod (%v %v) may have switched to another node, ignore old cleanup", name, namespace)
		return nil, nil
	}

	if isFixIPStatefulSetPodWep(wep) {
		log.Infof(ctx, "release: sts pod (%v %v) will update wep but private IP won't release", namespace, name)
		wep.Spec.UpdateAt = metav1.Time{ipam.clock.Now()}
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(ctx, wep, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to update sts pod (%v %v) status: %v", namespace, name, err)
		}
		log.Infof(ctx, "release: update wep for sts pod (%v %v) successfully", namespace, name)
		return wep, nil
	}

	idle := ipam.idleIPNum(wep.Spec.Node)
	if idle < ipam.idleIPPoolMaxSize {
		// mark ip as unassigned in datastore, then delete wep
		log.Infof(ctx, "try to only release wep for pod (%v %v) due to idle ip (%v) less than max idle pool", namespace, name, idle)
		err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
		if err != nil {
			log.Errorf(ctx, "release: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
		} else {
			goto delWep
		}
	}

	// not sts pod, delete eni ip, delete fip crd
	log.Infof(ctx, "try to release private IP %v and wep for non-sts pod (%v %v)", wep.Spec.IP, namespace, name)
	err = ipam.deletePrivateIP(ctx, wep.Spec.IP, wep.Spec.ENIID)
	if err != nil {
		log.Errorf(ctx, "release: error deleting private IP %v for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
	} else {
		ipam.lock.Lock()
		// ip was really on eni and deleted successfully, remove eni backoff and update privateIPNumCache
		if ipam.removeAddIPBackoffCache(wep.Spec.ENIID, true) {
			log.Infof(ctx, "remove backoff for eni %v when handling pod (%v %v) due to successful ip release", wep.Spec.ENIID, namespace, name)
		}
		ipam.decPrivateIPNumCache(wep.Spec.ENIID, true)
		ipam.lock.Unlock()

		metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, wep.Spec.SubnetID).Dec()
	}
	if err != nil && !(ipam.isErrorENIPrivateIPNotFound(err, wep) || cloud.IsErrorENINotFound(err)) {
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
		return nil, err
	}

	log.Infof(ctx, "release private IP %v for pod (%v %v) successfully", wep.Spec.IP, namespace, name)

	// update datastore
	err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
	if err != nil {
		log.Errorf(ctx, "release: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
	}

	err = ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
	if err != nil {
		log.Errorf(ctx, "release: error deleting private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
	}

delWep:
	// ipam may receive allocate request before release request.
	// For sts pod, wep name will not change.
	// double check to ensure we don't mistakenly delete wep.
	if wep.Spec.ContainerID == containerID {
		// remove finalizers
		wep.Finalizers = nil
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(ctx, wep, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", namespace, name, err)
			return nil, err
		}
		// delete wep
		err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Delete(ctx, name, *metav1.NewDeleteOptions(0))
		if err != nil {
			log.Errorf(ctx, "failed to delete wep for pod (%v %v): %v", namespace, name, err)
			return nil, err
		}
		log.Infof(ctx, "release wep for pod (%v %v) successfully", namespace, name)
	}

	ipam.removeIPFromCache(wep.Spec.IP, false)
	ipam.removeIPFromLeakedCache(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)

	total, used, err := ipam.datastore.GetNodeStats(wep.Spec.Node)
	if err == nil {
		log.Infof(ctx, "total, used after release for node %v: %v %v", wep.Spec.Node, total, used)
	}
	idleIPs, err := ipam.datastore.GetUnassignedPrivateIPByNode(wep.Spec.Node)
	if err == nil {
		log.Infof(ctx, "idle ip in datastore after release for node %v: %v", wep.Spec.Node, idleIPs)
	}

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
	idleIPPoolMinSize int,
	idleIPPoolMaxSize int,
	batchAddIPNum int,
	informerResyncPeriod time.Duration,
	eniSyncPeriod time.Duration,
	gcPeriod time.Duration,
	debug bool,
) (ipamgeneric.Interface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, informerResyncPeriod)
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, informerResyncPeriod)

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
		informerResyncPeriod:    informerResyncPeriod,
		gcPeriod:                gcPeriod,
		eniCache:                make(map[string][]*enisdk.Eni),
		privateIPNumCache:       make(map[string]int),
		possibleLeakedIPCache:   make(map[eniAndIPAddrKey]time.Time),
		addIPBackoffCache:       make(map[string]*wait.Backoff),
		allocated:               make(map[string]*v1alpha1.WorkloadEndpoint),
		datastore:               datastorev1.NewDataStore(),
		bucket:                  ratelimit.NewBucketWithRate(ipMutatingRate, ipMutatingBurst),
		idleIPPoolMinSize:       idleIPPoolMinSize,
		idleIPPoolMaxSize:       idleIPPoolMaxSize,
		batchAddIPNum:           batchAddIPNum,
		buildDataStoreEventChan: make(map[string]chan *event),
		increasePoolEventChan:   make(map[string]chan *event),
		cacheHasSynced:          false,
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

	// rebuild allocated cache
	err := ipam.buildAllocatedCache(ctx)
	if err != nil {
		return err
	}

	// rebuild datastore cache
	ipam.buildAllocatedNodeCache(ctx)

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

	go ipam.checkIdleIPPoolPeriodically()

	go ipam.syncSubnets(stopCh)

	<-stopCh
	return nil
}

func (ipam *IPAM) tryAllocateIPForFixIPPod(ctx context.Context, eni *enisdk.Eni, wep *v1alpha1.WorkloadEndpoint, ipToAllocate string, node *v1.Node, backoffCap time.Duration) (string, error) {
	var namespace, name string = wep.Namespace, wep.Name
	var ipResult []string
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

	if err := ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP); err != nil {
		log.Errorf(ctx, "error releasing deprecated private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
	}

	if err := ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP); err != nil {
		log.Errorf(ctx, "error deleting deprecated private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)

	}

	allocIPMaxTry := 3
	for i := 0; i < allocIPMaxTry; i++ {
		log.Infof(ctx, "try to add IP %v to %v", ipToAllocate, eni.EniId)
		ipResult, err = ipam.batchAddPrivateIP(ctx, []string{ipToAllocate}, 0, eni.EniId)
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

	if len(ipResult) < 1 {
		msg := "unexpected result from eni openapi: at least one ip should be added"
		log.Error(ctx, msg)
		return "", goerrors.New(msg)
	}

	log.Infof(ctx, "add private IP %v for pod (%v %v) successfully", ipResult, namespace, name)

	for _, ip := range ipResult {
		ipam.incPrivateIPNumCache(eni.EniId, false)
		ipam.datastore.AddPrivateIPToStore(node.Name, eni.EniId, ip, true)
	}

	metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, eni.SubnetId).Inc()

	return ipResult[0], nil
}

func (ipam *IPAM) tryAllocateIP(
	ctx context.Context,
	eni *enisdk.Eni,
	node *v1.Node,
	batchAddNum int,
	backoffCap time.Duration,
) ([]string, error) {
	var ipResult []string
	var err error

	for batchAddNum > 0 {
		ipResult, err = ipam.batchAddPrivateIPWithExponentialBackoff(ctx, ipResult, batchAddNum, eni, node)
		if err == nil {
			log.Infof(ctx, "batch add %v private ip(s) for %v on eni %s successfully, %v", batchAddNum, node.Name, eni.EniId, ipResult)
			ipam.removeAddIPBackoffCache(eni.EniId, false)
			break
		}

		if err != nil {
			log.Warningf(ctx, "warn: batch add %v private ip(s) for node %v failed: %v", batchAddNum, node.Name, err)

			if cloud.IsErrorSubnetHasNoMoreIP(err) {
				if e := ipam.declareSubnetHasNoMoreIP(ctx, eni.SubnetId, true); e != nil {
					log.Errorf(ctx, "failed to patch subnet %v that has no more ip: %v", eni.SubnetId, e)
				}
			}
			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}

			if batchAddNum == 1 && cloud.IsErrorSubnetHasNoMoreIP(err) {
				ipamgeneric.DeclareSubnetHasNoMoreIP(ctx, ipam.crdClient, ipam.crdInformer, eni.SubnetId, true)
			}

			if isErrorNeedExponentialBackoff(err) {
				if batchAddNum == 1 {
					ipam.lock.Lock()
					if _, ok := ipam.addIPBackoffCache[eni.EniId]; !ok {
						ipam.addIPBackoffCache[eni.EniId] = util.NewBackoffWithCap(backoffCap)
						log.Infof(ctx, "add backoff with cap %v for eni %v  due to error: %v", backoffCap, eni.EniId, err)
					}
					ipam.lock.Unlock()
				}

				// decrease batchAddNum then retry
				batchAddNum = batchAddNum >> 1
				continue
			}

			msg := fmt.Sprintf("error batch add %v private ip(s): %v", batchAddNum, err)
			log.Error(ctx, msg)
			return nil, goerrors.New(msg)
		}
	}

	if len(ipResult) == 0 {
		msg := fmt.Sprintf("cannot batch add more ip to eni on node %v instance %v", eni.EniId, node)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	for _ = range ipResult {
		ipam.incPrivateIPNumCache(eni.EniId, false)
		metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, eni.SubnetId).Inc()
	}

	return ipResult, nil
}

func (ipam *IPAM) tryAllocateIPByENIs(ctx context.Context, node string, enis []*enisdk.Eni) (string, *enisdk.Eni, error) {
	var (
		eniList []string
	)

	ipam.sortENIByDatastoreStats(node, enis, false)

	// iterate each subnet, try to allocate
	for _, eni := range enis {
		ipResult, err := ipam.datastore.AllocatePodPrivateIPByENI(node, eni.EniId)
		if err == nil {
			// allocate one successfully
			return ipResult, eni, nil
		} else {
			log.Warningf(ctx, "datastore try allocate ip for node %v in eni %v failed: %v", node, eni.EniId, err)
		}

		eniList = append(eniList, eni.EniId)
	}

	return "", nil, fmt.Errorf("no available ip address in datastore for node %v in enis: %v", node, eniList)
}

func (ipam *IPAM) canAllocateIP(ctx context.Context, nodeName string, enis []*enisdk.Eni) bool {
	var (
		total int = 0
		used  int = 0
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

func (ipam *IPAM) handleIncreasePoolEvent(ctx context.Context, node *v1.Node, ch chan *event) error {
	log.Infof(ctx, "start increase pool goroutine for node %v", node.Name)
	for {
		select {
		case e, ok := <-ch:

			if !ok {
				ctx := log.NewContext()
				log.Infof(ctx, "closed channel for node %v, exit...", node.Name)
				return nil
			}

			if e.passive && ipam.canAllocateIP(ctx, e.node.Name, e.enis) {
				continue
			}

			if !e.passive && ipam.idleIPNum(e.node.Name) >= ipam.idleIPPoolMinSize {
				continue
			}

			var (
				enis           = e.enis
				node           = e.node
				ctx            = e.ctx
				ipAddedENI     *enisdk.Eni
				ipResult       []string
				suitableENINum = len(enis)
				err            error
				addIPErrors    []error
			)

			ipam.sortENIByDatastoreStats(node.Name, enis, true)

			for _, eni := range enis {
				ipResult, err = ipam.tryAllocateIP(ctx, eni, node, ipam.batchAddIPNum, ipamgeneric.CCECniTimeout/time.Duration(suitableENINum))
				if err == nil {
					ipAddedENI = eni
					break
				} else {
					addErr := fmt.Errorf("error ENI: %v, %v", eni.EniId, err.Error())
					addIPErrors = append(addIPErrors, addErr)
				}
			}

			if ipAddedENI == nil {
				msg := fmt.Sprintf("all %d enis bound to node %v cannot add IP: %v", len(enis), node.Name, utilerrors.NewAggregate(addIPErrors))
				log.Error(ctx, msg)
			}

			for _, ip := range ipResult {
				err := ipam.datastore.AddPrivateIPToStore(e.node.Name, ipAddedENI.EniId, ip, false)
				if err != nil {
					msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip, err)
					log.Error(ctx, msg)
				}
			}
		} // end select
	}

	return nil
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
		for _, ip := range eni.PrivateIpSet {
			if !ip.Primary {
				// ipam will sync all weps to build allocated cache when it starts
				assigned := false
				if _, ok := ipam.allocated[ip.PrivateIpAddress]; ok {
					assigned = true
				}
				err := ipam.datastore.AddPrivateIPToStore(node.Name, eni.EniId, ip.PrivateIpAddress, assigned)
				if err != nil {
					msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip.PrivateIpAddress, err)
					log.Error(ctx, msg)
				} else {
					log.Infof(ctx, "add private ip %v(assigned: %v) from eni %v to node %v datastore successfully", ip.PrivateIpAddress, assigned, eni.EniId, node.Name)
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

func (ipam *IPAM) removeIPFromCache(ipAddr string, lockless bool) {
	if !lockless {
		ipam.lock.Lock()
		defer ipam.lock.Unlock()
	}
	delete(ipam.allocated, ipAddr)
}

func (ipam *IPAM) removeIPFromLeakedCache(node, eniID, ipAddr string) {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	delete(ipam.possibleLeakedIPCache, eniAndIPAddrKey{node, eniID, ipAddr})
}

func (ipam *IPAM) removeAddIPBackoffCache(eniID string, lockless bool) bool {
	if !lockless {
		ipam.lock.Lock()
		defer ipam.lock.Unlock()
	}
	_, ok := ipam.addIPBackoffCache[eniID]
	if ok {
		delete(ipam.addIPBackoffCache, eniID)
	}
	return ok
}

func (ipam *IPAM) syncENI(stopCh <-chan struct{}) error {
	eniSyncInterval := wait.Jitter(ipam.eniSyncPeriod, 0.2)

	err := wait.PollImmediateUntil(eniSyncInterval, func() (bool, error) {
		ctx := log.NewContext()

		// list vpc enis
		listArgs := enisdk.ListEniArgs{
			VpcId: ipam.vpcID,
			Name:  fmt.Sprintf("%s/", ipam.clusterID),
		}
		enis, err := ipam.cloud.ListENIs(ctx, listArgs)
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
		err = ipam.buildInuseENICache(ctx, nodes, enis)
		if err != nil {
			log.Errorf(ctx, "failed to build eni cache: %v", err)
			return false, nil
		}

		metric.MultiEniMultiIPEniCount.Reset()
		metric.MultiEniMultiIPEniIPCount.Reset()

		// update ippool status and metrics of bcc nodes
		ipam.updateENIMetrics(enis)

		for _, node := range nodes {
			instanceID, err := util.GetInstanceIDFromNode(node)
			if err != nil {
				log.Errorf(ctx, "failed to get instance id of node %v, skip updating ippool", node.Name)
				continue
			}

			if !ipam.datastore.NodeExistsInStore(node.Name) {
				ipam.datastore.AddNodeToStore(node.Name, instanceID)
			}

			err = ipam.updateIPPoolStatus(ctx, node, instanceID, enis)
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

func (ipam *IPAM) buildInuseENICache(ctx context.Context, nodes []*v1.Node, enis []enisdk.Eni) error {
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
		if eni.Status != utileni.ENIStatusInuse {
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

func (ipam *IPAM) updateENIMetrics(enis []enisdk.Eni) {
	for _, eni := range enis {
		if utileni.ENIOwnedByCluster(&eni, metric.MetaInfo.ClusterID) {
			metric.MultiEniMultiIPEniCount.WithLabelValues(
				metric.MetaInfo.ClusterID,
				metric.MetaInfo.VPCID,
				eni.SubnetId,
				eni.Status,
			).Inc()
		}

		if eni.Status == utileni.ENIStatusInuse && utileni.ENIOwnedByCluster(&eni, metric.MetaInfo.ClusterID) {
			metric.MultiEniMultiIPEniIPCount.WithLabelValues(
				metric.MetaInfo.ClusterID,
				eni.VpcId,
				eni.SubnetId,
			).Add((float64(len(eni.PrivateIpSet) - 1)))
		}
	}
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

				ipam.lock.Lock()
				enis := ipam.eniCache[node.Name]
				ipam.lock.Unlock()

				var (
					evt = &event{
						node:    node,
						enis:    enis,
						passive: false,
						ctx:     ctx,
					}
					ch = make(chan *event)
				)

				ipam.lock.Lock()
				_, ok := ipam.increasePoolEventChan[evt.node.Name]
				if !ok {
					ipam.increasePoolEventChan[node.Name] = ch
					go ipam.handleIncreasePoolEvent(ctx, evt.node, ch)
				}
				ch = ipam.increasePoolEventChan[evt.node.Name]
				ipam.lock.Unlock()

				ch <- evt
			}

		}(node)
	}

	wg.Wait()

	return false, nil
}

func (ipam *IPAM) checkIdleIPPoolPeriodically() error {
	return wait.PollImmediateInfinite(30*time.Second, ipam.checkIdleIPPool)
}

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	err := wait.PollImmediateUntil(wait.Jitter(ipam.gcPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()

		stsList, err := ipam.kubeInformer.Apps().V1().StatefulSets().Lister().List(labels.Everything())
		if err != nil {
			log.Errorf(ctx, "gc: error list sts in cluster: %v", err)
			return false, nil
		}

		wepSelector, err := wepListerSelector()
		if err != nil {
			log.Errorf(ctx, "gc: error parsing requirement: %v", err)
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

		err = ipam.gcLeakedIPPool(ctx)
		if err != nil {
			return false, nil
		}

		err = ipam.gcLeakedIP(ctx)
		if err != nil {
			return false, nil
		}

		err = ipam.gcDeletedNode(ctx)
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

				// delete ip from datastore
				err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
				if err != nil {
					log.Errorf(ctx, "gc: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
				}
				err = ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
				if err != nil {
					log.Errorf(ctx, "gc: error deleting private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
				}

				ipam.removeIPFromCache(wep.Spec.IP, false)
				ipam.removeIPFromLeakedCache(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)

				// remove finalizers
				wep.Finalizers = nil
				_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(ctx, wep, metav1.UpdateOptions{})
				if err != nil {
					log.Errorf(ctx, "gc: failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
					continue
				}
				// delete wep
				if err := ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Delete(ctx, wep.Name, *metav1.NewDeleteOptions(0)); err != nil {
					log.Errorf(ctx, "gc: failed to delete wep for orphaned pod (%v %v): %v", wep.Namespace, wep.Name, err)
				} else {
					log.Infof(ctx, "gc: delete wep for orphaned pod (%v %v) successfully", wep.Namespace, wep.Name)
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

				// delete ip from datastore
				err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
				if err != nil {
					log.Errorf(ctx, "gc: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
				}
				err = ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
				if err != nil {
					log.Errorf(ctx, "gc: error deleting private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
				}

				ipam.removeIPFromCache(wep.Spec.IP, false)
				ipam.removeIPFromLeakedCache(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)

				// remove finalizers
				wep.Finalizers = nil
				_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(ctx, wep, metav1.UpdateOptions{})
				if err != nil {
					log.Errorf(ctx, "gc: failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
					continue
				}
				// delete wep
				err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(sts.Namespace).Delete(ctx, stsPodName, *metav1.NewDeleteOptions(0))
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete wep for orphaned pod (%v %v): %v", wep.Namespace, wep.Name, err)
				} else {
					log.Infof(ctx, "gc: delete wep for orphaned pod (%v %v) successfully", wep.Namespace, wep.Name)
				}
			}
		}
	}

	return nil
}

func (ipam *IPAM) gcLeakedPod(ctx context.Context, wepList []*v1alpha1.WorkloadEndpoint) error {
	for _, wep := range wepList {
		// only gc non-fix ip pod
		if isFixIPStatefulSetPodWep(wep) {
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

				idle := ipam.idleIPNum(wep.Spec.Node)
				if idle < ipam.idleIPPoolMaxSize {
					// mark ip as unassigned in datastore, then delete wep
					log.Infof(ctx, "gc: try to only release wep for pod (%v %v) due to idle ip (%v) less than max idle pool", wep.Namespace, wep.Name, idle)
					err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
					if err != nil {
						log.Errorf(ctx, "gc: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
					} else {
						goto delWep
					}
				}

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
				// delete ip from datastore
				err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
				if err != nil {
					log.Errorf(ctx, "gc: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
				}
				err = ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
				if err != nil {
					log.Errorf(ctx, "gc: error deleting private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
				}

			delWep:
				ipam.removeIPFromCache(wep.Spec.IP, false)
				ipam.removeIPFromLeakedCache(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
				// remove finalizers
				wep.Finalizers = nil
				_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(ctx, wep, metav1.UpdateOptions{})
				if err != nil {
					log.Errorf(ctx, "gc: failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
					continue
				}
				// delete wep
				err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Delete(ctx, wep.Name, *metav1.NewDeleteOptions(0))
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete wep for leaked pod (%v %v): %v", wep.Namespace, wep.Name, err)
				} else {
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

func (ipam *IPAM) gcLeakedIPPool(ctx context.Context) error {
	pools, err := ipam.crdInformer.Cce().V1alpha1().IPPools().Lister().List(labels.Everything())
	if err != nil {
		log.Errorf(ctx, "gc: failed to list ippools: %v", err)
		return nil
	}

	for _, p := range pools {
		nodeName := utilippool.GetNodeNameFromIPPoolName(p.Name)
		_, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(nodeName)
		if err != nil && errors.IsNotFound(err) {
			// We only delete ippool created by cni.
			// To be compatible with prior version, nodeName in ip format is also considered as leaked one.
			if p.Spec.CreationSource == ipamgeneric.IPPoolCreationSourceCNI || net.ParseIP(nodeName) != nil {
				e := ipam.crdClient.CceV1alpha1().IPPools(p.Namespace).Delete(ctx, p.Name, *metav1.NewDeleteOptions(0))
				if e != nil && !errors.IsNotFound(e) {
					log.Errorf(ctx, "gc: failed to delete ippool %v: %v", p.Name, e)
				} else {
					log.Infof(ctx, "gc: delete leaked ippool %v successfully", p.Name)
				}
			}
		}
	}

	return nil
}

func (ipam *IPAM) gcLeakedIP(ctx context.Context) error {
	// list all pods
	pods, err := ipam.kubeInformer.Core().V1().Pods().Lister().List(labels.Everything())
	if err != nil {
		log.Errorf(ctx, "gc: error list pods in cluster: %v", err)
		return err
	}

	// list all weps whose owner is sts
	requirement, _ := labels.NewRequirement(ipamgeneric.WepLabelStsOwnerKey, selection.Exists, nil)
	selector := labels.NewSelector().Add(*requirement)
	stsWeps, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(v1.NamespaceAll).List(selector)
	if err != nil {
		log.Errorf(ctx, "gc: failed to list wep with selector: %v: %v", selector.String(), err)
		return err
	}

	var (
		podIPSet    = sets.NewString()
		stsPodIPSet = sets.NewString()
	)

	// store pod ip temporarily
	for _, pod := range pods {
		if !pod.Spec.HostNetwork && !k8sutil.IsPodFinished(pod) {
			if pod.Status.PodIP != "" {
				podIPSet.Insert(pod.Status.PodIP)
			} else {
				wep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(pod.Namespace).Get(pod.Name)
				if err != nil {
					podPhase := string(pod.Status.Phase)
					if pod.DeletionTimestamp != nil && pod.Status.Reason == "NodeLost" {
						podPhase = string(v1.PodUnknown)
					} else if pod.DeletionTimestamp != nil {
						podPhase = "Terminating"
					}
					log.Warningf(ctx, "gc: failed to get wep (%v/%v) with pod phase \"%v\": %v", pod.Namespace, pod.Name, podPhase, err)
					continue
				}
				podIPSet.Insert(wep.Spec.IP)
				log.Warningf(ctx, "gc: pod %v may be pending in the pod phase of pulling image, and the IP %v is not leaked", pod.Name, wep.Spec.IP)
			}
		}
	}

	// store sts pod ip temporarily
	for _, wep := range stsWeps {
		stsPodIPSet.Insert(wep.Spec.IP)
	}

	// build leaked ip cache
	ipam.buildPossibleLeakedIPCache(ctx, podIPSet, stsPodIPSet)

	// prune leaked ip
	ipam.pruneExpiredLeakedIP(ctx)

	return nil
}

func (ipam *IPAM) buildPossibleLeakedIPCache(ctx context.Context, podIPSet, stsPodIPSet sets.String) {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	for nodeName, enis := range ipam.eniCache {
		idleIPs, err := ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
		if err != nil {
			log.Errorf(ctx, "gc: failed to get idle ips in datastore of node %v: %v", nodeName, err)
		}
		idleIPSet := sets.NewString(idleIPs...)

		for _, eni := range enis {
			for _, ip := range eni.PrivateIpSet {
				if !ip.Primary {
					key := eniAndIPAddrKey{nodeName, eni.EniId, ip.PrivateIpAddress}
					// ip not in pod and neither in sts wep, nor in idle pool
					if !podIPSet.Has(ip.PrivateIpAddress) && !stsPodIPSet.Has(ip.PrivateIpAddress) && !idleIPSet.Has(ip.PrivateIpAddress) {
						if _, ok := ipam.possibleLeakedIPCache[key]; !ok {
							ipam.possibleLeakedIPCache[key] = ipam.clock.Now()
							log.Warningf(ctx, "gc: eni %v on node %v may has IP %v leaked", eni.EniId, nodeName, ip.PrivateIpAddress)
						}
					} else {
						// if ip in pod, maybe a false positive
						if _, ok := ipam.possibleLeakedIPCache[key]; ok {
							delete(ipam.possibleLeakedIPCache, key)
							log.Warningf(ctx, "gc: remove IP %v on eni %v from possibleLeakedIPCache", ip.PrivateIpAddress, eni.EniId)
						}
					}
				}
			}
		}
	}
}

func (ipam *IPAM) pruneExpiredLeakedIP(ctx context.Context) {
	var (
		leakedIPs []eniAndIPAddrKey
	)

	// prepare leaked ips for cleanup
	ipam.lock.Lock()
	for key, activeTime := range ipam.possibleLeakedIPCache {
		if ipam.clock.Since(activeTime) > ipamgeneric.LeakedPrivateIPExpiredTimeout {
			log.Infof(ctx, "gc: found leaked ip on eni %v: %v", key.eniID, key.ipAddr)
			leakedIPs = append(leakedIPs, eniAndIPAddrKey{key.nodeName, key.eniID, key.ipAddr})
			delete(ipam.possibleLeakedIPCache, key)
		}
	}
	ipam.lock.Unlock()

	// let's cleanup leaked ips
	for _, tuple := range leakedIPs {
		err := ipam.deletePrivateIP(ctx, tuple.ipAddr, tuple.eniID)
		if err != nil {
			log.Errorf(ctx, "gc: failed to delete leaked private IP %v on %v: %v", tuple.ipAddr, tuple.eniID, err)
		} else {
			log.Infof(ctx, "gc: delete leaked private IP %v on %v successfully", tuple.ipAddr, tuple.eniID)
		}
		if err != nil && !(cloud.IsErrorENIPrivateIPNotFound(err) || cloud.IsErrorENINotFound(err)) {
			log.Errorf(ctx, "gc: stop delete leaked private IP %v on %v, try next round", tuple.ipAddr, tuple.eniID)
			continue
		}
		// delete ip from datastore
		err = ipam.datastore.ReleasePodPrivateIP(tuple.nodeName, tuple.eniID, tuple.ipAddr)
		if err != nil {
			log.Errorf(ctx, "gc: error releasing private IP %v on node %v from datastore: %v", tuple.ipAddr, tuple.nodeName, err)
		}
		err = ipam.datastore.DeletePrivateIPFromStore(tuple.nodeName, tuple.eniID, tuple.ipAddr)
		if err != nil {
			log.Errorf(ctx, "gc: error deleting private IP %v on node %v from datastore: %v", tuple.ipAddr, tuple.nodeName, err)
		}

		ipam.removeIPFromCache(tuple.ipAddr, false)
	}
}

func (ipam *IPAM) gcDeletedNode(ctx context.Context) error {
	for _, node := range ipam.datastore.ListNodes() {
		_, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(node)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof(ctx, "gc: detect node %v has been deleted, clean up datastore", node)

				// clean up datastore
				delErr := ipam.datastore.DeleteNodeFromStore(node)
				if delErr != nil {
					log.Errorf(ctx, "gc: error delete node %v from datastore: %v", node, delErr)
				}

				ipam.lock.Lock()
				if ch, ok := ipam.increasePoolEventChan[node]; ok {
					delete(ipam.increasePoolEventChan, node)
					close(ch)
					log.Infof(ctx, "gc: clean up increase pool goroutine for node %v", node)
				}
				ipam.lock.Unlock()

				continue
			}
			return err
		}
	}

	return nil
}

func (ipam *IPAM) batchAddPrivateIP(ctx context.Context, privateIPs []string, batchAddNum int, eniID string) ([]string, error) {
	ipam.bucket.Wait(1)
	return ipam.cloud.BatchAddPrivateIP(ctx, privateIPs, batchAddNum, eniID)
}

func (ipam *IPAM) batchAddPrivateIPWithExponentialBackoff(
	ctx context.Context,
	privateIPs []string,
	batchAddNum int,
	eni *enisdk.Eni,
	node *v1.Node,
) ([]string, error) {
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
			return nil, goerrors.New(msg)
		}
		if err != nil {
			log.Errorf(ctx, "failed to describe subnet %v: %v", subnetID, err)
		}

		// 2. check if node cannot attach more ip due to memory
		node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(node.Name)
		if err == nil {
			maxIPPerENI, err := utileni.GetMaxIPPerENIFromNodeAnnotations(node)
			if err == nil {
				resp, err := ipam.cloud.StatENI(ctx, eni.EniId)
				if err == nil && len(resp.PrivateIpSet) >= maxIPPerENI {
					msg := fmt.Sprintf("backoff short-circuit: eni %v cannot add more ip due to memory", eni.EniId)
					log.Warning(ctx, msg)
					return nil, goerrors.New(msg)
				}

				if err != nil {
					log.Errorf(ctx, "failed to get stat eni %v: %v", eni.EniId, err)
				}
			}
		}
	}

	return ipam.batchAddPrivateIP(ctx, privateIPs, batchAddNum, eni.EniId)
}

func (ipam *IPAM) deletePrivateIP(ctx context.Context, privateIP string, eniID string) error {
	ipam.bucket.Wait(1)
	return ipam.cloud.DeletePrivateIP(ctx, privateIP, eniID)
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
	requirement, err := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BCC", "GPU", "DCC"})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}
