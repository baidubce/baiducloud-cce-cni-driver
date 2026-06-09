package os

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	ifcfgTemplate = `TYPE=Ethernet
DEVICE=%s
NM_CONTROLLED=no
ONBOOT=yes
BOOTPROTO=static
RES_OPTIONS="rotate timeout:1"
IPV6INIT="false"
IPV6_PEERDNS="no"
DHCPV6C="no"
DHCPV6C_OPTIONS=-nw
IPV6_DEFROUTE="no"
IPV6_PEERROUTES="no"`
	etcPath            = "/etc-host"
	networkScriptsPath = etcPath + "/sysconfig/network-scripts"
	sysconfigPath      = networkScriptsPath + "/ifcfg-%s"

	retryInterval  = 500 * time.Millisecond
	timeout        = 10 * time.Second
	settleTime     = 200 * time.Millisecond
	commandTimeout = 5 * time.Second
)

// Test overrides (only used in tests)
var (
	retryIntervalOverride = retryInterval
	timeoutOverride       = timeout
)

// Command execution functions that can be replaced in tests
var (
	runCommandWithTimeoutFunc         = runCommandWithTimeout
	runCommandWithTimeoutNoOutputFunc = runCommandWithTimeoutNoOutput
	execCommandFunc                   = execCommand
)

// runCommandWithTimeout executes a command with timeout and returns output and error
func runCommandWithTimeout(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// runCommandWithTimeoutNoOutput executes a command with timeout without capturing output
func runCommandWithTimeoutNoOutput(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

// execCommand executes a command and returns error
func execCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

// in centos 7, the os-release file is like:
// cat /etc/os-release
// NAME="CentOS Linux"
// VERSION="7 (Core)"
// ID="centos"
// ID_LIKE="rhel fedora"
// VERSION_ID="7"
// PRETTY_NAME="CentOS Linux 7 (Core)"
// ANSI_COLOR="0;31"
// CPE_NAME="cpe:/o:centos:centos:7"
// HOME_URL="https://www.centos.org/"
// BUG_REPORT_URL="https://bugs.centos.org/"

// CENTOS_MANTISBT_PROJECT="CentOS-7"
// CENTOS_MANTISBT_PROJECT_VERSION="7"
// REDHAT_SUPPORT_PRODUCT="centos"
// REDHAT_SUPPORT_PRODUCT_VERSION="7"
type centos struct {
	*OSRelease
}

// generateIfcfg generates ifcfg file for the given interface.
// DHCPv6 will configure all secondary IPs to the ENI interface,
// so we need to disable DHCPv6 for the interface.
// NetworkManager will use this file to configure the interface in Redhat OS.
// CentOS / Fedora / RHEL / Rocky Linux
func (c *centos) DisableDHCPv6(udevName, cceName string) error {
	// maybe not redhat os
	if _, err := os.ReadDir(networkScriptsPath); err != nil {
		return nil
	}

	path := fmt.Sprintf(sysconfigPath, udevName)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			goto createNew
		}
		return fmt.Errorf("failed to read %s: %s", path, err)
	}
	return nil

createNew:
	cceNamePath := fmt.Sprintf(sysconfigPath, cceName)
	_, err = os.Stat(cceNamePath)
	if err == nil {
		os.Remove(cceNamePath)
	}
	newCfg := fmt.Sprintf(ifcfgTemplate, udevName)
	err = os.WriteFile(path, []byte(newCfg), 0644)
	if err != nil {
		return fmt.Errorf("failed to write %s: %s", path, err)
	}

	err = runCommandWithTimeoutNoOutput("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "c", "reload", path)
	if err != nil {
		return fmt.Errorf("failed to reload network config: %s", err)
	}

	// wait for network manager to configure the interface
	time.Sleep(1 * time.Second)
	return nil
}

func (c *centos) DisableAndMonitorMacPersistant() error {
	return nil
}

// isNetworkManagerAvailable checks if NetworkManager is available and running on the system
func isNetworkManagerAvailable() bool {
	// First, check whether nmcli binary exists in the host namespace
	if err := runCommandWithTimeoutNoOutputFunc("nsenter", "-t", "1", "-m", "-u", "-i", "which", "nmcli"); err != nil {
		log.WithError(err).Debug("NetworkManager (nmcli) not available on the system")
		return false
	}

	// Then, check whether NetworkManager is actually reachable on the node
	_, err := runCommandWithTimeoutFunc("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "-t", "-f", "RUNNING", "general")
	if err != nil {
		log.WithError(err).Debug("NetworkManager (nmcli) present but not running on the system")
		return false
	}

	return true
}

// isDeviceUnmanaged checks if the device is already unmanaged by NetworkManager
func isDeviceUnmanaged(ifName string) (bool, error) {
	output, err := runCommandWithTimeoutFunc("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "-t", "-f", "GENERAL.STATE", "device", "show", ifName)
	if err != nil {
		return false, fmt.Errorf("failed to check NetworkManager status for %s: %v", ifName, err)
	}

	outputStr := strings.TrimSpace(string(output))
	return strings.Contains(strings.ToLower(outputStr), "unmanaged"), nil
}

// setDeviceUnmanaged sets the device as unmanaged by NetworkManager
func setDeviceUnmanaged(ifName string) error {
	return execCommandFunc("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "device", "set", ifName, "managed", "false")
}

// EnsureNetworkManagerUnmanaged ensures NetworkManager stops managing the specified interface
func (c *centos) EnsureNetworkManagerUnmanaged(ifName string) error {
	if !isNetworkManagerAvailable() {
		log.Warnf("NetworkManager not available on current operator system, skipping configuration for %s", ifName)
		return nil
	}

	// Check if already unmanaged
	unmanaged, err := isDeviceUnmanaged(ifName)
	if err != nil {
		log.WithError(err).Warnf("Failed to check NetworkManager status for %s, will retry", ifName)
		return err
	}

	if unmanaged {
		log.Infof("Device %s is already unmanaged by NetworkManager", ifName)
		return nil
	}

	log.Infof("Setting %s as unmanaged by NetworkManager with retry on current operator system", ifName)

	return wait.PollImmediate(retryIntervalOverride, timeoutOverride, func() (bool, error) {
		// Set device as unmanaged
		if err := setDeviceUnmanaged(ifName); err != nil {
			log.WithError(err).Warnf("Failed to set %s as unmanaged, will retry", ifName)
			return false, nil
		}

		// Wait for configuration to take effect
		time.Sleep(settleTime)

		// Verify the configuration
		unmanaged, err := isDeviceUnmanaged(ifName)
		if err != nil {
			log.WithError(err).Warnf("Failed to check NetworkManager status for %s, will retry", ifName)
			return false, nil
		}

		if unmanaged {
			log.Infof("Successfully set %s as unmanaged by NetworkManager (verified)", ifName)
			return true, nil
		}

		log.Warnf("Device %s is still managed by NetworkManager, retrying...", ifName)
		return false, nil
	})
}
