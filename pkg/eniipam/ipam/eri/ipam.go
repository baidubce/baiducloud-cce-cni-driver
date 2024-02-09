package eri

import (
	"context"
	goerrors "errors"
	"fmt"
	"sync"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/scheme"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
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
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

const (
	syncNodePeriod = 2 * time.Hour
	cloudMaxTry    = 3
	// minPrivateIPLifeTime is the life time of a private ip (from allocation to release), aim to trade off db slave delay
	minPrivateIPLifeTime       = 5 * time.Second
	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5
)

func NewIPAM(
	vpcID string,
	kubeClient kubernetes.Interface,
	crdClient versioned.Interface,
	bceClient cloud.Interface,
	informerResyncPeriod time.Duration,
	gcPeriod time.Duration,
) (ipamgeneric.RoceInterface, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-eri-ipam"})

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, informerResyncPeriod)
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, informerResyncPeriod)

	ipam := &IPAM{
		vpcID:          vpcID,
		eventRecorder:  recorder,
		kubeInformer:   kubeInformer,
		kubeClient:     kubeClient,
		crdInformer:    crdInformer,
		crdClient:      crdClient,
		gcPeriod:       gcPeriod,
		cloud:          bceClient,
		cacheHasSynced: false,
		nodeCache:      make(map[string]*corev1.Node),
	}
	return ipam, nil
}

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string,
	mac string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Allocate] allocating IP for eri pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating IP for eri pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "eri ipam has not synced cache yet")
		return nil, fmt.Errorf("eri ipam has not synced cache yet")
	}

	node, nodeErr := ipam.getNodeByPodName(ctx, namespace, name)
	if nodeErr != nil {
		log.Errorf(ctx, "get node for pod (%s/%s) error: %v", namespace, name, nodeErr)
		return nil, nodeErr
	}
	nodeName := node.Name

	instanceID, insIDErr := util.GetInstanceIDFromNode(node)
	if insIDErr != nil {
		log.Errorf(ctx, "get instanceID for pod (%s/%s) error: %v", namespace, name, insIDErr)
		return nil, insIDErr
	}
	log.Infof(ctx, "instanceID for pod (%s/%s) is %s ", namespace, name, instanceID)

	// add node to cache if not in the cache
	if _, exist := ipam.nodeCache[instanceID]; !exist {
		ipam.nodeCache[instanceID] = node
	}

	eriInfo, err := ipam.findMatchedEriByMac(ctx, instanceID, mac)
	if err != nil {
		log.Errorf(ctx, "failed to find a suitable eri by mac for pod (%v %v) in eri ipam: %v", namespace, name, err)
		return nil, err
	}
	log.Infof(ctx, "find eniID is %s ", eriInfo.EniId)

	ipam.lock.Lock()
	defer ipam.lock.Unlock()
	mwep, err := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().MultiIPWorkloadEndpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return ipam.allocateFirstIP(ctx, namespace, name, eriInfo.EniId, nodeName, instanceID, containerID, mac)
		}
		log.Errorf(ctx, "get mwep for pod (%v/%v) error: %v", namespace, name, err)
		return nil, err
	}

	// not first allocate ip for pod
	if mwep.NodeName != nodeName {
		msg := fmt.Sprintf("mwep node name: %s, not match current node in eri ipam: %s",
			mwep.NodeName, nodeName)
		log.Warningf(ctx, msg)
		return nil, fmt.Errorf(msg)
	}

	return ipam.allocateOtherIP(ctx, mwep, eriInfo.EniId, containerID, mac)
}

// find eri by mac address. Return matched eri and eri list of the instanceID
func (ipam *IPAM) findMatchedEriByMac(ctx context.Context, instanceID string, macAddress string) (*enisdk.Eni, error) {
	log.Infof(ctx, "start to find suitable eri by mac for instanceID %v/%v", instanceID, macAddress)
	listArgs := enisdk.ListEniArgs{
		InstanceId: instanceID,
		VpcId:      ipam.vpcID,
	}
	eriList, listErr := ipam.cloud.ListERIs(ctx, listArgs)
	if listErr != nil {
		log.Errorf(ctx, "failed to get eri: %v", listErr)
		return nil, listErr
	}

	for index := range eriList {
		eriInfo := eriList[index]
		if eriInfo.MacAddress == macAddress {
			return &eriInfo, nil
		}
	}

	log.Errorf(ctx, "macAddress %s mismatch, eriList: %v", macAddress, eriList)
	return nil, fmt.Errorf("macAddress %s mismatch, eriList: %v", macAddress, eriList)
}

