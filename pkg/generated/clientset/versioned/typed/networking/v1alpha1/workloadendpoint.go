// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	scheme "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// WorkloadEndpointsGetter has a method to return a WorkloadEndpointInterface.
// A group's client should implement this interface.
type WorkloadEndpointsGetter interface {
	WorkloadEndpoints(namespace string) WorkloadEndpointInterface
}

// WorkloadEndpointInterface has methods to work with WorkloadEndpoint resources.
type WorkloadEndpointInterface interface {
	Create(ctx context.Context, workloadEndpoint *v1alpha1.WorkloadEndpoint, opts v1.CreateOptions) (*v1alpha1.WorkloadEndpoint, error)
	Update(ctx context.Context, workloadEndpoint *v1alpha1.WorkloadEndpoint, opts v1.UpdateOptions) (*v1alpha1.WorkloadEndpoint, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.WorkloadEndpoint, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.WorkloadEndpointList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.WorkloadEndpoint, err error)
	WorkloadEndpointExpansion
}

// workloadEndpoints implements WorkloadEndpointInterface
type workloadEndpoints struct {
	client rest.Interface
	ns     string
}

// newWorkloadEndpoints returns a WorkloadEndpoints
func newWorkloadEndpoints(c *CceV1alpha1Client, namespace string) *workloadEndpoints {
	return &workloadEndpoints{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the workloadEndpoint, and returns the corresponding workloadEndpoint object, and an error if there is any.
func (c *workloadEndpoints) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.WorkloadEndpoint, err error) {
	result = &v1alpha1.WorkloadEndpoint{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("workloadendpoints").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of WorkloadEndpoints that match those selectors.
func (c *workloadEndpoints) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.WorkloadEndpointList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.WorkloadEndpointList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("workloadendpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested workloadEndpoints.
func (c *workloadEndpoints) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("workloadendpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a workloadEndpoint and creates it.  Returns the server's representation of the workloadEndpoint, and an error, if there is any.
func (c *workloadEndpoints) Create(ctx context.Context, workloadEndpoint *v1alpha1.WorkloadEndpoint, opts v1.CreateOptions) (result *v1alpha1.WorkloadEndpoint, err error) {
	result = &v1alpha1.WorkloadEndpoint{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("workloadendpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(workloadEndpoint).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a workloadEndpoint and updates it. Returns the server's representation of the workloadEndpoint, and an error, if there is any.
func (c *workloadEndpoints) Update(ctx context.Context, workloadEndpoint *v1alpha1.WorkloadEndpoint, opts v1.UpdateOptions) (result *v1alpha1.WorkloadEndpoint, err error) {
	result = &v1alpha1.WorkloadEndpoint{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("workloadendpoints").
		Name(workloadEndpoint.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(workloadEndpoint).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the workloadEndpoint and deletes it. Returns an error if one occurs.
func (c *workloadEndpoints) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("workloadendpoints").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *workloadEndpoints) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("workloadendpoints").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched workloadEndpoint.
func (c *workloadEndpoints) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.WorkloadEndpoint, err error) {
	result = &v1alpha1.WorkloadEndpoint{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("workloadendpoints").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
