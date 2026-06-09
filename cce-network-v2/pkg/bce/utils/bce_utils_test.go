package utils

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/stretchr/testify/assert"
)

// test IsAgentMgrENI,eg bcc/bbc/ebc/edma eni
func TestIsAgentMgrENI(t *testing.T) {
	{
		// rdma secondary eni can be managered by agent
		rdmaENI := new(ccev2.ENI)
		rdmaENISpecJson := `{
			"id": "eni-x8p7yanyj6du",
			"instanceID": "i-XTI7e9fM",
			"macAddress": "b8:3f:d2:04:1b:90",
			"name": "eni-x8p7yanyj6du",
			"nodeName": "cce-rdma-test-test-test-test-test-test-test-test1-b83fd2041b90-rdmaroce",
			"routeTableOffset": 0,
			"type": "rdma_roce",
			"useMode": "Secondary",
			"vpcID": "vpc-ggypsz662xdi"
		}`
		var rdmaENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(rdmaENISpecJson), &rdmaENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		rdmaENI.Spec = rdmaENISpec
		fmt.Printf("rdma: %s %s\n", rdmaENI.Spec.Description, rdmaENI.Spec.ID)
		assert.Equal(t, rdmaENI.Spec.Description, "")
		assert.True(t, IsAgentMgrENI(rdmaENI))
	}
	{
		// bbc primary eni can be managered by agent
		bbcPrimaryENI := new(ccev2.ENI)
		bbcPrimaryENISpecJson := `{
			"routeTableOffset": 127,
			"subnetID": "sbn-49qjy0pk2e8r",
			"type": "bbc",
			"useMode": "PrimaryWithSecondaryIP",
			"vpcID": "vpc-t686xe1isp02",
			"vpcVersion": 1,
			"zoneName": "cn-bj-d"
		}`
		var bbcPrimaryENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(bbcPrimaryENISpecJson), &bbcPrimaryENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		bbcPrimaryENI.Spec = bbcPrimaryENISpec
		fmt.Printf("bbc: %s %s\n", bbcPrimaryENI.Spec.Description, bbcPrimaryENI.Spec.ID)
		assert.Equal(t, bbcPrimaryENI.Spec.Description, "")
		assert.True(t, IsAgentMgrENI(bbcPrimaryENI))
	}
	{
		// bcc secondary eni can be managered by agent
		bccSecondaryENI := new(ccev2.ENI)
		bccSecondaryENISpecJson := `{
			"routeTableOffset": 127,
			"subnetID": "sbn-49qjy0pk2e8r",
			"type": "bcc",
			"useMode": "Secondary",
			"description": "auto created by cce-cni, do not modify",
			"vpcID": "vpc-t686xe1isp02",
			"vpcVersion": 1,
			"zoneName": "cn-bj-d"
		}`
		var bccSecondaryENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(bccSecondaryENISpecJson), &bccSecondaryENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		bccSecondaryENI.Spec = bccSecondaryENISpec
		fmt.Printf("bcc: %s %s\n", bccSecondaryENI.Spec.Description, bccSecondaryENI.Spec.ID)
		assert.Equal(t, bccSecondaryENI.Spec.Description, "auto created by cce-cni, do not modify")
		assert.True(t, IsAgentMgrENI(bccSecondaryENI))
	}
	{
		// bcc primary eni can't be managered by agent
		bccPrimaryENI := new(ccev2.ENI)
		bccPrimaryENISpecJson := `{
			"routeTableOffset": 127,
			"subnetID": "sbn-49qjy0pk2e8r",
			"type": "bcc",
			"useMode": "Secondary",
			"description": "custom description",
			"vpcID": "vpc-t686xe1isp02",
			"vpcVersion": 1,
			"zoneName": "cn-bj-d"
		}`
		var bccPrimaryENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(bccPrimaryENISpecJson), &bccPrimaryENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		bccPrimaryENI.Spec = bccPrimaryENISpec
		fmt.Printf("bcc: %s %s\n", bccPrimaryENI.Spec.Description, bccPrimaryENI.Spec.ID)
		assert.Equal(t, bccPrimaryENI.Spec.Description, "custom description")
		assert.False(t, IsAgentMgrENI(bccPrimaryENI))
	}
}

