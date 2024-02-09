package networking

import (
	"time"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
)

// IsCustomReuseModeWEP WorkloadEndpoint type is custom
func ISCustomReuseModeWEP(wep *networkingv1alpha1.WorkloadEndpoint) bool {
	return wep.Spec.Type == ipamgeneric.WepTypeReuseIPPod && wep.Spec.EnableFixIP == "True"
}

func NeedReleaseReuseModeWEP(wep *networkingv1alpha1.WorkloadEndpoint) bool {
	if wep.Spec.Phase == networkingv1alpha1.WorkloadEndpointPhasePodDeleted &&
		wep.Spec.Release != nil &&
		wep.Spec.Release.PodDeletedTime != nil {
		deleteTime := wep.Spec.Release.PodDeletedTime
		ttl := wep.Spec.Release.TTL
		return deleteTime.Add(ttl.Duration).After(time.Now())
	}
	return false
}

func IsFixIPStatefulSetPodWep(wep *networkingv1alpha1.WorkloadEndpoint) bool {
	return wep.Spec.Type == ipamgeneric.WepTypeSts && wep.Spec.EnableFixIP == "True"
}
