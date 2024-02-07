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

package api

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/sirupsen/logrus"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

var log = logging.DefaultLogger.WithField(logfields.LogSubsys, "api")

// getGroupIDByName returns the group ID for the given grpName.
func getGroupIDByName(grpName string) (int, error) {
	group, err := user.LookupGroup(grpName)
	if err != nil {
		return -1, err
	}
	return strconv.Atoi(group.Gid)
}

// SetDefaultPermissions sets the given socket's group to `CCEGroupName` and
// mode to `SocketFileMode`.
func SetDefaultPermissions(socketPath string) error {
	gid, err := getGroupIDByName(CCEGroupName)
	if err != nil {
		log.WithError(err).WithFields(logrus.Fields{
			logfields.Path: socketPath,
			"group":        CCEGroupName,
		}).Debug("Group not found")
	} else {
		if err := os.Chown(socketPath, 0, gid); err != nil {
			return fmt.Errorf("failed while setting up %s's group ID"+
				" in %q: %s", CCEGroupName, socketPath, err)
		}
	}
	if err := os.Chmod(socketPath, SocketFileMode); err != nil {
		return fmt.Errorf("failed while setting up file permissions in %q: %w",
			socketPath, err)
	}
	return nil
}
