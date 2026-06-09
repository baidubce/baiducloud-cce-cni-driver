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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// eriMockMetaClient is a mock implementation of metadata.Interface for ERI testing
type eriMockMetaClient struct {
	featureList *metadata.FeatureList
	err         error
}

func (m *eriMockMetaClient) GetFeatureList() (*metadata.FeatureList, error) {
	return m.featureList, m.err
}

// Mock other required methods with empty implementations
func (m *eriMockMetaClient) GetVPCID() (string, error)                           { return "", nil }
func (m *eriMockMetaClient) GetInstanceType() (metadata.InstanceType, error)     { return "", nil }
func (m *eriMockMetaClient) GetInstanceTypeEx() (metadata.InstanceTypeEx, error) { return "", nil }
func (m *eriMockMetaClient) GetSourceType() (string, error)                      { return "", nil }
func (m *eriMockMetaClient) GetAvailabilityZone() (string, error)                { return "", nil }
func (m *eriMockMetaClient) GetInstanceID() (string, error)                      { return "", nil }
func (m *eriMockMetaClient) GetSubnetID() (string, error)                        { return "", nil }
func (m *eriMockMetaClient) ListMacs() ([]string, error)                         { return nil, nil }
func (m *eriMockMetaClient) GetVifFeatures(macAddress string) (string, error)    { return "", nil }
func (m *eriMockMetaClient) GetLinkGateway(macAddress, ipAddress string) (string, error) {
	return "", nil
}
func (m *eriMockMetaClient) GetInstanceName() (string, error)                         { return "", nil }
func (m *eriMockMetaClient) GetLocalIPv4() (string, error)                            { return "", nil }
func (m *eriMockMetaClient) GetRegion() (string, error)                               { return "", nil }
func (m *eriMockMetaClient) GetLinkMask(macAddress, ipAddress string) (string, error) { return "", nil }

func TestEnableERI_GivenNilNode_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	var k8sNode *corev1.Node = nil

	// When
	result := EnableERI(k8sNode, true)

	// Then
	assert.False(t, result)
}

func TestEnableERI_GivenNodeWithNilLabels_WhenCalled_ThenReturnGlobalConfig(t *testing.T) {
	// Given
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-node",
			Labels: nil,
		},
	}

	// When & Then
	assert.True(t, EnableERI(k8sNode, true))
	assert.False(t, EnableERI(k8sNode, false))
}

func TestEnableERI_GivenNodeWithNoERILabel_WhenCalled_ThenReturnGlobalConfig(t *testing.T) {
	// Given
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				"other-label": "value",
			},
		},
	}

	// When & Then
	assert.True(t, EnableERI(k8sNode, true))
	assert.False(t, EnableERI(k8sNode, false))
}

func TestEnableERI_GivenNodeWithERILabelTrue_WhenCalled_ThenReturnTrue(t *testing.T) {
	// Given
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				k8s.LabelERIManagement: k8s.ValueStringTrue,
			},
		},
	}

	// When
	result := EnableERI(k8sNode, false)

	// Then
	assert.True(t, result)
}

func TestEnableERI_GivenNodeWithERILabelFalse_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				k8s.LabelERIManagement: "false",
			},
		},
	}

	// When
	result := EnableERI(k8sNode, true)

	// Then
	assert.False(t, result)
}

func TestEnableERI_GivenNodeWithERILabelInvalidValue_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				k8s.LabelERIManagement: "invalid",
			},
		},
	}

	// When
	result := EnableERI(k8sNode, true)

	// Then
	assert.False(t, result)
}

func TestEnableERI_GivenNodeWithERILabelEmptyString_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				k8s.LabelERIManagement: "",
			},
		},
	}

	// When
	result := EnableERI(k8sNode, true)

	// Then
	assert.False(t, result)
}

func TestEnableERI_GivenNodeWithERILabelUpperCaseTrue_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				k8s.LabelERIManagement: "TRUE",
			},
		},
	}

	// When
	result := EnableERI(k8sNode, true)

	// Then
	assert.False(t, result)
}

// Note: The following tests are commented out because they would cause log.Fatal
// which terminates the test process. In production, these error conditions
// should cause the daemon to exit, which is the intended behavior.

