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

package ipam

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

// PostIpamIPReader is a Reader for the PostIpamIP structure.
type PostIpamIPReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *PostIpamIPReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewPostIpamIPOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewPostIpamIPInvalid()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 409:
		result := NewPostIpamIPExists()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewPostIpamIPFailure()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 501:
		result := NewPostIpamIPDisabled()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result

	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewPostIpamIPOK creates a PostIpamIPOK with default headers values
func NewPostIpamIPOK() *PostIpamIPOK {
	return &PostIpamIPOK{}
}

/*
PostIpamIPOK handles this case with default header values.

Success
*/
type PostIpamIPOK struct {
}

func (o *PostIpamIPOK) Error() string {
	return fmt.Sprintf("[POST /ipam/{ip}][%d] postIpamIpOK ", 200)
}

func (o *PostIpamIPOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewPostIpamIPInvalid creates a PostIpamIPInvalid with default headers values
func NewPostIpamIPInvalid() *PostIpamIPInvalid {
	return &PostIpamIPInvalid{}
}

/*
PostIpamIPInvalid handles this case with default header values.

Invalid IP address
*/
type PostIpamIPInvalid struct {
}

func (o *PostIpamIPInvalid) Error() string {
	return fmt.Sprintf("[POST /ipam/{ip}][%d] postIpamIpInvalid ", 400)
}

func (o *PostIpamIPInvalid) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewPostIpamIPExists creates a PostIpamIPExists with default headers values
func NewPostIpamIPExists() *PostIpamIPExists {
	return &PostIpamIPExists{}
}

/*
PostIpamIPExists handles this case with default header values.

IP already allocated
*/
type PostIpamIPExists struct {
}

func (o *PostIpamIPExists) Error() string {
	return fmt.Sprintf("[POST /ipam/{ip}][%d] postIpamIpExists ", 409)
}

func (o *PostIpamIPExists) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewPostIpamIPFailure creates a PostIpamIPFailure with default headers values
func NewPostIpamIPFailure() *PostIpamIPFailure {
	return &PostIpamIPFailure{}
}

/*
PostIpamIPFailure handles this case with default header values.

IP allocation failure. Details in message.
*/
type PostIpamIPFailure struct {
	Payload models.Error
}

func (o *PostIpamIPFailure) Error() string {
	return fmt.Sprintf("[POST /ipam/{ip}][%d] postIpamIpFailure  %+v", 500, o.Payload)
}

func (o *PostIpamIPFailure) GetPayload() models.Error {
	return o.Payload
}

func (o *PostIpamIPFailure) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	// response payload
	if err := consumer.Consume(response.Body(), &o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewPostIpamIPDisabled creates a PostIpamIPDisabled with default headers values
func NewPostIpamIPDisabled() *PostIpamIPDisabled {
	return &PostIpamIPDisabled{}
}

/*
PostIpamIPDisabled handles this case with default header values.

Allocation for address family disabled
*/
type PostIpamIPDisabled struct {
}

func (o *PostIpamIPDisabled) Error() string {
	return fmt.Sprintf("[POST /ipam/{ip}][%d] postIpamIpDisabled ", 501)
}

func (o *PostIpamIPDisabled) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}
