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

package bbc

import (
	"context"
	goerrors "errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/im7mortal/kmutex"
	"github.com/juju/ratelimit"
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

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	sbncontroller "github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet"
	datastorev2 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
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
	minPrivateIPLifeTime       = 3 * time.Second
	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5
)

func NewIPAM(
	kubeClient kubernetes.Interface,
	crdClient versioned.Interface,
	kubeInformer informers.SharedInformerFactory,
	crdInformer crdinformers.SharedInformerFactory,
	bceClient cloud.Interface,
	cniMode types.ContainerNetworkMode,
	vpcID string,
	clusterID string,
	gcPeriod time.Duration,
	batchAddIPNum int,
	ipMutatingRate float64,
	ipMutatingBurst int64,
	idleIPPoolMinSize int,
	idleIPPoolMaxSize int,
	debug bool,
) (ipam.Interface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

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
		datastore:             datastorev2.NewDataStore(),
		addIPBackoffCache:     make(map[string]*wait.Backoff),
		allocated:             make(map[string]*v1alpha1.WorkloadEndpoint),
		bucket:                ratelimit.NewBucketWithRate(ipMutatingRate, ipMutatingBurst),
		nodeENIMap:            make(map[string]string),
		possibleLeakedIPCache: make(map[privateIPAddrKey]time.Time),
		batchAddIPNum:         batchAddIPNum,
		idleIPPoolMinSize:     idleIPPoolMinSize,
		idleIPPoolMaxSize:     idleIPPoolMaxSize,
		cacheHasSynced:        false,
		gcPeriod:              gcPeriod,
		nodeLock:              kmutex.New(),
		debug:                 debug,
	}
	return ipam, nil
}

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	// get pod
	pod, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	// get node
	node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
	if err != nil {
		return nil, err
	}

	// get ippool
	ippool, err := ipam.crdInformer.Cce().V1alpha1().IPPools().Lister().IPPools(v1.NamespaceDefault).Get(utilippool.GetNodeIPPoolName(node.Name))
	if err != nil {
		return nil, err
	}

	// get instance id from node
	instanceID, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		return nil, err
	}

	var candidateSubnets []string

	// check pod annotation, judge whether pod needs specific subnets
	if PodNeedsSpecificSubnets(pod) {
		candidateSubnets = GetPodSpecificSubnetsFromAnnotation(pod)
		log.Infof(ctx, "pod (%v %v) requires specific subnets: %v", namespace, name, pod.Annotations[PodAnnotationSpecificSubnets])
	} else {
		// normal pod use predefined subnets
		candidateSubnets = ippool.Spec.PodSubnets
	}

	log.Infof(ctx, "pod (%v %v) will get ip from candidate subnets: %v", namespace, name, candidateSubnets)

	// try best to create subnet cr
	if err := ipam.createSubnetCRs(ctx, candidateSubnets); err != nil {
		log.Warningf(ctx, "warn: create subnet crs failed: %v", err)
	}

	// check if we need to rebuild cache
	if !ipam.datastore.NodeExistsInStore(node.Name) {
		t := time.Now()
		ipam.nodeLock.Lock(node.Name)
		if ipam.debug {
			metric.RPCPerPodLockLatency.WithLabelValues(
				metric.MetaInfo.ClusterID, namespace, name, containerID, "rebuildNodeDataStoreCache").Set(metric.MsSince(t))
		}
		if !ipam.datastore.NodeExistsInStore(node.Name) {
			log.Infof(ctx, "rebuild cache for node %v starts", node.Name)
			err = ipam.rebuildNodeDataStoreCache(ctx, node, instanceID)
			if err != nil {
				ipam.nodeLock.Unlock(node.Name)
				log.Errorf(ctx, "rebuild cache for node %v error: %v", node.Name, err)
				return nil, err
			}
			log.Infof(ctx, "rebuild cache for node %v ends", node.Name)
		}
		ipam.nodeLock.Unlock(node.Name)
	}

	// get node stats from store, to further check if pool is corrupted
	total, used, err := ipam.datastore.GetNodeStats(node.Name)
	if err != nil {
		msg := fmt.Sprintf("get node %v stats in datastore failed: %v", node, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	log.Infof(ctx, "total, used before allocate for node %v: %v %v", node.Name, total, used)

	idleIPs, _ := ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	log.Infof(ctx, "idle ip in datastore before allocate for node %v: %v", node.Name, idleIPs)

	// if pool status is corrupted, clean and wait until rebuilding
	// we are unlikely to hit this
	if ipam.poolCorrupted(total, used) {
		log.Errorf(ctx, "corrupted pool status detected on node %v. total, used: %v, %v", node.Name, total, used)
		ipam.datastore.DeleteNodeFromStore(node.Name)
		return nil, fmt.Errorf("ipam cached pool is rebuilding")
	}

	// if pool cannot allocate ip, then batch add ips
	if !ipam.poolCanAllocateIP(ctx, node.Name, candidateSubnets) {
		t := time.Now()
		ipam.nodeLock.Lock(node.Name)
		if ipam.debug {
			metric.RPCPerPodLockLatency.WithLabelValues(
				metric.MetaInfo.ClusterID, namespace, name, containerID, "increasePool").Set(metric.MsSince(t))
		}
		if !ipam.poolCanAllocateIP(ctx, node.Name, candidateSubnets) {
			start := time.Now()
			err = ipam.increasePool(ctx, node.Name, instanceID, candidateSubnets, ipam.batchAddIPNum)
			if err != nil {
				msg := fmt.Sprintf("increase pool for node %v failed: %v", node.Name, err)
				log.Error(ctx, msg)
				ipam.eventRecorder.Event(node, v1.EventTypeWarning, "IncreasePoolFailed", msg)
			}
			log.Infof(ctx, "increase pool for node %v takes %v to finish", node.Name, time.Since(start))
		}
		ipam.nodeLock.Unlock(node.Name)
	}

	// try to allocate ip by subnets for both cases: pod that requires specific subnets or not
	ipResult, ipSubnet, err := ipam.tryAllocateIPBySubnets(ctx, node.Name, candidateSubnets)
	if err != nil {
		msg := fmt.Sprintf("error allocate private IP for pod (%v %v): %v", namespace, name, err)
		ipam.eventRecorder.Event(pod, v1.EventTypeWarning, "AllocateIPFailed", msg)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}
	log.Infof(ctx, "allocate private IP for pod (%v %v) with %v successfully", namespace, name, ipResult)

	wep := &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels: map[string]string{
				ipamgeneric.WepLabelInstanceTypeKey: string(metadata.InstanceTypeExBBC),
			},
			Finalizers: []string{ipamgeneric.WepFinalizer},
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ContainerID: containerID,
			IP:          ipResult,
			SubnetID:    ipSubnet,
			Node:        pod.Spec.NodeName,
			InstanceID:  instanceID,
			UpdateAt:    metav1.Time{Time: time.Unix(0, 0)},
		},
	}

	// create wep
	t := time.Now()
	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Create(ctx, wep, metav1.CreateOptions{})
	if err != nil {
		log.Errorf(ctx, "failed to create wep for pod (%v %v): %v", namespace, name, err)
		if errors.IsAlreadyExists(err) {
			log.Warningf(ctx, "wep for pod (%v %v) already exists", namespace, name)
			tmpWep, getErr := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(namespace).Get(name)
			if getErr != nil {
				log.Errorf(ctx, "failed to get wep (%v %v): %v", namespace, name)
				return nil, err
			}
			wep.ResourceVersion = tmpWep.ResourceVersion
			_, updateErr := ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(ctx, wep, metav1.UpdateOptions{})
			if updateErr != nil {
				log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", namespace, name, updateErr)
				_ = ipam.datastore.ReleasePodPrivateIP(node.Name, ipSubnet, ipResult)
				return nil, updateErr
			}
		} else {
			if delErr := ipam.datastore.ReleasePodPrivateIP(node.Name, ipSubnet, ipResult); delErr != nil {
				log.Errorf(ctx, "rollback: error releasing private IP %v from datastore for pod (%v %v): %v", ipResult, namespace, name, delErr)
			}
			return nil, err
		}
	}
	log.Infof(ctx, "create wep with spec %+v for pod (%v %v) successfully, elapsed %v", wep.Spec, namespace, name, time.Since(t))

	ipam.lock.Lock()
	if _, ok := ipam.addIPBackoffCache[wep.Spec.InstanceID]; ok {
		delete(ipam.addIPBackoffCache, wep.Spec.InstanceID)
		log.Infof(ctx, "remove backoff for instance %v when handling pod (%v %v) due to successful ip allocate", wep.Spec.InstanceID, namespace, name)
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

func (ipam *IPAM) poolCanAllocateIP(ctx context.Context, nodeName string, candidateSubnets []string) bool {
	canAllocateIP := false

	for _, sbn := range candidateSubnets {
		total, used, err := ipam.datastore.GetSubnetBucketStats(nodeName, sbn)
		if err != nil {
			log.Errorf(ctx, "failed to get bucket stats for subnet %v: %v", sbn, err)
			continue
		}

		// still have available ip to allocate
		if total-used > 0 {
			canAllocateIP = true
			break
		}
	}

	return canAllocateIP
}

func (ipam *IPAM) tryAllocateIPBySubnets(ctx context.Context, node string, candidateSubnets []string) (string, string, error) {
	// subnet cache
	var subnets []subnet

	// build subnet cache from datastore
	for _, sbn := range candidateSubnets {
		total, used, err := ipam.datastore.GetSubnetBucketStats(node, sbn)
		if err != nil {
			log.Errorf(ctx, "failed to get bucket stats for subnet %v: %v", sbn, err)
			continue
		}
		available := total - used
		subnets = append(subnets, subnet{sbn, available})
	}

	// sort subnet cache by available ip count
	sort.Slice(subnets, func(i, j int) bool {
		return subnets[i].availableCnt > subnets[j].availableCnt
	})

	// iterate each subnet, try to allocate
	for _, sbn := range subnets {
		ipResult, ipSubnet, err := ipam.datastore.AllocatePodPrivateIPBySubnet(node, sbn.subnetID)
		if err == nil {
			// allocate one successfully
			return ipResult, ipSubnet, nil
		} else {
			log.Errorf(ctx, "datastore try allocate ip for node %v in subnet %v failed: %v", node, sbn.subnetID, err)
		}
	}

	return "", "", fmt.Errorf("no available ip address in datastore for node %v in subnets: %v", node, candidateSubnets)
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Release] releasing IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	tmpWep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof(ctx, "wep of pod (%v %v) not found", namespace, name)
			return nil, nil
		}
		log.Errorf(ctx, "failed to get wep of pod (%v %v): %v", namespace, name, err)
		return nil, err
	}

	// new a wep, avoid data racing
	wep := tmpWep.DeepCopy()

	// this may be due to a pod migrate to another node
	if wep.Spec.ContainerID != containerID {
		log.Warningf(ctx, "pod (%v %v) may have switched to another node, ignore old cleanup", name, namespace)
		return nil, nil
	}

	idle := ipam.idleIPNum(wep.Spec.Node)
	if idle < ipam.idleIPPoolMaxSize {
		// mark ip as unassigned in datastore, then delete wep
		log.Infof(ctx, "try to only release wep for pod (%v %v) due to idle ip (%v) less than max idle pool", namespace, name, idle)
		err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.SubnetID, wep.Spec.IP)
		if err != nil {
			log.Errorf(ctx, "release: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
		} else {
			goto delWep
		}
	}

	// delete eni ip, delete fip crd
	log.Infof(ctx, "try to release private IP and wep for pod (%v %v)", namespace, name)
	err = ipam.batchDelIP(ctx, &bbc.BatchDelIpArgs{
		InstanceId: wep.Spec.InstanceID,
		PrivateIps: []string{wep.Spec.IP},
	})
	if err != nil {
		log.Errorf(ctx, "release: error deleting private IP %v for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
		if !(cloud.IsErrorBBCENIPrivateIPNotFound(err) && ipam.clock.Since(wep.Spec.UpdateAt.Time) >= minPrivateIPLifeTime) {
			return nil, err
		}
	} else {
		// err == nil, ip was really on eni and deleted successfully, remove backoff
		ipam.lock.Lock()
		if _, ok := ipam.addIPBackoffCache[wep.Spec.InstanceID]; ok {
			delete(ipam.addIPBackoffCache, wep.Spec.InstanceID)
			log.Infof(ctx, "remove backoff for instance %v when handling pod (%v %v) due to successful ip release", wep.Spec.InstanceID, namespace, name)
		}
		ipam.lock.Unlock()
	}
	log.Infof(ctx, "release private IP %v for pod (%v %v) successfully", wep.Spec.IP, namespace, name)

	// update datastore
	err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.SubnetID, wep.Spec.IP)
	if err != nil {
		log.Errorf(ctx, "release: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
	}

	err = ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.SubnetID, wep.Spec.IP)
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
	ipam.removeIPFromLeakedCache(wep.Spec.InstanceID, wep.Spec.SubnetID, wep.Spec.Node, wep.Spec.IP)

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

