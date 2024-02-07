package event

import (
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

type EndpointProbeEventHandler interface {
	AcceptType() EndpointProbeEventType
	Handle(event *EndpointProbeEvent) (*ccev2.ExtFeatureStatus, error)
}

type EndpointProbeEventType string

const (
	EndpointProbeEventBandwidth      = "bandwidth"
	EndpointProbeEventEgressPriority = "egresspriority"
)

type EndpointProbeEvent struct {
	ID   string
	Obj  *ccev2.CCEEndpoint
	Type EndpointProbeEventType

	// result of probe
	result *ccev2.ExtFeatureStatus
	Err    error
}
