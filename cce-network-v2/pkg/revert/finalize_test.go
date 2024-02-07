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

package revert

import (
	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
)

func (s *RevertTestSuite) TestFinalizeList(c *C) {
	expectedContent := []string{"foo", "bar", "ayy", "lmao"}
	content := make([]string, 0, 4)
	fList := FinalizeList{}

	fList.Append(func() {
		content = append(content, "foo")
	})
	fList.Append(func() {
		content = append(content, "bar")
	})
	fList.Append(func() {
		content = append(content, "ayy")
	})
	fList.Append(func() {
		content = append(content, "lmao")
	})
	fList.Finalize()

	c.Assert(content, checker.DeepEquals, expectedContent)
}
