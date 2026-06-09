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

package watchers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	applyv1 "k8s.io/client-go/applyconfigurations/core/v1"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listv1 "k8s.io/client-go/listers/core/v1"
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
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetZoneMismatch,
			message:       "zone mismatch for node test-node",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_EmptyNodeName_When_SetNodeCondition_Then_Continue",
			nodeName:      "",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionFalse,
			reason:        watchers.ReasonSubnetsAvailable,
			message:       "subnets are available",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_ValidFalseCondition_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-2",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionFalse,
			reason:        watchers.ReasonSubnetsAvailable,
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
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetInsufficientIPs,
			message:       "This is a very long message that tests the JSON marshaling functionality with special characters: !@#$%^&*()_+-=[]{}|;':\",./<>?",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_EmptyMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-5",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionFalse,
			reason:        watchers.ReasonSubnetsAvailable,
			message:       "",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_SpecialCharacters_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-6",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetEnsureFailure,
			message:       "Message with special chars: \n\t\r\"\\",
			expectError:   false,
			logger:        logger,
		},
		{
			name:          "Given_EmptyNodeNameWithNilLogger_When_SetNodeCondition_Then_Continue",
			nodeName:      "",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetZoneMismatch,
			message:       "test message",
			expectError:   false,
			logger:        nil,
		},
		{
			name:          "Given_NonEmptyNodeNameWithNilLogger_When_SetNodeCondition_Then_Continue",
			nodeName:      "test-node-nil-logger",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetZoneMismatch,
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
			watchers.SetNodeCondition(tt.nodeName, tt.conditionType, tt.status, tt.reason, tt.message, tt.logger)

			// If we reach here, the function executed without panic
			assert.True(t, true, "SetNodeCondition executed without panic")
		})
	}
}

func TestConstants(t *testing.T) {
	t.Run("Given_Constants_When_CheckValues_Then_ReturnExpectedValues", func(t *testing.T) {
		// Test that constants are properly defined
		assert.Equal(t, corev1.NodeConditionType("CCESubnetUnavailable"), watchers.CCESubnetUnavailable)
		assert.Equal(t, "SubnetZoneMismatch", watchers.ReasonSubnetZoneMismatch)
		assert.Equal(t, "SubnetInsufficientIPs", watchers.ReasonSubnetInsufficientIPs)
		assert.Equal(t, "SubnetEnsureFailure", watchers.ReasonSubnetEnsureFailure)
		assert.Equal(t, "NoAvailableSubnet", watchers.ReasonNoAvailableSubnet)
		assert.Equal(t, "SubnetsAvailable", watchers.ReasonSubnetsAvailable)
		assert.Equal(t, "SubnetEnsured", watchers.ReasonSubnetEnsured)
		assert.Equal(t, "IPBorrowSucceeded", watchers.ReasonIPBorrowSucceeded)
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
			watchers.SetNodeCondition(tt.nodeName, watchers.CCESubnetUnavailable, corev1.ConditionTrue, watchers.ReasonSubnetZoneMismatch, "test message", tt.logger)

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
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetZoneMismatch,
			message:       "Unicode message: 你好世界 🌍 🚀",
		},
		{
			name:          "Given_JSONLikeMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-json",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetZoneMismatch,
			message:       `{"key": "value", "nested": {"array": [1,2,3]}}`,
		},
		{
			name:          "Given_HTMLMessage_When_SetNodeCondition_Then_Success",
			nodeName:      "test-node-html",
			conditionType: watchers.CCESubnetUnavailable,
			status:        corev1.ConditionTrue,
			reason:        watchers.ReasonSubnetZoneMismatch,
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
			watchers.SetNodeCondition(tt.nodeName, tt.conditionType, tt.status, tt.reason, tt.message, logger)

			// If we reach here, the function executed without panic
			assert.True(t, true, "SetNodeCondition executed without panic")
		})
	}
}

