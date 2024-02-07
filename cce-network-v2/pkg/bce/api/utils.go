/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */
package api

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

	zonePrefix string = "zone"
)

// TransZoneNameToAvailableZone - 将 zoneName 转成 availableZone
func TransZoneNameToAvailableZone(zoneName string) (string, error) {
	arr := strings.Split(zoneName, "-")
	if len(arr) != 3 {
		return "", fmt.Errorf("unrecognized zoneName format: %s", zoneName)
	}

	zone := arr[2]
	return zonePrefix + strings.ToUpper(zone), nil
}

// TransAvailableZoneToZoneName - 将 availableZone 转成 zoneName
func TransAvailableZoneToZoneName(country, region, availableZone string) string {
	azPrefix := country + "-" + region + "-"
	zone := strings.ToLower(strings.Trim(availableZone, zonePrefix))
	return azPrefix + zone
}
