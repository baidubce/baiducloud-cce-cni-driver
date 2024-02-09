// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/baidu/jpaas-caas/baiducloud-cce-cni-driver/pkg/apimachinery/networking/subnet.go - Subnet object manipulation tool

// modification history
// --------------------
// 2022/08/01, by wangeweiwei22, create subnet

// Subnet object manipulation tool

package networking

import (
	"net"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8sutilnet "k8s.io/utils/net"

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
	if len(psts.Spec.Subnets) != 1 {
		return false
	}
	for _, sub := range psts.Spec.Subnets {
		if sub.Type == networkingv1alpha1.IPAllocTypeFixed {
			return true
		}
	}
	return false
}

func IsManualMode(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	for _, sub := range psts.Spec.Subnets {
		if sub.Type == networkingv1alpha1.IPAllocTypeManual {
			return true
		}
	}
	return false
}

func IsElasticMode(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	for _, sub := range psts.Spec.Subnets {
		if sub.Type == networkingv1alpha1.IPAllocTypeElastic || sub.Type == "" {
			return true
		}
	}
	return false
}

func GetReleaseStrategy(psts *networkingv1alpha1.PodSubnetTopologySpread) networkingv1alpha1.ReleaseStrategy {
	for _, v := range psts.Spec.Subnets {
		return v.ReleaseStrategy
	}
	return networkingv1alpha1.ReleaseStrategyTTL
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

func PSTSContainsAvailableSubnet(psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	return len(psts.Status.AvailableSubnets) > 0
}

func PSTSContainersIP(ip string, psts *networkingv1alpha1.PodSubnetTopologySpread) bool {
	netIP := net.ParseIP(ip)
	for _, sbn := range psts.Spec.Subnets {
		for _, v := range sbn.IPv4 {
			if v == ip {
				return true
			}
		}

		cidrs, err := k8sutilnet.ParseCIDRs(sbn.IPv4Range)
		if err == nil {
			for _, cidr := range cidrs {
				if cidr.Contains(netIP) {
					return true
				}
			}
		}

		for _, v := range sbn.IPv6 {
			if v == ip {
				return true
			}
		}

		cidrs, err = k8sutilnet.ParseCIDRs(sbn.IPv6Range)
		if err == nil {
			for _, cidr := range cidrs {
				if cidr.Contains(netIP) {
					return true
				}
			}
		}
	}
	return false
}
