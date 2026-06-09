//go:build !privileged_tests

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

package common

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/stretchr/testify/assert"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	check.TestingT(t)
}

type CommonSuite struct{}

var _ = check.Suite(&CommonSuite{})

func (s *CommonSuite) TestC2GoArray(c *check.C) {
	c.Assert(C2GoArray("0x0, 0x1, 0x2, 0x3"), checker.DeepEquals, []byte{0, 0x01, 0x02, 0x03})
	c.Assert(C2GoArray("0x0, 0xff, 0xff, 0xff"), checker.DeepEquals, []byte{0, 0xFF, 0xFF, 0xFF})
	c.Assert(C2GoArray("0xa, 0xbc, 0xde, 0xf1"), checker.DeepEquals, []byte{0xa, 0xbc, 0xde, 0xf1})
	c.Assert(C2GoArray("0x0"), checker.DeepEquals, []byte{0})
	c.Assert(C2GoArray(""), checker.DeepEquals, []byte{})
}

func (s *CommonSuite) TestGoArray2C(c *check.C) {
	tests := []struct {
		input  []byte
		output string
	}{
		{
			input:  []byte{0, 0x01, 0x02, 0x03},
			output: "0x0, 0x1, 0x2, 0x3",
		},
		{
			input:  []byte{0, 0xFF, 0xFF, 0xFF},
			output: "0x0, 0xff, 0xff, 0xff",
		},
		{
			input:  []byte{0xa, 0xbc, 0xde, 0xf1},
			output: "0xa, 0xbc, 0xde, 0xf1",
		},
		{
			input:  []byte{0},
			output: "0x0",
		},
		{
			input:  []byte{},
			output: "",
		},
	}

	for _, test := range tests {
		c.Assert(GoArray2C(test.input), check.Equals, test.output)
	}
}

func (s *CommonSuite) TestGetNumPossibleCPUsFromReader(c *check.C) {
	log := logging.DefaultLogger.WithField(logfields.LogSubsys, "utils-test")
	tests := []struct {
		in       string
		expected int
	}{
		{"0", 1},
		{"0-7", 8},
		{"0,2-3", 3},
		{"", 0},
		{"foobar", 0},
	}

	for _, t := range tests {
		possibleCpus := getNumPossibleCPUsFromReader(log, strings.NewReader(t.in))
		c.Assert(possibleCpus, check.Equals, t.expected)
	}

}

