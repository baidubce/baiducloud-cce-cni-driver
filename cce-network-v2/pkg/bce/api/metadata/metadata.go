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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/version"
)

const (
	metadataHost     = "169.254.169.254"
	metadataScheme   = "http"
	metadataBasePath = "/1.0/meta-data/"
)

var (
	// DefaultMetaClient is the shared metadata client instance
	// Initialized lazily in InitMetadataClient() to ensure retry config is set
	DefaultMetaClient Interface
)

type InstanceType string
type InstanceTypeEx string

// FeatureList represents the response from feature_list endpoint
type FeatureList struct {
	ENIQuota string `json:"eni_quota"`
	ERIQuota string `json:"eri_quota"`
}

var (
	InstanceTypeExBBC     InstanceTypeEx = "bbc"
	InstanceTypeExBCC     InstanceTypeEx = "bcc"
	InstanceTypeExEBC     InstanceTypeEx = "ebc"
	InstanceTypeExEHC     InstanceTypeEx = "ehc"
	InstanceTypeExHPAS    InstanceTypeEx = "hpas"
	InstanceTypeExUnknown InstanceTypeEx = "unknown"

	IPTypePrimary   = "primary"
	IPTypeSecondary = "secondary"

	ErrorNotImplemented = errors.New("meta-data API not implemented")

	log = logging.NewSubysLogger("bce-metadata")
)

type Interface interface {
	GetInstanceID() (string, error)
	GetInstanceName() (string, error)
	GetResourceID() (string, error)
	GetInstanceType() (InstanceType, error)
	GetInstanceSpecs() (string, error)
	GetInstanceTypeEx() (InstanceTypeEx, error)
	GetLocalIPv4() (string, error)
	GetAvailabilityZone() (string, error)
	GetRegion() (string, error)
	GetVPCID() (string, error)
	GetSourceType() (string, error)
	GetSubnetID() (string, error)
	GetLinkGateway(string, string) (string, error)
	GetLinkMask(string, string) (string, error)
	GetLinkFixedIPs(string) ([]string, error)
	GetLinkPrimaryIP(string) (string, error)
	GetLinkSecondaryIPs(string) ([]string, error)
	GetLinkMTU(string) (int, error)
	GetVifFeatures(macAddress string) (string, error)
	GetFeatureList() (*FeatureList, error)

	ListMacs() ([]string, error)
}

var _ Interface = &Client{}

type Client struct {
	host              string
	scheme            string
	debug             bool
	retryCount        int
	retryMaxDelay     time.Duration
	retryBaseInterval time.Duration
}

func NewClient(retryCount int, retryMaxDelay, retryBaseInterval time.Duration) *Client {
	c := &Client{
		host:              metadataHost,
		scheme:            metadataScheme,
		debug:             false,
		retryCount:        retryCount,
		retryMaxDelay:     retryMaxDelay,
		retryBaseInterval: retryBaseInterval,
	}

	if _, exists := os.LookupEnv("DEBUG_METADATA"); exists {
		c.debug = true
	}

	return c
}

// InitMetadataClient initializes the global DefaultMetaClient with the provided retry configuration
// This is the main initialization function that sets up retry config and creates the client
func InitMetadataClient(retryCount int, retryMaxDelay, retryBaseInterval time.Duration) {
	// Use defaults if not provided
	if retryCount < 0 {
		retryCount = defaults.APIRetryCount
	}
	if retryMaxDelay <= 0 {
		retryMaxDelay = defaults.APIRetryMaxDelay
	}
	if retryBaseInterval <= 0 {
		retryBaseInterval = defaults.APIRetryBaseInterval
	}
	DefaultMetaClient = NewClient(retryCount, retryMaxDelay, retryBaseInterval)
}

