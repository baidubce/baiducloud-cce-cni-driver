/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package cloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/hpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/eip"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/esg"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/stretchr/testify/assert"
	grate "golang.org/x/time/rate"
)

// mockClient implements Interface for testing
type mockClient struct {
	listSubnetsFunc                      func(ctx context.Context, args *vpc.ListSubnetArgs) (*vpc.ListSubnetResult, error)
	batchAddPrivateIPWithClientTokenFunc func(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool, clientToken string) ([]string, error)
}

func (m *mockClient) ListSubnets(ctx context.Context, args *vpc.ListSubnetArgs) (*vpc.ListSubnetResult, error) {
	if m.listSubnetsFunc != nil {
		return m.listSubnetsFunc(ctx, args)
	}
	return &vpc.ListSubnetResult{}, nil
}

// Implement other Interface methods as no-ops for testing
func (m *mockClient) DescribeVPC(ctx context.Context, vpcID string) (*vpc.ShowVPCModel, error) {
	return nil, nil
}
func (m *mockClient) AddPrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) (string, error) {
	return "", nil
}
func (m *mockClient) DeletePrivateIP(ctx context.Context, privateIP string, eniID string, isIpv6 bool) error {
	return nil
}
func (m *mockClient) BindENIPublicIP(ctx context.Context, privateIP string, publicIP string, eniID string) error {
	return nil
}
func (m *mockClient) UnBindENIPublicIP(ctx context.Context, publicIP string, eniID string) error {
	return nil
}
func (m *mockClient) DirectEIP(ctx context.Context, eip string) error { return nil }
func (m *mockClient) UnDirectEIP(ctx context.Context, eip string) error {
	return nil
}
func (m *mockClient) ListEIPs(ctx context.Context, args eip.ListEipArgs) ([]eip.EipModel, error) {
	return nil, nil
}
func (m *mockClient) BatchAddPrivateIpCrossSubnet(ctx context.Context, eniID, subnetID string, privateIPs []string, count int, isIpv6 bool) ([]string, error) {
	return nil, nil
}
func (m *mockClient) BatchAddPrivateIP(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool) ([]string, error) {
	return nil, nil
}
func (m *mockClient) BatchAddPrivateIPWithClientToken(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool, clientToken string) ([]string, error) {
	if m.batchAddPrivateIPWithClientTokenFunc != nil {
		return m.batchAddPrivateIPWithClientTokenFunc(ctx, privateIPs, count, eniID, isIpv6, clientToken)
	}
	return nil, nil
}
func (m *mockClient) BatchDeletePrivateIP(ctx context.Context, privateIPs []string, eniID string, isIpv6 bool) error {
	return nil
}
func (m *mockClient) CreateENI(ctx context.Context, args *eni.CreateEniArgs) (string, error) {
	return "", nil
}
func (m *mockClient) DeleteENI(ctx context.Context, eniID string) error { return nil }
func (m *mockClient) AttachENI(ctx context.Context, args *eni.EniInstance) error {
	return nil
}
func (m *mockClient) DetachENI(ctx context.Context, args *eni.EniInstance) error {
	return nil
}
func (m *mockClient) StatENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	return nil, nil
}
func (m *mockClient) GetENIQuota(ctx context.Context, instanceID string) (*eni.EniQuoteInfo, error) {
	return nil, nil
}
func (m *mockClient) ListENIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	return nil, nil
}
func (m *mockClient) ListERIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error) {
	return nil, nil
}
func (m *mockClient) ListRouteTable(ctx context.Context, vpcID, routeTableID string) ([]vpc.RouteRule, error) {
	return nil, nil
}
func (m *mockClient) CreateRouteRule(ctx context.Context, args *vpc.CreateRouteRuleArgs) (string, error) {
	return "", nil
}
func (m *mockClient) DeleteRouteRule(ctx context.Context, routeID string) error {
	return nil
}
func (m *mockClient) DescribeSubnet(ctx context.Context, subnetID string) (*vpc.Subnet, error) {
	return nil, nil
}
func (m *mockClient) ListSecurityGroup(ctx context.Context, vpcID, instanceID string) ([]bccapi.SecurityGroupModel, error) {
	return nil, nil
}
func (m *mockClient) ListAclEntrys(ctx context.Context, vpcID string) ([]vpc.AclEntry, error) {
	return nil, nil
}
func (m *mockClient) ListEsg(ctx context.Context, instanceID string) ([]esg.EnterpriseSecurityGroup, error) {
	return nil, nil
}
func (m *mockClient) GetBCCInstanceDetail(ctx context.Context, instanceID string) (*bccapi.InstanceModel, error) {
	return nil, nil
}
func (m *mockClient) GetBBCInstanceDetail(ctx context.Context, instanceID string) (*bbc.InstanceModel, error) {
	return nil, nil
}
func (m *mockClient) GetBBCInstanceENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error) {
	return nil, nil
}
func (m *mockClient) BBCBatchAddIP(ctx context.Context, args *bbc.BatchAddIpArgs) (*bbc.BatchAddIpResponse, error) {
	return nil, nil
}
func (m *mockClient) BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error {
	return nil
}
func (m *mockClient) BBCBatchAddIPCrossSubnet(ctx context.Context, args *bbc.BatchAddIpCrossSubnetArgs) (*bbc.BatchAddIpResponse, error) {
	return nil, nil
}
func (m *mockClient) GetHPCEniID(ctx context.Context, instanceID string) (*hpc.EniList, error) {
	return nil, nil
}
func (m *mockClient) BatchDeleteHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchDeleteIPArgs) error {
	return nil
}
func (m *mockClient) BatchAddHpcEniPrivateIP(ctx context.Context, args *hpc.EniBatchPrivateIPArgs) (*hpc.BatchAddPrivateIPResult, error) {
	return nil, nil
}
func (m *mockClient) ListBCCInstanceEni(ctx context.Context, instanceID string) ([]bccapi.Eni, error) {
	return nil, nil
}
func (m *mockClient) BCCBatchAddIP(ctx context.Context, args *bccapi.BatchAddIpArgs) (*bccapi.BatchAddIpResponse, error) {
	return nil, nil
}
func (m *mockClient) BCCBatchDelIP(ctx context.Context, args *bccapi.BatchDelIpArgs) error {
	return nil
}

