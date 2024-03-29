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
	"fmt"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	endpointapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/bandwidth"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/qos"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/sirupsen/logrus"
)

var (
	endpointLog = log.WithField("module", "endpoint-handler")
)

type getEndpointExtpluginStatus struct {
	daemon *Daemon
}

// Handle implements endpoint.GetEndpointExtpluginStatusHandler
func (handler *getEndpointExtpluginStatus) Handle(param endpointapi.GetEndpointExtpluginStatusParams) middleware.Responder {
	var (
		err error
		ctx = context.Background()

		// tmp variable
		limit          rate.LimitedRequest
		extFeatureData models.ExtFeatureData
		containerID    = swag.StringValue(param.ContainerID)
	)

	// api rate limit
	ctx, cancel := context.WithTimeout(ctx, defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = handler.daemon.apiLimiterSet.Wait(ctx, apiRequestGetExtPluginStatus)
	if err != nil {
		return endpointapi.NewGetEndpointExtpluginStatusFailure().WithPayload(models.Error(err.Error()))
	}
	defer func() {
		limit.Error(err)
		scopeLog := endpointLog.WithField("extFeatureData", logfields.Repr(extFeatureData))
		if err != nil {
			scopeLog.WithError(err).Error("GetEndpointExtpluginStatus failed")
		} else if extFeatureData != nil {
			scopeLog.Debug("GetEndpointExtpluginStatus success")
		}
	}()

	owner := strings.Split(swag.StringValue(param.Owner), "/")
	if len(owner) != 2 {
		err = fmt.Errorf("invalid owner parameter")
		return endpointapi.NewGetEndpointExtpluginStatusFailure().WithPayload(models.Error(err.Error()))
	}
	namespace := owner[0]
	name := owner[1]

	extFeatureData, err = handler.daemon.endpointAPIHandler.GetEndpointExtpluginStatus(ctx, namespace, name, containerID)
	if err != nil {
		return endpointapi.NewGetEndpointExtpluginStatusFailure().WithPayload(models.Error(err.Error()))
	}
	return endpointapi.NewGetEndpointExtpluginStatusOK().WithPayload(extFeatureData)
}

// NewGetEndpointExtpluginStatusHandler creates a new getEndpointExtpluginStatus from the daemon.
func NewGetEndpointExtpluginStatusHandler(d *Daemon) endpointapi.GetEndpointExtpluginStatusHandler {
	return &getEndpointExtpluginStatus{daemon: d}
}

type putEndpointProbe struct {
	daemon *Daemon
}

// Handle implements endpoint.GetEndpointExtpluginStatusHandler
func (handler *putEndpointProbe) Handle(param endpointapi.PutEndpointProbeParams) middleware.Responder {
	var (
		err error
		ctx = context.Background()

		// tmp variable
		limit       rate.LimitedRequest
		result      *models.EndpointProbeResponse
		containerID = swag.StringValue(param.ContainerID)
		cnidriver   = swag.StringValue(param.CniDriver)
		scopeLog    = endpointLog.WithFields(logrus.Fields{
			"method":      "PutEndpointProbe",
			"containerID": containerID,
			"cnidriver":   cnidriver,
			"owner":       swag.StringValue(param.Owner),
		})
	)

	owner := strings.Split(swag.StringValue(param.Owner), "/")
	if len(owner) != 2 {
		err = fmt.Errorf("invalid owner parameter")
		return endpointapi.NewPutEndpointProbeFailure().WithPayload(models.Error(err.Error()))
	}
	namespace := owner[0]
	name := owner[1]

	// api rate limit

	ctx, cancel := context.WithTimeout(ctx, defaults.ClientConnectTimeout)
	defer cancel()
	limit, err = handler.daemon.apiLimiterSet.Wait(ctx, apiRequestPutEndpointProbe)
	if err != nil {
		return endpointapi.NewPutEndpointProbeFailure().WithPayload(models.Error(err.Error()))
	}
	defer func() {
		limit.Error(err)
		scopeLog = scopeLog.WithField("result", logfields.Repr(result)).WithField("method", "PutEndpointProbe")
		if err != nil {
			scopeLog.WithError(err).Error("failed to handler api")
		} else {
			scopeLog.Info("success to handler api")
		}
	}()

	result, err = handler.daemon.endpointAPIHandler.PutEndpointProbe(ctx, namespace, name, containerID, cnidriver)
	if err != nil {
		return endpointapi.NewPutEndpointProbeFailure().WithPayload(models.Error(err.Error()))
	}
	return endpointapi.NewPutEndpointProbeCreated().WithPayload(result)
}

// NewGetEndpointExtpluginStatusHandler creates a new getEndpointExtpluginStatus from the daemon.
func NewPutEndpointProbeHandler(d *Daemon) endpointapi.PutEndpointProbeHandler {
	return &putEndpointProbe{daemon: d}
}

func (d *Daemon) startEndpointHanler() {
	d.endpointAPIHandler = endpoint.NewEndpointAPIHandler(d.k8sWatcher)
	bandwidth.InitBandwidthManager()
	qos.InitEgressPriorityManager()
}
