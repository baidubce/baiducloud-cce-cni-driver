package hpas

import (
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/http"
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
	// 如果 HPAS 标准版开关未开启，则使用 hpas-id wrapper
	if !operatorOption.Config.HPASStanderd {
		req.SetHeader("cce-instance-type", string(metadata.InstanceTypeExHPAS))
	}
	b.Signer.Sign(req, cred, opt)
}
