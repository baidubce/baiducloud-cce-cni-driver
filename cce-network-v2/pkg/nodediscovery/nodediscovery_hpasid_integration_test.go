//go:build linux
// +build linux

/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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

package nodediscovery

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/agent"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	nodemanager "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/manager"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

// TestMutateNodeResource_HPASInstance_WithValidHPASID tests HPAS instance with valid HPASID
// This test covers lines 421-442 in nodediscovery.go
func TestMutateNodeResource_HPASInstance_WithValidHPASID(t *testing.T) {
	// Setup
	mockClient := &mockMetadataClientForHPASID{
		instanceTypeEx:    metadata.InstanceTypeExHPAS,
		instanceTypeExErr: nil,
		hpasID:            "hpas-test-12345",
		hpasIDErr:         nil,
		instanceID:        "i-test-hpas",
		instanceIDErr:     nil,
	}

	// Set the mock client
	agent.SetMetadataClient(mockClient)
	defer agent.ResetMetadataClient()

	// Create NodeDiscovery instance
	nodeMngr, err := nodemanager.NewManager("test", nil)
	require.NoError(t, err, "Failed to create node manager")

	nd := &NodeDiscovery{
		Manager: nodeMngr,
		k8sNodeGetter: &mockK8sNodeGetter{
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
		},
		nrcsNodeGetter: &mockNrcsNodeGetter{
			nrcs: nil,
			err:  nil,
		},
		eventRecorder: record.NewFakeRecorder(100),
	}

	// Initialize option.Config
	option.Config.IPAM = "cluster-pool"

	// Create NetResourceSet
	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Node",
					Name:       "test-node",
				},
			},
		},
		Spec: ccev2.NetResourceSpec{},
	}

	// Execute
	err = nd.mutateNodeResource(nrs)

	// Assert
	require.NoError(t, err, "mutateNodeResource should not return error for HPAS instance with valid HPASID")
	assert.Equal(t, "hpas-test-12345", nrs.Spec.HPASID, "HPASID should be set correctly")
	assert.Equal(t, "i-test-hpas", nrs.Spec.InstanceID, "InstanceID should be set correctly")
}

// 专用 mock：覆盖 GetSourceType 返回 BCC
type mockMetadataClientForHPASID_BCC struct{ mockMetadataClientForHPASID }

func (m *mockMetadataClientForHPASID_BCC) GetSourceType() (string, error) { return "bcc", nil }

// 专用 mock：覆盖 GetSourceType 返回 EBC
type mockMetadataClientForHPASID_EBC struct{ mockMetadataClientForHPASID }

func (m *mockMetadataClientForHPASID_EBC) GetSourceType() (string, error) { return "ebc", nil }

// TestMutateNodeResource_BCCInstance_WithEmptyHPASID tests BCC instance with empty HPASID
// This test covers lines 421-442 in nodediscovery.go
func TestMutateNodeResource_BCCInstance_WithEmptyHPASID(t *testing.T) {
	// Setup
	base := mockMetadataClientForHPASID{
		instanceTypeEx:    metadata.InstanceTypeExBCC,
		instanceTypeExErr: nil,
		hpasID:            "",
		hpasIDErr:         nil,
		instanceID:        "i-test-bcc",
		instanceIDErr:     nil,
	}
	mockClient := &mockMetadataClientForHPASID_BCC{base}

	// Set the mock client
	agent.SetMetadataClient(mockClient)
	defer agent.ResetMetadataClient()

	addr := linux.NewNodeAddressing()
	// Create NodeDiscovery instance，使用唯一名称避免 Prometheus 指标重复注册
	nodeMngr, err := nodemanager.NewManager(t.Name(), linux.NewNodeHandler(addr))
	require.NoError(t, err, "Failed to create node manager")
	defer nodeMngr.Close()

	nd := &NodeDiscovery{
		Manager: nodeMngr,
		k8sNodeGetter: &mockK8sNodeGetter{
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
		},
		nrcsNodeGetter: &mockNrcsNodeGetter{
			nrcs: nil,
			err:  nil,
		},
		eventRecorder: record.NewFakeRecorder(100),
	}

	// Initialize option.Config
	option.Config.IPAM = "cluster-pool"

	// Create NetResourceSet
	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Node",
					Name:       "test-node",
				},
			},
		},
		Spec: ccev2.NetResourceSpec{},
	}

	// Execute
	err = nd.mutateNodeResource(nrs)

	// Assert
	require.NoError(t, err, "mutateNodeResource should not return error for BCC instance")
	assert.Empty(t, nrs.Spec.HPASID, "HPASID should be empty for BCC instance")
	assert.Equal(t, "i-test-bcc", nrs.Spec.InstanceID, "InstanceID should be set correctly")
}

