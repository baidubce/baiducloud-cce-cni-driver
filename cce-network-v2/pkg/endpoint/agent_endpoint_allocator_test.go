package endpoint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

// Helper functions for creating test objects
func createTestPod(name, namespace string, hasRDMA bool, rdmaResourceType string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-uid"),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}

	if hasRDMA {
		resourceKey := fmt.Sprintf("%s/%s", bceutils.PodResourceName, rdmaResourceType)
		pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName(resourceKey): resource.MustParse("1"),
			},
		}
	}

	return pod
}

// createTestPodWithMultipleRDMAResources creates a pod with multiple RDMA resource types
func createTestPodWithMultipleRDMAResources(name, namespace string, rdmaResourceTypes []string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-uid"),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}

	if len(rdmaResourceTypes) > 0 {
		requests := make(corev1.ResourceList)
		limits := make(corev1.ResourceList)

		for i, resourceType := range rdmaResourceTypes {
			resourceKey := fmt.Sprintf("%s/%s", bceutils.PodResourceName, resourceType)
			resourceName := corev1.ResourceName(resourceKey)

			// Add to requests
			requests[resourceName] = resource.MustParse("1")

			// Add to limits for even-indexed resources
			if i%2 == 0 {
				limits[resourceName] = resource.MustParse("2")
			}
		}

		pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
			Requests: requests,
			Limits:   limits,
		}
	}

	return pod
}

func TestIsRDMAResourceMatched(t *testing.T) {
	// Save original config
	originalHPCType := option.Config.RDMAResourceTypeForHPC
	originalERIType := option.Config.RDMAResourceTypeForERI

	// Restore config after test
	defer func() {
		option.Config.RDMAResourceTypeForHPC = originalHPCType
		option.Config.RDMAResourceTypeForERI = originalERIType
	}()

	tests := []struct {
		name                string
		podRDMAResourceType string
		ipamType            string
		setupConfig         func()
		expected            bool
	}{
		{
			name:                "TestIsRDMAResourceMatched_GivenDefaultHPCConfig_WhenHCAResourceMatchesUnderlayRDMA_ThenReturnTrue",
			podRDMAResourceType: "hca",
			ipamType:            bceutils.UnderlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: true,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenDefaultERIConfig_WhenEHCAResourceMatchesOverlayRDMA_ThenReturnTrue",
			podRDMAResourceType: "ehca",
			ipamType:            bceutils.OverlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: true,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenDefaultConfig_WhenHCAResourceWithOverlayRDMA_ThenReturnFalse",
			podRDMAResourceType: "hca",
			ipamType:            bceutils.OverlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenDefaultConfig_WhenEHCAResourceWithUnderlayRDMA_ThenReturnFalse",
			podRDMAResourceType: "ehca",
			ipamType:            bceutils.UnderlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenEmptyPodResourceType_WhenUnderlayRDMA_ThenReturnFalse",
			podRDMAResourceType: "",
			ipamType:            bceutils.UnderlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenEmptyIPAMType_WhenHCAResource_ThenReturnFalse",
			podRDMAResourceType: "hca",
			ipamType:            "",
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenBothEmpty_WhenEmptyInputs_ThenReturnFalse",
			podRDMAResourceType: "",
			ipamType:            "",
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenUnknownResourceType_WhenUnderlayRDMA_ThenReturnFalse",
			podRDMAResourceType: "unknown",
			ipamType:            bceutils.UnderlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenUnknownIPAMType_WhenHCAResource_ThenReturnFalse",
			podRDMAResourceType: "hca",
			ipamType:            "unknown",
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenCustomHPCConfig_WhenCustomResourceMatchesUnderlayRDMA_ThenReturnTrue",
			podRDMAResourceType: "custom_hpc",
			ipamType:            bceutils.UnderlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = "custom_hpc"
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: true,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenCustomERIConfig_WhenCustomResourceMatchesOverlayRDMA_ThenReturnTrue",
			podRDMAResourceType: "custom_eri",
			ipamType:            bceutils.OverlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
				option.Config.RDMAResourceTypeForERI = "custom_eri"
			},
			expected: true,
		},
		{
			name:                "TestIsRDMAResourceMatched_GivenCustomConfig_WhenDefaultResourceType_ThenReturnFalse",
			podRDMAResourceType: "hca",
			ipamType:            bceutils.UnderlayRDMA,
			setupConfig: func() {
				option.Config.RDMAResourceTypeForHPC = "custom_hpc"
				option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup configuration for this test
			tt.setupConfig()

			// Execute the function
			result := isRDMAResourceMatched(tt.podRDMAResourceType, tt.ipamType)

			// Assert the result
			assert.Equal(t, tt.expected, result, "Expected %v but got %v for podRDMAResourceType=%s, ipamType=%s", tt.expected, result, tt.podRDMAResourceType, tt.ipamType)
		})
	}
}

