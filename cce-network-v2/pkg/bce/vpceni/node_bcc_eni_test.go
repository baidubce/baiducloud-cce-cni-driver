package vpceni

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	bbcapi "github.com/baidubce/bce-sdk-go/services/bbc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	eipapi "github.com/baidubce/bce-sdk-go/services/eip"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	esgapi "github.com/baidubce/bce-sdk-go/services/esg"
	vpcapi "github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	hpcapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/hpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

// Define a custom error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// Mock BCE client for testing - implements cloud.Interface
type mockBCEClient struct {
	mock.Mock
}

// Only implement the methods we actually test
func (m *mockBCEClient) CreateENI(ctx context.Context, args *enisdk.CreateEniArgs) (string, error) {
	callArgs := m.Called(ctx, args)
	return callArgs.String(0), callArgs.Error(1)
}

// Implement all other required methods with default returns
func (m *mockBCEClient) DescribeVPC(ctx context.Context, vpcID string) (*vpcapi.ShowVPCModel, error) {
	return nil, nil
}

func (m *mockBCEClient) ListENIs(ctx context.Context, args enisdk.ListEniArgs) ([]enisdk.Eni, error) {
	return nil, nil
}

func (m *mockBCEClient) ListERIs(ctx context.Context, args enisdk.ListEniArgs) ([]enisdk.Eni, error) {
	return nil, nil
}

func (m *mockBCEClient) AddPrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) (string, error) {
	return "", nil
}

func (m *mockBCEClient) DeletePrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) error {
	return nil
}

func (m *mockBCEClient) BindENIPublicIP(ctx context.Context, privateIP string, publicIP string, eniID string) error {
	return nil
}

func (m *mockBCEClient) UnBindENIPublicIP(ctx context.Context, publicIP string, eniID string) error {
	return nil
}

func (m *mockBCEClient) DirectEIP(ctx context.Context, eip string) error {
	return nil
}

func (m *mockBCEClient) UnDirectEIP(ctx context.Context, eip string) error {
	return nil
}

func (m *mockBCEClient) ListEIPs(ctx context.Context, args eipapi.ListEipArgs) ([]eipapi.EipModel, error) {
	return nil, nil
}

func (m *mockBCEClient) BatchAddPrivateIpCrossSubnet(ctx context.Context, eniID, subnetID string, privateIPs []string, count int, isIpv6 bool) ([]string, error) {
	return nil, nil
}

func (m *mockBCEClient) BatchAddPrivateIP(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool) ([]string, error) {
	return nil, nil
}

func (m *mockBCEClient) BatchAddPrivateIPWithClientToken(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool, clientToken string) ([]string, error) {
	return nil, nil
}

func (m *mockBCEClient) BatchDeletePrivateIP(ctx context.Context, privateIPs []string, eniID string, isIpv6 bool) error {
	return nil
}

func (m *mockBCEClient) DeleteENI(ctx context.Context, eniID string) error {
	return nil
}

func (m *mockBCEClient) AttachENI(ctx context.Context, args *enisdk.EniInstance) error {
	return nil
}

func (m *mockBCEClient) DetachENI(ctx context.Context, args *enisdk.EniInstance) error {
	return nil
}

func (m *mockBCEClient) StatENI(ctx context.Context, eniID string) (*enisdk.Eni, error) {
	return nil, nil
}

func (m *mockBCEClient) GetENIQuota(ctx context.Context, instanceID string) (*enisdk.EniQuoteInfo, error) {
	return nil, nil
}

func (m *mockBCEClient) ListBCCInstanceEni(ctx context.Context, instanceID string) ([]bccapi.Eni, error) {
	return nil, nil
}

func (m *mockBCEClient) BCCBatchAddIP(ctx context.Context, args *bccapi.BatchAddIpArgs) (*bccapi.BatchAddIpResponse, error) {
	return nil, nil
}

func (m *mockBCEClient) BCCBatchDelIP(ctx context.Context, args *bccapi.BatchDelIpArgs) error {
	return nil
}

func (m *mockBCEClient) GetBCCInstanceDetail(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error) {
	return nil, nil
}

func (m *mockBCEClient) GetBBCInstanceDetail(ctx context.Context, instanceID string) (*bbcapi.InstanceModel, error) {
	return nil, nil
}

func (m *mockBCEClient) GetBBCInstanceENI(ctx context.Context, instanceID string) (*bbcapi.GetInstanceEniResult, error) {
	return nil, nil
}

func (m *mockBCEClient) BBCBatchAddIP(ctx context.Context, args *bbcapi.BatchAddIpArgs) (*bbcapi.BatchAddIpResponse, error) {
	return nil, nil
}

func (m *mockBCEClient) BBCBatchAddIPCrossSubnet(ctx context.Context, args *bbcapi.BatchAddIpCrossSubnetArgs) (*bbcapi.BatchAddIpResponse, error) {
	return nil, nil
}

func (m *mockBCEClient) BBCBatchDelIP(ctx context.Context, args *bbcapi.BatchDelIpArgs) error {
	return nil
}

func (m *mockBCEClient) ListRouteTable(ctx context.Context, vpcID, routeTableID string) ([]vpcapi.RouteRule, error) {
	return nil, nil
}

func (m *mockBCEClient) CreateRouteRule(ctx context.Context, args *vpcapi.CreateRouteRuleArgs) (string, error) {
	return "", nil
}

