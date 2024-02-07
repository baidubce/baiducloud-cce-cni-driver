package cm

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	operatorMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
)

const (
	DefaultRetryInterval = 1 * time.Second
)

var log = logging.NewSubysLogger("controller-manager")

type ReconcileFunc func(key string) error

type WorkqueueController struct {
	Name        string
	ClusterName string
	WorkerNum   int
	Queue       workqueue.RateLimitingInterface
	Reconcile   ReconcileFunc
	log         *logrus.Entry
}

func NewWorkqueueController(name string, workerNum int, reconcile ReconcileFunc) *WorkqueueController {
	controller := &WorkqueueController{Name: name, WorkerNum: workerNum, Reconcile: reconcile}
	controller.Queue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), name)
	controller.log = log.WithField("controller", name)
	return controller
}

func (ctl *WorkqueueController) WithClusterName(clusterName string) *WorkqueueController {
	ctl.ClusterName = clusterName
	return ctl
}

func (ctl *WorkqueueController) OnAdd(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		ctl.log.WithError(err).Warningf("Unable to process %s Add event", ctl.Name)
		return
	}
	ctl.Queue.Add(key)
	ctl.recordEvent(metrics.LabelEventMethodAdd)
}

func (ctl *WorkqueueController) OnUpdate(oldObj, newObj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newObj)
	if err != nil {
		ctl.log.WithError(err).Warningf("Unable to process %s Update event", ctl.Name)
		return
	}
	ctl.Queue.Add(key)
	ctl.recordEvent(metrics.LabelEventMethodUpdate)
}

func (ctl *WorkqueueController) OnDelete(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		ctl.log.WithError(err).Warningf("Unable to process %s Delete event", ctl.Name)
		return
	}
	ctl.Queue.Add(key)
	ctl.recordEvent(metrics.LabelEventMethodDelete)
}

func (ctl *WorkqueueController) Run() {
	for i := 0; i < ctl.WorkerNum; i++ {
		go func(worker string) {
			for ctl.processNextWorkItem(worker) {
			}
		}(fmt.Sprintf("%s-%d", ctl.Name, i))
	}

	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			operatorMetrics.WorkQueueLens.WithLabelValues(ctl.ClusterName, ctl.Name).Set(float64(ctl.Queue.Len()))
		}
	}()
}

// processNextWorkItem process all events from the workqueue.
func (ctl *WorkqueueController) processNextWorkItem(worker string) bool {
	stepLog := ctl.log.WithField("worker", worker)
	key, quit := ctl.Queue.Get()
	if quit {
		stepLog.Info("Workqueue queue is quit")
		return false
	}
	var err error
	start := time.Now()

	defer func() {
		ctl.Queue.Done(key)
		errstr := metrics.LabelValueOutcomeSuccess
		if err != nil {
			errstr = metrics.LabelError
		}

		operatorMetrics.ControllerHandlerDurationMilliseconds.WithLabelValues(ctl.ClusterName, ctl.Name, "Reconcile", errstr).Observe(float64(time.Since(start).Milliseconds()))
	}()

	err = ctl.Reconcile(key.(string))
	if err == nil {
		// If err is nil we can forget it from the queue, if it is not nil
		// the queue handler will retry to process this key until it succeeds.
		ctl.Queue.Forget(key)
		return true
	}

	// revice a delay event from reconciler, we should try
	if event, ok := err.(*DelayEvent); ok {
		if newKey, ok := event.Key.(string); ok {
			ctl.Queue.Forget(key)
			ctl.Queue.AddAfter(newKey, event.Duration)
			ctl.recordEvent("delay")
		}
		return true
	}

	stepLog.WithError(err).Errorf("sync %q failed with %v", key, err)
	ctl.Queue.AddRateLimited(key)
	ctl.recordEvent("retry")
	return true
}

// recordEvent records the event metrics.
func (ctl *WorkqueueController) recordEvent(method string) {
	operatorMetrics.WorkQueueEventCount.WithLabelValues(ctl.ClusterName, ctl.Name, method).Inc()
	operatorMetrics.WorkQueueLens.WithLabelValues(ctl.ClusterName, ctl.Name).Set(float64(ctl.Queue.Len()))
}

// ResyncController is a controller that resyncs periodically.
type ResyncController struct {
	*WorkqueueController
	ctl      *controller.Manager
	informer cache.SharedIndexInformer
}

// NewResyncController Run starts the controller.
// it can custom the resync period.
func NewResyncController(name string, workerNum int, informer cache.SharedIndexInformer, reconcile ReconcileFunc) *ResyncController {
	controller := &ResyncController{
		WorkqueueController: NewWorkqueueController(name, workerNum, reconcile),
		informer:            informer,
		ctl:                 controller.NewManager(),
	}
	return controller
}

func (resync *ResyncController) RunWithResync(resyncPeriod time.Duration) {
	// Simulate resynchronization using timed tasks
	resync.ctl.UpdateController(resync.WorkqueueController.Name, controller.ControllerParams{
		RunInterval: resyncPeriod,
		DoFunc: func(ctx context.Context) error {
			// should not use resync.informer.GetStore().Resync() as function synchronizes
			// the entire kind
			for _, key := range resync.informer.GetStore().ListKeys() {
				resync.Queue.Add(key)
				resync.WorkqueueController.recordEvent("resync")
			}
			return nil
		},
	})

	resync.WorkqueueController.Run()
	resync.informer.AddEventHandler(resync)
}
