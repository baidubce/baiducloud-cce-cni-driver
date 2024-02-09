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
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

// PutEndpointProbeCreatedCode is the HTTP code returned for type PutEndpointProbeCreated
const PutEndpointProbeCreatedCode int = 201

/*
PutEndpointProbeCreated Success

swagger:response putEndpointProbeCreated
*/
type PutEndpointProbeCreated struct {

	/*
	  In: Body
	*/
	Payload *models.EndpointProbeResponse `json:"body,omitempty"`
}

// NewPutEndpointProbeCreated creates PutEndpointProbeCreated with default headers values
func NewPutEndpointProbeCreated() *PutEndpointProbeCreated {

	return &PutEndpointProbeCreated{}
}

// WithPayload adds the payload to the put endpoint probe created response
func (o *PutEndpointProbeCreated) WithPayload(payload *models.EndpointProbeResponse) *PutEndpointProbeCreated {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the put endpoint probe created response
func (o *PutEndpointProbeCreated) SetPayload(payload *models.EndpointProbeResponse) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *PutEndpointProbeCreated) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(201)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// PutEndpointProbeFailureCode is the HTTP code returned for type PutEndpointProbeFailure
const PutEndpointProbeFailureCode int = 500

/*
PutEndpointProbeFailure update endpoint failed. Details in message.

swagger:response putEndpointProbeFailure
*/
type PutEndpointProbeFailure struct {

	/*
	  In: Body
	*/
	Payload models.Error `json:"body,omitempty"`
}

// NewPutEndpointProbeFailure creates PutEndpointProbeFailure with default headers values
func NewPutEndpointProbeFailure() *PutEndpointProbeFailure {

	return &PutEndpointProbeFailure{}
}

// WithPayload adds the payload to the put endpoint probe failure response
func (o *PutEndpointProbeFailure) WithPayload(payload models.Error) *PutEndpointProbeFailure {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the put endpoint probe failure response
func (o *PutEndpointProbeFailure) SetPayload(payload models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *PutEndpointProbeFailure) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(500)
	payload := o.Payload
	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}