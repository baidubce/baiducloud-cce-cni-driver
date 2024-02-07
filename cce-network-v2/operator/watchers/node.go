package watchers

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

var (
	nodeQueue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "node-queue")

	NodeClient = &NodeUpdater{}
)

// nodesInit starts up a node watcher to handle node events.
func nodesInit() {
	k8s.WatcherClient().Informers.Core().V1().Nodes().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, _ := queueKeyFunc(obj)
			nodeQueue.Add(key)
		},
		UpdateFunc: func(_, newObj interface{}) {
			key, _ := queueKeyFunc(newObj)
			nodeQueue.Add(key)
		},
	})
}

type NodeUpdater struct {
}

func (*NodeUpdater) Lister() listv1.NodeLister {
	return k8s.WatcherClient().Informers.Core().V1().Nodes().Lister()
}

func (*NodeUpdater) NodeInterface() typev1.NodeInterface {
	return k8s.WatcherClient().CoreV1().Nodes()
}
