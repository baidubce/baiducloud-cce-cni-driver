/*
 * Copyright 2023 Baidu, Inc.
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

package bci

import (
	"github.com/baidubce/bce-sdk-go/bce"
)

const (
	URI_PREFIX_v2  = bce.URI_PREFIX + "v2"
	DEFAULT_PREFIX = URI_PREFIX_v2

	DEFAULT_BCI_ENDPOINT = "bci." + bce.DEFAULT_REGION + ".baidubce.com"

	REQUEST_BCI_URL = "/instance"
)

// Client of BCI service is a kind of BceClient, so derived from BceClient
type Client struct {
	*bce.BceClient
}

func NewClient(ak, sk, endPoint string) (*Client, error) {
	if len(endPoint) == 0 {
		endPoint = DEFAULT_BCI_ENDPOINT
	}
	client, err := bce.NewBceClientWithAkSk(ak, sk, endPoint)
	if err != nil {
		return nil, err
	}
	return &Client{client}, nil
}

func getURLForBci() string {
	return DEFAULT_PREFIX + REQUEST_BCI_URL
}

func getURLForBciId(bciId string) string {
	return getURLForBci() + "/" + bciId
}
