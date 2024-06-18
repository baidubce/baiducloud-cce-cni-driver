//go:build ipam_provider_vpc_eni

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
	"time"

	"github.com/spf13/viper"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/vpceni"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

func init() {
	allocatorProviders[ipamOption.IPAMVpcEni] = &vpceni.BCEAllocatorProvider{}
	subnetSyncerProviders[ipamOption.IPAMVpcEni] = &bcesync.VPCSubnetSyncher{}
	eniSyncerProviders[ipamOption.IPAMVpcEni] = &bcesync.VPCENISyncer{}

	registerFlags()
}

func registerFlags() {
	flags := rootCmd.Flags()

	flags.String(operatorOption.BCECloudAccessKey, "", "BCE OpenApi AccessKeyID.")
	option.BindEnv(operatorOption.BCECloudAccessKey)

	flags.String(operatorOption.BCECloudSecureKey, "", "BCE OpenApi AccessKeySecret.")
	option.BindEnv(operatorOption.BCECloudSecureKey)

	flags.String(operatorOption.BCECloudHost, "", "host name for BCE api")
	option.BindEnv(operatorOption.BCECloudHost)

	flags.String(operatorOption.BCECloudContry, "", "contry for BCE api")
	option.BindEnv(operatorOption.BCECloudContry)

	flags.String(operatorOption.BCECloudRegion, "", "region for BCE api")
	option.BindEnv(operatorOption.BCECloudRegion)

	flags.String(operatorOption.BCECloudVPCID, "", "vpc id")
	option.BindEnv(operatorOption.BCECloudVPCID)

	flags.Duration(option.ResourceResyncInterval, operatorOption.DefaultResourceResyncInterval, "synchronization cycle of vpc resources, such as subnet and ENI")
	option.BindEnv(option.ResourceResyncInterval)

	flags.String(operatorOption.CCEClusterID, "", "cluster id defined in CCE")
	option.BindEnv(operatorOption.CCEClusterID)

	flags.Duration(operatorOption.ResourceENIResyncInterval, 60*time.Second, "Interval to resync eni resources")
	option.BindEnv(operatorOption.ResourceENIResyncInterval)

	flags.Int(operatorOption.BCECustomerMaxENI, 0, "max eni number for customer")
	option.BindEnv(operatorOption.BCECustomerMaxENI)

	flags.Int(operatorOption.BCECustomerMaxIP, 0, "max ip number of eni for customer")
	option.BindEnv(operatorOption.BCECustomerMaxIP)

	viper.BindPFlags(flags)
}
