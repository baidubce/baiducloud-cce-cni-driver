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

package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

var (
	allocateIPConcurrencyLimit = 20
	releaseIPConcurrencyLimit  = 30

	errorMsgQPSExceedLimit = "QPS exceed limit, wait another try"
)

type ENIIPAMGrpcServer struct {
	bccipamd       ipam.Interface
	bbcipamd       ipam.Interface
	eniipamd       ipam.ExclusiveEniInterface
	port           int
	allocWorkers   int
	releaseWorkers int
	debug          bool
	mutex          sync.Mutex
}

func New(
	bccipam ipam.Interface,
	bbcipam ipam.Interface,
	eniipam ipam.ExclusiveEniInterface,
	port int,
	allocLimit int,
	releaseLimit int,
	debug bool,
) *ENIIPAMGrpcServer {
	// set concurrency limit
	if allocLimit > 0 {
		allocateIPConcurrencyLimit = allocLimit
	}
	if releaseLimit > 0 {
		releaseIPConcurrencyLimit = releaseLimit
	}

	return &ENIIPAMGrpcServer{
		bccipamd: bccipam,
		bbcipamd: bbcipam,
		eniipamd: eniipam,
		port:     port,
		debug:    debug,
	}
}

type Interface interface {
	RunRPCHandler(context.Context) error
}

var _ rpc.CNIBackendServer = &ENIIPAMGrpcServer{}

func (cb *ENIIPAMGrpcServer) RunRPCHandler(ctx context.Context) error {
	address := fmt.Sprintf(":%d", cb.port)
	log.Infof(ctx, "ipam grpc server serving handler on %v", address)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Errorf(ctx, "failed to listen on GRPC port: %v", err)
		return err
	}

	grpcServer := grpc.NewServer()
	rpc.RegisterCNIBackendServer(grpcServer, cb)

	if err := grpcServer.Serve(listener); err != nil {
		log.Errorf(ctx, "failed to start server on GRPC port: %v", err)
		return err
	}

	return nil
}

