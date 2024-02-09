package watchers

import (
	"context"
	"reflect"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccev2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	listv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	listv2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

const (
	subResourcesFinalizer = "subresources"
	cpstsComponent        = "cpsts-watcher"
)

var (
	cpstsLog = logging.NewSubysLogger(cpstsComponent)
)

// CPSTSEventHandler should implement the behavior to handle PSTS
type CPSTSEventHandler interface {
	Update(resource *ccev2alpha1.ClusterPodSubnetTopologySpread) error
	Delete(namespace, pstsName string) error
}

func StartSynchronizingCPSTS(ctx context.Context) error {
	var cpstsManager = &cpstsSyncher{
		pstsLister:  k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister(),
		cpstsLister: k8s.CCEClient().Informers.Cce().V2alpha1().ClusterPodSubnetTopologySpreads().Lister(),
		recorder:    k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: cpstsComponent}),
	}

	log.Info("Starting to synchronize CPSTS custom resources")

	cpstsManagerSyncHandler := func(key string) error {
		obj, err := cpstsManager.cpstsLister.Get(key)

		// Delete handling
		if errors.IsNotFound(err) {
			return cpstsManager.Delete(key)
		}
		if err != nil {
			log.WithError(err).Warning("Unable to retrieve CPSTS from watcher store")
			return err
		}
		return cpstsManager.Update(obj)
	}

	controller := cm.NewResyncController("cce-cpsts-controller", int(operatorOption.Config.ResourceResyncWorkers), k8s.CCEClient().Informers.Cce().V2alpha1().ClusterPodSubnetTopologySpreads().Informer(), cpstsManagerSyncHandler)
	controller.RunWithResync(1 * time.Hour)

	pstsManagerSyncHandler := func(key string) error {
		ns, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return nil
		}

		psts, err := cpstsManager.pstsLister.PodSubnetTopologySpreads(ns).Get(name)
		if err != nil {
			return nil
		}
		if len(psts.Labels) > 0 {
			if psts.GetLabels()[k8s.LabelOwnerByReference] != "" {
				controller.Queue.Add(psts.GetLabels()[k8s.LabelOwnerByReference])
			}

		}
		return nil
	}
	cm.NewResyncController("cce-cpsts-psts-controller", int(operatorOption.Config.ResourceResyncWorkers), k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Informer(), pstsManagerSyncHandler).RunWithResync(operatorOption.Config.ResourceResyncInterval)

	updateNsFunc := func(objObj, newObj interface{}) {
		oldNs, oldOk := objObj.(*corev1.Namespace)
		newNs, newOk := newObj.(*corev1.Namespace)

		if !newOk {
			return
		} else if oldOk && newOk {
			if reflect.DeepEqual(oldNs.Labels, newNs.Labels) {
				return
			}
		}
		allCpsts, err := cpstsManager.cpstsLister.List(labels.Everything())
		if err != nil {
			cpstsLog.WithError(err).Error("Unable to list CPSTS")
		}

		for _, cpsts := range allCpsts {
			if cpsts.Spec.NamespaceSelector == nil {
				controller.Queue.Add(cpsts.Name)
			} else if _, ok := cpsts.Status.SubStatus[newNs.Name]; ok {
				controller.Queue.Add(cpsts.Name)
			} else {
				selector, err := metav1.LabelSelectorAsSelector(cpsts.Spec.NamespaceSelector)
				if err != nil {
					continue
				}
				if selector.Matches(labels.Set(newNs.Labels)) {
					controller.Queue.Add(cpsts.Name)
				}
				if oldOk {
					selector, err = metav1.LabelSelectorAsSelector(cpsts.Spec.NamespaceSelector)
					if err != nil {
						continue
					}
					if selector.Matches(labels.Set(newNs.Labels)) {
						controller.Queue.Add(cpsts.Name)
					}
				}
			}

		}
	}
	// watch namespace update
	k8s.WatcherClient().Informers.Core().V1().Namespaces().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { updateNsFunc(nil, obj) },
		UpdateFunc: updateNsFunc,
	})

	return nil
}

