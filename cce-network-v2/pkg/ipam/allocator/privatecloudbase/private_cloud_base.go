package privatecloudbase

import (
	"context"
	"fmt"

	operatorMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/allocator"
	ipamMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	openapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase/api"
)

var (
	componentName = "ipam-allocator-private-cloud-base"
	log           = logging.DefaultLogger.WithField(logfields.LogSubsys, componentName)
)

// AllocatorPrivateCloudBase is an implementation of IPAM allocator interface for PrivateCloudBas baidu private cloud base
type AllocatorPrivateCloudBase struct {
	client api.Client

	instances *openapi.InstancesManager
}

// Init sets up ENI limits based on given options
func (a *AllocatorPrivateCloudBase) Init(ctx context.Context) error {

	var err error

	a.client, err = api.NewClient(operatorOption.Config.BCECloudBaseHost, operatorOption.Config.BCECloudRegion, componentName)
	if err != nil {
		log.Errorf("create client error %v", err)
	}
	return nil
}

// Start kicks off ENI allocation, the initial connection to Private Cloud Base
// APIs is done in a blocking manner. Provided this is successful, a controller is
// started to manage allocation based on NetResourceSet custom resources
func (a *AllocatorPrivateCloudBase) Start(ctx context.Context, getterUpdater ipam.NetResourceSetGetterUpdater) (allocator.NetResourceSetEventHandler, error) {
	var iMetrics ipam.MetricsAPI

	log.Info("Starting PrivateCloudBase allocator...")

	if operatorOption.Config.EnableMetrics {
		iMetrics = ipamMetrics.NewPrometheusMetrics(operatorMetrics.Namespace, operatorMetrics.Registry)
	} else {
		iMetrics = &ipamMetrics.NoOpMetrics{}
	}
	a.instances = openapi.NewInstancesManager(a.client, getterUpdater)
	nodeManager, err := ipam.NewNetResourceSetManager(a.instances, getterUpdater, iMetrics,
		operatorOption.Config.ParallelAllocWorkers, true, false)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize PrivateCloudBase node manager: %w", err)
	}

	if err := nodeManager.Start(ctx); err != nil {
		return nil, err
	}

	return nodeManager, nil
}

func (a *AllocatorPrivateCloudBase) StartEndpointManager(ctx context.Context, getterUpdater endpoint.CCEEndpointGetterUpdater) (endpoint.EndpointEventHandler, error) {
	em := endpoint.NewEndpointManager(getterUpdater, a.instances)
	return em, em.Start(ctx)
}
