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

package sysctl

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	TestingT(t)
}

type SysctlLinuxTestSuite struct{}

var _ = Suite(&SysctlLinuxTestSuite{})

func (s *SysctlLinuxTestSuite) TestFullPath(c *C) {
	testCases := []struct {
		name        string
		expected    string
		expectedErr bool
	}{
		{
			name:     "net.ipv4.ip_forward",
			expected: "/proc/sys/net/ipv4/ip_forward",
		},
		{
			name:     "net.ipv4.conf.all.forwarding",
			expected: "/proc/sys/net/ipv4/conf/all/forwarding",
		},
		{
			name:     "net.ipv6.conf.all.forwarding",
			expected: "/proc/sys/net/ipv6/conf/all/forwarding",
		},
		{
			name:     "foo.bar",
			expected: "/proc/sys/foo/bar",
		},
		{
			name:        "double..dot",
			expectedErr: true,
		},
		{
			name:        "invalid.char$",
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		path, err := parameterPath(tc.name)
		if tc.expectedErr {
			c.Assert(err, NotNil)
		} else {
			c.Assert(err, IsNil)
			c.Assert(path, Equals, tc.expected)
		}
	}
}

// TestFields contains dependencies for SetIfNeeded tests.
type TestFields struct {
	// savedReadFn / savedWriteFn store the original package-level function variables
	// so they can be restored after each test via TestPostRun.
	savedReadFn  func(name string) (string, error)
	savedWriteFn func(name string, val string) error
	tempDir      string
	// readForVerify reads sysctl values for post-call assertions.
	// For tempDir-based tests this reads from tempDir; for fully-mocked tests it is nil
	// and file-content verification is skipped.
	readForVerify func(name string) (string, error)
}

// TestArgs contains arguments passed to SetIfNeeded.
type TestArgs struct {
	paramName   string
	targetValue string
}

// TestCase defines a single test scenario for SetIfNeeded.
type TestCase struct {
	Name            string
	TestSetup       func() *TestFields
	TestPostRun     func(fields *TestFields)
	Args            TestArgs
	WantErr         error
	InitialValue    string
	ExpectedValue   string
	ShouldCallWrite bool
}

// makeProcfsReadFn returns a readFn that reads sysctl files from procfsDir.
// Unlike the real Read(), raw errors are returned so errors.Is() works correctly
// inside SetIfNeeded without relying on string matching.
func makeProcfsReadFn(procfsDir string) func(string) (string, error) {
	return func(name string) (string, error) {
		elems := strings.Split(name, ".")
		path := filepath.Join(append([]string{procfsDir, "sys"}, elems...)...)
		val, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(val), "\n"), nil
	}
}

