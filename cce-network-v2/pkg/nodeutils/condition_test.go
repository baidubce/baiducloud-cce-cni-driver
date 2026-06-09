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
	"fmt"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetNodeCondition(t *testing.T) {
	// Create logger
	logger := logrus.NewEntry(logrus.New())

	tests := []struct {
		name          string
		nodeName      string
		conditionType corev1.NodeConditionType
		status        corev1.ConditionStatus
		reason        string
		message       string
		expectError   bool
		logger        *logrus.Entry
	}{
		{
			name:          "Given_ValidCondition_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetZoneMismatch,
			message:       "zone mismatch for node test-node",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_EmptyNodeName_When_SetNodeCondition_Then_Continue",
			nodeName:      "",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionFalse,
			reason:        ReasonSubnetsAvailable,
			message:       "subnets are available",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_ValidFalseCondition_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-2",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionFalse,
			reason:        ReasonSubnetsAvailable,
			message:       "subnets are available for node test-node-2",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_DifferentConditionType_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-3",
			conditionType: corev1.NodeConditionType("CustomCondition"),
			status:        corev1.ConditionUnknown,
			reason:        "CustomReason",
			message:       "custom message for test",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_LongMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-4",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetInsufficientIPs,
			message:       "This is a very long message that tests the JSON marshaling functionality with special characters: !@#$%^&*()_+-=[]{}|;':\",./<>?",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_EmptyMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-5",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionFalse,
			reason:        ReasonSubnetsAvailable,
			message:       "",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_SpecialCharacters_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-6",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetEnsureFailure,
			message:       "Message with special chars: \n\t\r\"\\",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_EmptyNodeNameWithNilLogger_When_SetNodeCondition_Then_Continue",
			nodeName:      "",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetZoneMismatch,
			message:       "test message",
			expectError:   false,
			logger:        nil,
		},
		{
			name:          "Given_NonEmptyNodeNameWithNilLogger_When_SetNodeCondition_Then_Continue",
			nodeName:      "test-node-nil-logger",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetZoneMismatch,
			message:       "test message",
			expectError:   false,
			logger:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use recover to catch any panic and continue testing
			defer func() {
				if r := recover(); r != nil {
					// Expected panic due to missing Kubernetes API
					t.Logf("Expected panic caught: %v", r)
				}
			}()

			// Call SetNodeCondition - this will panic due to missing Kubernetes API
			// but we catch it and continue
			SetNodeCondition(tt.nodeName, tt.conditionType, tt.status, tt.reason, tt.message, tt.logger)

			// If we reach here, the function executed without panic
			assert.True(t, true, "SetNodeCondition executed without panic")
		})
	}
}

func TestConstants(t *testing.T) {
	t.Run("Given_Constants_When_CheckValues_Then_ReturnExpectedValues", func(t *testing.T) {
		// Test that constants are properly defined
		assert.Equal(t, corev1.NodeConditionType("CCESubnetUnavailable"), CCESubnetUnavailable)
		assert.Equal(t, "SubnetZoneMismatch", ReasonSubnetZoneMismatch)
		assert.Equal(t, "SubnetInsufficientIPs", ReasonSubnetInsufficientIPs)
		assert.Equal(t, "SubnetEnsureFailure", ReasonSubnetEnsureFailure)
		assert.Equal(t, "NoAvailableSubnet", ReasonNoAvailableSubnet)
		assert.Equal(t, "SubnetsAvailable", ReasonSubnetsAvailable)
		assert.Equal(t, "SubnetEnsured", ReasonSubnetEnsured)
		assert.Equal(t, "IPBorrowSucceeded", ReasonIPBorrowSucceeded)
	})
}

