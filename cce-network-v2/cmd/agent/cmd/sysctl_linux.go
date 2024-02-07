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

package cmd

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/sysctl"
)

func enableIPForwarding() error {
	if err := sysctl.Enable("net.ipv4.ip_forward"); err != nil {
		return err
	}
	if err := sysctl.Enable("net.ipv4.conf.all.forwarding"); err != nil {
		return err
	}
	if option.Config.EnableIPv6 {
		if err := sysctl.Enable("net.ipv6.conf.all.forwarding"); err != nil {
			return err
		}
	}
	return nil
}
