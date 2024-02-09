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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CceEniList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CceEni `json:"items"`
}

type CceEni struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EniSpec   `json:"spec,omitempty"`
	Status EniStatus `json:"status,omitempty"`
}

type EniSpec struct {
	Eni Eni `json:"eni,omitempty"`
	// Eni 要绑定的节点 id
	InstanceID string `json:"instanceID,omitempty"`
}

// 和 vpc Eni spec 定义保持一致
// https://github.com/baidubce/bce-sdk-go/blob/master/services/eni/model.go#L61
type Eni struct {
	EniID                      string      `json:"eniID,omitempty"`
	Name                       string      `json:"name,omitempty"`
	ZoneName                   string      `json:"zoneName,omitempty"`
	Description                string      `json:"description,omitempty"`
	InstanceID                 string      `json:"instanceID,omitempty"`
	MacAddress                 string      `json:"macAddress,omitempty"`
	VpcID                      string      `json:"vpcID,omitempty"`
	SubnetID                   string      `json:"subnetID,omitempty"`
	Status                     string      `json:"status,omitempty"`
	PrivateIPSet               []PrivateIP `json:"privateIPSet,omitempty"`
	SecurityGroupIds           []string    `json:"securityGroupIds,omitempty"`
	EnterpriseSecurityGroupIds []string    `json:"enterpriseSecurityGroupIds,omitempty"`
	CreatedTime                string      `json:"createdTime,omitempty"`
}

type PrivateIP struct {
	PublicIPAddress  string `json:"publicIPAddress,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
	PrivateIPAddress string `json:"privateIPAddress,omitempty"`
}

type EniStatus struct {
	StatusInfo StatusInfo `json:"statusInfo,omitempty"`
	Node       string     `json:"node,omitempty"`
	PodInfo    PodInfo    `json:"podInfo,omitempty"`
}

type PodInfo struct {
	PodNs       string `json:"podNs,omitempty"`
	PodName     string `json:"podName,omitempty"`
	ContainerID string `json:"containerID,omitempty"`
	NetNs       string `json:"netNs,omitempty"`
}

type StatusInfo struct {
	CurrentStatus CceEniStatus `json:"currentStatus,omitempty"`
	LastStatus    CceEniStatus `json:"lastStatus,omitempty"`
	VpcStatus     VpcEniStatus `json:"vpcStatus,omitempty"`
	UpdateTime    metav1.Time  `json:"updateTime,omitempty"`
}

type CceEniStatus string
type VpcEniStatus string

/* vpc 中 Eni 共 4 种状态(https://cloud.baidu.com/doc/VPC/s/6kknfn5m8):
 *     available：创建完成，未挂载
 *     attaching：挂载中
 *     inuse：    已挂载到单机，vpc 认为的可用状态
 *     detaching：卸载中
 */
const (
	vpcENIStatusAvailable VpcEniStatus = "available"
	vpcENIStatusAttaching VpcEniStatus = "attaching"
	vpcENIStatusInuse     VpcEniStatus = "inuse"
	vpcENIStatusDetaching VpcEniStatus = "detaching"
)

/* cce 中 Eni 状态:
 *     Pending:      创建 crd 之后的初始状态
 *     Created:      向 Vpc 发起创建 Eni 请求成功之后
 *     ReadyInVpc:   attach 成功，Vpc 中进入 inuse 状态
 *     ReadyOnNode:  ReadyInVpc 之后单机对 Eni check ok
 *     UsingInPod:   Eni 被 pod 独占使用中
 *     DeletedInVpc: Eni 被从 Vpc 中强删后的最终状态
 *
 * 独占 Eni 状态机流转：
 *
 *             创建请求成功(ipam写)             attach后进入inuse状态(ipam写)
 *   Pending ----------------------> Created ---------------------------> ReadyInVpc
 * 							            ^                                      |
 * 	           Vpc中强制detach后(ipam写)  |                                      |单机check ok(agent写)
 *								         |                                     |
 *                                        ---------------------------------    |
 *                                         |                               |   |
 *                  Vpc中强删后(ipam写)      |           pod创建后(agent写)   |   v
 *   DeletedInVpc <--------------------- UsingInPod <-------------------- ReadyOnNode --
 *                              |           |                                  ^        |
 *                              |           |                                  |        |
 * 	                            |           |          pod删除后(agent写)       |        |
 *	                            |            ----------------------------------         |
 *                              |                                                       |
 *                               -------------------------------------------------------
 */
const (
	cceEniStatusPending      CceEniStatus = "Pending"
	cceEniStatusCreated      CceEniStatus = "Created"
	cceEniStatusReadyInVpc   CceEniStatus = "ReadyInVpc"
	cceEniStatusReadyOnNode  CceEniStatus = "ReadyOnNode"
	cceEniStatusUsingInPod   CceEniStatus = "UsingInPod"
	cceENIStatusDeletedInVpc CceEniStatus = "DeletedInVpc"
)
