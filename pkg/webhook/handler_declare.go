// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/baidu/jpaas-caas/cce-accelerate-image/pkg/webhook/handler_declare.go - declare all webhook handler

// modification history
// --------------------
// 2022/05/23, by wangeweiwei22, create handler_declare

// declare all webhook handler
// such as :
// validating webhook for pod subnet topology spread
// mutating webhook for pod

package webhook

import (
	podmutating "github.com/baidubce/baiducloud-cce-cni-driver/pkg/webhook/pod/mutating"
	pstsvalidating "github.com/baidubce/baiducloud-cce-cni-driver/pkg/webhook/psts/validating"
)

func init() {
	addHandlersWithGate(pstsvalidating.HandlerMap, func() (enabled bool) {
		return true
	})
	addHandlersWithGate(podmutating.HandlerMap, func() (enabled bool) {
		return true
	})
}