// test IsAgentMgrENI
func TestIsAgentMgrENI_2(t *testing.T) {
	{
		// rdma secondary eni can be managered by agent
		rdmaENI := new(ccev2.ENI)
		rdmaENISpecJson := `{
			"id": "eni-x8p7yanyj6du",
			"instanceID": "i-XTI7e9fM",
			"macAddress": "b8:3f:d2:04:1b:90",
			"name": "eni-x8p7yanyj6du",
			"nodeName": "cce-rdma-test-test-test-test-test-test-test-test1-b83fd2041b90-rdmaroce",
			"routeTableOffset": 0,
			"type": "rdma_roce",
			"useMode": "Secondary",
			"vpcID": "vpc-ggypsz662xdi"
		}`
		var rdmaENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(rdmaENISpecJson), &rdmaENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		rdmaENI.Spec = rdmaENISpec
		fmt.Printf("rdma: %s %s\n", rdmaENI.Spec.Description, rdmaENI.Spec.ID)
		assert.Equal(t, rdmaENI.Spec.Description, "")
		assert.True(t, IsAgentMgrENI(rdmaENI))
	}
	{
		// elastic rdma eni can be managered by agent
		elasticRdmaENI := new(ccev2.ENI)
		elasticRdmaENISpecJson := `{
			"id": "eni-x8p7yanyj6du",
			"instanceID": "i-XTI7e9fM",
			"macAddress": "b8:3f:d2:04:1b:90",
			"name": "eni-x8p7yanyj6du",
			"nodeName": "cce-rdma-test-test-test-test-test-test-test-test1-b83fd2041b90-rdmaroce",
			"routeTableOffset": 0,
			"type": "elastic_rdma",
			"useMode": "Secondary",
			"vpcID": "vpc-ggypsz662xdi"
		}`
		var elasticRdmaENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(elasticRdmaENISpecJson), &elasticRdmaENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		elasticRdmaENI.Spec = elasticRdmaENISpec
		fmt.Printf("elastic rdma: %s %s\n", elasticRdmaENI.Spec.Description, elasticRdmaENI.Spec.ID)
		assert.Equal(t, elasticRdmaENI.Spec.Description, "")
		assert.True(t, IsAgentMgrENI(elasticRdmaENI))
	}
	{
		// bcc secondary eni can be managered by agent
		bccSecondaryENI := new(ccev2.ENI)
		bccSecondaryENISpecJson := `{
			"routeTableOffset": 127,
			"subnetID": "sbn-49qjy0pk2e8r",
			"type": "bcc",
			"useMode": "Secondary",
			"description": "auto created by cce-cni, do not modify",
			"vpcID": "vpc-t686xe1isp02",
			"vpcVersion": 1,
			"zoneName": "cn-bj-d"
		}`
		var bccSecondaryENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(bccSecondaryENISpecJson), &bccSecondaryENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		bccSecondaryENI.Spec = bccSecondaryENISpec
		fmt.Printf("bcc: %s %s\n", bccSecondaryENI.Spec.Description, bccSecondaryENI.Spec.ID)
		assert.Equal(t, bccSecondaryENI.Spec.Description, "auto created by cce-cni, do not modify")
		assert.True(t, IsAgentMgrENI(bccSecondaryENI))
	}
}

// mockMetaClient is a mock implementation of metadata client for testing
type mockMetaClient struct {
	instanceID string
	err        error
}

func (m *mockMetaClient) GetInstanceID() (string, error) {
	return m.instanceID, m.err
}

// Implement other required methods from metadata.Interface
func (m *mockMetaClient) GetInstanceName() (string, error)                    { return "", nil }
func (m *mockMetaClient) GetInstanceType() (metadata.InstanceType, error)     { return "", nil }
func (m *mockMetaClient) GetInstanceTypeEx() (metadata.InstanceTypeEx, error) { return "", nil }
func (m *mockMetaClient) GetLocalIPv4() (string, error)                       { return "", nil }
func (m *mockMetaClient) GetAvailabilityZone() (string, error)                { return "", nil }
func (m *mockMetaClient) GetRegion() (string, error)                          { return "", nil }
func (m *mockMetaClient) GetVPCID() (string, error)                           { return "", nil }
func (m *mockMetaClient) GetSourceType() (string, error)                      { return "", nil }
func (m *mockMetaClient) GetSubnetID() (string, error)                        { return "", nil }
func (m *mockMetaClient) GetLinkGateway(string, string) (string, error)       { return "", nil }
func (m *mockMetaClient) GetLinkMask(string, string) (string, error)          { return "", nil }
func (m *mockMetaClient) GetVifFeatures(macAddress string) (string, error)    { return "", nil }
func (m *mockMetaClient) GetFeatureList() (*metadata.FeatureList, error)      { return nil, nil }
func (m *mockMetaClient) ListMacs() ([]string, error)                         { return nil, nil }

