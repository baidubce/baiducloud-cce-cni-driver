package agent

import (
	"fmt"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/stretchr/testify/assert"
)

// TestGetFeatureList_FunctionExists tests that the GetFeatureList function exists and can be called
func TestGetFeatureList_FunctionExists(t *testing.T) {
	// Setup mock metadata client
	mockClient := &mockMetadataClient{
		featureList: &metadata.FeatureList{
			ENIQuota: "10",
			ERIQuota: "20",
		},
		genericErr: nil,
	}
	SetMetadataClient(mockClient)
	defer ResetMetadataClient()

	// Act - Call the function to ensure it exists and doesn't panic
	result, err := GetFeatureList()

	// Assert - We expect a successful result
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "10", result.ENIQuota)
	assert.Equal(t, "20", result.ERIQuota)
}

// TestGetFeatureList_CallsDefaultMetaClient tests that GetFeatureList calls the default metadata client
func TestGetFeatureList_CallsDefaultMetaClient(t *testing.T) {
	// Setup mock metadata client
	mockClient := &mockMetadataClient{
		featureList: &metadata.FeatureList{
			ENIQuota: "5",
			ERIQuota: "15",
		},
		genericErr: nil,
	}
	SetMetadataClient(mockClient)
	defer ResetMetadataClient()

	// Act - Call the function multiple times
	result1, err1 := GetFeatureList()
	result2, err2 := GetFeatureList()

	// Assert - Both calls should succeed with the same results
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NotNil(t, result1)
	assert.NotNil(t, result2)
	assert.Equal(t, "5", result1.ENIQuota)
	assert.Equal(t, "5", result2.ENIQuota)
	assert.Equal(t, "15", result1.ERIQuota)
	assert.Equal(t, "15", result2.ERIQuota)
}

// TestGetFeatureList_ReturnType tests that GetFeatureList returns the correct types
func TestGetFeatureList_ReturnType(t *testing.T) {
	// Setup mock metadata client
	mockClient := &mockMetadataClient{
		featureList: &metadata.FeatureList{
			ENIQuota: "8",
			ERIQuota: "12",
		},
		genericErr: nil,
	}
	SetMetadataClient(mockClient)
	defer ResetMetadataClient()

	// Act
	result, err := GetFeatureList()

	// Assert - Verify return types are correct
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.IsType(t, "", result.ENIQuota)
	assert.IsType(t, "", result.ERIQuota)
	assert.Equal(t, "8", result.ENIQuota)
	assert.Equal(t, "12", result.ERIQuota)
}

// mockMetadataClient is a mock implementation of the metadataClient interface for testing.
type mockMetadataClient struct {
	resourceID          string
	featureList         *metadata.FeatureList
	genericErr          error
	sourceType          string
	instanceSpecs       string
	getSourceTypeCalled int
	vpcID               string
	instanceTypeEx      metadata.InstanceTypeEx
	instanceType        metadata.InstanceType
	availabilityZone    string
	instanceID          string
	subnetID            string
	primaryIP           string
	gateway             string
	mtu                 int
}

func (m *mockMetadataClient) GetVPCID() (string, error) { return m.vpcID, m.genericErr }
func (m *mockMetadataClient) GetInstanceTypeEx() (metadata.InstanceTypeEx, error) {
	return m.instanceTypeEx, m.genericErr
}
func (m *mockMetadataClient) GetSourceType() (string, error) {
	m.getSourceTypeCalled++
	return m.sourceType, m.genericErr
}
func (m *mockMetadataClient) GetInstanceType() (metadata.InstanceType, error) {
	return m.instanceType, m.genericErr
}

func (m *mockMetadataClient) GetInstanceName() (string, error) { return "", m.genericErr }
func (m *mockMetadataClient) GetAvailabilityZone() (string, error) {
	return m.availabilityZone, m.genericErr
}
func (m *mockMetadataClient) GetRegion() (string, error)        { return "", m.genericErr }
func (m *mockMetadataClient) GetLocalIPv4() (string, error)     { return "", m.genericErr }
func (m *mockMetadataClient) GetInstanceID() (string, error)    { return m.instanceID, m.genericErr }
func (m *mockMetadataClient) GetResourceID() (string, error)    { return m.resourceID, m.genericErr }
func (m *mockMetadataClient) GetSubnetID() (string, error)      { return m.subnetID, m.genericErr }
func (m *mockMetadataClient) GetInstanceSpecs() (string, error) { return m.instanceSpecs, m.genericErr }

