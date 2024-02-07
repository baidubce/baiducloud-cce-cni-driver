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

package pidfile

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/checker"
)

const (
	path = "/tmp/cce-test-pidfile"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	TestingT(t)
}

type PidfileTestSuite struct{}

var _ = Suite(&PidfileTestSuite{})

func (s *PidfileTestSuite) TestWrite(c *C) {
	err := Write(path)
	c.Assert(err, IsNil)
	defer Remove(path)

	content, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(content, checker.DeepEquals, []byte(fmt.Sprintf("%d\n", os.Getpid())))
}

func (s *PidfileTestSuite) TestKill(c *C) {
	cmd := exec.Command("sleep", "inf")
	err := cmd.Start()
	c.Assert(err, IsNil)

	err = write(path, cmd.Process.Pid)
	c.Assert(err, IsNil)
	defer Remove(path)

	pid, err := Kill(path)
	c.Assert(err, IsNil)
	c.Assert(pid, Not(Equals), 0)

	err = cmd.Wait()
	c.Assert(err, ErrorMatches, "signal: killed")
}

func (s *PidfileTestSuite) TestKillAlreadyFinished(c *C) {
	cmd := exec.Command("sleep", "0")
	err := cmd.Start()
	c.Assert(err, IsNil)

	err = write(path, cmd.Process.Pid)
	c.Assert(err, IsNil)
	defer Remove(path)

	err = cmd.Wait()
	c.Assert(err, IsNil)

	pid, err := Kill(path)
	c.Assert(err, IsNil)
	c.Assert(pid, Equals, 0)
}

func (s *PidfileTestSuite) TestKillPidfileNotExist(c *C) {
	_, err := Kill("/tmp/cce-foo-bar-some-not-existing-file")
	c.Assert(err, IsNil)
}

func (s *PidfileTestSuite) TestKillPidfilePermissionDenied(c *C) {
	err := os.WriteFile(path, []byte("foobar\n"), 0000)
	c.Assert(err, IsNil)
	defer Remove(path)

	_, err = Kill(path)
	c.Assert(err, ErrorMatches, ".* permission denied")
}

func (s *PidfileTestSuite) TestKillFailedParsePid(c *C) {
	err := os.WriteFile(path, []byte("foobar\n"), 0644)
	c.Assert(err, IsNil)
	defer Remove(path)

	_, err = Kill(path)
	c.Assert(err, ErrorMatches, "failed to parse pid .*")
}