// TestVethNameForPod tests the VethNameForPod function
func TestVethNameForPod(t *testing.T) {
	type TestFields struct {
		// No fields needed for this test
	}

	type TestArgs struct {
		name      string
		namespace string
		prefix    string
	}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields, result string)
		Args        TestArgs
		WantResult  string
	}

	cases := []TestCase{
		{
			Name: "TestVethNameForPod_GivenNormalPod_WhenCalled_ThenReturnCorrectVethName",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify result length (prefix + 11 hex chars)
				assert.Equal(t, 14, len(result), "Result should be prefix length + 11 hex chars")
				// Verify result contains only valid hex characters after prefix
				hexPart := result[len("lxc"):]
				for _, r := range hexPart {
					assert.True(t, (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'), "Hex part should contain only hex characters")
				}
			},
			Args: TestArgs{
				name:      "test-pod",
				namespace: "default",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("default.test-pod"))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenEmptyPrefix_WhenCalled_ThenReturnOnlyHexPart",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result length is exactly 11 hex chars
				assert.Equal(t, 11, len(result), "Result should be exactly 11 hex chars when prefix is empty")
				// Verify result contains only valid hex characters
				for _, r := range result {
					assert.True(t, (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'), "Result should contain only hex characters")
				}
			},
			Args: TestArgs{
				name:      "test-pod",
				namespace: "default",
				prefix:    "",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("default.test-pod"))
				return hex.EncodeToString(h.Sum(nil))[:11]
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenLongPrefix_WhenCalled_ThenReturnCorrectVethName",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "veth"), "Result should start with prefix")
				// Verify result length (prefix + 11 hex chars)
				assert.Equal(t, 15, len(result), "Result should be prefix length + 11 hex chars")
			},
			Args: TestArgs{
				name:      "test-pod",
				namespace: "default",
				prefix:    "veth",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("default.test-pod"))
				return fmt.Sprintf("veth%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenEmptyName_WhenCalled_ThenReturnCorrectVethName",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify result length
				assert.Equal(t, 14, len(result), "Result should be prefix length + 11 hex chars")
			},
			Args: TestArgs{
				name:      "",
				namespace: "default",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("default."))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenEmptyNamespace_WhenCalled_ThenReturnCorrectVethName",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify result length
				assert.Equal(t, 14, len(result), "Result should be prefix length + 11 hex chars")
			},
			Args: TestArgs{
				name:      "test-pod",
				namespace: "",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte(".test-pod"))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenBothEmpty_WhenCalled_ThenReturnCorrectVethName",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify result length
				assert.Equal(t, 14, len(result), "Result should be prefix length + 11 hex chars")
			},
			Args: TestArgs{
				name:      "",
				namespace: "",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("."))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenLongNameAndNamespace_WhenCalled_ThenReturnCorrectVethName",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify result length (prefix + 11 hex chars)
				assert.Equal(t, 14, len(result), "Result should be prefix length + 11 hex chars")
				// Verify result is deterministic (same input produces same output)
				result2 := VethNameForPod("very-long-pod-name-with-many-characters", "very-long-namespace-name-with-many-characters", "lxc")
				assert.Equal(t, result, result2, "Same input should produce same output")
			},
			Args: TestArgs{
				name:      "very-long-pod-name-with-many-characters",
				namespace: "very-long-namespace-name-with-many-characters",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("very-long-namespace-name-with-many-characters.very-long-pod-name-with-many-characters"))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenSpecialCharacters_WhenCalled_ThenReturnCorrectVethName",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify result length
				assert.Equal(t, 14, len(result), "Result should be prefix length + 11 hex chars")
			},
			Args: TestArgs{
				name:      "pod-with-special-chars-123",
				namespace: "namespace-with-special-chars-456",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("namespace-with-special-chars-456.pod-with-special-chars-123"))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenDifferentPods_WhenCalled_ThenReturnDifferentVethNames",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify different pods produce different names
				result2 := VethNameForPod("pod-2", "default", "lxc")
				assert.NotEqual(t, result, result2, "Different pods should produce different veth names")
			},
			Args: TestArgs{
				name:      "pod-1",
				namespace: "default",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("default.pod-1"))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
		{
			Name: "TestVethNameForPod_GivenSamePodDifferentNamespace_WhenCalled_ThenReturnDifferentVethNames",
			TestSetup: func() *TestFields {
				return &TestFields{}
			},
			TestPostRun: func(fields *TestFields, result string) {
				// Verify result starts with prefix
				assert.True(t, strings.HasPrefix(result, "lxc"), "Result should start with prefix")
				// Verify same pod in different namespace produces different names
				result2 := VethNameForPod("test-pod", "kube-system", "lxc")
				assert.NotEqual(t, result, result2, "Same pod in different namespace should produce different veth names")
			},
			Args: TestArgs{
				name:      "test-pod",
				namespace: "default",
				prefix:    "lxc",
			},
			WantResult: func() string {
				h := sha1.New()
				h.Write([]byte("default.test-pod"))
				return fmt.Sprintf("lxc%s", hex.EncodeToString(h.Sum(nil))[:11])
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			gotResult := VethNameForPod(tc.Args.name, tc.Args.namespace, tc.Args.prefix)
			if tc.WantResult != "" {
				assert.Equal(t, tc.WantResult, gotResult, "Result should match expected value")
			}
			if tc.TestPostRun != nil {
				tc.TestPostRun(fields, gotResult)
			}
		})
	}
}
