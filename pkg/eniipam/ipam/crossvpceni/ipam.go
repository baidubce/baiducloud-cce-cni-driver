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

package crossvpceni

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/clock"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/util/event"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/scheme"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

var _ ipamgeneric.ExclusiveEniInterface = &IPAM{}

var (
	defaultBackoff = wait.Backoff{
		Duration: 2 * time.Second,
		Factor:   1,
		Jitter:   0.1,
		Steps:    40,
	}
)

var (
	PodAnnotationCrossVPCEniUserID           = "cross-vpc-eni.cce.io/userID"
	PodAnnotationCrossVPCEniSubnetID         = "cross-vpc-eni.cce.io/subnetID"
	PodAnnotationCrossVPCEniSecurityGroupIDs = "cross-vpc-eni.cce.io/securityGroupIDs"
	PodAnnotationCrossVPCEniPrivateIPAddress = "cross-vpc-eni.cce.io/privateIPAddress"
	PodAnnotationCrossVPCEniVPCCIDR          = "cross-vpc-eni.cce.io/vpcCidr"

	necessaryAnnoKeyList = []string{
		PodAnnotationCrossVPCEniUserID,
		PodAnnotationCrossVPCEniSubnetID,
		PodAnnotationCrossVPCEniVPCCIDR,
		PodAnnotationCrossVPCEniSecurityGroupIDs,
	}

	PodLabelOwnerNamespace = "cce.io/ownerNamespace"
	PodLabelOwnerName      = "cce.io/ownerName"
	PodLabelOwnerNode      = "cce.io/ownerNode"
)

type IPAM struct {
	lock  sync.RWMutex
	debug bool

	cacheHasSynced bool

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	kubeInformer informers.SharedInformerFactory
	kubeClient   kubernetes.Interface

	crdInformer crdinformers.SharedInformerFactory
	crdClient   versioned.Interface

	cloud cloud.Interface
	clock clock.Clock

	cniMode   types.ContainerNetworkMode
	vpcID     string
	clusterID string

	gcPeriod time.Duration
}