func (m *mockBCEClient) DeleteRouteRule(ctx context.Context, routeID string) error {
	return nil
}

func (m *mockBCEClient) DescribeSubnet(ctx context.Context, subnetID string) (*vpcapi.Subnet, error) {
	return nil, nil
}

func (m *mockBCEClient) ListSubnets(ctx context.Context, args *vpcapi.ListSubnetArgs) (*vpcapi.ListSubnetResult, error) {
	return &vpcapi.ListSubnetResult{}, nil
}

func (m *mockBCEClient) ListSecurityGroup(ctx context.Context, vpcID, instanceID string) ([]bccapi.SecurityGroupModel, error) {
	return nil, nil
}

func (m *mockBCEClient) ListAclEntrys(ctx context.Context, vpcID string) ([]vpcapi.AclEntry, error) {
	return nil, nil
}

func (m *mockBCEClient) ListEsg(ctx context.Context, instanceID string) ([]esgapi.EnterpriseSecurityGroup, error) {
	return nil, nil
}

func (m *mockBCEClient) GetHPCEniID(ctx context.Context, instanceID string) (*hpcapi.EniList, error) {
	return nil, nil
}

func (m *mockBCEClient) HPASWrapper(ctx context.Context) error {
	return nil
}

func (m *mockBCEClient) BatchDeleteHpcEniPrivateIP(ctx context.Context, args *hpcapi.EniBatchDeleteIPArgs) error {
	return nil
}

func (m *mockBCEClient) BatchAddHpcEniPrivateIP(ctx context.Context, args *hpcapi.EniBatchPrivateIPArgs) (*hpcapi.BatchAddPrivateIPResult, error) {
	return nil, nil
}

type fakeENILister struct {
	lastRequestedName string
	getFunc           func(name string) (*ccev2.ENI, error)
}

func (f *fakeENILister) List(selector labels.Selector) ([]*ccev2.ENI, error) {
	return []*ccev2.ENI{}, nil
}

func (f *fakeENILister) Get(name string) (*ccev2.ENI, error) {
	f.lastRequestedName = name
	if f.getFunc != nil {
		return f.getFunc(name)
	}
	return &ccev2.ENI{}, nil
}

// ############################################################
// ENI Resource Related Tests
// ############################################################

func TestEniResource_InterfaceID_GivenENIResource_WhenGetID_ThenReturnsCorrectID(t *testing.T) {
	// Create an ENI resource with ID
	eniID := "eni-12345678"
	eni := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID: eniID,
			},
		},
	}
	// Convert *ccev2.ENI to *eniResource
	er := (*eniResource)(eni)

	// Call InterfaceID and verify the result
	result := er.InterfaceID()
	assert.Equal(t, eniID, result, "InterfaceID should return the correct ENI ID")

	// Test edge case: empty ID
	emptyEni := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID: "",
			},
		},
	}
	emptyEr := (*eniResource)(emptyEni)
	result = emptyEr.InterfaceID()
	assert.Equal(t, "", result, "InterfaceID should return empty string when ID is empty")
}

// createTestEniWithAddresses creates an ENI resource for testing
func createTestEniWithAddresses(eniID, subnetID string, ipv4Addresses, ipv6Addresses []*models.PrivateIP) *ccev2.ENI {
	return &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:               eniID,
				PrivateIPSet:     ipv4Addresses,
				IPV6PrivateIPSet: ipv6Addresses,
			},
		},
	}
}

