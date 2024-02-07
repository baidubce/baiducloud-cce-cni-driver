// Copyright Authors of Baidu AI Cloud
// SPDX-License-Identifier: Apache-2.0
package enim

import (
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/agent"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/enim/eniprovider"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
)

type noopENIM struct {
	// provider provides the actual ENI allocate and release logic
	provider eniprovider.ENIProvider
}

func (*noopENIM) ADD(owner, containerID, netnsPath string) (ipv4Addr, ipv6Addr *models.IPAMAddressResponse, err error) {
	return nil, nil, fmt.Errorf("noop enim")
}
func (*noopENIM) DEL(owner, containerID, netnsPath string) error {
	return fmt.Errorf("noop enim")
}
func (*noopENIM) GC() error {
	return nil
}

var _ ENIManagerServer = &noopENIM{}

func NewENIInitFactoryENIM(c ipam.Configuration, watcher *watchers.K8sWatcher) ENIManagerServer {
	enim := &noopENIM{}
	agent.RegisterENIInitFatory(watcher)
	return enim
}
