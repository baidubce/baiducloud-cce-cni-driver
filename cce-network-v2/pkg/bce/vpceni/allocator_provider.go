/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */
package vpceni

import (
	"context"
	"fmt"
	"time"

	operatorMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	bceoption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/allocator"
	ipamMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listv1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
	listv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

var (
	log = logging.NewSubysLogger("bce-allocator")
)

type BCEAllocatorProvider struct {
	// client to get k8s objects
	enilister listv2.ENILister
	sbnlister listv1.SubnetLister

	// bceClient to get bce objects
	bceclient cloud.Interface

	// manager all of instances
	manager *InstancesManager
}

type VpcEniNetResourceSetEventHandler struct {
	// realHandler is the real handler to handle node event
	realHandler allocator.NetResourceSetEventHandler
}

// Init implements allocator.AllocatorProvider
func (provider *BCEAllocatorProvider) Init(ctx context.Context) error {
	provider.enilister = k8s.CCEClient().Informers.Cce().V2().ENIs().Lister()
	provider.sbnlister = k8s.CCEClient().Informers.Cce().V1().Subnets().Lister()
	provider.bceclient = bceoption.BCEClient()
	provider.manager = newInstancesManager(provider.bceclient, provider.enilister, provider.sbnlister, watchers.CCEEndpointClient)
	return nil
}

// Start implements allocator.AllocatorProvider
func (provider *BCEAllocatorProvider) Start(ctx context.Context, getterUpdater ipam.NetResourceSetGetterUpdater) (allocator.NetResourceSetEventHandler, error) {
	log.Info("Starting  Baidu BCE allocator...")

	if ipamMetrics.IMetrics == nil {
		if operatorOption.Config.EnableMetrics {
			ipamMetrics.IMetrics = ipamMetrics.NewPrometheusMetrics(operatorMetrics.Namespace, operatorMetrics.Registry)
		} else {
			ipamMetrics.IMetrics = &ipamMetrics.NoOpMetrics{}
		}
	}
	provider.manager.nrsGetterUpdater = getterUpdater

	netResourceSetManager, err := ipam.NewNetResourceSetManager(provider.manager, getterUpdater, ipamMetrics.IMetrics,
		operatorOption.Config.ParallelAllocWorkers, true, false)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize bce instance manager: %w", err)
	}

	if err := netResourceSetManager.Start(ctx); err != nil {
		return nil, err
	}

	vpcEniNetResourceSetEventHandler := &VpcEniNetResourceSetEventHandler{}
	vpcEniNetResourceSetEventHandler.realHandler = netResourceSetManager

	return vpcEniNetResourceSetEventHandler, nil
}

// StartEndpointManager implements endpoint.DirectAllocatorStarter
func (provider *BCEAllocatorProvider) StartEndpointManager(ctx context.Context, getterUpdater endpoint.CCEEndpointGetterUpdater) (endpoint.EndpointEventHandler, error) {

	endpointNanager := endpoint.NewEndpointManager(getterUpdater, provider.manager)
	endpointNanager.Start(ctx)
	return endpointNanager, nil
}

// Create implements allocator.NetResourceSetEventHandler
func (handler *VpcEniNetResourceSetEventHandler) Create(resource *ccev2.NetResourceSet) error {
	// Only processing the resource when it is not RDMA
	if bceutils.IsRdmaNetResourceSet(resource.Name) {
		return nil
	}

	return handler.realHandler.Create(resource)
}

// Delete implements allocator.NetResourceSetEventHandler
func (handler *VpcEniNetResourceSetEventHandler) Delete(netResourceSetName string) error {
	// Only processing the resource when it is not RDMA
	if bceutils.IsRdmaNetResourceSet(netResourceSetName) {
		return nil
	}

	return handler.realHandler.Delete(netResourceSetName)
}

// Update implements allocator.NetResourceSetEventHandler
func (handler *VpcEniNetResourceSetEventHandler) Update(resource *ccev2.NetResourceSet) error {
	// Only processing the resource when it is not RDMA
	if bceutils.IsRdmaNetResourceSet(resource.Name) {
		return nil
	}

	return handler.realHandler.Update(resource)
}

// Resync implements allocator.NetResourceSetEventHandler
func (handler *VpcEniNetResourceSetEventHandler) Resync(context.Context, time.Time) {
	handler.realHandler.Resync(context.Background(), time.Now())
}

var _ allocator.AllocatorProvider = &BCEAllocatorProvider{}

var _ endpoint.DirectAllocatorStarter = &BCEAllocatorProvider{}
