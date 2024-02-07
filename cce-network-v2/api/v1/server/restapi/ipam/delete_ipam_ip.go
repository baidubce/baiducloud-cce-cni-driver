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
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// DeleteIpamIPHandlerFunc turns a function with the right signature into a delete ipam IP handler
type DeleteIpamIPHandlerFunc func(DeleteIpamIPParams) middleware.Responder

// Handle executing the request and returning a response
func (fn DeleteIpamIPHandlerFunc) Handle(params DeleteIpamIPParams) middleware.Responder {
	return fn(params)
}

// DeleteIpamIPHandler interface for that can handle valid delete ipam IP params
type DeleteIpamIPHandler interface {
	Handle(DeleteIpamIPParams) middleware.Responder
}

// NewDeleteIpamIP creates a new http.Handler for the delete ipam IP operation
func NewDeleteIpamIP(ctx *middleware.Context, handler DeleteIpamIPHandler) *DeleteIpamIP {
	return &DeleteIpamIP{Context: ctx, Handler: handler}
}

/*
DeleteIpamIP swagger:route DELETE /ipam/{ip} ipam deleteIpamIp

Release an allocated IP address
*/
type DeleteIpamIP struct {
	Context *middleware.Context
	Handler DeleteIpamIPHandler
}

func (o *DeleteIpamIP) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		r = rCtx
	}
	var Params = NewDeleteIpamIPParams()

	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request

	o.Context.Respond(rw, r, route.Produces, route, res)

}
