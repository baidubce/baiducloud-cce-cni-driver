package os

import (
	"fmt"
	"os"
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
func (*ubuntuOS) DisableDHCPv6(dev string) error {
	return nil
}

// DisableMacPersistant implements HostOS.
func (o *ubuntuOS) DisableMacPersistant() error {
	if o.VersionID != "22.04" {
		log.Info("not ubuntu 22.04, skip disable mac persistent")
		return nil
	}
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
	return UpdateSystemdConfigOption(defaultLinkPath, macAddressPolicyKey, "none")
}
