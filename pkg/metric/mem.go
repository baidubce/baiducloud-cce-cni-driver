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

package metric

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric/cgroup"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

var (
	// MEMUsagePercent is the metric for cni component memory usage.
	MEMUsagePercent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "memory_usage_percent",
			Help: "current memory usage percent",
		},
		[]string{"cluster"},
	)
)

var memoryCapacityRegexp = regexp.MustCompile(`MemTotal:\s*([0-9]+) kB`)

// Absolute path to the cgroup hierarchies of this container.
// (e.g.: "cpu" -> "/sys/fs/cgroup/cpu/test")
var cgroupPaths map[string]string

// parseCapacity matches a Regexp in a []byte, returning the resulting value in bytes.
// Assumes that the value matched by the Regexp is in KB.
func parseCapacity(b []byte, r *regexp.Regexp) (uint64, error) {
	matches := r.FindSubmatch(b)
	if len(matches) != 2 {
		return 0, fmt.Errorf("failed to match regexp in output: %q", string(b))
	}
	m, err := strconv.ParseUint(string(matches[1]), 10, 64)
	if err != nil {
		return 0, err
	}

	// Convert to bytes.
	return m * 1024, err
}

// getMachineMemoryCapacity returns the machine's total memory from /proc/meminfo.
// Returns the total memory capacity as an uint64 (number of bytes).
func getMachineMemoryCapacity(ctx context.Context) (uint64, error) {
	out, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}

	memoryCapacity, err := parseCapacity(out, memoryCapacityRegexp)
	if err != nil {
		return 0, err
	}
	return memoryCapacity, err
}

func readString(ctx context.Context, dirpath string, file string) string {
	cgroupFile := path.Join(dirpath, file)

	// Read
	out, err := ioutil.ReadFile(cgroupFile)
	if err != nil {
		// Ignore non-existent files
		if !os.IsNotExist(err) {
			logger.Warningf(ctx, "readString: Failed to read %q: %v", cgroupFile, err)
		}
		return ""
	}
	return strings.TrimSpace(string(out))
}

func readUInt64(ctx context.Context, dirpath string, file string) uint64 {
	out := readString(ctx, dirpath, file)
	if out == "max" {
		return math.MaxUint64
	}
	if out == "" {
		return 0
	}

	val, err := strconv.ParseUint(out, 10, 64)
	if err != nil {
		logger.Warningf(ctx, "readUInt64: Failed to parse int %q from file %q: %v", out, path.Join(dirpath, file), err)
		return 0
	}

	return val
}

// fileExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func RunMemUsagePercentMetric(ctx context.Context) {
	var (
		hasMemory       bool
		memUsage        uint64
		memLimit        uint64
		memUsagePercent float64
		//memoryReservation uint64
		//memorySwapLimit   uint64
	)

	cgroupPaths = cgroup.MakeCgroupPaths(cgroup.CgroupSubsystems, "")
	// Get the memory usage in bytes from cgroup file.
	// set memUsageBytes from cgroup "/sys/fs/cgroup/memory/memory.usage_in_bytes"
	// or set memUsageBytes from cgroup v2 "/sys/fs/cgroup/memory.current"
	cgroup2UnifiedMode := cgroups.IsCgroup2UnifiedMode()
	// Get cgroup controller filepath for memory
	memoryRoot, ok := cgroup.GetControllerPath(cgroupPaths, "memory", cgroup2UnifiedMode)

	// Update the memory usage percent metric every second in a separate goroutine.
	for {
		hasMemory = false
		if ok {
			if cgroup2UnifiedMode {
				if fileExists(path.Join(memoryRoot, "memory.max")) {
					hasMemory = true
					memUsage = readUInt64(ctx, memoryRoot, "memory.current")
					memLimit = readUInt64(ctx, memoryRoot, "memory.max")
					//memoryReservation = readUInt64(ctx, memoryRoot, "memory.min")
					//memorySwapLimit = readUInt64(ctx, memoryRoot, "memory.swap.max")
				}
			} else {
				if fileExists(memoryRoot) {
					hasMemory = true
					memUsage = readUInt64(ctx, memoryRoot, "memory.usage_in_bytes")
					memLimit = readUInt64(ctx, memoryRoot, "memory.limit_in_bytes")
					//memoryReservation = readUInt64(ctx, memoryRoot, "memory.soft_limit_in_bytes")
					//memorySwapLimit = readUInt64(ctx, memoryRoot, "memory.memsw.limit_in_bytes")
				}
			}
		}

		// Calculate the memory usage percentage.
		memUsagePercent = 0
		// hasMemory == true if the pod set memory limit.
		if hasMemory && memLimit > 0 {
			memUsagePercent = float64(memUsage) / float64(memLimit) * 100
		} else {
			machineMemoryCapacity, err := getMachineMemoryCapacity(ctx)
			if err != nil {
				logger.Warningf(ctx, "failed to get machine memory capacity. Error: %v", err)
			} else if machineMemoryCapacity == 0 { // no value of type uint64 is less than 0
				logger.Warningf(ctx, "failed to get machine memory capacity. Machine memory capacity is negative. machineMemoryCapacity=%d", machineMemoryCapacity)
			} else {
				memUsagePercent = float64(memUsage) / float64(machineMemoryCapacity) * 100
			}
		}
		// Set the metric to the value of memUsagePercent.
		MEMUsagePercent.WithLabelValues(MetaInfo.ClusterID).Set(memUsagePercent)

		// Sleep for a second.
		time.Sleep(time.Second)
	}
}