func (ipam *IPAM) Run(ctx context.Context, stopCh <-chan struct{}) error {
	defer func() {
		runtime.HandleCrash()
	}()

	log.Info(ctx, "Starting cce ipam controller for BBC")
	defer log.Info(ctx, "Shutting down cce ipam controller for BBC")

	log.Infof(ctx, "limit ip mutating rate to %v, burst to %v", ipam.bucket.Rate(), ipam.bucket.Capacity())

	nodeInformer := ipam.kubeInformer.Core().V1().Nodes().Informer()
	podInformer := ipam.kubeInformer.Core().V1().Pods().Informer()
	wepInformer := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()
	subnetInformer := ipam.crdInformer.Cce().V1alpha1().Subnets().Informer()
	ippoolInformer := ipam.crdInformer.Cce().V1alpha1().IPPools().Informer()

	ipam.kubeInformer.Start(stopCh)
	ipam.crdInformer.Start(stopCh)

	if !cache.WaitForNamedCacheSync(
		"cce-ipam",
		stopCh,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		wepInformer.HasSynced,
		subnetInformer.HasSynced,
		ippoolInformer.HasSynced,
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

	ipam.cacheHasSynced = true

	go func() {
		ipam.checkIdleIPPoolPeriodically()
	}()

	go func() {
		if err := ipam.gc(stopCh); err != nil {
			log.Errorf(ctx, "failed to start ipam gc: %v", err)
		}
	}()

	<-stopCh
	return nil
}

func (ipam *IPAM) checkIdleIPPool() (bool, error) {
	var (
		ctx = log.NewContext()
		wg  sync.WaitGroup
		ch  = make(chan struct{}, 10)
	)

	bbcNodeSelector, _ := bbcNodeListerSelector()
	nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(bbcNodeSelector)
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
			instanceID, err := util.GetInstanceIDFromNode(node)
			if err != nil {
				log.Errorf(ctx, "failed to get instance id of node %v: %v", node.Name, err)
				return
			}

			if !ipam.datastore.NodeExistsInStore(node.Name) {
				return
			}

			idle := ipam.idleIPNum(node.Name)
			if idle < ipam.idleIPPoolMinSize {
				// we should try to increase pool here.
				pool, err := ipam.crdInformer.Cce().V1alpha1().IPPools().Lister().IPPools(v1.NamespaceDefault).Get(utilippool.GetNodeIPPoolName(node.Name))
				if err != nil {
					return
				}
				log.Infof(ctx, "try to increase pool of %v ips for node %v due to lack of ips", ipam.idleIPPoolMinSize-idle, node.Name)
				ipam.nodeLock.Lock(node.Name)
				err = ipam.increasePool(ctx, node.Name, instanceID, pool.Spec.PodSubnets, ipam.idleIPPoolMinSize-idle)
				ipam.nodeLock.Unlock(node.Name)
				if err != nil {
					log.Errorf(ctx, "increase pool for node %v failed: %v", node.Name, err)
					return
				}
			}

		}(node)
	}

	wg.Wait()

	return false, nil
}

