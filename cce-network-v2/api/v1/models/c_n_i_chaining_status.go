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

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"encoding/json"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
)

// CNIChainingStatus Status of CNI chaining
//
// +k8s:deepcopy-gen=true
//
// swagger:model CNIChainingStatus
type CNIChainingStatus struct {

	// mode
	// Enum: [none aws-cni flannel generic-veth portmap]
	Mode string `json:"mode,omitempty"`
}

// Validate validates this c n i chaining status
func (m *CNIChainingStatus) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateMode(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

var cNIChainingStatusTypeModePropEnum []interface{}

func init() {
	var res []string
	if err := json.Unmarshal([]byte(`["none","aws-cni","flannel","generic-veth","portmap"]`), &res); err != nil {
		panic(err)
	}
	for _, v := range res {
		cNIChainingStatusTypeModePropEnum = append(cNIChainingStatusTypeModePropEnum, v)
	}
}

const (

	// CNIChainingStatusModeNone captures enum value "none"
	CNIChainingStatusModeNone string = "none"

	// CNIChainingStatusModeAwsCni captures enum value "aws-cni"
	CNIChainingStatusModeAwsCni string = "aws-cni"

	// CNIChainingStatusModeFlannel captures enum value "flannel"
	CNIChainingStatusModeFlannel string = "flannel"

	// CNIChainingStatusModeGenericVeth captures enum value "generic-veth"
	CNIChainingStatusModeGenericVeth string = "generic-veth"

	// CNIChainingStatusModePortmap captures enum value "portmap"
	CNIChainingStatusModePortmap string = "portmap"
)

// prop value enum
func (m *CNIChainingStatus) validateModeEnum(path, location string, value string) error {
	if err := validate.EnumCase(path, location, value, cNIChainingStatusTypeModePropEnum, true); err != nil {
		return err
	}
	return nil
}

func (m *CNIChainingStatus) validateMode(formats strfmt.Registry) error {

	if swag.IsZero(m.Mode) { // not required
		return nil
	}

	// value enum
	if err := m.validateModeEnum("mode", "body", m.Mode); err != nil {
		return err
	}

	return nil
}

// MarshalBinary interface implementation
func (m *CNIChainingStatus) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *CNIChainingStatus) UnmarshalBinary(b []byte) error {
	var res CNIChainingStatus
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
