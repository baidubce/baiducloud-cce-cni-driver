package os

import (
	"fmt"
	"os"

	"gopkg.in/fsnotify.v1"
)

// in ubuntu 22.04, the os-release file is like:
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

// startWatchingDefaultLinkFile starts watching the default link file for any changes.
// This function is specific to Ubuntu 22.04.
// It first creates a new file system watcher and adds the default link file to the watcher.
// If there are any write or remove operations on the file, appropriate actions are taken.
// On a write operation, it logs the event and checks and deals with the MAC address policy.
// On a remove operation, it removes the watcher for the deleted file, recreates the file if it does not exist,
// restarts the systemd-udevd, adds the new file to the watcher, and then checks and deals with the MAC address policy again.
func (o *ubuntuOS) startWatchingDefaultLinkFile() {
	log.Info("ubuntu 22.04 detected, start to watch default link file")

	// ensure the file was created correctly
	o.checkAndDealMACAddressPolicy(defaultLinkPath, defaultLinkTemplate)
	// Create a new file system watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Unable to create watcher for default link file")
	}
	defer watcher.Close()

	// Add the default link file to the watcher
	err = watcher.Add(defaultLinkPath)
	if err != nil {
		log.Fatalf("watcher failed to watch file at: %s", defaultLinkPath)
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

				o.checkAndDealMACAddressPolicy(defaultLinkPath, defaultLinkTemplate)
			}
			if event.Op&fsnotify.Remove == fsnotify.Remove {
				log.Infof("event: 'default link file removed' is watched")

				// Remove the watcher for the deleted file
				watcher.Remove(defaultLinkPath)

				// ensure the file was created correctly
				o.checkAndDealMACAddressPolicy(defaultLinkPath, defaultLinkTemplate)
				// Add the new file to the watcher
				err = watcher.Add(defaultLinkPath)
				if err != nil {
					log.Fatalf("watcher failed to re-watch file at: %s", defaultLinkPath)
				}
				log.Infof("re-watching default link file: %s", defaultLinkPath)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				log.Fatalf("Watcher error channel closed")
			}
			log.WithError(err).Fatal("Watcher encountered an error")
		}
	}
}

// checkAndDealMACAddressPolicy ensures that the MAC address policy in the default link file is correctly set.
// If the policy is incorrect or the file does not exist, it attempts to correct it by writing the default configuration.
func (o *ubuntuOS) checkAndDealMACAddressPolicy(filePath, template string) {
	if err := PrintFileContent(filePath); err != nil {
		log.Warnf("PrintFileContent error: %v, try to recreate it", err)
		// If printing fails, write the default template to the file.
		err = os.WriteFile(filePath, []byte(template), 0644)
		if err != nil {
			log.Fatalf("write default link file %s failed: %v", filePath, err)
		}
	}
	isConfigured, err := CheckIfLinkOptionConfigured(filePath, macAddressPolicyKey, macAddressPolicyValueNone)
	if isConfigured {
		log.Infof("default link file option %s is still as expected: %s", macAddressPolicyKey, macAddressPolicyValueNone)
		return
	}

	log.Warningf("default link file option %s go wrong, reason: %s, now try to set it back", macAddressPolicyKey, err)
	err = UpdateSystemdConfigOption(filePath, macAddressPolicyKey, macAddressPolicyValueNone)
	if err != nil {
		log.Fatalf("update default link file %s failed: %v", filePath, err)
	}
	log.Infof("update default link file success，now option %s is set correctly to %s", macAddressPolicyKey, macAddressPolicyValueNone)
}
