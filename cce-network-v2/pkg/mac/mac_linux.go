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

package mac

import "github.com/vishvananda/netlink"

// HasMacAddr returns true if the given network interface has L2 addr.
func HasMacAddr(iface string) bool {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return false
	}
	return LinkHasMacAddr(link)
}

// LinkHasMacAddr returns true if the given network interface has L2 addr.
func LinkHasMacAddr(link netlink.Link) bool {
	return len(link.Attrs().HardwareAddr) != 0
}
