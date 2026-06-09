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

package utils

import (
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
)

// EnableERI determines if ERI management should be enabled based on node label or global config
// Returns true if ERI management should be enabled, false otherwise
func EnableERI(k8sNode *corev1.Node, globalConfig bool) bool {
	if k8sNode == nil {
		log.Error("K8s node is nil")
		return false
	}

	if k8sNode.Labels == nil {
		log.Debug("Node has no labels")
		// If no labels exist, use global configuration
		log.WithFields(logrus.Fields{
			"hasLabel":     false,
			"globalConfig": globalConfig,
			"nodeName":     k8sNode.Name,
		}).Info("ERI management determined by global configuration")
		return globalConfig
	}

	labelValue, hasLabel := k8sNode.Labels[k8s.LabelERIManagement]
	log.WithFields(logrus.Fields{
		"labelKey":   k8s.LabelERIManagement,
		"labelValue": labelValue,
		"hasLabel":   hasLabel,
		"nodeName":   k8sNode.Name,
	}).Info("Checked ERI management label")

	if hasLabel {
		// If label exists, use label value to determine ERI management
		isEnabled := labelValue == k8s.ValueStringTrue
		log.WithFields(logrus.Fields{
			"hasLabel":  hasLabel,
			"isEnabled": isEnabled,
			"nodeName":  k8sNode.Name,
		}).Info("ERI management determined by node label")
		return isEnabled
	}

	// If no label exists, use global configuration
	log.WithFields(logrus.Fields{
		"hasLabel":     hasLabel,
		"globalConfig": globalConfig,
		"nodeName":     k8sNode.Name,
	}).Info("ERI management determined by global configuration")
	return globalConfig
}

// HasAvailableERIQuota checks if there is available ERI quota
// This function is decoupled from Daemon and can be used as a utility function
func HasAvailableERIQuota() bool {
	// Get ERI quota from metadata API
	featureList, err := defaultMetaClient.GetFeatureList()
	if err != nil {
		log.WithError(err).Fatal("Failed to get feature list from metadata API for ERI quota check")
	}

	// Check if featureList is nil
	if featureList == nil {
		log.Fatal("Feature list is nil from metadata API")
	}

	// Parse ERI quota from string to int
	eriQuota, err := strconv.Atoi(featureList.ERIQuota)
	if err != nil {
		log.WithError(err).WithField("eriQuota", featureList.ERIQuota).Fatal("Failed to parse ERI quota from metadata API")
	}

	// Check ERI quota
	hasQuota := eriQuota > 0
	log.WithFields(logrus.Fields{
		"eriQuota": eriQuota,
		"hasQuota": hasQuota,
	}).Info("ERI quota availability checked from metadata API")

	return hasQuota
}

// GetERIManagedLabelSelector gets the label selector value for ERI management
// This function is decoupled from K8sWatcher and can be used as a utility function
func GetERIManagedLabelSelector() string {
	nodeName := nodeTypes.GetName()

	// Get node instance ID
	metaClient := metadata.NewClient()
	instanceID, err := metaClient.GetInstanceID()
	if err != nil {
		// Return empty string if unable to get instance ID
		return ""
	}

	// Construct ERI management label selector value
	// Use the same naming convention as ERI managed NRS
	eriManagedLabelValue := GetRdmaNrsLabelSelectorValueFromNetResourceSetName(
		GetEriMangedNrsName(nodeName),
		nodeName,
		instanceID,
		"", // MAC address is empty because ERI management MAC address is dynamically obtained
		string(ccev2.ENIForERI),
	)

	return eriManagedLabelValue
}

// IsERIManagedENI checks if the ENI is managed by ERI management
// This function checks if the ENI's NodeName corresponds to an ERI managed NRS
func IsERIManagedENI(resource *ccev2.ENI) bool {
	// Check if resource is nil
	if resource == nil {
		return false
	}

	// Check if the ENI has the ERI managed annotation
	if value, exists := resource.Annotations[k8s.AnnotationERIManaged]; exists && value == "true" {
		return true
	}

	// Check if the ENI's NodeName corresponds to an ERI managed NRS
	// ERI managed NRS has a specific naming pattern: nodeName-eri-managed
	if strings.HasSuffix(resource.Spec.NodeName, "-eri-managed") {
		return true
	}

	return false
}
