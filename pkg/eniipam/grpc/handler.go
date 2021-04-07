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
	"fmt"
	"net"

	"google.golang.org/grpc"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

type ENIIPAMGrpcServer struct {
	bccipamd ipam.Interface
	bbcipamd ipam.Interface
	port     int
}

func New(bccipam ipam.Interface, bbcipam ipam.Interface, port int) *ENIIPAMGrpcServer {
	return &ENIIPAMGrpcServer{
		bccipamd: bccipam,
		bbcipamd: bbcipam,
		port:     port,
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

	name := req.K8SPodName
	namespace := req.K8SPodNamespace
	containerID := req.K8SPodInfraContainerID
	log.Infof(ctx, "====> allocate request for pod (%v %v) recv <====", namespace, name)
	log.Infof(ctx, "[Request Body]: %v", req.String())

	rpcReply := &rpc.AllocateIPReply{
		IPType: req.IPType,
	}

	// get ipamd by request type
	var ipamd ipam.Interface
	switch req.IPType {
	case rpc.IPType_BCCMultiENIMultiIPType:
		ipamd = cb.bccipamd
	case rpc.IPType_BBCPrimaryENIMultiIPType:
		ipamd = cb.bbcipamd
	default:
		ipamd = cb.bccipamd
		log.Warningf(ctx, "unknown ipType %v from cni request, assume runs in BCC", req.IPType)
	}

	// we are unlikely to hit this
	if ipamd == nil {
		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = fmt.Sprintf("unsupported ipType %v from cni request", req.IPType)
		return rpcReply, nil
	}

	wep, err := ipamd.Allocate(ctx, name, namespace, containerID)

	defer func() {
		if data, err := json.Marshal(wep); err == nil {
			log.Infof(ctx, "alloc resp: %v", string(data))
		}
		log.Infof(ctx, "====> allocate response for pod (%v %v) sent <====", namespace, name)
	}()

	if err != nil {
		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = err.Error()
	} else {
		rpcReply.IsSuccess = true
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

	return rpcReply, nil
}

func (cb *ENIIPAMGrpcServer) ReleaseIP(ctx context.Context, req *rpc.ReleaseIPRequest) (*rpc.ReleaseIPReply, error) {
	ctx = log.EnsureTraceIDInCtx(ctx)

	name := req.K8SPodName
	namespace := req.K8SPodNamespace
	containerID := req.K8SPodInfraContainerID
	log.Infof(ctx, "====> release request for pod (%v %v) recv <====", namespace, name)
	log.Infof(ctx, "[Request Body]: %v", req.String())

	rpcReply := &rpc.ReleaseIPReply{
		IPType: req.IPType,
	}

	// get ipamd by request type
	var ipamd ipam.Interface
	switch req.IPType {
	case rpc.IPType_BCCMultiENIMultiIPType:
		ipamd = cb.bccipamd
	case rpc.IPType_BBCPrimaryENIMultiIPType:
		ipamd = cb.bbcipamd
	default:
		ipamd = cb.bccipamd
		log.Warningf(ctx, "unknown ipType %v from cni request, assume runs in BCC", req.IPType)
	}

	// we are unlikely to hit this
	if ipamd == nil {
		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = fmt.Sprintf("unsupported ipType %v from cni request", req.IPType)
		return rpcReply, nil
	}

	wep, err := ipamd.Release(ctx, name, namespace, containerID)

	defer func() {
		if data, err := json.Marshal(wep); err == nil {
			log.Infof(ctx, "release resp: %v", string(data))
		}
		log.Infof(ctx, "====> release response for pod (%v %v) sent <====", namespace, name)
	}()

	if err != nil {
		rpcReply.IsSuccess = false
		rpcReply.ErrMsg = err.Error()
	} else {
		rpcReply.IsSuccess = true
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

	return rpcReply, nil
}

func (cb *ENIIPAMGrpcServer) CheckIP(ctx context.Context, req *rpc.CheckIPRequest) (*rpc.CheckIPReply, error) {
	return &rpc.CheckIPReply{}, nil
}
