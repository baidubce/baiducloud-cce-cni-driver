package roce

import (
	"context"
	goerrors "errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
)

func NewIPAM(
	kubeClient kubernetes.Interface,
	crdClient versioned.Interface,
	bceClient cloud.Interface,
	informerResyncPeriod time.Duration,
	eniSyncPeriod time.Duration,
	gcPeriod time.Duration,
	_ bool,
) (ipam.RoceInterface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-roce-ipam"})

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, informerResyncPeriod)
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, informerResyncPeriod)

	ipamServer := &IPAM{
		eventBroadcaster:      eventBroadcaster,
		eventRecorder:         recorder,
		kubeInformer:          kubeInformer,
		kubeClient:            kubeClient,
		crdInformer:           crdInformer,
		crdClient:             crdClient,
		gcPeriod:              gcPeriod,
		clock:                 clock.RealClock{},
		cloud:                 bceClient,
		eniSyncPeriod:         eniSyncPeriod,
		cacheHasSynced:        false,
		hpcEniCache:           make(map[string][]hpc.Result),
		nodeCache:             make(map[string]*corev1.Node),
		allocated:             make(map[string]*v1alpha1.MultiIPWorkloadEndpoint),
		increasePoolEventChan: make(map[string]chan *event),
	}
	return ipamServer, nil
}