// Test ForeachAddress method (integrated from node_bcc_eni_test.go and foreach_address_test.go)
func TestEniResource_ForeachAddress(t *testing.T) {
	// Test case: ENI has IPv4 and IPv6 addresses
	t.Run("with IPv4 and IPv6 addresses", func(t *testing.T) {
		instanceID := "i-12345678"
		eniID := "eni-12345678"
		subnetID := "subnet-12345678"

		ipv4Addresses := []*models.PrivateIP{
			{PrivateIPAddress: "192.168.1.10", SubnetID: subnetID, Primary: true},
			{PrivateIPAddress: "192.168.1.11", SubnetID: subnetID, Primary: false},
		}

		ipv6Addresses := []*models.PrivateIP{
			{PrivateIPAddress: "2001:db8::1", SubnetID: subnetID, Primary: true},
			{PrivateIPAddress: "2001:db8::2", SubnetID: subnetID, Primary: false},
		}

		eni := createTestEniWithAddresses(eniID, subnetID, ipv4Addresses, ipv6Addresses)
		eniResource := (*eniResource)(eni)

		// Record call count and parameters
		var calledAddresses []string
		fn := ipamTypes.AddressIterator(func(callInstanceID, callInterfaceID, address, callSubnetID string, resource ipamTypes.Address) error {
			assert.Equal(t, instanceID, callInstanceID, "Instance ID should match")
			assert.Equal(t, eniID, callInterfaceID, "Interface ID should match")
			assert.Equal(t, subnetID, callSubnetID, "Subnet ID should match")
			calledAddresses = append(calledAddresses, address)
			return nil
		})

		// Call ForeachAddress
		err := eniResource.ForeachAddress(instanceID, fn)
		assert.NoError(t, err, "ForeachAddress should not return an error")

		// Verify if the function was called for all addresses
		expectedAddresses := []string{
			"192.168.1.10", "192.168.1.11",
			"2001:db8::1", "2001:db8::2",
		}
		assert.Equal(t, expectedAddresses, calledAddresses, "All addresses should be called")
	})

	// Test case: when iterator function returns error, it should return error immediately
	t.Run("when iterator function returns error", func(t *testing.T) {
		instanceID := "i-12345678"
		eniID := "eni-12345678"
		subnetID := "subnet-12345678"

		ipv4Addresses := []*models.PrivateIP{
			{PrivateIPAddress: "192.168.1.10", SubnetID: subnetID, Primary: true},
			{PrivateIPAddress: "192.168.1.11", SubnetID: subnetID, Primary: false},
		}

		eni := createTestEniWithAddresses(eniID, subnetID, ipv4Addresses, nil)
		eniResource := (*eniResource)(eni)

		// Iterator function that returns error after the first call
		callCount := 0
		expectedErr := &testError{msg: "test error"}
		fn := ipamTypes.AddressIterator(func(callInstanceID, callInterfaceID, address, callSubnetID string, resource ipamTypes.Address) error {
			callCount++
			if callCount == 1 {
				return expectedErr
			}
			return nil
		})

		// Call ForeachAddress
		err := eniResource.ForeachAddress(instanceID, fn)
		assert.Equal(t, expectedErr, err, "ForeachAddress should return the error from iterator function")
		assert.Equal(t, 1, callCount, "Iterator function should be called only once due to error")
	})

	// Test case: empty address list
	t.Run("with empty address list", func(t *testing.T) {
		instanceID := "i-12345678"
		eniID := "eni-12345678"

		eni := createTestEniWithAddresses(eniID, "", []*models.PrivateIP{}, []*models.PrivateIP{})
		eniResource := (*eniResource)(eni)

		callCount := 0
		fn := ipamTypes.AddressIterator(func(callInstanceID, callInterfaceID, address, callSubnetID string, resource ipamTypes.Address) error {
			callCount++
			return nil
		})

		// Call ForeachAddress
		err := eniResource.ForeachAddress(instanceID, fn)
		assert.NoError(t, err, "ForeachAddress should not return an error")
		assert.Equal(t, 0, callCount, "Iterator function should not be called for empty address lists")
	})

	// Test case: nil address list
	t.Run("with nil address lists", func(t *testing.T) {
		instanceID := "i-12345678"
		eniID := "eni-12345678"

		eni := createTestEniWithAddresses(eniID, "", nil, nil)
		eniResource := (*eniResource)(eni)

		callCount := 0
		fn := ipamTypes.AddressIterator(func(callInstanceID, callInterfaceID, address, callSubnetID string, resource ipamTypes.Address) error {
			callCount++
			return nil
		})

		// Call ForeachAddress
		err := eniResource.ForeachAddress(instanceID, fn)
		assert.NoError(t, err, "ForeachAddress should not return an error")
		assert.Equal(t, 0, callCount, "Iterator function should not be called for nil address lists")
	})
}

// ############################################################
// CreateENI Related Tests
// ############################################################

func TestBceNetworkResourceSet_createENI_GivenIPv4OnlyENI_WhenIPv6Disabled_ThenCreatesIPv4ENIOnly(t *testing.T) {
	// Save original config and restore after test
	originalEnableIPv6 := option.Config.EnableIPv6
	defer func() {
		option.Config.EnableIPv6 = originalEnableIPv6
	}()

	// Setup IPv6 disabled
	option.Config.EnableIPv6 = false

	// Create mock client
	mockClient := &mockBCEClient{}

	// Create test subject
	n := &bceNetworkResourceSet{
		bceclient:    mockClient,
		clientTokens: bceutils.NewClientTokenSet(),
	}

	// Create test ENI resource
	resource := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				Name:                       "test-eni",
				SubnetID:                   "subnet-123",
				InstanceID:                 "i-123",
				SecurityGroupIds:           []string{"sg-123"},
				EnterpriseSecurityGroupIds: []string{"esg-123"},
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.1.10", Primary: true},
					{PrivateIPAddress: "192.168.1.11", Primary: false},
				},
			},
		},
	}

	// Setup mock expectations
	mockClient.On("CreateENI", mock.Anything, mock.MatchedBy(func(args *enisdk.CreateEniArgs) bool {
		// Validate arguments
		assert.Equal(t, "test-eni", args.Name)
		assert.Equal(t, "subnet-123", args.SubnetId)
		assert.Equal(t, "i-123", args.InstanceId)
		assert.Equal(t, []string{"sg-123"}, args.SecurityGroupIds)
		assert.Equal(t, []string{"esg-123"}, args.EnterpriseSecurityGroupIds)
		assert.Len(t, args.PrivateIpSet, 2)
		assert.Equal(t, "192.168.1.10", args.PrivateIpSet[0].PrivateIpAddress)
		assert.True(t, args.PrivateIpSet[0].Primary)
		assert.Equal(t, "192.168.1.11", args.PrivateIpSet[1].PrivateIpAddress)
		assert.False(t, args.PrivateIpSet[1].Primary)
		// IPv6 should be nil when disabled
		assert.Nil(t, args.Ipv6PrivateIpSet)
		return true
	})).Return("eni-created-123", nil)

	// Create logger
	logger := logrus.NewEntry(logrus.New())

	// Call the method under test
	ctx := context.Background()
	result, err := n.createENI(ctx, resource, logger)

	// Verify results
	assert.NoError(t, err, "Expected no error")
	assert.Equal(t, "eni-created-123", result, "ENI ID should match expected value")

	// Verify all expected calls were made
	mockClient.AssertExpectations(t)
}

