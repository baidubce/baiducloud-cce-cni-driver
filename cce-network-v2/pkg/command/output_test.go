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

package command

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type CMDHelpersSuite struct{}

var _ = Suite(&CMDHelpersSuite{})

func (s *CMDHelpersSuite) TestDumpJSON(c *C) {
	type sampleData struct {
		ID   int
		Name string
	}

	tt := sampleData{
		ID:   1,
		Name: "test",
	}

	err := dumpJSON(tt, "")
	c.Assert(err, IsNil)

	err = dumpJSON(tt, "{.Id}")
	c.Assert(err, IsNil)

	err = dumpJSON(tt, "{{.Id}}")
	if err == nil {
		c.Fatalf("Dumpjson jsonpath no error with invalid path '%s'", err)
	}
}

func (s *CMDHelpersSuite) TestDumpYAML(c *C) {
	type sampleData struct {
		ID   int
		Name string
	}

	tt := sampleData{
		ID:   1,
		Name: "test",
	}

	err := dumpYAML(tt)
	c.Assert(err, IsNil)
}