func (c *Client) sendRequest(path string) ([]byte, error) {
	ctx := logging.NewContext()
	reqURL := url.URL{
		Scheme: c.scheme,
		Host:   c.host,
		Path:   path,
	}

	var lastErr error
	for attempt := 0; attempt <= c.retryCount; attempt++ {
		req, err := http.NewRequest("GET", reqURL.String(), nil)
		if err != nil {
			return nil, err
		}

		userAgent := fmt.Sprintf("cce-network-v2/%s", version.GetCCEVersion().Version)
		req.Header.Set("User-Agent", userAgent)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			if !c.shouldRetry(attempt, nil, err) {
				return nil, fmt.Errorf("MetaAPI request failed after %d retries: %w", c.retryCount+1, err)
			}
			c.backoffAndRetry(ctx, "request failed", attempt, err)
			continue
		}
		defer resp.Body.Close()

		bodyContent, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			if !c.shouldRetry(attempt, resp, err) {
				return nil, fmt.Errorf("MetaAPI body read failed after %d retries: %w", c.retryCount+1, err)
			}
			c.backoffAndRetry(ctx, "body read failed", attempt, err)
			continue
		}

		if c.debug {
			log.WithContext(ctx).WithField("path", path).WithField("response", string(bodyContent)).Debug("curl metadata")
		}

		if resp.StatusCode >= http.StatusBadRequest {
			lastErr = fmt.Errorf("Error Message: \"%s\", Status Code: %d", string(bodyContent), resp.StatusCode)
			if !c.shouldRetry(attempt, resp, nil) {
				return bodyContent, lastErr
			}
			c.backoffAndRetry(ctx, "5xx error", attempt, lastErr)
			continue
		}

		if strings.TrimSpace(string(bodyContent)) == "" {
			return bodyContent, ErrorNotImplemented
		}

		return bodyContent, nil
	}

	return nil, lastErr
}

// shouldRetry determines if a request should be retried
// Aligns with VPC API retry logic in bce-sdk-go/bce/retry.go:
// - Retry on network errors (net.Error or resp == nil)
// - Retry on 5xx errors (500-599)
// - Retry on 421 (RateLimit)
// - Do not retry on 4xx errors (except 421)
func (c *Client) shouldRetry(attempt int, resp *http.Response, err error) bool {
	if attempt >= c.retryCount {
		return false
	}

	// Network error - always retry
	if err != nil {
		if _, ok := err.(net.Error); ok {
			return true
		}
	}

	// No response (network error) - always retry
	if resp == nil {
		return true
	}

	// Retry on RateLimit (421)
	if resp.StatusCode == 421 {
		return true
	}

	// Retry on all 5xx errors
	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		return true
	}

	// Do not retry on other status codes (2xx, 3xx, 4xx except 421)
	return false
}

// backoffAndRetry logs retry attempt, calculates backoff delay and sleeps
func (c *Client) backoffAndRetry(ctx context.Context, reason string, attempt int, err error) {
	delay := time.Duration((1 << uint(attempt))) * c.retryBaseInterval
	if delay > c.retryMaxDelay {
		delay = c.retryMaxDelay
	}
	log.WithContext(ctx).WithError(err).Warnf("MetaAPI %s (attempt %d/%d), retrying in %v",
		reason, attempt+1, c.retryCount+1, delay)
	time.Sleep(delay)
}

func (c *Client) GetInstanceID() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "instance-shortid")
	if err != nil {
		return "", err
	}
	instanceID := strings.TrimSpace(string(body))
	return instanceID, nil
}

func (c *Client) GetResourceID() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "source-resource-id")
	if err != nil {
		return "", err
	}
	resourceID := strings.TrimSpace(string(body))
	return resourceID, nil
}

func (c *Client) GetInstanceName() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "instance-name")
	if err != nil {
		return "", err
	}
	instanceID := strings.TrimSpace(string(body))
	return instanceID, nil
}

func (c *Client) GetInstanceType() (InstanceType, error) {
	bodyType, err := c.sendRequest(metadataBasePath + "instance-type")
	if err != nil {
		return "", err
	}
	typeStr := strings.TrimSpace(string(bodyType))
	return InstanceType(typeStr), nil
}

func (c *Client) GetInstanceSpecs() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "instance-specs")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
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