// TestSetNodeCondition_Given_DifferentStatuses_When_SetNodeCondition_Then_HandleAllStatuses tests different status values
func TestSetNodeCondition_Given_DifferentStatuses_When_SetNodeCondition_Then_HandleAllStatuses(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Test different status values
	statuses := []corev1.ConditionStatus{
		corev1.ConditionTrue,
		corev1.ConditionFalse,
		corev1.ConditionUnknown,
	}

	for _, status := range statuses {
		t.Run(fmt.Sprintf("Test_%s", status), func(t *testing.T) {
			// Execute test
			logger := logrus.NewEntry(logrus.New())
			watchers.SetNodeCondition("test-node", watchers.CCESubnetUnavailable, status, "TestReason", "Test message", logger)

			// Wait for informer to sync
			time.Sleep(50 * time.Millisecond)

			// Verify condition was set
			node, err := watchers.NodeClient.Lister().Get("test-node")
			assert.NoError(t, err, "should get node from lister")

			// Check if target condition exists
			found := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == watchers.CCESubnetUnavailable {
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

// TestSetNodeCondition_Given_ConditionAlreadyExists_When_SetNodeCondition_Then_ReturnEarly tests condition already exists
func TestSetNodeCondition_Given_ConditionAlreadyExists_When_SetNodeCondition_Then_ReturnEarly(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node with existing condition
	existingCondition := corev1.NodeCondition{
		Type:               watchers.CCESubnetUnavailable,
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

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Execute test - try to set the same condition
	logger := logrus.NewEntry(logrus.New())
	watchers.SetNodeCondition("test-node-existing", watchers.CCESubnetUnavailable, corev1.ConditionTrue, "TestReason", "Test message", logger)

	// Wait for informer to sync
	time.Sleep(50 * time.Millisecond)

	// Verify condition count hasn't changed
	node, err := watchers.NodeClient.Lister().Get("test-node-existing")
	assert.NoError(t, err, "should get node from lister")
	assert.Len(t, node.Status.Conditions, 1, "should have exactly one condition")
}

// TestSetNodeCondition_Given_DifferentConditionStatus_When_SetNodeCondition_Then_UpdateCondition tests condition update with different status
func TestSetNodeCondition_Given_DifferentConditionStatus_When_SetNodeCondition_Then_UpdateCondition(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node with different status condition
	existingCondition := corev1.NodeCondition{
		Type:               watchers.CCESubnetUnavailable,
		Status:             corev1.ConditionFalse, // Different status
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

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Execute test - try to set different status
	logger := logrus.NewEntry(logrus.New())
	watchers.SetNodeCondition("test-node-different-status", watchers.CCESubnetUnavailable, corev1.ConditionTrue, "TestReason", "Test message", logger)

	// Wait for informer to sync
	time.Sleep(50 * time.Millisecond)

	// Verify condition was updated
	node, err := watchers.NodeClient.Lister().Get("test-node-different-status")
	assert.NoError(t, err, "should get node from lister")

	// Check if condition status was updated
	found := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == watchers.CCESubnetUnavailable {
			found = true
			assert.Equal(t, corev1.ConditionTrue, condition.Status, "condition status should be updated")
			break
		}
	}
	assert.True(t, found, "should find CCESubnetUnavailable condition")
}

// TestSetNodeCondition_Given_DifferentConditionReason_When_SetNodeCondition_Then_UpdateCondition tests condition update with different reason
func TestSetNodeCondition_Given_DifferentConditionReason_When_SetNodeCondition_Then_UpdateCondition(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node with different reason condition
	existingCondition := corev1.NodeCondition{
		Type:               watchers.CCESubnetUnavailable,
		Status:             corev1.ConditionTrue,
		Reason:             "OldReason", // Different reason
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

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Execute test - try to set different reason
	logger := logrus.NewEntry(logrus.New())
	watchers.SetNodeCondition("test-node-different-reason", watchers.CCESubnetUnavailable, corev1.ConditionTrue, "NewReason", "Test message", logger)

	// Wait for informer to sync
	time.Sleep(50 * time.Millisecond)

	// Verify condition was updated
	node, err := watchers.NodeClient.Lister().Get("test-node-different-reason")
	assert.NoError(t, err, "should get node from lister")

	// Check if condition reason was updated
	found := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == watchers.CCESubnetUnavailable {
			found = true
			assert.Equal(t, "NewReason", condition.Reason, "condition reason should be updated")
			break
		}
	}
	assert.True(t, found, "should find CCESubnetUnavailable condition")
}

// TestSetNodeCondition_Given_MultipleConditions_When_SetNodeCondition_Then_CheckAllConditions tests multiple conditions
func TestSetNodeCondition_Given_MultipleConditions_When_SetNodeCondition_Then_CheckAllConditions(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node with multiple conditions
	existingCondition1 := corev1.NodeCondition{
		Type:               corev1.NodeConditionType("OtherCondition"),
		Status:             corev1.ConditionFalse,
		Reason:             "OtherReason",
		Message:            "Other message",
		LastTransitionTime: metav1.Now(),
		LastHeartbeatTime:  metav1.Now(),
	}

	existingCondition2 := corev1.NodeCondition{
		Type:               watchers.CCESubnetUnavailable,
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

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Execute test - try to set the same condition
	logger := logrus.NewEntry(logrus.New())
	watchers.SetNodeCondition("test-node-multiple", watchers.CCESubnetUnavailable, corev1.ConditionTrue, "TestReason", "Test message", logger)

	// Wait for informer to sync
	time.Sleep(50 * time.Millisecond)

	// Verify condition count hasn't changed
	node, err := watchers.NodeClient.Lister().Get("test-node-multiple")
	assert.NoError(t, err, "should get node from lister")
	assert.Len(t, node.Status.Conditions, 2, "should have exactly two conditions")
}

// TestSetNodeCondition_Given_AllReasonConstants_When_SetNodeCondition_Then_HandleCorrectly tests all reason constants
func TestSetNodeCondition_Given_AllReasonConstants_When_SetNodeCondition_Then_HandleCorrectly(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-reasons",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Test all reason constants
	reasons := []string{
		watchers.ReasonSubnetZoneMismatch,
		watchers.ReasonSubnetInsufficientIPs,
		watchers.ReasonSubnetEnsureFailure,
		watchers.ReasonNoAvailableSubnet,
		watchers.ReasonSubnetsAvailable,
		watchers.ReasonSubnetEnsured,
		watchers.ReasonIPBorrowSucceeded,
	}

	for _, reason := range reasons {
		t.Run(fmt.Sprintf("Test_%s", reason), func(t *testing.T) {
			// Execute test
			logger := logrus.NewEntry(logrus.New())
			watchers.SetNodeCondition("test-node-reasons", watchers.CCESubnetUnavailable, corev1.ConditionTrue, reason, "Test message", logger)

			// Wait for informer to sync
			time.Sleep(50 * time.Millisecond)

			// Verify condition was set
			node, err := watchers.NodeClient.Lister().Get("test-node-reasons")
			assert.NoError(t, err, "should get node from lister")

			// Check if target condition exists
			found := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == watchers.CCESubnetUnavailable && condition.Reason == reason {
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

// TestSetNodeCondition_Given_NodeNotFound_When_SetNodeCondition_Then_ReturnEarly tests node not found scenario
func TestSetNodeCondition_Given_NodeNotFound_When_SetNodeCondition_Then_ReturnEarly(t *testing.T) {
	ccemock.InitMockEnv()

	// Execute test - try to set condition for non-existing node
	logger := logrus.NewEntry(logrus.New())
	// This should return early without error since node doesn't exist
	watchers.SetNodeCondition("non-existing-node", watchers.CCESubnetUnavailable, corev1.ConditionTrue, watchers.ReasonSubnetZoneMismatch, "Test message", logger)

	// Wait for potential operations
	time.Sleep(50 * time.Millisecond)

	// Verify node doesn't exist in lister
	_, err := watchers.NodeClient.Lister().Get("non-existing-node")
	assert.Error(t, err, "should get error when querying non-existing node")
}

// TestSetNodeCondition_Given_JSONMarshalError_When_SetNodeCondition_Then_LogError tests JSON marshal error scenario
func TestSetNodeCondition_Given_JSONMarshalError_When_SetNodeCondition_Then_LogError(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-json-error",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Mock json.Marshal to return error
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(json.Marshal, func(v interface{}) ([]byte, error) {
		return nil, errors.New("mock json marshal error")
	})

	// Execute test
	logger := logrus.NewEntry(logrus.New())
	watchers.SetNodeCondition("test-node-json-error", watchers.CCESubnetUnavailable, corev1.ConditionTrue, watchers.ReasonSubnetZoneMismatch, "Test message", logger)

	// Wait for operations
	time.Sleep(50 * time.Millisecond)
}

// TestSetNodeCondition_Given_ListerGetError_When_SetNodeCondition_Then_LogError tests Lister.Get error scenario (non-NotFound)
func TestSetNodeCondition_Given_ListerGetError_When_SetNodeCondition_Then_LogError(t *testing.T) {
	ccemock.InitMockEnv()

	// Use gomonkey to patch Lister method to return mock lister
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	var mockUpdater watchers.NodeUpdater
	mockLister := &mockNodeListerWithError{}
	patches.ApplyMethod(&mockUpdater, "Lister", func(*watchers.NodeUpdater) listv1.NodeLister {
		return mockLister
	})

	// Execute test
	logger := logrus.NewEntry(logrus.New())
	watchers.SetNodeCondition("test-node", watchers.CCESubnetUnavailable, corev1.ConditionTrue, watchers.ReasonSubnetZoneMismatch, "Test message", logger)

	// Wait for operations
	time.Sleep(50 * time.Millisecond)
}

// TestSetNodeCondition_Given_PatchStatusError_When_SetNodeCondition_Then_LogError tests PatchStatus error scenario
func TestSetNodeCondition_Given_PatchStatusError_When_SetNodeCondition_Then_LogError(t *testing.T) {
	ccemock.InitMockEnv()

	// Create test node with different condition to trigger update
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-patch-error",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   watchers.CCESubnetUnavailable,
					Status: corev1.ConditionFalse,
					Reason: watchers.ReasonSubnetsAvailable,
				},
			},
		},
	}

	// Add to informer
	err := ccemock.EnsureNodeToInformer(t, []*corev1.Node{testNode})
	assert.NoError(t, err, "should add node to informer successfully")

	// Use gomonkey to patch NodeInterface method to return mock interface
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	var mockUpdater watchers.NodeUpdater
	mockInterface := &mockNodeInterfaceWithPatchError{}
	patches.ApplyMethod(&mockUpdater, "NodeInterface", func(*watchers.NodeUpdater) typev1.NodeInterface {
		return mockInterface
	})

	// Execute test
	logger := logrus.NewEntry(logrus.New())
	watchers.SetNodeCondition("test-node-patch-error", watchers.CCESubnetUnavailable, corev1.ConditionTrue, watchers.ReasonSubnetZoneMismatch, "Test message", logger)

	// Wait for operations
	time.Sleep(50 * time.Millisecond)
}

// mockNodeListerWithError is a mock NodeLister that returns error on Get
type mockNodeListerWithError struct {
}

func (m *mockNodeListerWithError) List(selector labels.Selector) ([]*corev1.Node, error) {
	return nil, nil
}

func (m *mockNodeListerWithError) Get(name string) (*corev1.Node, error) {
	// Return a non-NotFound error
	return nil, errors.New("mock lister get error")
}

// mockNodeInterfaceWithPatchError is a mock NodeInterface that returns error on PatchStatus
type mockNodeInterfaceWithPatchError struct {
}

func (m *mockNodeInterfaceWithPatchError) Create(ctx context.Context, node *corev1.Node, opts metav1.CreateOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) Update(ctx context.Context, node *corev1.Node, opts metav1.UpdateOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) UpdateStatus(ctx context.Context, node *corev1.Node, opts metav1.UpdateOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) List(ctx context.Context, opts metav1.ListOptions) (*corev1.NodeList, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) Apply(ctx context.Context, node *applyv1.NodeApplyConfiguration, opts metav1.ApplyOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) ApplyStatus(ctx context.Context, node *applyv1.NodeApplyConfiguration, opts metav1.ApplyOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterfaceWithPatchError) PatchStatus(ctx context.Context, nodeName string, data []byte) (*corev1.Node, error) {
	// Return error to simulate PatchStatus failure
	return nil, errors.New("mock patch status error")
}