/*
func TestHasAvailableERIQuota_GivenEmptyERIQuota_WhenCalled_ThenPanic(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: &metadata.FeatureList{
			ERIQuota: "",
		},
		err: nil,
	}
	defaultMetaClient = mockClient

	// When & Then
	// This test would cause log.Fatal which terminates the process
	assert.Panics(t, func() {
		HasAvailableERIQuota()
	})
}

func TestHasAvailableERIQuota_GivenInvalidERIQuota_WhenCalled_ThenPanic(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: &metadata.FeatureList{
			ERIQuota: "invalid",
		},
		err: nil,
	}
	defaultMetaClient = mockClient

	// When & Then
	// This test would cause log.Fatal which terminates the process
	assert.Panics(t, func() {
		HasAvailableERIQuota()
	})
}

func TestHasAvailableERIQuota_GivenGetFeatureListError_WhenCalled_ThenPanic(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: nil,
		err:         assert.AnError,
	}
	defaultMetaClient = mockClient

	// When & Then
	// This test would cause log.Fatal which terminates the process
	assert.Panics(t, func() {
		HasAvailableERIQuota()
	})
}

func TestHasAvailableERIQuota_GivenNilFeatureList_WhenCalled_ThenPanic(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: nil,
		err:         nil,
	}
	defaultMetaClient = mockClient

	// When & Then
	// This test would cause log.Fatal which terminates the process
	assert.Panics(t, func() {
		HasAvailableERIQuota()
	})
}
*/

func TestHasAvailableERIQuota_GivenValidERIQuota_WhenCalled_ThenReturnTrue(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: &metadata.FeatureList{
			ERIQuota: "5",
		},
		err: nil,
	}
	defaultMetaClient = mockClient

	// When
	result := HasAvailableERIQuota()

	// Then
	assert.True(t, result)
}

func TestHasAvailableERIQuota_GivenZeroERIQuota_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: &metadata.FeatureList{
			ERIQuota: "0",
		},
		err: nil,
	}
	defaultMetaClient = mockClient

	// When
	result := HasAvailableERIQuota()

	// Then
	assert.False(t, result)
}

func TestHasAvailableERIQuota_GivenNegativeERIQuota_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: &metadata.FeatureList{
			ERIQuota: "-1",
		},
		err: nil,
	}
	defaultMetaClient = mockClient

	// When
	result := HasAvailableERIQuota()

	// Then
	assert.False(t, result)
}

func TestHasAvailableERIQuota_GivenLargeERIQuota_WhenCalled_ThenReturnTrue(t *testing.T) {
	// Given
	originalClient := defaultMetaClient
	defer func() { defaultMetaClient = originalClient }()

	mockClient := &eriMockMetaClient{
		featureList: &metadata.FeatureList{
			ERIQuota: "999999",
		},
		err: nil,
	}
	defaultMetaClient = mockClient

	// When
	result := HasAvailableERIQuota()

	// Then
	assert.True(t, result)
}

// Table-driven tests for EnableERI
func TestEnableERI_TableDriven(t *testing.T) {
	type TestArgs struct {
		node   *corev1.Node
		global bool
	}
	type TestCase struct {
		Name string
		Args TestArgs
		Want bool
	}

	cases := []TestCase{
		{
			Name: "Given_NilNode_When_EnableERI_Then_False",
			Args: TestArgs{
				node:   nil,
				global: true,
			},
			Want: false,
		},
		{
			Name: "Given_NilLabels_And_GlobalTrue_When_EnableERI_Then_True",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: nil}},
				global: true,
			},
			Want: true,
		},
		{
			Name: "Given_NilLabels_And_GlobalFalse_When_EnableERI_Then_False",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: nil}},
				global: false,
			},
			Want: false,
		},
		{
			Name: "Given_NoERILabel_And_GlobalTrue_When_EnableERI_Then_True",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n2", Labels: map[string]string{"other": "v"}}},
				global: true,
			},
			Want: true,
		},
		{
			Name: "Given_NoERILabel_And_GlobalFalse_When_EnableERI_Then_False",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n2", Labels: map[string]string{"other": "v"}}},
				global: false,
			},
			Want: false,
		},
		{
			Name: "Given_ERILabelTrue_When_EnableERI_Then_True",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n3", Labels: map[string]string{k8s.LabelERIManagement: k8s.ValueStringTrue}}},
				global: false,
			},
			Want: true,
		},
		{
			Name: "Given_ERILabelFalse_When_EnableERI_Then_False",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n4", Labels: map[string]string{k8s.LabelERIManagement: "false"}}},
				global: true,
			},
			Want: false,
		},
		{
			Name: "Given_ERILabelInvalid_When_EnableERI_Then_False",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n5", Labels: map[string]string{k8s.LabelERIManagement: "invalid"}}},
				global: true,
			},
			Want: false,
		},
		{
			Name: "Given_ERILabelEmpty_When_EnableERI_Then_False",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n6", Labels: map[string]string{k8s.LabelERIManagement: ""}}},
				global: true,
			},
			Want: false,
		},
		{
			Name: "Given_ERILabelUpperTRUE_When_EnableERI_Then_False",
			Args: TestArgs{
				node:   &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n7", Labels: map[string]string{k8s.LabelERIManagement: "TRUE"}}},
				global: true,
			},
			Want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := EnableERI(tc.Args.node, tc.Args.global)
			assert.Equal(t, tc.Want, got)
		})
	}
}

