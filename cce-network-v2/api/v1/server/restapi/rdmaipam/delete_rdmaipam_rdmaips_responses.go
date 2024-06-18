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

package rdmaipam

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

// DeleteRdmaipamRdmaipsOKCode is the HTTP code returned for type DeleteRdmaipamRdmaipsOK
const DeleteRdmaipamRdmaipsOKCode int = 200

/*
DeleteRdmaipamRdmaipsOK Success

swagger:response deleteRdmaipamRdmaipsOK
*/
type DeleteRdmaipamRdmaipsOK struct {
}

// NewDeleteRdmaipamRdmaipsOK creates DeleteRdmaipamRdmaipsOK with default headers values
func NewDeleteRdmaipamRdmaipsOK() *DeleteRdmaipamRdmaipsOK {

	return &DeleteRdmaipamRdmaipsOK{}
}

// WriteResponse to the client
func (o *DeleteRdmaipamRdmaipsOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.Header().Del(runtime.HeaderContentType) //Remove Content-Type on empty responses

	rw.WriteHeader(200)
}

// DeleteRdmaipamRdmaipsInvalidCode is the HTTP code returned for type DeleteRdmaipamRdmaipsInvalid
const DeleteRdmaipamRdmaipsInvalidCode int = 400

/*
DeleteRdmaipamRdmaipsInvalid Invalid IP address

swagger:response deleteRdmaipamRdmaipsInvalid
*/
type DeleteRdmaipamRdmaipsInvalid struct {
}

// NewDeleteRdmaipamRdmaipsInvalid creates DeleteRdmaipamRdmaipsInvalid with default headers values
func NewDeleteRdmaipamRdmaipsInvalid() *DeleteRdmaipamRdmaipsInvalid {

	return &DeleteRdmaipamRdmaipsInvalid{}
}

// WriteResponse to the client
func (o *DeleteRdmaipamRdmaipsInvalid) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.Header().Del(runtime.HeaderContentType) //Remove Content-Type on empty responses

	rw.WriteHeader(400)
}

// DeleteRdmaipamRdmaipsNotFoundCode is the HTTP code returned for type DeleteRdmaipamRdmaipsNotFound
const DeleteRdmaipamRdmaipsNotFoundCode int = 404

/*
DeleteRdmaipamRdmaipsNotFound IP address not found

swagger:response deleteRdmaipamRdmaipsNotFound
*/
type DeleteRdmaipamRdmaipsNotFound struct {
}

// NewDeleteRdmaipamRdmaipsNotFound creates DeleteRdmaipamRdmaipsNotFound with default headers values
func NewDeleteRdmaipamRdmaipsNotFound() *DeleteRdmaipamRdmaipsNotFound {

	return &DeleteRdmaipamRdmaipsNotFound{}
}

// WriteResponse to the client
func (o *DeleteRdmaipamRdmaipsNotFound) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.Header().Del(runtime.HeaderContentType) //Remove Content-Type on empty responses

	rw.WriteHeader(404)
}

// DeleteRdmaipamRdmaipsFailureCode is the HTTP code returned for type DeleteRdmaipamRdmaipsFailure
const DeleteRdmaipamRdmaipsFailureCode int = 500

/*
DeleteRdmaipamRdmaipsFailure Address release failure

swagger:response deleteRdmaipamRdmaipsFailure
*/
type DeleteRdmaipamRdmaipsFailure struct {

	/*
	  In: Body
	*/
	Payload models.Error `json:"body,omitempty"`
}

// NewDeleteRdmaipamRdmaipsFailure creates DeleteRdmaipamRdmaipsFailure with default headers values
func NewDeleteRdmaipamRdmaipsFailure() *DeleteRdmaipamRdmaipsFailure {

	return &DeleteRdmaipamRdmaipsFailure{}
}

// WithPayload adds the payload to the delete rdmaipam rdmaips failure response
func (o *DeleteRdmaipamRdmaipsFailure) WithPayload(payload models.Error) *DeleteRdmaipamRdmaipsFailure {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the delete rdmaipam rdmaips failure response
func (o *DeleteRdmaipamRdmaipsFailure) SetPayload(payload models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *DeleteRdmaipamRdmaipsFailure) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(500)
	payload := o.Payload
	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

// DeleteRdmaipamRdmaipsDisabledCode is the HTTP code returned for type DeleteRdmaipamRdmaipsDisabled
const DeleteRdmaipamRdmaipsDisabledCode int = 501

/*
DeleteRdmaipamRdmaipsDisabled Allocation for address family disabled

swagger:response deleteRdmaipamRdmaipsDisabled
*/
type DeleteRdmaipamRdmaipsDisabled struct {
}

// NewDeleteRdmaipamRdmaipsDisabled creates DeleteRdmaipamRdmaipsDisabled with default headers values
func NewDeleteRdmaipamRdmaipsDisabled() *DeleteRdmaipamRdmaipsDisabled {

	return &DeleteRdmaipamRdmaipsDisabled{}
}

// WriteResponse to the client
func (o *DeleteRdmaipamRdmaipsDisabled) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.Header().Del(runtime.HeaderContentType) //Remove Content-Type on empty responses

	rw.WriteHeader(501)
}
