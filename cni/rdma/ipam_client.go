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
	"fmt"
	"math/rand"
	"time"

	"google.golang.org/grpc"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	grpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/grpc"
	rpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc"
)

const (
	rpcTimeout = 50 * time.Second
)

type roceIPAM struct {
	gRPCClient grpcwrapper.Interface
	rpc        rpcwrapper.Interface
}

var MaxRetries = 5
var InitialDelay = 5 * time.Second
var Factor = 1.0

func NewRoceIPAM(grpc grpcwrapper.Interface, rpc rpcwrapper.Interface) *roceIPAM {
	return &roceIPAM{
		gRPCClient: grpc,
		rpc:        rpc,
	}
}

// func (ipam *roceIPAM) AllocIP(ctx context.Context, k8sArgs *cni.K8SArgs, endpoint string, masterMac string) (*rpc.AllocateIPReply, error) {
// 	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
// 	defer cancel()

// 	conn, err := ipam.gRPCClient.DialContext(ctx, endpoint, grpc.WithInsecure())
// 	if err != nil {
// 		log.Errorf(ctx, "failed to connect to ipam server on %v: %v", endpoint, err)
// 		return nil, err
// 	}
// 	defer func() {
// 		if conn != nil {
// 			conn.Close()
// 		}
// 	}()

// 	c := ipam.rpc.NewCNIBackendClient(conn)

// 	resp, err := c.AllocateIP(ctx, &rpc.AllocateIPRequest{
// 		K8SPodName:             string(k8sArgs.K8S_POD_NAME),
// 		K8SPodNamespace:        string(k8sArgs.K8S_POD_NAMESPACE),
// 		K8SPodInfraContainerID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
// 		IPType:                 rpc.IPType_RoceENIMultiIPType,
// 		NetworkInfo: &rpc.AllocateIPRequest_ENIMultiIP{
// 			ENIMultiIP: &rpc.ENIMultiIPRequest{
// 				Mac: masterMac,
// 			},
// 		},
// 	})
// 	if err != nil {
// 		log.Errorf(ctx, "failed to allocate ip from cni backend: %v", err)
// 		return nil, err
// 	}
// 	log.Infof(ctx, "allocate ip response body: %v", resp.String())
// 	return resp, nil
// }

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

	return ipam.RetryWithBackoff(ctx, c, k8sArgs, masterMac, rpc.IPType_RoceENIMultiIPType, MaxRetries, InitialDelay)
}

func (ipam *roceIPAM) getRandomNum() int {
	rand.Seed(time.Now().UnixNano())
	return rand.Intn(4)
}

func (ipam *roceIPAM) RetryWithBackoff(ctx context.Context, rpcClient rpc.CNIBackendClient,
	k8sArgs *cni.K8SArgs, masterMac string, ipType rpc.IPType, maxRetries int,
	initialDelay time.Duration) (*rpc.AllocateIPReply, error) {
	delay := initialDelay

	for i := 0; i < maxRetries; i++ {
		resp, err := ipam.TryAllocateIP(ctx, rpcClient, k8sArgs, masterMac, ipType)
		if err == nil && resp.IsSuccess {
			return resp, nil
		}
		log.Infof(ctx, "retry %d failed: %v, errmsg:%v, sleep: %v\n", i+1, err, resp.ErrMsg, delay)
		time.Sleep(delay)
		delay = initialDelay + time.Duration(ipam.getRandomNum())*time.Second
	}
	return nil, fmt.Errorf("max retries exceeded")
}

// GetLocalNode is a function that returns the local k8s node struct
func (ipam *roceIPAM) TryAllocateIP(ctx context.Context, rpcClient rpc.CNIBackendClient,
	k8sArgs *cni.K8SArgs, masterMac string,
	ipType rpc.IPType) (*rpc.AllocateIPReply, error) {
	// create a config from the service account token
	resp, err := rpcClient.AllocateIP(ctx, &rpc.AllocateIPRequest{
		K8SPodName:             string(k8sArgs.K8S_POD_NAME),
		K8SPodNamespace:        string(k8sArgs.K8S_POD_NAMESPACE),
		K8SPodInfraContainerID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		IPType:                 ipType,
		NetworkInfo: &rpc.AllocateIPRequest_ENIMultiIP{
			ENIMultiIP: &rpc.ENIMultiIPRequest{
				Mac: masterMac,
			},
		},
	})
	if err != nil {
		log.Errorf(ctx, "failed to allocate ip from cni backend: %v", err)
		return resp, err
	}
	log.Infof(ctx, "allocate ip response body: %v", resp.String())

	return resp, nil
}

func (ipam *roceIPAM) ReleaseIP(ctx context.Context, k8sArgs *cni.K8SArgs, endpoint string, instanceType string) (*rpc.ReleaseIPReply, error) {
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
