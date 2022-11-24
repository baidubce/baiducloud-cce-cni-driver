package cloud_util

import (
	"fmt"
	"strings"
)

// AvailableZone 可用区
type AvailableZone string

const (
	// ZoneA 可用区 A
	ZoneA AvailableZone = "zoneA"

	// ZoneB 可用区 B
	ZoneB AvailableZone = "zoneB"

	// ZoneC 可用区 C
	ZoneC AvailableZone = "zoneC"

	// ZoneD 可用区 D
	ZoneD AvailableZone = "zoneD"

	// ZoneE 可用区 E
	ZoneE AvailableZone = "zoneE"

	// ZoneF 可用区 F
	ZoneF AvailableZone = "zoneF"
)

// TransZoneNameToAvailableZone - 将 zoneName 转成 availableZone
func TransZoneNameToAvailableZone(zoneName string) (AvailableZone, error) {
	arr := strings.Split(zoneName, "-")
	if len(arr) != 3 {
		return "", fmt.Errorf("unrecognized zoneName format: %s", zoneName)
	}

	zone := arr[2]
	if zone == "a" {
		return ZoneA, nil
	}
	if zone == "b" {
		return ZoneB, nil
	}
	if zone == "c" {
		return ZoneC, nil
	}
	if zone == "d" {
		return ZoneD, nil
	}
	if zone == "e" {
		return ZoneE, nil
	}
	if zone == "f" {
		return ZoneF, nil
	}

	return "", fmt.Errorf("unrecognized zoneName: %s", zoneName)
}

// TransAvailableZoneToZoneName - 将 availableZone 转成 zoneName
func TransAvailableZoneToZoneName(country, region, availableZone string) string {
	az := AvailableZone(availableZone)
	zonePrefix := country + "-" + region + "-"
	switch az {
	case ZoneA:
		return zonePrefix + "a"
	case ZoneB:
		return zonePrefix + "b"
	case ZoneC:
		return zonePrefix + "c"
	case ZoneD:
		return zonePrefix + "d"
	case ZoneE:
		return zonePrefix + "e"
	case ZoneF:
		return zonePrefix + "f"
	default:
		return zonePrefix + availableZone
	}
}
