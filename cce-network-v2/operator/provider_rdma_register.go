//go:build ipam_provider_rdma

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

package main

import (
	"github.com/spf13/viper"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/rdma"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

func init() {
	allocatorProviders[ipamOption.IPAMRdma] = &rdma.BCERDMAAllocatorProvider{}
	subnetSyncerProviders[ipamOption.IPAMRdma] = &bcesync.VPCSubnetSyncher{}
	eniSyncerProviders[ipamOption.IPAMRdma] = &bcesync.VPCENISyncer{}

	registerRdmaFlags()
}

func registerRdmaFlags() {
	flags := rootCmd.Flags()

	flags.Int(operatorOption.BCECustomerMaxRdmaIP, 0, "max rdma ip number of rdma-interface for customer")
	option.BindEnv(operatorOption.BCECustomerMaxRdmaIP)

	viper.BindPFlags(flags)
}
