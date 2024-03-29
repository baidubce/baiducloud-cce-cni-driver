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

package eni

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

// DeleteEniReader is a Reader for the DeleteEni structure.
type DeleteEniReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *DeleteEniReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewDeleteEniOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewDeleteEniInvalid()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 404:
		result := NewDeleteEniNotFound()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewDeleteEniFailure()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 501:
		result := NewDeleteEniDisabled()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result

	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewDeleteEniOK creates a DeleteEniOK with default headers values
func NewDeleteEniOK() *DeleteEniOK {
	return &DeleteEniOK{}
}

/*
DeleteEniOK handles this case with default header values.

Success
*/
type DeleteEniOK struct {
}

func (o *DeleteEniOK) Error() string {
	return fmt.Sprintf("[DELETE /eni][%d] deleteEniOK ", 200)
}

func (o *DeleteEniOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteEniInvalid creates a DeleteEniInvalid with default headers values
func NewDeleteEniInvalid() *DeleteEniInvalid {
	return &DeleteEniInvalid{}
}

/*
DeleteEniInvalid handles this case with default header values.

Invalid IP address
*/
type DeleteEniInvalid struct {
}

func (o *DeleteEniInvalid) Error() string {
	return fmt.Sprintf("[DELETE /eni][%d] deleteEniInvalid ", 400)
}

func (o *DeleteEniInvalid) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteEniNotFound creates a DeleteEniNotFound with default headers values
func NewDeleteEniNotFound() *DeleteEniNotFound {
	return &DeleteEniNotFound{}
}

/*
DeleteEniNotFound handles this case with default header values.

IP address not found
*/
type DeleteEniNotFound struct {
}

func (o *DeleteEniNotFound) Error() string {
	return fmt.Sprintf("[DELETE /eni][%d] deleteEniNotFound ", 404)
}

func (o *DeleteEniNotFound) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteEniFailure creates a DeleteEniFailure with default headers values
func NewDeleteEniFailure() *DeleteEniFailure {
	return &DeleteEniFailure{}
}

/*
DeleteEniFailure handles this case with default header values.

Address release failure
*/
type DeleteEniFailure struct {
	Payload models.Error
}

func (o *DeleteEniFailure) Error() string {
	return fmt.Sprintf("[DELETE /eni][%d] deleteEniFailure  %+v", 500, o.Payload)
}

func (o *DeleteEniFailure) GetPayload() models.Error {
	return o.Payload
}

func (o *DeleteEniFailure) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	// response payload
	if err := consumer.Consume(response.Body(), &o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewDeleteEniDisabled creates a DeleteEniDisabled with default headers values
func NewDeleteEniDisabled() *DeleteEniDisabled {
	return &DeleteEniDisabled{}
}

/*
DeleteEniDisabled handles this case with default header values.

Allocation for address family disabled
*/
type DeleteEniDisabled struct {
}

func (o *DeleteEniDisabled) Error() string {
	return fmt.Sprintf("[DELETE /eni][%d] deleteEniDisabled ", 501)
}

func (o *DeleteEniDisabled) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}