func (cb *ENIIPAMGrpcServer) AllocateIP(ctx context.Context, req *rpc.AllocateIPRequest) (*rpc.AllocateIPReply, error) {
	ctx = log.EnsureTraceIDInCtx(ctx)

	const (
		rpcAPI = "AllocateIP"
	)
	var (
		t        = time.Now()
		err      error
		rpcReply = &rpc.AllocateIPReply{
			IPType: req.IPType,
		}
	)
	var (
		name        = req.K8SPodName
		namespace   = req.K8SPodNamespace
		containerID = req.K8SPodInfraContainerID
	)

	defer func(startTime time.Time) {
		metric.RPCLatency.WithLabelValues(
			metric.MetaInfo.ClusterID,
			req.IPType.String(),
			rpcAPI,
			fmt.Sprint(!rpcReply.IsSuccess),
		).Observe(metric.MsSince(t))

		if cb.debug {
			metric.RPCPerPodLatency.WithLabelValues(
				metric.MetaInfo.ClusterID,
				req.IPType.String(),
				rpcAPI,
				fmt.Sprint(!rpcReply.IsSuccess),
				namespace, name, containerID,
			).Set(metric.MsSince(t))
		}

		if !rpcReply.IsSuccess && !isRateLimitErrorMessage(rpcReply.ErrMsg) {
			metric.RPCErrorCounter.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Inc()
		}
	}(t)

	log.Infof(ctx, "====> allocate request for pod (%v %v) recv <====", namespace, name)
	log.Infof(ctx, "[Request Body]: %v", req.String())
	defer func() {
		if data, err := json.Marshal(rpcReply); err == nil {
			log.Infof(ctx, "alloc resp: %v", string(data))
		}
		log.Infof(ctx, "====> allocate response for pod (%v %v) sent <====", namespace, name)
	}()

	// get ipamd by request type
	var (
		ipamd            ipam.Interface
		crossVpcEniIpamd ipam.ExclusiveEniInterface
	)
	if req.IPType == rpc.IPType_CrossVPCENIIPType {
		crossVpcEniIpamd = cb.eniipamd
	} else {
		ipamd = cb.getIpamByIPType(ctx, req.IPType)
	}

	// request comes in
	cb.incWorker(true)
	if cb.getWorker(true) > allocateIPConcurrencyLimit {
		// request rejected
		cb.decWorker(true)
		metric.RPCRejectedCounter.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Inc()

		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = errorMsgQPSExceedLimit
		return rpcReply, nil
	}

	metric.RPCConcurrency.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Inc()
	defer metric.RPCConcurrency.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Dec()

	// allocate IP
	var (
		crossVpcEni *v1alpha1.CrossVPCEni
		wep         *v1alpha1.WorkloadEndpoint
	)

	if crossVpcEniIpamd != nil {
		crossVpcEni, err = crossVpcEniIpamd.Allocate(ctx, name, namespace, containerID)
	} else {
		wep, err = ipamd.Allocate(ctx, name, namespace, containerID)
	}

	// request completes
	cb.decWorker(true)

	if err != nil {
		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = err.Error()
	} else {
		rpcReply.IsSuccess = true
		if crossVpcEni != nil {
			rpcReply.NetworkInfo = &rpc.AllocateIPReply_CrossVPCENI{
				CrossVPCENI: &rpc.CrossVPCENIReply{
					IP:      crossVpcEni.Status.PrimaryIPAddress,
					Mac:     crossVpcEni.Status.MacAddress,
					VPCCIDR: crossVpcEni.Spec.VPCCIDR,
				},
			}
		} else {
			if wep != nil {
				rpcReply.NetworkInfo = &rpc.AllocateIPReply_ENIMultiIP{
					ENIMultiIP: &rpc.ENIMultiIPReply{
						IP:                wep.Spec.IP,
						Type:              wep.Spec.Type,
						Mac:               wep.Spec.Mac,
						Gw:                wep.Spec.Gw,
						ENIID:             wep.Spec.ENIID,
						Node:              wep.Spec.Node,
						SubnetID:          wep.Spec.SubnetID,
						EnableFixIP:       wep.Spec.EnableFixIP,
						FixIPDeletePolicy: wep.Spec.FixIPDeletePolicy,
					},
				}
			}
		}
	}

	return rpcReply, nil
}

