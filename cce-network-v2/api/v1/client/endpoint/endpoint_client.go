// Code generated by go-swagger; DO NOT EDIT.

// /*
//  * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
//  *
//  * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  * except in compliance with the License. You may obtain a copy of the License at
//  *
//  * http://www.apache.org/licenses/LICENSE-2.0
//  *
//  * Unless required by applicable law or agreed to in writing, software distributed under the
//  * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  * either express or implied. See the License for the specific language governing permissions
//  * and limitations under the License.
//  *
//  */

package endpoint

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
)

// New creates a new endpoint API client.
func New(transport runtime.ClientTransport, formats strfmt.Registry) ClientService {
	return &Client{transport: transport, formats: formats}
}

/*
Client for endpoint API
*/
type Client struct {
	transport runtime.ClientTransport
	formats   strfmt.Registry
}

// ClientService is the interface for Client methods
type ClientService interface {
	GetEndpointExtpluginStatus(params *GetEndpointExtpluginStatusParams) (*GetEndpointExtpluginStatusOK, error)

	PutEndpointProbe(params *PutEndpointProbeParams) (*PutEndpointProbeCreated, error)

	SetTransport(transport runtime.ClientTransport)
}

/*
GetEndpointExtpluginStatus gets external plugin status
*/
func (a *Client) GetEndpointExtpluginStatus(params *GetEndpointExtpluginStatusParams) (*GetEndpointExtpluginStatusOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewGetEndpointExtpluginStatusParams()
	}

	result, err := a.transport.Submit(&runtime.ClientOperation{
		ID:                 "GetEndpointExtpluginStatus",
		Method:             "GET",
		PathPattern:        "/endpoint/extplugin/status",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"http"},
		Params:             params,
		Reader:             &GetEndpointExtpluginStatusReader{formats: a.formats},
		Context:            params.Context,
		Client:             params.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	success, ok := result.(*GetEndpointExtpluginStatusOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for GetEndpointExtpluginStatus: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

/*
PutEndpointProbe creates or update endpint probe
*/
func (a *Client) PutEndpointProbe(params *PutEndpointProbeParams) (*PutEndpointProbeCreated, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewPutEndpointProbeParams()
	}

	result, err := a.transport.Submit(&runtime.ClientOperation{
		ID:                 "PutEndpointProbe",
		Method:             "PUT",
		PathPattern:        "/endpoint/probe",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"http"},
		Params:             params,
		Reader:             &PutEndpointProbeReader{formats: a.formats},
		Context:            params.Context,
		Client:             params.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	success, ok := result.(*PutEndpointProbeCreated)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for PutEndpointProbe: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

// SetTransport changes the transport on the client
func (a *Client) SetTransport(transport runtime.ClientTransport) {
	a.transport = transport
}
