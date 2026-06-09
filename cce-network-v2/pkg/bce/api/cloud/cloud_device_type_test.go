package cloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/baidubce/bce-sdk-go/services/bcc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	eniapi "github.com/baidubce/bce-sdk-go/services/eni"

	eniwrapper "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
)

const (
	testEndpointAK = "test-ak"
	testEndpointSK = "test-sk"
)

func newENICloudClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	eniClient, err := eniapi.NewClient(testEndpointAK, testEndpointSK, endpoint)
	require.NoError(t, err)
	return &Client{
		eniClient: &eniwrapper.Client{Client: eniClient},
	}
}

func newBBCCloudClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	bbcClient, err := bbc.NewClient(testEndpointAK, testEndpointSK, endpoint)
	require.NoError(t, err)
	return &Client{
		bbcClient: bbcClient,
	}
}

func newBCCCloudClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	bccClient, err := bcc.NewClient(testEndpointAK, testEndpointSK, endpoint)
	require.NoError(t, err)
	return &Client{
		bccClient: bccClient,
	}
}

func assertInstanceTypeFromBody(t *testing.T, r *http.Request, want string) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	defer r.Body.Close()

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &payload))

	value, ok := payload["instanceType"].(string)
	require.Truef(t, ok, "instanceType missing in payload %s", string(body))
	assert.Equal(t, want, value, "unexpected instanceType")
}

func TestGetDeviceType(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		want       string
	}{
		{
			name:       "hpas prefix",
			instanceID: string(metadata.InstanceTypeExHPAS) + "-123",
			want:       string(metadata.InstanceTypeExHPAS),
		},
		{
			name:       "non hpas",
			instanceID: "bcc-test",
			want:       "",
		},
		{
			name:       "empty",
			instanceID: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDeviceType(tt.instanceID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClient_ListENIs_HPASEndpoint(t *testing.T) {
	instanceID := string(metadata.InstanceTypeExHPAS) + "-eni"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/eni", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"enis": [
				{
					"eniId": "eni-test",
					"networkInterfaceTrafficMode": "standard"
				}
			],
			"marker": "",
			"isTruncated": false,
			"nextMarker": "",
			"maxKeys": 1
		}`))
	}))
	t.Cleanup(svr.Close)

	client := newENICloudClient(t, svr.URL)
	enis, err := client.ListENIs(context.Background(), eniapi.ListEniArgs{
		VpcId:      "vpc-test",
		InstanceId: instanceID,
		MaxKeys:    1,
	})
	require.NoError(t, err)
	assert.Len(t, enis, 1)
}

func TestClient_ListERIs_UsesDeviceTypeQuery(t *testing.T) {
	instanceID := string(metadata.InstanceTypeExHPAS) + "-eri"
	expectedType := string(metadata.InstanceTypeExHPAS)

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/eni", r.URL.Path)
		assert.Equal(t, expectedType, r.URL.Query().Get("deviceType"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"enis": [
				{
					"eniId": "eni-test",
					"networkInterfaceTrafficMode": "highPerformance"
				}
			],
			"marker": "",
			"isTruncated": false,
			"nextMarker": "",
			"maxKeys": 1
		}`))
	}))
	t.Cleanup(svr.Close)

	client := newENICloudClient(t, svr.URL)
	eris, err := client.ListERIs(context.Background(), eniapi.ListEniArgs{
		VpcId:      "vpc-test",
		InstanceId: instanceID,
		MaxKeys:    1,
	})
	require.NoError(t, err)
	assert.Len(t, eris, 1)
}

func TestClient_ENIMutationsIncludeDeviceType(t *testing.T) {
	instanceID := string(metadata.InstanceTypeExHPAS) + "-eni"
	expectedType := string(metadata.InstanceTypeExHPAS)
	eniID := "eni-test"

	tests := []struct {
		name          string
		pathAssertion func(t *testing.T, r *http.Request)
		invoke        func(context.Context, *Client) error
		responseBody  string
	}{
		{
			name: "create",
			pathAssertion: func(t *testing.T, r *http.Request) {
				assert.Equal(t, "/v1/eni", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assertInstanceTypeFromBody(t, r, expectedType)
			},
			invoke: func(ctx context.Context, c *Client) error {
				_, err := c.CreateENI(ctx, &eniapi.CreateEniArgs{
					Name:       "eni-name",
					SubnetId:   "subnet-test",
					InstanceId: instanceID,
					PrivateIpSet: []eniapi.PrivateIp{
						{PrivateIpAddress: "192.168.1.10", Primary: true},
					},
					SecurityGroupIds: []string{"sg-test"},
				})
				return err
			},
			responseBody: `{"eniId":"` + eniID + `"}`,
		},
		{
			name: "attach",
			pathAssertion: func(t *testing.T, r *http.Request) {
				assert.Equal(t, "/v1/eni/"+eniID, r.URL.Path)
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "", r.URL.Query().Get("attach"))
				assertInstanceTypeFromBody(t, r, expectedType)
			},
			invoke: func(ctx context.Context, c *Client) error {
				return c.AttachENI(ctx, &eniapi.EniInstance{
					EniId:      eniID,
					InstanceId: instanceID,
				})
			},
		},
		{
			name: "detach",
			pathAssertion: func(t *testing.T, r *http.Request) {
				assert.Equal(t, "/v1/eni/"+eniID, r.URL.Path)
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "", r.URL.Query().Get("detach"))
				assertInstanceTypeFromBody(t, r, expectedType)
			},
			invoke: func(ctx context.Context, c *Client) error {
				return c.DetachENI(ctx, &eniapi.EniInstance{
					EniId:      eniID,
					InstanceId: instanceID,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.pathAssertion(t, r)
				if tt.responseBody != "" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tt.responseBody))
				}
			}))
			t.Cleanup(svr.Close)

			client := newENICloudClient(t, svr.URL)
			err := tt.invoke(context.Background(), client)
			require.NoError(t, err)
		})
	}
}

