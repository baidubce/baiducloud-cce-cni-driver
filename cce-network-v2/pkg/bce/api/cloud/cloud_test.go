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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
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

	if len(config.RequestBody) > 0 {
		body, _ := ioutil.ReadAll(r.Body)
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

	// Write response body
	if len(config.ResponseBody) > 0 {
		w.Write(config.ResponseBody)
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

	return &Client{
		vpcClient: vpcClient,
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
