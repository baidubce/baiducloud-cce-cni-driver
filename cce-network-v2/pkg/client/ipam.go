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

package client

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/client/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/api"
)

const (
	AddressFamilyIPv6 = "ipv6"
	AddressFamilyIPv4 = "ipv4"
)

// IPAMCNIAllocate allocates an IP address out of address family specific pool.
func (c *Client) IPAMCNIAllocate(family, owner, containerID, netns string) (*models.IPAMResponse, error) {
	params := ipam.NewPostIpamParams().WithTimeout(api.ClientTimeout)

	if family != "" {
		params.SetFamily(&family)
	}

	if owner != "" {
		params.SetOwner(&owner)
	}
	if containerID != "" {
		params.SetContainerID(&containerID)
	}
	if netns != "" {
		params.SetNetns(&netns)
	}

	resp, err := c.Ipam.PostIpam(params)
	if err != nil {
		return nil, Hint(err)
	}
	return resp.Payload, nil
}

// IPAMAllocate allocates an IP address out of address family specific pool.
func (c *Client) IPAMAllocate(family, owner string, expiration bool) (*models.IPAMResponse, error) {
	params := ipam.NewPostIpamParams().WithTimeout(api.ClientTimeout)

	if family != "" {
		params.SetFamily(&family)
	}

	if owner != "" {
		params.SetOwner(&owner)
	}

	params.SetExpiration(&expiration)

	resp, err := c.Ipam.PostIpam(params)
	if err != nil {
		return nil, Hint(err)
	}
	return resp.Payload, nil
}

// IPAMAllocateIP tries to allocate a particular IP address.
func (c *Client) IPAMAllocateIP(ip, owner string) error {
	params := ipam.NewPostIpamIPParams().WithIP(ip).WithOwner(&owner).WithTimeout(api.ClientTimeout)
	_, err := c.Ipam.PostIpamIP(params)
	return Hint(err)
}

// IPAMCNIReleaseIP releases a IP address back to the pool.
func (c *Client) IPAMCNIReleaseIP(owner, containerID, netns string) error {
	params := ipam.NewDeleteIpamIPParams().WithIP(containerID).WithOwner(&owner).WithContainerID(&containerID).WithTimeout(api.ClientTimeout)
	_, err := c.Ipam.DeleteIpamIP(params)
	return Hint(err)
}

// IPAMReleaseIP releases a IP address back to the pool.
func (c *Client) IPAMReleaseIP(ip string) error {
	params := ipam.NewDeleteIpamIPParams().WithIP(ip).WithTimeout(api.ClientTimeout)
	_, err := c.Ipam.DeleteIpamIP(params)
	return Hint(err)
}
