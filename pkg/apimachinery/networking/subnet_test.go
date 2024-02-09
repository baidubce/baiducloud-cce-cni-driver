package networking

import (
	"testing"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestIsSubnetTopologySpreadPod(t *testing.T) {
	pod := &corev1.Pod{}
	IsSubnetTopologySpreadPod(pod)
	pod.Annotations = map[string]string{"": ""}
	IsSubnetTopologySpreadPod(pod)
	pod.Annotations[AnnotationPodSubnetTopologySpread] = "true"
	IsSubnetTopologySpreadPod(pod)
}

func TestIsFixedIPMode(t *testing.T) {
	l := labels.Set{}
	psts := data.MockPodSubnetTopologySpread("default", "psts-test", "sbn-test", l)
	if !IsElasticMode(psts) {
		t.Errorf("IsElasticMode")
	}
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type: networkingv1alpha1.IPAllocTypeManual,
	}
	if !IsManualMode(psts) {
		t.Errorf("IsManualMode")
	}
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:            networkingv1alpha1.IPAllocTypeFixed,
		ReleaseStrategy: networkingv1alpha1.ReleaseStrategyNever,
	}
	if !IsFixedIPMode(psts) {
		t.Errorf("IsFixedIPMode")
	}

	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:            networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy: networkingv1alpha1.ReleaseStrategyTTL,
	}
	if !IsCustomMode(psts) {
		t.Errorf("IsCustomMode")
	}

	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:                 networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy:      networkingv1alpha1.ReleaseStrategyTTL,
		EnableReuseIPAddress: true,
	}
	if !IsReuseIPCustomPSTS(psts) {
		t.Errorf("IsCustomMode")
	}

	psts.Spec.Strategy.Type = networkingv1alpha1.IPAllocTypeNil
	GetReleaseStrategy(psts)
	psts.Spec.Strategy = nil
	GetReleaseStrategy(psts)

	OwnerByPodSubnetTopologySpread(nil, psts)
	wep := data.MockFixedWorkloadEndpoint()
	wep.Spec.SubnetTopologyReference = "psts-test"
	if !OwnerByPodSubnetTopologySpread(wep, psts) {
		t.Errorf("OwnerByPodSubnetTopologySpread")
	}

	if !IsEndWithNum("a-0") {
		t.Errorf("IsEndWithNum")
	}
	PSTSMode(psts)
	psts.Status.AvailableSubnets = nil
	PSTSMode(psts)
	if PSTSContainsAvailableSubnet(psts) {
		t.Errorf("PSTSContainsAvailableSubnet")
	}
}
