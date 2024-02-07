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

package netns

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/containernetworking/plugins/pkg/ns"
)

func netNSPath(name string) string {
	return filepath.Join(netNSRootDir(), name)
}

func netNSRootDir() string {
	return filepath.Join("/", "var", "run", "netns")
}

// Returns an object representing the namespace referred to by @path
func GetContainerNS(nspath string) (ns.NetNS, error) {
	path, err := GetProcNSPath(nspath)
	if err != nil {
		return nil, err
	}
	return ns.GetNS(path)
}

func GetProcNSPath(nspath string) (string, error) {
	var (
		err error
	)

	targetStat, err := getStat(nspath)
	if err != nil {
		return "", fmt.Errorf("failed to Statfs %q: %v", nspath, err)
	}

	// scan /proc for network namespace inode
	files, err := os.ReadDir("/proc")
	if err != nil {
		return "", fmt.Errorf("failed to read /proc: %v", err)
	}
	for _, file := range files {
		// ignored non-directory files
		if !file.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(file.Name()); err != nil {
			continue
		}

		// get /proc/<pid>/ns/net inode
		netnsPath := filepath.Join("/proc", file.Name(), "ns/net")
		stat, err := getStat(netnsPath)
		if err != nil {
			continue
		}
		if stat.Dev == targetStat.Dev && stat.Ino == targetStat.Ino {
			return netnsPath, nil
		}
	}
	return nspath, fmt.Errorf("failed to find proc network namespace path from %q", nspath)
}

func getStat(path string) (*syscall.Stat_t, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var stat syscall.Stat_t
	err = syscall.Fstat(int(file.Fd()), &stat)
	if err != nil {
		return nil, err
	}

	return &stat, nil
}

func WithContainerNetns(nspath string, toRun func(ns.NetNS) error) error {
	path, err := GetProcNSPath(nspath)
	if err != nil {
		return err
	}
	return ns.WithNetNSPath(path, toRun)
}
