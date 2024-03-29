// Code generated by go-swagger; DO NOT EDIT.

/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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

package operator

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"
)

// GetHealthzOKCode is the HTTP code returned for type GetHealthzOK
const GetHealthzOKCode int = 200

/*
GetHealthzOK CCE operator is healthy

swagger:response getHealthzOK
*/
type GetHealthzOK struct {

	/*
	  In: Body
	*/
	Payload string `json:"body,omitempty"`
}

// NewGetHealthzOK creates GetHealthzOK with default headers values
func NewGetHealthzOK() *GetHealthzOK {

	return &GetHealthzOK{}
}

// WithPayload adds the payload to the get healthz o k response
func (o *GetHealthzOK) WithPayload(payload string) *GetHealthzOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get healthz o k response
func (o *GetHealthzOK) SetPayload(payload string) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetHealthzOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	payload := o.Payload
	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

// GetHealthzInternalServerErrorCode is the HTTP code returned for type GetHealthzInternalServerError
const GetHealthzInternalServerErrorCode int = 500

/*
GetHealthzInternalServerError CCE operator is not healthy

swagger:response getHealthzInternalServerError
*/
type GetHealthzInternalServerError struct {

	/*
	  In: Body
	*/
	Payload string `json:"body,omitempty"`
}

// NewGetHealthzInternalServerError creates GetHealthzInternalServerError with default headers values
func NewGetHealthzInternalServerError() *GetHealthzInternalServerError {

	return &GetHealthzInternalServerError{}
}

// WithPayload adds the payload to the get healthz internal server error response
func (o *GetHealthzInternalServerError) WithPayload(payload string) *GetHealthzInternalServerError {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get healthz internal server error response
func (o *GetHealthzInternalServerError) SetPayload(payload string) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetHealthzInternalServerError) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(500)
	payload := o.Payload
	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}
