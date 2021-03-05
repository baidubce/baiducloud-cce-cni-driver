// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	time "time"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	versioned "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	internalinterfaces "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// WorkloadEndpointInformer provides access to a shared informer and lister for
// WorkloadEndpoints.
type WorkloadEndpointInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.WorkloadEndpointLister
}

type workloadEndpointInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewWorkloadEndpointInformer constructs a new informer for WorkloadEndpoint type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewWorkloadEndpointInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredWorkloadEndpointInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredWorkloadEndpointInformer constructs a new informer for WorkloadEndpoint type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredWorkloadEndpointInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CceV1alpha1().WorkloadEndpoints(namespace).List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CceV1alpha1().WorkloadEndpoints(namespace).Watch(options)
			},
		},
		&networkingv1alpha1.WorkloadEndpoint{},
		resyncPeriod,
		indexers,
	)
}

func (f *workloadEndpointInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredWorkloadEndpointInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *workloadEndpointInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&networkingv1alpha1.WorkloadEndpoint{}, f.defaultInformer)
}

func (f *workloadEndpointInformer) Lister() v1alpha1.WorkloadEndpointLister {
	return v1alpha1.NewWorkloadEndpointLister(f.Informer().GetIndexer())
}