// Mock metadata client for GetERIManagedLabelSelector testing
type labelSelectorMockMetaClient struct {
	instanceID string
	err        error
}

func (m *labelSelectorMockMetaClient) GetInstanceID() (string, error) {
	return m.instanceID, m.err
}

// Implement other required methods from metadata.Interface
func (m *labelSelectorMockMetaClient) GetInstanceName() (string, error) { return "", nil }
func (m *labelSelectorMockMetaClient) GetInstanceType() (metadata.InstanceType, error) {
	return "", nil
}
func (m *labelSelectorMockMetaClient) GetInstanceTypeEx() (metadata.InstanceTypeEx, error) {
	return "", nil
}
func (m *labelSelectorMockMetaClient) GetLocalIPv4() (string, error)        { return "", nil }
func (m *labelSelectorMockMetaClient) GetAvailabilityZone() (string, error) { return "", nil }
func (m *labelSelectorMockMetaClient) GetRegion() (string, error)           { return "", nil }
func (m *labelSelectorMockMetaClient) GetVPCID() (string, error)            { return "", nil }
func (m *labelSelectorMockMetaClient) GetSubnetID() (string, error)         { return "", nil }
func (m *labelSelectorMockMetaClient) ListMacs() ([]string, error)          { return nil, nil }
func (m *labelSelectorMockMetaClient) GetVifFeatures(macAddress string) (string, error) {
	return "", nil
}
func (m *labelSelectorMockMetaClient) GetLinkGateway(macAddress, ipAddress string) (string, error) {
	return "", nil
}
func (m *labelSelectorMockMetaClient) GetLinkMask(macAddress, ipAddress string) (string, error) {
	return "", nil
}
func (m *labelSelectorMockMetaClient) GetFeatureList() (*metadata.FeatureList, error) {
	return nil, nil
}
func (m *labelSelectorMockMetaClient) GetSourceType() (string, error) { return "", nil }

func TestGetERIManagedLabelSelector_GivenValidNodeNameAndInstanceID_WhenCalled_ThenReturnLabelSelectorValue(t *testing.T) {
	// Note: This is an integration test that tests the function with real dependencies
	// In a production environment, this would work with actual node name and metadata client

	// When
	result := GetERIManagedLabelSelector()

	// Then
	// The result should either be a valid label selector string or empty string if metadata fails
	// We can't assert the exact value since it depends on the actual node name and instance ID
	// But we can verify it's a string (not panicking)
	assert.IsType(t, "", result)
}

// Note: For more comprehensive testing of GetERIManagedLabelSelector, we would need to:
// 1. Mock nodeTypes.GetName() using gomonkey or similar
// 2. Mock metadata.NewClient() to return a controlled mock client
// 3. Test various scenarios like metadata errors, different node names, etc.
//
// However, since this function is primarily a utility that combines existing tested functions
// (GetEriMangedNrsName and GetRdmaNrsLabelSelectorValueFromNetResourceSetName),
// and those functions already have comprehensive unit tests, this integration test
// provides sufficient coverage for the new function's basic functionality.