func (ipam *IPAM) checkIdleIPPoolPeriodically() error {
	return wait.PollImmediateInfinite(5*time.Second, ipam.checkIdleIPPool)
}

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	err := wait.PollImmediateUntil(ipam.gcPeriod, func() (bool, error) {
		ctx := log.NewContext()

		selector, err := wepListerSelector()
		if err != nil {
			log.Errorf(ctx, "gc: error parsing requirement: %v", err)
			return false, nil
		}

		wepList, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().List(selector)
		if err != nil {
			log.Errorf(ctx, "gc: error list wep in cluster: %v", err)
			return false, nil
		}

		// release wep if pod not found
		err = ipam.gcLeakedPod(ctx, wepList)
		if err != nil {
			return false, nil
		}

		// release leaked ip
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

func (ipam *IPAM) gcLeakedPod(ctx context.Context, wepList []*v1alpha1.WorkloadEndpoint) error {
	var (
		wg sync.WaitGroup
		ch = make(chan struct{}, 5)
	)

	for _, wep := range wepList {
		_, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(wep.Namespace).Get(wep.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				ch <- struct{}{}
				wg.Add(1)

				go func(wep *v1alpha1.WorkloadEndpoint) {
					defer func() {
						wg.Done()
						<-ch
					}()

					wep = wep.DeepCopy()

					log.Infof(ctx, "gc: try to release leaked wep (%v %v)", wep.Namespace, wep.Name)

					idle := ipam.idleIPNum(wep.Spec.Node)
					if idle < ipam.idleIPPoolMaxSize {
						// mark ip as unassigned in datastore, then delete wep
						log.Infof(ctx, "gc: try to only release wep for pod (%v %v) due to idle ip (%v) less than max idle pool", wep.Namespace, wep.Name, idle)
						err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.SubnetID, wep.Spec.IP)
						if err != nil {
							log.Errorf(ctx, "gc: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
						} else {
							goto delWep
						}
					}

					// delete ip
					err = ipam.batchDelIP(ctx, &bbc.BatchDelIpArgs{
						InstanceId: wep.Spec.InstanceID,
						PrivateIps: []string{wep.Spec.IP},
					})

					if err != nil {
						log.Errorf(ctx, "gc: failed to delete private IP %v on %v for leaked pod (%v %v): %v", wep.Spec.IP, wep.Spec.InstanceID, wep.Namespace, wep.Name, err)
						if cloud.IsErrorRateLimit(err) {
							time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
						}
						if !(cloud.IsErrorBBCENIPrivateIPNotFound(err) && ipam.clock.Since(wep.Spec.UpdateAt.Time) >= minPrivateIPLifeTime) {
							log.Errorf(ctx, "gc: stop delete wep for leaked pod (%v %v), try next round", wep.Namespace, wep.Name)
							return
						}
					} else {
						log.Infof(ctx, "gc: delete private IP %v on %v for leaked pod (%v %v) successfully", wep.Spec.IP, wep.Spec.InstanceID, wep.Namespace, wep.Name)
					}

					// delete ip from datastore
					err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.SubnetID, wep.Spec.IP)
					if err != nil {
						log.Errorf(ctx, "gc: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
					}
					err = ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.SubnetID, wep.Spec.IP)
					if err != nil {
						log.Errorf(ctx, "gc: error deleting private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
					}

				delWep:
					// remove finalizers
					wep.Finalizers = nil
					_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(ctx, wep, metav1.UpdateOptions{})
					if err != nil {
						log.Errorf(ctx, "gc: failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
						return
					}
					// delete wep
					err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Delete(ctx, wep.Name, *metav1.NewDeleteOptions(0))
					if err != nil {
						log.Errorf(ctx, "gc: failed to delete wep for leaked pod (%v %v): %v", wep.Namespace, wep.Name, err)
					} else {
						log.Infof(ctx, "gc: delete wep for leaked pod (%v %v) successfully", wep.Namespace, wep.Name)
						ipam.removeIPFromCache(wep.Spec.IP, false)
						ipam.removeIPFromLeakedCache(wep.Spec.InstanceID, wep.Spec.SubnetID, wep.Spec.Node, wep.Spec.IP)
					}
				}(wep)
			} else {
				// get pod error, but is not 404
				log.Errorf(ctx, "gc: failed to get pod (%v %v): %v", wep.Namespace, wep.Name, err)
			}
		}
	}

	wg.Wait()

	return nil
}

func (ipam *IPAM) gcLeakedIP(ctx context.Context) error {
	var (
		podIPSet = sets.NewString()
	)

	// list all pods
	pods, err := ipam.kubeInformer.Core().V1().Pods().Lister().List(labels.Everything())
	if err != nil {
		log.Errorf(ctx, "gc: error list pods in cluster: %v", err)
		return err
	}

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

	err = ipam.buildPossibleLeakedIPCache(ctx, podIPSet)
	if err != nil {
		return err
	}

	ipam.pruneExpiredLeakedIP(ctx)

	return nil
}

func (ipam *IPAM) buildPossibleLeakedIPCache(ctx context.Context, podIPSet sets.String) error {
	var (
		wg sync.WaitGroup
		ch = make(chan struct{}, 3)
	)

	bbcNodeSelector, _ := bbcNodeListerSelector()
	nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(bbcNodeSelector)
	if err != nil {
		log.Errorf(ctx, "gc: failed to list bbc nodes: %v", err)
		return err
	}

	// check each node
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
				log.Errorf(ctx, "gc: failed to get instance id of node %v: %v", node.Name, err)
				return
			}

			idleIPs, err := ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
			if err != nil {
				log.Errorf(ctx, "gc: failed to idle ips in datastore of node %v: %v", node.Name, err)
				return
			}

			idleIPSet := sets.NewString(idleIPs...)

			resp, err := ipam.cloud.GetBBCInstanceENI(ctx, instanceID)
			if err != nil {
				log.Errorf(ctx, "gc: get instance eni failed: %v", err)
			}

			ipam.lock.Lock()
			defer ipam.lock.Unlock()

			for _, ip := range resp.PrivateIpSet {
				if !ip.Primary {
					key := privateIPAddrKey{instanceID, ip.SubnetId, node.Name, ip.PrivateIpAddress}
					// if ip not in pod and neither in datastore as unassigned
					if !podIPSet.Has(ip.PrivateIpAddress) && !idleIPSet.Has(ip.PrivateIpAddress) {
						if _, ok := ipam.possibleLeakedIPCache[key]; !ok {
							ipam.possibleLeakedIPCache[key] = ipam.clock.Now()
							log.Warningf(ctx, "gc: node %v may has IP %v leaked", node.Name, ip.PrivateIpAddress)
						}
					} else {
						// if ip in pod, maybe a false positive
						if _, ok := ipam.possibleLeakedIPCache[key]; ok {
							delete(ipam.possibleLeakedIPCache, key)
							log.Warningf(ctx, "gc: remove IP %v on node %v from possibleLeakedIPCache", ip.PrivateIpAddress, node.Name)
						}
					}
				}
			}

		}(node)
	}

	wg.Wait()

	return nil
}