// Test edge cases and boundary conditions
func TestIsRDMAResourceMatched_EdgeCases(t *testing.T) {
	// Save original config
	originalHPCType := option.Config.RDMAResourceTypeForHPC
	originalERIType := option.Config.RDMAResourceTypeForERI

	// Restore config after test
	defer func() {
		option.Config.RDMAResourceTypeForHPC = originalHPCType
		option.Config.RDMAResourceTypeForERI = originalERIType
	}()

	t.Run("TestIsRDMAResourceMatched_GivenCaseSensitiveConfig_WhenUpperCaseResource_ThenReturnFalse", func(t *testing.T) {
		option.Config.RDMAResourceTypeForHPC = "hca"
		option.Config.RDMAResourceTypeForERI = "ehca"

		result := isRDMAResourceMatched("HCA", bceutils.UnderlayRDMA)
		assert.False(t, result, "Function should be case sensitive")
	})

	t.Run("TestIsRDMAResourceMatched_GivenWhitespaceConfig_WhenWhitespaceResource_ThenReturnTrue", func(t *testing.T) {
		option.Config.RDMAResourceTypeForHPC = " hca "
		option.Config.RDMAResourceTypeForERI = "ehca"

		result := isRDMAResourceMatched(" hca ", bceutils.UnderlayRDMA)
		assert.True(t, result, "Function should handle whitespace exactly")
	})

	t.Run("TestIsRDMAResourceMatched_GivenSameResourceTypeForBoth_WhenBothIPAMTypes_ThenReturnTrue", func(t *testing.T) {
		option.Config.RDMAResourceTypeForHPC = "same"
		option.Config.RDMAResourceTypeForERI = "same"

		result1 := isRDMAResourceMatched("same", bceutils.UnderlayRDMA)
		result2 := isRDMAResourceMatched("same", bceutils.OverlayRDMA)

		assert.True(t, result1, "Should match HPC configuration")
		assert.True(t, result2, "Should match ERI configuration")
	})
}

// Test helper functions
func TestGetPodRDMAResourceType(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected string
	}{
		{
			name:     "TestGetPodRDMAResourceType_GivenPodWithoutRDMAResource_WhenNormalPod_ThenReturnEmpty",
			pod:      createTestPod("test-pod", "default", false, ""),
			expected: "",
		},
		{
			name:     "TestGetPodRDMAResourceType_GivenPodWithHCAResource_WhenRDMAPod_ThenReturnHCA",
			pod:      createTestPod("test-pod", "default", true, "hca"),
			expected: "hca",
		},
		{
			name:     "TestGetPodRDMAResourceType_GivenPodWithEHCAResource_WhenRDMAPod_ThenReturnEHCA",
			pod:      createTestPod("test-pod", "default", true, "ehca"),
			expected: "ehca",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPodRDMAResourceType(tt.pod)
			assert.Equal(t, tt.expected, result, "RDMA resource type mismatch")
		})
	}
}

func TestWantRoce(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name:     "TestWantRoce_GivenPodWithoutRDMAResource_WhenNormalPod_ThenReturnFalse",
			pod:      createTestPod("test-pod", "default", false, ""),
			expected: false,
		},
		{
			name:     "TestWantRoce_GivenPodWithHCAResource_WhenRDMAPod_ThenReturnTrue",
			pod:      createTestPod("test-pod", "default", true, "hca"),
			expected: true,
		},
		{
			name:     "TestWantRoce_GivenPodWithEHCAResource_WhenRDMAPod_ThenReturnTrue",
			pod:      createTestPod("test-pod", "default", true, "ehca"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wantRoce(tt.pod)
			assert.Equal(t, tt.expected, result, "wantRoce result mismatch")
		})
	}
}

