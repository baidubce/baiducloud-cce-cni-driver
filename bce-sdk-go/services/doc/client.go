/*
 * Copyright 2022 Baidu, Inc.
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

// client.go - define the client for DOC service
// Package doc defines the DOC services of BCE. The supported APIs are all defined in sub-package

package doc

import (
	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/doc/api"
)

const (
	DEFAULT_SERVICE_DOMAIN = "doc.bj.baidubce.com"
)

// Client of DOC service is a kind of BceClient, so derived from BceClient
type Client struct {
	*bce.BceClient
}

// DocClientConfiguration defines the config components structure by user.
type DocClientConfiguration struct {
	Ak       string
	Sk       string
	Endpoint string
}

// NewClient make the DOC service client with default configuration.
// Use `cli.Config.xxx` to access the config or change it to non-default value.
func NewClient(ak, sk string) (*Client, error) {
	return NewClientWithConfig(&DocClientConfiguration{
		Ak:       ak,
		Sk:       sk,
		Endpoint: DEFAULT_SERVICE_DOMAIN,
	})
}

func NewClientWithConfig(config *DocClientConfiguration) (*Client, error) {
	var credentials *auth.BceCredentials
	var err error
	ak, sk, endpoint := config.Ak, config.Sk, config.Endpoint
	if len(ak) == 0 && len(sk) == 0 { // to support public-read-write request
		credentials, err = nil, nil
	} else {
		credentials, err = auth.NewBceCredentials(ak, sk)
		if err != nil {
			return nil, err
		}
	}
	if len(endpoint) == 0 {
		endpoint = DEFAULT_SERVICE_DOMAIN
	}
	defaultSignOptions := &auth.SignOptions{
		HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
		ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS}
	defaultConf := &bce.BceClientConfiguration{
		Endpoint:                  endpoint,
		Region:                    bce.DEFAULT_REGION,
		UserAgent:                 bce.DEFAULT_USER_AGENT,
		Credentials:               credentials,
		SignOption:                defaultSignOptions,
		Retry:                     bce.DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: bce.DEFAULT_CONNECTION_TIMEOUT_IN_MILLIS,
		RedirectDisabled:          false}
	v1Signer := &auth.BceV1Signer{}

	client := &Client{bce.NewBceClient(defaultConf, v1Signer)}
	return client, nil
}

// RegisterDocument - register document in doc service
//
// PARAMS:
//   - regParam title and format of the document being registered
//
// RETURNS:
//   - *api.RegDocumentResp: id and document location in bos
//   - error: the return error if any occurs
func (c *Client) RegisterDocument(regParam *api.RegDocumentParam) (*api.RegDocumentResp, error) {
	return api.RegisterDocument(c, regParam)
}

// PublishDocument - publish document
//
// PARAMS:
//   - documentId: id of document in doc service
//
// RETURNS:
//   - error: the return error if any occurs
func (c *Client) PublishDocument(documentId string) error {
	return api.PublishDocument(c, documentId)
}

// QueryDocument - query document's status
//
// PARAMS:
//   - documentId: id of document in doc service
//   - queryParam: enable/disable https of coverl url
//
// RETURNS:
//   - *api.QueryDocumentResp
//   - error: the return error if any occurs
func (c *Client) QueryDocument(documentId string, queryParam *api.QueryDocumentParam) (*api.QueryDocumentResp, error) {
	return api.QueryDocument(c, documentId, queryParam)
}

// ReadDocument - get document token for client sdk
//
// PARAMS:
//   - documentId: id of document in doc service
//   - readParam: expiration time of the doc's html
//
// RETURNS:
//   - *api.ReadDocumentResp
//   - error: the return error if any occurs
func (c *Client) ReadDocument(documentId string, readParam *api.ReadDocumentParam) (*api.ReadDocumentResp, error) {
	return api.ReadDocument(c, documentId, readParam)
}

// GetImages - Get the list of images generated by the document conversion
//
// PARAMS:
//   - documentId: id of document in doc service
//
// RETURNS:
//   - *api.ImagesListResp
//   - error: the return error if any occurs
func (c *Client) GetImages(documentId string) (*api.GetImagesResp, error) {
	return api.GetImages(c, documentId)
}

// DeleteDocument - delete document in doc service
//
// PARAMS:
//   - documentId: id of document in doc service
//
// RETURNS:
//   - error: the return error if any occurs
func (c *Client) DeleteDocument(documentId string) error {
	return api.DeleteDocument(c, documentId)
}

// ListDocuments - list all documents
//
// PARAMS:
//   - cli: the client agent which can perform sending request
//   - param: the optional arguments to list documents
//
// RETURNS:
//   - *ListDocumentsResp: the result docments list structure
//   - error: nil if ok otherwise the specific error
func (c *Client) ListDocuments(listParam *api.ListDocumentsParam) (*api.ListDocumentsResp, error) {
	return api.ListDocuments(c, listParam)
}