const (
	// minPrivateIPLifeTime is the lifetime of a private ip (from allocation to release), aim to trade off db slave delay
	minPrivateIPLifeTime = 5 * time.Second

	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5
)

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string, mac string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Allocate] allocating IP for roce pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating IP for roce pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "roce ipam has not synced cache yet")
		return nil, fmt.Errorf("roce ipam has not synced cache yet")
	}

	pod, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(name)
	if err != nil {
		log.Errorf(ctx, "failed to list pod (%s/%s) in roce ipam from kubeInformer : %v", namespace, name, err)
		return nil, err
	}

	// find out which enis are suitable to bind
	instanceID, err := ipam.findMatchENIs(ctx, pod)
	if err != nil {
		log.Errorf(ctx, "failed to find a match eni for pod (%v %v) in roce ipam: %v", namespace, name, err)
		return nil, err
	}
	log.Infof(ctx, "get instanceID is %s ", instanceID)

	eniID, hpcEniList, err := ipam.findMatchedEniByMac(ctx, instanceID, mac)
	if err != nil {
		log.Errorf(ctx, "failed to find a suitable eni by mac for pod (%v %v) in roce ipam: %v", namespace, name, err)
		return nil, err
	}
	log.Infof(ctx, "find eniID is %s ", eniID)

	var ipResult = ""
	wep := &v1alpha1.WorkloadEndpoint{}
	roceNode := &corev1.Node{}

	node, exist := ipam.nodeCache[instanceID]
	if !exist {
		// get node
		node, err = ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
		if err != nil {
			log.Errorf(ctx, "failed to list node (%s/%s) in roce ipam from kubeInformer : %v", namespace, name, err)
			return nil, err
		}

		ipam.nodeCache[instanceID] = node
	}
	roceNode = node.DeepCopy()

	ipam.lock.Lock()
	defer ipam.lock.Unlock()
	mwep, err := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().MultiIPWorkloadEndpoints(namespace).Get(name)
	if err == nil {
		if mwep.Type != ipamgeneric.MwepTypeRoce {
			log.Warningf(ctx, "get mwep in %v/%v type is not roce", namespace, name)
			return nil, fmt.Errorf("get mwep in %v/%v type is not roce", namespace, name)
		}

		if mwep.NodeName != roceNode.Name {
			log.Warningf(ctx, "mwep node name: %s, not match current node in roce ipam: %s", mwep.NodeName, roceNode.Name)
			return nil, fmt.Errorf("mwep node name: %s, not match current node in roce ipam: %s", mwep.NodeName, roceNode.Name)
		}

		mwepInfo := make(map[string]v1alpha1.MultiIPWorkloadEndpointSpec)
		if len(mwep.Spec) != 0 && mwep.Spec != nil {
			for _, mwepSpec := range mwep.Spec {
				if _, exist := mwepInfo[mwepSpec.EniID]; !exist {
					mwepNewSpec := v1alpha1.MultiIPWorkloadEndpointSpec{
						IP:    mwepSpec.IP,
						EniID: mwepSpec.EniID,
						Mac:   mwepSpec.Mac,
					}
					mwepInfo[mwepSpec.EniID] = mwepNewSpec
				}
			}
		}
		log.Infof(ctx, "fill mwep success, mwep spec count is %v, mwepInfo count is %v ", len(mwep.Spec), len(mwepInfo))

		if _, exist := mwepInfo[eniID]; !exist {
			ipResult, err = ipam.tryAllocateIPForRoceIPPod(ctx, eniID, instanceID, hpcEniList, mwep)
			if err != nil {
				msg := fmt.Sprintf("error allocate private IP for pod (%v %v): %v", namespace, name, err)
				log.Error(ctx, msg)
				return nil, goerrors.New(msg)
			}
			log.Infof(ctx, "roce ipamd allocate ip success, allocate ip result %s ", ipResult)

			mwepNewSpec := v1alpha1.MultiIPWorkloadEndpointSpec{
				IP:    ipResult,
				EniID: eniID,
				Mac:   mac,
			}
			mwepInfo[eniID] = mwepNewSpec

			wep = &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					ENIID:       eniID,
					ContainerID: containerID,
					IP:          ipResult,
					Mac:         mac,
				},
			}
		}
		specList := make([]v1alpha1.MultiIPWorkloadEndpointSpec, 0)
		for _, spec := range mwepInfo {
			specList = append(specList, spec)
		}
		log.Infof(ctx, "new specList count is %d, mwepInfo count is %d ", len(specList), len(mwepInfo))

		mwep.Spec = specList

		_, err = ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(namespace).Update(ctx, mwep, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to update mwep by (%v/%v) pod: %v", namespace, name, err)
			time.Sleep(minPrivateIPLifeTime)
			if delErr := ipam.deleteRocePrivateIP(ctx, eniID, ipResult); delErr != nil {
				log.Errorf(ctx, "deleted roce private ip %v by pod (%v/%v) has error: %v", ipResult, namespace, name, delErr)
				return nil, err
			}
			return nil, err
		}
		ipam.allocated[ipResult] = mwep
		return wep, nil
	} else {
		if !errors.IsNotFound(err) {
			log.Errorf(ctx, "get mwep by pod (%v/%v) is not found: %v", pod.Namespace, pod.Name, err)
			return nil, err
		}
	}

	ipResult, err = ipam.addRocePrivateIP(ctx, eniID, instanceID, hpcEniList)
	if err != nil {
		log.Errorf(ctx, "allocate private IP by pod (%v/%v) from hpcEni interface has error: %v", namespace, name, err)
		return nil, err
	}

	log.Infof(ctx, "add roce private ip success. Private ip is %s ,macAddress is %s", ipResult, mac)

	mwep = &v1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       pod.Name,
			Namespace:  pod.Namespace,
			Finalizers: []string{ipamgeneric.MwepFinalizer},
			Labels: map[string]string{
				ipamgeneric.MwepLabelInstanceTypeKey: ipamgeneric.MwepTypeRoce,
				v1.LabelInstanceType:                 "BBC",
			},
		},
		NodeName:   roceNode.Name,
		Type:       ipamgeneric.MwepTypeRoce,
		InstanceID: instanceID,
		Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
			{
				EniID:       eniID,
				ContainerID: containerID,
				IP:          ipResult,
				Mac:         mac,
			},
		},
	}

	_, err = ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(namespace).Create(ctx, mwep, metav1.CreateOptions{})
	if err != nil {
		log.Errorf(ctx, "failed to create mwep by pod (%v/%v): %v", namespace, name, err)
		return nil, err
	}
	log.Infof(ctx, "create mwep %v by pod (%v/%v) success", mwep.Spec, namespace, name)

	//for replay
	wep = &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ENIID:       eniID,
			ContainerID: containerID,
			IP:          ipResult,
			Mac:         mac,
		},
	}
	log.Infof(ctx, "create wep %v/%v for reply to cni success", wep.Name, wep.Namespace)
	return wep, nil
}

