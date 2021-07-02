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

package eni

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

const (
	ENIStatusInuse     string = "inuse"
	ENIStatusAvailable string = "available"
	ENIStatusAttaching string = "attaching"
	ENIStatusDetaching string = "detaching"
	ENIStatusDeleting  string = "deleting"
)

const (
	NodeAnnotationMaxENINum         = "cce.io/max-eni-num"
	NodeAnnotationMaxIPPerENI       = "cce.io/max-ip-per-eni"
	NodeAnnotationPreAttachedENINum = "cce.io/pre-attached-eni-num"
	NodeAnnotationWarmIPTarget      = "cce.io/warm-ip-target"
)

const eniNamePartNum int = 4

// GetMaxIPPerENI returns the max num of IPs that can be attached to single ENI
// Ref: https://cloud.baidu.com/doc/VPC/s/0jwvytzll
func GetMaxIPPerENI(memoryCapacityInGB int) int {
	maxIPNum := 0

	switch {
	case memoryCapacityInGB > 0 && memoryCapacityInGB < 2:
		maxIPNum = 2
	case memoryCapacityInGB >= 2 && memoryCapacityInGB <= 8:
		maxIPNum = 8
	case memoryCapacityInGB > 8 && memoryCapacityInGB <= 32:
		maxIPNum = 16
	case memoryCapacityInGB > 32 && memoryCapacityInGB <= 64:
		maxIPNum = 30
	case memoryCapacityInGB > 64:
		maxIPNum = 40
	}
	return maxIPNum
}

// GetMaxENIPerNode returns the max num of ENIs that can be attached to a node
func GetMaxENIPerNode(CPUCount int) int {
	maxENINum := 0

	switch {
	case CPUCount > 0 && CPUCount < 8:
		maxENINum = CPUCount
	case CPUCount >= 8:
		maxENINum = 8
	}

	return maxENINum
}

// CreateNameForENI creates name for newly created eni
func CreateNameForENI(clusterID, instanceID, nodeName string) string {
	hash := sha1.Sum([]byte(time.Now().String()))
	suffix := hex.EncodeToString(hash[:])

	return fmt.Sprintf("%s/%s/%s/%s", clusterID, instanceID, nodeName, suffix[:6])
}

// ENICreatedByCCE judges whether an eni is created by cce
func ENICreatedByCCE(eni *enisdk.Eni) bool {
	if eni == nil {
		return false
	}
	parts := strings.Split(eni.Name, "/")
	if len(parts) != eniNamePartNum {
		return false
	}
	// parts[0] is clusterId and parts[1] is instanceId
	if !(strings.HasPrefix(parts[0], "c-") || strings.HasPrefix(parts[0], "cce-")) || !strings.HasPrefix(parts[1], "i-") {
		return false
	}

	return true
}

// ENIOwnedByCluster judges whether an eni is owned by specific cluster
func ENIOwnedByCluster(eni *enisdk.Eni, clusterID string) bool {
	if !ENICreatedByCCE(eni) {
		return false
	}
	// ENICreatedByCCE ensures len(parts) == 4
	parts := strings.Split(eni.Name, "/")
	return clusterID == parts[0]
}

// ENIOwnedByNode judges whether an eni is owned by specific node
func ENIOwnedByNode(eni *enisdk.Eni, clusterID, instanceID string) bool {
	if !ENICreatedByCCE(eni) {
		return false
	}
	// ENICreatedByCCE ensures len(parts) == 4
	parts := strings.Split(eni.Name, "/")
	return clusterID == parts[0] && instanceID == parts[1]
}

func GetNodeNameFromENIName(eniName string) (string, error) {
	parts := strings.Split(eniName, "/")
	if len(parts) != eniNamePartNum {
		return "", fmt.Errorf("invalid eni name: %v", eniName)
	}
	return parts[2], nil
}

func GetPrivateIPSet(eni *enisdk.Eni) []v1alpha1.PrivateIP {
	var res []v1alpha1.PrivateIP
	if eni == nil {
		return res
	}
	for _, ip := range eni.PrivateIpSet {
		res = append(res, v1alpha1.PrivateIP{
			IsPrimary:        ip.Primary,
			PublicIPAddress:  ip.PublicIpAddress,
			PrivateIPAddress: ip.PrivateIpAddress,
		})
	}
	return res
}

func GetMaxIPPerENIFromNodeAnnotations(node *v1.Node) (int, error) {
	return getIntegerFromAnnotations(node, NodeAnnotationMaxIPPerENI)
}

func GetMaxENINumFromNodeAnnotations(node *v1.Node) (int, error) {
	return getIntegerFromAnnotations(node, NodeAnnotationMaxENINum)
}

func GetPreAttachedENINumFromNodeAnnotations(node *v1.Node) (int, error) {
	return getIntegerFromAnnotations(node, NodeAnnotationPreAttachedENINum)
}

func GetWarmIPTargetFromNodeAnnotations(node *v1.Node) (int, error) {
	return getIntegerFromAnnotations(node, NodeAnnotationWarmIPTarget)
}

func getIntegerFromAnnotations(node *v1.Node, annotation string) (int, error) {
	if node == nil {
		return 0, fmt.Errorf("node is nil")
	}
	annotations := node.Annotations
	if annotations == nil {
		return 0, fmt.Errorf("node %v has no annotations", node.Name)
	}
	num, ok := annotations[annotation]
	if !ok {
		return 0, fmt.Errorf("node %v annotations does not have key: %v", node.Name, annotation)
	}

	return strconv.Atoi(num)
}