func NewIPAM(
	kubeClient kubernetes.Interface,
	crdClient versioned.Interface,
	cniMode types.ContainerNetworkMode,
	vpcID string,
	clusterID string,
	resyncPeriod time.Duration,
	gcPeriod time.Duration,
	debug bool,
) (ipam.ExclusiveEniInterface, error) {
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
		clock:            clock.RealClock{},
		cniMode:          cniMode,
		vpcID:            vpcID,
		clusterID:        clusterID,
		cacheHasSynced:   false,
		gcPeriod:         gcPeriod,
		debug:            debug,
	}
	return ipam, nil
}

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.CrossVPCEni, error) {
	log.Infof(ctx, "[Allocate] allocating eni for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating eni for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	pod, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
	if err != nil {
		return nil, err
	}

	// pod without all crossvpceni annotations should return
	if !podHasCrossVPCEniAnnotation(ctx, pod) {
		return nil, nil
	}

	// node has attaching/detaching eni
	unstable, err := ipam.nodeHasUnstableEni(ctx, pod.Spec.NodeName)
	if err != nil {
		log.Errorf(ctx, "nodeHasUnstableEni error: %v", err)
		return nil, err
	}

	if unstable {
		msg := fmt.Sprintf("node %v has unstable eni", pod.Spec.NodeName)
		log.Error(ctx, msg)
		return nil, errors.New(msg)
	}

	// get spec from annotation
	spec, err := getEniSpecFromPodAnnotations(ctx, pod)
	if err != nil {
		return nil, err
	}

	spec.BoundInstanceID, err = util.GetInstanceIDFromNode(node)
	if err != nil {
		return nil, fmt.Errorf("failed to get node %v instanceId: %v", node.Name, err)
	}

	// create eni cr
	var (
		crossVPCEni = &v1alpha1.CrossVPCEni{
			ObjectMeta: metav1.ObjectMeta{
				Name: containerID,
				Labels: map[string]string{
					PodLabelOwnerNamespace: namespace,
					PodLabelOwnerName:      name,
					PodLabelOwnerNode:      pod.Spec.NodeName,
				},
				Finalizers: []string{ipamgeneric.WepFinalizer},
			},
			Spec: *spec,
		}
	)

	crossVPCEni, err = ipam.crdClient.CceV1alpha1().CrossVPCEnis().Create(ctx, crossVPCEni, metav1.CreateOptions{})
	if err != nil {
		msg := fmt.Sprintf("failed to create crossvpceni for pod (%v %v): %v", namespace, name, err)
		log.Error(ctx, msg)
		return nil, errors.New(msg)
	}

	crossVPCEni.Status = v1alpha1.CrossVPCEniStatus{
		EniStatus:           v1alpha1.EniStatusPending,
		InvolvedContainerID: containerID,
	}
	_, err = ipam.crdClient.CceV1alpha1().CrossVPCEnis().UpdateStatus(ctx, crossVPCEni, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("failed to update status of crossvpceni for pod (%v %v): %v", namespace, name, err)
		log.Error(ctx, msg)
		return nil, errors.New(msg)
	}

	log.Infof(ctx, "create crossvpceni %v for pod (%v %v)", containerID, namespace, name)

	var (
		t      = time.Now()
		eni    *v1alpha1.CrossVPCEni
		errMsg string
	)
	err = wait.ExponentialBackoff(defaultBackoff, func() (done bool, err error) {
		eni, err = ipam.crdInformer.Cce().V1alpha1().CrossVPCEnis().Lister().Get(containerID)
		if err != nil {
			errMsg = fmt.Sprintf("failed to get crossvpceni %v for pod (%v %v): %v", containerID, namespace, name, err)
			log.Warning(ctx, errMsg)
			return false, nil
		}

		log.Infof(ctx, "crossvpceni %v current status: %v", containerID, eni.Status.EniStatus)

		if eni.Status.EniStatus == v1alpha1.EniStatusInuse {
			return true, nil
		}

		return false, nil
	})
	if err != nil && err == wait.ErrWaitTimeout {
		if eni != nil {
			if events, err := ipam.searchCrossVPCEniEvents(ctx, eni); err == nil {
				errMsg = eventsToErrorMsg(events)
			}
		}
		return nil, fmt.Errorf("wait eni %v status to become inuse timeout after %v: %v", containerID, time.Since(t), errMsg)
	}

	result, err := ipam.crdInformer.Cce().V1alpha1().CrossVPCEnis().Lister().Get(containerID)
	if err != nil {
		msg := fmt.Sprintf("failed to get crossvpceni for pod (%v %v): %v", namespace, name, err)
		log.Error(ctx, msg)
		return nil, errors.New(msg)
	}

	return result, nil
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.CrossVPCEni, error) {
	log.Infof(ctx, "[Release] releasing eni for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing eni for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	eni, err := ipam.crdInformer.Cce().V1alpha1().CrossVPCEnis().Lister().Get(containerID)
	if err != nil {
		if kerrors.IsNotFound(err) {
			log.Infof(ctx, "crossvpceni %v of pod (%v %v) not found", containerID, namespace, name)
			return nil, nil
		}
		log.Errorf(ctx, "failed to get crossvpceni %v of pod (%v %v): %v", containerID, namespace, name, err)
		return nil, err
	}

	var (
		nodeName = eni.Labels[PodLabelOwnerNode]
	)

	// node has attaching/detaching eni
	unstable, err := ipam.nodeHasUnstableEni(ctx, nodeName)
	if err != nil {
		log.Errorf(ctx, "nodeHasUnstableEni error: %v", err)
		return nil, err
	}

	if unstable {
		msg := fmt.Sprintf("node %v has unstable eni", nodeName)
		log.Error(ctx, msg)
		return nil, errors.New(msg)
	}

	err = ipam.crdClient.CceV1alpha1().CrossVPCEnis().Delete(ctx, containerID, *metav1.NewDeleteOptions(0))
	if err != nil {
		log.Errorf(ctx, "failed to delete crossvpceni %v of pod (%v %v): %v", containerID, namespace, name, err)
		return nil, err
	}

	var (
		t = time.Now()
	)
	err = wait.ExponentialBackoff(defaultBackoff, func() (done bool, err error) {
		eni, err := ipam.crdInformer.Cce().V1alpha1().CrossVPCEnis().Lister().Get(containerID)
		if err != nil {
			log.Warningf(ctx, "failed to get crossvpceni %v for pod (%v %v): %v", containerID, namespace, name, err)
			if kerrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		}

		log.Infof(ctx, "crossvpceni %v current status: %v", containerID, eni.Status.EniStatus)

		if eni.Status.EniStatus == v1alpha1.EniStatusDeleted {
			return true, nil
		}

		return false, nil
	})
	if err != nil && err == wait.ErrWaitTimeout {
		return nil, fmt.Errorf("wait eni %v status to become deleted timeout after %v", containerID, time.Since(t))
	}

	return nil, nil
}

func (ipam *IPAM) Ready(ctx context.Context) bool {
	return ipam.cacheHasSynced
}

func (ipam *IPAM) Run(ctx context.Context, stopCh <-chan struct{}) error {
	defer func() {
		runtime.HandleCrash()
	}()

	log.Info(ctx, "Starting cross vpc eni ipam controller")
	defer log.Info(ctx, "Shutting down cross vpc eni ipam controller")

	nodeInformer := ipam.kubeInformer.Core().V1().Nodes().Informer()
	podInformer := ipam.kubeInformer.Core().V1().Pods().Informer()
	crossVpcEniInformer := ipam.crdInformer.Cce().V1alpha1().CrossVPCEnis().Informer()

	ipam.kubeInformer.Start(stopCh)
	ipam.crdInformer.Start(stopCh)

	if !cache.WaitForNamedCacheSync(
		"cce-ipam",
		stopCh,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		crossVpcEniInformer.HasSynced,
	) {
		log.Warning(ctx, "failed WaitForCacheSync, timeout")
		return nil
	} else {
		log.Info(ctx, "WaitForCacheSync done")
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
	err := wait.PollImmediateUntil(wait.Jitter(ipam.gcPeriod, 0.5), func() (bool, error) {
		// gc leaked crossvpceni
		var (
			ctx = log.NewContext()
			err error
		)

		eniList, err := ipam.crdInformer.Cce().V1alpha1().CrossVPCEnis().Lister().List(labels.Everything())
		if err != nil {
			log.Errorf(ctx, "gc: failed to list crossvpceni: %v", err)
			return false, nil
		}

		err = ipam.gcLeakedCrossVPCEni(ctx, eniList)
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

func (ipam *IPAM) gcLeakedCrossVPCEni(ctx context.Context, eniList []*v1alpha1.CrossVPCEni) error {
	for _, cveni := range eniList {
		var (
			namespace = cveni.Labels[PodLabelOwnerNamespace]
			name      = cveni.Labels[PodLabelOwnerName]
		)

		_, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(name)
		if err != nil {
			if kerrors.IsNotFound(err) {
				log.Infof(ctx, "gc: try to release leaked crossvpceni (%v %v)", namespace, name)
				err = ipam.crdClient.CceV1alpha1().CrossVPCEnis().Delete(ctx, cveni.Name, *metav1.NewDeleteOptions(0))
				if err != nil {
					log.Errorf(ctx, "gc: failed to delete crossvpceni %v of pod (%v %v): %v", cveni.Name, namespace, name, err)
					continue
				} else {
					log.Infof(ctx, "gc: delete crossvpceni %v of pod (%v %v) successfully", cveni.Name, namespace, name)
				}
			}
		}
	}

	return nil
}

func podHasCrossVPCEniAnnotation(ctx context.Context, pod *v1.Pod) bool {
	var (
		podAnnotations = pod.Annotations
		result         = false
	)

	for _, k := range necessaryAnnoKeyList {
		if _, ok := podAnnotations[k]; ok {
			result = true
			break
		}
	}

	return result
}

func getEniSpecFromPodAnnotations(ctx context.Context, pod *v1.Pod) (*v1alpha1.CrossVPCEniSpec, error) {
	var (
		eniSpec v1alpha1.CrossVPCEniSpec
	)

	eniSpec.UserID = pod.Annotations[PodAnnotationCrossVPCEniUserID]
	eniSpec.SubnetID = pod.Annotations[PodAnnotationCrossVPCEniSubnetID]
	eniSpec.VPCCIDR = pod.Annotations[PodAnnotationCrossVPCEniVPCCIDR]
	eniSpec.PrivateIPAddress = pod.Annotations[PodAnnotationCrossVPCEniPrivateIPAddress]
	eniSpec.SecurityGroupIDs = make([]string, 0)
	secGroups := strings.Split(pod.Annotations[PodAnnotationCrossVPCEniSecurityGroupIDs], ",")
	for _, s := range secGroups {
		eniSpec.SecurityGroupIDs = append(eniSpec.SecurityGroupIDs, strings.TrimSpace(s))
	}

	if eniSpec.UserID == "" {
		return nil, fmt.Errorf("pod annotation %v cannot be empty", PodAnnotationCrossVPCEniUserID)
	}

	if eniSpec.SubnetID == "" {
		return nil, fmt.Errorf("pod annotation %v cannot be empty", PodAnnotationCrossVPCEniSubnetID)
	}

	if eniSpec.VPCCIDR == "" {
		return nil, fmt.Errorf("pod annotation %v cannot be empty", PodAnnotationCrossVPCEniVPCCIDR)
	}

	if len(eniSpec.SecurityGroupIDs) == 0 {
		return nil, fmt.Errorf("pod annotation %v cannot be empty", PodAnnotationCrossVPCEniVPCCIDR)
	}

	return &eniSpec, nil
}

func (ipam *IPAM) nodeHasUnstableEni(ctx context.Context, nodeName string) (bool, error) {
	var (
		selector = labels.NewSelector()
		result   = false
	)

	requirement, err := labels.NewRequirement(PodLabelOwnerNode, selection.Equals, []string{nodeName})
	if err != nil {
		return true, err
	}
	selector.Add(*requirement)

	enis, err := ipam.crdInformer.Cce().V1alpha1().CrossVPCEnis().Lister().List(selector)
	if err != nil {
		return true, err
	}
	for _, e := range enis {
		if e.Status.EniStatus == v1alpha1.EniStatusAttaching || e.Status.EniStatus == v1alpha1.EniStatusDetaching {
			result = true
			break
		}
	}
	return result, nil
}

func (ipam *IPAM) searchCrossVPCEniEvents(ctx context.Context, eni *v1alpha1.CrossVPCEni) (*v1.EventList, error) {
	var (
		tmp     = eni.DeepCopy()
		refKind *string
		refUID  *string
	)
	stringRefKind := string(tmp.Kind)
	if len(stringRefKind) > 0 {
		refKind = &stringRefKind
	}

	stringRefUID := string(tmp.UID)
	if len(stringRefUID) > 0 {
		refUID = &stringRefUID
	}

	e := ipam.kubeClient.CoreV1().Events(eni.Namespace)
	fieldSelector := e.GetFieldSelector(&tmp.Name, &tmp.Namespace, refKind, refUID)
	listOpts := metav1.ListOptions{
		FieldSelector: fieldSelector.String(),
	}
	eventList, err := e.List(ctx, listOpts)
	if err != nil {
		log.Errorf(ctx, "failed to list events of CrossVPCEni %v: %v", tmp.Name, err)
		return nil, err
	}
	return eventList, nil
}

func eventsToErrorMsg(events *v1.EventList) string {
	var (
		errs   []error
		errMsg string
		m      map[string]v1.Event = make(map[string]v1.Event)
	)

	sort.Sort(sort.Reverse(event.SortableEvents(events.Items)))

	for _, evt := range events.Items {
		if _, ok := m[evt.Reason]; !ok {
			m[evt.Reason] = evt
		}
	}

	for _, evt := range m {
		if evt.Type == v1.EventTypeWarning {
			errs = append(errs, fmt.Errorf("%s: %s", evt.Reason, evt.Message))
		}
	}

	allErr := utilerrors.NewAggregate(errs)
	if allErr != nil {
		errMsg = allErr.Error()
	}

	return errMsg
}
