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

package cgroup

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/opencontainers/runc/libcontainer/cgroups"
	fs "github.com/opencontainers/runc/libcontainer/cgroups/fs"
	fs2 "github.com/opencontainers/runc/libcontainer/cgroups/fs2"
	configs "github.com/opencontainers/runc/libcontainer/configs"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

// MetricKind represents the kind of metrics that cAdvisor exposes.
type MetricKind string

const (
	CPUUsageMetrics                MetricKind = "cpu"
	ProcessSchedulerMetrics        MetricKind = "sched"
	PerCPUUsageMetrics             MetricKind = "percpu"
	MemoryUsageMetrics             MetricKind = "memory"
	MemoryNumaMetrics              MetricKind = "memory_numa"
	CPULoadMetrics                 MetricKind = "cpuLoad"
	DiskIOMetrics                  MetricKind = "diskIO"
	DiskUsageMetrics               MetricKind = "disk"
	NetworkUsageMetrics            MetricKind = "network"
	NetworkTCPUsageMetrics         MetricKind = "tcp"
	NetworkAdvancedTCPUsageMetrics MetricKind = "advtcp"
	NetworkUDPUsageMetrics         MetricKind = "udp"
	AppMetrics                     MetricKind = "app"
	ProcessMetrics                 MetricKind = "process"
	HugetlbUsageMetrics            MetricKind = "hugetlb"
	PerfMetrics                    MetricKind = "perf_event"
	ReferencedMemoryMetrics        MetricKind = "referenced_memory"
	CPUTopologyMetrics             MetricKind = "cpu_topology"
	ResctrlMetrics                 MetricKind = "resctrl"
	CPUSetMetrics                  MetricKind = "cpuset"
	OOMMetrics                     MetricKind = "oom_event"
)

var (
	// allMetrics represents all kinds of metrics that cAdvisor supported.
	allMetrics = MetricSet{
		CPUUsageMetrics:                struct{}{},
		ProcessSchedulerMetrics:        struct{}{},
		PerCPUUsageMetrics:             struct{}{},
		MemoryUsageMetrics:             struct{}{},
		MemoryNumaMetrics:              struct{}{},
		CPULoadMetrics:                 struct{}{},
		DiskIOMetrics:                  struct{}{},
		DiskUsageMetrics:               struct{}{},
		NetworkUsageMetrics:            struct{}{},
		NetworkTCPUsageMetrics:         struct{}{},
		NetworkAdvancedTCPUsageMetrics: struct{}{},
		NetworkUDPUsageMetrics:         struct{}{},
		ProcessMetrics:                 struct{}{},
		AppMetrics:                     struct{}{},
		HugetlbUsageMetrics:            struct{}{},
		PerfMetrics:                    struct{}{},
		ReferencedMemoryMetrics:        struct{}{},
		CPUTopologyMetrics:             struct{}{},
		ResctrlMetrics:                 struct{}{},
		CPUSetMetrics:                  struct{}{},
		OOMMetrics:                     struct{}{},
	}

	// Metrics to be ignored.
	// Tcp metrics are ignored by default.
	ignoreMetrics = MetricSet{
		MemoryNumaMetrics:              struct{}{},
		NetworkTCPUsageMetrics:         struct{}{},
		NetworkUDPUsageMetrics:         struct{}{},
		NetworkAdvancedTCPUsageMetrics: struct{}{},
		ProcessSchedulerMetrics:        struct{}{},
		ProcessMetrics:                 struct{}{},
		HugetlbUsageMetrics:            struct{}{},
		ReferencedMemoryMetrics:        struct{}{},
		CPUTopologyMetrics:             struct{}{},
		ResctrlMetrics:                 struct{}{},
		CPUSetMetrics:                  struct{}{},
	}

	// Metrics to be enabled.  Used only if non-empty.
	enableMetrics = MetricSet{}

	// A map of cgroup subsystems we support listing (should be the minimal set
	// we need stats from) to a respective MetricKind.
	supportedSubsystems = map[string]MetricKind{
		"cpu":        CPUUsageMetrics,
		"cpuacct":    CPUUsageMetrics,
		"memory":     MemoryUsageMetrics,
		"hugetlb":    HugetlbUsageMetrics,
		"pids":       ProcessMetrics,
		"cpuset":     CPUSetMetrics,
		"blkio":      DiskIOMetrics,
		"io":         DiskIOMetrics,
		"devices":    "",
		"perf_event": PerfMetrics,
	}
)

var CgroupSubsystems map[string]string

func init() {
	var err error
	var includedMetrics MetricSet

	ctx := logger.NewContext()
	if len(enableMetrics) > 0 {
		includedMetrics = enableMetrics
	} else {
		includedMetrics = allMetrics.Difference(ignoreMetrics)
	}
	logger.Infof(ctx, "enabled metrics: %s", includedMetrics.String())

	CgroupSubsystems, err = getCgroupSubsystems(ctx, includedMetrics)
	if err != nil {
		logger.Errorf(ctx, "failed to get cgroup subsystems, error: %v", err)
	} else {
		logger.Infof(ctx, "found %v cgroup subsystems: %v", len(CgroupSubsystems), CgroupSubsystems)
	}
}

