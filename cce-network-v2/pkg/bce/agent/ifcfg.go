package agent

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
	networkScriptsPath = "/etc/sysconfig/network-scripts"
	sysconfigPath      = networkScriptsPath + "/ifcfg-%s"
)

// generateIfcfg generates ifcfg file for the given interface.
// DHCPv6 will configure all secondary IPs to the ENI interface,
// so we need to disable DHCPv6 for the interface.
// NetworkManager will use this file to configure the interface in Redhat OS.
func (ec *eniLink) generateIfcfg(ifname string) error {
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
	scopeLog := ec.log.WithField("path", path)
	newCfg := fmt.Sprintf(ifcfgTemplate, ifname)
	err = os.WriteFile(path, []byte(newCfg), 0644)
	if err != nil {
		return fmt.Errorf("failed to write %s: %s", path, err)
	}
	scopeLog.Infof("created new ifcfg %s success", path)

	err = exec.Command("nsenter", "-t", "1", "-m", "-u", "-i", "nmcli", "c", "reload", path).Run()
	if err != nil {
		return fmt.Errorf("failed to reload network config: %s", err)
	}
	scopeLog.Infof("reload network config success")

	// wait for network manager to configure the interface
	time.Sleep(1 * time.Second)
	return nil
}