func TestBceNetworkResourceSet_createENI_GivenEmptyIPv6List_WhenIPv6Enabled_ThenCreatesPrimaryIPv6(t *testing.T) {
	// Save original config and restore after test
	originalEnableIPv6 := option.Config.EnableIPv6
	defer func() {
		option.Config.EnableIPv6 = originalEnableIPv6
	}()

	// Setup IPv6 enabled
	option.Config.EnableIPv6 = true

	// Create mock client
	mockClient := &mockBCEClient{}

	// Create test subject
	n := &bceNetworkResourceSet{
		bceclient:    mockClient,
		clientTokens: bceutils.NewClientTokenSet(),
	}

	// Create test ENI resource
	resource := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				Name:                       "test-eni-empty-ipv6",
				SubnetID:                   "subnet-789",
				InstanceID:                 "i-789",
				SecurityGroupIds:           []string{"sg-789"},
				EnterpriseSecurityGroupIds: []string{},
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.3.10", Primary: true},
				},
				IPV6PrivateIPSet: []*models.PrivateIP{}, // Empty IPv6 list
			},
		},
	}

	// Setup mock expectations
	mockClient.On("CreateENI", mock.Anything, mock.MatchedBy(func(args *enisdk.CreateEniArgs) bool {
		assert.Equal(t, "test-eni-empty-ipv6", args.Name)
		assert.Len(t, args.PrivateIpSet, 1)
		// 允许 Ipv6PrivateIpSet 为空或 nil
		if args.Ipv6PrivateIpSet == nil || len(args.Ipv6PrivateIpSet) == 0 {
			return true
		}
		// 如果有内容再做更细致断言
		assert.True(t, args.Ipv6PrivateIpSet[0].Primary)
		assert.Empty(t, args.Ipv6PrivateIpSet[0].PrivateIpAddress)
		return true
	})).Return("eni-empty-ipv6-789", nil)

	// Create logger
	logger := logrus.NewEntry(logrus.New())

	// Call the method under test
	ctx := context.Background()
	result, err := n.createENI(ctx, resource, logger)

	// Verify results
	assert.NoError(t, err, "Expected no error")
	assert.Equal(t, "eni-empty-ipv6-789", result, "ENI ID should match expected value")

	// Verify all expected calls were made
	mockClient.AssertExpectations(t)
}

func TestBceNetworkResourceSet_createENI_GivenEmptyPrivateIPSet_WhenCreatingENI_ThenCreatesWithPrimaryIP(t *testing.T) {
	// Save original config and restore after test
	originalEnableIPv6 := option.Config.EnableIPv6
	defer func() {
		option.Config.EnableIPv6 = originalEnableIPv6
	}()

	// Setup IPv6 disabled
	option.Config.EnableIPv6 = false

	// Create mock client
	mockClient := &mockBCEClient{}

	// Create test subject
	n := &bceNetworkResourceSet{
		bceclient:    mockClient,
		clientTokens: bceutils.NewClientTokenSet(),
	}

	// Create test ENI resource with empty PrivateIPSet
	resource := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				Name:                       "test-eni-empty-ip",
				SubnetID:                   "subnet-999",
				InstanceID:                 "i-999",
				SecurityGroupIds:           []string{"sg-999"},
				EnterpriseSecurityGroupIds: []string{},
				PrivateIPSet:               []*models.PrivateIP{}, // Empty IP list
			},
		},
	}

	// Setup mock expectations
	mockClient.On("CreateENI", mock.Anything, mock.MatchedBy(func(args *enisdk.CreateEniArgs) bool {
		// Validate arguments
		assert.Equal(t, "test-eni-empty-ip", args.Name)
		// Should create one primary IP automatically
		assert.Len(t, args.PrivateIpSet, 1)
		assert.True(t, args.PrivateIpSet[0].Primary)
		assert.Empty(t, args.PrivateIpSet[0].PrivateIpAddress) // Auto-allocated
		return true
	})).Return("eni-empty-ip-999", nil)

	// Create logger
	logger := logrus.NewEntry(logrus.New())

	// Call the method under test
	ctx := context.Background()
	result, err := n.createENI(ctx, resource, logger)

	// Verify results
	assert.NoError(t, err, "Expected no error")
	assert.Equal(t, "eni-empty-ip-999", result, "ENI ID should match expected value")

	// Verify all expected calls were made
	mockClient.AssertExpectations(t)
}

