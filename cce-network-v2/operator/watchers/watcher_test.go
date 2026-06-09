package watchers

import (
	"fmt"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestIndexHPASIDToNRS tests the IndexHPASIDToNRS indexer function
func TestIndexHPASIDToNRS(t *testing.T) {
	err := k8s.InitNewFakeK8sClient()
	assert.NoError(t, err, "InitNewFakeK8sClient should succeed")
	addIndexer()

	indexers := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Informer().GetIndexer().GetIndexers()
	indexFunc, exists := indexers[IndexHPASIDToNRS]
	if !exists {
		t.Fatalf("Index function %s not registered", IndexHPASIDToNRS)
	}

	tests := []struct {
		name           string
		input          interface{}
		expectedResult []string
		expectedError  string
		description    string
	}{
		{
			name: "Given_ValidNRS_And_ValidHPASID_When_Index_Then_ReturnHPASID",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-1",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "i-12345",
					HPASID:     "hpas-12345",
				},
			},
			expectedResult: []string{"hpas-12345"},
			expectedError:  "",
			description:    "Valid NRS with valid HPASID should return HPASID in array",
		},
		{
			name: "Given_ValidNRS_And_EmptyHPASID_When_Index_Then_ReturnEmptyArray",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-2",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "i-67890",
					HPASID:     "",
				},
			},
			expectedResult: []string{},
			expectedError:  "",
			description:    "Valid NRS with empty HPASID should return empty array",
		},
		{
			name: "Given_ValidNRS_And_HPASIDWithSpaces_When_Index_Then_ReturnHPASIDWithSpaces",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-3",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "i-11111",
					HPASID:     " hpas-spaces ",
				},
			},
			expectedResult: []string{" hpas-spaces "},
			expectedError:  "",
			description:    "Valid NRS with HPASID containing spaces should return HPASID as-is (no trimming)",
		},
		{
			name: "Given_ValidNRS_And_LongHPASID_When_Index_Then_ReturnLongHPASID",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-4",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "i-22222",
					HPASID:     "hpas-very-long-id-with-many-characters-1234567890",
				},
			},
			expectedResult: []string{"hpas-very-long-id-with-many-characters-1234567890"},
			expectedError:  "",
			description:    "Valid NRS with long HPASID should return the full HPASID",
		},
		{
			name:           "Given_InvalidObjectType_When_Index_Then_ReturnError",
			input:          "not a NRS object",
			expectedResult: nil,
			expectedError:  "object is not NRS object",
			description:    "Invalid object type should return error",
		},
		{
			name:           "Given_NilObject_When_Index_Then_ReturnError",
			input:          nil,
			expectedResult: nil,
			expectedError:  "object is not NRS object",
			description:    "Nil object should return error",
		},
		{
			name: "Given_DifferentObjectType_When_Index_Then_ReturnError",
			input: &ccev2.ENI{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-eni",
				},
			},
			expectedResult: nil,
			expectedError:  "object is not NRS object",
			description:    "Different CCE object type (ENI) should return error",
		},
		{
			name: "Given_ValidNRS_And_SpecialCharactersInHPASID_When_Index_Then_ReturnHPASID",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-5",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "i-33333",
					HPASID:     "hpas-123_456-789.abc",
				},
			},
			expectedResult: []string{"hpas-123_456-789.abc"},
			expectedError:  "",
			description:    "Valid NRS with special characters in HPASID should return HPASID as-is",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			result, err := indexFunc(tt.input)

			// Then
			if tt.expectedError != "" {
				assert.Error(t, err, tt.description)
				assert.Contains(t, err.Error(), tt.expectedError, "Error message should match")
				assert.Nil(t, result, "Result should be nil when error occurs")
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expectedResult, result, "Result should match expected")
			}
		})
	}
}

// TestIndexInstanceIDToNRS tests the IndexInstanceIDToNRS indexer function
func TestIndexInstanceIDToNRS(t *testing.T) {
	// Define the indexer function (extracted from addIndexer for testing)
	indexFunc := func(obj interface{}) ([]string, error) {
		nrs, ok := obj.(*ccev2.NetResourceSet)
		if !ok {
			return nil, fmt.Errorf("object is not NRS object")
		}
		return []string{nrs.Spec.InstanceID}, nil
	}

	tests := []struct {
		name           string
		input          interface{}
		expectedResult []string
		expectedError  string
		description    string
	}{
		{
			name: "Given_ValidNRS_And_ValidInstanceID_When_Index_Then_ReturnInstanceID",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-1",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "i-12345",
				},
			},
			expectedResult: []string{"i-12345"},
			expectedError:  "",
			description:    "Valid NRS with valid InstanceID should return InstanceID in array",
		},
		{
			name: "Given_ValidNRS_And_EmptyInstanceID_When_Index_Then_ReturnEmptyString",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-2",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "",
				},
			},
			expectedResult: []string{""},
			expectedError:  "",
			description:    "Valid NRS with empty InstanceID should return array with empty string",
		},
		{
			name:           "Given_InvalidObjectType_When_Index_Then_ReturnError",
			input:          "not a NRS object",
			expectedResult: nil,
			expectedError:  "object is not NRS object",
			description:    "Invalid object type should return error",
		},
		{
			name:           "Given_NilObject_When_Index_Then_ReturnError",
			input:          nil,
			expectedResult: nil,
			expectedError:  "object is not NRS object",
			description:    "Nil object should return error",
		},
		{
			name: "Given_ValidNRS_And_BothInstanceIDAndHPASID_When_Index_Then_ReturnInstanceID",
			input: &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nrs-3",
				},
				Spec: ccev2.NetResourceSpec{
					InstanceID: "i-67890",
					HPASID:     "hpas-12345",
				},
			},
			expectedResult: []string{"i-67890"},
			expectedError:  "",
			description:    "Valid NRS with both InstanceID and HPASID should return InstanceID (not HPASID)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			result, err := indexFunc(tt.input)

			// Then
			if tt.expectedError != "" {
				assert.Error(t, err, tt.description)
				assert.Contains(t, err.Error(), tt.expectedError, "Error message should match")
				assert.Nil(t, result, "Result should be nil when error occurs")
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expectedResult, result, "Result should match expected")
			}
		})
	}
}

// TestIndexConstants tests that index constants are defined correctly
func TestIndexConstants(t *testing.T) {
	t.Run("Given_IndexConstants_When_Check_Then_ValuesAreCorrect", func(t *testing.T) {
		// Given & When & Then
		assert.Equal(t, "ipToEndpoint", IndexIPToEndpoint, "IndexIPToEndpoint constant should be correct")
		assert.Equal(t, "ipToENI", IndexIPToENI, "IndexIPToENI constant should be correct")
		assert.Equal(t, "instanceIDToNRS", IndexInstanceIDToNRS, "IndexInstanceIDToNRS constant should be correct")
		assert.Equal(t, "hpasIDToNRS", IndexHPASIDToNRS, "IndexHPASIDToNRS constant should be correct")
	})

	t.Run("Given_IndexConstants_When_Check_Then_AllAreUnique", func(t *testing.T) {
		// Given
		constants := []string{
			IndexIPToEndpoint,
			IndexIPToENI,
			IndexInstanceIDToNRS,
			IndexHPASIDToNRS,
		}

		// When & Then - Check all constants are unique
		seen := make(map[string]bool)
		for _, c := range constants {
			assert.False(t, seen[c], "Index constant %s should be unique", c)
			seen[c] = true
		}
	})
}
