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
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/topology_spread"
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

func (ipam *IPAM) Ready(ctx context.Context) bool {
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
	debug bool,
) (ipamgeneric.Interface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	// build subsystem controller
	sbnController := subnet.NewSubnetController(crdInformer, crdClient, bceClient, eventBroadcaster)
	pstsController := topology_spread.NewTopologySpreadController(kubeInformer, crdInformer, crdClient, eventBroadcaster, sbnController)

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
	ipam.buildAllocatedNodeCache(ctx)

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

	group.Start(func() { ipam.checkIdleIPPoolPeriodically() })

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
		ipam.reconcileRelationOfWepEni(stopCh)
	})
	// k8sr resource and ip cache are synced
	ipam.cacheHasSynced = true
	group.Wait()
	<-stopCh
	return nil
}

func (ipam *IPAM) tryAllocateIPForFixIPPod(ctx context.Context, eni *enisdk.Eni, wep *v1alpha1.WorkloadEndpoint, ipToAllocate string, node *v1.Node, backoffCap time.Duration) (string, error) {
	var namespace, name string = wep.Namespace, wep.Name
	var ipResult []string
	var err error
	// Note: here DeletePrivateIP and AddPrivateIP should be atomic. we leverage a lock to do this
	// ensure private ip not attached to other eni

	err = ipam.__tryDeleteIPByIPAndENI(ctx, wep.Spec.Node, wep.Spec.IP, wep.Spec.ENIID, true)
	if err != nil && !cloud.IsErrorENIPrivateIPNotFound(err) {
		log.Errorf(ctx, "error delete private IP %v for pod (%v %v): %v", wep.Spec.IP, namespace, name, err)
		if cloud.IsErrorRateLimit(err) {
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		}
	}

	allocIPMaxTry := 3
	for i := 0; i < allocIPMaxTry; i++ {
		log.Infof(ctx, "try to add IP %v to %v", ipToAllocate, eni.EniId)
		ipResult, err = ipam.batchAddPrivateIP(ctx, []string{ipToAllocate}, 0, eni.EniId)
		if err != nil {
			log.Errorf(ctx, "error add private IP %v for pod (%v %v): %v", ipToAllocate, namespace, name, err)
			if cloud.IsErrorSubnetHasNoMoreIP(err) {
				if e := ipam.sbnCtl.DeclareSubnetHasNoMoreIP(ctx, eni.SubnetId, true); e != nil {
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
			return "", err
		} else if err == nil {
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
				if e := ipam.sbnCtl.DeclareSubnetHasNoMoreIP(ctx, eni.SubnetId, true); e != nil {
					log.Errorf(ctx, "failed to patch subnet %v that has no more ip: %v", eni.SubnetId, e)
				}
			}
			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}

			if batchAddNum == 1 && cloud.IsErrorSubnetHasNoMoreIP(err) {
				ipam.sbnCtl.DeclareSubnetHasNoMoreIP(ctx, eni.SubnetId, true)
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
		msg := fmt.Sprintf("cannot batch add more ip to eni on node %s instance %s", node.Name, eni.EniId)
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

func (ipam *IPAM) handleIncreasePoolEvent(ctx context.Context, node *v1.Node, ch chan *event) {
	log.Infof(ctx, "start increase pool goroutine for node %v", node.Name)
	for e := range ch {
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
		if e.passive && ipam.canAllocateIP(ctx, e.node.Name, e.enis) {
			continue
		}

		if !e.passive && ipam.idleIPNum(e.node.Name) >= ipam.idleIPPoolMinSize {
			continue
		}

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
	}
	log.Infof(ctx, "closed channel for node %v, exit...", node.Name)
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
				assigned := false
				if _, ok := ipam.allocated[ip.PrivateIpAddress]; ok {
					assigned = true
				}

				// If the private IP is not in the CIDR of Eni, it means that it is a cross subnet IP
				crossSubnet := !ipNet.Contains(net.ParseIP(ip.PrivateIpAddress))
				if crossSubnet {
					assigned = true
				}

				ipam.datastore.Synchronized(func() error {
					err := ipam.datastore.AddPrivateIPToStoreUnsafe(node.Name, eni.EniId, ip.PrivateIpAddress, assigned, crossSubnet)
					if err != nil {
						msg := fmt.Sprintf("add private ip %v to datastore failed: %v", ip.PrivateIpAddress, err)
						log.Error(ctx, msg)
					} else {
						log.Infof(ctx, "add private ip %v(assigned: %v) from eni %v to node %v datastore successfully", ip.PrivateIpAddress, assigned, eni.EniId, node.Name)
					}
					return err
				})

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
	ipam.lock.Lock()
	if _, ok := ipam.addIPBackoffCache[eniID]; !ok {
		ipam.addIPBackoffCache[eniID] = util.NewBackoffWithCap(cap)
	}
	ipam.lock.Unlock()
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

				ipam.sendIncreasePoolEvent(ctx, node, enis, false)
			}

		}(node)
	}

	wg.Wait()

	return false, nil
}

// sendIncreasePoolEvent send increase poll event to cache chan
// create chan if not exsit
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
		ch = make(chan *event)
		ipam.increasePoolEventChan[node.Name] = ch
		go ipam.handleIncreasePoolEvent(ctx, evt.node, ch)
	}
	ipam.lock.Unlock()

	ch <- evt
}

func (ipam *IPAM) checkIdleIPPoolPeriodically() error {
	return wait.PollImmediateInfinite(30*time.Second, ipam.checkIdleIPPool)
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
