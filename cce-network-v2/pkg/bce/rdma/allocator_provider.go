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
package rdma

import (
	"context"
	"fmt"
	"time"

	operatorMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	bceoption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma/client"
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
	log = logging.NewSubysLogger("bce-rdma-allocator")
)

type BCERDMAAllocatorProvider struct {
	// client to get k8s objects
	enilister listv2.ENILister
	sbnlister listv1.SubnetLister

	// bceClient to get bce objects
	bceClient cloud.Interface

	//iaasClient client.IaaSClient
	eriClient *client.EriClient
	hpcClient *client.HpcClient

	// manager all of instances
	manager *rdmaInstancesManager
}

type RdmaNetResourceSetEventHandler struct {
	// realHandler is the real handler to handle node event
	realHandler allocator.NetResourceSetEventHandler
}

// Init implements allocator.AllocatorProvider
func (provider *BCERDMAAllocatorProvider) Init(ctx context.Context) error {
	provider.enilister = k8s.CCEClient().Informers.Cce().V2().ENIs().Lister()
	provider.sbnlister = k8s.CCEClient().Informers.Cce().V1().Subnets().Lister()
	bceClient := bceoption.BCEClient()
	provider.bceClient = bceClient
	provider.eriClient = client.NewEriClient(bceClient)
	provider.hpcClient = client.NewHpcClient(bceClient)
	provider.manager = newInstancesManager(provider.bceClient, provider.eriClient, provider.hpcClient, provider.enilister, provider.sbnlister, watchers.CCEEndpointClient)
	return nil
}

// Start implements allocator.AllocatorProvider
func (provider *BCERDMAAllocatorProvider) Start(ctx context.Context, getterUpdater ipam.NetResourceSetGetterUpdater) (allocator.NetResourceSetEventHandler, error) {
	log.Info("Starting  Baidu BCE RDMA allocator...")

	if ipamMetrics.IMetrics == nil {
		if operatorOption.Config.EnableMetrics {
			ipamMetrics.IMetrics = ipamMetrics.NewPrometheusMetrics(operatorMetrics.Namespace, operatorMetrics.Registry)
		} else {
			ipamMetrics.IMetrics = &ipamMetrics.NoOpMetrics{}
		}
	}
	provider.manager.nrsGetterUpdater = getterUpdater

	netResourceSetManager, err := ipam.NewNetResourceSetManager(provider.manager, getterUpdater, ipamMetrics.IMetrics,
		operatorOption.Config.RdmaResourceResyncWorkers, operatorOption.Config.EnableExcessIPRelease, false)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize bce rdma instance manager: %w", err)
	}

	if err := netResourceSetManager.Start(ctx); err != nil {
		return nil, err
	}

	rdmaNetResourceSetEventHandler := &RdmaNetResourceSetEventHandler{}
	rdmaNetResourceSetEventHandler.realHandler = netResourceSetManager

	return rdmaNetResourceSetEventHandler, nil
}

// Create implements allocator.NodeEventHandler
func (handler *RdmaNetResourceSetEventHandler) Create(resource *ccev2.NetResourceSet) error {
	return handler.realHandler.Create(resource)
}

// Delete implements allocator.NodeEventHandler
func (handler *RdmaNetResourceSetEventHandler) Delete(netResourceSetName string) error {
	return handler.realHandler.Delete(netResourceSetName)
}

// Update implements allocator.NodeEventHandler
func (handler *RdmaNetResourceSetEventHandler) Update(resource *ccev2.NetResourceSet) error {
	return handler.realHandler.Update(resource)
}

// Resync implements allocator.NodeEventHandler
func (handler *RdmaNetResourceSetEventHandler) Resync(context.Context, time.Time) {
	handler.realHandler.Resync(context.Background(), time.Now())
}

func (handler *RdmaNetResourceSetEventHandler) ResourceType() string {
	return ccev2.NetResourceSetEventHandlerTypeRDMA
}

var _ allocator.AllocatorProvider = &BCERDMAAllocatorProvider{}
