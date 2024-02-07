// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/baidu/jpaas-caas/baiducloud-cce-cni-driver/pkg/apimachinery/networking/subnet.go - Subnet object manipulation tool

// modification history
// --------------------
// 2022/08/01, by wangeweiwei22, create subnet

// Subnet object manipulation tool

package networking

import (
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

// Whether pod complies with the topology distribution of pod subnet
func IsSubnetTopologySpreadPod(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}
	return GetPodSubnetTopologySpreadName(pod) != ""
}

func GetPodSubnetTopologySpreadName(pod *corev1.Pod) string {
	if v, ok := pod.Annotations[AnnotationPodSubnetTopologySpread]; ok {
		return v
	}
	return ""
}

func IsFixedIPMode(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	var strategy = GetPSTSStrategy(psts)
	return strategy.Type == networkingv1alpha1.IPAllocTypeFixed
}

func IsManualMode(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	var strategy = GetPSTSStrategy(psts)
	return strategy.Type == networkingv1alpha1.IPAllocTypeManual
}

func IsElasticMode(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	var strategy = GetPSTSStrategy(psts)
	return strategy.Type == networkingv1alpha1.IPAllocTypeElastic || strategy.Type == ""
}

// IsReuseIPCustomPSTS whether to enable the reuse IP mode
func IsReuseIPCustomPSTS(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	return IsCustomMode(psts) && psts.Spec.Strategy.EnableReuseIPAddress
}

// IsCustomMode psts type is custom
func IsCustomMode(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	return psts.Spec.Strategy != nil && psts.Spec.Strategy.Type == networkingv1alpha1.IPAllocTypeCustom
}

// GetReleaseStrategy defaults to release strategy is TTL
func GetReleaseStrategy(psts *networkingv1alpha1.PodSubnetTopologySpread) networkingv1alpha1.ReleaseStrategy {
	return GetPSTSStrategy(psts).ReleaseStrategy
}

// GetPSTSStrategy Recursively obtain the IP application policy
func GetPSTSStrategy(psts *networkingv1alpha1.PodSubnetTopologySpread) *networkingv1alpha1.IPAllocationStrategy {
	var strategy *networkingv1alpha1.IPAllocationStrategy
	if psts.Spec.Strategy != nil {
		strategy = psts.Spec.Strategy
	} else {
		for _, sub := range psts.Spec.Subnets {
			strategy = &sub.IPAllocationStrategy
		}
	}

	if strategy.Type == networkingv1alpha1.IPAllocTypeNil {
		strategy.Type = networkingv1alpha1.IPAllocTypeElastic
	}
	if strategy.ReleaseStrategy == "" {
		strategy.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyTTL
	}
	return strategy
}

func IsEndWithNum(name string) bool {
	names := strings.Split(name, "-")
	_, err := strconv.Atoi(names[len(names)-1])
	return err == nil
}

func OwnerByPodSubnetTopologySpread(wep *networkingv1alpha1.WorkloadEndpoint, psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	if wep == nil {
		return false
	}
	return wep.GetNamespace() == psts.GetNamespace() && wep.Spec.SubnetTopologyReference == psts.Name
}

// PSTSContainsAvailableSubnet whatever length of the available subnet of psts greater than 0
func PSTSContainsAvailableSubnet(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	return len(psts.Status.AvailableSubnets) > 0
}

// PSTSMode slecte mod of psts and return true if a available subnet in psts
func PSTSMode(psts *networkingv1alpha1.PodSubnetTopologySpread) (*networkingv1alpha1.IPAllocationStrategy, bool) {
	if len(psts.Status.AvailableSubnets) == 0 {
		return nil, false
	}
	return GetPSTSStrategy(psts), true
}