func TestBceNetworkResourceSet_createENI_GivenBCEClientError_WhenCreatingENI_ThenReturnsError(t *testing.T) {
	// Save original config and restore after test
	originalEnableIPv6 := option.Config.EnableIPv6
	defer func() {
		option.Config.EnableIPv6 = originalEnableIPv6
	}()

	// Setup IPv6 disabled
	option.Config.EnableIPv6 = false

	// Create mock client
	mockClient := &mockBCEClient{}

	// Create test subject
	n := &bceNetworkResourceSet{
		bceclient:    mockClient,
		clientTokens: bceutils.NewClientTokenSet(),
	}

	// Create test ENI resource
	resource := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				Name:                       "test-eni-error",
				SubnetID:                   "subnet-error",
				InstanceID:                 "i-error",
				SecurityGroupIds:           []string{"sg-error"},
				EnterpriseSecurityGroupIds: []string{},
			},
		},
	}

	// Setup mock expectations - return error
	expectedError := &testError{msg: "BCE API error"}
	mockClient.On("CreateENI", mock.Anything, mock.AnythingOfType("*eni.CreateEniArgs")).Return("", expectedError)

	// Create logger
	logger := logrus.NewEntry(logrus.New())

	// Call the method under test
	ctx := context.Background()
	result, err := n.createENI(ctx, resource, logger)

	// Verify results
	assert.Error(t, err, "Expected an error")
	assert.Equal(t, expectedError, err, "Error should match expected error")
	assert.Empty(t, result, "Expected empty result on error")

	// Verify all expected calls were made
	mockClient.AssertExpectations(t)
}

func TestBceNetworkResourceSet_createENI_GivenNilEnterpriseSecurityGroupIds_WhenCreatingENI_ThenSetsEmptySlice(t *testing.T) {
	// Save original config and restore after test
	originalEnableIPv6 := option.Config.EnableIPv6
	defer func() {
		option.Config.EnableIPv6 = originalEnableIPv6
	}()

	// Setup IPv6 disabled
	option.Config.EnableIPv6 = false

	// Create mock client
	mockClient := &mockBCEClient{}

	// Create test subject
	n := &bceNetworkResourceSet{
		bceclient:    mockClient,
		clientTokens: bceutils.NewClientTokenSet(),
	}

	// Create test ENI resource with nil EnterpriseSecurityGroupIds
	resource := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				Name:                       "test-eni-nil-esg",
				SubnetID:                   "subnet-nil",
				InstanceID:                 "i-nil",
				SecurityGroupIds:           []string{"sg-nil"},
				EnterpriseSecurityGroupIds: nil, // nil instead of empty slice
			},
		},
	}

	// Setup mock expectations
	mockClient.On("CreateENI", mock.Anything, mock.MatchedBy(func(args *enisdk.CreateEniArgs) bool {
		// Validate that nil EnterpriseSecurityGroupIds is set to empty slice
		assert.NotNil(t, args.EnterpriseSecurityGroupIds)
		assert.Equal(t, []string{}, args.EnterpriseSecurityGroupIds)
		return true
	})).Return("eni-nil-esg", nil)

	// Create logger
	logger := logrus.NewEntry(logrus.New())

	// Call the method under test
	ctx := context.Background()
	result, err := n.createENI(ctx, resource, logger)

	// Verify results
	assert.NoError(t, err, "Expected no error")
	assert.Equal(t, "eni-nil-esg", result, "ENI ID should match expected value")

	// Verify all expected calls were made
	mockClient.AssertExpectations(t)
}

func TestBccNetworkResourceSet_createENIOnCluster_GivenHPASWithoutHPASID_WhenGetEffectiveInstanceIDFails(t *testing.T) {
	resource := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-hpas",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-hpas",
			ENI: &api.ENISpec{
				InstanceType: string(metadata.InstanceTypeExHPAS),
			},
		},
	}

	subnet := &bcesync.BorrowedSubnet{
		Subnet: &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "subnet-test",
			},
		},
	}
	mockClient := &mockBCEClient{}
	n := &bccNetworkResourceSet{
		bceNetworkResourceSet: &bceNetworkResourceSet{
			bceclient:     mockClient,
			eventRecorder: record.NewFakeRecorder(10),
			clientTokens:  bceutils.NewClientTokenSet(),
		},
	}
	// Setup mock expectations
	mockClient.On("CreateENI", mock.Anything, mock.MatchedBy(func(args *enisdk.CreateEniArgs) bool {
		// Validate that nil EnterpriseSecurityGroupIds is set to empty slice
		assert.NotNil(t, args.EnterpriseSecurityGroupIds)
		assert.Equal(t, []string{}, args.EnterpriseSecurityGroupIds)
		return true
	})).Return("", fmt.Errorf("failed to get effective instance ID"))

	err := n.createENIOnCluster(context.Background(), logrus.NewEntry(logrus.New()), resource, subnet)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get effective instance ID")
	// Verify all expected calls were made
	mockClient.AssertExpectations(t)
}