func (ipam *IPAM) tryAllocateIPForRoceIPPod(
	ctx context.Context,
	eniID, instanceID string,
	hpcEniList *hpc.EniList,
	mwep *v1alpha1.MultiIPWorkloadEndpoint) (string, error) {
	log.Infof(ctx, "start allocate IP for roce privateIP pod %s, mwep is %v", eniID, mwep)

	var namespace, name = mwep.Namespace, mwep.Name
	allocIPMaxTry := 3

	var ipResult string
	var err error

	for i := 0; i < allocIPMaxTry; i++ {
		log.Infof(ctx, "allocIP max try is %d, now is %d time", allocIPMaxTry, i)

		ipResult, err = ipam.addRocePrivateIP(ctx, eniID, instanceID, hpcEniList)
		if err != nil {
			log.Errorf(ctx, "error add roce privateIP in eniID %v for pod %v/%v: %v", eniID, namespace, name, err)
			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			return "", err
		} else if err == nil {
			break
		}
	}

	log.Infof(ctx, "add hpc private IP %v by pod (%v %v) success", ipResult, namespace, name)
	metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, eniID).Inc()

	return ipResult, nil
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, _ string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Release] releasing IP for roce pod (%v/%v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing IP for roce pod (%v/%v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "release: roce ipamd has not synced cache yet")
		return nil, fmt.Errorf("release: roce ipamd has not synced cache yet")
	}

	//  mwep, avoid data racing
	tmpMwep, err := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().MultiIPWorkloadEndpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof(ctx, "release: get mwep by pod (%v/%v) not found", namespace, name)
			return nil, err
		}
		log.Errorf(ctx, "release: failed to get mwep of pod (%v/%v): %v", namespace, name, err)
		return nil, err
	}
	mwep := tmpMwep.DeepCopy()

	wep := &v1alpha1.WorkloadEndpoint{}
	//  delete eni ip, delete mwep crd
	for _, spec := range mwep.Spec {
		if err = ipam.deleteRocePrivateIP(ctx, spec.EniID, spec.IP); err != nil {
			log.Errorf(ctx, "release: error deleted roce private IP %v for pod (%v %v): %v", spec.IP, namespace, name, err)
		} else {
			ipam.lock.Lock()
			// ip was really on eni and deleted successfully, remove eni backoff and update privateIPNumCache
			if ipam.removeAddIPBackoffCache(spec.EniID, true) {
				log.Infof(ctx, "release: remove backoff for eni %v when handling pod (%v %v) due to successful ip release", spec.EniID, namespace, name)
			}
			ipam.lock.Unlock()
		}

		if err != nil && !(ipam.isErrorENIPrivateIPNotFound(err) || cloud.IsErrorENINotFound(err)) {
			if cloud.IsErrorRateLimit(err) {
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			log.Errorf(ctx, "release: delete %s privateIP from %s/%s mwep has error", spec.IP, mwep.Namespace, mwep.Name, err)
		}
		metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, spec.EniID).Dec()

		log.Infof(ctx, "release: private IP %v for pod (%v %v) success", spec.IP, namespace, name)
	}

	err = ipam.gcMwepAndCache(ctx, mwep)
	if err != nil {
		log.Errorf(ctx, "release: delete mwep %v/%v from gc has error: %v", namespace, name, err)
		return wep, err
	} else {
		log.Errorf(ctx, "release: delete mwep %v/%v from gc success: %v", namespace, name, err)
	}

	return wep, nil
}

