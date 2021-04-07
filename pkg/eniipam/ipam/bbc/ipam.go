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
	"time"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/juju/ratelimit"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/runtime"
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
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	ipamtypes "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/keymutex"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	// cniTimeout set to be slightly less than 220 sec in kubelet
	// Ref: https://github.com/kubernetes/kubernetes/pull/71653
	cniTimeout                 = 210 * time.Second
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
		eventBroadcaster: eventBroadcaster,
		eventRecorder:    recorder,
		kubeInformer:     kubeInformer,
		kubeClient:       kubeClient,
		crdInformer:      crdInformer,
		crdClient:        crdClient,
		cloud:            bceClient,
		clock:            clock.RealClock{},
		cniMode:          cniMode,
		vpcID:            vpcID,
		clusterID:        clusterID,
		datastore:        datastore.NewDataStore(),
		allocated:        make(map[string]*v1alpha1.WorkloadEndpoint),
		bucket:           ratelimit.NewBucketWithRate(ipMutatingRate, ipMutatingBurst),
		batchAddIPNum:    batchAddIPNum,
		cacheHasSynced:   false,
		gcPeriod:         gcPeriod,
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

	// get instance id from node
	instanceID := util.GetInstanceIDFromNode(node)

	// add lock for each node
	if !ipam.nodeLock.WaitLock(node.Name, (int)(cniTimeout/keymutex.WaitLockSleepTime)) {
		msg := fmt.Sprintf("wait key lock for node %v timeout", node.Name)
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

	// check if we need to batch add ip
	total, used, err := ipam.datastore.GetNodeStats(node.Name)
	if err != nil {
		msg := fmt.Sprintf("get node %v stats failed: %v", node, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	log.Infof(ctx, "total, used before allocate for node %v: %v %v", node.Name, total, used)

	unassignedIPs, _ := ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	log.Infof(ctx, "unassigned ip in datastore before allocate: %v", unassignedIPs)

	// if pool status is corrupted, clean and wait until rebuilding
	// we are unlikely to hit this
	if ipam.poolCorrupted(total, used) {
		log.Errorf(ctx, "corrupted pool status detected on node %v. total, used: %v, %v", node.Name, total, used)
		ipam.datastore.DeleteNodeFromStore(node.Name)
		return nil, fmt.Errorf("ipam cached pool rebuilding")
	}

	// if pool status is low, batch add ips
	if ipam.poolTooLow(total, used) {
		start := time.Now()
		err = ipam.increasePool(ctx, node.Name, instanceID)
		if err != nil {
			log.Errorf(ctx, "increase pool for node %v failed: %v", node.Name, err)
		}
		log.Infof(ctx, "increase pool takes %v to finish", time.Since(start))
	}

	// allocated ip result
	ipResult, err := ipam.datastore.AllocatePodPrivateIP(node.Name)
	if err != nil {
		msg := fmt.Sprintf("error add private IP for pod (%v %v): %v", namespace, name, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	wep := &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels: map[string]string{
				ipamtypes.WepLabelInstanceTypeKey: string(metadata.InstanceTypeExBBC),
			},
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ContainerID: containerID,
			IP:          ipResult,
			Node:        pod.Spec.NodeName,
			ENIID:       instanceID,
			UpdateAt:    metav1.Time{ipam.clock.Now()},
		},
	}

	// create wep
	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Create(wep)
	if err != nil {
		log.Errorf(ctx, "failed to create wep for pod (%v %v): %v", namespace, name, err)
		if delErr := ipam.datastore.ReleasePodPrivateIP(node.Name, instanceID, ipResult); delErr != nil {
			log.Errorf(ctx, "rollback: error releasing private IP %v from datastore for pod (%v %v): %v", ipResult, namespace, name, delErr)
		}
		return nil, err
	}
	log.Infof(ctx, "create wep with spec %+v for pod (%v %v) successfully", wep.Spec, namespace, name)

	ipam.lock.Lock()
	ipam.allocated[ipResult] = wep
	ipam.lock.Unlock()

	total, used, err = ipam.datastore.GetNodeStats(node.Name)
	if err == nil {
		log.Infof(ctx, "total, used after allocate for node %v: %v %v", node.Name, total, used)
	}
	unassignedIPs, err = ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	if err == nil {
		log.Infof(ctx, "unassigned ip in datastore after allocate: %v", unassignedIPs)
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

	ipam.bucket.Wait(1)
	// delete eni ip, delete fip crd
	log.Infof(ctx, "try to release private IP and wep for pod (%v %v)", namespace, name)
	err = ipam.cloud.BBCBatchDelIP(ctx, &bbc.BatchDelIpArgs{
		InstanceId: wep.Spec.ENIID,
		PrivateIps: []string{wep.Spec.IP},
	})
	if err != nil {
		log.Errorf(ctx, "release: error deleting private IP %v for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
		if !cloud.IsErrorBBCENIPrivateIPNotFound(err) {
			return nil, err
		}
	}
	log.Infof(ctx, "release private IP for pod (%v %v) successfully", namespace, name)

	// update datastore
	err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
	if err != nil {
		log.Errorf(ctx, "release: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
	}
	err = ipam.datastore.DeletePrivateIPFromStore(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
	if err != nil {
		log.Errorf(ctx, "release: error deleting private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
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

	ipam.kubeInformer.Start(stopCh)
	ipam.crdInformer.Start(stopCh)

	if !cache.WaitForNamedCacheSync(
		"cce-ipam",
		stopCh,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		wepInformer.HasSynced,
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

				ipam.bucket.Wait(1)
				// delete ip
				err = ipam.cloud.BBCBatchDelIP(ctx, &bbc.BatchDelIpArgs{
					InstanceId: wep.Spec.ENIID,
					PrivateIps: []string{wep.Spec.IP},
				})

				if err != nil {
					log.Errorf(ctx, "gc: failed to delete private IP %v on %v for leaked pod (%v %v): %v", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name, err)
					if cloud.IsErrorRateLimit(err) {
						time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
					}
					if !cloud.IsErrorBBCENIPrivateIPNotFound(err) {
						log.Errorf(ctx, "gc: stop delete wep for leaked pod (%v %v), try next round", wep.Namespace, wep.Name)
						continue
					}
				} else {
					log.Infof(ctx, "gc: delete private IP %v on %v for leaked pod (%v %v) successfully", wep.Spec.IP, wep.Spec.ENIID, wep.Namespace, wep.Name)
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

				// delete wep
				err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Delete(wep.Name, metav1.NewDeleteOptions(0))
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
				delErr := ipam.datastore.DeleteNodeFromStore(node)
				if delErr != nil {
					log.Errorf(ctx, "error delete node %v from datastore: %v", node, delErr)
				}
				continue
			}
			return err
		}
	}

	return nil
}

func (ipam *IPAM) poolTooLow(total, used int) bool {
	available := total - used

	if available <= 0 {
		return true
	}

	return false
}

func (ipam *IPAM) poolCorrupted(total, used int) bool {
	if total < 0 || used < 0 || total < used {
		return true
	}

	return false
}

func (ipam *IPAM) increasePool(ctx context.Context, node, instanceID string) error {
	// alloc ip for bbc
	var err error
	var batchAddNum int = ipam.batchAddIPNum
	var batchAddResult *bbc.BatchAddIpResponse

	for batchAddNum > 0 {
		ipam.bucket.Wait(1)
		batchAddResult, err = ipam.cloud.BBCBatchAddIP(ctx, &bbc.BatchAddIpArgs{
			InstanceId:                     instanceID,
			SecondaryPrivateIpAddressCount: batchAddNum,
		})
		if err == nil {
			log.Infof(ctx, "batch add %v private ip(s) for %v successfully, %v", batchAddNum, node, batchAddResult.PrivateIps)
			break
		}

		if err != nil {
			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			if cloud.IsErrorBBCENIPrivateIPExceedLimit(err) {
				batchAddNum = batchAddNum >> 1
				continue
			}

			msg := fmt.Sprintf("error batch add %v private ip(s): %v", batchAddNum, err)
			log.Error(ctx, msg)
			return goerrors.New(msg)
		}
	}

	if batchAddResult == nil {
		msg := fmt.Sprintf("cannot batch add more ip to node %v, instance %v", node, instanceID)
		log.Error(ctx, msg)
		return goerrors.New(msg)
	}

	for _, ip := range batchAddResult.PrivateIps {
		err = ipam.datastore.AddPrivateIPToStore(node, instanceID, ip, false)
		if err != nil {
			msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip, err)
			log.Error(ctx, msg)
			return goerrors.New(msg)
		}
	}

	return nil
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

	// add node to store
	err = ipam.datastore.AddNodeToStore(node.Name, instanceID)
	if err != nil {
		msg := fmt.Sprintf("add node %v to datastore failed: %v", node.Name, err)
		log.Error(ctx, msg)
		return goerrors.New(msg)
	}

	// add eni to store
	ipam.datastore.AddENIToStore(node.Name, instanceID)
	if err != nil {
		msg := fmt.Sprintf("add eni %v to datastore failed: %v", instanceID, err)
		log.Error(ctx, msg)
		return goerrors.New(msg)
	}

	ipam.lock.RLock()
	defer ipam.lock.RUnlock()
	// add ip to store
	for _, ip := range resp.PrivateIpSet {
		if !ip.Primary {
			assigned := false
			if _, ok := ipam.allocated[ip.PrivateIpAddress]; ok {
				assigned = true
			}
			err = ipam.datastore.AddPrivateIPToStore(node.Name, instanceID, ip.PrivateIpAddress, assigned)
			if err != nil {
				msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip.PrivateIpAddress, err)
				log.Error(ctx, msg)
				return goerrors.New(msg)
			}
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
		log.Infof(ctx, "build cache: found IP %v assigned to pod (%v %v)", wep.Spec.IP, wep.Namespace, wep.Name)
	}

	return nil
}

func wepListerSelector() (labels.Selector, error) {
	// for wep owned by bbc, use selector "cce.io/instance-type=bbc"
	requirement, err := labels.NewRequirement(ipamtypes.WepLabelInstanceTypeKey, selection.Equals, []string{string(metadata.InstanceTypeExBBC)})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}