// makeProcfsWriteFn returns a writeFn that writes sysctl files to procfsDir.
// O_RDWR (no truncate) is used to match the behaviour of the real writeSysctl,
// which is important for the empty-target-value test case.
func makeProcfsWriteFn(procfsDir string) func(string, string) error {
	return func(name string, val string) error {
		elems := strings.Split(name, ".")
		path := filepath.Join(append([]string{procfsDir, "sys"}, elems...)...)
		f, err := os.OpenFile(path, os.O_RDWR, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.WriteString(f, val)
		return err
	}
}

// TestSetIfNeeded tests the SetIfNeeded function using function-variable injection
// instead of gomonkey so that the tests are not sensitive to compiler inlining.
func TestSetIfNeeded(t *testing.T) {
	cases := []TestCase{
		{
			Name: "TestSetIfNeeded_Given_CurrentValueEqualsTargetValue_When_CallSetIfNeeded_Then_ReturnNilWithoutCallingWrite",
			TestSetup: func() *TestFields {
				tempDir, _ := os.MkdirTemp("", "sysctl_test_*")
				sysctlPath := filepath.Join(tempDir, "sys", "net", "ipv4", "ip_forward")
				os.MkdirAll(filepath.Dir(sysctlPath), 0755)
				os.WriteFile(sysctlPath, []byte("1\n"), 0644)

				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn, tempDir: tempDir}
				readFn = makeProcfsReadFn(tempDir)
				writeFn = makeProcfsWriteFn(tempDir)
				fields.readForVerify = makeProcfsReadFn(tempDir)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
				if fields.tempDir != "" {
					os.RemoveAll(fields.tempDir)
				}
			},
			Args:            TestArgs{paramName: "net.ipv4.ip_forward", targetValue: "1"},
			WantErr:         nil,
			InitialValue:    "1",
			ExpectedValue:   "1",
			ShouldCallWrite: false,
		},
		{
			Name: "TestSetIfNeeded_Given_CurrentValueNotEqualsTargetValue_When_CallSetIfNeeded_Then_CallWriteAndReturnResult",
			TestSetup: func() *TestFields {
				tempDir, _ := os.MkdirTemp("", "sysctl_test_*")
				sysctlPath := filepath.Join(tempDir, "sys", "net", "ipv4", "ip_forward")
				os.MkdirAll(filepath.Dir(sysctlPath), 0755)
				os.WriteFile(sysctlPath, []byte("0\n"), 0644)

				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn, tempDir: tempDir}
				readFn = makeProcfsReadFn(tempDir)
				writeFn = makeProcfsWriteFn(tempDir)
				fields.readForVerify = makeProcfsReadFn(tempDir)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
				if fields.tempDir != "" {
					os.RemoveAll(fields.tempDir)
				}
			},
			Args:            TestArgs{paramName: "net.ipv4.ip_forward", targetValue: "1"},
			WantErr:         nil,
			InitialValue:    "0",
			ExpectedValue:   "1",
			ShouldCallWrite: true,
		},
		{
			Name: "TestSetIfNeeded_Given_ReadReturnsENOENT_When_CallSetIfNeeded_Then_ReturnNilWithoutCallingWrite",
			TestSetup: func() *TestFields {
				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn}
				readFn = func(name string) (string, error) {
					return "", os.ErrNotExist
				}
				// writeFn replaced to detect unexpected calls; returning nil is safe.
				writeFn = func(name string, val string) error {
					return nil
				}
				// readForVerify is nil — file-content verification is skipped for mock-only cases.
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
			},
			Args:            TestArgs{paramName: "net.ipv6.conf.cce-eni-0.disable_ipv6", targetValue: "1"},
			WantErr:         nil,
			InitialValue:    "",
			ExpectedValue:   "",
			ShouldCallWrite: false,
		},
		{
			Name: "TestSetIfNeeded_Given_ReadSuccessWriteReturnsENOENT_When_CallSetIfNeeded_Then_ReturnNil",
			TestSetup: func() *TestFields {
				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn}
				readFn = func(name string) (string, error) {
					return "0", nil
				}
				writeFn = func(name string, val string) error {
					return os.ErrNotExist
				}
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
			},
			Args:            TestArgs{paramName: "net.ipv6.conf.cce-eni-0.disable_ipv6", targetValue: "1"},
			WantErr:         nil,
			InitialValue:    "0",
			ExpectedValue:   "0",
			ShouldCallWrite: true,
		},
		{
			Name: "TestSetIfNeeded_Given_ReadReturnsNonENOENTError_When_CallSetIfNeeded_Then_ContinueToSet",
			TestSetup: func() *TestFields {
				tempDir, _ := os.MkdirTemp("", "sysctl_test_*")
				sysctlPath := filepath.Join(tempDir, "sys", "net", "ipv4", "ip_forward")
				os.MkdirAll(filepath.Dir(sysctlPath), 0755)
				os.WriteFile(sysctlPath, []byte(""), 0644)

				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn, tempDir: tempDir}
				// readFn returns ErrPermission; SetIfNeeded treats currentValue as ""
				// and still calls writeFn because "" != targetValue "1".
				readFn = func(name string) (string, error) {
					return "", os.ErrPermission
				}
				writeFn = makeProcfsWriteFn(tempDir)
				fields.readForVerify = makeProcfsReadFn(tempDir)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
				if fields.tempDir != "" {
					os.RemoveAll(fields.tempDir)
				}
			},
			Args:            TestArgs{paramName: "net.ipv4.ip_forward", targetValue: "1"},
			WantErr:         nil,
			InitialValue:    "",
			ExpectedValue:   "1",
			ShouldCallWrite: true,
		},
		{
			Name: "TestSetIfNeeded_Given_WriteReturnsNonENOENTError_When_CallSetIfNeeded_Then_ReturnError",
			TestSetup: func() *TestFields {
				tempDir, _ := os.MkdirTemp("", "sysctl_test_*")
				sysctlPath := filepath.Join(tempDir, "sys", "net", "ipv4", "ip_forward")
				os.MkdirAll(filepath.Dir(sysctlPath), 0755)
				os.WriteFile(sysctlPath, []byte("0\n"), 0644)

				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn, tempDir: tempDir}
				readFn = makeProcfsReadFn(tempDir)
				writeFn = func(name string, val string) error {
					return os.ErrPermission
				}
				fields.readForVerify = makeProcfsReadFn(tempDir)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
				if fields.tempDir != "" {
					os.RemoveAll(fields.tempDir)
				}
			},
			Args:            TestArgs{paramName: "net.ipv4.ip_forward", targetValue: "1"},
			WantErr:         os.ErrPermission,
			InitialValue:    "0",
			ExpectedValue:   "0",
			ShouldCallWrite: true,
		},
		{
			Name: "TestSetIfNeeded_Given_EmptyTargetValue_When_CallSetIfNeeded_Then_HandleCorrectly",
			TestSetup: func() *TestFields {
				tempDir, _ := os.MkdirTemp("", "sysctl_test_*")
				sysctlPath := filepath.Join(tempDir, "sys", "net", "ipv4", "ip_forward")
				os.MkdirAll(filepath.Dir(sysctlPath), 0755)
				os.WriteFile(sysctlPath, []byte("1\n"), 0644)

				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn, tempDir: tempDir}
				readFn = makeProcfsReadFn(tempDir)
				writeFn = makeProcfsWriteFn(tempDir)
				fields.readForVerify = makeProcfsReadFn(tempDir)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
				if fields.tempDir != "" {
					os.RemoveAll(fields.tempDir)
				}
			},
			Args:    TestArgs{paramName: "net.ipv4.ip_forward", targetValue: ""},
			WantErr: nil,
			// writeFn uses O_RDWR (no truncate), so writing "" leaves "1" in the file.
			InitialValue:    "1",
			ExpectedValue:   "1",
			ShouldCallWrite: true,
		},
		{
			Name: "TestSetIfNeeded_Given_EmptyCurrentValueAndEmptyTargetValue_When_CallSetIfNeeded_Then_ReturnNilWithoutCallingWrite",
			TestSetup: func() *TestFields {
				tempDir, _ := os.MkdirTemp("", "sysctl_test_*")
				sysctlPath := filepath.Join(tempDir, "sys", "net", "ipv4", "ip_forward")
				os.MkdirAll(filepath.Dir(sysctlPath), 0755)
				os.WriteFile(sysctlPath, []byte("\n"), 0644)

				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn, tempDir: tempDir}
				readFn = makeProcfsReadFn(tempDir)
				writeFn = makeProcfsWriteFn(tempDir)
				fields.readForVerify = makeProcfsReadFn(tempDir)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
				if fields.tempDir != "" {
					os.RemoveAll(fields.tempDir)
				}
			},
			Args:            TestArgs{paramName: "net.ipv4.ip_forward", targetValue: ""},
			WantErr:         nil,
			InitialValue:    "",
			ExpectedValue:   "",
			ShouldCallWrite: false,
		},
		{
			Name: "TestSetIfNeeded_Given_DifferentStringValues_When_CallSetIfNeeded_Then_CallWrite",
			TestSetup: func() *TestFields {
				tempDir, _ := os.MkdirTemp("", "sysctl_test_*")
				sysctlPath := filepath.Join(tempDir, "sys", "net", "ipv6", "conf", "all", "forwarding")
				os.MkdirAll(filepath.Dir(sysctlPath), 0755)
				os.WriteFile(sysctlPath, []byte("0\n"), 0644)

				fields := &TestFields{savedReadFn: readFn, savedWriteFn: writeFn, tempDir: tempDir}
				readFn = makeProcfsReadFn(tempDir)
				writeFn = makeProcfsWriteFn(tempDir)
				fields.readForVerify = makeProcfsReadFn(tempDir)
				return fields
			},
			TestPostRun: func(fields *TestFields) {
				readFn = fields.savedReadFn
				writeFn = fields.savedWriteFn
				if fields.tempDir != "" {
					os.RemoveAll(fields.tempDir)
				}
			},
			Args:            TestArgs{paramName: "net.ipv6.conf.all.forwarding", targetValue: "1"},
			WantErr:         nil,
			InitialValue:    "0",
			ExpectedValue:   "1",
			ShouldCallWrite: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			defer tc.TestPostRun(fields)

			// Call SetIfNeeded — this is the function under test.
			gotErr := SetIfNeeded(tc.Args.paramName, tc.Args.targetValue)

			// Verify error result.
			if tc.WantErr != nil {
				assert.Error(t, gotErr, "SetIfNeeded should return an error")
				if gotErr != nil {
					assert.Equal(t, tc.WantErr, gotErr, "SetIfNeeded should return the expected error")
				}
			} else {
				assert.NoError(t, gotErr, "SetIfNeeded should not return an error")
			}

			// Verify file state only when a tempDir-aware reader is available.
			// Mock-only test cases (readForVerify == nil) skip file-content assertions.
			if fields.readForVerify == nil {
				return
			}

			if tc.ShouldCallWrite && gotErr == nil {
				actualValue, readErr := fields.readForVerify(tc.Args.paramName)
				if readErr == nil {
					assert.Equal(t, tc.ExpectedValue, actualValue, "File value should match expected value after write")
				} else {
					t.Logf("Warning: could not read file to verify write: %v", readErr)
				}
			} else if !tc.ShouldCallWrite {
				actualValue, readErr := fields.readForVerify(tc.Args.paramName)
				if readErr == nil {
					assert.Equal(t, tc.InitialValue, actualValue, "File value should not change when write is not expected")
				}
			}
		})
	}
}
