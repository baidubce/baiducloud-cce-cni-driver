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

package set

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	TestingT(t)
}

type SetTestSuite struct{}

var _ = Suite(&SetTestSuite{})

func (s *SetTestSuite) TestSliceSubsetOf(c *C) {
	testCases := []struct {
		sub          []string
		main         []string
		isSubset     bool
		expectedDiff []string
	}{
		{
			sub:          []string{"foo", "bar"},
			main:         []string{"foo", "bar", "baz"},
			isSubset:     true,
			expectedDiff: nil,
		},
		{
			sub:          []string{"foo", "bar"},
			main:         []string{"foo", "bar"},
			isSubset:     true,
			expectedDiff: nil,
		},
		{
			sub:          []string{"foo", "bar"},
			main:         []string{"foo", "baz"},
			isSubset:     false,
			expectedDiff: []string{"bar"},
		},
		{
			sub:          []string{"baz"},
			main:         []string{"foo", "bar"},
			isSubset:     false,
			expectedDiff: []string{"baz"},
		},
		{
			sub:          []string{"foo", "bar", "fizz"},
			main:         []string{"fizz", "buzz"},
			isSubset:     false,
			expectedDiff: []string{"foo", "bar"},
		},
		{
			sub:          []string{"foo", "foo", "bar"},
			main:         []string{"foo", "bar"},
			isSubset:     false,
			expectedDiff: nil,
		},
		{
			sub:          []string{"foo", "foo", "foo", "bar", "bar"},
			main:         []string{"foo", "foo", "bar"},
			isSubset:     false,
			expectedDiff: nil,
		},
	}
	for _, tc := range testCases {
		isSubset, diff := SliceSubsetOf(tc.sub, tc.main)
		c.Assert(isSubset, Equals, tc.isSubset)
		c.Assert(diff, checker.DeepEquals, tc.expectedDiff)
	}
}
