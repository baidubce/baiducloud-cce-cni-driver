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

package metrics

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/operator/models"
)

// GetMetricsOKCode is the HTTP code returned for type GetMetricsOK
const GetMetricsOKCode int = 200

/*
GetMetricsOK Success

swagger:response getMetricsOK
*/
type GetMetricsOK struct {

	/*
	  In: Body
	*/
	Payload []*models.Metric `json:"body,omitempty"`
}

// NewGetMetricsOK creates GetMetricsOK with default headers values
func NewGetMetricsOK() *GetMetricsOK {

	return &GetMetricsOK{}
}

// WithPayload adds the payload to the get metrics o k response
func (o *GetMetricsOK) WithPayload(payload []*models.Metric) *GetMetricsOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get metrics o k response
func (o *GetMetricsOK) SetPayload(payload []*models.Metric) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetMetricsOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	payload := o.Payload
	if payload == nil {
		// return empty array
		payload = make([]*models.Metric, 0, 50)
	}

	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

// GetMetricsFailedCode is the HTTP code returned for type GetMetricsFailed
const GetMetricsFailedCode int = 500

/*
GetMetricsFailed Metrics cannot be retrieved

swagger:response getMetricsFailed
*/
type GetMetricsFailed struct {
}

// NewGetMetricsFailed creates GetMetricsFailed with default headers values
func NewGetMetricsFailed() *GetMetricsFailed {

	return &GetMetricsFailed{}
}

// WriteResponse to the client
func (o *GetMetricsFailed) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.Header().Del(runtime.HeaderContentType) //Remove Content-Type on empty responses

	rw.WriteHeader(500)
}