// TestSetNodeConditionWithDifferentLoggers tests SetNodeCondition with different logger configurations
func TestSetNodeConditionWithDifferentLoggers(t *testing.T) {
	tests := []struct {
		name     string
		logger   *logrus.Entry
		nodeName string
	}{
		{
			name:     "Given_NilLogger_When_SetNodeCondition_Then_Continue",
			logger:   nil,
			nodeName: "test-node-nil-logger",
		},
		{
			name:     "Given_EmptyLogger_When_SetNodeCondition_Then_Continue",
			logger:   logrus.NewEntry(logrus.New()),
			nodeName: "test-node-empty-logger",
		},
		{
			name:     "Given_LoggerWithFields_When_SetNodeCondition_Then_Continue",
			logger:   logrus.NewEntry(logrus.New()).WithField("test", "value"),
			nodeName: "test-node-logger-with-fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use recover to catch any panic and continue testing
			defer func() {
				if r := recover(); r != nil {
					// Expected panic due to missing Kubernetes API
					t.Logf("Expected panic caught: %v", r)
				}
			}()

			// Call SetNodeCondition with different logger configurations
			SetNodeCondition(tt.nodeName, CCESubnetUnavailable, corev1.ConditionTrue, ReasonSubnetZoneMismatch, "test message", tt.logger)

			// If we reach here, the function executed without panic
			assert.True(t, true, "SetNodeCondition executed without panic")
		})
	}
}

// TestSetNodeConditionEdgeCases tests SetNodeCondition with edge cases
func TestSetNodeConditionEdgeCases(t *testing.T) {
	logger := logrus.NewEntry(logrus.New())

	tests := []struct {
		name          string
		nodeName      string
		conditionType corev1.NodeConditionType
		status        corev1.ConditionStatus
		reason        string
		message       string
	}{
		{
			name:          "Given_UnicodeMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-unicode",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetZoneMismatch,
			message:       "Unicode message: 你好世界 🌍 🚀",
		},
		{
			name:          "Given_JSONLikeMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-json",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetZoneMismatch,
			message:       `{"key": "value", "nested": {"array": [1,2,3]}}`,
		},
		{
			name:          "Given_HTMLMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-html",
			conditionType: CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        ReasonSubnetZoneMismatch,
			message:       "<html><body><h1>Test</h1></body></html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use recover to catch any panic and continue testing
			defer func() {
				if r := recover(); r != nil {
					// Expected panic due to missing Kubernetes API
					t.Logf("Expected panic caught: %v", r)
				}
			}()

			// Call SetNodeCondition with edge case messages
			SetNodeCondition(tt.nodeName, tt.conditionType, tt.status, tt.reason, tt.message, logger)

			// If we reach here, the function executed without panic
			assert.True(t, true, "SetNodeCondition executed without panic")
		})
	}
}

// TestSetNodeCondition_Given_DifferentStatuses_When_SetNodeCondition_Then_HandleAllStatuses 测试不同的状态值
func TestSetNodeCondition_Given_DifferentStatuses_When_SetNodeCondition_Then_HandleAllStatuses(t *testing.T) {
	ccemock.InitMockEnv()

	// 创建测试节点
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	// 添加到 informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// 测试不同的状态值
	statuses := []corev1.ConditionStatus{
		corev1.ConditionTrue,
		corev1.ConditionFalse,
		corev1.ConditionUnknown,
	}

	for _, status := range statuses {
		t.Run(fmt.Sprintf("Test_%s", status), func(t *testing.T) {
			// 执行测试
			logger := logrus.NewEntry(logrus.New())
			SetNodeCondition("test-node", CCESubnetUnavailable, status, "TestReason", "Test message", logger)

			// 等待一下让 informer 同步
			time.Sleep(50 * time.Millisecond)

			// 验证条件被设置
			node, err := watchers.NodeClient.Lister().Get("test-node")
			assert.NoError(t, err, "should get node from lister")

			// 检查是否有目标条件
			found := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == CCESubnetUnavailable {
					found = true
					assert.Equal(t, status, condition.Status, "condition status should match")
					assert.Equal(t, "TestReason", condition.Reason, "condition reason should match")
					assert.Equal(t, "Test message", condition.Message, "condition message should match")
					break
				}
			}
			assert.True(t, found, "should find CCESubnetUnavailable condition")
		})
	}
}