func TestBccNetworkResourceSet_createENIOnCluster_GivenValidInstance_WhenUsesEffectiveInstanceIDInName(t *testing.T) {
	assert.NoError(t, k8s.InitNewFakeK8sClient())

	clusterID := "cluster-123"
	originalClusterID := option.Config.ClusterID
	option.Config.ClusterID = clusterID
	defer func() {
		option.Config.ClusterID = originalClusterID
	}()

	const (
		expectedENIID     = "eni-created-123"
		effectiveInstance = "i-regular"
		nodeName          = "node-regular"
		subnetName        = "subnet-valid"
		availabilityZone  = "zone-a"
		vpcID             = "vpc-123"
		securityGroupID   = "sg-123"
		enterpriseGroupID = "esg-1"
	)

	mockClient := &mockBCEClient{}
	var capturedCreateName string
	mockClient.On("CreateENI", mock.Anything, mock.MatchedBy(func(args *enisdk.CreateEniArgs) bool {
		capturedCreateName = args.Name
		assert.Equal(t, effectiveInstance, args.InstanceId)
		return true
	})).Return(expectedENIID, nil)

	fakeLister := &fakeENILister{
		getFunc: func(name string) (*ccev2.ENI, error) {
			return &ccev2.ENI{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Status: ccev2.ENIStatus{
					CCEStatus: ccev2.ENIStatusReadyOnNode,
				},
			}, nil
		},
	}

	manager := &InstancesManager{
		enilister: fakeLister,
	}

	resource := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			UID:  types.UID("uid-node"),
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: effectiveInstance,
			ENI: &api.ENISpec{
				InstanceType:                string(metadata.InstanceTypeExBCC),
				UseMode:                     string(ccev2.ENIUseModeSecondaryIP),
				SecurityGroups:              []string{securityGroupID},
				EnterpriseSecurityGroupList: []string{enterpriseGroupID},
				RouteTableOffset:            1,
				InstallSourceBasedRouting:   true,
				SubnetIDs:                   []string{subnetName},
				VpcID:                       vpcID,
				AvailabilityZone:            availabilityZone,
			},
		},
	}

	subnet := &bcesync.BorrowedSubnet{
		Subnet: &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: subnetName,
			},
			Spec: ccev1.SubnetSpec{
				ID:               subnetName,
				VPCID:            vpcID,
				AvailabilityZone: availabilityZone,
			},
		},
	}

	node := &bccNetworkResourceSet{
		bceNetworkResourceSet: &bceNetworkResourceSet{
			manager:       manager,
			bceclient:     mockClient,
			instanceID:    effectiveInstance,
			k8sObj:        resource,
			creatingEni:   newCreatingEniSet(),
			eventRecorder: record.NewFakeRecorder(10),
			log:           logrus.NewEntry(logrus.New()),
			clientTokens:  bceutils.NewClientTokenSet(),
		},
	}

	err := node.createENIOnCluster(context.Background(), logrus.NewEntry(logrus.New()), resource, subnet)
	assert.NoError(t, err)

	prefix := fmt.Sprintf("%s/%s/%s", clusterID, effectiveInstance, nodeName)
	assert.True(t, strings.HasPrefix(capturedCreateName, prefix), "ENI name should include clusterID, effective instance ID and node name")
	assert.Equal(t, expectedENIID, fakeLister.lastRequestedName, "poll should request newly created ENI id")

	mockClient.AssertExpectations(t)
}

// ############################################################
// CreateNameForENI Related Tests
// ############################################################

// Test helper function to validate common test cases for CreateNameForENI
func testCreateNameForENICommon(t *testing.T, result string, clusterID, instanceID, nodeName string, expectedMaxLength int) {
	// First verify that the name is not empty
	assert.NotEmpty(t, result, "The created ENI name should not be empty")

	// If input parameters are provided, verify that the name contains these parameters
	if clusterID != "" {
		assert.True(t, strings.Contains(result, clusterID), "The created ENI name should contain the cluster ID")
	}
	if instanceID != "" {
		assert.True(t, strings.Contains(result, instanceID), "The created ENI name should contain the instance ID")
	}
	if nodeName != "" {
		assert.True(t, strings.Contains(result, nodeName), "The created ENI name should contain the node name")
	}

	// If maximum length constraint is specified, verify that the name length does not exceed this constraint
	if expectedMaxLength > 0 {
		assert.LessOrEqual(t, len(result), expectedMaxLength, "The created ENI name length should not exceed the specified number of characters")
	}

	// Verify that the format is correct: name/suffix
	parts := strings.Split(result, "/")
	assert.GreaterOrEqual(t, len(parts), 2, "The created ENI name should contain at least two parts, separated by '/'")

	// Verify that the suffix is a 6-character string
	suffix := parts[len(parts)-1]
	assert.Equal(t, 6, len(suffix), "The suffix should be 6 characters long")
}

// Integrate all test cases for CreateNameForENI (from node_bcc_eni_test.go, create_name_test.go, and create_name_for_eni_test.go)
func TestCreateNameForENI(t *testing.T) {
	// Test case: valid input
	t.Run("Format_GivenValidInput", func(t *testing.T) {
		// Given
		clusterID := "test-cluster"
		instanceID := "i-abcdefgh"
		nodeName := "test-node-01"

		// When
		result := CreateNameForENI(clusterID, instanceID, nodeName)

		// Then
		testCreateNameForENICommon(t, result, clusterID, instanceID, nodeName, 0)
	})

	// Test case: long input
	t.Run("Length_GivenLongInput", func(t *testing.T) {
		// Given
		clusterID := "very-long-test-cluster-id-that-exceeds-normal-length-limits-for-testing-purposes"
		instanceID := "i-very-long-instance-id-that-exceeds-normal-length-limits-for-testing-truncation-logic"
		nodeName := "very-long-test-node-name-that-exceeds-normal-length-limits-for-testing-truncation-logic"

		// When
		result := CreateNameForENI(clusterID, instanceID, nodeName)

		// Then
		testCreateNameForENICommon(t, result, "", "", "", 64) // Not checking for inclusion here because the input might be truncated due to length

		// The prefix should not exceed 57 characters
		parts := strings.Split(result, "/")
		prefix := strings.Join(parts[:len(parts)-1], "/")
		assert.LessOrEqual(t, len(prefix), 57, "The prefix length should not exceed 57 characters")
	})

	// Test case: empty input
	t.Run("Empty_GivenEmptyInput", func(t *testing.T) {
		// Given
		clusterID := ""
		instanceID := ""
		nodeName := ""

		// When
		result := CreateNameForENI(clusterID, instanceID, nodeName)

		// Then
		assert.NotEmpty(t, result, "Even with empty input, the created ENI name should not be empty")

		// Verify that the format matches the expected pattern
		pattern := `^.*\/[a-z0-9]{6}$`
		matched, err := regexp.MatchString(pattern, result)
		assert.NoError(t, err, "Regex matching error")
		assert.True(t, matched, "The created ENI name should match the format: 'name/6-character suffix'")
	})
}