func (m *mockClient) HPASWrapper(ctx context.Context) error {
	return nil
}

// TestFlowControlClient_ListSubnets tests the flowControlClient.ListSubnets method
func TestFlowControlClient_ListSubnets(t *testing.T) {
	ass := assert.New(t)

	tests := []struct {
		name              string
		enableRateLimit   bool
		args              *vpc.ListSubnetArgs
		mockResult        *vpc.ListSubnetResult
		mockError         error
		wantErr           bool
		validateRateLimit bool
	}{
		{
			name:            "list_subnets_without_rate_limit",
			enableRateLimit: false,
			args: &vpc.ListSubnetArgs{
				VpcId:   "vpc-test123",
				MaxKeys: 100,
			},
			mockResult: &vpc.ListSubnetResult{
				Subnets: []vpc.Subnet{
					{
						SubnetId: "sbn-test1",
						Name:     "test-subnet-1",
						Cidr:     "192.168.1.0/24",
					},
				},
				IsTruncated: false,
				MaxKeys:     100,
			},
			mockError:         nil,
			wantErr:           false,
			validateRateLimit: false,
		},
		{
			name:            "list_subnets_with_rate_limit",
			enableRateLimit: true,
			args: &vpc.ListSubnetArgs{
				VpcId:   "vpc-test456",
				MaxKeys: 50,
			},
			mockResult: &vpc.ListSubnetResult{
				Subnets: []vpc.Subnet{
					{
						SubnetId: "sbn-test2",
						Name:     "test-subnet-2",
						Cidr:     "192.168.2.0/24",
					},
					{
						SubnetId: "sbn-test3",
						Name:     "test-subnet-3",
						Cidr:     "192.168.3.0/24",
					},
				},
				IsTruncated: false,
				MaxKeys:     50,
			},
			mockError:         nil,
			wantErr:           false,
			validateRateLimit: true,
		},
		{
			name:            "list_subnets_with_subnet_ids",
			enableRateLimit: false,
			args: &vpc.ListSubnetArgs{
				SubnetIds: "sbn-test1,sbn-test2,sbn-test3",
				MaxKeys:   1000,
			},
			mockResult: &vpc.ListSubnetResult{
				Subnets: []vpc.Subnet{
					{SubnetId: "sbn-test1"},
					{SubnetId: "sbn-test2"},
					{SubnetId: "sbn-test3"},
				},
				IsTruncated: false,
				MaxKeys:     1000,
			},
			mockError:         nil,
			wantErr:           false,
			validateRateLimit: false,
		},
		{
			name:            "list_subnets_empty_result",
			enableRateLimit: false,
			args: &vpc.ListSubnetArgs{
				VpcId:   "vpc-empty",
				MaxKeys: 100,
			},
			mockResult: &vpc.ListSubnetResult{
				Subnets:     []vpc.Subnet{},
				IsTruncated: false,
				MaxKeys:     100,
			},
			mockError:         nil,
			wantErr:           false,
			validateRateLimit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original config and restore after test
			originalEnableRateLimit := option.Config.EnableAPIRateLimit
			defer func() {
				option.Config.EnableAPIRateLimit = originalEnableRateLimit
			}()

			// Set rate limit config for this test
			option.Config.EnableAPIRateLimit = tt.enableRateLimit

			// Create mock client
			mock := &mockClient{
				listSubnetsFunc: func(ctx context.Context, args *vpc.ListSubnetArgs) (*vpc.ListSubnetResult, error) {
					return tt.mockResult, tt.mockError
				},
			}

			// Create rate limiter
			limiterSet, err := rate.NewAPILimiterSet(nil, map[string]rate.APILimiterParameters{
				ListSubnets: {
					RateLimit: grate.Limit(10),
					RateBurst: 10,
				},
			}, nil)
			ass.Nil(err, "failed to create rate limiter set")

			limiter := rate.APILimiterSetWrapDefault(limiterSet, &rate.APILimiterParameters{
				RateLimit: grate.Limit(10),
				RateBurst: 10,
			})

			// Create flow control client
			fc := &flowControlClient{
				client:  mock,
				limiter: limiter,
			}

			// Execute test
			ctx := context.Background()
			got, err := fc.ListSubnets(ctx, tt.args)

			// Validate error
			if tt.wantErr {
				ass.NotNil(err, "expected error but got nil")
				return
			}

			ass.Nil(err, "unexpected error: %v", err)
			ass.NotNil(got, "result should not be nil")

			// Validate result
			ass.Equal(len(tt.mockResult.Subnets), len(got.Subnets), "subnet count mismatch")
			ass.Equal(tt.mockResult.IsTruncated, got.IsTruncated, "IsTruncated mismatch")
			ass.Equal(tt.mockResult.MaxKeys, got.MaxKeys, "MaxKeys mismatch")

			// Verify subnet details
			for i, wantSubnet := range tt.mockResult.Subnets {
				ass.Equal(wantSubnet.SubnetId, got.Subnets[i].SubnetId, "subnet ID mismatch at index %d", i)
				if wantSubnet.Name != "" {
					ass.Equal(wantSubnet.Name, got.Subnets[i].Name, "subnet name mismatch at index %d", i)
				}
				if wantSubnet.Cidr != "" {
					ass.Equal(wantSubnet.Cidr, got.Subnets[i].Cidr, "subnet CIDR mismatch at index %d", i)
				}
			}
		})
	}
}