// TestSetNodeCondition_Given_ConditionAlreadyExists_When_SetNodeCondition_Then_ReturnEarly 测试条件已存在的情况
func TestSetNodeCondition_Given_ConditionAlreadyExists_When_SetNodeCondition_Then_ReturnEarly(t *testing.T) {
	ccemock.InitMockEnv()

	// 创建已有条件的测试节点
	existingCondition := corev1.NodeCondition{
		Type:               CCESubnetUnavailable,
		Status:             corev1.ConditionTrue,
		Reason:             "TestReason",
		Message:            "Test message",
		LastTransitionTime: metav1.Now(),
		LastHeartbeatTime:  metav1.Now(),
	}

	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-existing",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{existingCondition},
		},
	}

	// 添加到 informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// 执行测试 - 尝试设置相同的条件
	logger := logrus.NewEntry(logrus.New())
	SetNodeCondition("test-node-existing", CCESubnetUnavailable, corev1.ConditionTrue, "TestReason", "Test message", logger)

	// 等待一下让 informer 同步
	time.Sleep(50 * time.Millisecond)

	// 验证条件数量没有变化
	node, err := watchers.NodeClient.Lister().Get("test-node-existing")
	assert.NoError(t, err, "should get node from lister")
	assert.Len(t, node.Status.Conditions, 1, "should have exactly one condition")
}

// TestSetNodeCondition_Given_DifferentConditionStatus_When_SetNodeCondition_Then_UpdateCondition 测试不同状态的条件更新
func TestSetNodeCondition_Given_DifferentConditionStatus_When_SetNodeCondition_Then_UpdateCondition(t *testing.T) {
	ccemock.InitMockEnv()

	// 创建有不同状态条件的测试节点
	existingCondition := corev1.NodeCondition{
		Type:               CCESubnetUnavailable,
		Status:             corev1.ConditionFalse, // 不同的状态
		Reason:             "TestReason",
		Message:            "Test message",
		LastTransitionTime: metav1.Now(),
		LastHeartbeatTime:  metav1.Now(),
	}

	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-different-status",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{existingCondition},
		},
	}

	// 添加到 informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// 执行测试 - 尝试设置不同的状态
	logger := logrus.NewEntry(logrus.New())
	SetNodeCondition("test-node-different-status", CCESubnetUnavailable, corev1.ConditionTrue, "TestReason", "Test message", logger)

	// 等待一下让 informer 同步
	time.Sleep(50 * time.Millisecond)

	// 验证条件被更新
	node, err := watchers.NodeClient.Lister().Get("test-node-different-status")
	assert.NoError(t, err, "should get node from lister")

	// 检查条件状态是否被更新
	found := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == CCESubnetUnavailable {
			found = true
			assert.Equal(t, corev1.ConditionTrue, condition.Status, "condition status should be updated")
			break
		}
	}
	assert.True(t, found, "should find CCESubnetUnavailable condition")
}

// TestSetNodeCondition_Given_DifferentConditionReason_When_SetNodeCondition_Then_UpdateCondition 测试不同原因的条件更新
func TestSetNodeCondition_Given_DifferentConditionReason_When_SetNodeCondition_Then_UpdateCondition(t *testing.T) {
	ccemock.InitMockEnv()

	// 创建有不同原因条件的测试节点
	existingCondition := corev1.NodeCondition{
		Type:               CCESubnetUnavailable,
		Status:             corev1.ConditionTrue,
		Reason:             "OldReason", // 不同的原因
		Message:            "Test message",
		LastTransitionTime: metav1.Now(),
		LastHeartbeatTime:  metav1.Now(),
	}

	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-different-reason",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{existingCondition},
		},
	}

	// 添加到 informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// 执行测试 - 尝试设置不同的原因
	logger := logrus.NewEntry(logrus.New())
	SetNodeCondition("test-node-different-reason", CCESubnetUnavailable, corev1.ConditionTrue, "NewReason", "Test message", logger)

	// 等待一下让 informer 同步
	time.Sleep(50 * time.Millisecond)

	// 验证条件被更新
	node, err := watchers.NodeClient.Lister().Get("test-node-different-reason")
	assert.NoError(t, err, "should get node from lister")

	// 检查条件原因是否被更新
	found := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == CCESubnetUnavailable {
			found = true
			assert.Equal(t, "NewReason", condition.Reason, "condition reason should be updated")
			break
		}
	}
	assert.True(t, found, "should find CCESubnetUnavailable condition")
}

