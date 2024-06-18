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

package v2

import (
	"fmt"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// errors for eni
	ErrorCodeENIIPCapacityExceed = "ENIIPCapacityExceed"
	ErrorCodeENISubnetNoMoreIP   = "SubnetNoMoreIP"
	ErrorCodeWaitNewENIInuse     = "WaitCreateMoreENI"
	ErrorCodeENICapacityExceed   = "ENICapacityExceed"
	ErrorCodeIPPoolExhausted     = "IPPoolExhausted"
	ErrorCodeNoMoreIP            = "NoMoreIP"
	ErrorCodeNoAvailableSubnet   = "NoAvailableSubnet"

	// errors for open api
	ErrorCodeOpenAPIError = "OpenAPIError"
	ErrorCodeSuccess      = "Success"
)

type CodeError struct {
	Code string `json:"code,omitempty"`
	Msg  string `json:"msg,omitempty"`
}

func NewCodeError(code, msg string) *CodeError {
	return &CodeError{
		Code: code,
		Msg:  msg,
	}
}

func (e *CodeError) Error() string {
	return fmt.Sprintf("code: %s, msg: %s", e.Code, e.Msg)
}

var _ error = &CodeError{}

// ErrorList holds a set of Errors.  It is plausible that we might one day have
// non-field errors in this same umbrella package, but for now we don't, so
// we can keep it simple and leave ErrorList here.
type CodeErrorList []*CodeError

// ToAggregate converts the ErrorList into an errors.Aggregate.
func (list CodeErrorList) ToAggregate() utilerrors.Aggregate {
	if len(list) == 0 {
		return nil
	}
	errs := make([]error, 0, len(list))
	errorMsgs := sets.NewString()
	for _, err := range list {
		msg := fmt.Sprintf("%v", err)
		if errorMsgs.Has(msg) {
			continue
		}
		errorMsgs.Insert(msg)
		errs = append(errs, err)
	}
	return utilerrors.NewAggregate(errs)
}

func fromAggregate(agg utilerrors.Aggregate) CodeErrorList {
	errs := agg.Errors()
	list := make(CodeErrorList, len(errs))
	for i := range errs {
		list[i] = errs[i].(*CodeError)
	}
	return list
}

// Filter removes items from the ErrorList that match the provided fns.
func (list CodeErrorList) Filter(fns ...utilerrors.Matcher) CodeErrorList {
	err := utilerrors.FilterOut(list.ToAggregate(), fns...)
	if err == nil {
		return nil
	}
	// FilterOut takes an Aggregate and returns an Aggregate
	return fromAggregate(err.(utilerrors.Aggregate))
}
