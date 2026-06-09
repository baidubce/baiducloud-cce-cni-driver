// Copyright Authors of Baidu AI Cloud
// SPDX-License-Identifier: Apache-2.0

package enim

import (
	"errors"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/agent"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamopt "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

var (
	enimLog           = logging.NewSubysLogger("enim")
	getRdmaIFsInfoAll = bceutils.GetRdmaIFsInfoAll
)

// ENIManagerServer It is mainly used when assigning ENI manager,
// which is mainly applicable to the scenario of exclusive ENI
type ENIManagerServer interface {
	// ADD select an available ENI from the ENI list and assign it to the
	// network endpoint
	ADD(owner, containerID, netnsPath string) (ipv4Addr, ipv6Addr *models.IPAMAddressResponse, err error)

	// DEL  recycle the endpoint has been recycled and needs to trigger the
	// reuse of ENI
	DEL(owner, containerID, netnsPath string) error

	// GC the eni manager needs to implement the garbage cleaning logic for
	// the endpoint. When the endpoint no longer exists, it needs to trigger
	// the ENI release logic.
	// Warning When releasing ENI, it also involves device management. It is
	// necessary to move the network device put into the container namespace
	// to the host namespace, and rename the device when moving.
	GC() error
}

// NewENIManager creates a new ENIManager instance.
// The eni package will wswitch by type of IPAM
// The ENI manager determines which method to use to manage ENI based on the
//
//	type of eni used by the current node:
//
// If Secondary mode is currently used, policy routing will be configured for ENI
// If the Primary mode is used, the management of endpoint association relationships
// will be provided for ENI
func NewENIManager(c ipam.Configuration, watcher *watchers.K8sWatcher) ENIManagerServer {
	switch option.Config.IPAM {
	case ipamopt.IPAMVpcEni:
		if option.Config.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
			agent.RegisterENIInitFatory(watcher)
			return newENIEndpointAllocator(option.Config, watcher)
		}
		agent.RegisterENIInitFatory(watcher)
		return &noopENIM{}
	case ipamopt.IPAMVpcRoute:
		enable, err := shouldEnableENIInitFactoryForVpcRoute()
		if err != nil {
			enimLog.WithError(err).Fatal("failed to detect RDMA interfaces in vpc-route mode")
			return &noopENIM{}
		}
		if !enable {
			return &noopENIM{}
		}
		return NewENIInitFactoryENIM(c, watcher)
	default:
		return &noopENIM{}
	}
}

func shouldEnableENIInitFactoryForVpcRoute() (bool, error) {
	rdmaIFs, err := getRdmaIFsInfoAll(nodeTypes.GetName(), enimLog)
	if err != nil {
		if errors.Is(err, metadata.ErrorNotImplemented) {
			enimLog.WithError(err).Info("RDMA metadata not implemented, skip ENI init factory in vpc-route mode")
			return false, nil
		}
		return false, err
	}

	if len(rdmaIFs) == 0 {
		enimLog.Info("no RDMA interface detected in vpc-route mode, skip ENI init factory")
		return false, nil
	}

	return true, nil
}
