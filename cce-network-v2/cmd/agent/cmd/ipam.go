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

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	ipamapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
)

var (
	ipamLog = log.WithField("module", "ipam-handler")
)

type postIPAM struct {
	daemon *Daemon
}

// NewPostIPAMHandler creates a new postIPAM from the daemon.
func NewPostIPAMHandler(d *Daemon) ipamapi.PostIpamHandler {
	return &postIPAM{daemon: d}
}

// Handle incoming requests address allocation requests for the daemon.
func (h *postIPAM) Handle(params ipamapi.PostIpamParams) middleware.Responder {
	var (
		err   error
		limit rate.LimitedRequest

		owner       = swag.StringValue(params.Owner)
		containerID = swag.StringValue(params.ContainerID)
		netns       = swag.StringValue(params.Netns)
		family      = strings.ToLower(swag.StringValue(params.Family))
		resp        *models.IPAMResponse
		scopeLog    = ipamLog.WithField("owner", owner).WithField("containerID", containerID).WithField("netns", netns)
	)

	// api rate limit
	ctx, cancel := context.WithTimeout(context.Background(), defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = h.daemon.apiLimiterSet.Wait(ctx, apiRequestPostIPAM)
	if err != nil {
		return api.Error(ipamapi.PostIpamFailureCode, err)
	}
	defer func() {
		limit.Error(err)
		if err != nil {
			scopeLog.WithError(err).Error("allocate next ip error")
		} else {
			scopeLog.WithField("response", logfields.Repr(resp)).Info("allocate next ip success")
		}
	}()

	ipv4Result, ipv6Result, err := h.daemon.ipam.ADD(family, owner, containerID, netns)
	if err != nil {
		scopeLog.WithError(err).Error("allocate next ip error")
		return api.Error(ipamapi.PostIpamFailureCode, err)
	}

	resp = &models.IPAMResponse{
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
	scopeLog.WithField("resp", logfields.Repr(resp)).Debug("allocate next ip success")
	return ipamapi.NewPostIpamCreated().WithPayload(resp)
}

type postIPAMIP struct {
	daemon *Daemon
}

// NewPostIPAMIPHandler creates a new postIPAM from the daemon.
func NewPostIPAMIPHandler(d *Daemon) ipamapi.PostIpamIPHandler {
	return &postIPAMIP{
		daemon: d,
	}
}

// Handle incoming requests address allocation requests for the daemon.
func (h *postIPAMIP) Handle(params ipamapi.PostIpamIPParams) middleware.Responder {
	return ipamapi.NewPostIpamIPOK()
}

type deleteIPAMIP struct {
	daemon *Daemon
}

// NewDeleteIPAMIPHandler handle incoming requests to delete addresses.
func NewDeleteIPAMIPHandler(d *Daemon) ipamapi.DeleteIpamIPHandler {
	return &deleteIPAMIP{daemon: d}
}

func (h *deleteIPAMIP) Handle(params ipamapi.DeleteIpamIPParams) middleware.Responder {
	var (
		err   error
		limit rate.LimitedRequest

		owner       = swag.StringValue(params.Owner)
		containerID = swag.StringValue(params.ContainerID)
	)

	// api rate limit
	ctx, cancel := context.WithTimeout(context.Background(), defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = h.daemon.apiLimiterSet.Wait(ctx, apiRequestDeleteIPAMIP)
	if err != nil {
		return api.Error(ipamapi.DeleteIpamIPFailureCode, err)
	}
	defer func() {
		limit.Error(err)
		scopeLog := ipamLog.WithField("owner", owner).WithField("containerID", containerID)
		if err != nil {
			scopeLog.WithError(err).Error("delete ip error")
		} else {
			scopeLog.Info("delete ip success")
		}
	}()

	if err := h.daemon.ipam.DEL(owner, containerID); err != nil {
		return api.Error(ipamapi.DeleteIpamIPFailureCode, err)
	}

	return ipamapi.NewDeleteIpamIPOK()
}

// DumpIPAM dumps in the form of a map, the list of
// reserved IPv4 and IPv6 addresses.
func (d *Daemon) DumpIPAM() *models.IPAMStatus {
	allocv4, allocv6, st := d.ipam.Dump()
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

	return status
}

func (d *Daemon) configureIPAM() {
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
			log.WithError(err).WithField(logfields.V4Prefix, option.Config.IPv4Range).Fatal("Invalid IPv4 allocation prefix")
		}
		node.SetIPv4AllocRange(allocCIDR)
	}

	if option.Config.IPv6Range != AutoCIDR {
		allocCIDR, err := cidr.ParseCIDR(option.Config.IPv6Range)
		if err != nil {
			log.WithError(err).WithField(logfields.V6Prefix, option.Config.IPv6Range).Fatal("Invalid IPv6 allocation prefix")
		}

		node.SetIPv6NodeRange(allocCIDR)
	}

	if err := node.AutoComplete(); err != nil {
		log.WithError(err).Fatal("Cannot autocomplete node addresses")
	}
}

func (d *Daemon) startIPAM() {
	bootstrapStats.ipam.Start()
	log.Info("Initializing ipam")
	// Set up ipam conf after init() because we might be running d.conf.KVStoreIPv4Registration
	d.ipam = endpoint.NewIPAM(d.nodeAddressing, option.Config, d.nodeDiscovery, d.k8sWatcher, nil)
	bootstrapStats.ipam.End(true)
}