func (ipam *IPAM) isErrorENIPrivateIPNotFound(err error) bool {
	return cloud.IsErrorENIPrivateIPNotFound(err)
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

func (ipam *IPAM) Ready(_ context.Context) bool {
	return ipam.cacheHasSynced
}

func (ipam *IPAM) Run(ctx context.Context, stopCh <-chan struct{}) error {
	defer func() {
		runtime.HandleCrash()
	}()

	log.Info(ctx, "Starting cce ipam controller for roce")
	defer log.Info(ctx, "Shutting down cce ipam controller for roce")

	nodeInformer := ipam.kubeInformer.Core().V1().Nodes().Informer()
	podInformer := ipam.kubeInformer.Core().V1().Pods().Informer()
	mwepInformer := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Informer()

	ipam.kubeInformer.Start(stopCh)
	ipam.crdInformer.Start(stopCh)

	if !cache.WaitForNamedCacheSync(
		"cce-ipam",
		stopCh,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		mwepInformer.HasSynced,
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
	ipam.cacheHasSynced = true

	go func() {
		if err := ipam.syncHpcENI(stopCh); err != nil {
			log.Errorf(ctx, "failed to sync eni info: %v", err)
		}
	}()

	go func() {
		if err := ipam.gc(stopCh); err != nil {
			log.Errorf(ctx, "failed to start ipam gc: %v", err)
		}
	}()

	// k8s resource and ip cache are synced
	ipam.cacheHasSynced = true
	log.Infof(ctx, "ipam cacheHasSynced is: %v", ipam.cacheHasSynced)

	<-stopCh
	return nil
}

func (ipam *IPAM) DeleteNodeFromCache(node *v1.Node) error {
	instanceId, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		return fmt.Errorf("warning: cannot get instanceID of node %s", node.Name)
	}
	if _, exist := ipam.nodeCache[instanceId]; exist {
		delete(ipam.nodeCache, node.Name)
	}
	return nil
}

func (ipam *IPAM) syncHpcENI(stopCh <-chan struct{}) error {
	ctx := log.NewContext()
	//hpcEniSyncInterval := wait.Jitter(ipam.eniSyncPeriod, 0.2)
	hpcEniSyncInterval := 2 * time.Hour
	log.Infof(ctx, "roce ipam eniSyncPeriod is %v", hpcEniSyncInterval)

	err := wait.PollImmediateUntil(hpcEniSyncInterval, func() (bool, error) {
		roceNodeSelector, err := roceNodeListerSelector()
		if err != nil {
			log.Errorf(ctx, "error parsing requirement: %v", err)
			return false, nil
		}
		// list nodes whose instance type is Roce
		nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(roceNodeSelector)
		if err != nil {
			log.Errorf(ctx, "failed to list nodes when syncing roce ipam info: %v", err)
			return false, nil
		}

		// build eni cache of bbc nodes
		ipam.buildInstanceIdToNodeNameMap(ctx, nodes)
		if err != nil {
			log.Errorf(ctx, "build instanceId to node name map has error: %v", err)
			return false, nil
		}
		log.Infof(ctx, "node cache count is %v", len(ipam.nodeCache))

		var hpcEni *hpc.EniList
		for instance := range ipam.nodeCache {
			// get hpc eni id
			log.Infof(ctx, "get instanceId is %v in hpcEni interface", instance)
			hpcEni, err = ipam.cloud.GetHPCEniID(ctx, instance)
			if err != nil {
				log.Errorf(ctx, "failed to get hpc eniID when syncing eni, err is : %v", err)
				continue
			}

			log.Infof(ctx, "roce get hpc eniID count is %v", len(hpcEni.Result))
			// check each node, determine whether to add new eni
			err = ipam.buildENIIDCache(instance, hpcEni)
			if err != nil {
				log.Errorf(ctx, "failed to build eniID cache when syncing: %v", err)
			}
		}

		metric.MultiEniMultiIPEniCount.Reset()
		metric.MultiEniMultiIPEniIPCount.Reset()

		return false, nil
	}, stopCh)

	if err != nil {
		return err
	}
	return nil
}

func (ipam *IPAM) buildENIIDCache(instance string, hpcEnis *hpc.EniList) error {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	// build eni cache
	ipam.hpcEniCache = make(map[string][]hpc.Result)
	if hpcEnis.Result != nil || len(hpcEnis.Result) != 0 {
		ipam.hpcEniCache[instance] = hpcEnis.Result
	}

	return nil
}

func (ipam *IPAM) buildInstanceIdToNodeNameMap(ctx context.Context, nodes []*v1.Node) {
	log.Infof(ctx, "start buildInstanceIdToNodeNameMap")

	instanceIdToNodeNameMap := make(map[string]*v1.Node, len(nodes))
	for _, n := range nodes {
		instanceId, err := util.GetInstanceIDFromNode(n)
		if err != nil {
			//不阻塞创建
			log.Warningf(ctx, "warning: cannot get instanceID of node %v", n.Name)
			continue
		}
		instanceIdToNodeNameMap[instanceId] = n
	}
	ipam.nodeCache = instanceIdToNodeNameMap
	log.Infof(ctx, "build instanceId to node name map success %v", len(ipam.nodeCache))
}

func (ipam *IPAM) buildAllocatedNodeCache(ctx context.Context) error {
	var (
		wg sync.WaitGroup
		ch = make(chan struct{}, 10)
	)

	roceNodeSelector, _ := roceNodeListerSelector()
	nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(roceNodeSelector)
	if err != nil {
		log.Errorf(ctx, "failed to list roce nodes: %v", err)
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

			err = ipam.rebuildNodeInfoCache(ctx, node, instanceID)
			if err != nil {
				log.Errorf(ctx, "rebuild cache for node %v error: %v", node.Name, err)
			}
		}(node)
	}

	wg.Wait()

	return nil
}

