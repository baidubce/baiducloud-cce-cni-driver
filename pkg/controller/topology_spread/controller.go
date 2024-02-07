package topology_spread

import (
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	listercorev1 "k8s.io/client-go/listers/core/v1"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	clientv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
)

type TopologySpreadController struct {
	queue         workqueue.RateLimitingInterface
	eventRecorder record.EventRecorder

	kubeInformer informers.SharedInformerFactory
	crdInformer  crdinformers.SharedInformerFactory
	crdClient    versioned.Interface

	pstsLister   clientv1alpha1.PodSubnetTopologySpreadLister
	subnetLister clientv1alpha1.SubnetLister
	wepLister    clientv1alpha1.WorkloadEndpointLister
	podLister    listercorev1.PodLister

	sbnController *subnet.SubnetController
	psttc         *topologySpreadTableController
}

func NewTopologySpreadController(
	kubeInformer informers.SharedInformerFactory,
	crdInformer crdinformers.SharedInformerFactory,
	crdClient versioned.Interface,
	broadcaster record.EventBroadcaster,
	sbnController *subnet.SubnetController,
) *TopologySpreadController {
	eventRecorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "pod-topology-spread-controller"})

	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "pod-topology-spread_changes")

	pstsInfomer := crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads()
	psttc := NewTopologySpreadTableController(crdInformer, crdClient, eventRecorder)

	tsc := &TopologySpreadController{
		queue:         queue,
		eventRecorder: eventRecorder,
		crdClient:     crdClient,
		sbnController: sbnController,
		psttc:         psttc,
		crdInformer:   crdInformer,
		kubeInformer:  kubeInformer,

		podLister:    kubeInformer.Core().V1().Pods().Lister(),
		pstsLister:   pstsInfomer.Lister(),
		subnetLister: crdInformer.Cce().V1alpha1().Subnets().Lister(),
		wepLister:    crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister(),
	}

	pstsInfomer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    tsc.addPSTSEvent,
			UpdateFunc: tsc.updatePSTSEvent,
			DeleteFunc: tsc.deletePSTSEvent,
		},
	)
	kubeInformer.Core().V1().Pods().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    tsc.addPodEvent,
		UpdateFunc: tsc.updatePodEvent,
		DeleteFunc: tsc.addPodEvent,
	})

	return tsc
}

func (tsc *TopologySpreadController) waitCache(stopCh <-chan struct{}) bool {
	podInformer := tsc.kubeInformer.Core().V1().Pods().Informer()
	pstsInfomer := tsc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Informer()
	sbnInformer := tsc.crdInformer.Cce().V1alpha1().Subnets().Informer()
	wepInformer := tsc.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()

	return cache.WaitForNamedCacheSync("topology-spread-controller", stopCh, podInformer.HasSynced, sbnInformer.HasSynced, pstsInfomer.HasSynced, wepInformer.HasSynced)
}

func (tsc *TopologySpreadController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer tsc.queue.ShutDown()
	klog.Infof("Starting disruption controller")
	defer klog.Infof("Shutting down disruption controller")
	tsc.waitCache(stopCh)
	group := wait.Group{}
	group.StartWithChannel(stopCh, func(stopCh <-chan struct{}) {
		tsc.psttc.Run(stopCh)
	})

	group.StartWithChannel(stopCh, func(stopCh <-chan struct{}) {
		wait.Until(tsc.worker, time.Second, stopCh)
	})
	group.Wait()
}

func (tsc *TopologySpreadController) worker() {
	for tsc.processNextWorkItem() {
	}
}

func (tsc *TopologySpreadController) processNextWorkItem() bool {
	dKey, quit := tsc.queue.Get()
	if quit {
		return false
	}
	defer tsc.queue.Done(dKey)

	err := tsc.sync(dKey.(string))
	if err == nil {
		tsc.queue.Forget(dKey)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("error syncing PodTopologySpreadController %v, requeuing: %v", dKey.(string), err))
	tsc.queue.AddRateLimited(dKey)

	return true
}