func (ipam *IPAM) pruneExpiredLeakedIP(ctx context.Context) {
	var (
		leakedIPs []privateIPAddrKey
	)

	ipam.lock.Lock()
	for key, activeTime := range ipam.possibleLeakedIPCache {
		if ipam.clock.Since(activeTime) > ipamgeneric.LeakedPrivateIPExpiredTimeout {
			log.Infof(ctx, "gc: found leaked ip on instance %v: %v", key.instanceID, key.ipAddr)
			leakedIPs = append(leakedIPs, privateIPAddrKey{key.instanceID, key.subnetID, key.nodeName, key.ipAddr})
			delete(ipam.possibleLeakedIPCache, key)
		}
	}
	ipam.lock.Unlock()

	// let's cleanup leaked ips
	for _, tuple := range leakedIPs {
		err := ipam.batchDelIP(ctx, &bbc.BatchDelIpArgs{
			InstanceId: tuple.instanceID,
			PrivateIps: []string{tuple.ipAddr},
		})

		if err != nil {
			log.Errorf(ctx, "gc: failed to delete private IP %v on %v: %v", tuple.ipAddr, tuple.nodeName, err)
			if !cloud.IsErrorBBCENIPrivateIPNotFound(err) {
				log.Errorf(ctx, "gc: stop delete private IP %v on %v, try next round", tuple.ipAddr, tuple.nodeName)
				continue
			}
		} else {
			log.Infof(ctx, "gc: delete leaked private IP %v on %v successfully", tuple.ipAddr, tuple.nodeName)
		}

		// delete ip from datastore
		err = ipam.datastore.ReleasePodPrivateIP(tuple.nodeName, tuple.subnetID, tuple.ipAddr)
		if err != nil {
			log.Errorf(ctx, "gc: error releasing private IP %v on node %v from datastore: %v", tuple.ipAddr, tuple.nodeName, err)
		}
		err = ipam.datastore.DeletePrivateIPFromStore(tuple.nodeName, tuple.subnetID, tuple.ipAddr)
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

				// clean up node eni map
				ipam.lock.Lock()
				delete(ipam.nodeENIMap, node)
				ipam.lock.Unlock()

				continue
			}
			return err
		}
	}

	return nil
}

