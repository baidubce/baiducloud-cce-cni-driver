package ccegateway

import (
	"fmt"

	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
)

type accessKeyPairViaGatewaySigner struct {
	bceSigner        auth.Signer
	ccegatewaySigner *bceSigner
}

func NewAccessKeyViaGatewaySigner(signer auth.Signer) auth.Signer {
	return &accessKeyPairViaGatewaySigner{
		bceSigner:        signer,
		ccegatewaySigner: NewBCESigner("", ""),
	}
}

func (as *accessKeyPairViaGatewaySigner) Sign(req *http.Request, cred *auth.BceCredentials, opt *auth.SignOptions) {
	// add default content-type if not set
	if req.Header("Content-Type") == "" {
		req.SetHeader("Content-Type", "application/json")
	}

	// add x-bce-request-id if not set
	if req.Header(http.BCE_REQUEST_ID) == "" {
		req.SetHeader(http.BCE_REQUEST_ID, util.NewRequestId())
	}
	gatewayHost, gatewayPort := as.ccegatewaySigner.GetHostAndPort()
	reqHost := fmt.Sprintf("%s:%d", gatewayHost, gatewayPort)
	// make Sign idempotent as it may be invoked many times on retry
	if req.Host() != reqHost {
		req.SetHeader(RemoteHostHeaderKey, req.Host())
		req.SetHeader(http.HOST, reqHost)
		req.SetHost(reqHost)
	}
	as.bceSigner.Sign(req, cred, opt)
}
