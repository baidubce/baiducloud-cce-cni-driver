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
	"testing"

	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/stretchr/testify/assert"
)

// TestShouldExitOnRegularERINRS tests the shouldExitOnRegularERINRS function
func TestShouldExitOnRegularERINRS(t *testing.T) {
	tests := []struct {
		name                string
		enableERIManagement bool
		vifFeatures         string
		expectedShouldExit  bool
	}{
		{
			name:                "Given_ERIManagementEnabled_And_ERIInterface_When_Check_Then_ReturnTrue",
			enableERIManagement: true,
			vifFeatures:         string(ccev2.ENIForERI),
			expectedShouldExit:  true,
		},
		{
			name:                "Given_ERIManagementDisabled_And_ERIInterface_When_Check_Then_ReturnFalse",
			enableERIManagement: false,
			vifFeatures:         string(ccev2.ENIForERI),
			expectedShouldExit:  false,
		},
		{
			name:                "Given_ERIManagementEnabled_And_NonERIInterface_When_Check_Then_ReturnFalse",
			enableERIManagement: true,
			vifFeatures:         "other-feature",
			expectedShouldExit:  false,
		},
		{
			name:                "Given_ERIManagementEnabled_And_EmptyVifFeatures_When_Check_Then_ReturnFalse",
			enableERIManagement: true,
			vifFeatures:         "",
			expectedShouldExit:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original config
			originalEnableERIManagement := option.Config.EnableERIManagement
			defer func() {
				option.Config.EnableERIManagement = originalEnableERIManagement
			}()

			// Set test config
			option.Config.EnableERIManagement = tt.enableERIManagement

			// Create test data
			rii := bceutils.RdmaIfInfo{
				VifFeatures: tt.vifFeatures,
			}

			// Create RdmaDiscovery instance
			rd := &RdmaDiscovery{}

			// Execute test
			result := rd.shouldExitOnRegularERINRS(rii)

			// Verify result
			assert.Equal(t, tt.expectedShouldExit, result, "Test case: %s", tt.name)
		})
	}
}

// TestShouldExitOnRegularERINRS_EdgeCases tests edge cases for shouldExitOnRegularERINRS
func TestShouldExitOnRegularERINRS_EdgeCases(t *testing.T) {
	// Save original config
	originalEnableERIManagement := option.Config.EnableERIManagement
	defer func() {
		option.Config.EnableERIManagement = originalEnableERIManagement
	}()

	option.Config.EnableERIManagement = true
	rd := &RdmaDiscovery{}

	t.Run("Given_CaseMismatchVifFeatures_When_Check_Then_ReturnFalse", func(t *testing.T) {
		// Test case sensitivity - should be exact match
		rii := bceutils.RdmaIfInfo{
			VifFeatures: "eri", // lowercase instead of "ERI"
		}
		result := rd.shouldExitOnRegularERINRS(rii)
		assert.False(t, result, "Should not exit for case mismatch")
	})
}
