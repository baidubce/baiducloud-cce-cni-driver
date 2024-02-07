// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// WorkloadEndpointLister helps list WorkloadEndpoints.
type WorkloadEndpointLister interface {
	// List lists all WorkloadEndpoints in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.WorkloadEndpoint, err error)
	// WorkloadEndpoints returns an object that can list and get WorkloadEndpoints.
	WorkloadEndpoints(namespace string) WorkloadEndpointNamespaceLister
	WorkloadEndpointListerExpansion
}

// workloadEndpointLister implements the WorkloadEndpointLister interface.
type workloadEndpointLister struct {
	indexer cache.Indexer
}

// NewWorkloadEndpointLister returns a new WorkloadEndpointLister.
func NewWorkloadEndpointLister(indexer cache.Indexer) WorkloadEndpointLister {
	return &workloadEndpointLister{indexer: indexer}
}

// List lists all WorkloadEndpoints in the indexer.
func (s *workloadEndpointLister) List(selector labels.Selector) (ret []*v1alpha1.WorkloadEndpoint, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.WorkloadEndpoint))
	})
	return ret, err
}

// WorkloadEndpoints returns an object that can list and get WorkloadEndpoints.
func (s *workloadEndpointLister) WorkloadEndpoints(namespace string) WorkloadEndpointNamespaceLister {
	return workloadEndpointNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// WorkloadEndpointNamespaceLister helps list and get WorkloadEndpoints.
type WorkloadEndpointNamespaceLister interface {
	// List lists all WorkloadEndpoints in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha1.WorkloadEndpoint, err error)
	// Get retrieves the WorkloadEndpoint from the indexer for a given namespace and name.
	Get(name string) (*v1alpha1.WorkloadEndpoint, error)
	WorkloadEndpointNamespaceListerExpansion
}

// workloadEndpointNamespaceLister implements the WorkloadEndpointNamespaceLister
// interface.
type workloadEndpointNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all WorkloadEndpoints in the indexer for a given namespace.
func (s workloadEndpointNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.WorkloadEndpoint, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.WorkloadEndpoint))
	})
	return ret, err
}

// Get retrieves the WorkloadEndpoint from the indexer for a given namespace and name.
func (s workloadEndpointNamespaceLister) Get(name string) (*v1alpha1.WorkloadEndpoint, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("workloadendpoint"), name)
	}
	return obj.(*v1alpha1.WorkloadEndpoint), nil
}
