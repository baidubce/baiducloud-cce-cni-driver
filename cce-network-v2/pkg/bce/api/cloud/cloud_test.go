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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	eniExt "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	mockeni "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni/mock"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/wait"
)

// TestServerConfig defines the configuration for a mock HTTP server
type TestServerConfig struct {
	t                  *testing.T
	ass                *assert.Assertions
	RequestMethod      string
	RequestURLPath     string
	RequestBody        []byte
	RequestHeaders     map[string]string
	RequestQueryParams map[string]string
	ResponseHeaders    map[string]string
	ResponseBody       []byte
	ResponseBodyFunc   func(t *testing.T, actualBody []byte)
	ResponseStatusCode int
	HookAfterResponse  func()
	Debug              bool
}

// NewTestServer creates a new test HTTP server with the given configuration
func NewTestServer(t *testing.T, config *TestServerConfig) *httptest.Server {
	config.ass = assert.New(t)
	config.t = t
	return httptest.NewServer(config)
}

// ServeHTTP implements http.Handler for TestServerConfig
func (config *TestServerConfig) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ass := config.ass
	defer r.Body.Close()

	if config.RequestMethod != "" {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Method: ")
			spew.Fdump(os.Stderr, r.Method)
		}
		ass.Equal(config.RequestMethod, r.Method, "request method mismatch")
	}

	if config.RequestURLPath != "" {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Path: ")
			spew.Fdump(os.Stderr, r.URL.Path)
		}
		ass.Equal(config.RequestURLPath, r.URL.Path, "request path mismatch")
	}

	if config.Debug && len(config.RequestHeaders) > 0 {
		fmt.Fprintf(os.Stderr, "Header: ")
		spew.Fdump(os.Stderr, r.Header)
	}
	reqHeaders := r.Header
	for k, want := range config.RequestHeaders {
		actual := reqHeaders.Get(k)
		ass.Equal(want, actual, fmt.Sprintf("header '%s' mismatch", k))
	}

	if len(config.RequestQueryParams) > 0 {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "QueryParams: ")
			spew.Fdump(os.Stderr, r.URL.RawQuery)
		}
	}
	reqQueryParams := r.URL.Query()
	for k, want := range config.RequestQueryParams {
		actual := reqQueryParams.Get(k)
		ass.Equal(want, actual, fmt.Sprintf("query param '%s' mismatch", k))
	}

	// Always read the request body to avoid connection issues
	body, _ := ioutil.ReadAll(r.Body)
	if len(config.RequestBody) > 0 {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Body: ")
			spew.Fdump(os.Stderr, body)
		}
		if config.ResponseBodyFunc != nil {
			config.ResponseBodyFunc(config.t, body)
		} else {
			ass.Equal(config.RequestBody, body, "request body mismatch")
		}
	}

	// Write response headers
	for k, v := range config.ResponseHeaders {
		w.Header().Set(k, v)
	}

	// Write response status code
	if config.ResponseStatusCode != 0 {
		w.WriteHeader(config.ResponseStatusCode)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// Write response body (always write something to avoid nil body)
	if len(config.ResponseBody) > 0 {
		w.Write(config.ResponseBody)
	} else {
		// Write empty JSON object if no body is specified
		w.Write([]byte("{}"))
	}

	// Call hook after response
	if config.HookAfterResponse != nil {
		config.HookAfterResponse()
	}
}

// NewMockClient creates a new Client with mock VPC client pointing to the test server
func NewMockClient(endpoint string) (*Client, error) {
	vpcClient, err := vpc.NewClient("test-ak", "test-sk", endpoint)
	if err != nil {
		return nil, err
	}

	eniClient, err := eniExt.NewClient("test-ak", "test-sk", endpoint)
	if err != nil {
		return nil, err
	}

	return &Client{
		vpcClient: vpcClient,
		eniClient: eniClient,
	}, nil
}

