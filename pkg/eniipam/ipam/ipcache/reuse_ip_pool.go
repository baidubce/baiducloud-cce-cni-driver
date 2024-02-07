package ipcache

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

type ReuseIPAndWepPool struct {
	*CacheMap[*networkingv1alpha1.WorkloadEndpoint]
}

func NewReuseIPAndWepPool() *ReuseIPAndWepPool {
	return &ReuseIPAndWepPool{
		CacheMap: NewCacheMap[*networkingv1alpha1.WorkloadEndpoint](),
	}
}

func (pool *ReuseIPAndWepPool) OnAdd(obj interface{}) {
	wep, ok := obj.(*networkingv1alpha1.WorkloadEndpoint)
	if ok {
		if networking.ISCustomReuseModeWEP(wep) || networking.IsFixIPStatefulSetPodWep(wep) {
			pool.CacheMap.Add(wep.Spec.IP, wep)
			if wep.Spec.IPv6 != "" {
				pool.CacheMap.Add(wep.Spec.IPv6, wep)
			}
		}
	}
}
func (pool *ReuseIPAndWepPool) OnUpdate(oldObj, newObj interface{}) {
	old, ok := oldObj.(*networkingv1alpha1.WorkloadEndpoint)
	if ok {
		pool.CacheMap.Delete(old.Spec.IP)
		if old.Spec.IPv6 != "" {
			pool.CacheMap.Delete(old.Spec.IPv6)
		}
	}
	wep, ok := newObj.(*networkingv1alpha1.WorkloadEndpoint)
	if ok {
		if networking.ISCustomReuseModeWEP(wep) || networking.IsFixIPStatefulSetPodWep(wep) {
			pool.CacheMap.Add(wep.Spec.IP, wep)
			if wep.Spec.IPv6 != "" {
				pool.CacheMap.Add(wep.Spec.IPv6, wep)
			}
		}
	}
}

func (pool *ReuseIPAndWepPool) OnDelete(obj interface{}) {
	old, ok := obj.(*networkingv1alpha1.WorkloadEndpoint)
	if ok {
		pool.CacheMap.Delete(old.Spec.IP)
		if old.Spec.IPv6 != "" {
			pool.CacheMap.Delete(old.Spec.IPv6)
		}
	}
}

var _ cache.ResourceEventHandler = &ReuseIPAndWepPool{}