func TestGetEriMangedNrsName(t *testing.T) {
	tests := []struct {
		name           string
		nodeName       string
		expectedResult string
		description    string
	}{
		{
			name:           "TestGetEriMangedNrsName_GivenShortNodeName_WhenGettingEriManagedNrsName_ThenReturnNodeNameWithSuffix",
			nodeName:       "test-node",
			expectedResult: "test-node-eri-managed",
			description:    "Normal node name should use node name + suffix",
		},
		{
			name:           "TestGetEriMangedNrsName_GivenLongNodeName_WhenGettingEriManagedNrsName_ThenReturnNodeNameWithSuffix",
			nodeName:       "very-long-node-name-that-exceeds-the-kubernetes-name-length-limit-of-253-characters-and-needs-to-be-truncated-or-replaced-with-instance-id-to-avoid-validation-errors-in-kubernetes-api-server-when-creating-resources-with-this-name-extra-long-suffix",
			expectedResult: "very-long-node-name-that-exceeds-the-kubernetes-name-length-limit-of-253-characters-and-needs-to-be-truncated-or-replaced-with-instance-id-to-avoid-validation-errors-in-kubernetes-api-server-when-creating-resources-with-this-name-extra-long-suffix-eri-managed",
			description:    "Long node name should use node name + suffix directly",
		},
		{
			name:           "TestGetEriMangedNrsName_GivenExactLengthLimit_WhenGettingEriManagedNrsName_ThenReturnNodeNameWithSuffix",
			nodeName:       "exactly-240-characters-long-node-name-that-is-exactly-at-the-limit-of-253-minus-13-characters-for-the-suffix-this-should-still-use-the-node-name-as-identification-because-it-is-not-exceeding-the-limit-just-reaching-it",
			expectedResult: "exactly-240-characters-long-node-name-that-is-exactly-at-the-limit-of-253-minus-13-characters-for-the-suffix-this-should-still-use-the-node-name-as-identification-because-it-is-not-exceeding-the-limit-just-reaching-it-eri-managed",
			description:    "Node name at exact limit should use node name + suffix",
		},
		{
			name:           "TestGetEriMangedNrsName_GivenEmptyNodeName_WhenGettingEriManagedNrsName_ThenReturnSuffixOnly",
			nodeName:       "",
			expectedResult: "-eri-managed",
			description:    "Empty node name should return suffix only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function with the new signature
			result := GetEriMangedNrsName(tt.nodeName)

			// Verify the result
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

func TestGenerateRdmaNrsLabelSelectorValue(t *testing.T) {
	tests := []struct {
		name           string
		nodeName       string
		nodeInstanceID string
		macAddress     string
		vifFeatures    string
		expectedResult string
		description    string
	}{
		{
			name:           "TestGenerateRdmaNrsLabelSelectorValue_GivenEmptyMacAddress_WhenGeneratingLabelSelectorValue_ThenReturnNodeNameWithEmptyMacAndFeatures",
			nodeName:       "test-node",
			nodeInstanceID: "i-abc123",
			macAddress:     "",
			vifFeatures:    "elastic_rdma",
			expectedResult: "test-node--elasticrdma",
			description:    "When MAC address is empty, should return node name with empty MAC and features",
		},
		{
			name:           "TestGenerateRdmaNrsLabelSelectorValue_GivenShortNodeNameWithMacAddress_WhenGeneratingLabelSelectorValue_ThenReturnNodeNameWithMacAndFeatures",
			nodeName:       "test-node",
			nodeInstanceID: "i-abc123",
			macAddress:     "fa:27:00:07:83:02",
			vifFeatures:    "elastic_rdma",
			expectedResult: "test-node-fa2700078302-elasticrdma",
			description:    "When node name is short with MAC address, should return node name with MAC and features",
		},
		{
			name:           "TestGenerateRdmaNrsLabelSelectorValue_GivenLongNodeNameWithMacAddress_WhenGeneratingLabelSelectorValue_ThenReturnInstanceIDWithMacAndFeatures",
			nodeName:       "very-long-node-name-that-exceeds-the-length-limit-for-label-selector-value",
			nodeInstanceID: "i-abc123",
			macAddress:     "fa:27:00:07:83:02",
			vifFeatures:    "elastic_rdma",
			expectedResult: "i-abc123-fa2700078302-elasticrdma",
			description:    "When node name is too long, should use instance ID with MAC and features",
		},
		{
			name:           "TestGenerateRdmaNrsLabelSelectorValue_GivenMacAddressWithHyphens_WhenGeneratingLabelSelectorValue_ThenReturnWithCleanedMacAddress",
			nodeName:       "test-node",
			nodeInstanceID: "i-abc123",
			macAddress:     "fa-27-00-07-83-02",
			vifFeatures:    "elastic_rdma",
			expectedResult: "test-node-fa2700078302-elasticrdma",
			description:    "When MAC address has hyphens, should remove them from the result",
		},
		{
			name:           "TestGenerateRdmaNrsLabelSelectorValue_GivenVifFeaturesWithUnderscores_WhenGeneratingLabelSelectorValue_ThenReturnWithCleanedVifFeatures",
			nodeName:       "test-node",
			nodeInstanceID: "i-abc123",
			macAddress:     "fa:27:00:07:83:02",
			vifFeatures:    "rdma_roce",
			expectedResult: "test-node-fa2700078302-rdmaroce",
			description:    "When VIF features have underscores, should remove them from the result",
		},
		{
			name:           "TestGenerateRdmaNrsLabelSelectorValue_GivenEmptyMacAddressAndLongNodeName_WhenGeneratingLabelSelectorValue_ThenReturnInstanceIDWithEmptyMacAndFeatures",
			nodeName:       "very-long-node-name-that-exceeds-the-kubernetes-name-length-limit-of-253-characters-and-needs-to-be-truncated-or-replaced-with-instance-id-to-avoid-validation-errors-in-kubernetes-api-server-when-creating-resources-with-this-name-extra-long-suffix",
			nodeInstanceID: "i-abc123",
			macAddress:     "",
			vifFeatures:    "elastic_rdma",
			expectedResult: "i-abc123--elasticrdma",
			description:    "When MAC address is empty and node name is too long, should return instance ID with empty MAC and features",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			result := generateRdmaNrsLabelSelectorValue(tt.nodeName, tt.nodeInstanceID, tt.macAddress, tt.vifFeatures)

			// Verify the result
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

func TestGetRdmaNrsLabelSelectorValueFromNetResourceSetName(t *testing.T) {
	tests := []struct {
		name                   string
		netResourceSetName     string
		ownerReferenceNodeName string
		nodeInstanceID         string
		macAddress             string
		vifFeatures            string
		expectedResult         string
		description            string
	}{
		{
			name:                   "TestGetRdmaNrsLabelSelectorValueFromNetResourceSetName_GivenEriManagedNrsName_WhenGettingLabelSelectorValue_ThenReturnEriManagedSuffix",
			netResourceSetName:     "test-node-eri-managed",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			macAddress:             "",
			vifFeatures:            "elastic_rdma",
			expectedResult:         "test-node-eri-managed",
			description:            "When NRS name is ERI managed, should return ERI managed suffix",
		},
		{
			name:                   "TestGetRdmaNrsLabelSelectorValueFromNetResourceSetName_GivenInstanceIDAsNrsName_WhenGettingLabelSelectorValue_ThenReturnOwnerReferenceNodeName",
			netResourceSetName:     "i-abc123-eri-managed",
			ownerReferenceNodeName: "very-long-node-name-that-exceeds-the-length-limit",
			nodeInstanceID:         "i-abc123",
			macAddress:             "",
			vifFeatures:            "elastic_rdma",
			expectedResult:         "very-long-node-name-that-exceeds-the-length-limit-eri-managed",
			description:            "When NRS name uses instance ID, should return owner reference node name",
		},
		{
			name:                   "TestGetRdmaNrsLabelSelectorValueFromNetResourceSetName_GivenRdmaRoceNrsName_WhenGettingLabelSelectorValue_ThenReturnNodeNameWithMacAndFeatures",
			netResourceSetName:     "test-node-fa2700078302-rdmaroce",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			macAddress:             "fa:27:00:07:83:02",
			vifFeatures:            "rdma_roce",
			expectedResult:         "test-node-fa2700078302-rdmaroce",
			description:            "When NRS name is RDMA ROCE, should return node name with MAC and features",
		},
		{
			name:                   "TestGetRdmaNrsLabelSelectorValueFromNetResourceSetName_GivenElasticRdmaNrsName_WhenGettingLabelSelectorValue_ThenReturnNodeNameWithMacAndFeatures",
			netResourceSetName:     "test-node-fa2700078302-elasticrdma",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			macAddress:             "fa:27:00:07:83:02",
			vifFeatures:            "elastic_rdma",
			expectedResult:         "test-node-fa2700078302-elasticrdma",
			description:            "When NRS name is elastic RDMA, should return node name with MAC and features",
		},
		{
			name:                   "TestGetRdmaNrsLabelSelectorValueFromNetResourceSetName_GivenInstanceIDInRdmaNrsName_WhenGettingLabelSelectorValue_ThenReturnOwnerReferenceNodeName",
			netResourceSetName:     "i-abc123-fa2700078302-rdmaroce",
			ownerReferenceNodeName: "test-node-short",
			nodeInstanceID:         "i-abc123",
			macAddress:             "fa:27:00:07:83:02",
			vifFeatures:            "rdma_roce",
			expectedResult:         "test-node-short-fa2700078302-rdmaroce",
			description:            "When NRS name uses instance ID for RDMA, should return owner reference node name",
		},
		{
			name:                   "TestGetRdmaNrsLabelSelectorValueFromNetResourceSetName_GivenRegularNrsName_WhenGettingLabelSelectorValue_ThenReturnNodeNameWithMacAndFeatures",
			netResourceSetName:     "regular-node-name",
			ownerReferenceNodeName: "regular-node-name",
			nodeInstanceID:         "i-abc123",
			macAddress:             "fa:27:00:07:83:02",
			vifFeatures:            "rdma_roce",
			expectedResult:         "regular-node-name-fa2700078302-rdmaroce",
			description:            "When NRS name is regular (not ERI or RDMA), should return node name with MAC and features",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			result := GetRdmaNrsLabelSelectorValueFromNetResourceSetName(tt.netResourceSetName, tt.ownerReferenceNodeName, tt.nodeInstanceID, tt.macAddress, tt.vifFeatures)

			// Verify the result
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

// Enhanced mockMetaClient for testing GetRdmaIFsInfo functions
type enhancedMockMetaClient struct {
	instanceID       string
	instanceIDError  error
	macList          []string
	macListError     error
	vifFeatures      map[string]string
	vifFeaturesError map[string]error
}

func (m *enhancedMockMetaClient) GetInstanceID() (string, error) {
	return m.instanceID, m.instanceIDError
}

func (m *enhancedMockMetaClient) ListMacs() ([]string, error) {
	return m.macList, m.macListError
}

func (m *enhancedMockMetaClient) GetVifFeatures(macAddress string) (string, error) {
	if m.vifFeaturesError != nil {
		if err, exists := m.vifFeaturesError[macAddress]; exists {
			return "", err
		}
	}
	if m.vifFeatures != nil {
		if features, exists := m.vifFeatures[macAddress]; exists {
			return features, nil
		}
	}
	return "", fmt.Errorf("vif features not found for mac: %s", macAddress)
}

// Implement other required methods from metadata.Interface
func (m *enhancedMockMetaClient) GetInstanceName() (string, error)                    { return "", nil }
func (m *enhancedMockMetaClient) GetInstanceType() (metadata.InstanceType, error)     { return "", nil }
func (m *enhancedMockMetaClient) GetInstanceTypeEx() (metadata.InstanceTypeEx, error) { return "", nil }
func (m *enhancedMockMetaClient) GetLocalIPv4() (string, error)                       { return "", nil }
func (m *enhancedMockMetaClient) GetAvailabilityZone() (string, error)                { return "", nil }
func (m *enhancedMockMetaClient) GetRegion() (string, error)                          { return "", nil }
func (m *enhancedMockMetaClient) GetVPCID() (string, error)                           { return "", nil }
func (m *enhancedMockMetaClient) GetSourceType() (string, error)                      { return "", nil }
func (m *enhancedMockMetaClient) GetSubnetID() (string, error)                        { return "", nil }
func (m *enhancedMockMetaClient) GetFeatureList() (*metadata.FeatureList, error)      { return nil, nil }
func (m *enhancedMockMetaClient) GetLinkGateway(macAddress string, ip string) (string, error) {
	return "", nil
}
func (m *enhancedMockMetaClient) GetLinkMask(macAddress string, ip string) (string, error) {
	return "", nil
}

func TestGetRdmaIFsInfo(t *testing.T) {
	// Save original values
	originalMetaClient := defaultMetaClient
	originalEnableERIManagement := option.Config.EnableERIManagement

	// Restore after test
	defer func() {
		defaultMetaClient = originalMetaClient
		option.Config.EnableERIManagement = originalEnableERIManagement
	}()

	tests := []struct {
		name                         string
		nodeName                     string
		mockClient                   *enhancedMockMetaClient
		enableERIManagement          bool
		expectedRdmaIfInfoCount      int
		expectedError                bool
		expectedMacAddressesInResult []string
		description                  string
	}{
		{
			name:     "TestGetRdmaIFsInfo_GivenRdmaRoceInterface_WhenGettingRdmaIFsInfo_ThenReturnRdmaIfInfo",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "rdma_roce",
					"fa:27:00:07:83:02": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      1,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:01"},
			description:                  "When there is RDMA ROCE interface, should return RdmaIfInfo",
		},
		{
			name:     "TestGetRdmaIFsInfo_GivenElasticRdmaInterface_WhenGettingRdmaIFsInfo_ThenReturnRdmaIfInfo",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "elastic_rdma",
					"fa:27:00:07:83:02": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      1,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:01"},
			description:                  "When there is Elastic RDMA interface, should return RdmaIfInfo",
		},
		{
			name:     "TestGetRdmaIFsInfo_GivenElasticRdmaInterfaceWithERIManagementEnabled_WhenGettingRdmaIFsInfo_ThenSkipERIInterface",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "elastic_rdma",
					"fa:27:00:07:83:02": "rdma_roce",
				},
			},
			enableERIManagement:          true,
			expectedRdmaIfInfoCount:      1,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:02"},
			description:                  "When ERI management is enabled, should skip ERI interface",
		},
		{
			name:     "TestGetRdmaIFsInfo_GivenMixedInterfaces_WhenGettingRdmaIFsInfo_ThenReturnOnlyRdmaInterfaces",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02", "fa:27:00:07:83:03"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "rdma_roce",
					"fa:27:00:07:83:02": "elastic_rdma",
					"fa:27:00:07:83:03": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      2,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
			description:                  "When there are mixed interfaces, should return only RDMA interfaces",
		},
		{
			name:     "TestGetRdmaIFsInfo_GivenNoRdmaInterfaces_WhenGettingRdmaIFsInfo_ThenReturnEmptyMap",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "normal",
					"fa:27:00:07:83:02": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      0,
			expectedError:                false,
			expectedMacAddressesInResult: []string{},
			description:                  "When there are no RDMA interfaces, should return empty map",
		},
		{
			name:     "TestGetRdmaIFsInfo_GivenListMacsError_WhenGettingRdmaIFsInfo_ThenReturnError",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				macListError: fmt.Errorf("failed to list macs"),
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      0,
			expectedError:                true,
			expectedMacAddressesInResult: []string{},
			description:                  "When ListMacs fails, should return error",
		},
		{
			name:     "TestGetRdmaIFsInfo_GivenGetVifFeaturesError_WhenGettingRdmaIFsInfo_ThenSkipErrorInterface",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:02": "rdma_roce",
				},
				vifFeaturesError: map[string]error{
					"fa:27:00:07:83:01": fmt.Errorf("failed to get vif features"),
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      1,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:02"},
			description:                  "When GetVifFeatures fails for some interfaces, should skip error interface",
		},
		{
			name:     "TestGetRdmaIFsInfo_GivenGetInstanceIDError_WhenGettingRdmaIFsInfo_ThenReturnError",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceIDError: fmt.Errorf("failed to get instance ID"),
				macList:         []string{"fa:27:00:07:83:01"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "rdma_roce",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      0,
			expectedError:                true,
			expectedMacAddressesInResult: []string{},
			description:                  "When GetInstanceID fails, should return error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock client
			defaultMetaClient = tt.mockClient
			option.Config.EnableERIManagement = tt.enableERIManagement

			// Call the function
			result, err := GetRdmaIFsInfo(tt.nodeName, nil)

			// Verify error expectation
			if tt.expectedError {
				assert.Error(t, err, tt.description)
				return
			}
			assert.NoError(t, err, tt.description)

			// Verify result count
			assert.Equal(t, tt.expectedRdmaIfInfoCount, len(result), tt.description)

			// Verify expected MAC addresses are in result
			for _, expectedMac := range tt.expectedMacAddressesInResult {
				rdmaIfInfo, exists := result[expectedMac]
				assert.True(t, exists, "Expected MAC address %s should be in result", expectedMac)
				assert.Equal(t, expectedMac, rdmaIfInfo.MacAddress, "MAC address should match")
				assert.NotEmpty(t, rdmaIfInfo.NetResourceSetName, "NetResourceSetName should not be empty")
				assert.NotEmpty(t, rdmaIfInfo.LabelSelectorValue, "LabelSelectorValue should not be empty")
				assert.NotEmpty(t, rdmaIfInfo.VifFeatures, "VifFeatures should not be empty")
			}
		})
	}
}

func TestGetRdmaIFsInfoAll(t *testing.T) {
	// Save original values
	originalMetaClient := defaultMetaClient
	originalEnableERIManagement := option.Config.EnableERIManagement

	// Restore after test
	defer func() {
		defaultMetaClient = originalMetaClient
		option.Config.EnableERIManagement = originalEnableERIManagement
	}()

	tests := []struct {
		name                         string
		nodeName                     string
		mockClient                   *enhancedMockMetaClient
		enableERIManagement          bool
		expectedRdmaIfInfoCount      int
		expectedError                bool
		expectedMacAddressesInResult []string
		description                  string
	}{
		{
			name:     "TestGetRdmaIFsInfoAll_GivenRdmaRoceInterface_WhenGettingRdmaIFsInfoAll_ThenReturnRdmaIfInfo",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "rdma_roce",
					"fa:27:00:07:83:02": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      1,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:01"},
			description:                  "When there is RDMA ROCE interface, should return RdmaIfInfo",
		},
		{
			name:     "TestGetRdmaIFsInfoAll_GivenElasticRdmaInterface_WhenGettingRdmaIFsInfoAll_ThenReturnRdmaIfInfo",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "elastic_rdma",
					"fa:27:00:07:83:02": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      1,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:01"},
			description:                  "When there is Elastic RDMA interface, should return RdmaIfInfo",
		},
		{
			name:     "TestGetRdmaIFsInfoAll_GivenElasticRdmaInterfaceWithERIManagementEnabled_WhenGettingRdmaIFsInfoAll_ThenIncludeERIInterface",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "elastic_rdma",
					"fa:27:00:07:83:02": "rdma_roce",
				},
			},
			enableERIManagement:          true,
			expectedRdmaIfInfoCount:      2,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
			description:                  "GetRdmaIFsInfoAll should include ERI interface even when ERI management is enabled",
		},
		{
			name:     "TestGetRdmaIFsInfoAll_GivenMixedInterfaces_WhenGettingRdmaIFsInfoAll_ThenReturnOnlyRdmaInterfaces",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02", "fa:27:00:07:83:03"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "rdma_roce",
					"fa:27:00:07:83:02": "elastic_rdma",
					"fa:27:00:07:83:03": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      2,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
			description:                  "When there are mixed interfaces, should return only RDMA interfaces",
		},
		{
			name:     "TestGetRdmaIFsInfoAll_GivenNoRdmaInterfaces_WhenGettingRdmaIFsInfoAll_ThenReturnEmptyMap",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "normal",
					"fa:27:00:07:83:02": "normal",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      0,
			expectedError:                false,
			expectedMacAddressesInResult: []string{},
			description:                  "When there are no RDMA interfaces, should return empty map",
		},
		{
			name:     "TestGetRdmaIFsInfoAll_GivenListMacsError_WhenGettingRdmaIFsInfoAll_ThenReturnError",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				macListError: fmt.Errorf("failed to list macs"),
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      0,
			expectedError:                true,
			expectedMacAddressesInResult: []string{},
			description:                  "When ListMacs fails, should return error",
		},
		{
			name:     "TestGetRdmaIFsInfoAll_GivenGetVifFeaturesError_WhenGettingRdmaIFsInfoAll_ThenSkipErrorInterface",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceID: "i-12345678",
				macList:    []string{"fa:27:00:07:83:01", "fa:27:00:07:83:02"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:02": "rdma_roce",
				},
				vifFeaturesError: map[string]error{
					"fa:27:00:07:83:01": fmt.Errorf("failed to get vif features"),
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      1,
			expectedError:                false,
			expectedMacAddressesInResult: []string{"fa:27:00:07:83:02"},
			description:                  "When GetVifFeatures fails for some interfaces, should skip error interface",
		},
		{
			name:     "TestGetRdmaIFsInfoAll_GivenGetInstanceIDError_WhenGettingRdmaIFsInfoAll_ThenReturnError",
			nodeName: "test-node",
			mockClient: &enhancedMockMetaClient{
				instanceIDError: fmt.Errorf("failed to get instance ID"),
				macList:         []string{"fa:27:00:07:83:01"},
				vifFeatures: map[string]string{
					"fa:27:00:07:83:01": "rdma_roce",
				},
			},
			enableERIManagement:          false,
			expectedRdmaIfInfoCount:      0,
			expectedError:                true,
			expectedMacAddressesInResult: []string{},
			description:                  "When GetInstanceID fails, should return error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock client
			defaultMetaClient = tt.mockClient
			option.Config.EnableERIManagement = tt.enableERIManagement

			// Call the function
			result, err := GetRdmaIFsInfoAll(tt.nodeName, nil)

			// Verify error expectation
			if tt.expectedError {
				assert.Error(t, err, tt.description)
				return
			}
			assert.NoError(t, err, tt.description)

			// Verify result count
			assert.Equal(t, tt.expectedRdmaIfInfoCount, len(result), tt.description)

			// Verify expected MAC addresses are in result
			for _, expectedMac := range tt.expectedMacAddressesInResult {
				rdmaIfInfo, exists := result[expectedMac]
				assert.True(t, exists, "Expected MAC address %s should be in result", expectedMac)
				assert.Equal(t, expectedMac, rdmaIfInfo.MacAddress, "MAC address should match")
				assert.NotEmpty(t, rdmaIfInfo.NetResourceSetName, "NetResourceSetName should not be empty")
				assert.NotEmpty(t, rdmaIfInfo.LabelSelectorValue, "LabelSelectorValue should not be empty")
				assert.NotEmpty(t, rdmaIfInfo.VifFeatures, "VifFeatures should not be empty")
			}
		})
	}
}

// TestGenerateEriNrsLabelSelectorValue tests the generateEriNrsLabelSelectorValue function
func TestGenerateEriNrsLabelSelectorValue(t *testing.T) {
	tests := []struct {
		name           string
		nodeName       string
		nodeInstanceID string
		expectedResult string
		description    string
	}{
		{
			name:           "TestGenerateEriNrsLabelSelectorValue_GivenShortNodeName_WhenGeneratingLabelSelectorValue_ThenReturnNodeNameWithSuffix",
			nodeName:       "test-node",
			nodeInstanceID: "i-12345678",
			expectedResult: "test-node-eri-managed",
			description:    "Normal node name should use node name + ERI managed suffix",
		},
		{
			name:           "TestGenerateEriNrsLabelSelectorValue_GivenLongNodeName_WhenGeneratingLabelSelectorValue_ThenReturnInstanceIDWithSuffix",
			nodeName:       "very-long-node-name-that-exceeds-the-kubernetes-label-length-limit-of-63-characters-and-needs-to-be-truncated-or-replaced-with-instance-id-to-avoid-validation-errors",
			nodeInstanceID: "i-12345678",
			expectedResult: "i-12345678-eri-managed",
			description:    "Long node name should use instance ID + ERI managed suffix",
		},
		{
			name:           "TestGenerateEriNrsLabelSelectorValue_GivenExactLengthLimit_WhenGeneratingLabelSelectorValue_ThenReturnNodeNameWithSuffix",
			nodeName:       "exactly-50-characters-long-node-name-at-the-limit",
			nodeInstanceID: "i-12345678",
			expectedResult: "exactly-50-characters-long-node-name-at-the-limit-eri-managed",
			description:    "Node name at exact limit (63 - 13 = 50) should use node name + suffix",
		},
		{
			name:           "TestGenerateEriNrsLabelSelectorValue_GivenEmptyNodeName_WhenGeneratingLabelSelectorValue_ThenReturnSuffixOnly",
			nodeName:       "",
			nodeInstanceID: "i-12345678",
			expectedResult: "-eri-managed",
			description:    "Empty node name should still work",
		},
		{
			name:           "TestGenerateEriNrsLabelSelectorValue_GivenEmptyInstanceID_WhenGeneratingLabelSelectorValue_ThenReturnInstanceIDWithSuffix",
			nodeName:       "very-long-node-name-that-exceeds-the-kubernetes-label-length-limit-of-63-characters-and-needs-to-be-truncated-or-replaced-with-instance-id-to-avoid-validation-errors",
			nodeInstanceID: "",
			expectedResult: "-eri-managed",
			description:    "Empty instance ID should still work when node name is too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			result := generateEriNrsLabelSelectorValue(tt.nodeName, tt.nodeInstanceID)

			// Verify the result
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

// TestGetNodeNameFromNetResourceSetName tests the GetNodeNameFromNetResourceSetName function
func TestGetNodeNameFromNetResourceSetName(t *testing.T) {
	tests := []struct {
		name                   string
		netResourceSetName     string
		ownerReferenceNodeName string
		nodeInstanceID         string
		expectedResult         string
		description            string
	}{
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenRdmaRoceNrsName_WhenGettingNodeName_ThenReturnNodeName",
			netResourceSetName:     "test-node-fa2700078302-rdmaroce",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "test-node",
			description:            "When NRS name contains rdmaroce, should extract node name",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenElasticRdmaNrsName_WhenGettingNodeName_ThenReturnNodeName",
			netResourceSetName:     "test-node-fa2700078302-elasticrdma",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "test-node",
			description:            "When NRS name contains elasticrdma, should extract node name",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenEriManagedNrsName_WhenGettingNodeName_ThenReturnNodeName",
			netResourceSetName:     "test-node-eri-managed",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "test-node",
			description:            "When NRS name has ERI managed suffix, should extract node name",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenInstanceIDInNrsName_WhenGettingNodeName_ThenReturnOwnerReferenceNodeName",
			netResourceSetName:     "i-abc123-fa2700078302-rdmaroce",
			ownerReferenceNodeName: "very-long-node-name-that-was-replaced-with-instance-id",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "very-long-node-name-that-was-replaced-with-instance-id",
			description:            "When NRS name uses instance ID, should return owner reference node name",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenInstanceIDInEriManagedNrsName_WhenGettingNodeName_ThenReturnOwnerReferenceNodeName",
			netResourceSetName:     "i-abc123-eri-managed",
			ownerReferenceNodeName: "very-long-node-name-that-was-replaced-with-instance-id",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "very-long-node-name-that-was-replaced-with-instance-id",
			description:            "When ERI managed NRS name uses instance ID, should return owner reference node name",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenRegularNrsName_WhenGettingNodeName_ThenReturnNrsName",
			netResourceSetName:     "regular-node-name",
			ownerReferenceNodeName: "regular-node-name",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "regular-node-name",
			description:            "When NRS name is regular (not RDMA or ERI), should return NRS name as is",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenComplexNodeNameWithRdmaRoce_WhenGettingNodeName_ThenReturnNodeName",
			netResourceSetName:     "node-with-dashes-and-dots.example.com-fa2700078302-rdmaroce",
			ownerReferenceNodeName: "node-with-dashes-and-dots.example.com",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "node-with-dashes-and-dots.example.com",
			description:            "When node name has complex format with RDMA ROCE, should extract correctly",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenComplexNodeNameWithElasticRdma_WhenGettingNodeName_ThenReturnNodeName",
			netResourceSetName:     "node-with-dashes-and-dots.example.com-fa2700078302-elasticrdma",
			ownerReferenceNodeName: "node-with-dashes-and-dots.example.com",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "node-with-dashes-and-dots.example.com",
			description:            "When node name has complex format with Elastic RDMA, should extract correctly",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenEmptyNrsName_WhenGettingNodeName_ThenReturnEmpty",
			netResourceSetName:     "",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "",
			description:            "When NRS name is empty, should return empty string",
		},
		{
			name:                   "TestGetNodeNameFromNetResourceSetName_GivenNrsNameWithoutRdmaOrEriSuffix_WhenGettingNodeName_ThenReturnNrsName",
			netResourceSetName:     "custom-resource-name",
			ownerReferenceNodeName: "test-node",
			nodeInstanceID:         "i-abc123",
			expectedResult:         "custom-resource-name",
			description:            "When NRS name doesn't contain RDMA or ERI patterns, should return as is",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			result := GetNodeNameFromNetResourceSetName(tt.netResourceSetName, tt.ownerReferenceNodeName, tt.nodeInstanceID)

			// Verify the result
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}