// allocate first ip for pod, and create mwep
func (ipam *IPAM) allocateFirstIP(ctx context.Context, namespace, name, eniID, nodeName,
	instanceID, containerID, mac string) (*v1alpha1.WorkloadEndpoint, error) {
	// 1. allocate ip
	ipResult, ipErr := ipam.tryAllocateIP(ctx, namespace, name, eniID)
	if ipErr != nil {
		msg := fmt.Sprintf("error allocate private IP for pod (%s/%s): %s", namespace, name, ipErr)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	log.Infof(ctx, "eri ipam allocate ip for pod (%s/%s) success, allocate ip result %s ", namespace, name, ipResult)

	// 2. create mwep
	mwep := &v1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{ipamgeneric.MwepFinalizer},
			Labels: map[string]string{
				ipamgeneric.MwepLabelInstanceTypeKey: ipamgeneric.MwepTypeERI,
				v1.LabelInstanceType:                 "BCC",
			},
		},
		NodeName:   nodeName,
		Type:       ipamgeneric.MwepTypeERI,
		InstanceID: instanceID,
		Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
			{
				EniID:       eniID,
				ContainerID: containerID,
				IP:          ipResult,
				Mac:         mac,
				UpdateAt:    metav1.Time{Time: time.Now()},
			},
		},
	}

	_, createErr := ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(namespace).Create(ctx, mwep, metav1.CreateOptions{})
	if createErr != nil {
		// rollback
		log.Errorf(ctx, "create mwep for pod (%s/%s) error: %v", namespace, name, createErr)
		time.Sleep(minPrivateIPLifeTime)

		if delErr := ipam.tryDeleteIP(ctx, mwep.Namespace, mwep.Name, eniID, ipResult); delErr != nil {
			log.Errorf(ctx, "deleted private ip %s for pod (%s/%s) error: %v",
				ipResult, namespace, name, delErr)
			return nil, delErr
		}
		return nil, createErr
	}
	log.Infof(ctx, "create mwep %v for pod (%s/%s) success", mwep.Spec, namespace, name)

	return &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ENIID:       eniID,
			ContainerID: containerID,
			IP:          ipResult,
			Mac:         mac,
		},
	}, nil
}

// allocate ip then update mwep
func (ipam *IPAM) allocateOtherIP(ctx context.Context, mwep *v1alpha1.MultiIPWorkloadEndpoint,
	eniID, containerID, mac string) (*v1alpha1.WorkloadEndpoint, error) {
	if mwep == nil {
		return nil, fmt.Errorf("mwep required")
	}
	if mwep.Type != ipamgeneric.MwepTypeERI {
		msg := fmt.Sprintf("mwep %s/%s type is %s, not eri", mwep.Namespace, mwep.Name, mwep.Type)
		log.Warning(ctx, msg)
		return nil, fmt.Errorf(msg)
	}

	var oldMwepSpec *v1alpha1.MultiIPWorkloadEndpointSpec
	for i := range mwep.Spec {
		tmpMwepSpec := mwep.Spec[i]
		if tmpMwepSpec.EniID == eniID {
			oldMwepSpec = &tmpMwepSpec
			break
		}
	}
	// 1. return wep info if mwep contains info of the eniID
	if oldMwepSpec != nil {
		return &v1alpha1.WorkloadEndpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mwep.Name,
				Namespace: mwep.Namespace,
			},
			Spec: v1alpha1.WorkloadEndpointSpec{
				ENIID:       eniID,
				ContainerID: containerID,
				IP:          oldMwepSpec.IP,
				Mac:         oldMwepSpec.Mac,
			},
		}, nil
	}
	// 2. allocate ip for the eniID
	ipResult, ipErr := ipam.tryAllocateIP(ctx, mwep.Namespace, mwep.Name, eniID)
	if ipErr != nil {
		msg := fmt.Sprintf("error allocate private IP for pod (%s/%s): %s", mwep.Namespace, mwep.Name, ipErr)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}
	log.Infof(ctx, "eri ipam allocate ip for pod (%s/%s) success, allocate ip result %s ",
		mwep.Namespace, mwep.Name, ipResult)

	// append to mwep
	newMwepSpec := v1alpha1.MultiIPWorkloadEndpointSpec{
		IP:          ipResult,
		EniID:       eniID,
		Mac:         mac,
		ContainerID: containerID,
		UpdateAt:    metav1.Time{Time: time.Now()},
	}

	mwep.Spec = append(mwep.Spec, newMwepSpec)
	log.Infof(ctx, "new specList count is %d", len(mwep.Spec))

	// 3. update mwep
	_, updateErr := ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(mwep.Namespace).Update(
		ctx, mwep, metav1.UpdateOptions{})
	if updateErr != nil {
		// rollback
		log.Errorf(ctx, "update mwep for pod (%s/%s) error: %s", mwep.Namespace, mwep.Name, updateErr)
		time.Sleep(minPrivateIPLifeTime)

		if delErr := ipam.tryDeleteIP(ctx, mwep.Namespace, mwep.Name, eniID, ipResult); delErr != nil {
			log.Errorf(ctx, "deleted private ip %s for pod (%v/%v) error: %v",
				ipResult, mwep.Namespace, mwep.Name, delErr)
			return nil, delErr
		}
		return nil, updateErr
	}

	return &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwep.Name,
			Namespace: mwep.Namespace,
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ENIID:       eniID,
			ContainerID: containerID,
			IP:          ipResult,
			Mac:         mac,
		},
	}, nil
}

