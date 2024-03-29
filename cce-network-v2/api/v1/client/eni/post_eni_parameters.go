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
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
)

// NewPostEniParams creates a new PostEniParams object
// with the default values initialized.
func NewPostEniParams() *PostEniParams {
	var ()
	return &PostEniParams{

		timeout: cr.DefaultTimeout,
	}
}

// NewPostEniParamsWithTimeout creates a new PostEniParams object
// with the default values initialized, and the ability to set a timeout on a request
func NewPostEniParamsWithTimeout(timeout time.Duration) *PostEniParams {
	var ()
	return &PostEniParams{

		timeout: timeout,
	}
}

// NewPostEniParamsWithContext creates a new PostEniParams object
// with the default values initialized, and the ability to set a context for a request
func NewPostEniParamsWithContext(ctx context.Context) *PostEniParams {
	var ()
	return &PostEniParams{

		Context: ctx,
	}
}

// NewPostEniParamsWithHTTPClient creates a new PostEniParams object
// with the default values initialized, and the ability to set a custom HTTPClient for a request
func NewPostEniParamsWithHTTPClient(client *http.Client) *PostEniParams {
	var ()
	return &PostEniParams{
		HTTPClient: client,
	}
}

/*
PostEniParams contains all the parameters to send to the API endpoint
for the post eni operation typically these are written to a http.Request
*/
type PostEniParams struct {

	/*ContainerID
	  container id provider by cni

	*/
	ContainerID *string
	/*Eni
	  Expectations when applying for ENI

	*/
	Eni *models.ENI
	/*Ifname
	  ifname provider by cni

	*/
	Ifname *string
	/*Netns
	  netns provider by cni

	*/
	Netns *string
	/*Owner*/
	Owner *string

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithTimeout adds the timeout to the post eni params
func (o *PostEniParams) WithTimeout(timeout time.Duration) *PostEniParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the post eni params
func (o *PostEniParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the post eni params
func (o *PostEniParams) WithContext(ctx context.Context) *PostEniParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the post eni params
func (o *PostEniParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the post eni params
func (o *PostEniParams) WithHTTPClient(client *http.Client) *PostEniParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the post eni params
func (o *PostEniParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithContainerID adds the containerID to the post eni params
func (o *PostEniParams) WithContainerID(containerID *string) *PostEniParams {
	o.SetContainerID(containerID)
	return o
}

// SetContainerID adds the containerId to the post eni params
func (o *PostEniParams) SetContainerID(containerID *string) {
	o.ContainerID = containerID
}

// WithEni adds the eni to the post eni params
func (o *PostEniParams) WithEni(eni *models.ENI) *PostEniParams {
	o.SetEni(eni)
	return o
}

// SetEni adds the eni to the post eni params
func (o *PostEniParams) SetEni(eni *models.ENI) {
	o.Eni = eni
}

// WithIfname adds the ifname to the post eni params
func (o *PostEniParams) WithIfname(ifname *string) *PostEniParams {
	o.SetIfname(ifname)
	return o
}

// SetIfname adds the ifname to the post eni params
func (o *PostEniParams) SetIfname(ifname *string) {
	o.Ifname = ifname
}

// WithNetns adds the netns to the post eni params
func (o *PostEniParams) WithNetns(netns *string) *PostEniParams {
	o.SetNetns(netns)
	return o
}

// SetNetns adds the netns to the post eni params
func (o *PostEniParams) SetNetns(netns *string) {
	o.Netns = netns
}

// WithOwner adds the owner to the post eni params
func (o *PostEniParams) WithOwner(owner *string) *PostEniParams {
	o.SetOwner(owner)
	return o
}

// SetOwner adds the owner to the post eni params
func (o *PostEniParams) SetOwner(owner *string) {
	o.Owner = owner
}

// WriteToRequest writes these params to a swagger request
func (o *PostEniParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	if o.ContainerID != nil {

		// query param containerID
		var qrContainerID string
		if o.ContainerID != nil {
			qrContainerID = *o.ContainerID
		}
		qContainerID := qrContainerID
		if qContainerID != "" {
			if err := r.SetQueryParam("containerID", qContainerID); err != nil {
				return err
			}
		}

	}

	if o.Eni != nil {
		if err := r.SetBodyParam(o.Eni); err != nil {
			return err
		}
	}

	if o.Ifname != nil {

		// query param ifname
		var qrIfname string
		if o.Ifname != nil {
			qrIfname = *o.Ifname
		}
		qIfname := qrIfname
		if qIfname != "" {
			if err := r.SetQueryParam("ifname", qIfname); err != nil {
				return err
			}
		}

	}

	if o.Netns != nil {

		// query param netns
		var qrNetns string
		if o.Netns != nil {
			qrNetns = *o.Netns
		}
		qNetns := qrNetns
		if qNetns != "" {
			if err := r.SetQueryParam("netns", qNetns); err != nil {
				return err
			}
		}

	}

	if o.Owner != nil {

		// query param owner
		var qrOwner string
		if o.Owner != nil {
			qrOwner = *o.Owner
		}
		qOwner := qrOwner
		if qOwner != "" {
			if err := r.SetQueryParam("owner", qOwner); err != nil {
				return err
			}
		}

	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