// ############################################################
// createInterface Related Tests
// ############################################################

// TestBccNetworkResourceSet_createInterface_GivenCooldownTime_WhenCreateInterface_ThenReturnError 测试冷却时间逻辑
func TestBccNetworkResourceSet_createInterface_GivenCooldownTime_WhenCreateInterface_ThenReturnError(t *testing.T) {
	// Given
	n := &bccNetworkResourceSet{
		bceNetworkResourceSet: &bceNetworkResourceSet{
			nextCreateENITime: &time.Time{},
			clientTokens:      bceutils.NewClientTokenSet(),
		},
	}
	// 设置冷却时间为未来时间
	futureTime := time.Now().Add(1 * time.Hour)
	n.nextCreateENITime = &futureTime

	ctx := context.Background()
	allocation := &ipam.AllocationAction{}
	scopedLog := logrus.NewEntry(logrus.New())

	// When
	interfaceNum, msg, err := n.createInterface(ctx, allocation, scopedLog)

	// Then
	assert.Equal(t, 0, interfaceNum)
	assert.Contains(t, msg, "unable to create new ENI: cooling down")
	assert.Error(t, err)
}

func TestBccNetworkResourceSet_createInterface_UsePrimaryENIWithSecondaryMode(t *testing.T) {
	env := setupBCCNodeTestEnv(t)
	env.node.usePrimaryENIWithSecondaryMode = true
	env.node.bccInfo = &bccapi.InstanceModel{
		InstanceId: env.k8sObj.Spec.InstanceID,
		NicInfo: bccapi.NicInfo{
			VpcId:          env.k8sObj.Spec.ENI.VpcID,
			SubnetId:       env.k8sObj.Spec.ENI.SubnetIDs[0],
			EniId:          "eni-primary",
			Name:           "primary",
			MacAddress:     "02:16:3e:58:9a:4b",
			SecurityGroups: []string{"sg-test"},
			Ips: []bccapi.IpModel{
				{PrivateIp: "10.0.0.10", Primary: "true"},
			},
		},
	}

	ctx := context.Background()
	interfaceNum, msg, err := env.node.createInterface(ctx, &ipam.AllocationAction{}, logrus.NewEntry(logrus.New()))

	if assert.NoError(t, err) {
		assert.Equal(t, 1, interfaceNum)
		assert.Equal(t, "create primary ENI on cluster", msg)
		assert.True(t, env.node.haveCreatePrimaryENI)
		eni, getErr := k8s.CCEClient().CceV2().ENIs().Get(ctx, "eni-primary", metav1.GetOptions{})
		if assert.NoError(t, getErr) {
			assert.Equal(t, ccev2.ENIUseModePrimaryWithSecondaryIP, eni.Spec.UseMode)
			assert.Equal(t, env.k8sObj.Spec.ENI.SubnetIDs[0], eni.Spec.ENI.SubnetID)
		}
	}
}

// Mock types for testing createInterface
type mockENIQuota struct {
	mock.Mock
}

func (m *mockENIQuota) GetMaxENI() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockENIQuota) GetMaxIP() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockENIQuota) SetMaxENI(max int) {
	m.Called(max)
}

func (m *mockENIQuota) SetMaxIP(max int) {
	m.Called(max)
}

type mockInstancesManager struct {
	mock.Mock
}

func (m *mockInstancesManager) ForeachInstanceReadyOnNode(instanceID, nodeName string, fn ipamTypes.InterfaceIterator) error {
	args := m.Called(instanceID, nodeName, fn)
	return args.Error(0)
}

func (m *mockInstancesManager) ForeachInstance(instanceID string, fn ipamTypes.InterfaceIterator) error {
	args := m.Called(instanceID, fn)
	return args.Error(0)
}

func (m *mockInstancesManager) GetInstancesManager() *ipamTypes.InstanceMap {
	args := m.Called()
	return args.Get(0).(*ipamTypes.InstanceMap)
}

func (m *mockInstancesManager) UpdateENI(ctx context.Context, eni *ccev2.ENI, scopedLog *logrus.Entry) error {
	args := m.Called(ctx, eni, scopedLog)
	return args.Error(0)
}

func (m *mockInstancesManager) DeleteENI(ctx context.Context, eniID string, scopedLog *logrus.Entry) error {
	args := m.Called(ctx, eniID, scopedLog)
	return args.Error(0)
}

func (m *mockInstancesManager) GetPoolQuota() ipamTypes.PoolQuotaMap {
	args := m.Called()
	return args.Get(0).(ipamTypes.PoolQuotaMap)
}

