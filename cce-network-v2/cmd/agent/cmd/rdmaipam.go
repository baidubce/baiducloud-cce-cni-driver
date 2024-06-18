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

package cmd

import (
	"context"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/mohae/deepcopy"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	rdmaipamapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/rdmaipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/api"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
)

var (
	rdmaIpamLog = log.WithField("module", "rdmaipam-handler")
	rdmaIfMaps  = map[string]bceutils.RdmaIfInfo{}
)

type postRDMAIPAM struct {
	daemon *Daemon
}

// NewPostIPAMHandler creates a new postIPAM from the daemon.
func NewPostRDMAIPAMHandler(d *Daemon) rdmaipamapi.PostRdmaipamHandler {
	return &postRDMAIPAM{daemon: d}
}

// Handle incoming requests address allocation requests for the daemon.
func (h *postRDMAIPAM) Handle(params rdmaipamapi.PostRdmaipamParams) middleware.Responder {
	var (
		err    error
		allErr []error
		limit  rate.LimitedRequest

		owner       = swag.StringValue(params.Owner)
		containerID = swag.StringValue(params.ContainerID)
		netns       = swag.StringValue(params.Netns)
		family      = strings.ToLower(swag.StringValue(params.Family))
		response    = &rdmaipamapi.PostRdmaipamCreated{}
		resp        *models.RDMAIPAMResponse
		scopeLog    = rdmaIpamLog.WithField("owner", owner).WithField("containerID", containerID).WithField("netns", netns)
	)

	// api rate limit
	ctx, cancel := context.WithTimeout(context.Background(), defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = h.daemon.apiLimiterSet.Wait(ctx, apiRequestPostIPAM)
	if err != nil {
		return api.Error(rdmaipamapi.PostRdmaipamFailureCode, err)
	}
	defer func() {
		limit.Error(err)
		if err != nil {
			scopeLog.WithError(err).Error("allocate rdma ips error")
		} else {
			scopeLog.WithField("response", logfields.Repr(resp)).Info("allocate rdma ips success")
		}
	}()

	for masterMac, ri := range h.daemon.rdmaIpam {
		var ipv4Result, ipv6Result *ipam.AllocationResult
		ipv4Result, ipv6Result, err = ri.ADD(family, owner, containerID, netns)
		if err != nil {
			scopeLog.WithError(err).Error("allocate next rdma ip error")
			allErr = append(allErr, err)
			goto DELAPPLIED
		}

		if ipv4Result != nil {
			ipv4Result.PrimaryMAC = masterMac
		}

		resp = &models.RDMAIPAMResponse{
			HostAddressing: node.GetNodeAddressing(),
			Address:        &models.AddressPair{},
		}

		if ipv4Result != nil {
			resp.Address.IPV4 = ipv4Result.IP.String()
			resp.IPV4 = &models.IPAMAddressResponse{
				Cidrs:           ipv4Result.CIDRs,
				IP:              ipv4Result.IP.String(),
				MasterMac:       ipv4Result.PrimaryMAC,
				Gateway:         ipv4Result.GatewayIP,
				ExpirationUUID:  ipv4Result.ExpirationUUID,
				InterfaceNumber: ipv4Result.InterfaceNumber,
			}
		}

		if ipv6Result != nil {
			resp.Address.IPV6 = ipv6Result.IP.String()
			resp.IPV6 = &models.IPAMAddressResponse{
				Cidrs:           ipv6Result.CIDRs,
				IP:              ipv6Result.IP.String(),
				MasterMac:       ipv6Result.PrimaryMAC,
				Gateway:         ipv6Result.GatewayIP,
				ExpirationUUID:  ipv6Result.ExpirationUUID,
				InterfaceNumber: ipv6Result.InterfaceNumber,
			}
		}
		scopeLog.WithField("resp", logfields.Repr(resp)).WithField("ipv4", resp.Address.IPV4).WithField("ipv6", resp.Address.IPV6).Debug("allocate next rdma ip success")
		response.Payload = append(response.Payload, resp)
	}
	scopeLog.WithField("response", logfields.Repr(response)).Debug("allocate rdma ips success")
	return rdmaipamapi.NewPostRdmaipamCreated().WithPayload(response.Payload)

DELAPPLIED:
	for _, resp := range response.Payload {
		for masterMac, ri := range h.daemon.rdmaIpam {
			if (resp.IPV4 != nil && resp.IPV4.MasterMac == masterMac) || (resp.IPV6 != nil && resp.IPV6.MasterMac == masterMac) {
				if err = ri.DEL(owner, containerID); err != nil {
					scopeLog.WithField("ipv4", resp.Address.IPV4).WithField("ipv6", resp.Address.IPV6).WithError(err).Error("delete rdma ip error")
					allErr = append(allErr, err)
				}
				break
			}
		}
	}

	if len(allErr) > 0 {
		var errMsg string
		for _, err := range allErr {
			errMsg = errMsg + "[error: " + err.Error() + "]"
		}
		return api.Error(rdmaipamapi.DeleteRdmaipamRdmaipsFailureCode, api.New(rdmaipamapi.DeleteRdmaipamRdmaipsFailureCode, errMsg))
	}

	return api.Error(rdmaipamapi.PostRdmaipamFailureCode, err)
}

type deleteRDMAIPAMIP struct {
	daemon *Daemon
}

// NewDeleteIPAMIPHandler handle incoming requests to delete addresses.
func NewDeleteRDMAIPAMIPHandler(d *Daemon) rdmaipamapi.DeleteRdmaipamRdmaipsHandler {
	return &deleteRDMAIPAMIP{daemon: d}
}

func (h *deleteRDMAIPAMIP) Handle(params rdmaipamapi.DeleteRdmaipamRdmaipsParams) middleware.Responder {
	var (
		err    error
		delErr []error
		limit  rate.LimitedRequest

		owner       = swag.StringValue(params.Owner)
		containerID = swag.StringValue(params.ContainerID)
		scopeLog    = rdmaIpamLog.WithField("owner", owner).WithField("containerID", containerID)
	)

	// api rate limit
	ctx, cancel := context.WithTimeout(context.Background(), defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = h.daemon.apiLimiterSet.Wait(ctx, apiRequestDeleteIPAMIP)
	if err != nil {
		return api.Error(rdmaipamapi.DeleteRdmaipamRdmaipsFailureCode, err)
	}
	defer func() {
		limit.Error(err)
		if err != nil {
			scopeLog.WithError(err).Error("delete rdma ips error")
		} else {
			scopeLog.Info("delete rdma ips success")
		}
	}()

	for masterMac, ri := range h.daemon.rdmaIpam {
		if err = ri.DEL(owner, containerID); err != nil {
			scopeLog.WithField("masterMacAddress", masterMac).WithError(err).Error("delete rdma ip error")
			delErr = append(delErr, err)
		}
	}

	if len(delErr) > 0 {
		var errMsg string
		for _, err := range delErr {
			errMsg = errMsg + "[error: " + err.Error() + "]"
		}
		return api.Error(rdmaipamapi.DeleteRdmaipamRdmaipsFailureCode, api.New(rdmaipamapi.DeleteRdmaipamRdmaipsFailureCode, errMsg))
	}

	return rdmaipamapi.NewDeleteRdmaipamRdmaipsOK()
}

// DumpRDMAIPAM dumps in the form of a map, the list of
// reserved IPv4 and IPv6 addresses.
func (d *Daemon) DumpRDMAIPAM() (statusMap map[string]*models.IPAMStatus) {
	for key, ri := range d.rdmaIpam {
		allocv4, allocv6, st := ri.Dump()
		status := &models.IPAMStatus{
			Status: st,
		}

		v4 := make([]string, 0, len(allocv4))
		for ip := range allocv4 {
			v4 = append(v4, ip)
		}

		v6 := make([]string, 0, len(allocv6))
		if allocv4 == nil {
			allocv4 = map[string]string{}
		}
		for ip, owner := range allocv6 {
			v6 = append(v6, ip)
			// merge allocv6 into allocv4
			allocv4[ip] = owner
		}

		if option.Config.EnableIPv4 {
			status.IPV4 = v4
		}

		if option.Config.EnableIPv6 {
			status.IPV6 = v6
		}

		status.Allocations = allocv4

		statusMap[key] = status
	}

	return statusMap
}

func (d *Daemon) configureRDMAIPAM() {
	if !option.Config.EnableRDMA {
		rdmaIpamLog.Info("RDMA is not enabled, skipping rdma node discovery")
		return
	}

	// If the device has been specified, the IPv4AllocPrefix and the
	// IPv6AllocPrefix were already allocated before the k8s.Init().
	//
	// If the device hasn't been specified, k8s.Init() allocated the
	// IPv4AllocPrefix and the IPv6AllocPrefix from k8s node annotations.
	//
	// If k8s.Init() failed to retrieve the IPv4AllocPrefix we can try to derive
	// it from an existing node_config.h file or from previous cce_host
	// interfaces.
	//
	// Then, we will calculate the IPv4 or IPv6 alloc prefix based on the IPv6
	// or IPv4 alloc prefix, respectively, retrieved by k8s node annotations.
	if option.Config.IPv4Range != AutoCIDR {
		allocCIDR, err := cidr.ParseCIDR(option.Config.IPv4Range)
		if err != nil {
			rdmaIpamLog.WithError(err).WithField(logfields.V4Prefix, option.Config.IPv4Range).Fatal("Invalid IPv4 allocation prefix")
		}
		node.SetIPv4AllocRange(allocCIDR)
	}

	if option.Config.IPv6Range != AutoCIDR {
		allocCIDR, err := cidr.ParseCIDR(option.Config.IPv6Range)
		if err != nil {
			rdmaIpamLog.WithError(err).WithField(logfields.V6Prefix, option.Config.IPv6Range).Fatal("Invalid IPv6 allocation prefix")
		}

		node.SetIPv6NodeRange(allocCIDR)
	}

	if err := node.AutoComplete(); err != nil {
		rdmaIpamLog.WithError(err).Fatal("Cannot autocomplete node addresses")
	}
}

func (d *Daemon) startRDMAIPAM() {
	if !option.Config.EnableRDMA {
		rdmaIpamLog.Info("RDMA is not enabled, skipping rdma node discovery")
		return
	}

	if d.hasRoCE() {
		bootstrapStats.rdmaIpam.Start()
		rdmaIpamLog.Info("Initializing rdmaIpam")

		d.rdmaIpam = make(map[string]ipam.CNIIPAMServer)
		// one roce interface one rdmaIpam instance
		for _, rii := range rdmaIfMaps {
			cpy := deepcopy.Copy(option.Config)
			config := cpy.(*option.DaemonConfig)
			config.IPAM = ipamOption.IPAMRdma
			// Set up ipam conf after init() because we might be running d.conf.KVStoreIPv4Registration
			d.rdmaIpam[rii.MacAddress] = endpoint.NewIPAM(rii.NetResourceSetName, true, rii.MacAddress, d.nodeAddressing, config, d.rdmaDiscovery, d.k8sWatcher, nil)
		}
		bootstrapStats.rdmaIpam.End(true)
	}
}

func (d *Daemon) hasRoCE() bool {
	var err error

	nodeName := nodeTypes.GetName()
	rdmaIfMaps, err = bceutils.GetRdmaIFsInfo(nodeName, rdmaIpamLog)
	if err != nil {
		rdmaIpamLog.Errorf("get Roce NetResourceSetName(s) failed: %v", err)
		return false
	}

	if len(rdmaIfMaps) == 0 {
		rdmaIpamLog.Info("No Roce vif found, skip rdmaIpam initialization.")
		return false
	}
	return true
}
