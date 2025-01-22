package os

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

const (
	osReleasePath  = "/usr-host/lib/os-release"
	etcReleasePath = "/etc-host/os-release"
)

var log = logging.DefaultLogger.WithField(logfields.LogSubsys, "os")

var localOS *OSRelease

type HostOS interface {
	// DisableDHCPv6
	DisableDHCPv6(udevName, cceName string) error
	DisableAndMonitorMacPersistant() error
}

type OSRelease struct {
	Name       string
	Version    string
	ID         string
	IDLike     string
	PrettyName string
	VersionID  string
	HomeURL    string
	Comment    string
}

// NewOSDistribution returns a new OSRelease object.
//
// -------
func NewOSDistribution() (*OSRelease, error) {
	if localOS != nil {
		return localOS, nil
	}
	file, err := os.ReadFile(osReleasePath)
	if err != nil {
		file, err = os.ReadFile(etcReleasePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s and %s: %v", osReleasePath, etcReleasePath, err)
		}
	}
	var osRelease OSRelease
	reader := bufio.NewReader(bytes.NewReader(file))

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSuffix(line, "\n")
		fields := strings.Split(line, "=")
		switch {
		case len(fields) >= 2 && fields[0] == "NAME":
			osRelease.Name = strings.Trim(fields[1], `"`)
		case len(fields) >= 2 && fields[0] == "VERSION":
			osRelease.Version = strings.Trim(fields[1], `"`)
		case len(fields) >= 2 && fields[0] == "VERSION_ID":
			osRelease.VersionID = strings.Trim(fields[1], `"`)
		case len(fields) >= 2 && fields[0] == "ID":
			osRelease.ID = strings.Trim(fields[1], `"`)
		case len(fields) >= 2 && fields[0] == "ID_LIKE":
			osRelease.IDLike = strings.Trim(fields[1], `"`)
		case len(fields) >= 2 && fields[0] == "PRETTY_NAME":
			osRelease.PrettyName = strings.Trim(fields[1], `"`)
		case len(fields) >= 2 && fields[0] == "COMMENT":
			osRelease.Comment = fields[1]
		}
	}
	localOS = &osRelease
	log.WithField("osRelease", logfields.Repr(osRelease)).Infof("Detected OS")
	return localOS, nil
}

func (o *OSRelease) HostOS() HostOS {
	switch o.ID {
	case "ubuntu":
		return &ubuntuOS{
			OSRelease: o,
		}
	case "centos":
		return &centos{OSRelease: o}
	case "baidulinux":
		return &baidulinux{centos: &centos{OSRelease: o}}
	default:
		// we use centos as default
		return &baidulinux{centos: &centos{OSRelease: o}}
	}
}
