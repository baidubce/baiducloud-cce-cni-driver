package cloud

import (
	"errors"
	"net/http"
	"testing"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/stretchr/testify/require"
)

func TestReasonForError(t *testing.T) {
	cases := []struct {
		msg      string
		expected ErrorReason
	}{
		{"PrivateIPNotExistException", ErrorReasonENIPrivateIPNotFound},
		{"EniIdException", ErrorReasonENINotFound},
		{"SubnetHasNoMoreIpException", ErrorReasonSubnetHasNoMoreIP},
		{"RateLimit", ErrorReasonRateLimit},
		{"NoSuchObject", ErrorReasonNoSuchObject},
		{"IpReserve.IpReserveIdInvalid", ErrorReasonIPReserveNotFound},
		{"VmMemoryCanNotAttachMoreIpException", ErrorReasonVmMemoryCanNotAttachMoreIpException},
		{"ExceedLimitException", ErrorReasonBBCENIPrivateIPExceedLimit},
		{"PrivateIpInUseException", ErrorReasonPrivateIPInUse},
		{"RouteRuleRepeated", ErrorReasonRouteRuleRepeated},
		{"QuotaLimitExceeded", ErrorReasonQuotaLimitExceeded},
		{"RouteRuleExceedQuota", ErrorReasonNoSRouteRuleExceedQuota},
	}
	for _, tc := range cases {
		require.Equal(t, tc.expected, ReasonForError(errors.New(tc.msg)))
	}
	// case-insensitive match
	require.Equal(t, ErrorReasonENIPrivateIPNotFound, ReasonForError(errors.New("privateipnotexistexception")))
	// unknown
	require.Equal(t, ErrorReasonUnknown, ReasonForError(errors.New("something else")))
	// nil
	require.Equal(t, ErrorReasonUnknown, ReasonForError(nil))
}

func TestIsIPAlreadyDeleted(t *testing.T) {
	require.False(t, IsIPAlreadyDeleted(nil))
	require.True(t, IsIPAlreadyDeleted(errors.New("NoSuchObject")))
	require.True(t, IsIPAlreadyDeleted(errors.New("eni not found")))
	require.True(t, IsIPAlreadyDeleted(errors.New("PrivateIPNotExistException")))
	require.False(t, IsIPAlreadyDeleted(errors.New("other error")))
}

func TestCaseInsensitiveContains(t *testing.T) {
	require.True(t, caseInsensitiveContains("HelloWorld", "world"))
	require.False(t, caseInsensitiveContains("HelloWorld", "abc"))
}

func TestIsErrorPSTSCrossSubnetCandidateConflict(t *testing.T) {
	require.False(t, IsErrorPSTSCrossSubnetCandidateConflict(nil))
	require.True(t, IsErrorPSTSCrossSubnetCandidateConflict(errors.New("[Code: BadRequest; Message: Bad request parameters or illegal request.; RequestId: 123]")))
	require.False(t, IsErrorPSTSCrossSubnetCandidateConflict(errors.New("[Code: BadRequest; Message: invalid parameter.]")))
	require.False(t, IsErrorPSTSCrossSubnetCandidateConflict(errors.New("BadRequest")))
	require.False(t, IsErrorPSTSCrossSubnetCandidateConflict(errors.New("PrivateIpInUseException")))

	require.True(t, IsErrorPSTSCrossSubnetCandidateConflict(&bce.BceServiceError{
		Code:    "BadRequest",
		Message: "Bad request parameters or illegal request.",
	}))
	require.False(t, IsErrorPSTSCrossSubnetCandidateConflict(&bce.BceServiceError{
		Code:    "BadRequest",
		Message: "invalid parameter",
	}))
}

func TestBceServiceErrorToHTTPCode(t *testing.T) {
	require.Equal(t, http.StatusOK, BceServiceErrorToHTTPCode(nil))
	require.Equal(t, 0, BceServiceErrorToHTTPCode(errors.New("not bce error")))
	require.Equal(t, http.StatusNotFound, BceServiceErrorToHTTPCode(&bce.BceServiceError{StatusCode: http.StatusNotFound}))
}