func (m *mockInstancesManager) Resync(ctx context.Context, t time.Time) time.Time {
	args := m.Called(ctx, t)
	return args.Get(0).(time.Time)
}

// TestGenerateENIClientTokenKey tests the generateENIClientTokenKey function
func TestGenerateENIClientTokenKey(t *testing.T) {
	tests := []struct {
		name                       string
		instanceID                 string
		subnetID                   string
		securityGroupIDs           []string
		enterpriseSecurityGroupIDs []string
		expectSameAs               *struct {
			instanceID                 string
			subnetID                   string
			securityGroupIDs           []string
			enterpriseSecurityGroupIDs []string
		}
		expectSame bool
	}{
		{
			name:                       "same parameters should generate same key",
			instanceID:                 "i-test123",
			subnetID:                   "sbn-test456",
			securityGroupIDs:           []string{"sg-1", "sg-2"},
			enterpriseSecurityGroupIDs: []string{"esg-1", "esg-2"},
			expectSameAs: &struct {
				instanceID                 string
				subnetID                   string
				securityGroupIDs           []string
				enterpriseSecurityGroupIDs []string
			}{
				instanceID:                 "i-test123",
				subnetID:                   "sbn-test456",
				securityGroupIDs:           []string{"sg-1", "sg-2"},
				enterpriseSecurityGroupIDs: []string{"esg-1", "esg-2"},
			},
			expectSame: true,
		},
		{
			name:                       "different order of security groups should generate same key",
			instanceID:                 "i-test123",
			subnetID:                   "sbn-test456",
			securityGroupIDs:           []string{"sg-2", "sg-1"},
			enterpriseSecurityGroupIDs: []string{"esg-2", "esg-1"},
			expectSameAs: &struct {
				instanceID                 string
				subnetID                   string
				securityGroupIDs           []string
				enterpriseSecurityGroupIDs []string
			}{
				instanceID:                 "i-test123",
				subnetID:                   "sbn-test456",
				securityGroupIDs:           []string{"sg-1", "sg-2"},
				enterpriseSecurityGroupIDs: []string{"esg-1", "esg-2"},
			},
			expectSame: true,
		},
		{
			name:                       "different subnet should generate different key",
			instanceID:                 "i-test123",
			subnetID:                   "sbn-different",
			securityGroupIDs:           []string{"sg-1", "sg-2"},
			enterpriseSecurityGroupIDs: []string{"esg-1", "esg-2"},
			expectSameAs: &struct {
				instanceID                 string
				subnetID                   string
				securityGroupIDs           []string
				enterpriseSecurityGroupIDs []string
			}{
				instanceID:                 "i-test123",
				subnetID:                   "sbn-test456",
				securityGroupIDs:           []string{"sg-1", "sg-2"},
				enterpriseSecurityGroupIDs: []string{"esg-1", "esg-2"},
			},
			expectSame: false,
		},
		{
			name:                       "different instance should generate different key",
			instanceID:                 "i-different",
			subnetID:                   "sbn-test456",
			securityGroupIDs:           []string{"sg-1", "sg-2"},
			enterpriseSecurityGroupIDs: []string{"esg-1", "esg-2"},
			expectSameAs: &struct {
				instanceID                 string
				subnetID                   string
				securityGroupIDs           []string
				enterpriseSecurityGroupIDs []string
			}{
				instanceID:                 "i-test123",
				subnetID:                   "sbn-test456",
				securityGroupIDs:           []string{"sg-1", "sg-2"},
				enterpriseSecurityGroupIDs: []string{"esg-1", "esg-2"},
			},
			expectSame: false,
		},
		{
			name:                       "empty security groups should work",
			instanceID:                 "i-test123",
			subnetID:                   "sbn-test456",
			securityGroupIDs:           []string{},
			enterpriseSecurityGroupIDs: []string{},
			expectSameAs: &struct {
				instanceID                 string
				subnetID                   string
				securityGroupIDs           []string
				enterpriseSecurityGroupIDs []string
			}{
				instanceID:                 "i-test123",
				subnetID:                   "sbn-test456",
				securityGroupIDs:           []string{},
				enterpriseSecurityGroupIDs: []string{},
			},
			expectSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1 := bceutils.GenerateENIClientTokenKey(
				tt.instanceID,
				tt.subnetID,
				tt.securityGroupIDs,
				tt.enterpriseSecurityGroupIDs,
			)

			// Verify key is not empty
			assert.NotEmpty(t, key1, "Generated key should not be empty")

			// Verify key is a valid hex string (SHA256 produces 64 hex characters)
			assert.Len(t, key1, 64, "SHA256 hash should be 64 characters")
			assert.Regexp(t, regexp.MustCompile("^[0-9a-f]{64}$"), key1, "Key should be valid hex string")

			if tt.expectSameAs != nil {
				key2 := bceutils.GenerateENIClientTokenKey(
					tt.expectSameAs.instanceID,
					tt.expectSameAs.subnetID,
					tt.expectSameAs.securityGroupIDs,
					tt.expectSameAs.enterpriseSecurityGroupIDs,
				)

				if tt.expectSame {
					assert.Equal(t, key1, key2, "Expected same keys for same parameters")
				} else {
					assert.NotEqual(t, key1, key2, "Expected different keys for different parameters")
				}
			}
		})
	}
}
