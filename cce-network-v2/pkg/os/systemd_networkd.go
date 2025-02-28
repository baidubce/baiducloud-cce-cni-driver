package os

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/coreos/go-systemd/v22/unit"
)

const (
	usrPath                   = "/usr-host"
	defaultLinkPath           = usrPath + "/lib/systemd/network/98-default.link"
	macAddressPolicyKey       = "MACAddressPolicy"
	macAddressPolicyValueNone = "none"

	defaultLinkTemplate = `
[Match]
OriginalName=*

[Link]
NamePolicy=keep kernel database onboard slot path
AlternativeNamesPolicy=database onboard slot path
MACAddressPolicy=none
`
)

func UpdateSystemdConfigOption(linkPath, key, value string) error {
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
			if opts[i].Value == value {
				return nil
			}
			opts[i].Value = value
			update = true
		}
	}

	if !update {
		//here means maybe the option is removed, we need to add it to the default link file
		opts = append(opts, &unit.UnitOption{
			Name:  key,
			Value: value,
		})
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

// check a certain option key in the default link file if it is configured as the value
func CheckIfLinkOptionConfigured(linkPath, key, value string) (bool, error) {
	file, err := os.Open(linkPath)
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %w", linkPath, err)
	}
	defer file.Close()

	opts, err := unit.DeserializeOptions(file)
	if err != nil {
		return false, fmt.Errorf("failed to deserialize %s: %w", linkPath, err)
	}

	for _, opt := range opts {
		if opt.Name != key {
			continue
		}
		if opt.Value == value {
			return true, nil
		} else {
			return false, fmt.Errorf("the key %s in %s is not configured correctly, expected %s, got %s", key, linkPath, value, opt.Value)
		}
	}

	return false, fmt.Errorf("failed to find %s in %s, the key is missing", key, linkPath)
}

// PrintFileContent reads the entire content of the specified file into a string, and then prints it
func PrintFileContent(filename string) error {
	log.Info(filename, " file content:")
	// Read the entire file
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("unable to read file: %v", err)
	}

	// Convert the data to a string and print it
	content := string(data)
	log.Info(content)

	return nil
}
