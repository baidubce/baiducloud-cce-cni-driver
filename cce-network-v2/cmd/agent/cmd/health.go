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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/daemon"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/health/plugin"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/go-openapi/runtime/middleware"
)

var healthLog = logging.NewSubysLogger("daemon-health")

type daemonHealth struct {
}

// Handle implements daemon.GetHealthzHandler
func (*daemonHealth) Handle(daemon.GetHealthzParams) middleware.Responder {
	manager := plugin.GlobalHealthManager()
	if err := manager.Check(); err != nil {
		healthLog.WithError(err).Error("fail to health check manager")
		return api.Error(500, err)
	}
	return daemon.NewGetHealthzOK()
}

var _ daemon.GetHealthzHandler = &daemonHealth{}