type cpstsSyncher struct {
	pstsLister  listv2.PodSubnetTopologySpreadLister
	cpstsLister listv2alpha1.ClusterPodSubnetTopologySpreadLister
	recorder    record.EventRecorder
}

func (s *cpstsSyncher) Update(resource *ccev2alpha1.ClusterPodSubnetTopologySpread) error {
	var (
		err       error
		scopedLog = cpstsLog.WithFields(logrus.Fields{
			"name": resource.Name,
		})

		// selector is used to select pods that are matched by the PSTS
		selector labels.Selector = labels.Everything()

		affectNamespaces []*corev1.Namespace

		newStatus = &ccev2alpha1.ClusterPodSubnetTopologySpreadStatus{
			SubStatus: make(map[string]ccev2.PodSubnetTopologySpreadStatus),
		}

		expiredPstsMap = make(map[string]*ccev2.PodSubnetTopologySpread)
	)
	resource = resource.DeepCopy()

	// remove finalizer if no subresource is created
	shouldReturn, err := s.managerFinalizer(resource, scopedLog)
	if shouldReturn {
		return nil
	}
	if err != nil {
		return err
	}

	if resource.Spec.Selector != nil {
		selector = labels.Set(resource.Spec.NamespaceSelector.MatchLabels).AsSelector()
	}
	affectNamespaces, err = k8s.WatcherClient().Informers.Core().V1().Namespaces().Lister().List(selector)
	if err != nil {
		scopedLog.Errorf("failed to list namesapce with selector %s: %v", selector, err)
		return err
	}

	subresources, err := s.pstsLister.PodSubnetTopologySpreads(corev1.NamespaceAll).
		List(labels.Set{k8s.LabelOwnerByReference: resource.Name}.AsSelector())
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to list sub psts in all namespaces")
	}
	for _, subresource := range subresources {
		expiredPstsMap[subresource.Namespace+"/"+subresource.Name] = subresource
	}
	for i := range affectNamespaces {
		newStatus.ExpectObjectCount++

		namespace := affectNamespaces[i].Name
		pstsName := namespace + "-" + resource.Name
		key := namespace + "/" + pstsName
		expired := expiredPstsMap[key]
		delete(expiredPstsMap, key)

		// should create a new psts
		psts, err := s.updateSubPSTS(scopedLog, namespace, resource, expired)
		if err != nil {
			return err
		}
		if psts != nil {
			if len(newStatus.Status.AvailableSubnets) == 0 {
				newStatus.Status.AvailableSubnets = psts.Status.AvailableSubnets
				newStatus.Status.SchedulableSubnetsNum = psts.Status.SchedulableSubnetsNum
			}
			if len(newStatus.Status.UnavailableSubnets) == 0 {
				newStatus.Status.UnavailableSubnets = psts.Status.UnavailableSubnets
				newStatus.Status.UnSchedulableSubnetsNum = psts.Status.UnSchedulableSubnetsNum
			}
			newStatus.SubStatus[affectNamespaces[i].Name] = psts.Status
			newStatus.SubObjectCount++
			newStatus.Status.PodMatchedCount += psts.Status.PodMatchedCount
			newStatus.Status.PodAffectedCount += psts.Status.PodAffectedCount
		}
	}

	// delete expired psts
	// 1. if psts is not in the list of subresources, like labels of namespace has been
	// changed it means that this psts should be deleted
	for key := range expiredPstsMap {
		psts := expiredPstsMap[key]
		err = pstsClient.Delete(psts.Namespace, psts.Name)
		if err != nil {
			s.recorder.Eventf(resource, corev1.EventTypeWarning, "FailedDeleteSubresource", "cpsts failed delete subresource psts %s: %v", key, err)
		} else {
			s.recorder.Eventf(resource, corev1.EventTypeNormal, "DeleteSubresource", "labels of namesapce have been changed and delete subresource psts %s", key)
		}
	}

	if !reflect.DeepEqual(&resource.Status, newStatus) {
		resource.Status = *newStatus
		_, err = k8s.CCEClient().CceV2alpha1().ClusterPodSubnetTopologySpreads().UpdateStatus(context.TODO(), resource, metav1.UpdateOptions{})
		if err != nil {
			scopedLog.WithError(err).Errorf("failed to update cpsts status ")
			return err
		}
	}
	return nil
}

