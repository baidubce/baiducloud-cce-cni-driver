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

package netlink

import (
	"syscall"

	"github.com/vishvananda/netlink"
)

// IsNotExistError returns true if the error type is syscall.ENOENT or syscall.ESRCH
func IsNotExistError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT || errno == syscall.ESRCH
	}
	return false
}

// IsExistsError returns true if the error type is syscall.EEXIST
func IsExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EEXIST
	}
	return false
}

// IsLinkNotFound returns true if error is LinkNotFoundError
func IsLinkNotFound(err error) bool {
	if _, ok := err.(netlink.LinkNotFoundError); ok {
		return true
	}
	return false
}
