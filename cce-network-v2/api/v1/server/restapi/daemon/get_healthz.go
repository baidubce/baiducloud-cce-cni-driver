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

package daemon

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// GetHealthzHandlerFunc turns a function with the right signature into a get healthz handler
type GetHealthzHandlerFunc func(GetHealthzParams) middleware.Responder

// Handle executing the request and returning a response
func (fn GetHealthzHandlerFunc) Handle(params GetHealthzParams) middleware.Responder {
	return fn(params)
}

// GetHealthzHandler interface for that can handle valid get healthz params
type GetHealthzHandler interface {
	Handle(GetHealthzParams) middleware.Responder
}

// NewGetHealthz creates a new http.Handler for the get healthz operation
func NewGetHealthz(ctx *middleware.Context, handler GetHealthzHandler) *GetHealthz {
	return &GetHealthz{Context: ctx, Handler: handler}
}

/*
GetHealthz swagger:route GET /healthz daemon getHealthz

# Get health of CCE daemon

Returns health and status information of the CCE daemon and related
components such as the local container runtime, connected datastore,
Kubernetes integration and Hubble.
*/
type GetHealthz struct {
	Context *middleware.Context
	Handler GetHealthzHandler
}

func (o *GetHealthz) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		r = rCtx
	}
	var Params = NewGetHealthzParams()

	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request

	o.Context.Respond(rw, r, route.Produces, route, res)

}
