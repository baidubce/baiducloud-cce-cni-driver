// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// MultiIPWorkloadEndpointLister helps list MultiIPWorkloadEndpoints.
type MultiIPWorkloadEndpointLister interface {
	// List lists all MultiIPWorkloadEndpoints in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.MultiIPWorkloadEndpoint, err error)
	// MultiIPWorkloadEndpoints returns an object that can list and get MultiIPWorkloadEndpoints.
	MultiIPWorkloadEndpoints(namespace string) MultiIPWorkloadEndpointNamespaceLister
	MultiIPWorkloadEndpointListerExpansion
}

// multiIPWorkloadEndpointLister implements the MultiIPWorkloadEndpointLister interface.
type multiIPWorkloadEndpointLister struct {
	indexer cache.Indexer
}

// NewMultiIPWorkloadEndpointLister returns a new MultiIPWorkloadEndpointLister.
func NewMultiIPWorkloadEndpointLister(indexer cache.Indexer) MultiIPWorkloadEndpointLister {
	return &multiIPWorkloadEndpointLister{indexer: indexer}
}

// List lists all MultiIPWorkloadEndpoints in the indexer.
func (s *multiIPWorkloadEndpointLister) List(selector labels.Selector) (ret []*v1alpha1.MultiIPWorkloadEndpoint, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.MultiIPWorkloadEndpoint))
	})
	return ret, err
}

// MultiIPWorkloadEndpoints returns an object that can list and get MultiIPWorkloadEndpoints.
func (s *multiIPWorkloadEndpointLister) MultiIPWorkloadEndpoints(namespace string) MultiIPWorkloadEndpointNamespaceLister {
	return multiIPWorkloadEndpointNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// MultiIPWorkloadEndpointNamespaceLister helps list and get MultiIPWorkloadEndpoints.
type MultiIPWorkloadEndpointNamespaceLister interface {
	// List lists all MultiIPWorkloadEndpoints in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha1.MultiIPWorkloadEndpoint, err error)
	// Get retrieves the MultiIPWorkloadEndpoint from the indexer for a given namespace and name.
	Get(name string) (*v1alpha1.MultiIPWorkloadEndpoint, error)
	MultiIPWorkloadEndpointNamespaceListerExpansion
}

// multiIPWorkloadEndpointNamespaceLister implements the MultiIPWorkloadEndpointNamespaceLister
// interface.
type multiIPWorkloadEndpointNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all MultiIPWorkloadEndpoints in the indexer for a given namespace.
func (s multiIPWorkloadEndpointNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.MultiIPWorkloadEndpoint, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.MultiIPWorkloadEndpoint))
	})
	return ret, err
}

// Get retrieves the MultiIPWorkloadEndpoint from the indexer for a given namespace and name.
func (s multiIPWorkloadEndpointNamespaceLister) Get(name string) (*v1alpha1.MultiIPWorkloadEndpoint, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("multiipworkloadendpoint"), name)
	}
	return obj.(*v1alpha1.MultiIPWorkloadEndpoint), nil
}