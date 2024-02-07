/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package cloud

import (
	"net/http"
	"strings"

	"github.com/baidubce/bce-sdk-go/bce"
)

type ErrorReason string

const (
	ErrorReasonUnknown                             ErrorReason = "Unknown"
	ErrorReasonENIPrivateIPNotFound                ErrorReason = "ENIPrivateIPNotFound"
	ErrorReasonENINotFound                         ErrorReason = "ENINotFound"
	ErrorReasonSubnetHasNoMoreIP                   ErrorReason = "SubnetHasNoMoreIP"
	ErrorReasonRateLimit                           ErrorReason = "RateLimit"
	ErrorReasonPrivateIPInUse                      ErrorReason = "PrivateIPInUse"
	ErrorReasonVmMemoryCanNotAttachMoreIpException ErrorReason = "VmMemoryCanNotAttachMoreIpException"
	ErrorReasonBBCENIPrivateIPNotFound             ErrorReason = "BBCENIPrivateIPNotFound"
	ErrorReasonBBCENIPrivateIPExceedLimit          ErrorReason = "BBCENIPrivateIPExceedLimit"
	ErrorReasonRouteRuleRepeated                   ErrorReason = "RouteRuleRepeated"
	ErrorReasonQuotaLimitExceeded                  ErrorReason = "QuotaLimitExceeded"
	ErrorReasonNoSuchObject                        ErrorReason = "NoSuchObject"
)

func ReasonForError(err error) ErrorReason {
	if err != nil {
		errMsg := err.Error()
		switch {
		case caseInsensitiveContains(errMsg, "PrivateIPNotExistException"):
			return ErrorReasonENIPrivateIPNotFound
		case caseInsensitiveContains(errMsg, "EniIdException"):
			return ErrorReasonENINotFound
		case caseInsensitiveContains(errMsg, "SubnetHasNoMoreIpException"):
			return ErrorReasonSubnetHasNoMoreIP
		case caseInsensitiveContains(errMsg, "RateLimit"):
			return ErrorReasonRateLimit
		case caseInsensitiveContains(errMsg, "NoSuchObject") || caseInsensitiveContains(errMsg, "is invalid"):
			// TODO: remove BadRequest when IaaS fixes their API
			return ErrorReasonBBCENIPrivateIPNotFound
		case caseInsensitiveContains(errMsg, "VmMemoryCanNotAttachMoreIpException"):
			return ErrorReasonVmMemoryCanNotAttachMoreIpException
		case caseInsensitiveContains(errMsg, "ExceedLimitException") || caseInsensitiveContains(errMsg, "BadRequest"):
			// TODO: remove BadRequest when IaaS fixes their API
			return ErrorReasonBBCENIPrivateIPExceedLimit
		case caseInsensitiveContains(errMsg, "PrivateIpInUseException"):
			return ErrorReasonPrivateIPInUse
		case caseInsensitiveContains(errMsg, "RouteRuleRepeated"):
			return ErrorReasonRouteRuleRepeated
		case caseInsensitiveContains(errMsg, "QuotaLimitExceeded"):
			return ErrorReasonQuotaLimitExceeded
		case caseInsensitiveContains(errMsg, "ErrorReasonNoSuchObject"):
			return ErrorReasonNoSuchObject
		}
	}
	return ErrorReasonUnknown
}

// IsErrorENIPrivateIPNotFound 判定删除辅助 IP 的 err 是否是因为 IP 不属于弹性网卡
func IsErrorENIPrivateIPNotFound(err error) bool {
	return ReasonForError(err) == ErrorReasonENIPrivateIPNotFound
}

func IsErrorENINotFound(err error) bool {
	return ReasonForError(err) == ErrorReasonENINotFound
}

func IsErrorSubnetHasNoMoreIP(err error) bool {
	return ReasonForError(err) == ErrorReasonSubnetHasNoMoreIP
}

func IsErrorRateLimit(err error) bool {
	return ReasonForError(err) == ErrorReasonRateLimit
}

func IsErrorReasonNoSuchObject(err error) bool {
	return ReasonForError(err) == ErrorReasonNoSuchObject
}

func IsErrorPrivateIPInUse(err error) bool {
	return ReasonForError(err) == ErrorReasonPrivateIPInUse
}

func IsErrorVmMemoryCanNotAttachMoreIpException(err error) bool {
	return ReasonForError(err) == ErrorReasonVmMemoryCanNotAttachMoreIpException
}

func IsErrorBBCENIPrivateIPNotFound(err error) bool {
	return ReasonForError(err) == ErrorReasonBBCENIPrivateIPNotFound
}

func IsErrorBBCENIPrivateIPExceedLimit(err error) bool {
	return ReasonForError(err) == ErrorReasonBBCENIPrivateIPExceedLimit
}

func IsErrorRouteRuleRepeated(err error) bool {
	return ReasonForError(err) == ErrorReasonRouteRuleRepeated
}

func IsErrorQuotaLimitExceeded(err error) bool {
	return ReasonForError(err) == ErrorReasonQuotaLimitExceeded
}

func caseInsensitiveContains(s, substr string) bool {
	s, substr = strings.ToLower(s), strings.ToLower(substr)
	return strings.Contains(s, substr)
}

func BceServiceErrorToHTTPCode(err error) int {
	if err == nil {
		return http.StatusOK
	}

	bceErr, ok := err.(*bce.BceServiceError)
	if ok {
		return bceErr.StatusCode
	}

	return 0
}