// TestClient_ListSubnets tests the Client.ListSubnets method
func TestClient_ListSubnets(t *testing.T) {
	ass := assert.New(t)
	tests := []struct {
		name    string
		ctx     context.Context
		args    *vpc.ListSubnetArgs
		config  *TestServerConfig
		want    *vpc.ListSubnetResult
		wantErr bool
	}{
		{
			name: "list_subnets_with_vpc_id",
			ctx:  context.Background(),
			args: &vpc.ListSubnetArgs{
				VpcId:   "vpc-test123",
				MaxKeys: 100,
			},
			config: &TestServerConfig{
				RequestMethod:  http.MethodGet,
				RequestURLPath: "/v1/subnet",
				RequestQueryParams: map[string]string{
					"vpcId":   "vpc-test123",
					"maxKeys": "100",
				},
				ResponseHeaders: map[string]string{"Content-Type": "application/json"},
				ResponseBody: []byte(`{
					"subnets": [
						{
							"subnetId": "sbn-test1",
							"name": "test-subnet-1",
							"cidr": "192.168.1.0/24",
							"vpcId": "vpc-test123",
							"zoneName": "cn-bj-a"
						},
						{
							"subnetId": "sbn-test2",
							"name": "test-subnet-2",
							"cidr": "192.168.2.0/24",
							"vpcId": "vpc-test123",
							"zoneName": "cn-bj-b"
						}
					],
					"marker": "sbn-test2",
					"isTruncated": false,
					"nextMarker": "",
					"maxKeys": 100
				}`),
			},
			want: &vpc.ListSubnetResult{
				Subnets: []vpc.Subnet{
					{
						SubnetId: "sbn-test1",
						Name:     "test-subnet-1",
						Cidr:     "192.168.1.0/24",
						VPCId:    "vpc-test123",
						ZoneName: "cn-bj-a",
					},
					{
						SubnetId: "sbn-test2",
						Name:     "test-subnet-2",
						Cidr:     "192.168.2.0/24",
						VPCId:    "vpc-test123",
						ZoneName: "cn-bj-b",
					},
				},
				Marker:      "sbn-test2",
				IsTruncated: false,
				NextMarker:  "",
				MaxKeys:     100,
			},
			wantErr: false,
		},
		{
			name: "list_subnets_with_subnet_ids",
			ctx:  context.Background(),
			args: &vpc.ListSubnetArgs{
				SubnetIds: "sbn-test1,sbn-test2",
				MaxKeys:   1000,
			},
			config: &TestServerConfig{
				RequestMethod:  http.MethodGet,
				RequestURLPath: "/v1/subnet",
				RequestQueryParams: map[string]string{
					"subnetIds": "sbn-test1,sbn-test2",
					"maxKeys":   "1000",
				},
				ResponseHeaders: map[string]string{"Content-Type": "application/json"},
				ResponseBody: []byte(`{
					"subnets": [
						{
							"subnetId": "sbn-test1",
							"name": "test-subnet-1",
							"cidr": "192.168.1.0/24"
						},
						{
							"subnetId": "sbn-test2",
							"name": "test-subnet-2",
							"cidr": "192.168.2.0/24"
						}
					],
					"isTruncated": false,
					"maxKeys": 1000
				}`),
			},
			want: &vpc.ListSubnetResult{
				Subnets: []vpc.Subnet{
					{
						SubnetId: "sbn-test1",
						Name:     "test-subnet-1",
						Cidr:     "192.168.1.0/24",
					},
					{
						SubnetId: "sbn-test2",
						Name:     "test-subnet-2",
						Cidr:     "192.168.2.0/24",
					},
				},
				IsTruncated: false,
				MaxKeys:     1000,
			},
			wantErr: false,
		},
		{
			name: "list_subnets_empty_result",
			ctx:  context.Background(),
			args: &vpc.ListSubnetArgs{
				VpcId:   "vpc-empty",
				MaxKeys: 100,
			},
			config: &TestServerConfig{
				RequestMethod:  http.MethodGet,
				RequestURLPath: "/v1/subnet",
				RequestQueryParams: map[string]string{
					"vpcId":   "vpc-empty",
					"maxKeys": "100",
				},
				ResponseHeaders: map[string]string{"Content-Type": "application/json"},
				ResponseBody: []byte(`{
					"subnets": [],
					"isTruncated": false,
					"maxKeys": 100
				}`),
			},
			want: &vpc.ListSubnetResult{
				Subnets:     []vpc.Subnet{},
				IsTruncated: false,
				MaxKeys:     100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svr := NewTestServer(t, tt.config)
			defer svr.Close()

			client, err := NewMockClient(svr.URL)
			ass.Nil(err, "failed to create mock client")

			got, err := client.ListSubnets(tt.ctx, tt.args)
			if tt.wantErr {
				ass.NotNil(err, "expected error but got nil")
				return
			}

			ass.Nil(err, "unexpected error: %v", err)
			ass.NotNil(got, "result should not be nil")
			ass.Equal(len(tt.want.Subnets), len(got.Subnets), "subnet count mismatch")
			ass.Equal(tt.want.IsTruncated, got.IsTruncated, "IsTruncated mismatch")
			ass.Equal(tt.want.MaxKeys, got.MaxKeys, "MaxKeys mismatch")

			// Verify subnet details
			for i, wantSubnet := range tt.want.Subnets {
				ass.Equal(wantSubnet.SubnetId, got.Subnets[i].SubnetId, "subnet ID mismatch at index %d", i)
				ass.Equal(wantSubnet.Name, got.Subnets[i].Name, "subnet name mismatch at index %d", i)
				ass.Equal(wantSubnet.Cidr, got.Subnets[i].Cidr, "subnet CIDR mismatch at index %d", i)
			}
		})
	}
}

