package os

import (
	"fmt"
	"os"
	"os/exec"

	"gopkg.in/fsnotify.v1"
)

var (
	unbuntuReleasePath = etcPath + "/os-release"
)

// // in ubuntu 22.04, the os-release file is like:
// PRETTY_NAME="Ubuntu 22.04 LTS"
// NAME="Ubuntu"
// VERSION_ID="22.04"
// VERSION="22.04 LTS (Jammy Jellyfish)"
// VERSION_CODENAME=jammy
// ID=ubuntu
// ID_LIKE=debian
// HOME_URL="https://www.ubuntu.com/"
// SUPPORT_URL="https://help.ubuntu.com/"
// BUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"
// PRIVACY_POLICY_URL="https://www.ubuntu.com/legal/terms-and-policies/privacy-policy"
// UBUNTU_CODENAME=jammy
type ubuntuOS struct {
	*OSRelease
}

// DisableDHCPv6 implements HostOS.
func (*ubuntuOS) DisableDHCPv6(udevName, cceName string) error {
	return nil
}

// DisableMacPersistant implements HostOS.
// and start to monitor the default link file to detect whether it is removed
// or its option "MACAddressPolicy" is not 'none'
func (o *ubuntuOS) DisableAndMonitorMacPersistant() error {
	if o.VersionID != "22.04" {
		log.Info("not ubuntu 22.04, skip disable mac persistent")
		return nil
	}
	err := o.overrideSystemdDefaultLinkConfig()
	if err != nil {
		log.Errorf("failed to disable mac persistent, ignored os policy: %v", err)
	}

	go o.startWatchingDefaultLinkFile()

	return nil
}

func (o *ubuntuOS) overrideSystemdDefaultLinkConfig() error {
	_, err := os.Open(defaultLinkPath)
	if os.IsNotExist(err) {
		err = os.WriteFile(defaultLinkPath, []byte(defaultLinkTemplate), 0644)
		if err != nil {
			return fmt.Errorf("write default link file %s failed: %v", defaultLinkPath, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("open default link file %s failed: %v", defaultLinkPath, err)
	}

	err = UpdateSystemdConfigOption(defaultLinkPath, macAddressPolicyKey, macAddressPolicyValueNone)
	if err != nil {
		return fmt.Errorf("update default link file %s failed: %v", defaultLinkPath, err)
	}

	_, err = CheckIfLinkOptionConfigured(defaultLinkPath, macAddressPolicyKey, macAddressPolicyValueNone)
	if err != nil {
		return fmt.Errorf("check default link file %s failed: %v", defaultLinkPath, err)
	} else {
		log.Infof("override default link file %s success, option %s is set correctly", defaultLinkPath, macAddressPolicyKey)
		return nil
	}
}

// startWatchingDefaultLinkFile starts an goroutine that continues to watche the default link file,
// when the option "MACAddressPolicy" is changed from "none",
// which was configured by updateLocalNode() in StartDiscovery(),
// the condition "networkUnavailable" will be turned into "true" on node.
func (o *ubuntuOS) startWatchingDefaultLinkFile() {
	log.Info("ubuntu 22.04 detected, start to watch default link file")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error("Unable to create watcher for default link file")
		return
	}
	defer watcher.Close()

	err = watcher.Add(defaultLinkPath)
	if err != nil {
		log.Errorf("watcher failed to watch file at: %s", defaultLinkPath)
		return
	}

	log.Infof("start to watch default link file: %s", defaultLinkPath)
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Infof("event: 'default link file overwritten' is watched")

				o.checkAndDealMACAddressPolicy()
			}
			if event.Op&fsnotify.Remove == fsnotify.Remove {
				log.Infof("event: 'default link file removed' is watched")
				_, err := os.Open(defaultLinkPath)
				if os.IsNotExist(err) {
					err = os.WriteFile(defaultLinkPath, []byte(defaultLinkTemplate), 0644)
					if err != nil {
						log.Errorf("write default link file %s failed: %v", defaultLinkPath, err)
						return
					}
					log.Infof("write default link file %s success", defaultLinkPath)
					log.Infof("restart systemd-udevd")
					// restart systemd-udevd
					exec.Command("nsenter", "-m", "-u", "-t", "1", "systemctl", "restart", "systemd-udevd").Run()
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.WithError(err).Errorf("watcher error: %s", err)
		}
	}
}

func (o *ubuntuOS) checkAndDealMACAddressPolicy() {

	isConfigured, err := CheckIfLinkOptionConfigured(defaultLinkPath, macAddressPolicyKey, macAddressPolicyValueNone)
	if isConfigured {
		log.Infof("default link file option %s is still as expected: %s", macAddressPolicyKey, macAddressPolicyValueNone)
		return
	}

	log.Warningf("default link file option %s go wrong, reason: %s, now try to set it back", macAddressPolicyKey, err)
	err = UpdateSystemdConfigOption(defaultLinkPath, macAddressPolicyKey, macAddressPolicyValueNone)
	if err != nil {
		log.Errorf("update default link file %s failed: %v", defaultLinkPath, err)
		return
	}
	log.Infof("update default link file success，now option %s is set correctly to %s", macAddressPolicyKey, macAddressPolicyValueNone)
}