// TestFlowControlClient_ListSubnets_RateLimitBehavior tests rate limiting behavior
func TestFlowControlClient_ListSubnets_RateLimitBehavior(t *testing.T) {
	ass := assert.New(t)

	// Save original config and restore after test
	originalEnableRateLimit := option.Config.EnableAPIRateLimit
	defer func() {
		option.Config.EnableAPIRateLimit = originalEnableRateLimit
	}()

	// Enable rate limiting
	option.Config.EnableAPIRateLimit = true

	// Create mock client that returns immediately
	mock := &mockClient{
		listSubnetsFunc: func(ctx context.Context, args *vpc.ListSubnetArgs) (*vpc.ListSubnetResult, error) {
			return &vpc.ListSubnetResult{
				Subnets:     []vpc.Subnet{{SubnetId: "sbn-test"}},
				IsTruncated: false,
				MaxKeys:     100,
			}, nil
		},
	}

	// Create rate limiter with very low rate (1 request per second)
	limiterSet, err := rate.NewAPILimiterSet(nil, map[string]rate.APILimiterParameters{
		ListSubnets: {
			RateLimit: grate.Limit(1),
			RateBurst: 1,
		},
	}, nil)
	ass.Nil(err, "failed to create rate limiter set")

	limiter := rate.APILimiterSetWrapDefault(limiterSet, &rate.APILimiterParameters{
		RateLimit: grate.Limit(1),
		RateBurst: 1,
	})

	// Create flow control client
	fc := &flowControlClient{
		client:  mock,
		limiter: limiter,
	}

	ctx := context.Background()
	args := &vpc.ListSubnetArgs{VpcId: "vpc-test", MaxKeys: 100}

	// First call should succeed immediately
	start := time.Now()
	result1, err1 := fc.ListSubnets(ctx, args)
	duration1 := time.Since(start)

	ass.Nil(err1, "first call should succeed")
	ass.NotNil(result1, "first result should not be nil")
	ass.Less(duration1, 100*time.Millisecond, "first call should be fast")

	// Second call should be rate limited (delayed)
	start = time.Now()
	result2, err2 := fc.ListSubnets(ctx, args)
	duration2 := time.Since(start)

	ass.Nil(err2, "second call should succeed")
	ass.NotNil(result2, "second result should not be nil")
	// Due to rate limit of 1 req/sec, second call should take ~1 second
	ass.GreaterOrEqual(duration2, 500*time.Millisecond, "second call should be delayed by rate limiter")
}