func TestClient_GetENIQuota_HPASEndpoint(t *testing.T) {
	instanceID := string(metadata.InstanceTypeExHPAS) + "-eni"

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/eni/quota", r.URL.Path)
		assert.Equal(t, instanceID, r.URL.Query().Get("instanceId"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalQuantity":5,"availableQuantity":3}`))
	}))
	t.Cleanup(svr.Close)

	client := newENICloudClient(t, svr.URL)
	resp, err := client.GetENIQuota(context.Background(), instanceID)
	require.NoError(t, err)
	assert.Equal(t, 5, resp.TotalQuantity)
	assert.Equal(t, 3, resp.AvailableQuantity)
}

func TestClient_BBCBatchIPOperationsIncludeDeviceType(t *testing.T) {
	instanceID := string(metadata.InstanceTypeExHPAS) + "-bbc"
	expectedType := string(metadata.InstanceTypeExHPAS)

	tests := []struct {
		name         string
		path         string
		responseBody string
		invoke       func(context.Context, *Client) error
	}{
		{
			name:         "batch_add_ip",
			path:         "/v1/instance/batchAddIp",
			responseBody: `{"privateIps":["10.0.0.10"]}`,
			invoke: func(ctx context.Context, c *Client) error {
				_, err := c.BBCBatchAddIP(ctx, &bbc.BatchAddIpArgs{
					InstanceId:                     instanceID,
					PrivateIps:                     []string{"10.0.0.10"},
					SecondaryPrivateIpAddressCount: 1,
				})
				return err
			},
		},
		{
			name: "batch_del_ip",
			path: "/v1/instance/batchDelIp",
			invoke: func(ctx context.Context, c *Client) error {
				return c.BBCBatchDelIP(ctx, &bbc.BatchDelIpArgs{
					InstanceId: instanceID,
					PrivateIps: []string{"10.0.0.10"},
				})
			},
		},
		{
			name:         "batch_add_ip_cross_subnet",
			path:         "/v1/instance/batchAddIpCrossSubnet",
			responseBody: `{"privateIps":["10.0.0.20"]}`,
			invoke: func(ctx context.Context, c *Client) error {
				_, err := c.BBCBatchAddIPCrossSubnet(ctx, &bbc.BatchAddIpCrossSubnetArgs{
					InstanceId: instanceID,
					SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
						{
							EniId:                          "eni-1",
							SubnetId:                       "subnet-1",
							SecondaryPrivateIpAddressCount: 1,
						},
					},
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.path, r.URL.Path)
				assertInstanceTypeFromBody(t, r, expectedType)
				if tt.responseBody != "" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tt.responseBody))
				}
			}))
			t.Cleanup(svr.Close)

			client := newBBCCloudClient(t, svr.URL)
			err := tt.invoke(context.Background(), client)
			require.NoError(t, err)
		})
	}
}

func TestClient_BCCBatchIPOperationsIncludeDeviceType(t *testing.T) {
	instanceID := string(metadata.InstanceTypeExHPAS) + "-bcc"
	expectedType := string(metadata.InstanceTypeExHPAS)

	tests := []struct {
		name         string
		path         string
		responseBody string
		invoke       func(context.Context, *Client) error
	}{
		{
			name:         "bcc_batch_add_ip",
			path:         "/v2/instance/batchAddIp",
			responseBody: `{"privateIps":["10.2.0.10"]}`,
			invoke: func(ctx context.Context, c *Client) error {
				_, err := c.BCCBatchAddIP(ctx, &bccapi.BatchAddIpArgs{
					InstanceId:                     instanceID,
					PrivateIps:                     []string{"10.2.0.10"},
					SecondaryPrivateIpAddressCount: 1,
				})
				return err
			},
		},
		{
			name: "bcc_batch_del_ip",
			path: "/v2/instance/batchDelIp",
			invoke: func(ctx context.Context, c *Client) error {
				return c.BCCBatchDelIP(ctx, &bccapi.BatchDelIpArgs{
					InstanceId: instanceID,
					PrivateIps: []string{"10.2.0.10"},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.path, r.URL.Path)
				assertInstanceTypeFromBody(t, r, expectedType)
				if tt.responseBody != "" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tt.responseBody))
				}
			}))
			t.Cleanup(svr.Close)

			client := newBCCCloudClient(t, svr.URL)
			err := tt.invoke(context.Background(), client)
			require.NoError(t, err)
		})
	}
}
