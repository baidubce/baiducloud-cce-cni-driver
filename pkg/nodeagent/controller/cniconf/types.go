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

package cniconf

import (
	hostlocal "github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator"
	"k8s.io/client-go/kubernetes"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	crdlisters "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	fsutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/kernel"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
)

const (
	IPAMServiceNamespace    = "kube-system"
	IPAMServiceName         = "cce-eni-ipam"
	CCETemplateFileBasePath = "/etc/kubernetes/cni/"
)

var (
	CCETemplateFilePathMap = map[types.ContainerNetworkMode]string{
		// VPC Route
		types.CCEModeRouteVeth:   CCETemplateFileBasePath + "cce-vpc-route-veth.tmpl",
		types.CCEModeRouteIPVlan: CCETemplateFileBasePath + "cce-vpc-route-ipvlan.tmpl",
		// VPC-CNI BCC
		types.CCEModeSecondaryIPVeth:   CCETemplateFileBasePath + "cce-cni-secondary-ip-veth.tmpl",
		types.CCEModeSecondaryIPIPVlan: CCETemplateFileBasePath + "cce-cni-secondary-ip-ipvlan.tmpl",
		// VPC-CNI BBC
		types.CCEModeBBCSecondaryIPVeth:   CCETemplateFileBasePath + "cce-cni-bbc-secondary-ip-veth.tmpl",
		types.CCEModeBBCSecondaryIPIPVlan: CCETemplateFileBasePath + "cce-cni-bbc-secondary-ip-ipvlan.tmpl",
		// Cross VPC ENI
		types.CCEModeCrossVPCEni:          CCETemplateFileBasePath + "cce-cross-vpc-eni.tmpl",
		types.CCEModeExclusiveCrossVPCEni: CCETemplateFileBasePath + "cce-exclusive-cross-vpc-eni.tmpl",
	}
)

// Controller updates cni config file on the node
type Controller struct {
	kubeClient    kubernetes.Interface
	metaClient    metadata.Interface
	ippoolLister  crdlisters.IPPoolLister
	cniMode       types.ContainerNetworkMode
	nodeName      string
	config        *v1alpha1.CNIConfigControllerConfiguration
	netutil       networkutil.Interface
	kernelhandler kernel.Interface
	filesystem    fsutil.FileSystem
}

// CNIConfigData contains everything a cni config needs
type CNIConfigData struct {
	// NetworkName is the network name in cni config file
	NetworkName string
	// IPAMEndPoint is the ipam endpoint. etc 127.0.0.1:50050
	IPAMEndPoint string

	VethMTU         int
	MasterInterface string
	LocalDNSAddress string
	InstanceType    string

	// Subnet is node PodCIDR for vpc route mode
	Subnet string

	// HostLocalRange is host-local plugin RangeSet
	HostLocalRange hostlocal.RangeSet
}
