/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package metadata

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

const (
	metadataHost     = "169.254.169.254"
	metadataScheme   = "http"
	metadataBasePath = "/1.0/meta-data/"
)

type InstanceTypeEx string

var (
	InstanceTypeExBBC     InstanceTypeEx = "bbc"
	InstanceTypeExBCC     InstanceTypeEx = "bcc"
	InstanceTypeExUnknown InstanceTypeEx = "unknown"

	ErrorNotImplemented = errors.New("meta-data API not implemented")
)

type Interface interface {
	GetInstanceID() (string, error)
	GetInstanceName() (string, error)
	GetInstanceTypeEx() (InstanceTypeEx, error)
	GetLocalIPv4() (string, error)
	GetAvailabilityZone() (string, error)
	GetRegion() (string, error)
	GetVPCID() (string, error)
	GetSubnetID() (string, error)
	GetLinkGateway(string, string) (string, error)
	GetLinkMask(string, string) (string, error)
}

var _ Interface = &Client{}

type Client struct {
	host   string
	scheme string
}

func NewClient() *Client {
	c := &Client{
		host:   metadataHost,
		scheme: metadataScheme,
	}
	return c
}

func (c *Client) sendRequest(path string) ([]byte, error) {
	url := url.URL{
		Scheme: c.scheme,
		Host:   c.host,
		Path:   path,
	}
	resp, err := http.Get(url.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Get body content
	bodyContent, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return bodyContent, fmt.Errorf("Error Message: \"%s\", Status Code: %d", string(bodyContent), resp.StatusCode)
	}
	// Empty body means openstack meta-data API not implemented yet
	if strings.TrimSpace(string(bodyContent)) == "" {
		return bodyContent, ErrorNotImplemented
	}

	return bodyContent, err
}

func (c *Client) GetInstanceID() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "instance-shortid")
	if err != nil {
		return "", err
	}
	instanceID := strings.TrimSpace(string(body))
	return instanceID, nil
}

func (c *Client) GetInstanceName() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "instance-name")
	if err != nil {
		return "", err
	}
	instanceID := strings.TrimSpace(string(body))
	return instanceID, nil
}

func (c *Client) GetInstanceTypeEx() (InstanceTypeEx, error) {
	body, err := c.sendRequest(metadataBasePath + "instance-type-ex")
	if err != nil {
		return "", err
	}
	typeStr := strings.TrimSpace(string(body))
	switch typeStr {
	case "bbc":
		return InstanceTypeExBBC, nil
	case "bcc":
		return InstanceTypeExBCC, nil
	default:
		return InstanceTypeExUnknown, nil
	}
}

func (c *Client) GetLocalIPv4() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "local-ipv4")
	if err != nil {
		return "", err
	}
	addr := strings.TrimSpace(string(body))
	return addr, nil
}

func (c *Client) GetAvailabilityZone() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "azone")
	if err != nil {
		return "", err
	}
	azone := strings.TrimSpace(string(body))
	return azone, nil
}

func (c *Client) GetRegion() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "region")
	if err != nil {
		return "", err
	}
	region := strings.TrimSpace(string(body))
	return region, nil
}

func (c *Client) GetVPCID() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "vpc-id")
	if err != nil {
		return "", err
	}
	VPCID := strings.TrimSpace(string(body))
	return VPCID, nil
}

func (c *Client) GetSubnetID() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "subnet-id")
	if err != nil {
		return "", err
	}
	subnetID := strings.TrimSpace(string(body))
	return subnetID, nil
}

func (c *Client) GetLinkGateway(macAddress, ipAddress string) (string, error) {
	// eg. /1.0/meta-data/network/interfaces/macs/fa:26:00:01:6f:37/fixed_ips/10.0.4.140/gateway
	path := fmt.Sprintf(metadataBasePath+"network/interfaces/macs/%s/fixed_ips/%s/gateway", macAddress, ipAddress)
	body, err := c.sendRequest(path)
	if err != nil {
		return "", err
	}
	gateway := strings.TrimSpace(string(body))
	return gateway, nil
}

func (c *Client) GetLinkMask(macAddress, ipAddress string) (string, error) {
	// eg. /1.0/meta-data/network/interfaces/macs/fa:26:00:01:6f:37/fixed_ips/10.0.4.140/mask
	path := fmt.Sprintf(metadataBasePath+"network/interfaces/macs/%s/fixed_ips/%s/mask", macAddress, ipAddress)
	body, err := c.sendRequest(path)
	if err != nil {
		return "", err
	}
	mask := strings.TrimSpace(string(body))
	return mask, nil
}