func (cb *ENIIPAMGrpcServer) ReleaseIP(ctx context.Context, req *rpc.ReleaseIPRequest) (*rpc.ReleaseIPReply, error) {
	ctx = log.EnsureTraceIDInCtx(ctx)

	const (
		rpcAPI = "ReleaseIP"
	)
	var (
		t        = time.Now()
		err      error
		rpcReply = &rpc.ReleaseIPReply{
			IPType: req.IPType,
		}
	)
	var (
		name        = req.K8SPodName
		namespace   = req.K8SPodNamespace
		containerID = req.K8SPodInfraContainerID
	)

	defer func(startTime time.Time) {
		metric.RPCLatency.WithLabelValues(
			metric.MetaInfo.ClusterID,
			req.IPType.String(),
			rpcAPI,
			fmt.Sprint(!rpcReply.IsSuccess),
		).Observe(metric.MsSince(t))

		if !rpcReply.IsSuccess && !isRateLimitErrorMessage(rpcReply.ErrMsg) {
			metric.RPCErrorCounter.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Inc()
		}
	}(t)

	log.Infof(ctx, "====> release request for pod (%v %v) recv <====", namespace, name)
	log.Infof(ctx, "[Request Body]: %v", req.String())
	defer func() {
		if data, err := json.Marshal(rpcReply); err == nil {
			log.Infof(ctx, "release resp: %v", string(data))
		}
		log.Infof(ctx, "====> release response for pod (%v %v) sent <====", namespace, name)
	}()

	// get ipamd by request type
	var (
		ipamd            ipam.Interface
		crossVpcEniIpamd ipam.ExclusiveEniInterface
	)
	if req.IPType == rpc.IPType_CrossVPCENIIPType {
		crossVpcEniIpamd = cb.eniipamd
	} else {
		ipamd = cb.getIpamByIPType(ctx, req.IPType)
	}

	// request comes in
	cb.incWorker(false)
	if cb.getWorker(false) > releaseIPConcurrencyLimit {
		// request rejected
		cb.decWorker(false)
		metric.RPCRejectedCounter.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Inc()

		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = errorMsgQPSExceedLimit
		return rpcReply, nil
	}

	metric.RPCConcurrency.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Inc()
	defer metric.RPCConcurrency.WithLabelValues(metric.MetaInfo.ClusterID, req.IPType.String(), rpcAPI).Dec()

	var (
		crossVpcEni *v1alpha1.CrossVPCEni
		wep         *v1alpha1.WorkloadEndpoint
	)

	if crossVpcEniIpamd != nil {
		crossVpcEni, err = crossVpcEniIpamd.Release(ctx, name, namespace, containerID)
	} else {
		wep, err = ipamd.Release(ctx, name, namespace, containerID)
	}

	// request completes
	cb.decWorker(false)

	if err != nil {
		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = err.Error()
	} else {
		rpcReply.IsSuccess = true
		if crossVpcEni != nil {
			rpcReply.NetworkInfo = &rpc.ReleaseIPReply_CrossVPCENI{
				CrossVPCENI: &rpc.CrossVPCENIReply{
					IP:      crossVpcEni.Status.PrimaryIPAddress,
					Mac:     crossVpcEni.Status.MacAddress,
					VPCCIDR: crossVpcEni.Spec.VPCCIDR,
				},
			}
		} else {
			if wep != nil {
				rpcReply.NetworkInfo = &rpc.ReleaseIPReply_ENIMultiIP{
					ENIMultiIP: &rpc.ENIMultiIPReply{
						IP:                wep.Spec.IP,
						Type:              wep.Spec.Type,
						Mac:               wep.Spec.Mac,
						Gw:                wep.Spec.Gw,
						ENIID:             wep.Spec.ENIID,
						Node:              wep.Spec.Node,
						SubnetID:          wep.Spec.SubnetID,
						EnableFixIP:       wep.Spec.EnableFixIP,
						FixIPDeletePolicy: wep.Spec.FixIPDeletePolicy,
					},
				}
			}
		}
	}

	return rpcReply, nil
}

func (cb *ENIIPAMGrpcServer) CheckIP(ctx context.Context, req *rpc.CheckIPRequest) (*rpc.CheckIPReply, error) {
	return &rpc.CheckIPReply{}, nil
}

func (cb *ENIIPAMGrpcServer) getIpamByIPType(ctx context.Context, ipType rpc.IPType) ipam.Interface {
	var ipamd ipam.Interface
	switch ipType {
	case rpc.IPType_BCCMultiENIMultiIPType:
		ipamd = cb.bccipamd
	case rpc.IPType_BBCPrimaryENIMultiIPType:
		ipamd = cb.bbcipamd
	default:
		ipamd = cb.bccipamd
		log.Warningf(ctx, "unknown ipType %v from cni request, assume runs in BCC", ipType)
	}

	return ipamd
}

func (cb *ENIIPAMGrpcServer) incWorker(isAlloc bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	if isAlloc {
		cb.allocWorkers++
	}
	cb.releaseWorkers++
}

func (cb *ENIIPAMGrpcServer) decWorker(isAlloc bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	if isAlloc {
		cb.allocWorkers--
	}
	cb.releaseWorkers--
}

func (cb *ENIIPAMGrpcServer) getWorker(isAlloc bool) int {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	if isAlloc {
		return cb.allocWorkers
	}
	return cb.releaseWorkers
}

// isRateLimitErrorMessage checks error message from cni and iaas openapi perspective
func isRateLimitErrorMessage(msg string) bool {
	return msg == errorMsgQPSExceedLimit || cloud.IsErrorRateLimit(errors.New(msg))
}
