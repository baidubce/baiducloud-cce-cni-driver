package networking

import (
	corev1 "k8s.io/api/core/v1"
)

var (
	AnnotationCCEPrefix = "cce.baidubce.com/"
	// annotation for PodSubnetTopologySpread
	AnnotationPodSubnetTopologySpread = AnnotationCCEPrefix + "PodSubnetTopologySpread"

	// cce defined k8s resource name
	ResourceIPForNode  = corev1.ResourceName(AnnotationCCEPrefix + "ip")
	ResourceENIForNode = corev1.ResourceName(AnnotationCCEPrefix + "eni")

	// CrossVPCEni resource name
	ResourceCrossVPCEni = corev1.ResourceName("cross-vpc-eni.cce.io/eni")

	// topodlogy key for psts
	TopologyKeyOfPod = "topology.kubernetes.io/zone"
	// AnnotationDisablePSTSPodAffinity This annotation is included on the pod, which means that the pod does not expect to use the scheduling function extended by pSTS
	AnnotationDisablePSTSPodAffinity = AnnotationCCEPrefix + "DisablePodSubnetTopologySpreadScheduler"
)