func (m *mockMetadataClient) GetLinkPrimaryIP(macAddress string) (string, error) {
	if m.primaryIP != "" {
		return m.primaryIP, nil
	}
	return "", m.genericErr
}
func (m *mockMetadataClient) GetLinkGateway(macAddress, ip string) (string, error) {
	if m.gateway != "" {
		return m.gateway, nil
	}
	return "", m.genericErr
}
func (m *mockMetadataClient) GetLinkMask(macAddress, ip string) (string, error) {
	return "", m.genericErr
}
func (m *mockMetadataClient) GetLinkFixedIPs(macAddress string) ([]string, error) {
	return nil, m.genericErr
}
func (m *mockMetadataClient) GetLinkSecondaryIPs(macAddress string) ([]string, error) {
	return nil, m.genericErr
}
func (m *mockMetadataClient) GetLinkMTU(macAddress string) (int, error) {
	if m.mtu > 0 {
		return m.mtu, nil
	}
	return 0, m.genericErr
}
func (m *mockMetadataClient) GetVifFeatures(macAddress string) (string, error) {
	return "", m.genericErr
}
func (m *mockMetadataClient) GetFeatureList() (*metadata.FeatureList, error) {
	return m.featureList, m.genericErr
}
func (m *mockMetadataClient) ListMacs() ([]string, error) { return nil, m.genericErr }

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

// TestInitMetadataClient tests InitMetadataClient function
func TestInitMetadataClient(t *testing.T) {
	// Just verify it doesn't panic
	metadata.InitMetadataClient(
		option.Config.MetaAPIRetryCount,
		option.Config.MetaAPIRetryMaxDelay,
		option.Config.MetaAPIRetryBaseInterval,
	)
	assert.NotNil(t, metadata.DefaultMetaClient)
}

// TestGetInstanceMetadata tests GetInstanceMetadata function
func TestGetInstanceMetadata(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			vpcID:            "vpc-123",
			instanceTypeEx:   metadata.InstanceTypeExBCC,
			sourceType:       "BCC",
			instanceType:     metadata.InstanceType("N3"),
			availabilityZone: "cn-bj-a",
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		vpcID, instanceType, instanceSpecType, availabilityZone, err := GetInstanceMetadata()

		assert.NoError(t, err)
		assert.Equal(t, "vpc-123", vpcID)
		assert.Equal(t, "bcc", instanceType)
		assert.Equal(t, "N3", instanceSpecType)
		assert.Equal(t, "cn-bj-a", availabilityZone)
	})

	t.Run("HPAS instance", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			vpcID:            "vpc-456",
			instanceTypeEx:   metadata.InstanceTypeExBCC,
			sourceType:       "HPAS",
			instanceType:     metadata.InstanceType("N3"),
			availabilityZone: "cn-bj-b",
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		vpcID, instanceType, instanceSpecType, availabilityZone, err := GetInstanceMetadata()

		assert.NoError(t, err)
		assert.Equal(t, "vpc-456", vpcID)
		assert.Equal(t, "hpas", instanceType)
		assert.Equal(t, "N3", instanceSpecType)
		assert.Equal(t, "cn-bj-b", availabilityZone)
	})

	t.Run("GetVPCID error", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			genericErr: fmt.Errorf("vpc error"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		_, _, _, _, err := GetInstanceMetadata()
		assert.Error(t, err)
	})
}

// TestGetInstanceSpecs tests GetInstanceSpecs function
func TestGetInstanceSpecs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			instanceSpecs: "bcc.g1.c2m4",
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		specs, err := GetInstanceSpecs()

		assert.NoError(t, err)
		assert.Equal(t, "bcc.g1.c2m4", specs)
	})

	t.Run("error", func(t *testing.T) {
		mockErr := fmt.Errorf("metadata service unavailable")
		mockClient := &mockMetadataClient{
			genericErr: mockErr,
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		specs, err := GetInstanceSpecs()

		assert.Error(t, err)
		assert.Equal(t, "", specs)
	})
}