func (tsc *TopologySpreadController) sync(key string) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing PodSubnetTopologySpread %q (%v)", key, time.Since(startTime))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	psts, err := tsc.pstsLister.PodSubnetTopologySpreads(namespace).Get(name)
	if errors.IsNotFound(err) {
		klog.V(4).Infof("PodSubnetTopologySpread %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	return tsc.trySync(psts.DeepCopy())
}

func (tsc *TopologySpreadController) trySync(psts *networkv1alpha1.PodSubnetTopologySpread) error {
	if len(psts.Spec.Subnets) == 0 {
		klog.Errorf("PodSubnetTopologySpread %s/%s hava none subnetes", psts.Namespace, psts.Name)
		return nil
	}

	// set default value to psts
	spec := &psts.Spec
	specCopy := spec.DeepCopy()
	if specCopy.Name == "" {
		specCopy.Name = psts.GetName()
	}
	useDefault(specCopy)
	if !reflect.DeepEqual(spec, specCopy) {
		klog.Infof("PodSubnetTopologySpread %s/%s update default spec", psts.Namespace, psts.Name)
		psts.Spec = *specCopy
		_, err := tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(psts.Namespace).Update(context.Background(), psts, metav1.UpdateOptions{})
		return err
	}

	return tsc.syncPSTSStatus(psts)
}

// Synchronize subnet status
// sent warning event to PodSubnetTopologySpread
// Statistics of subnet distribution information of pod
// Statistics of subnet distribution information of pod
// filter pod
func (tsc *TopologySpreadController) syncPSTSStatus(psts *networkv1alpha1.PodSubnetTopologySpread) error {
	status := networkv1alpha1.PodSubnetTopologySpreadStatus{}
	status.Name = psts.GetName()
	status.AvailableSubnets = make(map[string]networkv1alpha1.SubnetPodStatus, 0)
	status.UnavailableSubnets = make(map[string]networkv1alpha1.SubnetPodStatus, 0)

	for subnetID, _ := range psts.Spec.Subnets {
		sbn, err := tsc.sbnController.Get(subnetID)
		if errors.IsNotFound(err) {
			status.UnavailableSubnets[subnetID] = networkv1alpha1.SubnetPodStatus{
				SubenetDetail: v1alpha1.SubenetDetail{
					Enable: false,
				},
				Message: "subnet is not found",
			}
			klog.V(3).Infof("PodSubnetTopologySpread %s/%s contains a not found subnet %s", psts.Namespace, psts.Name, subnetID)
			continue
		}
		if err != nil {
			klog.Errorf("get Subnet %s error : %v", subnetID, err)
			return err
		}

		subnetStatus := networkv1alpha1.SubnetPodStatus{
			SubenetDetail: v1alpha1.SubenetDetail{
				AvailableIPNum:   sbn.Status.AvailableIPNum,
				Enable:           sbn.Status.Enable,
				HasNoMoreIP:      sbn.Status.HasNoMoreIP,
				ID:               sbn.Name,
				Name:             sbn.Spec.Name,
				AvailabilityZone: sbn.Spec.AvailabilityZone,
				CIDR:             sbn.Spec.CIDR,
			},
		}
		if subnetStatus.Enable {
			status.AvailableSubnets[subnetID] = subnetStatus

			if subnetStatus.HasNoMoreIP {
				tsc.eventRecorder.Eventf(psts, v1.EventTypeWarning, "NoMoreIP", "subnet %s have no more ip", subnetID)
			} else {
				status.SchedulableSubnetsNum++
			}
		} else {
			status.UnavailableSubnets[subnetID] = subnetStatus
		}
	}

	var (
		pods     []*corev1.Pod
		err      error
		selector labels.Selector
	)
	if psts.Spec.Selector != nil {

		selector, err = metav1.LabelSelectorAsSelector(psts.Spec.Selector)
		if err != nil {
			tsc.eventRecorder.Eventf(psts, v1.EventTypeWarning, "SpecError", "spec.selector error: %v", err)
			return err
		}
		pods, err = tsc.podLister.Pods(psts.Namespace).List(selector)
	} else {
		pods, err = tsc.podLister.Pods(psts.Namespace).List(labels.Everything())
	}
	if err != nil {
		klog.Errorf("PodSubnetTopologySpread %s/%s list pod error: %v", psts.Namespace, psts.Name, err)
	}
	if len(pods) > 0 {
		for _, pod := range pods {
			wep, err := tsc.wepLister.WorkloadEndpoints(pod.Namespace).Get(pod.Name)
			if errors.IsNotFound(err) {
				klog.V(3).Infof("No WorkloadEndpoint object matching pod %s/%s", pod.Namespace, pod.Name)
				continue
			}
			if pod.Spec.HostNetwork {
				continue
			}
			if wep.Spec.SubnetTopologyReference != psts.Name {
				continue
			}
			if subnetStatus, ok := status.AvailableSubnets[wep.Spec.SubnetID]; ok {
				subnetStatus.PodCount++
				status.PodAffectedCount++
			}
		}
	}

	status.PodMatchedCount = int32(len(pods))
	status.UnSchedulableSubnetsNum = int32(len(status.AvailableSubnets) + len(status.UnavailableSubnets) - int(status.SchedulableSubnetsNum))

	if !reflect.DeepEqual(psts.Status, status) {
		toUpdate := psts.DeepCopy()
		toUpdate.Status = status
		_, err = tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(psts.Namespace).UpdateStatus(context.Background(), toUpdate, metav1.UpdateOptions{})
		return err
	}
	return nil
}

// useDefault If there is a psts level policy, the psts level policy is directly used as the default policy.
// If there is no PSTS level policy, the first subnet level policy is used as the default policy.
// If there is no subnet level policy, a temporary Elastic policy will be built by default
func useDefault(spec *networkv1alpha1.PodSubnetTopologySpreadSpec) {
	if spec.Strategy == nil {
		for sbnID := range spec.Subnets {
			sbn := spec.Subnets[sbnID]
			if sbn.Type == "" {
				sbn.Type = networkv1alpha1.IPAllocTypeElastic
			}
			if sbn.Type == networkv1alpha1.IPAllocTypeElastic || sbn.Type == networkv1alpha1.IPAllocTypeManual {
				sbn.ReleaseStrategy = networkv1alpha1.ReleaseStrategyTTL
			}

			tmp := spec.Subnets[sbnID].IPAllocationStrategy
			spec.Strategy = &tmp

			spec.Subnets[sbnID] = sbn
			break
		}
	} else {
		if spec.Strategy.Type == networkv1alpha1.IPAllocTypeCustom {
			if spec.Strategy.TTL == nil {
				spec.Strategy.TTL = networkv1alpha1.DefaultReuseIPTTL
			}
		}
	}
}