func (ipam *IPAM) rebuildNodeInfoCache(ctx context.Context, node *v1.Node, instanceID string) error {
	var hpcEnis *hpc.EniList

	// list eni with backoff
	err := wait.ExponentialBackoff(retry.DefaultBackoff, func() (bool, error) {
		var err error
		log.Infof(ctx, "rebuild Node Info Cache instanceid is %v", instanceID)
		hpcEnis, err = ipam.cloud.GetHPCEniID(ctx, instanceID)
		if err != nil {
			log.Errorf(ctx, "failed to get hpc eniID to instance %s: %v", instanceID, err)
			return false, err
		}
		log.Infof(ctx, "hpceni info is %s,len HpcEniResult is %d", instanceID, len(hpcEnis.Result))
		return true, nil
	})
	if err != nil {
		log.Errorf(ctx, "failed to backoff list enis attached to instance %s: %v", instanceID, err)
		return err
	}

	ipam.lock.Lock()
	defer ipam.lock.Unlock()
	// add ip to store
	ipam.hpcEniCache[instanceID] = hpcEnis.Result
	//add node instance
	ipam.nodeCache[instanceID] = node
	return nil
}

func mwepListerSelector() (labels.Selector, error) {
	// for mwep owned by bcc, use selector "node.kubernetes.io/instance-type", to be compatible with old versions.
	requireInstanceType, insErr := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BCC", "BBC", "GPU", "DCC"})
	if insErr != nil {
		return nil, insErr
	}
	requireMwepType, typeErr := labels.NewRequirement(ipamgeneric.MwepLabelInstanceTypeKey, selection.Equals,
		[]string{ipamgeneric.MwepTypeRoce})
	if typeErr != nil {
		return nil, typeErr
	}
	return labels.NewSelector().Add(*requireInstanceType, *requireMwepType), nil
}

func (ipam *IPAM) buildAllocatedCache(ctx context.Context) error {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	selector, err := mwepListerSelector()
	if err != nil {
		log.Errorf(ctx, "mwep lister selector has error: %v", err)
		return err
	}

	mwepList, err := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().List(selector)
	if err != nil {
		return err
	}
	for _, mwep := range mwepList {
		nwep := mwep.DeepCopy()
		for _, spec := range mwep.Spec {
			ipam.allocated[spec.IP] = nwep
			log.Infof(ctx, "build allocated mwep cache: found IP %v assigned to pod (%v %v)", spec.IP, mwep.Namespace, mwep.Name)
		}
	}
	return nil
}

func roceNodeListerSelector() (labels.Selector, error) {
	requirement, err := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BCC", "BBC", "GPU", "DCC"})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}
