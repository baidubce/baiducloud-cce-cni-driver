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

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/enim"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	eniapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

var (
	eniLog = log.WithField("module", "enim-handler")
)

type postENI struct {
	daemon *Daemon
}

// NewPostEniHandler creates a new postENI from the daemon.
func NewPostEniHandler(d *Daemon) eniapi.PostEniHandler {
	return &postENI{daemon: d}
}

// Handle incoming requests address allocation requests for the daemon.
func (h *postENI) Handle(params eniapi.PostEniParams) middleware.Responder {
	var (
		err   error
		limit rate.LimitedRequest

		owner       = swag.StringValue(params.Owner)
		containerID = swag.StringValue(params.ContainerID)
		netns       = swag.StringValue(params.Netns)
		resp        *models.IPAMResponse
	)

	// api rate limit
	ctx, cancel := context.WithTimeout(context.Background(), defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = h.daemon.apiLimiterSet.Wait(ctx, apiRequestPostENI)
	if err != nil {
		return api.Error(eniapi.PostEniFailureCode, err)
	}
	defer func() {
		limit.Error(err)
		scopeLog := eniLog.WithError(err).WithField("owner", owner).WithField("containerID", containerID).WithField("netns", netns).WithField("response", logfields.Repr(resp))
		if err != nil {
			scopeLog.Error("allocate ENI error")
		} else {
			scopeLog.Debug("allocate ENI success")
		}
	}()

	ipv4Result, ipv6Result, err := h.daemon.enim.ADD(owner, containerID, netns)
	if err != nil {
		log.WithError(err).WithField("owner", owner).Error("allocate ENI error")
		return api.Error(eniapi.PostEniFailureCode, err)
	}

	resp = &models.IPAMResponse{
		HostAddressing: node.GetNodeAddressing(),
		Address:        &models.AddressPair{},
		IPV4:           ipv4Result,
		IPV6:           ipv6Result,
	}

	if ipv4Result != nil {
		resp.Address.IPV4 = ipv4Result.IP
	}

	if ipv6Result != nil {
		resp.Address.IPV6 = ipv6Result.IP
	}
	log.WithField("owner", owner).WithField("response", logfields.Json(resp)).Debug("allocate ENI success")

	return eniapi.NewPostEniOK().WithPayload(resp)
}

type deleteENI struct {
	daemon *Daemon
}

// NewDeleteENIHandler handle incoming requests to delete addresses.
func NewDeleteENIHandler(d *Daemon) eniapi.DeleteEniHandler {
	return &deleteENI{daemon: d}
}

func (h *deleteENI) Handle(params eniapi.DeleteEniParams) middleware.Responder {
	var (
		err   error
		limit rate.LimitedRequest

		owner       = swag.StringValue(params.Owner)
		containerID = swag.StringValue(params.ContainerID)
		netns       = swag.StringValue(params.Netns)
	)

	// api rate limit
	ctx, cancel := context.WithTimeout(context.Background(), defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = h.daemon.apiLimiterSet.Wait(ctx, apiRequestDeleteENI)
	if err != nil {
		return api.Error(eniapi.DeleteEniFailureCode, err)
	}
	defer func() {
		limit.Error(err)
		scopeLog := eniLog.WithError(err).WithField("owner", owner).WithField("containerID", containerID).WithField("netns", netns)
		if err != nil {
			scopeLog.Error("delete ENI error")
		} else {
			scopeLog.Debug("delete ENI success")
		}
	}()

	if err = h.daemon.enim.DEL(owner, containerID, netns); err != nil {
		return api.Error(eniapi.DeleteEniFailureCode, err)
	}

	return eniapi.NewDeleteEniOK()
}

func (d *Daemon) configureENIM() {

}

func (d *Daemon) startENIM() {
	bootstrapStats.enim.Start()
	d.enim = enim.NewENIManager(option.Config, d.k8sWatcher)
	bootstrapStats.enim.End(true)
}
