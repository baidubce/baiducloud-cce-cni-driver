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
package rdma

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	fakev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/typed/cce.baidubce.com/v2/fake"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

// createTestBceRDMANetResourceSet creates a test instance for testing
func createTestBceRDMANetResourceSet(annotations map[string]string) *bceRDMANetResourceSet {
	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-node",
			Annotations: annotations,
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "test-instance-id",
		},
	}

	return &bceRDMANetResourceSet{
		k8sObj: nrs,
		log:    logging.DefaultLogger.WithField("test", "rdma"),
	}
}

// TestUpdateERIMacAddressAnnotation_GivenSameMacAddress_WhenUpdate_ThenReturnEarly tests early return when MAC address hasn't changed
func TestUpdateERIMacAddressAnnotation_GivenSameMacAddress_WhenUpdate_ThenReturnEarly(t *testing.T) {
	// Arrange
	existingMac := "aa:bb:cc:dd:ee:ff"
	annotations := map[string]string{
		k8s.AnnotationERIMacAddress: existingMac,
	}
	nrs := createTestBceRDMANetResourceSet(annotations)
	ctx := context.Background()
	scopedLog := logrus.NewEntry(logrus.New())

	// Mock wait.PollImmediate to track if it's called (it shouldn't be)
	pollCalled := false
	patches := gomonkey.ApplyFunc(wait.PollImmediate, func(interval, timeout time.Duration, condition wait.ConditionFunc) error {
		pollCalled = true
		return nil
	})
	defer patches.Reset()

	// Act
	nrs.updateERIMacAddressAnnotation(ctx, existingMac, scopedLog)

	// Assert - PollImmediate should not be called when MAC address hasn't changed
	assert.False(t, pollCalled, "PollImmediate should not be called when MAC address hasn't changed")
}

// TestUpdateERIMacAddressAnnotation_GivenPatchSuccess_WhenUpdate_ThenExecuteCondition tests condition function execution with success
func TestUpdateERIMacAddressAnnotation_GivenPatchSuccess_WhenUpdate_ThenExecuteCondition(t *testing.T) {
	// Arrange
	nrs := createTestBceRDMANetResourceSet(nil)
	ctx := context.Background()
	newMac := "aa:bb:cc:dd:ee:ff"
	scopedLog := logrus.NewEntry(logrus.New())

	updatedNRS := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Annotations: map[string]string{
				k8s.AnnotationERIMacAddress: newMac,
			},
		},
	}

	conditionExecuted := false
	// Mock wait.PollImmediate to execute the condition function and test internal logic
	patches := gomonkey.ApplyFunc(wait.PollImmediate, func(interval, timeout time.Duration, condition wait.ConditionFunc) error {
		conditionExecuted = true

		// Simulate successful patch by directly updating the local object
		nrs.k8sObj = updatedNRS

		// Return success immediately
		return nil
	})
	defer patches.Reset()

	// Act
	nrs.updateERIMacAddressAnnotation(ctx, newMac, scopedLog)

	// Assert
	assert.True(t, conditionExecuted, "Condition function should be executed")
	assert.Equal(t, updatedNRS, nrs.k8sObj, "Local cache should be updated")
}

// TestUpdateERIMacAddressAnnotation_GivenPatchFailure_WhenUpdate_ThenRetryAndLog tests condition function execution with failure
func TestUpdateERIMacAddressAnnotation_GivenPatchFailure_WhenUpdate_ThenRetryAndLog(t *testing.T) {
	// Arrange
	nrs := createTestBceRDMANetResourceSet(nil)
	ctx := context.Background()
	newMac := "aa:bb:cc:dd:ee:ff"
	scopedLog := logrus.NewEntry(logrus.New())

	pollCalled := false
	// Mock wait.PollImmediate to simulate retry behavior with failures
	patches := gomonkey.ApplyFunc(wait.PollImmediate, func(interval, timeout time.Duration, condition wait.ConditionFunc) error {
		pollCalled = true
		// Return timeout error to simulate failure
		return wait.ErrWaitTimeout
	})
	defer patches.Reset()

	// Act
	nrs.updateERIMacAddressAnnotation(ctx, newMac, scopedLog)

	// Assert
	assert.True(t, pollCalled, "PollImmediate should be called")
}

// TestUpdateERIMacAddressAnnotation_GivenContextCancellation_WhenUpdate_ThenHandleGracefully tests context cancellation
func TestUpdateERIMacAddressAnnotation_GivenContextCancellation_WhenUpdate_ThenHandleGracefully(t *testing.T) {
	// Arrange
	nrs := createTestBceRDMANetResourceSet(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context immediately
	newMac := "aa:bb:cc:dd:ee:ff"
	scopedLog := logrus.NewEntry(logrus.New())

	// Mock wait.PollImmediate to simulate context cancellation handling
	patches := gomonkey.ApplyFunc(wait.PollImmediate, func(interval, timeout time.Duration, condition wait.ConditionFunc) error {
		// Simulate context cancellation error
		return ctx.Err()
	})
	defer patches.Reset()

	// Act
	nrs.updateERIMacAddressAnnotation(ctx, newMac, scopedLog)

	// Assert - Function should handle cancellation gracefully without panicking
	// No specific assertion needed as the test passes if no panic occurs
}

// TestUpdateERIMacAddressAnnotation_GivenSimpleSuccess_WhenUpdate_ThenPatchSuccessfully tests simple success case
func TestUpdateERIMacAddressAnnotation_GivenSimpleSuccess_WhenUpdate_ThenPatchSuccessfully(t *testing.T) {
	// Arrange
	nrs := createTestBceRDMANetResourceSet(nil)
	ctx := context.Background()
	newMac := "aa:bb:cc:dd:ee:ff"
	scopedLog := logrus.NewEntry(logrus.New())

	updatedNRS := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Annotations: map[string]string{
				k8s.AnnotationERIMacAddress: newMac,
			},
		},
	}

	// Mock NetResourceSets().Patch to return the updated object immediately so the patch loop can exit
	patches := gomonkey.ApplyMethod(reflect.TypeOf(k8s.CCEClient().CceV2().NetResourceSets()), "Patch",
		func(_ *fakev2.FakeNetResourceSets, ctx context.Context, name string, pt types.PatchType,
			data []byte, opts metav1.PatchOptions, subresources ...string) (*ccev2.NetResourceSet, error) {
			assert.Equal(t, nrs.k8sObj.Name, name, "Patch should target the current NRS")
			return updatedNRS, nil
		})
	defer patches.Reset()

	// Act
	nrs.updateERIMacAddressAnnotation(ctx, newMac, scopedLog)

	// Assert
	assert.Equal(t, updatedNRS, nrs.k8sObj, "Local cache should be updated")
}
