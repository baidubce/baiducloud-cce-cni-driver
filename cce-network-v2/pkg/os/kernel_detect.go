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

package os

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	utilexec "k8s.io/utils/exec"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

var kernelLog = logging.DefaultLogger.WithField(logfields.LogSubsys, "os-kernel")

type Interface interface {
	DetectKernelVersion(ctx context.Context) (string, error)
	GetModules(ctx context.Context, wantedModules []string) ([]string, error)
}

type LinuxKernelHandler struct {
	executor utilexec.Interface
}

// NewLinuxKernelHandler initializes LinuxKernelHandler with exec.
func NewLinuxKernelHandler() *LinuxKernelHandler {
	return &LinuxKernelHandler{
		executor: utilexec.New(),
	}
}

func (kh *LinuxKernelHandler) DetectKernelVersion(ctx context.Context) (string, error) {
	kernelVersionFile := "/proc/sys/kernel/osrelease"
	fileContent, err := ioutil.ReadFile(kernelVersionFile)
	if err != nil {
		return "", fmt.Errorf("error reading osrelease file %q: %v", kernelVersionFile, err)
	}
	return strings.TrimSpace(string(fileContent)), nil
}

// GetModulesAndTryToLoadByWanted returns all installed kernel modules which match any of the specified names.
// Try to load the required kernel modules if not built in.
func (kh *LinuxKernelHandler) GetModulesAndTryToLoadByWanted(ctx context.Context, wantedModules []string) ([]string, error) {
	var bmods, lmods []string

	kernelVersionStr, err := kh.DetectKernelVersion(ctx)
	if err != nil {
		return nil, err
	}
	// Find out loaded kernel modules. If this is a full static kernel it will try to verify if the module is compiled using /boot/config-KERNELVERSION
	modulesFile, err := os.Open("/proc/modules")
	if err == os.ErrNotExist {
		kernelLog.Infof("failed to read file /proc/modules with error %v. Assuming this is a kernel without loadable modules support enabled", err)
		kernelConfigFile := fmt.Sprintf("/boot/config-%s", kernelVersionStr)
		kConfig, err := ioutil.ReadFile(kernelConfigFile)
		if err != nil {
			return nil, fmt.Errorf("Failed to read Kernel Config file %s with error %v", kernelConfigFile, err)
		}
		for _, module := range wantedModules {
			if match, _ := regexp.Match("CONFIG_"+strings.ToUpper(module)+"=y", kConfig); match {
				bmods = append(bmods, module)
			}
		}
		return bmods, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to read file /proc/modules with error %v", err)
	}

	mods, err := getFirstColumn(modulesFile)
	if err != nil {
		return nil, fmt.Errorf("failed to find loaded kernel modules: %v", err)
	}

	builtinModsFilePath := fmt.Sprintf("/lib/modules/%s/modules.builtin", kernelVersionStr)
	b, err := ioutil.ReadFile(builtinModsFilePath)
	if err != nil {
		kernelLog.Infof("failed to read file %s with error %v. You can ignore this message when this is running inside container without mounting /lib/modules", builtinModsFilePath, err)
	}

	for _, module := range wantedModules {
		if match, _ := regexp.Match(module+".ko", b); match {
			bmods = append(bmods, module)
		} else {
			// Try to load the required IPVS kernel modules if not built in
			err := kh.executor.Command("modprobe", "--", module).Run()
			if err != nil {
				kernelLog.Infof("failed to load kernel module %v with modprobe. The error is %v."+
					"You can ignore this message when this is running inside container without mounting /lib/modules", module, err)
			} else {
				lmods = append(lmods, module)
			}
		}
	}

	mods = append(mods, bmods...)
	mods = append(mods, lmods...)
	return mods, nil
}

// getFirstColumn reads all the content from r into memory and return a
// slice which consists of the first word from each line.
func getFirstColumn(r io.Reader) ([]string, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(b), "\n")
	words := make([]string, 0, len(lines))
	for i := range lines {
		fields := strings.Fields(lines[i])
		if len(fields) > 0 {
			words = append(words, fields[0])
		}
	}
	return words, nil
}
