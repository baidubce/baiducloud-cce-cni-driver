package rdma

import (
	"context"
	goerrors "errors"
	"fmt"
	"strings"
	"time"

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

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/rdma/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	// minPrivateIPLifeTime is the lifetime of a private ip (from allocation to release), aim to trade off db slave delay
	minPrivateIPLifeTime = 5 * time.Second

	rateLimitErrorSleepPeriod  = time.Millisecond * 200
	rateLimitErrorJitterFactor = 5

	cloudMaxTry = 3
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
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-roce-ipam"})

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, informerResyncPeriod)
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, informerResyncPeriod)

	eriClient := client.NewEriClient(bceClient)
	roceClient := client.NewRoCEClient(bceClient)

	ipam := &IPAM{
		vpcID:         vpcID,
		eventRecorder: recorder,
		kubeInformer:  kubeInformer,
		kubeClient:    kubeClient,
		crdInformer:   crdInformer,
		crdClient:     crdClient,
		gcPeriod:      gcPeriod,

		eriClient:  eriClient,
		roceClient: roceClient,

		cacheHasSynced: false,
	}
	return ipam, nil
}

func (ipam *IPAM) getIaaSClientByIPType(ipType rpc.IPType) client.IaaSClient {
	if ipType == rpc.IPType_ERIENIMultiIPType {
		return ipam.eriClient
	}
	return ipam.roceClient
}

func (ipam *IPAM) getIaaSClient(mwepType string) client.IaaSClient {
	if strings.EqualFold(mwepType, ipam.eriClient.GetMwepType()) {
		return ipam.eriClient
	}
	return ipam.roceClient
}

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string,
	mac string, ipType rpc.IPType) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Allocate] allocating IP for roce pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating IP for roce pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "roce ipam has not synced cache yet")
		return nil, fmt.Errorf("roce ipam has not synced cache yet")
	}

	// 1. prepare data
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

	iaasClient := ipam.getIaaSClientByIPType(ipType)

	// 2. find eni and wep
	matchedEni, err := ipam.findMatchedEniByMac(ctx, iaasClient, instanceID, mac)
	if err != nil {
		log.Errorf(ctx, "failed to find a suitable eni by mac for pod (%v %v) in roce ipam: %v", namespace, name, err)
		return nil, err
	}
	log.Infof(ctx, "find eniID is %s ", matchedEni.EniID)

	//	ipam.lock.Lock()
	//	defer ipam.lock.Unlock()
	mwep, err := ipam.getMwep(ctx, namespace, name, nodeName)
	if err != nil {
		if errors.IsNotFound(err) {
			// 3. First time allocate ip for the pod
			return ipam.allocateFirstIP(ctx, iaasClient, namespace, name, matchedEni.EniID, nodeName, instanceID,
				containerID, mac)
		}
		log.Errorf(ctx, "get mwep for pod (%v/%v) error: %v", namespace, name, err)
		return nil, err
	}

	// // 4. not first time allocate ip for the pod
	return ipam.allocateOtherIP(ctx, iaasClient, mwep, matchedEni.EniID, containerID, mac)
}

func (ipam *IPAM) getNodeByPodName(ctx context.Context, namespace, podName string) (*corev1.Node, error) {
	pod, podErr := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(podName)
	if podErr != nil {
		log.Errorf(ctx, "get pod (%s/%s) error: %v", namespace, podName, podErr)
		return nil, podErr
	}

	return ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
}

// find eni by mac address, return matched eni.
func (ipam *IPAM) findMatchedEniByMac(ctx context.Context, iaasClient client.IaaSClient, instanceID string,
	macAddress string) (*client.EniResult, error) {
	log.Infof(ctx, "start to find suitable eni by mac for instanceID %v/%v", instanceID, macAddress)
	eniList, listErr := iaasClient.ListEnis(ctx, ipam.vpcID, instanceID)
	if listErr != nil {
		log.Errorf(ctx, "failed to get eni: %v", listErr)
		return nil, listErr
	}

	for index := range eniList {
		eniInfo := eniList[index]
		if strings.EqualFold(eniInfo.MacAddress, macAddress) {
			return &eniInfo, nil
		}
	}

	log.Errorf(ctx, "macAddress %s mismatch, eniList: %v", macAddress, eniList)
	return nil, fmt.Errorf("macAddress %s mismatch, eniList: %v", macAddress, eniList)
}

