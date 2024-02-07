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
	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
)

// IPAMStatus Status of IP address management
//
// +k8s:deepcopy-gen=true
//
// swagger:model IPAMStatus
type IPAMStatus struct {

	// allocations
	Allocations AllocationMap `json:"allocations,omitempty"`

	// ipv4
	IPV4 []string `json:"ipv4"`

	// ipv6
	IPV6 []string `json:"ipv6"`

	// status
	Status string `json:"status,omitempty"`
}

// Validate validates this IP a m status
func (m *IPAMStatus) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateAllocations(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *IPAMStatus) validateAllocations(formats strfmt.Registry) error {

	if swag.IsZero(m.Allocations) { // not required
		return nil
	}

	if err := m.Allocations.Validate(formats); err != nil {
		if ve, ok := err.(*errors.Validation); ok {
			return ve.ValidateName("allocations")
		}
		return err
	}

	return nil
}

// MarshalBinary interface implementation
func (m *IPAMStatus) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *IPAMStatus) UnmarshalBinary(b []byte) error {
	var res IPAMStatus
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