func TestHasRDMAResource(t *testing.T) {
	tests := []struct {
		name     string
		rl       corev1.ResourceList
		expected bool
	}{
		{
			name:     "TestHasRDMAResource_GivenEmptyResourceList_WhenNoResource_ThenReturnFalse",
			rl:       corev1.ResourceList{},
			expected: false,
		},
		{
			name: "TestHasRDMAResource_GivenResourceListWithRDMAHCA_WhenHCAResource_ThenReturnTrue",
			rl: corev1.ResourceList{
				corev1.ResourceName("rdma/hca"): resource.MustParse("1"),
			},
			expected: true,
		},
		{
			name: "TestHasRDMAResource_GivenResourceListWithRDMAEHCA_WhenEHCAResource_ThenReturnTrue",
			rl: corev1.ResourceList{
				corev1.ResourceName("rdma/ehca"): resource.MustParse("1"),
			},
			expected: true,
		},
		{
			name: "TestHasRDMAResource_GivenResourceListWithNonRDMAResource_WhenCPUResource_ThenReturnFalse",
			rl: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
			expected: false,
		},
		{
			name: "TestHasRDMAResource_GivenResourceListWithInvalidFormat_WhenInvalidResourceName_ThenReturnFalse",
			rl: corev1.ResourceList{
				corev1.ResourceName("invalid-format"): resource.MustParse("1"),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasRDMAResource(tt.rl)
			assert.Equal(t, tt.expected, result, "hasRDMAResource result mismatch")
		})
	}
}

func TestGetRDMAResourceType(t *testing.T) {
	tests := []struct {
		name     string
		rl       corev1.ResourceList
		expected string
	}{
		{
			name:     "TestGetRDMAResourceType_GivenEmptyResourceList_WhenNoResource_ThenReturnEmpty",
			rl:       corev1.ResourceList{},
			expected: "",
		},
		{
			name: "TestGetRDMAResourceType_GivenResourceListWithRDMAHCA_WhenHCAResource_ThenReturnHCA",
			rl: corev1.ResourceList{
				corev1.ResourceName("rdma/hca"): resource.MustParse("1"),
			},
			expected: "hca",
		},
		{
			name: "TestGetRDMAResourceType_GivenResourceListWithRDMAEHCA_WhenEHCAResource_ThenReturnEHCA",
			rl: corev1.ResourceList{
				corev1.ResourceName("rdma/ehca"): resource.MustParse("1"),
			},
			expected: "ehca",
		},
		{
			name: "TestGetRDMAResourceType_GivenResourceListWithNonRDMAResource_WhenCPUResource_ThenReturnEmpty",
			rl: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
			expected: "",
		},
		{
			name: "TestGetRDMAResourceType_GivenResourceListWithInvalidFormat_WhenInvalidResourceName_ThenReturnEmpty",
			rl: corev1.ResourceList{
				corev1.ResourceName("invalid-format"): resource.MustParse("1"),
			},
			expected: "",
		},
		{
			name: "TestGetRDMAResourceType_GivenResourceListWithCustomRDMAType_WhenCustomResource_ThenReturnCustom",
			rl: corev1.ResourceList{
				corev1.ResourceName("rdma/custom"): resource.MustParse("1"),
			},
			expected: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getRDMAResourceType(tt.rl)
			assert.Equal(t, tt.expected, result, "getRDMAResourceType result mismatch")
		})
	}
}

