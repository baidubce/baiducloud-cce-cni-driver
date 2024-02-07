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
	"net/http"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

// NewPostEniParams creates a new PostEniParams object
// no default values defined in spec.
func NewPostEniParams() PostEniParams {

	return PostEniParams{}
}

// PostEniParams contains all the bound params for the post eni operation
// typically these are obtained from a http.Request
//
// swagger:parameters PostEni
type PostEniParams struct {

	// HTTP Request Object
	HTTPRequest *http.Request `json:"-"`

	/*container id provider by cni
	  In: query
	*/
	ContainerID *string
	/*Expectations when applying for ENI
	  In: body
	*/
	Eni *models.ENI
	/*ifname provider by cni
	  In: query
	*/
	Ifname *string
	/*netns provider by cni
	  In: query
	*/
	Netns *string
	/*
	  In: query
	*/
	Owner *string
}

// BindRequest both binds and validates a request, it assumes that complex things implement a Validatable(strfmt.Registry) error interface
// for simple values it will use straight method calls.
//
// To ensure default values, the struct must have been initialized with NewPostEniParams() beforehand.
func (o *PostEniParams) BindRequest(r *http.Request, route *middleware.MatchedRoute) error {
	var res []error

	o.HTTPRequest = r

	qs := runtime.Values(r.URL.Query())

	qContainerID, qhkContainerID, _ := qs.GetOK("containerID")
	if err := o.bindContainerID(qContainerID, qhkContainerID, route.Formats); err != nil {
		res = append(res, err)
	}

	if runtime.HasBody(r) {
		defer r.Body.Close()
		var body models.ENI
		if err := route.Consumer.Consume(r.Body, &body); err != nil {
			res = append(res, errors.NewParseError("eni", "body", "", err))
		} else {
			// validate body object
			if err := body.Validate(route.Formats); err != nil {
				res = append(res, err)
			}

			if len(res) == 0 {
				o.Eni = &body
			}
		}
	}
	qIfname, qhkIfname, _ := qs.GetOK("ifname")
	if err := o.bindIfname(qIfname, qhkIfname, route.Formats); err != nil {
		res = append(res, err)
	}

	qNetns, qhkNetns, _ := qs.GetOK("netns")
	if err := o.bindNetns(qNetns, qhkNetns, route.Formats); err != nil {
		res = append(res, err)
	}

	qOwner, qhkOwner, _ := qs.GetOK("owner")
	if err := o.bindOwner(qOwner, qhkOwner, route.Formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

// bindContainerID binds and validates parameter ContainerID from query.
func (o *PostEniParams) bindContainerID(rawData []string, hasKey bool, formats strfmt.Registry) error {
	var raw string
	if len(rawData) > 0 {
		raw = rawData[len(rawData)-1]
	}

	// Required: false
	// AllowEmptyValue: false
	if raw == "" { // empty values pass all other validations
		return nil
	}

	o.ContainerID = &raw

	return nil
}

// bindIfname binds and validates parameter Ifname from query.
func (o *PostEniParams) bindIfname(rawData []string, hasKey bool, formats strfmt.Registry) error {
	var raw string
	if len(rawData) > 0 {
		raw = rawData[len(rawData)-1]
	}

	// Required: false
	// AllowEmptyValue: false
	if raw == "" { // empty values pass all other validations
		return nil
	}

	o.Ifname = &raw

	return nil
}

// bindNetns binds and validates parameter Netns from query.
func (o *PostEniParams) bindNetns(rawData []string, hasKey bool, formats strfmt.Registry) error {
	var raw string
	if len(rawData) > 0 {
		raw = rawData[len(rawData)-1]
	}

	// Required: false
	// AllowEmptyValue: false
	if raw == "" { // empty values pass all other validations
		return nil
	}

	o.Netns = &raw

	return nil
}

// bindOwner binds and validates parameter Owner from query.
func (o *PostEniParams) bindOwner(rawData []string, hasKey bool, formats strfmt.Registry) error {
	var raw string
	if len(rawData) > 0 {
		raw = rawData[len(rawData)-1]
	}

	// Required: false
	// AllowEmptyValue: false
	if raw == "" { // empty values pass all other validations
		return nil
	}

	o.Owner = &raw

	return nil
}
