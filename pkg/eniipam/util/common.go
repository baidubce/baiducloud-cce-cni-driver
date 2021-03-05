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
	"math/rand"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

func GetInstanceIDFromNode(node *v1.Node) string {
	providerID := node.Spec.ProviderID
	if !strings.HasPrefix(providerID, "cce://") {
		providerID = "cce://" + providerID
	}
	splitted := strings.Split(providerID, "//")
	if len(splitted) != 2 {
		return ""
	}
	return splitted[1]
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

// Random generates random num in [min, max)
func Random(min, max int) int {
	return rand.Intn(max-min) + min
}