func (ipam *IPAM) poolCorrupted(total, used int) bool {
	if total < 0 || used < 0 || total < used {
		return true
	}

	return false
}

func (ipam *IPAM) increasePool(
	ctx context.Context,
	node, instanceID string,
	candidateSubnets []string,
	addIPNum int,
) error {
	// subnet cache from subnet cr
	var subnets []subnet

	// build subnet cache
	for _, sbn := range candidateSubnets {
		sbnCr, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(sbn)
		if err != nil || !sbnCr.Status.Enable {
			log.Warningf(ctx, "failed to get subnet cr %v: %v", sbn, err)
		} else {
			subnets = append(subnets, subnet{subnetID: sbn, availableCnt: sbnCr.Status.AvailableIPNum})
		}
	}

	// sort subnet cache by available ip count
	sort.Slice(subnets, func(i, j int) bool {
		return subnets[i].availableCnt > subnets[j].availableCnt
	})

	var batchAddResult *bbc.BatchAddIpResponse
	var batchAddResultSubnet string
	var err error
	var errs []error

	t := time.Now()
	for _, sbn := range subnets {
		batchAddResult, err = ipam.tryBatchAddIP(ctx, node, instanceID, sbn.subnetID, addIPNum, ipam.backoffCap(len(subnets)))
		if err == nil {
			batchAddResultSubnet = sbn.subnetID
			break
		}
		errs = append(errs, err)
		log.Errorf(ctx, "batch add private ip(s) for %v in subnet %s failed: %v, try next subnet...", node, sbn.subnetID, err)
	}
	log.Infof(ctx, "tryBatchAddIP elapsed %v", time.Since(t))

	// none of the subnets succeeded
	if batchAddResult == nil {
		err := utilerrors.NewAggregate(errs)
		log.Errorf(ctx, "failed to batch add private ip(s) for %v in all subnets %+v: %v", node, subnets, err)
		return err
	}

	// add ip to store finally
	for _, ip := range batchAddResult.PrivateIps {
		err = ipam.datastore.AddPrivateIPToStore(node, batchAddResultSubnet, ip, false)
		if err != nil {
			msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip, err)
			log.Error(ctx, msg)
			return goerrors.New(msg)
		}
	}

	return nil
}