// TestClient_BatchAddPrivateIP tests the Client.BatchAddPrivateIP method
func TestClient_BatchAddPrivateIP(t *testing.T) {
	ass := assert.New(t)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		config := &TestServerConfig{
			RequestMethod:   http.MethodPost,
			RequestURLPath:  "/v1/eni/eni-test/privateIp/batchAdd",
			ResponseHeaders: map[string]string{"Content-Type": "application/json"},
			ResponseBody:    []byte(`{"privateIpAddresses": ["192.168.1.10", "192.168.1.11"]}`),
		}
		svr := NewTestServer(t, config)
		defer svr.Close()

		client, err := NewMockClient(svr.URL)
		ass.Nil(err)

		ips, err := client.BatchAddPrivateIP(ctx, []string{}, 2, "eni-test", false)
		ass.Nil(err)
		ass.Equal(2, len(ips))
	})
}

// TestClient_BatchAddPrivateIPWithClientToken tests the Client.BatchAddPrivateIPWithClientToken method
func TestClient_BatchAddPrivateIPWithClientToken(t *testing.T) {
	ass := assert.New(t)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		config := &TestServerConfig{
			RequestMethod:   http.MethodPost,
			RequestURLPath:  "/v1/eni/eni-test/privateIp/batchAdd",
			ResponseHeaders: map[string]string{"Content-Type": "application/json"},
			ResponseBody:    []byte(`{"privateIpAddresses": ["192.168.1.10"]}`),
		}
		svr := NewTestServer(t, config)
		defer svr.Close()

		client, err := NewMockClient(svr.URL)
		ass.Nil(err)

		ips, err := client.BatchAddPrivateIPWithClientToken(ctx, []string{}, 1, "eni-test", false, "token-123")
		ass.Nil(err)
		ass.Equal(1, len(ips))
	})

	t.Run("error", func(t *testing.T) {
		config := &TestServerConfig{
			RequestMethod:      http.MethodPost,
			RequestURLPath:     "/v1/eni/eni-test/privateIp/batchAdd",
			ResponseStatusCode: http.StatusInternalServerError,
			ResponseHeaders:    map[string]string{"Content-Type": "application/json"},
			ResponseBody:       []byte(`{"code": "InternalError", "message": "internal error"}`),
		}
		svr := NewTestServer(t, config)
		defer svr.Close()

		client, err := NewMockClient(svr.URL)
		ass.Nil(err)

		_, err = client.BatchAddPrivateIPWithClientToken(ctx, []string{}, 1, "eni-test", false, "token-123")
		ass.NotNil(err)
	})
}

func TestGetRetryConfig(t *testing.T) {
	ass := assert.New(t)

	tests := []struct {
		name               string
		setupConfig        func()
		wantRetryCount     int
		wantMaxDelayMs     int64
		wantBaseIntervalMs int64
	}{
		{
			name: "default_values_when_config_not_set",
			setupConfig: func() {
				operatorOption.Config.BCEAPIRetryCount = 0
				operatorOption.Config.BCEAPIRetryMaxDelay = 0
				operatorOption.Config.BCEAPIRetryBaseInterval = 0
			},
			wantRetryCount:     1,
			wantMaxDelayMs:     5000,
			wantBaseIntervalMs: 500,
		},
		{
			name: "use_config_values_when_set",
			setupConfig: func() {
				operatorOption.Config.BCEAPIRetryCount = 2
				operatorOption.Config.BCEAPIRetryMaxDelay = 10 * time.Second
				operatorOption.Config.BCEAPIRetryBaseInterval = 1 * time.Second
			},
			wantRetryCount:     2,
			wantMaxDelayMs:     10000,
			wantBaseIntervalMs: 1000,
		},
		{
			name: "partial_config_override",
			setupConfig: func() {
				operatorOption.Config.BCEAPIRetryCount = 3
				operatorOption.Config.BCEAPIRetryMaxDelay = 0
				operatorOption.Config.BCEAPIRetryBaseInterval = 800 * time.Millisecond
			},
			wantRetryCount:     3,
			wantMaxDelayMs:     5000, // default
			wantBaseIntervalMs: 800,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupConfig()

			retryCount, maxDelayMs, baseIntervalMs := getRetryConfig()

			ass.Equal(tt.wantRetryCount, retryCount, "retry count mismatch")
			ass.Equal(tt.wantMaxDelayMs, maxDelayMs, "max delay mismatch")
			ass.Equal(tt.wantBaseIntervalMs, baseIntervalMs, "base interval mismatch")
		})
	}
}

