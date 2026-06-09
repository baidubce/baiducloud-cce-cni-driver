package os

import (
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

	retryInterval = 500 * time.Millisecond
	timeout       = 10 * time.Second
	settleTime    = 200 * time.Millisecond
)

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

	err = exec.Command("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "c", "reload", path).Run()
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

// EnsureNetworkManagerUnmanaged ensures NetworkManager stops managing the specified interface
func (c *centos) EnsureNetworkManagerUnmanaged(ifName string) error {
	// Check if NetworkManager is available
	err := exec.Command("nsenter", "-t", "1", "-m", "-u", "-i", "which", "nmcli").Run()
	if err != nil {
		log.Warnf("NetworkManager not available on current operator system, skipping configuration for %s", ifName)
		return nil
	}

	log.Infof("Setting %s as unmanaged by NetworkManager with retry on current operator system", ifName)

	return wait.PollImmediate(retryInterval, timeout, func() (bool, error) {
		// 1. Execute configuration command
		err := exec.Command("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "device", "set", ifName, "managed", "false").Run()
		if err != nil {
			log.WithError(err).Warnf("Failed to set %s as unmanaged, will retry", ifName)
			return false, nil // Return false to continue retry, don't return error
		}

		// 2. Wait for configuration to take effect
		time.Sleep(settleTime)

		// 3. Check if configuration is effective
		cmd := exec.Command("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "-t", "-f", "GENERAL.STATE", "device", "show", ifName)
		output, err := cmd.Output()
		if err != nil {
			log.WithError(err).Warnf("Failed to check NetworkManager status for %s, will retry", ifName)
			return false, nil // Return false to continue retry, don't return error
		}

		// Parse output to find unmanaged status
		outputStr := strings.TrimSpace(string(output))
		isUnmanaged := strings.Contains(strings.ToLower(outputStr), "unmanaged")

		if isUnmanaged {
			log.Infof("Successfully set %s as unmanaged by NetworkManager (verified)", ifName)
			return true, nil // Success, stop retry
		}

		log.Warnf("Device %s is still managed by NetworkManager, retrying...", ifName)
		return false, nil // Return false to continue retry
	})
}
