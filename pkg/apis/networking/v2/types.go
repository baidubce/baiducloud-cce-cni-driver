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

package v2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CceENIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CceENI `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CceENI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ENISpec   `json:"spec,omitempty"`
	Status ENIStatus `json:"status,omitempty"`
}

type ENISpec struct {
	ENI ENI `json:"eni,omitempty"`
	// ENI 要绑定的节点 id
	InstanceID string `json:"instanceID,omitempty"`
}

// 和 VPC ENI spec 定义保持一致
// https://github.com/baidubce/bce-sdk-go/blob/master/services/eni/model.go#L61
type ENI struct {
	ENIID       string `json:"eniID"`
	Name        string `json:"name,omitempty"`
	ZoneName    string `json:"zoneName"`
	Description string `json:"description,omitempty"`
	InstanceID  string `json:"instanceID,omitempty"`
	MacAddress  string `json:"macAddress"`
	VPCID       string `json:"vpcID"`
	SubnetID    string `json:"subnetID"`

	// no use, just to keep up with iaas
	Status                     string      `json:"status,omitempty"`
	PrivateIPSet               []PrivateIP `json:"privateIPSet,omitempty"`
	SecurityGroupIds           []string    `json:"securityGroupIds,omitempty"`
	EnterpriseSecurityGroupIds []string    `json:"enterpriseSecurityGroupIds,omitempty"`
	CreatedTime                metav1.Time `json:"createdTime"`
}

type PrivateIP struct {
	PublicIPAddress  string `json:"publicIPAddress,omitempty"`
	Primary          bool   `json:"primary"`
	PrivateIPAddress string `json:"privateIPAddress"`
}

type ENIStatus struct {
	StatusInfo StatusInfo `json:"statusInfo,omitempty"`
	NodeName   string     `json:"nodeName,omitempty"`
	PodInfo    PodInfo    `json:"podInfo,omitempty"`
}

type PodInfo struct {
	PodNs       string `json:"podNs"`
	PodName     string `json:"podName"`
	ContainerID string `json:"containerID"`
	NetNs       string `json:"netNs"`
}

type StatusInfo struct {
	CurrentStatus CceENIStatus `json:"currentStatus"`
	LastStatus    CceENIStatus `json:"lastStatus,omitempty"`
	VPCStatus     VPCENIStatus `json:"vpcStatus,omitempty"`
	UpdateTime    metav1.Time  `json:"updateTime,omitempty"`
}

type CceENIStatus string
type VPCENIStatus string

/* vpc 中 ENI 共 4 种状态(https://cloud.baidu.com/doc/VPC/s/6kknfn5m8):
 *     available：创建完成，未挂载
 *     attaching：挂载中
 *     inuse：    已挂载到单机，vpc 认为的可用状态
 *     detaching：卸载中
 */
const (
	VPCENIStatusAvailable VPCENIStatus = "available"
	VPCENIStatusAttaching VPCENIStatus = "attaching"
	VPCENIStatusInuse     VPCENIStatus = "inuse"
	VPCENIStatusDetaching VPCENIStatus = "detaching"
)

/* cce 中 ENI 状态:
 *     Pending:      创建 crd 之后的初始状态
 *     Created:      向 VPC 发起创建 ENI 请求成功之后
 *     ReadyInVPC:   attach 成功，VPC 中进入 inuse 状态
 *     ReadyOnNode:  ReadyInVPC 之后单机对 ENI check ok
 *     UsingInPod:   ENI 被 pod 独占使用中
 *     DeletedInVPC: ENI 被从 VPC 中强删后的最终状态
 *
 * 独占 ENI 状态机流转：
 *
 *             创建请求成功(ipam写)             attach后进入inuse状态(ipam写)
 *   Pending ----------------------> Created ---------------------------> ReadyInVPC
 * 							            ^                                      |
 * 	           VPC中强制detach后(ipam写)  |                                      |单机check ok(agent写)
 *								         |                                     |
 *                                        ---------------------------------    |
 *                                         |                               |   |
 *                  VPC中强删后(ipam写)      |           pod创建后(agent写)   |   v
 *   DeletedInVPC <--------------------- UsingInPod <-------------------- ReadyOnNode --
 *                              |           |                                  ^        |
 *                              |           |                                  |        |
 * 	                            |           |          pod删除后(agent写)       |        |
 *	                            |            ----------------------------------         |
 *                              |                                                       |
 *                               -------------------------------------------------------
 */
const (
	CceENIStatusPending      CceENIStatus = "Pending"
	CceENIStatusCreated      CceENIStatus = "Created"
	CceENIStatusReadyInVPC   CceENIStatus = "ReadyInVPC"
	CceENIStatusReadyOnNode  CceENIStatus = "ReadyOnNode"
	CceENIStatusUsingInPod   CceENIStatus = "UsingInPod"
	CceENIStatusDeletedInVPC CceENIStatus = "DeletedInVPC"
)