// getMwep and delete leaked mwep
func (ipam *IPAM) getMwep(ctx context.Context, namespace, name, nodeName string) (*v1alpha1.MultiIPWorkloadEndpoint, error) {
	mwep, getErr := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().
		MultiIPWorkloadEndpoints(namespace).Get(name)
	if getErr != nil {
		return nil, getErr
	}

	if mwep.NodeName == nodeName {
		return mwep, nil
	}

	// it's a leaked mwep, need to delete.
	deleteErr := ipam.tryDeleteMwep(ctx, mwep)
	if deleteErr != nil {
		return nil, deleteErr
	}
	return nil, errors.NewNotFound(v1alpha1.Resource("multiipworkloadendpoint"), name)
}

// allocate first ip for pod, and create mwep
func (ipam *IPAM) allocateFirstIP(ctx context.Context, iaasClient client.IaaSClient, namespace, name, eniID, nodeName,
	instanceID, containerID, mac string) (*v1alpha1.WorkloadEndpoint, error) {
	// 1. allocate ip
	ipResult, ipErr := ipam.tryAllocateIP(ctx, iaasClient, namespace, name, eniID)
	if ipErr != nil {
		msg := fmt.Sprintf("error allocate private IP for pod (%s/%s): %s", namespace, name, ipErr)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	log.Infof(ctx, "roce ipam allocate ip for pod (%s/%s) success, allocate ip result %s ",
		namespace, name, ipResult)

	// 2. create mwep
	// 2.1 prepare mwep
	mwep := &v1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{ipamgeneric.MwepFinalizer},
		},
		NodeName:    nodeName,
		InstanceID:  instanceID,
		ContainerID: containerID,
		Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
			{
				EniID:       eniID,
				ContainerID: containerID,
				IP:          ipResult,
				Mac:         mac,
				UpdateAt:    metav1.Time{Time: time.Now()},
				Type:        iaasClient.GetMwepType(),
			},
		},
	}

	// 2.2 create mwep
	_, createErr := ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(namespace).Create(ctx, mwep, metav1.CreateOptions{})
	if createErr != nil {
		// 2.3 rollback ip
		log.Errorf(ctx, "create mwep for pod (%s/%s) error: %v", namespace, name, createErr)
		time.Sleep(minPrivateIPLifeTime)

		if delErr := ipam.tryDeleteIP(ctx, iaasClient, mwep.Namespace, mwep.Name, eniID, ipResult); delErr != nil {
			log.Errorf(ctx, "deleted private ip %s for pod (%s/%s) error: %v",
				ipResult, namespace, name, delErr)
			return nil, delErr
		}
		return nil, createErr
	}
	log.Infof(ctx, "create mwep %v for pod (%s/%s) success", mwep.Spec, namespace, name)

	// 3. for response
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
			Type:        iaasClient.GetMwepType(),
		},
	}, nil
}

