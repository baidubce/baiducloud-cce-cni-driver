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

package sysctl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

const (
	subsystem = "sysctl"

	procFsDefault = "/proc"
)

var (
	log = logging.DefaultLogger.WithField(logfields.LogSubsys, subsystem)

	// parameterElemRx matches an element of a sysctl parameter.
	parameterElemRx = regexp.MustCompile(`\A[-0-9_a-z]+\z`)

	procFsMU lock.Mutex
	// procFsRead is mark as true if procFs changes value.
	procFsRead bool
	procFs     = procFsDefault

	// SetIfNeededFunc can be replaced in tests
	SetIfNeededFunc = SetIfNeeded

	// readFn / writeFn are the underlying read/write implementations used by
	// SetIfNeeded. They are package-level variables so that tests can replace
	// them without requiring gomonkey (which can fail when functions are inlined).
	readFn  func(name string) (string, error)   = Read
	writeFn func(name string, val string) error = Write
)

// An ErrInvalidSysctlParameter is returned when a parameter is invalid.
type ErrInvalidSysctlParameter string

func (e ErrInvalidSysctlParameter) Error() string {
	return fmt.Sprintf("invalid sysctl parameter: %q", string(e))
}

// Setting represents a sysctl setting. Its purpose it to be able to iterate
// over a slice of settings.
type Setting struct {
	Name      string
	Val       string
	IgnoreErr bool

	// Warn if non-empty is the alternative warning log message to use when IgnoreErr is false.
	Warn string
}

// parameterPath returns the path to the sysctl file for parameter name.
func parameterPath(name string) (string, error) {
	elems := strings.Split(name, ".")
	for _, elem := range elems {
		if !parameterElemRx.MatchString(elem) {
			return "", ErrInvalidSysctlParameter(name)
		}
	}
	return filepath.Join(append([]string{GetProcfs(), "sys"}, elems...)...), nil
}

// SetProcfs sets path for the root's /proc. Calling it after GetProcfs causes
// panic.
func SetProcfs(path string) {
	procFsMU.Lock()
	defer procFsMU.Unlock()
	if procFsRead {
		// do not change the procfs after we have gotten its value from GetProcfs
		panic("SetProcfs called after GetProcfs")
	}
	procFs = path
}

// GetProcfs returns the path set in procFs. Executing SetProcFs after GetProcfs
// might panic depending. See SetProcfs for more info.
func GetProcfs() string {
	procFsMU.Lock()
	defer procFsMU.Unlock()
	procFsRead = true
	return procFs
}

func writeSysctl(name string, value string) error {
	path, err := parameterPath(name)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("could not open the sysctl file %s: %s",
			path, err)
	}
	defer f.Close()
	if _, err := io.WriteString(f, value); err != nil {
		return fmt.Errorf("could not write to the systctl file %s: %s",
			path, err)
	}
	return nil
}

// Disable disables the given sysctl parameter.
func Disable(name string) error {
	return writeSysctl(name, "0")
}

// Enable enables the given sysctl parameter.
func Enable(name string) error {
	return writeSysctl(name, "1")
}

// Write writes the given sysctl parameter.
func Write(name string, val string) error {
	return writeSysctl(name, val)
}

// Read reads the given sysctl parameter.
func Read(name string) (string, error) {
	path, err := parameterPath(name)
	if err != nil {
		return "", err
	}
	val, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("Failed to read %s: %s", path, err)
	}

	return strings.TrimRight(string(val), "\n"), nil
}

// ApplySettings applies all settings in sysSettings.
func ApplySettings(sysSettings []Setting) error {
	for _, s := range sysSettings {
		log.WithFields(logrus.Fields{
			logfields.SysParamName:  s.Name,
			logfields.SysParamValue: s.Val,
		}).Info("Setting sysctl")
		if err := Write(s.Name, s.Val); err != nil {
			if !s.IgnoreErr || errors.Is(err, ErrInvalidSysctlParameter("")) {
				return fmt.Errorf("Failed to sysctl -w %s=%s: %s", s.Name, s.Val, err)
			}

			warn := "Failed to sysctl -w"
			if s.Warn != "" {
				warn = s.Warn
			}
			log.WithError(err).WithFields(logrus.Fields{
				logfields.SysParamName:  s.Name,
				logfields.SysParamValue: s.Val,
			}).Warning(warn)
		}
	}

	return nil
}

func CompareAndSet(name string, oldVal string, val string) error {
	current, err := Read(name)
	if err != nil {
		return err
	}
	if oldVal != current {
		return nil
	}

	return Write(name, val)
}

// SetIfNeeded reads the current sysctl value and sets it to the target value if different
// Returns error if setting fails, or nil if already set to target value or successfully set
// If the sysctl file does not exist (e.g., IPv6 is completely disabled), returns nil without error
func SetIfNeeded(paramName, targetValue string) error {
	// Get current state before setting
	currentValue, readErr := readFn(paramName)
	if readErr != nil {
		// Check if the error is due to file not existing (e.g., IPv6 disabled)
		// Use errors.Is for Go 1.13+ error wrapping, fallback to string matching for compatibility
		if errors.Is(readErr, os.ErrNotExist) || strings.Contains(readErr.Error(), "no such file") {
			// File doesn't exist, which means the feature is not available
			// This is expected when IPv6 is disabled at kernel level
			log.WithField(logfields.SysParamName, paramName).Warning("sysctl file does not exist, skipping")
			return nil
		}
		// Continue to set even if read fails for other reasons
		currentValue = ""
	}

	// Only set if not already at target value
	if currentValue != targetValue {
		writeErr := writeFn(paramName, targetValue)
		if writeErr != nil {
			// Check if the error is due to file not existing
			if errors.Is(writeErr, os.ErrNotExist) || strings.Contains(writeErr.Error(), "no such file") {
				log.WithField(logfields.SysParamName, paramName).Warning("sysctl file does not exist during write, skipping")
				return nil
			}
			return writeErr
		}
	}

	return nil
}
