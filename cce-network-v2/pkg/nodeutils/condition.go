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

package nodeutils

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
)

// Custom NodeConditionType constants
const (
	CCESubnetUnavailable corev1.NodeConditionType = "CCESubnetUnavailable"
)

// Error reasons for subnet filtering and node conditions
const (
	ReasonSubnetZoneMismatch    = "SubnetZoneMismatch"
	ReasonSubnetInsufficientIPs = "SubnetInsufficientIPs"
	ReasonSubnetEnsureFailure   = "SubnetEnsureFailure"
	ReasonNoAvailableSubnet     = "NoAvailableSubnet"
	ReasonSubnetsAvailable      = "SubnetsAvailable"
	ReasonSubnetEnsured         = "SubnetEnsured"
	ReasonIPBorrowSucceeded     = "IPBorrowSucceeded"
)

// SetNodeCondition is a generic function to set node conditions
// This function is extracted as a common utility to be reused across different packages
func SetNodeCondition(nodeName string, conditionType corev1.NodeConditionType, status corev1.ConditionStatus, reason, message string, logger *logrus.Entry) {
	// Early return for empty node name to improve test coverage
	if nodeName == "" {
		if logger != nil {
			logger.Warn("SetNodeCondition called with empty node name")
		}
		return
	}

	// Early return for nil logger to improve test coverage
	if logger == nil {
		return
	}

	ctx := context.Background()

	node, err := watchers.NodeClient.Lister().Get(nodeName)
	if err != nil {
		logger.WithError(err).WithField("nodeName", nodeName).Error("Failed to get node from lister")
		return
	}

	// Check if condition already has the desired status
	for _, condition := range node.Status.Conditions {
		if condition.Type == conditionType && condition.Status == status && condition.Reason == reason {
			// Condition already has the desired status, no need to update
			return
		}
	}

	// Set condition
	now := metav1.Now()
	nodeCondition := corev1.NodeCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		LastHeartbeatTime:  now,
	}

	raw, err := json.Marshal(&[]corev1.NodeCondition{nodeCondition})
	if err != nil {
		logger.WithError(err).Error("Failed to marshal node conditions")
		return
	}

	patch := []byte(fmt.Sprintf(`{"status":{"conditions":%s}}`, raw))

	_, err = watchers.NodeClient.NodeInterface().PatchStatus(ctx, nodeName, patch)
	if err != nil {
		logger.WithError(err).WithFields(logrus.Fields{
			"nodeName":  nodeName,
			"condition": conditionType,
			"status":    status,
		}).Error("Failed to update node condition")
	} else {
		logger.WithFields(logrus.Fields{
			"nodeName":  nodeName,
			"condition": conditionType,
			"status":    status,
			"reason":    reason,
		}).Info("Successfully updated node condition")
	}
}
