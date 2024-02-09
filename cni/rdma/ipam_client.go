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
	"time"

	"google.golang.org/grpc"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	grpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/grpc"
	rpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc"
)

const (
	rpcTimeout = 90 * time.Second
)

type roceIPAM struct {
	gRPCClient grpcwrapper.Interface
	rpc        rpcwrapper.Interface
}

func NewRoceIPAM(grpc grpcwrapper.Interface, rpc rpcwrapper.Interface) *roceIPAM {
	return &roceIPAM{
		gRPCClient: grpc,
		rpc:        rpc,
	}
}

func (ipam *roceIPAM) AllocIP(ctx context.Context, k8sArgs *cni.K8SArgs, endpoint string, masterMac string) (*rpc.AllocateIPReply, error) {
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	conn, err := ipam.gRPCClient.DialContext(ctx, endpoint, grpc.WithInsecure())
	if err != nil {
		log.Errorf(ctx, "failed to connect to ipam server on %v: %v", endpoint, err)
		return nil, err
	}
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	c := ipam.rpc.NewCNIBackendClient(conn)

	resp, err := c.AllocateIP(ctx, &rpc.AllocateIPRequest{
		K8SPodName:             string(k8sArgs.K8S_POD_NAME),
		K8SPodNamespace:        string(k8sArgs.K8S_POD_NAMESPACE),
		K8SPodInfraContainerID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		IPType:                 rpc.IPType_RoceENIMultiIPType,
		NetworkInfo: &rpc.AllocateIPRequest_ENIMultiIP{
			ENIMultiIP: &rpc.ENIMultiIPRequest{
				Mac: masterMac,
			},
		},
	})
	if err != nil {
		log.Errorf(ctx, "failed to allocate ip from cni backend: %v", err)
		return nil, err
	}
	log.Infof(ctx, "allocate ip response body: %v", resp.String())
	return resp, nil
}

func (ipam *roceIPAM) ReleaseIP(ctx context.Context, k8sArgs *cni.K8SArgs, endpoint string) (*rpc.ReleaseIPReply, error) {
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	conn, err := ipam.gRPCClient.DialContext(ctx, endpoint, grpc.WithInsecure())
	if err != nil {
		log.Errorf(ctx, "failed to connect to ipam server on %v: %v", endpoint, err)
		return nil, err
	}
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	c := ipam.rpc.NewCNIBackendClient(conn)

	resp, err := c.ReleaseIP(ctx, &rpc.ReleaseIPRequest{
		K8SPodName:             string(k8sArgs.K8S_POD_NAME),
		K8SPodNamespace:        string(k8sArgs.K8S_POD_NAMESPACE),
		K8SPodInfraContainerID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		IPType:                 rpc.IPType_RoceENIMultiIPType,
	})
	if err != nil {
		log.Errorf(ctx, "failed to release ip from cni backend: %v", err)
		return nil, err
	}
	log.Infof(ctx, "release ip response body: %v", resp.String())

	return resp, nil
}