// TestHPASWrapper tests the HPASWrapper method
func TestHPASWrapper(t *testing.T) {
	ctx := context.Background()

	// Create mock client
	mock := &mockClient{}

	// Create flow control client
	fc := &flowControlClient{
		client: mock,
	}

	// Act
	err := fc.HPASWrapper(ctx)

	// Assert
	assert.Nil(t, err, "HPASWrapper should return nil")
}

// TestFlowControlClient_BatchAddPrivateIPWithClientToken tests the BatchAddPrivateIPWithClientToken method
func TestFlowControlClient_BatchAddPrivateIPWithClientToken(t *testing.T) {
	ass := assert.New(t)

	tests := []struct {
		name            string
		enableRateLimit bool
		privateIPs      []string
		count           int
		eniID           string
		isIpv6          bool
		clientToken     string
		mockResult      []string
		mockError       error
		wantErr         bool
	}{
		{
			name:            "add_ipv4_without_rate_limit",
			enableRateLimit: false,
			privateIPs:      []string{},
			count:           2,
			eniID:           "eni-test",
			isIpv6:          false,
			clientToken:     "token-123",
			mockResult:      []string{"10.0.0.1", "10.0.0.2"},
			mockError:       nil,
			wantErr:         false,
		},
		{
			name:            "add_ipv6_with_rate_limit",
			enableRateLimit: true,
			privateIPs:      []string{},
			count:           2,
			eniID:           "eni-test",
			isIpv6:          true,
			clientToken:     "token-456",
			mockResult:      []string{"2001:db8::1", "2001:db8::2"},
			mockError:       nil,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original config and restore after test
			originalEnableRateLimit := option.Config.EnableAPIRateLimit
			defer func() {
				option.Config.EnableAPIRateLimit = originalEnableRateLimit
			}()

			// Set rate limit config for this test
			option.Config.EnableAPIRateLimit = tt.enableRateLimit

			// Create mock client that returns the expected result
			mock := &mockClient{}

			// Create rate limiter
			limiterSet, err := rate.NewAPILimiterSet(nil, map[string]rate.APILimiterParameters{
				BatchAddPrivateIP: {
					RateLimit: grate.Limit(10),
					RateBurst: 10,
				},
			}, nil)
			ass.Nil(err, "failed to create rate limiter set")

			limiter := rate.APILimiterSetWrapDefault(limiterSet, &rate.APILimiterParameters{
				RateLimit: grate.Limit(10),
				RateBurst: 10,
			})

			// Create flow control client
			fc := &flowControlClient{
				client:  mock,
				limiter: limiter,
			}

			// Execute test
			ctx := context.Background()
			_, err = fc.BatchAddPrivateIPWithClientToken(ctx, tt.privateIPs, tt.count, tt.eniID, tt.isIpv6, tt.clientToken)

			// Validate error
			if tt.wantErr {
				ass.NotNil(err, "expected error but got nil")
				return
			}

			ass.Nil(err, "unexpected error: %v", err)
			// The mock returns nil, so we just check no error occurred
		})
	}
}