func (ipam *IPAM) tryAllocateIP(ctx context.Context, namespace, podName, eniID string) (string, error) {
	log.Infof(ctx, "start allocate IP for pod %s/%s, eniID: %s",
		namespace, podName, eniID)

	for i := 0; i < cloudMaxTry; i++ {
		log.Infof(ctx, "allocate IP max try time is %d, now is %d time", cloudMaxTry, i)

		ipResult, err := ipam.cloud.AddPrivateIP(ctx, "", eniID)
		if err == nil {
			log.Infof(ctx, "add private IP %s for pod (%s/%s) success", ipResult, namespace, podName)
			metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, eniID).Inc()
			return ipResult, nil
		}

		if cloud.IsErrorRateLimit(err) {
			// retry
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		} else {
			log.Errorf(ctx, "error add privateIP in eniID %v for pod %v/%v: %v", eniID, namespace, podName, err)
			return "", err
		}
	}
	return "", fmt.Errorf("allocate IP failed, retry count exceeded")
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Release] releasing IP for eri pod (%v/%v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing IP for eri pod (%v/%v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "release: eri ipamd has not synced cache yet")
		return nil, fmt.Errorf("release: eri ipamd has not synced cache yet")
	}

	//  mwep, avoid data racing
	tmpMwep, mwepErr := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().
		MultiIPWorkloadEndpoints(namespace).Get(name)
	if mwepErr != nil {
		log.Errorf(ctx, "release: get mwep of pod (%v/%v) failed: %v", namespace, name, mwepErr)
		return nil, mwepErr
	}
	mwep := tmpMwep.DeepCopy()

	//  delete ip, delete mwep crd
	releaseErr := ipam.releaseIPByMwep(ctx, mwep)
	if releaseErr != nil {
		return nil, releaseErr
	}
	// This API doesn't care about response body
	wep := &v1alpha1.WorkloadEndpoint{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwep.Name,
			Namespace: mwep.Namespace,
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ContainerID: containerID,
			Node:        mwep.NodeName,
			InstanceID:  mwep.InstanceID,
		},
	}
	return wep, nil
}

// delete ip, delete mwep
func (ipam *IPAM) releaseIPByMwep(ctx context.Context, mwep *v1alpha1.MultiIPWorkloadEndpoint) error {
	namespace := mwep.Namespace
	name := mwep.Name
	for _, spec := range mwep.Spec {
		ipErr := ipam.tryDeleteIP(ctx, namespace, name, spec.EniID, spec.IP)
		if ipErr != nil {
			log.Errorf(ctx, "release: delete private IP %s for pod (%s/%s) failed: %v", spec.IP, namespace, name, ipErr)
		} else {
			log.Infof(ctx, "release: delete private IP %s for pod (%s/%s) success", spec.IP, namespace, name)
		}
	}

	deleteErr := ipam.tryDeleteMwep(ctx, mwep)
	if deleteErr != nil {
		log.Errorf(ctx, "release: delete mwep %s/%s error: %v", namespace, name, deleteErr)
		return deleteErr
	}
	log.Infof(ctx, "release: delete mwep %s/%s success", namespace, name)
	return nil
}

func (ipam *IPAM) tryDeleteIP(ctx context.Context, namespace, podName, eniID, privateIP string) error {
	log.Infof(ctx, "start delete private IP %s for pod %s/%s, eniID: %s",
		privateIP, namespace, podName, eniID)

	for i := 0; i < cloudMaxTry; i++ {
		log.Infof(ctx, "delete private IP %s max try time is %d, now is %d time", privateIP, cloudMaxTry, i)

		err := ipam.cloud.DeletePrivateIP(ctx, privateIP, eniID)
		if err == nil {
			log.Infof(ctx, "delete private IP %s for pod (%s/%s) success", privateIP, namespace, podName)
			metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, eniID).Dec()
			return nil
		}

		if cloud.IsErrorRateLimit(err) {
			// retry
			time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
		} else {
			log.Errorf(ctx, "delete private IP %s for pod (%s/%s) failed: %s", privateIP, namespace, podName, err)
			return err
		}
	}
	return fmt.Errorf("delete IP failed, retry count exceeded")
}