func (mk MetricKind) String() string {
	return string(mk)
}

type MetricSet map[MetricKind]struct{}

func (ms MetricSet) Has(mk MetricKind) bool {
	_, exists := ms[mk]
	return exists
}

func (ms MetricSet) add(mk MetricKind) {
	ms[mk] = struct{}{}
}

func (ms MetricSet) String() string {
	values := make([]string, 0, len(ms))
	for metric := range ms {
		values = append(values, string(metric))
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

// Not thread-safe, exported only for https://pkg.go.dev/flag#Value
func (ms *MetricSet) Set(value string) error {
	*ms = MetricSet{}
	if value == "" {
		return nil
	}
	for _, metric := range strings.Split(value, ",") {
		if allMetrics.Has(MetricKind(metric)) {
			(*ms).add(MetricKind(metric))
		} else {
			return fmt.Errorf("unsupported metric %q specified", metric)
		}
	}
	return nil
}

func (ms MetricSet) Difference(ms1 MetricSet) MetricSet {
	result := MetricSet{}
	for kind := range ms {
		if !ms1.Has(kind) {
			result.add(kind)
		}
	}
	return result
}

func (ms MetricSet) Append(ms1 MetricSet) MetricSet {
	result := ms
	for kind := range ms1 {
		if !ms.Has(kind) {
			result.add(kind)
		}
	}
	return result
}

// Check if this cgroup subsystem/controller is of use.
func needSubsys(name string, metrics MetricSet) bool {
	// Check if supported.
	metric, supported := supportedSubsystems[name]
	if !supported {
		return false
	}
	// Check if needed.
	if metrics == nil || metric == "" {
		return true
	}

	return metrics.Has(metric)
}

func getCgroupSubsystemsHelper(ctx context.Context, allCgroups []cgroups.Mount, includedMetrics MetricSet) (map[string]string, error) {
	if len(allCgroups) == 0 {
		return nil, fmt.Errorf("failed to find cgroup mounts")
	}

	// Trim the mounts to only the subsystems we care about.
	mountPoints := make(map[string]string, len(allCgroups))
	for _, mount := range allCgroups {
		for _, subsystem := range mount.Subsystems {
			if !needSubsys(subsystem, includedMetrics) {
				continue
			}
			if _, ok := mountPoints[subsystem]; ok {
				// duplicate mount for this subsystem; use the first one we saw
				logger.Infof(ctx, "skipping %s, already using mount at %s", mount.Mountpoint, mountPoints[subsystem])
				continue
			}
			mountPoints[subsystem] = mount.Mountpoint
		}
	}

	return mountPoints, nil
}

// getCgroupSubsystems returns information about the cgroup subsystems that are
// of interest as a map of cgroup controllers to their mount points.
// For example, "cpu" -> "/sys/fs/cgroup/cpu".
//
// The incudeMetrics arguments specifies which metrics are requested,
// and is used to filter out some cgroups and their mounts. If nil,
// all supported cgroup subsystems are included.
//
// For cgroup v2, includedMetrics argument is unused, the only map key is ""
// (empty string), and the value is the unified cgroup mount point.
func getCgroupSubsystems(ctx context.Context, includedMetrics MetricSet) (map[string]string, error) {
	if cgroups.IsCgroup2UnifiedMode() {
		return map[string]string{"": fs2.UnifiedMountpoint}, nil
	}
	// Get all cgroup mounts.
	allCgroups, err := cgroups.GetCgroupMounts(true)
	if err != nil {
		return nil, err
	}

	return getCgroupSubsystemsHelper(ctx, allCgroups, includedMetrics)
}

func MakeCgroupPaths(mountPoints map[string]string, name string) map[string]string {
	cgroupPaths := make(map[string]string, len(mountPoints))
	for key, val := range mountPoints {
		cgroupPaths[key] = path.Join(val, name)
	}

	return cgroupPaths
}

func GetControllerPath(cgroupPaths map[string]string, controllerName string, cgroup2UnifiedMode bool) (string, bool) {

	ok := false
	path := ""

	if cgroup2UnifiedMode {
		path, ok = cgroupPaths[""]
	} else {
		path, ok = cgroupPaths[controllerName]
	}
	return path, ok
}

func NewCgroupManager(name string, paths map[string]string) (cgroups.Manager, error) {
	config := &configs.Cgroup{
		Name:      name,
		Resources: &configs.Resources{},
	}
	if cgroups.IsCgroup2UnifiedMode() {
		path := paths[""]
		return fs2.NewManager(config, path)
	}

	return fs.NewManager(config, paths)
}