// TestFlowControlClient_BatchAddPrivateIPWithClientToken_ErrorCases tests error branches
func TestFlowControlClient_BatchAddPrivateIPWithClientToken_ErrorCases(t *testing.T) {
	ass := assert.New(t)

	t.Run("limiter_wait_error", func(t *testing.T) {
		// Save original config and restore after test
		originalEnableRateLimit := option.Config.EnableAPIRateLimit
		defer func() {
			option.Config.EnableAPIRateLimit = originalEnableRateLimit
		}()

		// Enable rate limiting
		option.Config.EnableAPIRateLimit = true

		// Create mock client
		mock := &mockClient{
			batchAddPrivateIPWithClientTokenFunc: func(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool, clientToken string) ([]string, error) {
				return []string{"10.0.0.1"}, nil
			},
		}

		// Create rate limiter
		limiterSet, err := rate.NewAPILimiterSet(nil, map[string]rate.APILimiterParameters{
			BatchAddPrivateIP: {
				RateLimit: grate.Limit(10),
				RateBurst: 10,
			},
		}, nil)
		ass.Nil(err, "failed to create rate limiter set")

		limiter := rate.APILimiterSetWrapDefault(limiterSet, &rate.APILimiterParameters{
			RateLimit: grate.Limit(10),
			RateBurst: 10,
		})

		// Create flow control client
		fc := &flowControlClient{
			client:  mock,
			limiter: limiter,
		}

		// Create a cancelled context to trigger limiter.Wait error
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Execute test - should get context canceled error from limiter.Wait
		_, err = fc.BatchAddPrivateIPWithClientToken(ctx, []string{}, 1, "eni-test", false, "token-123")

		// Validate error
		ass.NotNil(err, "expected error from cancelled context")
		ass.Contains(err.Error(), "context canceled", "expected context canceled error")
	})

	t.Run("client_error_in_defer", func(t *testing.T) {
		// Save original config and restore after test
		originalEnableRateLimit := option.Config.EnableAPIRateLimit
		defer func() {
			option.Config.EnableAPIRateLimit = originalEnableRateLimit
		}()

		// Enable rate limiting
		option.Config.EnableAPIRateLimit = true

		// Create mock client that returns an error
		expectedErr := errors.New("mock client error")
		mock := &mockClient{
			batchAddPrivateIPWithClientTokenFunc: func(ctx context.Context, privateIPs []string, count int, eniID string, isIpv6 bool, clientToken string) ([]string, error) {
				return nil, expectedErr
			},
		}

		// Create rate limiter
		limiterSet, err := rate.NewAPILimiterSet(nil, map[string]rate.APILimiterParameters{
			BatchAddPrivateIP: {
				RateLimit: grate.Limit(10),
				RateBurst: 10,
			},
		}, nil)
		ass.Nil(err, "failed to create rate limiter set")

		limiter := rate.APILimiterSetWrapDefault(limiterSet, &rate.APILimiterParameters{
			RateLimit: grate.Limit(10),
			RateBurst: 10,
		})

		// Create flow control client
		fc := &flowControlClient{
			client:  mock,
			limiter: limiter,
		}

		// Execute test
		ctx := context.Background()
		_, err = fc.BatchAddPrivateIPWithClientToken(ctx, []string{}, 1, "eni-test", false, "token-123")

		// Validate error - should get the client error and defer should call req.Error(err)
		ass.NotNil(err, "expected error from client")
		ass.Equal(expectedErr, err, "expected mock client error")
	})
}
