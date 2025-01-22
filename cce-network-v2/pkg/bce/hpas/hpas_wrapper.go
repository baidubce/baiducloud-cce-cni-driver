package hpas

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/bce-sdk-go/http"

	"github.com/baidubce/bce-sdk-go/auth"
)

type HPASWrapper struct {
	auth.Signer
}

func NewHPASSignWrapper(signer auth.Signer) *HPASWrapper {
	return &HPASWrapper{
		Signer: signer,
	}
}

func (b *HPASWrapper) Sign(req *http.Request, cred *auth.BceCredentials, opt *auth.SignOptions) {
	req.SetHeader("cce-instance-type", string(metadata.InstanceTypeExHPAS))
	b.Signer.Sign(req, cred, opt)
}