func (ipam *IPAM) tryAllocateIP(ctx context.Context, iaasClient client.IaaSClient, namespace, podName,
	eniID string) (string, error) {
	log.Infof(ctx, "start allocate IP for pod %s/%s, eniID: %s",
		namespace, podName, eniID)

	for i := 0; i < cloudMaxTry; i++ {
		log.Infof(ctx, "allocate IP max try time is %d, now is %d time", cloudMaxTry, i)

		ipResult, err := iaasClient.AddPrivateIP(ctx, eniID, "")
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

// allocate ip then update mwep
func (ipam *IPAM) allocateOtherIP(ctx context.Context, iaasClient client.IaaSClient,
	mwep *v1alpha1.MultiIPWorkloadEndpoint, eniID, containerID, mac string) (*v1alpha1.WorkloadEndpoint, error) {
	var newWepSpec *v1alpha1.MultiIPWorkloadEndpointSpec
	var newErr error
	var newIP bool

	// 1. if exists spec with the eni, then use the old ip
	newWepSpec = ipam.existMwepSpecForEni(mwep, eniID, containerID)

	if newWepSpec != nil {
		newIP = false
	} else {
		// 2. allocate new ip
		newWepSpec, newErr = ipam.allocateNewIP(ctx, iaasClient, mwep, eniID, containerID, mac)
		if newErr != nil {
			return nil, newErr
		}
		newIP = true
	}

	// 3.2 update containerID
	mwep.ContainerID = containerID

	// 3.3 update mwep
	_, updateErr := ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(mwep.Namespace).Update(
		ctx, mwep, metav1.UpdateOptions{})
	if updateErr != nil {
		log.Errorf(ctx, "update mwep for pod (%s/%s) error: %s", mwep.Namespace, mwep.Name, updateErr)

		if newIP {
			// 3.4 rollback
			time.Sleep(minPrivateIPLifeTime)

			if delErr := ipam.tryDeleteIP(ctx, iaasClient, mwep.Namespace, mwep.Name, eniID,
				newWepSpec.IP); delErr != nil {
				log.Errorf(ctx, "deleted private ip %s for pod (%v/%v) error: %v",
					newWepSpec.IP, mwep.Namespace, mwep.Name, delErr)
				return nil, delErr
			}
		}
		return nil, updateErr
	}

	// 4. return wep
	return &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwep.Name,
			Namespace: mwep.Namespace,
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ENIID:       eniID,
			ContainerID: containerID,
			IP:          newWepSpec.IP,
			Mac:         mac,
		},
	}, nil
}

// existMwepSpecForEni already exist a spec with the eni
func (ipam *IPAM) existMwepSpecForEni(mwep *v1alpha1.MultiIPWorkloadEndpoint, eniID,
	containerID string) *v1alpha1.MultiIPWorkloadEndpointSpec {
	existIdx := -1
	for i := range mwep.Spec {
		tmpMwepSpec := mwep.Spec[i]
		if tmpMwepSpec.EniID == eniID {
			mwep.Spec[i].ContainerID = containerID
			existIdx = i
			break
		}
	}

	if existIdx == -1 {
		return nil
	}
	return &mwep.Spec[existIdx]
}

func (ipam *IPAM) allocateNewIP(ctx context.Context, iaasClient client.IaaSClient,
	mwep *v1alpha1.MultiIPWorkloadEndpoint, eniID, containerID, mac string) (*v1alpha1.MultiIPWorkloadEndpointSpec, error) {
	ipResult, ipErr := ipam.tryAllocateIP(ctx, iaasClient, mwep.Namespace, mwep.Name, eniID)
	if ipErr != nil {
		msg := fmt.Sprintf("error allocate private IP for pod (%s/%s): %s", mwep.Namespace, mwep.Name, ipErr)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}
	log.Infof(ctx, "roce ipam allocate ip for pod (%s/%s) success, allocate ip result %s ",
		mwep.Namespace, mwep.Name, ipResult)

	// 3. update mwep
	// 3.1 append new spec to mwep
	newMwepSpec := v1alpha1.MultiIPWorkloadEndpointSpec{
		IP:          ipResult,
		EniID:       eniID,
		Mac:         mac,
		ContainerID: containerID,
		UpdateAt:    metav1.Time{Time: time.Now()},
		Type:        iaasClient.GetMwepType(),
	}

	mwep.Spec = append(mwep.Spec, newMwepSpec)
	log.Infof(ctx, "new specList count is %d", len(mwep.Spec))
	return &newMwepSpec, nil
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Release] releasing IP for roce pod (%v/%v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing IP for roce pod (%v/%v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "release: roce ipamd has not synced cache yet")
		return nil, fmt.Errorf("release: roce ipamd has not synced cache yet")
	}

	//  mwep, avoid data racing
	tmpMwep, mwepErr := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().
		MultiIPWorkloadEndpoints(namespace).Get(name)
	if mwepErr != nil {
		log.Errorf(ctx, "release: get mwep of pod (%v/%v) failed: %v", namespace, name, mwepErr)
		return nil, mwepErr
	}
	mwep := tmpMwep.DeepCopy()

	if err := ipam.validateContainerID(mwep, containerID); err != nil {
		log.Errorf(ctx, "release: %v", err)
		return nil, err
	}

	//  delete ip, delete mwep crd
	releaseErr := ipam.releaseIPByMwep(ctx, mwep)
	if releaseErr != nil {
		return nil, releaseErr
	}
	// This API doesn't care about response body
	return &v1alpha1.WorkloadEndpoint{
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
	}, nil
}

