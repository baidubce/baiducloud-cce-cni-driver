/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package main

import (
	"context"

	"google.golang.org/grpc"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	grpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/grpc"
)

type ENIIPAM struct {
	gRPCClient grpcwrapper.Interface
}

func NewENIIPAM() *ENIIPAM {
	return &ENIIPAM{
		gRPCClient: grpcwrapper.New(),
	}
}

func (ipam *ENIIPAM) setupClientConnection(ctx context.Context, ipamServerAddress string) (*grpc.ClientConn, error) {
	// setup a connection to ipam server
	conn, err := ipam.gRPCClient.Dial(ipamServerAddress, grpc.WithInsecure())

	if err != nil {
		log.Errorf(ctx, "failed to connect to ipam server on %v: %v", ipamServerAddress, err)
		return nil, err
	}

	return conn, nil
}

func (ipam *ENIIPAM) AllocIP(ctx context.Context, k8sArgs *K8SArgs, ipamConf *IPAMConf) (*rpc.AllocateIPReply, error) {
	conn, err := ipam.setupClientConnection(ctx, ipamConf.Endpoint)
	if err != nil {
		log.Errorf(ctx, "setup a connection to ipam server failed: %v", err)
		return nil, err
	}
	defer conn.Close()

	c := rpc.NewCNIBackendClient(conn)

	var ipType rpc.IPType = rpc.IPType_BCCMultiENIMultiIPType
	if ipamConf.InstanceType == string(metadata.InstanceTypeExBBC) {
		ipType = rpc.IPType_BBCPrimaryENIMultiIPType
	}

	resp, err := c.AllocateIP(ctx, &rpc.AllocateIPRequest{
		K8SPodName:             string(k8sArgs.K8S_POD_NAME),
		K8SPodNamespace:        string(k8sArgs.K8S_POD_NAMESPACE),
		K8SPodInfraContainerID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		IPType:                 ipType,
	})
	if err != nil {
		log.Errorf(ctx, "failed to allocate ip from cni backend: %v", err)
		return nil, err
	}
	log.Infof(ctx, "allocate ip response body: %v", resp.String())
	return resp, nil
}

func (ipam *ENIIPAM) ReleaseIP(ctx context.Context, k8sArgs *K8SArgs, ipamConf *IPAMConf) (*rpc.ReleaseIPReply, error) {
	conn, err := ipam.setupClientConnection(ctx, ipamConf.Endpoint)
	if err != nil {
		log.Errorf(ctx, "setup a connection to ipam server failed: %v", err)
		return nil, err
	}
	defer conn.Close()

	// release ip from cni backend
	c := rpc.NewCNIBackendClient(conn)

	var ipType rpc.IPType = rpc.IPType_BCCMultiENIMultiIPType
	if ipamConf.InstanceType == string(metadata.InstanceTypeExBBC) {
		ipType = rpc.IPType_BBCPrimaryENIMultiIPType
	}

	resp, err := c.ReleaseIP(ctx, &rpc.ReleaseIPRequest{
		K8SPodName:             string(k8sArgs.K8S_POD_NAME),
		K8SPodNamespace:        string(k8sArgs.K8S_POD_NAMESPACE),
		K8SPodInfraContainerID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		IPType:                 ipType,
	})
	if err != nil {
		log.Errorf(ctx, "failed to release ip from cni backend: %v", err)
		return nil, err
	}
	log.Infof(ctx, "release ip response body: %v", resp.String())

	return resp, nil
}
