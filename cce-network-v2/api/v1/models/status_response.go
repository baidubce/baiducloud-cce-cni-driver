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

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
)

// StatusResponse Health and status information of daemon
//
// +k8s:deepcopy-gen=true
//
// swagger:model StatusResponse
type StatusResponse struct {

	// Status of CCE daemon
	Cce *Status `json:"cce,omitempty"`

	// When supported by the API, this client ID should be used by the
	// client when making another request to the server.
	// See for example "/cluster/nodes".
	//
	ClientID string `json:"client-id,omitempty"`

	// Status of local container runtime
	ContainerRuntime *Status `json:"container-runtime,omitempty"`

	// Status of all endpoint controllers
	Controllers ControllerStatuses `json:"controllers,omitempty"`

	// List of stale information in the status
	Stale map[string]strfmt.DateTime `json:"stale,omitempty"`
}

// Validate validates this status response
func (m *StatusResponse) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateCce(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateContainerRuntime(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateControllers(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateStale(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *StatusResponse) validateCce(formats strfmt.Registry) error {

	if swag.IsZero(m.Cce) { // not required
		return nil
	}

	if m.Cce != nil {
		if err := m.Cce.Validate(formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("cce")
			}
			return err
		}
	}

	return nil
}

func (m *StatusResponse) validateContainerRuntime(formats strfmt.Registry) error {

	if swag.IsZero(m.ContainerRuntime) { // not required
		return nil
	}

	if m.ContainerRuntime != nil {
		if err := m.ContainerRuntime.Validate(formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("container-runtime")
			}
			return err
		}
	}

	return nil
}

func (m *StatusResponse) validateControllers(formats strfmt.Registry) error {

	if swag.IsZero(m.Controllers) { // not required
		return nil
	}

	if err := m.Controllers.Validate(formats); err != nil {
		if ve, ok := err.(*errors.Validation); ok {
			return ve.ValidateName("controllers")
		}
		return err
	}

	return nil
}

func (m *StatusResponse) validateStale(formats strfmt.Registry) error {

	if swag.IsZero(m.Stale) { // not required
		return nil
	}

	for k := range m.Stale {

		if err := validate.FormatOf("stale"+"."+k, "body", "date-time", m.Stale[k].String(), formats); err != nil {
			return err
		}

	}

	return nil
}

// MarshalBinary interface implementation
func (m *StatusResponse) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *StatusResponse) UnmarshalBinary(b []byte) error {
	var res StatusResponse
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}