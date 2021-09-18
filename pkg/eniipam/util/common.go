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

package util

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
)

func GetInstanceIDFromNode(node *v1.Node) (string, error) {
	providerID := node.Spec.ProviderID
	if !strings.HasPrefix(providerID, "cce://") {
		providerID = "cce://" + providerID
	}
	splitted := strings.Split(providerID, "//")
	if len(splitted) != 2 {
		return "", fmt.Errorf("invalid ProviderID format: %v", node.Spec.ProviderID)
	}
	if splitted[1] == "" {
		return "", fmt.Errorf("instanceID of node %v is empty", node.Name)
	}
	return splitted[1], nil
}

func GetStsName(wep *v1alpha1.WorkloadEndpoint) string {
	return wep.Name[:strings.LastIndex(wep.Name, "-")]
}

func GetStsPodIndex(wep *v1alpha1.WorkloadEndpoint) int {
	indexStr := wep.Name[strings.LastIndex(wep.Name, "-")+1:]
	index, err := strconv.ParseInt(indexStr, 10, 32)
	if err != nil {
		return -1
	}
	return int(index)
}

func GetNodeInstanceType(node *v1.Node) metadata.InstanceTypeEx {
	if node.Labels == nil {
		return metadata.InstanceTypeExUnknown
	}

	instanceTypeStr := node.Labels[v1.LabelInstanceType]
	if instanceTypeStr == "BCC" || instanceTypeStr == "GPU" {
		return metadata.InstanceTypeExBCC
	}

	if instanceTypeStr == "BBC" {
		return metadata.InstanceTypeExBBC
	}

	return metadata.InstanceTypeExUnknown
}

func NewBackoffWithCap(cap time.Duration) *wait.Backoff {
	backoff := wait.Backoff{
		Steps:    30,
		Duration: 2 * time.Second,
		Factor:   2.0,
		Jitter:   0.05,
		Cap:      cap,
	}
	return &backoff
}

// Random generates random num in [min, max)
func Random(min, max int) int {
	return rand.Intn(max-min) + min
}