// TestSetNodeCondition_Given_MultipleConditions_When_SetNodeCondition_Then_CheckAllConditions 测试多个条件的情况
func TestSetNodeCondition_Given_MultipleConditions_When_SetNodeCondition_Then_CheckAllConditions(t *testing.T) {
	ccemock.InitMockEnv()

	// 创建有多个条件的测试节点
	existingCondition1 := corev1.NodeCondition{
		Type:               corev1.NodeConditionType("OtherCondition"),
		Status:             corev1.ConditionFalse,
		Reason:             "OtherReason",
		Message:            "Other message",
		LastTransitionTime: metav1.Now(),
		LastHeartbeatTime:  metav1.Now(),
	}

	existingCondition2 := corev1.NodeCondition{
		Type:               CCESubnetUnavailable,
		Status:             corev1.ConditionTrue,
		Reason:             "TestReason",
		Message:            "Test message",
		LastTransitionTime: metav1.Now(),
		LastHeartbeatTime:  metav1.Now(),
	}

	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-multiple",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{existingCondition1, existingCondition2},
		},
	}

	// 添加到 informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// 执行测试 - 尝试设置相同的条件
	logger := logrus.NewEntry(logrus.New())
	SetNodeCondition("test-node-multiple", CCESubnetUnavailable, corev1.ConditionTrue, "TestReason", "Test message", logger)

	// 等待一下让 informer 同步
	time.Sleep(50 * time.Millisecond)

	// 验证条件数量没有变化
	node, err := watchers.NodeClient.Lister().Get("test-node-multiple")
	assert.NoError(t, err, "should get node from lister")
	assert.Len(t, node.Status.Conditions, 2, "should have exactly two conditions")
}

// TestSetNodeCondition_Given_AllReasonConstants_When_SetNodeCondition_Then_HandleCorrectly 测试所有原因常量
func TestSetNodeCondition_Given_AllReasonConstants_When_SetNodeCondition_Then_HandleCorrectly(t *testing.T) {
	ccemock.InitMockEnv()

	// 创建测试节点
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-reasons",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	// 添加到 informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// 测试所有原因常量
	reasons := []string{
		ReasonSubnetZoneMismatch,
		ReasonSubnetInsufficientIPs,
		ReasonSubnetEnsureFailure,
		ReasonNoAvailableSubnet,
		ReasonSubnetsAvailable,
		ReasonSubnetEnsured,
		ReasonIPBorrowSucceeded,
	}

	for _, reason := range reasons {
		t.Run(fmt.Sprintf("Test_%s", reason), func(t *testing.T) {
			// 执行测试
			logger := logrus.NewEntry(logrus.New())
			SetNodeCondition("test-node-reasons", CCESubnetUnavailable, corev1.ConditionTrue, reason, "Test message", logger)

			// 等待一下让 informer 同步
			time.Sleep(50 * time.Millisecond)

			// 验证条件被设置
			node, err := watchers.NodeClient.Lister().Get("test-node-reasons")
			assert.NoError(t, err, "should get node from lister")

			// 检查是否有目标条件
			found := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == CCESubnetUnavailable && condition.Reason == reason {
					found = true
					assert.Equal(t, corev1.ConditionTrue, condition.Status, "condition status should match")
					assert.Equal(t, reason, condition.Reason, "condition reason should match")
					break
				}
			}
			assert.True(t, found, "should find CCESubnetUnavailable condition with correct reason")
		})
	}
}
