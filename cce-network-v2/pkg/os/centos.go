package os

import (
	"fmt"
	"os"
	"os/exec"
	"time"
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
func (c *centos) DisableDHCPv6(ifname string) error {
	// maybe not redhat os
	if _, err := os.ReadDir(networkScriptsPath); err != nil {
		return nil
	}

	path := fmt.Sprintf(sysconfigPath, ifname)
	_, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			goto createNew
		}
		return fmt.Errorf("failed to read %s: %s", path, err)
	}
	return nil

createNew:
	newCfg := fmt.Sprintf(ifcfgTemplate, ifname)
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

func (c *centos) DisableMacPersistant() error {
	return nil
}