func (ipam *IPAM) batchAddIPCrossSubnet(ctx context.Context, args *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error) {
	t := time.Now()
	defer func(t time.Time) {
		log.Infof(ctx, "batchAddIPCrossSubnet with rate limit elapsed %v", time.Since(t))
	}(t)
	ipam.bucket.Wait(1)
	return ipam.cloud.BBCBatchAddIPCrossSubnet(ctx, args)
}

func (ipam *IPAM) batchAddIPCrossSubnetWithExponentialBackoff(
	ctx context.Context,
	args *bbc.BatchAddIpCrossSubnetArgs,
	nodeName string) (*bbc.BatchAddIpResponse, error) {
	var backoffWaitPeriod time.Duration
	var backoff *wait.Backoff
	var ok bool
	const waitPeriodNum = 10

	ipam.lock.Lock()
	backoff = ipam.addIPBackoffCache[args.InstanceId]
	if backoff != nil && backoff.Steps >= 0 {
		backoffWaitPeriod = backoff.Step()
	}
	ipam.lock.Unlock()

	if backoffWaitPeriod != 0 {
		log.Infof(ctx, "backoff: wait %v to allocate private ip at %v", backoffWaitPeriod, args.InstanceId)
		// instead of sleep a large amount of time, we divide sleep time into small parts to check backoff cache.
		for i := 0; i < waitPeriodNum; i++ {
			time.Sleep(backoffWaitPeriod / waitPeriodNum)
			ipam.lock.RLock()
			backoff, ok = ipam.addIPBackoffCache[args.InstanceId]
			ipam.lock.RUnlock()
			if !ok {
				log.Warningf(ctx, "found backoff on instance %v removed", args.InstanceId)
				break
			}
		}
	}

	// if have reached backoff cap, first check then add ip
	if backoff != nil && backoffWaitPeriod >= backoff.Cap {
		// 1. check if subnet still has available ip
		if len(args.SingleEniAndSubentIps) > 0 {
			subnetID := args.SingleEniAndSubentIps[0].SubnetId
			subnet, err := ipam.cloud.DescribeSubnet(ctx, subnetID)
			if err == nil && subnet.AvailableIp <= 0 {
				msg := fmt.Sprintf("backoff short-circuit: subnet %v has no available ip", subnetID)
				log.Warning(ctx, msg)
				return nil, goerrors.New(msg)
			}
			if err != nil {
				log.Errorf(ctx, "failed to describe subnet %v: %v", subnetID, err)
			}
		}

		// 2. check if node cannot attach more ip due to memory
		node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(nodeName)
		if err == nil {
			maxPrivateIPNum, err := utileni.GetMaxIPPerENIFromNodeAnnotations(node)
			if err == nil && maxPrivateIPNum != 0 {
				eniResult, err := ipam.cloud.GetBBCInstanceENI(ctx, args.InstanceId)
				if err == nil && len(eniResult.PrivateIpSet) >= maxPrivateIPNum {
					msg := fmt.Sprintf("backoff short-circuit: instance %v cannot add more ip due to memory", args.InstanceId)
					log.Warning(ctx, msg)
					return nil, goerrors.New(msg)
				}

				if err != nil {
					log.Errorf(ctx, "failed to get instance %v eni: %v", args.InstanceId, err)
				}
			}
		}
	}

	return ipam.batchAddIPCrossSubnet(ctx, args)
}