func TestClient_ListENIs_PaginationSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	eniMock := mockeni.NewMockClientInterface(ctrl)
	client := &Client{
		eniClient: eniMock,
	}

	ctx := context.Background()
	args := eni.ListEniArgs{
		VpcId:      "vpc-test",
		Name:       "eni-name",
		InstanceId: "instance-123",
	}

	gomock.InOrder(
		eniMock.EXPECT().ListEnis(gomock.AssignableToTypeOf(&eni.ListEniArgs{})).
			DoAndReturn(func(req *eni.ListEniArgs) (*eni.ListEniResult, error) {
				assert.Equal(t, "", req.Marker)
				assert.Equal(t, args.VpcId, req.VpcId)
				assert.Equal(t, args.Name, req.Name)
				assert.Equal(t, args.InstanceId, req.InstanceId)
				return &eni.ListEniResult{
					Eni: []eni.Eni{
						{EniId: "eni-1"},
					},
					IsTruncated: true,
					NextMarker:  "marker-1",
				}, nil
			}),
		eniMock.EXPECT().ListEnis(gomock.AssignableToTypeOf(&eni.ListEniArgs{})).
			DoAndReturn(func(req *eni.ListEniArgs) (*eni.ListEniResult, error) {
				assert.Equal(t, "marker-1", req.Marker)
				assert.Equal(t, args.VpcId, req.VpcId)
				assert.Equal(t, args.Name, req.Name)
				assert.Equal(t, args.InstanceId, req.InstanceId)
				return &eni.ListEniResult{
					Eni: []eni.Eni{
						{EniId: "eni-2"},
					},
					IsTruncated: false,
				}, nil
			}),
	)

	enis, err := client.ListENIs(ctx, args)
	assert.NoError(t, err)
	assert.Len(t, enis, 2)
	assert.Equal(t, []string{"eni-1", "eni-2"}, []string{enis[0].EniId, enis[1].EniId})
}

func TestClient_ListENIs_ReturnsErrorOnNilResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	eniMock := mockeni.NewMockClientInterface(ctrl)
	client := &Client{
		eniClient: eniMock,
	}

	eniMock.EXPECT().ListEnis(gomock.AssignableToTypeOf(&eni.ListEniArgs{})).
		Return(nil, nil)

	enis, err := client.ListENIs(context.Background(), eni.ListEniArgs{})
	assert.Error(t, err)
	assert.Nil(t, enis)
	assert.EqualError(t, err, "ListENI returned nil response")
}

func TestClient_ListENIs_ReturnsLastErrorWhenContextCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	eniMock := mockeni.NewMockClientInterface(ctrl)
	client := &Client{
		eniClient: eniMock,
	}

	ctx, cancel := context.WithCancel(context.Background())
	expectedErr := errors.New("temporary failure")

	eniMock.EXPECT().ListEnis(gomock.AssignableToTypeOf(&eni.ListEniArgs{})).
		DoAndReturn(func(req *eni.ListEniArgs) (*eni.ListEniResult, error) {
			cancel()
			return nil, expectedErr
		})

	enis, err := client.ListENIs(ctx, eni.ListEniArgs{InstanceId: "instance-123"})
	assert.Error(t, err)
	assert.Nil(t, enis)
	assert.Equal(t, expectedErr, err)
}

func setupTestLogger(t *testing.T) *bytes.Buffer {
	buf := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
	})
	logger.SetOutput(buf)

	originalLog := log
	log = logger.WithField("subsys", "bce-cloud")
	t.Cleanup(func() {
		log = originalLog
	})

	return buf
}

func patchListEnis(fn func(*eni.ListEniArgs) (*eni.ListEniResult, error)) *gomonkey.Patches {
	return gomonkey.ApplyMethod(reflect.TypeOf((*eniExt.Client)(nil)), "ListEnis",
		func(_ *eniExt.Client, args *eni.ListEniArgs) (*eni.ListEniResult, error) {
			return fn(args)
		})
}

func patchBackoff(attempts int, finalErr error) *gomonkey.Patches {
	return gomonkey.ApplyFunc(wait.ExponentialBackoffWithContext,
		func(ctx context.Context, backoff wait.Backoff, condition wait.ConditionFunc) error {
			for i := 0; i < attempts; i++ {
				done, err := condition()
				if err != nil {
					return err
				}
				if done {
					return nil
				}
			}

			if finalErr != nil {
				return finalErr
			}
			return nil
		})
}
