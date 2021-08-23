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

package ippool

import (
	"net"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

// GetNodeIPPoolName is a helper function that creates IPPool CR name from nodeName
func GetNodeIPPoolName(nodeName string) string {
	if net.ParseIP(nodeName) != nil {
		return "ippool-" + strings.Replace(nodeName, ".", "-", -1)
	}
	return "ippool-" + nodeName
}

func GetNodeNameFromIPPoolName(ipPoolName string) string {
	nodeName := strings.TrimPrefix(ipPoolName, "ippool-")

	nodeIPAsName := strings.Replace(nodeName, "-", ".", -1)
	if net.ParseIP(nodeIPAsName) != nil {
		return nodeIPAsName
	}

	return nodeName
}

func NodeMatchesIPPool(node *v1.Node, pool *v1alpha1.IPPool) (bool, error) {
	selector, err := labels.Parse(pool.Spec.NodeSelector)
	if err != nil {
		return false, err
	}

	nodeLabels := node.Labels
	if selector.Matches(labels.Set(nodeLabels)) {
		return true, nil
	}

	return false, nil
}

func GetMatchedIPPoolsByNode(node *v1.Node, pools []v1alpha1.IPPool) ([]v1alpha1.IPPool, error) {
	var result []v1alpha1.IPPool

	for _, pool := range pools {
		matched, err := NodeMatchesIPPool(node, &pool)
		if err != nil {
			return nil, err
		}
		if matched {
			result = append(result, pool)
		}
	}

	return result, nil
}

func GetMatchedNodesByIPPool(pool *v1alpha1.IPPool, nodes []v1.Node) ([]v1.Node, error) {
	var result []v1.Node

	for _, node := range nodes {
		matched, err := NodeMatchesIPPool(&node, pool)
		if err != nil {
			return nil, err
		}
		if matched {
			result = append(result, node)
		}
	}

	return result, nil
}
