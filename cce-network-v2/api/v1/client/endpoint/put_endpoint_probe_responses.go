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
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

// PutEndpointProbeReader is a Reader for the PutEndpointProbe structure.
type PutEndpointProbeReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *PutEndpointProbeReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 201:
		result := NewPutEndpointProbeCreated()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 500:
		result := NewPutEndpointProbeFailure()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result

	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewPutEndpointProbeCreated creates a PutEndpointProbeCreated with default headers values
func NewPutEndpointProbeCreated() *PutEndpointProbeCreated {
	return &PutEndpointProbeCreated{}
}

/*
PutEndpointProbeCreated handles this case with default header values.

Success
*/
type PutEndpointProbeCreated struct {
	Payload *models.EndpointProbeResponse
}

func (o *PutEndpointProbeCreated) Error() string {
	return fmt.Sprintf("[PUT /endpoint/probe][%d] putEndpointProbeCreated  %+v", 201, o.Payload)
}

func (o *PutEndpointProbeCreated) GetPayload() *models.EndpointProbeResponse {
	return o.Payload
}

func (o *PutEndpointProbeCreated) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.EndpointProbeResponse)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewPutEndpointProbeFailure creates a PutEndpointProbeFailure with default headers values
func NewPutEndpointProbeFailure() *PutEndpointProbeFailure {
	return &PutEndpointProbeFailure{}
}

/*
PutEndpointProbeFailure handles this case with default header values.

update endpoint failed. Details in message.
*/
type PutEndpointProbeFailure struct {
	Payload models.Error
}

func (o *PutEndpointProbeFailure) Error() string {
	return fmt.Sprintf("[PUT /endpoint/probe][%d] putEndpointProbeFailure  %+v", 500, o.Payload)
}

func (o *PutEndpointProbeFailure) GetPayload() models.Error {
	return o.Payload
}

func (o *PutEndpointProbeFailure) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	// response payload
	if err := consumer.Consume(response.Body(), &o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
