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

package root

import (
	"time"

	componentbaseconfig "k8s.io/component-base/config"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/cmd"
)

type Options struct {
	cmd.CommonOptions
	CNIMode                    types.ContainerNetworkMode
	VPCID                      string
	ENISyncPeriod              time.Duration
	GCPeriod                   time.Duration
	Port                       int
	DebugPort                  int
	ResyncPeriod               time.Duration
	LeaderElection             componentbaseconfig.LeaderElectionConfiguration
	SubnetSelectionPolicy      string
	Debug                      bool
	IPMutatingRate             float64
	IPMutatingBurst            int64
	AllocateIPConcurrencyLimit int
	ReleaseIPConcurrencyLimit  int
	BatchAddIPNum              int
	IdleIPPoolMaxSize          int
	IdleIPPoolMinSize          int
	stopCh                     chan struct{}
}
