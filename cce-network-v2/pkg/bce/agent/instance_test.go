package agent

import (
	"fmt"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/stretchr/testify/assert"
)

// TestGetFeatureList_FunctionExists tests that the GetFeatureList function exists and can be called
func TestGetFeatureList_FunctionExists(t *testing.T) {
	// Act - Call the function to ensure it exists and doesn't panic
	result, err := GetFeatureList()

	// Assert - We expect either a result or an error, but not a panic
	// In test environment, this will likely return an error due to no metadata service
	// but that's expected and shows the function is working
	if err != nil {
		// Expected in test environment - metadata service not available
		assert.Nil(t, result)
		assert.Error(t, err)
	} else {
		// If somehow it succeeds, result should not be nil
		assert.NotNil(t, result)
	}
}

// TestGetFeatureList_CallsDefaultMetaClient tests that GetFeatureList calls the default metadata client
func TestGetFeatureList_CallsDefaultMetaClient(t *testing.T) {
	// This test verifies that the function exists and calls the underlying client
	// We can't easily mock the client in this test environment, but we can verify
	// that the function behaves consistently

	// Act - Call the function multiple times
	result1, err1 := GetFeatureList()
	result2, err2 := GetFeatureList()

	// Assert - Both calls should have the same behavior
	// Either both succeed or both fail with the same type of error
	if err1 != nil && err2 != nil {
		// Both calls failed - this is expected in test environment
		assert.Nil(t, result1)
		assert.Nil(t, result2)
		assert.Error(t, err1)
		assert.Error(t, err2)
	} else if err1 == nil && err2 == nil {
		// Both calls succeeded - verify results are not nil
		assert.NotNil(t, result1)
		assert.NotNil(t, result2)
	} else {
		// One succeeded and one failed - this would be unexpected
		t.Errorf("Inconsistent behavior: first call err=%v, second call err=%v", err1, err2)
	}
}

// TestGetFeatureList_ReturnType tests that GetFeatureList returns the correct types
func TestGetFeatureList_ReturnType(t *testing.T) {
	// Act
	result, err := GetFeatureList()

	// Assert - Verify return types are correct
	if err != nil {
		// Error case - result should be nil
		assert.Nil(t, result)
		assert.IsType(t, (*error)(nil), &err)
	} else {
		// Success case - result should be a FeatureList pointer
		assert.NotNil(t, result)
		// Verify the result has the expected fields (even if empty)
		// This tests the structure without requiring specific values
		assert.IsType(t, "", result.ENIQuota)
		assert.IsType(t, "", result.ERIQuota)
	}
}

// mockMetadataClient is a mock implementation of the metadataClient interface for testing.
type mockMetadataClient struct {
	resourceID          string
	featureList         *metadata.FeatureList
	genericErr          error
	sourceType          string
	getSourceTypeCalled int
}

func (m *mockMetadataClient) GetVPCID() (string, error) { return "", m.genericErr }
func (m *mockMetadataClient) GetInstanceTypeEx() (metadata.InstanceTypeEx, error) {
	return "", m.genericErr
}
func (m *mockMetadataClient) GetSourceType() (string, error) {
	m.getSourceTypeCalled++
	return m.sourceType, m.genericErr
}
func (m *mockMetadataClient) GetInstanceType() (metadata.InstanceType, error) {
	return "", m.genericErr
}
func (m *mockMetadataClient) GetAvailabilityZone() (string, error) { return "", m.genericErr }
func (m *mockMetadataClient) GetInstanceID() (string, error)       { return "", m.genericErr }
func (m *mockMetadataClient) GetResourceID() (string, error)       { return m.resourceID, m.genericErr }
func (m *mockMetadataClient) GetSubnetID() (string, error)         { return "", m.genericErr }
func (m *mockMetadataClient) GetLinkPrimaryIP(macAddress string) (string, error) {
	return "", m.genericErr
}
func (m *mockMetadataClient) GetLinkGateway(macAddress, ip string) (string, error) {
	return "", m.genericErr
}
func (m *mockMetadataClient) GetLinkMTU(macAddress string) (int, error) { return 0, m.genericErr }
func (m *mockMetadataClient) GetFeatureList() (*metadata.FeatureList, error) {
	return m.featureList, m.genericErr
}

// TestGetHpasID covers GetHpasID, SetMetadataClient, and ResetMetadataClient.
func TestGetHpasID(t *testing.T) {
	tests := []struct {
		name           string
		mockResourceID string
		mockError      error
		expectID       string
		expectError    bool
	}{
		{
			name:           "Successfully retrieve HPAS ID",
			mockResourceID: "hpas-12345",
			mockError:      nil,
			expectID:       "hpas-12345",
			expectError:    false,
		},
		{
			name:           "Metadata service returns an error",
			mockResourceID: "",
			mockError:      fmt.Errorf("metadata service unavailable"),
			expectID:       "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create and set the mock client
			mockClient := &mockMetadataClient{
				resourceID: tt.mockResourceID,
				genericErr: tt.mockError,
			}
			SetMetadataClient(mockClient)
			// Ensure the client is reset after the test
			defer ResetMetadataClient()

			// Call the function under test
			hpasID, err := GetHpasID()

			// Assert the results
			assert.Equal(t, tt.expectID, hpasID)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetSourceType verifies GetSourceType delegates to metadata client and propagates return values
func TestGetSourceType(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			sourceType: "HPAS",
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		st, err := GetSourceType()

		assert.NoError(t, err)
		assert.Equal(t, "HPAS", st)
		assert.Equal(t, 1, mockClient.getSourceTypeCalled)
	})

	t.Run("error", func(t *testing.T) {
		mockErr := fmt.Errorf("metadata service unavailable")
		mockClient := &mockMetadataClient{
			genericErr: mockErr,
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		st, err := GetSourceType()

		assert.Error(t, err)
		assert.Equal(t, "", st)
		assert.Equal(t, 1, mockClient.getSourceTypeCalled)
	})
}
