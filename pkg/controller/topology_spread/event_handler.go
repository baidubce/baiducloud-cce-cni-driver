package topology_spread

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	cache "k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

var defaultContext = context.Background()

func (tsc *TopologySpreadController) addPSTSEvent(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Errorf(defaultContext, "Couldn't get key for obj object %+v: %v", obj, err)
		return
	}
	log.V(5).Infof(defaultContext, "receive updating PSTS event %s", key)
	tsc.queue.Add(key)
}

func (tsc *TopologySpreadController) deletePSTSEvent(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Errorf(defaultContext, "Couldn't get key for obj object %+v: %v", obj, err)
		return
	}
	log.V(5).Infof(defaultContext, "receive delete PSTS event %s", key)
	tsc.queue.Add(key)
}

func (tsc *TopologySpreadController) updatePSTSEvent(old, cur interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(cur)
	if err != nil {
		log.Errorf(context.Background(), "Couldn't get key for obj object %+v: %v", cur, err)
		return
	}
	log.V(5).Infof(defaultContext, "receive updating PSTS event %s", key)
	tsc.queue.Add(key)
}

func (tsc *TopologySpreadController) addPodEvent(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if ok {
		if pstsName, ok := pod.Annotations[networking.AnnotationPodSubnetTopologySpread]; ok {
			tsc.queue.Add(pod.Namespace + "/" + pstsName)
		}
	}
}

func (tsc *TopologySpreadController) updatePodEvent(old, cur interface{}) {
	pod, ok := old.(*corev1.Pod)
	if ok {
		if pstsName, ok := pod.Annotations[networking.AnnotationPodSubnetTopologySpread]; ok {
			tsc.queue.Add(pod.Namespace + "/" + pstsName)
		}
	}

	pod, ok = cur.(*corev1.Pod)
	if ok {
		if pstsName, ok := pod.Annotations[networking.AnnotationPodSubnetTopologySpread]; ok {
			tsc.queue.Add(pod.Namespace + "/" + pstsName)
		}
	}
}
