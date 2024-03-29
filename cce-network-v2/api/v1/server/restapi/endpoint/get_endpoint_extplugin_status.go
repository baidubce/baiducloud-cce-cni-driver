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
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// GetEndpointExtpluginStatusHandlerFunc turns a function with the right signature into a get endpoint extplugin status handler
type GetEndpointExtpluginStatusHandlerFunc func(GetEndpointExtpluginStatusParams) middleware.Responder

// Handle executing the request and returning a response
func (fn GetEndpointExtpluginStatusHandlerFunc) Handle(params GetEndpointExtpluginStatusParams) middleware.Responder {
	return fn(params)
}

// GetEndpointExtpluginStatusHandler interface for that can handle valid get endpoint extplugin status params
type GetEndpointExtpluginStatusHandler interface {
	Handle(GetEndpointExtpluginStatusParams) middleware.Responder
}

// NewGetEndpointExtpluginStatus creates a new http.Handler for the get endpoint extplugin status operation
func NewGetEndpointExtpluginStatus(ctx *middleware.Context, handler GetEndpointExtpluginStatusHandler) *GetEndpointExtpluginStatus {
	return &GetEndpointExtpluginStatus{Context: ctx, Handler: handler}
}

/*
GetEndpointExtpluginStatus swagger:route GET /endpoint/extplugin/status endpoint getEndpointExtpluginStatus

get external plugin status
*/
type GetEndpointExtpluginStatus struct {
	Context *middleware.Context
	Handler GetEndpointExtpluginStatusHandler
}

func (o *GetEndpointExtpluginStatus) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		r = rCtx
	}
	var Params = NewGetEndpointExtpluginStatusParams()

	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request

	o.Context.Respond(rw, r, route.Produces, route, res)

}