func (c *Client) GetSourceType() (string, error) {
	body, err := c.sendRequest(metadataBasePath + "source")
	if err != nil {
		return "", err
	}
	source := strings.TrimSpace(string(body))
	return source, nil
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

func (c *Client) GetLinkFixedIPs(macAddress string) ([]string, error) {
	// eg. /1.0/meta-data/network/interfaces/macs/fa:26:00:01:6f:37/fixed_ips
	// response:
	// 192.168.96.13/
	// 192.168.96.14/
	// 192.168.96.15/
	// 192.168.96.16/
	path := fmt.Sprintf(metadataBasePath+"network/interfaces/macs/%s/fixed_ips", macAddress)
	body, err := c.sendRequest(path)
	if err != nil {
		return nil, err
	}

	var (
		ipAddrs []string
	)

	reader := strings.NewReader(strings.TrimSpace(string(body)))
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		ipAddrs = append(ipAddrs, strings.TrimSuffix(line, "/"))
	}

	return ipAddrs, nil
}

func (c *Client) GetLinkPrimaryIP(macAddress string) (string, error) {
	ipAddrs, err := c.GetLinkFixedIPs(macAddress)
	if err != nil {
		return "", err
	}

	for _, addr := range ipAddrs {
		path := fmt.Sprintf(metadataBasePath+"network/interfaces/macs/%s/fixed_ips/%s/ip_type", macAddress, addr)
		body, err := c.sendRequest(path)
		if err != nil {
			return "", err
		}
		ipType := strings.TrimSpace(string(body))
		if ipType == IPTypePrimary {
			return addr, nil
		}
	}

	return "", errors.New("primary ip not found")
}

func (c *Client) GetLinkSecondaryIPs(macAddress string) ([]string, error) {
	primaryIP, err := c.GetLinkPrimaryIP(macAddress)
	if err != nil {
		return nil, err
	}

	ipAddrs, err := c.GetLinkFixedIPs(macAddress)
	if err != nil {
		return nil, err
	}

	var (
		secondaryIPs []string
	)

	for _, addr := range ipAddrs {
		if addr != primaryIP {
			secondaryIPs = append(secondaryIPs, addr)
		}
	}

	return secondaryIPs, nil
}

func (c *Client) GetVifFeatures(macAddress string) (string, error) {
	// eg. /1.0/meta-data/network/interfaces/macs/fa:26:00:01:6f:37/vif_features
	// response:
	//   elastic_rdma for eri
	//   rdma_roce    for hpc
	path := fmt.Sprintf(metadataBasePath+"network/interfaces/macs/%s/vif_features", macAddress)
	body, err := c.sendRequest(path)
	if err != nil {
		return "", err
	}
	vifFeatures := strings.TrimSpace(string(body))
	return vifFeatures, nil
}

func (c *Client) GetLinkMTU(macAddress string) (int, error) {
	// eg. /1.0/meta-data/network/interfaces/macs/fa:28:00:18:ab:51/mtu
	// response: 9000
	path := fmt.Sprintf(metadataBasePath+"network/interfaces/macs/%s/mtu", macAddress)
	body, err := c.sendRequest(path)
	if err != nil {
		return 0, err
	}
	mtuStr := strings.TrimSpace(string(body))
	mtu, err := strconv.Atoi(mtuStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse MTU value '%s': %v", mtuStr, err)
	}
	return mtu, nil
}

func (c *Client) GetFeatureList() (*FeatureList, error) {
	body, err := c.sendRequest(metadataBasePath + "feature_list")
	if err != nil {
		return nil, err
	}

	var featureList FeatureList
	if err := json.Unmarshal(body, &featureList); err != nil {
		return nil, fmt.Errorf("failed to parse feature list response: %v", err)
	}

	return &featureList, nil
}

func (c *Client) ListMacs() ([]string, error) {
	// eg. /1.0/meta-data/network/interfaces/macs
	// response:
	// fa:f6:00:01:7b:f4/
	// fa:f6:00:07:f8:f4/
	// fa:f6:00:08:13:e1/
	// fa:f6:00:14:4c:f0/
	path := fmt.Sprintf(metadataBasePath + "network/interfaces/macs")
	body, err := c.sendRequest(path)
	if err != nil {
		return nil, err
	}

	var (
		macAddrs []string
	)

	reader := strings.NewReader(strings.TrimSpace(string(body)))
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		macAddrs = append(macAddrs, strings.TrimSuffix(line, "/"))
	}

	return macAddrs, nil
}