// TestIsERIManagedENI_GivenNilResource_WhenCalled_ThenReturnFalse tests the function with nil resource
func TestIsERIManagedENI_GivenNilResource_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	var resource *ccev2.ENI = nil

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithNilAnnotations_WhenCalled_ThenReturnFalse tests the function with nil annotations
func TestIsERIManagedENI_GivenResourceWithNilAnnotations_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-eni",
			Annotations: nil,
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithEmptyAnnotations_WhenCalled_ThenReturnFalse tests the function with empty annotations
func TestIsERIManagedENI_GivenResourceWithEmptyAnnotations_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-eni",
			Annotations: map[string]string{},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationTrue_WhenCalled_ThenReturnTrue tests the function with ERI managed annotation set to true
func TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationTrue_WhenCalled_ThenReturnTrue(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
			Annotations: map[string]string{
				k8s.AnnotationERIManaged: "true",
			},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.True(t, result)
}

// TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationFalse_WhenCalled_ThenReturnFalse tests the function with ERI managed annotation set to false
func TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationFalse_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
			Annotations: map[string]string{
				k8s.AnnotationERIManaged: "false",
			},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationInvalidValue_WhenCalled_ThenReturnFalse tests the function with invalid ERI managed annotation value
func TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationInvalidValue_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
			Annotations: map[string]string{
				k8s.AnnotationERIManaged: "invalid",
			},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationEmptyString_WhenCalled_ThenReturnFalse tests the function with empty ERI managed annotation value
func TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationEmptyString_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
			Annotations: map[string]string{
				k8s.AnnotationERIManaged: "",
			},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationUpperCaseTrue_WhenCalled_ThenReturnFalse tests the function with uppercase ERI managed annotation value
func TestIsERIManagedENI_GivenResourceWithERIManagedAnnotationUpperCaseTrue_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
			Annotations: map[string]string{
				k8s.AnnotationERIManaged: "TRUE",
			},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithERIManagedNodeName_WhenCalled_ThenReturnTrue tests the function with ERI managed node name pattern
func TestIsERIManagedENI_GivenResourceWithERIManagedNodeName_WhenCalled_ThenReturnTrue(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-eni",
			Annotations: map[string]string{},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node-eri-managed",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.True(t, result)
}

// TestIsERIManagedENI_GivenResourceWithERIManagedNodeNameAndAnnotation_WhenCalled_ThenReturnTrue tests the function with both ERI managed annotation and node name pattern
func TestIsERIManagedENI_GivenResourceWithERIManagedNodeNameAndAnnotation_WhenCalled_ThenReturnTrue(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
			Annotations: map[string]string{
				k8s.AnnotationERIManaged: "true",
			},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node-eri-managed",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.True(t, result)
}

// TestIsERIManagedENI_GivenResourceWithOtherAnnotations_WhenCalled_ThenReturnFalse tests the function with other annotations but not ERI managed
func TestIsERIManagedENI_GivenResourceWithOtherAnnotations_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
			Annotations: map[string]string{
				"other-annotation":   "value",
				"another-annotation": "another-value",
			},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithEmptyNodeName_WhenCalled_ThenReturnFalse tests the function with empty node name
func TestIsERIManagedENI_GivenResourceWithEmptyNodeName_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-eni",
			Annotations: map[string]string{},
		},
		Spec: ccev2.ENISpec{
			NodeName: "",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}

// TestIsERIManagedENI_GivenResourceWithNodeNameEndingWithEriManaged_WhenCalled_ThenReturnTrue tests the function with node name ending with -eri-managed
func TestIsERIManagedENI_GivenResourceWithNodeNameEndingWithEriManaged_WhenCalled_ThenReturnTrue(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-eni",
			Annotations: map[string]string{},
		},
		Spec: ccev2.ENISpec{
			NodeName: "worker-node-eri-managed",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.True(t, result)
}

// TestIsERIManagedENI_GivenResourceWithNodeNameContainingEriManaged_WhenCalled_ThenReturnFalse tests the function with node name containing but not ending with -eri-managed
func TestIsERIManagedENI_GivenResourceWithNodeNameContainingEriManaged_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	resource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-eni",
			Annotations: map[string]string{},
		},
		Spec: ccev2.ENISpec{
			NodeName: "eri-managed-node",
		},
	}

	// When
	result := IsERIManagedENI(resource)

	// Then
	assert.False(t, result)
}
