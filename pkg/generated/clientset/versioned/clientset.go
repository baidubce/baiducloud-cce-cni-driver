// Code generated by client-gen. DO NOT EDIT.

package versioned

import (
	"fmt"

	ccev1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/typed/networking/v1alpha1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/typed/networking/v2"
	discovery "k8s.io/client-go/discovery"
	rest "k8s.io/client-go/rest"
	flowcontrol "k8s.io/client-go/util/flowcontrol"
)

type Interface interface {
	Discovery() discovery.DiscoveryInterface
	CceV1alpha1() ccev1alpha1.CceV1alpha1Interface
	CceV2() ccev2.CceV2Interface
}

// Clientset contains the clients for groups. Each group has exactly one
// version included in a Clientset.
type Clientset struct {
	*discovery.DiscoveryClient
	cceV1alpha1 *ccev1alpha1.CceV1alpha1Client
	cceV2       *ccev2.CceV2Client
}

// CceV1alpha1 retrieves the CceV1alpha1Client
func (c *Clientset) CceV1alpha1() ccev1alpha1.CceV1alpha1Interface {
	return c.cceV1alpha1
}

// CceV2 retrieves the CceV2Client
func (c *Clientset) CceV2() ccev2.CceV2Interface {
	return c.cceV2
}

// Discovery retrieves the DiscoveryClient
func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	if c == nil {
		return nil
	}
	return c.DiscoveryClient
}

// NewForConfig creates a new Clientset for the given config.
// If config's RateLimiter is not set and QPS and Burst are acceptable,
// NewForConfig will generate a rate-limiter in configShallowCopy.
func NewForConfig(c *rest.Config) (*Clientset, error) {
	configShallowCopy := *c
	if configShallowCopy.RateLimiter == nil && configShallowCopy.QPS > 0 {
		if configShallowCopy.Burst <= 0 {
			return nil, fmt.Errorf("burst is required to be greater than 0 when RateLimiter is not set and QPS is set to greater than 0")
		}
		configShallowCopy.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(configShallowCopy.QPS, configShallowCopy.Burst)
	}
	var cs Clientset
	var err error
	cs.cceV1alpha1, err = ccev1alpha1.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.cceV2, err = ccev2.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}

	cs.DiscoveryClient, err = discovery.NewDiscoveryClientForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	return &cs, nil
}

// NewForConfigOrDie creates a new Clientset for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *Clientset {
	var cs Clientset
	cs.cceV1alpha1 = ccev1alpha1.NewForConfigOrDie(c)
	cs.cceV2 = ccev2.NewForConfigOrDie(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClientForConfigOrDie(c)
	return &cs
}

// New creates a new Clientset for the given RESTClient.
func New(c rest.Interface) *Clientset {
	var cs Clientset
	cs.cceV1alpha1 = ccev1alpha1.New(c)
	cs.cceV2 = ccev2.New(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClient(c)
	return &cs
}
