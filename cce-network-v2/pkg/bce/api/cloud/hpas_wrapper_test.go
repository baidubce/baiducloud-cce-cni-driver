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
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/hpas"
	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/services/bcc"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/stretchr/testify/assert"

	eniExt "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
)

// TestClient_HPASWrapper tests the Client.HPASWrapper method
func TestClient_HPASWrapper(t *testing.T) {
	ass := assert.New(t)

	tests := []struct {
		name        string
		setupClient func() *Client
		wantErr     bool
	}{
		{
			name: "hpas_wrapper_success",
			setupClient: func() *Client {
				// Create mock signers
				originalBCCSigner := &mockSigner{name: "original-bcc-signer"}
				originalENISigner := &mockSigner{name: "original-eni-signer"}

				// Create BceClient with signer
				bceClient := bce.NewBceClient(&bce.BceClientConfiguration{}, originalBCCSigner)
				bceClientENI := bce.NewBceClient(&bce.BceClientConfiguration{}, originalENISigner)

				// Create client with mock signers
				client := &Client{
					bccClient: &bcc.Client{
						BceClient: bceClient,
					},
					eniClient: &eniExt.Client{
						Client: &eni.Client{
							BceClient: bceClientENI,
						},
					},
				}

				return client
			},
			wantErr: false,
		},
		{
			name: "hpas_wrapper_with_nil_signers",
			setupClient: func() *Client {
				// Create BceClient with nil signer
				bceClient := bce.NewBceClient(&bce.BceClientConfiguration{}, nil)
				bceClientENI := bce.NewBceClient(&bce.BceClientConfiguration{}, nil)

				// Create client with nil signers
				client := &Client{
					bccClient: &bcc.Client{
						BceClient: bceClient,
					},
					eniClient: &eniExt.Client{
						Client: &eni.Client{
							BceClient: bceClientENI,
						},
					},
				}

				return client
			},
			wantErr: false,
		},
		{
			name: "hpas_wrapper_with_existing_wrapper",
			setupClient: func() *Client {
				// Create existing HPAS wrapper
				existingWrapper := hpas.NewHPASSignWrapper(&mockSigner{name: "existing-wrapper"})

				// Create BceClient with existing wrapper
				bceClient := bce.NewBceClient(&bce.BceClientConfiguration{}, existingWrapper)
				bceClientENI := bce.NewBceClient(&bce.BceClientConfiguration{}, existingWrapper)

				client := &Client{
					bccClient: &bcc.Client{
						BceClient: bceClient,
					},
					eniClient: &eniExt.Client{
						Client: &eni.Client{
							BceClient: bceClientENI,
						},
					},
				}

				return client
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient()

			// Store original signers for comparison
			originalBCCSigner := client.bccClient.Signer
			originalENISigner := client.eniClient.Signer

			// Call HPASWrapper
			err := client.HPASWrapper(context.Background())

			// Validate error
			if tt.wantErr {
				ass.NotNil(err, "expected error but got nil")
				return
			}

			ass.Nil(err, "unexpected error: %v", err)

			// Validate that signers have been wrapped
			ass.NotNil(client.bccClient.Signer, "bccClient.Signer should not be nil after HPASWrapper")
			ass.NotNil(client.eniClient.Signer, "eniClient.Signer should not be nil after HPASWrapper")

			// Validate that the signers are now HPASWrapper instances
			ass.IsType(&hpas.HPASWrapper{}, client.bccClient.Signer, "bccClient.Signer should be wrapped in HPASWrapper")
			ass.IsType(&hpas.HPASWrapper{}, client.eniClient.Signer, "eniClient.Signer should be wrapped in HPASWrapper")

			// Validate that the original signers are preserved inside the wrapper
			if originalBCCSigner != nil {
				bccWrapper := client.bccClient.Signer.(*hpas.HPASWrapper)
				ass.Equal(originalBCCSigner, bccWrapper.Signer, "original BCC signer should be preserved in wrapper")
			}

			if originalENISigner != nil {
				eniWrapper := client.eniClient.Signer.(*hpas.HPASWrapper)
				ass.Equal(originalENISigner, eniWrapper.Signer, "original ENI signer should be preserved in wrapper")
			}
		})
	}
}