// TestMutateNodeResource_HPASInstance_WithHPASID_Assignment tests HPASID assignment (line 442)
// This test specifically covers the assignment: nodeResource.Spec.HPASID = resourceID
func TestMutateNodeResource_HPASInstance_WithHPASID_Assignment(t *testing.T) {
	testCases := []struct {
		name           string
		instanceTypeEx metadata.InstanceTypeEx
		hpasID         string
		expectedHPASID string
		description    string
	}{
		{
			name:           "HPAS_with_valid_HPASID",
			instanceTypeEx: metadata.InstanceTypeExHPAS,
			hpasID:         "hpas-assignment-test",
			expectedHPASID: "hpas-assignment-test",
			description:    "HPAS instance should have HPASID assigned",
		},
		{
			name:           "BCC_with_empty_HPASID",
			instanceTypeEx: metadata.InstanceTypeExBCC,
			hpasID:         "",
			expectedHPASID: "",
			description:    "BCC instance should have empty HPASID",
		},
		{
			name:           "EBC_with_empty_HPASID",
			instanceTypeEx: metadata.InstanceTypeExEBC,
			hpasID:         "",
			expectedHPASID: "",
			description:    "EBC instance should have empty HPASID",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup base mock
			base := mockMetadataClientForHPASID{
				instanceTypeEx:    tc.instanceTypeEx,
				instanceTypeExErr: nil,
				hpasID:            tc.hpasID,
				hpasIDErr:         nil,
				instanceID:        "i-test-assignment",
				instanceIDErr:     nil,
			}

			// 根据实例类型选择合适的 mock，确保 GetSourceType 与实例类型一致
			switch tc.instanceTypeEx {
			case metadata.InstanceTypeExBCC:
				agent.SetMetadataClient(&mockMetadataClientForHPASID_BCC{base})
			case metadata.InstanceTypeExEBC:
				agent.SetMetadataClient(&mockMetadataClientForHPASID_EBC{base})
			default:
				agent.SetMetadataClient(&base)
			}
			defer agent.ResetMetadataClient()

			addr := linux.NewNodeAddressing()
			name := generateManagerName(t.Name(), tc.name)
			nodeMngr, err := nodemanager.NewManager(name, linux.NewNodeHandler(addr))
			require.NoError(t, err, "Failed to create node manager")
			defer nodeMngr.Close()

			nd := &NodeDiscovery{
				Manager: nodeMngr,
				k8sNodeGetter: &mockK8sNodeGetter{
					node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-node",
						},
					},
				},
				nrcsNodeGetter: &mockNrcsNodeGetter{
					nrcs: nil,
					err:  nil,
				},
				eventRecorder: record.NewFakeRecorder(100),
			}

			// Initialize option.Config
			option.Config.IPAM = "cluster-pool"

			// Create NetResourceSet
			nrs := &ccev2.NetResourceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Node",
							Name:       "test-node",
						},
					},
				},
				Spec: ccev2.NetResourceSpec{},
			}

			// Execute
			err = nd.mutateNodeResource(nrs)

			// Assert
			require.NoError(t, err, tc.description)
			assert.Equal(t, tc.expectedHPASID, nrs.Spec.HPASID, tc.description)
		})
	}
}

// generateManagerName 生成合法的节点管理器名称
func generateManagerName(testName, caseName string) string {
	// 替换所有非法字符为下划线
	sanitize := func(s string) string {
		s = strings.ReplaceAll(s, "/", "_")
		s = strings.ReplaceAll(s, "-", "_")
		s = strings.ReplaceAll(s, " ", "_")
		// 移除多余的连续下划线
		for strings.Contains(s, "__") {
			s = strings.ReplaceAll(s, "__", "_")
		}
		return strings.Trim(s, "_")
	}

	// 使用测试用例名称和随机后缀避免冲突
	testPart := sanitize(testName)
	casePart := sanitize(caseName)
	randomSuffix := uuid.New().String()[:8]

	return fmt.Sprintf("test_%s_%s_%s", testPart, casePart, randomSuffix)
}
