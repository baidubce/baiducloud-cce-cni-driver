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
	"time"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/juju/ratelimit"
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
	"k8s.io/kubernetes/pkg/kubelet/dockershim/network"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	datastorev2 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
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
	bceClient cloud.Interface,
	cniMode types.ContainerNetworkMode,
	vpcID string,
	clusterID string,
	resyncPeriod time.Duration,
	gcPeriod time.Duration,
	batchAddIPNum int,
	ipMutatingRate float64,
	ipMutatingBurst int64,
) (ipam.Interface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, resyncPeriod)
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, resyncPeriod)

	ipam := &IPAM{
		eventBroadcaster:  eventBroadcaster,
		eventRecorder:     recorder,
		kubeInformer:      kubeInformer,
		kubeClient:        kubeClient,
		crdInformer:       crdInformer,
		crdClient:         crdClient,
		cloud:             bceClient,
		clock:             clock.RealClock{},
		cniMode:           cniMode,
		vpcID:             vpcID,
		clusterID:         clusterID,
		datastore:         datastorev2.NewDataStore(),
		addIPBackoffCache: make(map[string]*wait.Backoff),
		allocated:         make(map[string]*v1alpha1.WorkloadEndpoint),
		bucket:            ratelimit.NewBucketWithRate(ipMutatingRate, ipMutatingBurst),
		nodeENIMap:        make(map[string]string),
		batchAddIPNum:     batchAddIPNum,
		cacheHasSynced:    false,
		gcPeriod:          gcPeriod,
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

	// add key lock for each node
	if !ipam.nodeLock.WaitLock(node.Name, (network.CNITimeoutSec*time.Second-ipamgeneric.CniTimeout)/2) {
		msg := fmt.Sprintf("QPS exceed limit on node %v", node.Name)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}
	defer ipam.nodeLock.UnLock(node.Name)

	// check if we need to rebuild cache
	if !ipam.datastore.NodeExistsInStore(node.Name) {
		log.Infof(ctx, "rebuild cache for node %v starts", node.Name)
		err = ipam.rebuildNodeDataStoreCache(ctx, node, instanceID)
		if err != nil {
			log.Errorf(ctx, "rebuild cache for node %v error: %v", node.Name, err)
			return nil, err
		}
		log.Infof(ctx, "rebuild cache for node %v ends", node.Name)
	}

	// get node stats from store, to further check if pool is corrupted
	total, used, err := ipam.datastore.GetNodeStats(node.Name)
	if err != nil {
		msg := fmt.Sprintf("get node %v stats in datastore failed: %v", node, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	log.Infof(ctx, "total, used before allocate for node %v: %v %v", node.Name, total, used)

	unassignedIPs, _ := ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	log.Infof(ctx, "unassigned ip in datastore before allocate for node %v: %v", node.Name, unassignedIPs)

	// if pool status is corrupted, clean and wait until rebuilding
	// we are unlikely to hit this
	if ipam.poolCorrupted(total, used) {
		log.Errorf(ctx, "corrupted pool status detected on node %v. total, used: %v, %v", node.Name, total, used)
		ipam.datastore.DeleteNodeFromStore(node.Name)
		return nil, fmt.Errorf("ipam cached pool is rebuilding")
	}

	// if pool cannot allocate ip, then batch add ips
	if !ipam.poolCanAllocateIP(ctx, node.Name, candidateSubnets) {
		start := time.Now()
		err = ipam.increasePool(ctx, node.Name, instanceID, candidateSubnets, pod)
		if err != nil {
			log.Errorf(ctx, "increase pool for node %v failed: %v", node.Name, err)
		}
		log.Infof(ctx, "increase pool takes %v to finish", time.Since(start))
	}

	// try to allocate ip by subnets for both cases: pod that requires specific subnets or not
	ipResult, ipSubnet, err := ipam.tryAllocateIPBySubnets(ctx, node.Name, candidateSubnets)
	if err != nil {
		msg := fmt.Sprintf("error allocate private IP for pod (%v %v): %v", namespace, name, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

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
			UpdateAt:    metav1.Time{ipam.clock.Now()},
		},
	}

	// create wep
	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Create(ctx, wep, metav1.CreateOptions{})
	if err != nil {
		log.Errorf(ctx, "failed to create wep for pod (%v %v): %v", namespace, name, err)
		if delErr := ipam.datastore.ReleasePodPrivateIP(node.Name, ipSubnet, ipResult); delErr != nil {
			log.Errorf(ctx, "rollback: error releasing private IP %v from datastore for pod (%v %v): %v", ipResult, namespace, name, delErr)
		}
		return nil, err
	}
	log.Infof(ctx, "create wep with spec %+v for pod (%v %v) successfully", wep.Spec, namespace, name)

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
	unassignedIPs, err = ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	if err == nil {
		log.Infof(ctx, "unassigned ip in datastore after allocate for node %v: %v", node.Name, unassignedIPs)
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
			log.Errorf(ctx, "datastore try allocate ip in subnet %v failed: %v", sbn.subnetID, err)
		}
	}

	return "", "", fmt.Errorf("no available ip address in datastore for subnets: %v", candidateSubnets)
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

	ipam.lock.Lock()
	delete(ipam.allocated, wep.Spec.IP)
	ipam.lock.Unlock()

	total, used, err := ipam.datastore.GetNodeStats(wep.Spec.Node)
	if err == nil {
		log.Infof(ctx, "total, used after release for node %v: %v %v", wep.Spec.Node, total, used)
	}
	unassignedIPs, err := ipam.datastore.GetUnassignedPrivateIPByNode(wep.Spec.Node)
	if err == nil {
		log.Infof(ctx, "unassigned ip in datastore after release for node %v: %v", wep.Spec.Node, unassignedIPs)
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

	// rebuild datastore cache
	err := ipam.buildAllocatedCache(ctx)
	if err != nil {
		return err
	}

	ipam.cacheHasSynced = true

	go func() {
		if err := ipam.gc(stopCh); err != nil {
			log.Errorf(ctx, "failed to start ipam gc: %v", err)
		}
	}()

	<-stopCh
	return nil
}

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	err := wait.PollImmediateUntil(ipam.gcPeriod, func() (bool, error) {
		ctx := log.NewContext()

		selector, err := wepListerSelector()
		if err != nil {
			log.Errorf(ctx, "error parsing requirement: %v", err)
			return false, nil
		}

		wepList, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().List(selector)
		if err != nil {
			log.Errorf(ctx, "gc: error list wep in cluster: %v", err)
			return false, nil
		}

		// release non-sts wep if pod not found
		err = ipam.gcLeakedPod(ctx, wepList)
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
	for _, wep := range wepList {
		_, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(wep.Namespace).Get(wep.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("gc: try to release leaked wep (%v %v)", wep.Namespace, wep.Name)
				log.Info(ctx, msg)
				ipam.eventRecorder.Event(&v1.ObjectReference{
					Kind: "wep",
					Name: fmt.Sprintf("%v %v", wep.Namespace, wep.Name),
				}, v1.EventTypeWarning, "PodLeaked", msg)

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
						continue
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

				// remove finalizers
				wep.Finalizers = nil
				_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(ctx, wep, metav1.UpdateOptions{})
				if err != nil {
					log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
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

					ipam.lock.Lock()
					delete(ipam.allocated, wep.Spec.IP)
					ipam.lock.Unlock()
				}
			} else {
				log.Errorf(ctx, "gc: failed to get pod (%v %v): %v", wep.Namespace, wep.Name, err)
			}
		}
	}

	return nil
}

func (ipam *IPAM) gcDeletedNode(ctx context.Context) error {
	for _, node := range ipam.datastore.ListNodes() {
		_, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(node)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof(ctx, "detect node %v has been deleted, clean up datastore", node)

				// clean up datastore
				delErr := ipam.datastore.DeleteNodeFromStore(node)
				if delErr != nil {
					log.Errorf(ctx, "error delete node %v from datastore: %v", node, delErr)
				}

				// clean up node eni map
				delete(ipam.nodeENIMap, node)

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
	pod *v1.Pod,
) error {
	// subnet cache from subnet cr
	var subnets []subnet

	// build subnet cache
	for _, sbn := range candidateSubnets {
		sbnCr, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(sbn)
		if err != nil {
			log.Warningf(ctx, "failed to get subnet cr %v: %v", sbn, err)
			subnets = append(subnets, subnet{subnetID: sbn, availableCnt: 0})
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

	for _, sbn := range subnets {
		batchAddResult, err = ipam.tryBatchAddIP(ctx, node, instanceID, sbn.subnetID, pod, ipam.backoffCap(len(subnets)))
		if err == nil {
			batchAddResultSubnet = sbn.subnetID
			break
		}
		errs = append(errs, err)
		log.Errorf(ctx, "batch add private ip(s) for %v in subnet %s failed: %v, try next subnet...", node, sbn.subnetID, err)
	}

	// none of the subnets succeeded
	if batchAddResult == nil {
		err := utilerrors.NewAggregate(errs)
		log.Errorf(ctx, "failed to batch add private ip(s) for %v in all subnets %+v: %v", node, subnets, err)
		return err
	}

	// finally add ip to store
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
	ipam.bucket.Wait(1)
	return ipam.cloud.BBCBatchAddIPCrossSubnet(ctx, args)
}

func (ipam *IPAM) batchAddIPCrossSubnetWithExponentialBackoff(
	ctx context.Context,
	args *bbc.BatchAddIpCrossSubnetArgs,
	pod *v1.Pod) (*bbc.BatchAddIpResponse, error) {
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
		node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
		if err == nil {
			maxPrivateIPNum, err := utileni.GetMaxIPPerENIFromNodeAnnotations(node)
			if err == nil && maxPrivateIPNum != 0 {
				eniResult, err := ipam.cloud.BBCGetInstanceENI(ctx, args.InstanceId)
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
	pod *v1.Pod,
	backoffCap time.Duration,
) (*bbc.BatchAddIpResponse, error) {
	// alloc ip for bbc
	var err error
	var batchAddNum int = ipam.batchAddIPNum
	var batchAddResult *bbc.BatchAddIpResponse
	var namespace, name string = pod.Namespace, pod.Name

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
		}, pod)
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
				ipamgeneric.DeclareSubnetHasNoMoreIP(ctx, ipam.crdClient, ipam.crdInformer, subnetID, true)
			}
			if isErrorNeedExponentialBackoff(err) {
				if batchAddNum == 1 {
					ipam.lock.Lock()
					if _, ok := ipam.addIPBackoffCache[instanceID]; !ok {
						ipam.addIPBackoffCache[instanceID] = util.NewBackoffWithCap(backoffCap)
						log.Infof(ctx, "add backoff with cap %v for instance %v when handling pod (%v %v) due to error: %v", backoffCap, instanceID, namespace, name, err)
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
	resp, err := ipam.cloud.BBCGetInstanceENI(ctx, instanceID)
	if err != nil {
		msg := fmt.Sprintf("get instance eni failed: %v", err)
		log.Error(ctx, msg)
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
		return goerrors.New(msg)
	}

	// if node was moved out and rejoined in, should cleanup previous backoff cache
	delete(ipam.addIPBackoffCache, instanceID)

	// build node eni map
	ipam.nodeENIMap[node.Name] = resp.Id
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

func (ipam *IPAM) createSubnetCRs(ctx context.Context, candidateSubnets []string) error {
	var errs []error

	for _, sbn := range candidateSubnets {
		_, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(sbn)
		if err != nil && errors.IsNotFound(err) {
			log.Warningf(ctx, "subnet cr for %v is not found, will create...", sbn)

			ipam.bucket.Wait(1)
			if err := ipamgeneric.CreateSubnetCR(ctx, ipam.cloud, ipam.crdClient, sbn); err != nil {
				log.Errorf(ctx, "failed to create subnet cr for %v: %v", sbn, err)
				errs = append(errs, err)
				continue
			}

			log.Infof(ctx, "create subnet cr for %v successfully", sbn)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (ipam *IPAM) backoffCap(subnetNum int) time.Duration {
	var addIPMaxTry int = int(math.Log2(float64(ipam.batchAddIPNum))) + 1
	return ipamgeneric.CniTimeout / time.Duration(addIPMaxTry) / time.Duration(subnetNum)
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