func (*cpstsSyncher) managerFinalizer(resource *ccev2alpha1.ClusterPodSubnetTopologySpread, scopedLog *logrus.Entry) (bool, error) {
	var (
		containsFinalizer = false
	)

	for i := range resource.Finalizers {
		if resource.Finalizers[i] == subResourcesFinalizer {
			containsFinalizer = true
		}
	}

	if resource.DeletionTimestamp != nil {
		if resource.Status.SubObjectCount != 0 {
			return false, nil
		}
		if !containsFinalizer {
			return false, nil
		}
		var finalizer []string
		for _, f := range resource.Finalizers {
			if f != subResourcesFinalizer {
				finalizer = append(finalizer, f)
			}
		}
		resource.Finalizers = finalizer
	} else if containsFinalizer {
		return false, nil
	} else {
		resource.Finalizers = append(resource.Finalizers, subResourcesFinalizer)
	}
	_, err := k8s.CCEClient().CceV2alpha1().ClusterPodSubnetTopologySpreads().Update(context.TODO(), resource, metav1.UpdateOptions{})
	if err != nil {
		scopedLog.WithError(err).Errorf("failed to update finalizer %s", subResourcesFinalizer)
		return true, err
	}
	scopedLog.Infof("update cpsts finalizer %s success", subResourcesFinalizer)
	return true, nil
}

// updateSubPSTS update subresource of cpsts
// return psts and error
// if error is nil and psts is nil, it means that psts should be deleted
// if error is not nil, it means that psts should not be skipped
// if psts is not nil, it means that status of psts should be created or updated
func (s *cpstsSyncher) updateSubPSTS(scopedLog *logrus.Entry, namespace string, resource *ccev2alpha1.ClusterPodSubnetTopologySpread, psts *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
	var (
		pstsName = namespace + "-" + resource.Name
		err      error
	)

	if psts != nil {
		if resource.DeletionTimestamp != nil {
			scopedLog.Infof("cpsts is deleting, we will delete subresource %s", pstsName)
			return nil, pstsClient.Delete(namespace, pstsName)
		}
	} else {
		psts = &ccev2.PodSubnetTopologySpread{
			ObjectMeta: metav1.ObjectMeta{
				Name:        pstsName,
				Namespace:   namespace,
				Labels:      resource.Labels,
				Annotations: resource.Annotations,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: ccev2alpha1.SchemeGroupVersion.String(),
						Kind:       ccev2alpha1.KindClusterPodSubnetTopologySpread,
						Name:       resource.Name,
						UID:        resource.UID,
					},
				},
			},
			Spec: resource.Spec.PodSubnetTopologySpreadSpec,
		}
		if psts.Labels == nil {
			psts.Labels = make(map[string]string)
		}
		psts.Labels[k8s.LabelOwnerByReference] = resource.Name
		psts, err = pstsClient.Create(psts)
		if err != nil {
			s.recorder.Eventf(resource, corev1.EventTypeWarning, "FailedCreateSubresource", "failed to create psts %s, cpsts will skip this namespace: %v", pstsName, err)
			return nil, nil
		}
		s.recorder.Eventf(resource, corev1.EventTypeNormal, "CreateSubresource", "create psts %s success", pstsName)
		return psts, nil
	}

	// should update psts
	if !reflect.DeepEqual(psts.Spec, resource.Spec.PodSubnetTopologySpreadSpec) {
		newpsts := psts.DeepCopy()
		newpsts.Spec = resource.Spec.PodSubnetTopologySpreadSpec
		psts, err = pstsClient.Update(newpsts)
		if err != nil {
			s.recorder.Eventf(resource, corev1.EventTypeWarning, "FailedUpdateSubresource", "failed to update psts %s: %v", pstsName, err)
			return nil, err
		}
	}

	return psts, nil
}

func (s *cpstsSyncher) Delete(key string) error {
	cpstsLog.Infof("cpsts %s have been deleted", key)
	return nil
}
