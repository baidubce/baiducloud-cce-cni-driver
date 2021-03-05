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

package v1alpha1

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
)

// NodeAgentConfiguration contains everything necessary to configure the cni node agent.
type NodeAgentConfiguration struct {
	// Kubeconfig is the path of kubeconfig with authorization information
	Kubeconfig string `json:"kubeconfig"`
	// CNIMode is cni mode
	CNIMode types.ContainerNetworkMode `json:"cniMode"`
	// Workers is number of threads to do work
	// default 1
	Workers int `json:"workers"`
	// ResyncPeriod is how often resources from the apiserver is refreshed.
	// Must be greater than 0
	ResyncPeriod types.Duration `json:"resyncPeriod"`
	// CCE contains everything necessary to configure the cni node agent for CCE
	CCE CCEConfiguration `json:"cce"`
	// CNIConfig contains everything necessary to update cni config file on the node
	CNIConfig CNIConfigControllerConfiguration `json:"cniConfig"`
}

type CCEConfiguration struct {
	// CCEConfiguration contains CCE-related configuration
	// details for the cni node agent.
	// AccessKeyID is OpenApi AccessKeyID
	AccessKeyID string `json:"accessKeyID"`
	// SecretAccessKey is BCE OpenApi SecretAccessKey
	SecretAccessKey string `json:"secretAccessKey"`
	// Region is BCE OpenApi Region
	Region string `json:"region"`
	// ClusterID is the CCE cluster ID
	ClusterID string `json:"clusterID"`
	// ContainerNetworkCIDRIPv4 is the CCE cluster IPv6 Container Network CIDR
	ContainerNetworkCIDRIPv4 string `json:"containerNetworkCIDRIPv4"`
	// ContainerNetworkCIDRIPv6  is the CCE cluster IPv6 Container Network CIDR
	ContainerNetworkCIDRIPv6 string `json:"containerNetworkCIDRIPv6"`
	// VPCID is the VPCID of the cluster
	VPCID string `json:"vpcID"`
	// RouteController contains route controller related configuration
	RouteController RouteControllerConfiguration `json:"routeController"`
	// ENIController contains eni controller related configuration
	ENIController ENIControllerConfiguration `json:"eniController"`
}

type CNIConfigControllerConfiguration struct {
	// CNINetworkName is the network name in cni config file
	CNINetworkName string `json:"cniNetworkName"`
	// CNIConfigFileName is the cni config file name. e.g. "00-vpc-cni.conflist"
	CNIConfigFileName string `json:"cniConfigFileName"`
	// CNIConfigDir is the full path of the directory in which to search for CNI config files, normally "/etc/cni/net.d"
	CNIConfigDir string `json:"cniConfigDir"`
	// CNIConfigTemplateFile is the full path of cni config template file
	CNIConfigTemplateFile string `json:"cniConfigTemplateFile"`
	// AutoDetectConfigTemplateFile determines whether node agent should auto detect cni config template
	AutoDetectConfigTemplateFile bool `json:"autoDetectConfigTemplateFile"`
}

// RouteControllerConfiguration contains route-controller configuration
type RouteControllerConfiguration struct {
	// EnableVPCRoute 创建到节点上容器的 VPC 路由
	// default true
	EnableVPCRoute bool `json:"enableVPCRoute"`
	// EnableStaticRoute 创建到其他节点容器的静态路由
	// default false
	EnableStaticRoute bool `json:"enableStaticRoute"`
}

// ENIControllerConfiguration contains eni-controller configuration
type ENIControllerConfiguration struct {
	// ENISubnetList 创建 ENI 的子网，采用','分隔多个子网
	// 可以为空，由用户自己指定每个节点的 ENI 子网
	// 例如 sbn-g53sb5a5ircf,sbn-30f9qg2ekcrm
	// 从用户全局配置中筛选相同可用区，更新至 ENISpec
	ENISubnetList []string `json:"eniSubnetList"`
	// SecurityGroupList 给 ENI 绑定的安全组，采用','分隔多个子网
	// 例如 g-twh19p9zcuqr,g-5yhyct307p98
	SecurityGroupList []string `json:"securityGroupList"`
	// ENISyncPeriod how often to reconcile eni status
	ENISyncPeriod types.Duration `json:"eniSyncPeriod"`
	// RouteTableOffset 给 ENI 创建策略路由的偏移
	RouteTableOffset int `json:"routeTableOffset"`
}