// TestGetInstanceType tests GetInstanceType function
func TestGetInstanceType(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			instanceType: metadata.InstanceType("N3"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		result, err := GetInstanceType()

		assert.NoError(t, err)
		assert.Equal(t, metadata.InstanceType("N3"), result)
	})

	t.Run("error", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			genericErr: fmt.Errorf("metadata error"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		_, err := GetInstanceType()
		assert.Error(t, err)
	})
}

// TestGetInstanceTypeEx tests GetInstanceTypeEx function
func TestGetInstanceTypeEx(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			instanceTypeEx: metadata.InstanceTypeExBCC,
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		result, err := GetInstanceTypeEx()

		assert.NoError(t, err)
		assert.Equal(t, metadata.InstanceTypeExBCC, result)
	})

	t.Run("error", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			genericErr: fmt.Errorf("metadata error"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		_, err := GetInstanceTypeEx()
		assert.Error(t, err)
	})
}

// TestGetVpcIDByName tests getVpcIDByName function
func TestGetVpcIDByName(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			vpcID: "vpc-test123",
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		result, err := getVpcIDByName()

		assert.NoError(t, err)
		assert.Equal(t, "vpc-test123", result)
	})

	t.Run("error", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			genericErr: fmt.Errorf("vpc error"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		_, err := getVpcIDByName()
		assert.Error(t, err)
	})
}

// TestGetInstanceID tests GetInstanceID function
func TestGetInstanceID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			instanceID: "i-test123",
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		result, err := GetInstanceID()

		assert.NoError(t, err)
		assert.Equal(t, "i-test123", result)
	})

	t.Run("error", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			genericErr: fmt.Errorf("instance id error"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		_, err := GetInstanceID()
		assert.Error(t, err)
	})
}

// TestGenerateENISpec_GetSubnetIDError tests GenerateENISpec function with GetSubnetID error (lines 157-158)
func TestGenerateENISpec_GetSubnetIDError(t *testing.T) {
	// Save original config
	originalENI := option.Config.ENI
	defer func() {
		option.Config.ENI = originalENI
	}()

	// Set up test config
	option.Config.ENI = &api.ENISpec{
		PreAllocateENI:   1,
		MaxAllocateENI:   5,
		SubnetIDs:        []string{},
		SecurityGroups:   []string{"sg-test"},
		RouteTableOffset: 100,
	}

	t.Run("BBC instance GetSubnetID error", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			vpcID:            "vpc-123",
			instanceTypeEx:   metadata.InstanceTypeExBBC,
			sourceType:       "BBC",
			instanceType:     metadata.InstanceType("bbc.g5.c48m192"),
			availabilityZone: "cn-bj-a",
			genericErr:       fmt.Errorf("subnet error"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		_, err := GenerateENISpec()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subnet error")
	})

	t.Run("HPAS instance GetSubnetID error", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			vpcID:            "vpc-456",
			instanceTypeEx:   metadata.InstanceTypeExBCC,
			sourceType:       "HPAS",
			instanceType:     metadata.InstanceType("hpas.g5.c48m192"),
			availabilityZone: "cn-bj-b",
			genericErr:       fmt.Errorf("subnet not found"),
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		_, err := GenerateENISpec()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subnet not found")
	})

	t.Run("BBC instance GetSubnetID success", func(t *testing.T) {
		mockClient := &mockMetadataClient{
			vpcID:            "vpc-789",
			instanceTypeEx:   metadata.InstanceTypeExBBC,
			sourceType:       "BBC",
			instanceType:     metadata.InstanceType("bbc.g5.c48m192"),
			availabilityZone: "cn-bj-c",
			subnetID:         "sbn-test123",
		}
		SetMetadataClient(mockClient)
		defer ResetMetadataClient()

		eni, err := GenerateENISpec()
		assert.NoError(t, err)
		assert.NotNil(t, eni)
		assert.Equal(t, 0, eni.PreAllocateENI)
		assert.Equal(t, 0, eni.MaxAllocateENI)
		assert.Contains(t, eni.SubnetIDs, "sbn-test123")
	})
}