// Delete workload objects from the k8s cluster
func (ipam *IPAM) tryDeleteMwep(ctx context.Context, mwep *v1alpha1.MultiIPWorkloadEndpoint) (err error) {
	// remove finalizers
	mwep.Finalizers = nil
	_, err = ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(mwep.Namespace).Update(ctx, mwep, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf(ctx, "tryDeleteWep failed to update wep for pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
		return err
	}
	// delete mwep
	if err := ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(mwep.Namespace).
		Delete(ctx, mwep.Name, *metav1.NewDeleteOptions(0)); err != nil {
		log.Errorf(ctx, "tryDeleteMwep failed to delete wep for orphaned pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
	} else {
		log.Infof(ctx, "tryDeleteMwep delete wep for orphaned pod (%v %v) successfully", mwep.Namespace, mwep.Name)
	}
	return nil
}

func (ipam *IPAM) Run(ctx context.Context, stopCh <-chan struct{}) error {
	defer func() {
		runtime.HandleCrash()
	}()

	log.Info(ctx, "Starting cce ipam controller for eri")
	defer log.Info(ctx, "Shutting down cce ipam controller for eri")

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
	}
	log.Info(ctx, "WaitForCacheSync done")

	// build node cache
	nodeErr := ipam.buildNodeCache(ctx)
	if nodeErr != nil {
		return nodeErr
	}
	ipam.cacheHasSynced = true

	go func() {
		if err := ipam.syncNode(stopCh); err != nil {
			log.Errorf(ctx, "failed to sync node info: %v", err)
		}
	}()

	go func() {
		if err := ipam.gc(stopCh); err != nil {
			log.Errorf(ctx, "failed to start ipam gc: %v", err)
		}
	}()

	// k8sr resource are synced
	ipam.cacheHasSynced = true
	log.Infof(ctx, "ipam cacheHasSynced is: %v", ipam.cacheHasSynced)

	<-stopCh
	return nil
}

func (ipam *IPAM) syncNode(stopCh <-chan struct{}) error {
	ctx := log.NewContext()

	err := wait.PollImmediateUntil(syncNodePeriod, func() (bool, error) {
		return false, ipam.buildNodeCache(ctx)
	}, stopCh)

	if err != nil {
		return err
	}
	return nil
}

func (ipam *IPAM) buildNodeCache(ctx context.Context) error {
	var (
		wg sync.WaitGroup
		ch = make(chan struct{}, 10)
	)

	nodeSelector, _ := nodeListerSelector()
	nodes, err := ipam.kubeInformer.Core().V1().Nodes().Lister().List(nodeSelector)
	if err != nil {
		log.Errorf(ctx, "failed to list nodes: %v", err)
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

			instanceID, err := util.GetInstanceIDFromNode(node)
			if err != nil {
				return
			}

			ipam.lock.Lock()
			defer ipam.lock.Unlock()
			//add node instance
			ipam.nodeCache[instanceID] = node
		}(node)
	}

	wg.Wait()

	return nil
}

func nodeListerSelector() (labels.Selector, error) {
	requirement, err := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BCC", "GPU", "DCC"})
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}

func mwepListerSelector() (labels.Selector, error) {
	// for mwep owned by bcc, use selector "node.kubernetes.io/instance-type", to be compatible with old versions.
	requireInstanceType, insErr := labels.NewRequirement(v1.LabelInstanceType, selection.In, []string{"BCC", "GPU", "DCC"})
	if insErr != nil {
		return nil, insErr
	}
	requireMwepType, typeErr := labels.NewRequirement(ipamgeneric.MwepLabelInstanceTypeKey, selection.Equals,
		[]string{ipamgeneric.MwepTypeERI})
	if typeErr != nil {
		return nil, typeErr
	}
	return labels.NewSelector().Add(*requireInstanceType, *requireMwepType), nil
}

func (ipam *IPAM) Ready(_ context.Context) bool {
	return ipam.cacheHasSynced
}

func (ipam *IPAM) getNodeByPodName(ctx context.Context, namespace, podName string) (*corev1.Node, error) {
	pod, podErr := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(podName)
	if podErr != nil {
		log.Errorf(ctx, "get pod (%s/%s) error: %v", namespace, podName, podErr)
		return nil, podErr
	}

	return ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
}
