package os

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	
	"github.com/coreos/go-systemd/v22/unit"
)

const (
	usrPath             = "/usr-host"
	defaultLinkPath     = usrPath + "/lib/systemd/network/99-default.link"
	macAddressPolicyKey = "MACAddressPolicy"

	defaultLinkTemplate = `
	[Match]
	OriginalName=*

	[Link]
	NamePolicy=keep kernel database onboard slot path
	AlternativeNamesPolicy=database onboard slot path
	MACAddressPolicy=none
	`
)

func UpdateSystemdConfigOption(linkPath, key, velue string) error {
	file, err := os.Open(linkPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", linkPath, err)
	}
	defer file.Close()

	opts, err := unit.DeserializeOptions(file)
	if err != nil {
		return fmt.Errorf("failed to deserialize %s: %w", linkPath, err)
	}

	update := false
	for i := 0; i < len(opts); i++ {
		if opts[i].Name == key {
			if opts[i].Value == velue {
				return nil
			}
			opts[i].Value = velue
			update = true
		}
	}

	if !update {
		return fmt.Errorf("failed to find %s in %s", key, linkPath)
	}
	newBytes, _ := io.ReadAll(unit.Serialize(opts))

	err = os.WriteFile(linkPath, newBytes, 0644)
	if err != nil {
		return fmt.Errorf("failed to write %s: %v", linkPath, err)
	}
	log.Infof("restart systemd-udevd")
	// restart systemd-udevd
	return exec.Command("nsenter", "-m", "-u", "-t", "1", "systemctl", "restart", "systemd-udevd").Run()
}