// mockSigner is a mock implementation of auth.Signer for testing
type mockSigner struct {
	name string
}

func (m *mockSigner) Sign(req *http.Request, cred *auth.BceCredentials, opt *auth.SignOptions) {
	// Mock implementation - does nothing
}

func (m *mockSigner) String() string {
	return m.name
}

// TestClient_HPASWrapper_Context tests that HPASWrapper handles context correctly
func TestClient_HPASWrapper_Context(t *testing.T) {
	ass := assert.New(t)

	// Create mock signers
	originalBCCSigner := &mockSigner{name: "test-bcc-signer"}
	originalENISigner := &mockSigner{name: "test-eni-signer"}

	// Create BceClient with signer
	bceClient := bce.NewBceClient(&bce.BceClientConfiguration{}, originalBCCSigner)
	bceClientENI := bce.NewBceClient(&bce.BceClientConfiguration{}, originalENISigner)

	client := &Client{
		bccClient: &bcc.Client{
			BceClient: bceClient,
		},
		eniClient: &eniExt.Client{
			Client: &eni.Client{
				BceClient: bceClientENI,
			},
		},
	}

	// Test with canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel the context immediately

	err := client.HPASWrapper(ctx)
	ass.Nil(err, "HPASWrapper should not error even with canceled context")

	// Validate that signers are still wrapped despite canceled context
	ass.NotNil(client.bccClient.Signer, "bccClient.Signer should not be nil")
	ass.NotNil(client.eniClient.Signer, "eniClient.Signer should not be nil")
	ass.IsType(&hpas.HPASWrapper{}, client.bccClient.Signer, "bccClient.Signer should be wrapped")
	ass.IsType(&hpas.HPASWrapper{}, client.eniClient.Signer, "eniClient.Signer should be wrapped")
}

// TestClient_HPASWrapper_MultipleCalls tests that multiple calls to HPASWrapper work correctly
func TestClient_HPASWrapper_MultipleCalls(t *testing.T) {
	ass := assert.New(t)

	// Create mock signers
	originalBCCSigner := &mockSigner{name: "original-bcc-signer"}
	originalENISigner := &mockSigner{name: "original-eni-signer"}

	// Create BceClient with signer
	bceClient := bce.NewBceClient(&bce.BceClientConfiguration{}, originalBCCSigner)
	bceClientENI := bce.NewBceClient(&bce.BceClientConfiguration{}, originalENISigner)

	client := &Client{
		bccClient: &bcc.Client{
			BceClient: bceClient,
		},
		eniClient: &eniExt.Client{
			Client: &eni.Client{
				BceClient: bceClientENI,
			},
		},
	}

	ctx := context.Background()

	// First call
	err1 := client.HPASWrapper(ctx)
	ass.Nil(err1, "first HPASWrapper call should not error")

	// Get the signers after first call
	firstBCCSigner := client.bccClient.Signer
	firstENISigner := client.eniClient.Signer

	// Second call
	err2 := client.HPASWrapper(ctx)
	ass.Nil(err2, "second HPASWrapper call should not error")

	// Validate that signers are still wrapped
	ass.NotNil(client.bccClient.Signer, "bccClient.Signer should not be nil after second call")
	ass.NotNil(client.eniClient.Signer, "eniClient.Signer should not be nil after second call")
	ass.IsType(&hpas.HPASWrapper{}, client.bccClient.Signer, "bccClient.Signer should be wrapped after second call")
	ass.IsType(&hpas.HPASWrapper{}, client.eniClient.Signer, "eniClient.Signer should be wrapped after second call")

	// Validate that the signers have been re-wrapped (new instances)
	ass.NotEqual(firstBCCSigner, client.bccClient.Signer, "BCC signer should be re-wrapped on second call")
	ass.NotEqual(firstENISigner, client.eniClient.Signer, "ENI signer should be re-wrapped on second call")
}
