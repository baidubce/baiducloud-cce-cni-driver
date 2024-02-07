/*
 * Copyright 2021 Baidu, Inc.
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
 */

package hpc

import (
	"fmt"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

const (
	URI_PREFIX = bce.URI_PREFIX + "v1"

	DEFAULT_ENI = "bcc." + bce.DEFAULT_REGION + ".baidubce.com"

	REQUEST_ENI_URL = "/eni/hpc"
)

var (
	log = logging.NewSubysLogger("hpc-cloud")
)

// Client of ENI service is a kind of BceClient, so derived from BceClient
type Client struct {
	*bce.BceClient
}

func (c *Client) GetHPCEniID(instanceID string) (*EniList, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("The instanceID cannot be empty.")
	}

	log.WithField("instanceID", instanceID).Debug("GetHPCEniID")
	result := &EniList{}

	err := bce.NewRequestBuilder(c).
		WithURL(getURLForHpcEni()).
		WithMethod(http.GET).
		WithQueryParamFilter("instanceId", instanceID).
		WithResult(result).
		Do()

	log.WithField("result", result).Debug("GetHPCEniID client")
	return result, err
}

func (c *Client) BatchAddPrivateIPByHpc(args *EniBatchPrivateIPArgs) (*BatchAddPrivateIPResult, error) {
	if args == nil {
		return nil, fmt.Errorf("the hpcEni batch privateIP args cannot be nil")
	}

	log.WithField("args", args).Debug("The hpcEni batch privateIP args")
	result := &BatchAddPrivateIPResult{}
	err := bce.NewRequestBuilder(c).
		WithURL(getURLForHpcEni() + "/privateIp/batchAdd").
		WithMethod(http.PUT).
		WithBody(args).
		WithResult(result).
		Do()

	log.WithField("result", result).Debug("The hpcEni batch privateIP result")
	return result, err
}

func (c *Client) BatchDeletePrivateIPByHpc(args *EniBatchDeleteIPArgs) error {
	if args == nil {
		return fmt.Errorf("the hpcEni batch privateIP args cannot be nil")
	}
	log.WithField("args", args).Debug("The hpcEni batch delete privateIP request body argument (args)")

	err := bce.NewRequestBuilder(c).
		WithURL(getURLForHpcEni() + "/privateIp/batchDel").
		WithMethod(http.PUT).
		WithBody(args).
		Do()

	return err
}

func NewClient(ak, sk, endPoint string) (*Client, error) {
	if len(endPoint) == 0 {
		endPoint = DEFAULT_ENI
	}
	client, err := bce.NewBceClientWithAkSk(ak, sk, endPoint)
	if err != nil {
		return nil, err
	}
	return &Client{client}, nil
}

func getURLForHpcEni() string {
	return URI_PREFIX + REQUEST_ENI_URL
}
