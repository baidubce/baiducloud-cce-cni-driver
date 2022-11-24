package topology_spread

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	clientv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
)

type topologySpreadTableController struct {
	queue         workqueue.RateLimitingInterface
	eventRecorder record.EventRecorder

	crdInformer crdinformers.SharedInformerFactory
	crdClient   versioned.Interface

	pstsLister clientv1alpha1.PodSubnetTopologySpreadLister
	psttLister clientv1alpha1.PodSubnetTopologySpreadTableLister
}

func NewTopologySpreadTableController(
	crdInformer crdinformers.SharedInformerFactory,
	crdClient versioned.Interface,
	eventRecorder record.EventRecorder,
) *topologySpreadTableController {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "pod-topology-spread_table_changes")

	pstsInfomer := crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads()
	psttInfomer := crdInformer.Cce().V1alpha1().PodSubnetTopologySpreadTables()
	psttc := &topologySpreadTableController{
		queue:         queue,
		eventRecorder: eventRecorder,
		crdClient:     crdClient,
		crdInformer:   crdInformer,
		pstsLister:    pstsInfomer.Lister(),
		psttLister:    psttInfomer.Lister(),
	}

	// only add namespace to queue
	pstsInfomer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: psttc.pstsEvent,
			UpdateFunc: func(oldObj, newObj interface{}) {
				psttc.pstsEvent(newObj)
			},
			DeleteFunc: psttc.pstsEvent,
		},
	)
	psttInfomer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: psttc.pstsEvent,
		UpdateFunc: func(oldObj, newObj interface{}) {
			psttc.pstsEvent(newObj)
		},
		DeleteFunc: psttc.pstsEvent,
	})
	return psttc
}

func (psttc *topologySpreadTableController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer psttc.queue.ShutDown()
	klog.Infof("Starting disruption controller")
	pstsInfomer := psttc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Informer()
	psttInfomer := psttc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreadTables().Informer()

	cache.WaitForNamedCacheSync("topology-spread-controller", stopCh, pstsInfomer.HasSynced, psttInfomer.HasSynced)
	defer klog.Infof("Shutting down disruption controller")
	wait.Until(psttc.worker, time.Second, stopCh)
}

// add namespace to queue
func (psttc *topologySpreadTableController) pstsEvent(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Couldn't get key for obj object %+v: %v", obj, err)
		return
	}
	klog.V(5).Infof("receive updating PSTS event %s", key)
	ns := strings.Split(key, "/")
	if len(ns) == 1 {
		psttc.queue.Add(corev1.NamespaceDefault)
	} else if len(ns) > 1 {
		psttc.queue.Add(ns[0])
	}
}

func (psttc *topologySpreadTableController) worker() {
	for psttc.processNextWorkItem() {
	}
}

func (psttc *topologySpreadTableController) processNextWorkItem() bool {
	dKey, quit := psttc.queue.Get()
	if quit {
		return false
	}
	defer psttc.queue.Done(dKey)

	if dKey == "" {
		return true
	}

	err := psttc.sync(dKey.(string))
	if err == nil {
		psttc.queue.Forget(dKey)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("error syncing PodTopologySpreadTableController %v, requeuing: %v", dKey.(string), err))
	psttc.queue.AddRateLimited(dKey)
	return true
}

func (psttc *topologySpreadTableController) sync(namespace string) error {
	psttList, err := psttc.psttLister.PodSubnetTopologySpreadTables(namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("list pstt error %v", err)
		return err
	}

	for i := 0; i < len(psttList); i++ {
		pstt := psttList[i]
		psttCopy := pstt.DeepCopy()

		specLen := len(psttCopy.Spec)
		newStatusArray := make([]networkv1alpha1.PodSubnetTopologySpreadStatus, specLen)

		for j := 0; j < specLen; j++ {
			pstsName := psttCopy.Spec[j].Name
			if pstsName == "" {
				psttc.eventRecorder.Eventf(pstt, corev1.EventTypeWarning, "ParamError", "psts name is nil at pstt(%s/%s)index %d of table ", namespace, pstt.Name, j)
				return fmt.Errorf("psts name is nil at pstt(%s/%s)index %d of table ", namespace, pstt.Name, j)
			}
			useDefault(&psttCopy.Spec[j])

			psts, err := psttc.pstsLister.PodSubnetTopologySpreads(namespace).Get(pstsName)

			// create new psts
			if errors.IsNotFound(err) {
				err = nil
				psts = &networkv1alpha1.PodSubnetTopologySpread{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      pstsName,
						// owner by table
						OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(psttCopy, networkv1alpha1.SchemeGroupVersion.WithKind("PodSubnetTopologySpreadTable"))},
					},
					Spec: psttCopy.Spec[j],
				}
				psts, err = psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(namespace).Create(context.TODO(), psts, metav1.CreateOptions{})
			}
			if err != nil {
				klog.Errorf("get psts (%s/%s) error: %v", namespace, pstsName, err)
				return err
			}

			// update psts if pstt change
			if !reflect.DeepEqual(&psttCopy.Spec[j], &psts.Spec) {
				pstsCopy := psts.DeepCopy()
				pstsCopy.Spec = psttCopy.Spec[j]
				_, err = psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(namespace).Update(context.TODO(), pstsCopy, metav1.UpdateOptions{})
				if err != nil {
					klog.Errorf("update psts (%s/%s) error: %v", namespace, pstsName, err)
					return err
				}
			}

			newStatusArray[j] = *psts.Status.DeepCopy()
		}

		// clean old psts
		for d := 0; d < len(psttCopy.Status); d++ {
			status := psttCopy.Status[d]
			pstsName := status.Name

			// delete old psts
			if !isInArray(psttCopy.Spec, pstsName) {
				psts, err := psttc.pstsLister.PodSubnetTopologySpreads(namespace).Get(pstsName)
				if errors.IsNotFound(err) {
					continue
				}
				if err != nil {
					klog.Errorf("get psts (%s/%s) error: %v", namespace, pstsName, err)
					return err
				}

				if metav1.IsControlledBy(psts, &psttCopy.ObjectMeta) {
					err = psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(namespace).Delete(context.TODO(), pstsName, metav1.DeleteOptions{})
					if err != nil {
						klog.Errorf("delete psts (%s/%s) error: %v", namespace, pstsName, err)
						return err
					} else {
						psttc.eventRecorder.Eventf(pstt, corev1.EventTypeNormal, "CleanPSTS", "psts(%s/%s) be deleted ", namespace, pstsName)
					}
				}
			}
		}

		// write new status
		if !reflect.DeepEqual(newStatusArray, pstt.Status) {
			psttCopy.Status = newStatusArray
			_, err = psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreadTables(namespace).UpdateStatus(context.TODO(), psttCopy, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("update pstt (%s/%s) error: %v", namespace, psttCopy.Name, err)
				return err
			}
			klog.V(3).Infof("update pstt/status (%s/%s) success", namespace, psttCopy.Name)
		}
	}
	return nil
}

func isInArray(arr []networkv1alpha1.PodSubnetTopologySpreadSpec, name string) bool {
	for _, v := range arr {
		if v.Name == name {
			return true
		}
	}
	return false
}
