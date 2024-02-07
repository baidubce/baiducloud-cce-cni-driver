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

// GetEndpointExtpluginStatusReader is a Reader for the GetEndpointExtpluginStatus structure.
type GetEndpointExtpluginStatusReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *GetEndpointExtpluginStatusReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewGetEndpointExtpluginStatusOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 500:
		result := NewGetEndpointExtpluginStatusFailure()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result

	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewGetEndpointExtpluginStatusOK creates a GetEndpointExtpluginStatusOK with default headers values
func NewGetEndpointExtpluginStatusOK() *GetEndpointExtpluginStatusOK {
	return &GetEndpointExtpluginStatusOK{}
}

/*
GetEndpointExtpluginStatusOK handles this case with default header values.

Success
*/
type GetEndpointExtpluginStatusOK struct {
	Payload models.ExtFeatureData
}

func (o *GetEndpointExtpluginStatusOK) Error() string {
	return fmt.Sprintf("[GET /endpoint/extplugin/status][%d] getEndpointExtpluginStatusOK  %+v", 200, o.Payload)
}

func (o *GetEndpointExtpluginStatusOK) GetPayload() models.ExtFeatureData {
	return o.Payload
}

func (o *GetEndpointExtpluginStatusOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	// response payload
	if err := consumer.Consume(response.Body(), &o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewGetEndpointExtpluginStatusFailure creates a GetEndpointExtpluginStatusFailure with default headers values
func NewGetEndpointExtpluginStatusFailure() *GetEndpointExtpluginStatusFailure {
	return &GetEndpointExtpluginStatusFailure{}
}

/*
GetEndpointExtpluginStatusFailure handles this case with default header values.

failed to get external plugin status. Details in message.
*/
type GetEndpointExtpluginStatusFailure struct {
	Payload models.Error
}

func (o *GetEndpointExtpluginStatusFailure) Error() string {
	return fmt.Sprintf("[GET /endpoint/extplugin/status][%d] getEndpointExtpluginStatusFailure  %+v", 500, o.Payload)
}

func (o *GetEndpointExtpluginStatusFailure) GetPayload() models.Error {
	return o.Payload
}

func (o *GetEndpointExtpluginStatusFailure) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	// response payload
	if err := consumer.Consume(response.Body(), &o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