func TestGetPodRDMAResourceTypes(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected []string
	}{
		{
			name:     "TestGetPodRDMAResourceTypes_GivenPodWithoutRDMAResource_WhenNormalPod_ThenReturnEmpty",
			pod:      createTestPod("test-pod", "default", false, ""),
			expected: []string{},
		},
		{
			name:     "TestGetPodRDMAResourceTypes_GivenPodWithHCAResource_WhenSingleRDMAResource_ThenReturnHCA",
			pod:      createTestPod("test-pod", "default", true, "hca"),
			expected: []string{"hca"},
		},
		{
			name:     "TestGetPodRDMAResourceTypes_GivenPodWithEHCAResource_WhenSingleRDMAResource_ThenReturnEHCA",
			pod:      createTestPod("test-pod", "default", true, "ehca"),
			expected: []string{"ehca"},
		},
		{
			name:     "TestGetPodRDMAResourceTypes_GivenPodWithBothHCAndEHCAResources_WhenMultipleRDMAResources_ThenReturnBoth",
			pod:      createTestPodWithMultipleRDMAResources("test-pod", "default", []string{"hca", "ehca"}),
			expected: []string{"hca", "ehca"},
		},
		{
			name:     "TestGetPodRDMAResourceTypes_GivenPodWithDuplicateRDMAResources_WhenSameResourceInLimitsAndRequests_ThenReturnUnique",
			pod:      createTestPodWithMultipleRDMAResources("test-pod", "default", []string{"hca", "hca"}),
			expected: []string{"hca"},
		},
		{
			name: "TestGetPodRDMAResourceTypes_GivenPodWithMultipleContainers_WhenDifferentRDMAResources_ThenReturnAll",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					UID:       types.UID("test-uid"),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
							Image: "test-image1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceName("rdma/hca"): resource.MustParse("1"),
								},
							},
						},
						{
							Name:  "container2",
							Image: "test-image2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceName("rdma/ehca"): resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expected: []string{"hca", "ehca"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPodRDMAResourceTypes(tt.pod)
			// Sort both slices for comparison since order doesn't matter
			assert.ElementsMatch(t, tt.expected, result, "RDMA resource types mismatch")
		})
	}
}

func TestIsRDMAResourceMatchedMulti(t *testing.T) {
	// Save original config
	originalHPCType := option.Config.RDMAResourceTypeForHPC
	originalERIType := option.Config.RDMAResourceTypeForERI

	// Restore config after test
	defer func() {
		option.Config.RDMAResourceTypeForHPC = originalHPCType
		option.Config.RDMAResourceTypeForERI = originalERIType
	}()

	// Setup default config
	option.Config.RDMAResourceTypeForHPC = defaults.RDMAResourceTypeForHPC
	option.Config.RDMAResourceTypeForERI = defaults.RDMAResourceTypeForERI

	tests := []struct {
		name                 string
		podRDMAResourceTypes []string
		ipamType             string
		expected             bool
	}{
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenEmptyResourceTypes_WhenNoResources_ThenReturnFalse",
			podRDMAResourceTypes: []string{},
			ipamType:             bceutils.UnderlayRDMA,
			expected:             false,
		},
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenHCAResourceType_WhenUnderlayRDMA_ThenReturnTrue",
			podRDMAResourceTypes: []string{"hca"},
			ipamType:             bceutils.UnderlayRDMA,
			expected:             true,
		},
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenEHCAResourceType_WhenOverlayRDMA_ThenReturnTrue",
			podRDMAResourceTypes: []string{"ehca"},
			ipamType:             bceutils.OverlayRDMA,
			expected:             true,
		},
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenBothHCAndEHCAResources_WhenUnderlayRDMA_ThenReturnTrue",
			podRDMAResourceTypes: []string{"hca", "ehca"},
			ipamType:             bceutils.UnderlayRDMA,
			expected:             true,
		},
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenBothHCAndEHCAResources_WhenOverlayRDMA_ThenReturnTrue",
			podRDMAResourceTypes: []string{"hca", "ehca"},
			ipamType:             bceutils.OverlayRDMA,
			expected:             true,
		},
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenHCAResourceType_WhenOverlayRDMA_ThenReturnFalse",
			podRDMAResourceTypes: []string{"hca"},
			ipamType:             bceutils.OverlayRDMA,
			expected:             false,
		},
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenEHCAResourceType_WhenUnderlayRDMA_ThenReturnFalse",
			podRDMAResourceTypes: []string{"ehca"},
			ipamType:             bceutils.UnderlayRDMA,
			expected:             false,
		},
		{
			name:                 "TestIsRDMAResourceMatchedMulti_GivenUnmatchedResourceTypes_WhenUnderlayRDMA_ThenReturnFalse",
			podRDMAResourceTypes: []string{"custom", "unknown"},
			ipamType:             bceutils.UnderlayRDMA,
			expected:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRDMAResourceMatchedMulti(tt.podRDMAResourceTypes, tt.ipamType)
			assert.Equal(t, tt.expected, result, "isRDMAResourceMatchedMulti result mismatch")
		})
	}
}