func (ipam *IPAM) batchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error {
	ipam.bucket.Wait(1)
	return ipam.cloud.BBCBatchDelIP(ctx, args)
}

func (ipam *IPAM) tryBatchAddIP(
	ctx context.Context,
	node, instanceID, subnetID string,
	batchAddNum int,
	backoffCap time.Duration,
) (*bbc.BatchAddIpResponse, error) {
	// alloc ip for bbc
	var err error
	var batchAddResult *bbc.BatchAddIpResponse

	for batchAddNum > 0 {
		batchAddResult, err = ipam.batchAddIPCrossSubnetWithExponentialBackoff(ctx, &bbc.BatchAddIpCrossSubnetArgs{
			InstanceId: instanceID,
			SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
				{
					EniId:                          ipam.nodeENIMap[node],
					SubnetId:                       subnetID,
					SecondaryPrivateIpAddressCount: batchAddNum,
				},
			},
		}, node)
		if err == nil {
			log.Infof(ctx, "batch add %v private ip(s) for %v in subnet %s successfully, %v", batchAddNum, node, subnetID, batchAddResult.PrivateIps)
			break
		}

		if err != nil {
			log.Warningf(ctx, "warn: batch add %v private ip(s) failed: %v", batchAddNum, err)

			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			if batchAddNum == 1 && cloud.IsErrorSubnetHasNoMoreIP(err) {
				sbncontroller.DeclareSubnetHasNoMoreIP(ctx, ipam.crdClient, ipam.crdInformer, subnetID, true)
			}
			if isErrorNeedExponentialBackoff(err) {
				if batchAddNum == 1 {
					ipam.lock.Lock()
					if _, ok := ipam.addIPBackoffCache[instanceID]; !ok {
						ipam.addIPBackoffCache[instanceID] = util.NewBackoffWithCap(backoffCap)
						log.Infof(ctx, "add backoff with cap %v for instance %v due to error: %v", backoffCap, instanceID, err)
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

	if batchAddResult == nil {
		msg := fmt.Sprintf("cannot batch add more ip to node %v, instance %v", node, instanceID)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	return batchAddResult, nil
}

func (ipam *IPAM) rebuildNodeDataStoreCache(ctx context.Context, node *v1.Node, instanceID string) error {
	ipam.bucket.Wait(1)
	resp, err := ipam.cloud.GetBBCInstanceENI(ctx, instanceID)
	if err != nil {
		msg := fmt.Sprintf("get instance eni failed: %v", err)
		log.Error(ctx, msg)
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
		return goerrors.New(msg)
	}

	ipam.lock.Lock()
	// if node was moved out and rejoined in, should cleanup previous backoff cache
	delete(ipam.addIPBackoffCache, instanceID)
	// build node eni map
	ipam.nodeENIMap[node.Name] = resp.Id
	ipam.lock.Unlock()

	log.Infof(ctx, "eni id of node %v is: %v", node.Name, resp.Id)

	// add node to store
	err = ipam.datastore.AddNodeToStore(node.Name, instanceID)
	if err != nil {
		msg := fmt.Sprintf("add node %v to datastore failed: %v", node.Name, err)
		log.Error(ctx, msg)
		return goerrors.New(msg)
	}
	log.Infof(ctx, "add node %v to datastore successfully", node.Name)

	ipam.lock.RLock()
	defer ipam.lock.RUnlock()
	// add ip to store
	for _, ip := range resp.PrivateIpSet {
		if !ip.Primary {
			// ipam will sync all weps to build allocated cache when it starts
			assigned := false
			if _, ok := ipam.allocated[ip.PrivateIpAddress]; ok {
				assigned = true
			}
			err = ipam.datastore.AddPrivateIPToStore(node.Name, ip.SubnetId, ip.PrivateIpAddress, assigned)
			if err != nil {
				if err == datastorev2.EmptySubnetError {
					// if subnetId of each ip is empty, cannot rebuild datastore cache.
					// clear node from datastore and wait for next request.
					ipam.datastore.DeleteNodeFromStore(node.Name)
				}
				msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip.PrivateIpAddress, err)
				log.Error(ctx, msg)
				return goerrors.New(msg)
			}
			log.Infof(ctx, "add private ip %v from subnet %v to datastore successfully", ip.PrivateIpAddress, ip.SubnetId)
		}
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

func (ipam *IPAM) buildAllocatedNodeCache(ctx context.Context) {
	var (
		wg sync.WaitGroup
		ch = make(chan struct{}, 10)
	)

	bbcNodeSelector, _ := bbcNodeListerSelector()
	nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(bbcNodeSelector)
	if err != nil {
		log.Errorf(ctx, "failed to list bbc nodes: %v", err)
		return
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
				log.Errorf(ctx, "failed to get instance id of node %v: %v", node.Name, err)
				return
			}
			err = ipam.rebuildNodeDataStoreCache(ctx, node, instanceID)
			if err != nil {
				log.Errorf(ctx, "rebuild cache for node %v error: %v", node.Name, err)
				return
			}
			log.Infof(ctx, "rebuild cache for node %v successfully", node.Name)

		}(node)
	}

	wg.Wait()
}

func (ipam *IPAM) removeIPFromCache(ipAddr string, lockless bool) {
	if !lockless {
		ipam.lock.Lock()
		defer ipam.lock.Unlock()
	}
	delete(ipam.allocated, ipAddr)
}

func (ipam *IPAM) removeIPFromLeakedCache(intanceID, subnetID, node, ipAddr string) {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	delete(ipam.possibleLeakedIPCache, privateIPAddrKey{intanceID, subnetID, node, ipAddr})
}

func (ipam *IPAM) createSubnetCRs(ctx context.Context, candidateSubnets []string) error {
	var errs []error

	for _, sbn := range candidateSubnets {
		_, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(sbn)
		if err != nil && errors.IsNotFound(err) {
			log.Warningf(ctx, "subnet cr for %v is not found, will create...", sbn)

			ipam.bucket.Wait(1)
			if err := sbncontroller.CreateSubnetCR(ctx, ipam.cloud, ipam.crdClient, sbn); err != nil {
				log.Errorf(ctx, "failed to create subnet cr for %v: %v", sbn, err)
				errs = append(errs, err)
				continue
			}

			log.Infof(ctx, "create subnet cr for %v successfully", sbn)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (ipam *IPAM) idleIPNum(node string) int {
	total, used, err := ipam.datastore.GetNodeStats(node)
	idle := total - used
	if err == nil && idle >= 0 {
		return idle
	}
	return 0
}

func (ipam *IPAM) backoffCap(subnetNum int) time.Duration {
	var addIPMaxTry int = int(math.Log2(float64(ipam.batchAddIPNum))) + 1
	return ipamgeneric.CCECniTimeout / time.Duration(addIPMaxTry) / time.Duration(subnetNum)
}

func isErrorNeedExponentialBackoff(err error) bool {
	return cloud.IsErrorBBCENIPrivateIPExceedLimit(err) || cloud.IsErrorSubnetHasNoMoreIP(err)
}

func PodNeedsSpecificSubnets(pod *v1.Pod) bool {
	if pod.Annotations != nil && pod.Annotations[PodAnnotationSpecificSubnets] != "" {
		return true
	}
	return false
}

func GetPodSpecificSubnetsFromAnnotation(pod *v1.Pod) []string {
	var result []string
	subnets := strings.Split(pod.Annotations[PodAnnotationSpecificSubnets], ",")
	for _, s := range subnets {
		result = append(result, strings.TrimSpace(s))
	}
	return result
}

func wepListerSelector() (labels.Selector, error) {
	// for wep owned by bbc, use selector "cce.io/instance-type=bbc"
	requirement, err := labels.NewRequirement(ipamgeneric.WepLabelInstanceTypeKey, selection.Equals, []string{string(metadata.InstanceTypeExBBC)})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}

func bbcNodeListerSelector() (labels.Selector, error) {
	requirement, err := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BBC"})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}