func (ipam *IPAM) validateContainerID(mwep *v1alpha1.MultiIPWorkloadEndpoint, containerID string) error {
	if mwep == nil {
		return nil
	}

	if len(mwep.ContainerID) == 0 {
		return nil
	}
	if strings.EqualFold(mwep.ContainerID, containerID) {
		return nil
	}
	return fmt.Errorf("containerID not match, expect: %s, actual: %s", mwep.ContainerID, containerID)
}

func (ipam *IPAM) releaseIPByMwep(ctx context.Context, mwep *v1alpha1.MultiIPWorkloadEndpoint) error {
	namespace := mwep.Namespace
	name := mwep.Name
	defaultType := mwep.Type
	//  delete eni ip, delete mwep crd
	for _, spec := range mwep.Spec {
		ipType := defaultType
		if len(spec.Type) > 0 {
			ipType = spec.Type
		}
		iaasClient := ipam.getIaaSClient(ipType)

		ipErr := ipam.tryDeleteIP(ctx, iaasClient, namespace, name, spec.EniID, spec.IP)
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

func (ipam *IPAM) tryDeleteIP(ctx context.Context, iaasClient client.IaaSClient,
	namespace, podName, eniID, privateIP string) error {
	log.Infof(ctx, "start delete private IP %s for pod %s/%s, eniID: %s",
		privateIP, namespace, podName, eniID)

	for i := 0; i < cloudMaxTry; i++ {
		log.Infof(ctx, "delete private IP %s max try time is %d, now is %d time", privateIP, cloudMaxTry, i)

		err := iaasClient.DeletePrivateIP(ctx, eniID, privateIP)
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
	}
	log.Info(ctx, "WaitForCacheSync done")
	ipam.cacheHasSynced = true

	go func() {
		if err := ipam.gc(stopCh); err != nil {
			log.Errorf(ctx, "failed to start ipam gc: %v", err)
		}
	}()

	log.Infof(ctx, "ipam cacheHasSynced is: %v", ipam.cacheHasSynced)

	<-stopCh
	return nil
}

// rdma-device-plugin node selector:
//
//	feature.node.kubernetes.io/custom-rdma.available: "true"
//	feature.node.kubernetes.io/custom-rdma.capable: "true"
func (ipam *IPAM) nodeListerSelector() (labels.Selector, error) {
	rdmaAvailable, availableErr := labels.NewRequirement(
		ipamgeneric.RDMANodeLabelAvailableKey, selection.Equals, []string{"true"})
	if availableErr != nil {
		return nil, availableErr
	}

	rdmaCapable, capableErr := labels.NewRequirement(
		ipamgeneric.RDMANodeLabelCapableKey, selection.Equals, []string{"true"})
	if capableErr != nil {
		return nil, capableErr
	}

	return labels.NewSelector().Add(*rdmaAvailable, *rdmaCapable), nil
}
