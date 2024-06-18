/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package k8s

import (
	"strconv"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	CCEPrefix = "cce.baidubce.com/"

	// LabelNodeInstanceType type of k8s node
	LabelNodeInstanceType = "node.kubernetes.io/instance-type"

	// LabelPodUseFixedIP Only the pod marked with this annotation when creating the pod will enable the fixed IP
	LabelPodUseFixedIP = "cce.baidubce.com/fixedip"
	LabelNodeName      = "cce.baidubce.com/node"
	LabelInstanceID    = "cce.baidubce.com/instanceid"
	LabelContainerID   = "cce.baidubce.com/containerid"

	// AnnotationFixedIPTTLSeconds If the fixed IP is stuck for a long time when the pod fails, IP recycling will be triggered
	// default value is 6040800 (7)
	AnnotationFixedIPTTLSeconds = "fixedip.cce.baidubce.com/ttl"

	// AnnotationNodeAnnotationSynced node have synced with cloud
	AnnotationNodeAnnotationSynced = "node.cce.baidubce.com/annotation-synced"

	// AnnotationNodeLabelSynced node have synced with cloud
	AnnotationCCEInstanceLabel = "kubernetes.io/cce.instance.labels"

	// AnnotationNodeLabelSynced node speicified subnets id use to create eni and allocate ip
	AnnotationNodeEniSubnetIDs = "network.cce.baidubce.com/node-eni-subnet-ids"

	AnnotationNodeMaxENINum       = "network.cce.baidubce.com/node-max-eni-num"
	AnnotationNodeMaxPerENIIPsNum = "network.cce.baidubce.com/node-eni-max-ips-num"

	// FinalizerOfCCEEndpointRemoteIP finalizer to remove ip from remote iaas
	FinalizerOfCCEEndpointRemoteIP = "RemoteIPFinalizer"

	// FinalizerOfNetResourceSetRoute finalizer to remove ip from remote iaas
	FinalizerOfNetResourceSetRoute = "RemoteRouteFinalizer"

	// LabelENIUseMode is the label used to store the ENI use mode of the node.
	// if the label is set before the NetResourceSet is created, we wiil use the label
	// value as the ENI use mode of the node.
	LabelENIUseMode = "cce.baidubce.com/eni-use-mode"
	LabelENIType    = "cce.baidubce.com/eni-type"

	// VPCIDLabel is the label used to store the VPC ID of the node.
	VPCIDLabel = "cce.baidubce.com/vpc-id"

	// AnnotationIPResourceCapacitySynced is the annotation used to store the ip resource capacity synced status of the node.
	AnnotationIPResourceCapacitySynced = "cce.baidubce.com/ip-resource-capacity-synced"

	// LabelAvailableZone is the label used to store the available zone of the node.
	LabelAvailableZone = "cce.baidubce.com/available-zone"
	LabelRegion        = "topology.kubernetes.io/region"
	LabelZone          = "topology.kubernetes.io/zone"

	// LabelOwnerByReference this label is used to mark the owner of the resource.
	// for example, if a psts is created by a cpsts, the label will be set to
	LabelOwnerByReference = CCEPrefix + "owner-by-reference"
)

var (
	// LabelPodUseFixedIP use fixed ip
	ValueStringTrue = "true"

	// annotation for PodSubnetTopologySpread
	AnnotationPodSubnetTopologySpread = CCEPrefix + "PodSubnetTopologySpread"

	// cce defined k8s resource name
	ResourceIPForNode  = corev1.ResourceName(CCEPrefix + "ip")
	ResourceENIForNode = corev1.ResourceName(CCEPrefix + "eni")

	// CrossVPCEni resource name
	ResourceCrossVPCEni = corev1.ResourceName("cross-vpc-eni.cce.io/eni")

	// topodlogy key for psts
	TopologyKeyOfPod = "topology.kubernetes.io/zone"
	// AnnotationDisablePSTSPodAffinity This annotation is included on the pod, which means that the pod does not expect to use the scheduling function extended by pSTS
	AnnotationDisablePSTSPodAffinity = CCEPrefix + "DisablePodSubnetTopologySpreadScheduler"

	// AnnotationExternalENI means ENI was created by external system
	AnnotationExternalENI = "cce.baidubce.com/external-eni"

	// AnnotationExternalENI means ENI primary IP was created by cce
	AnnotationENIIPv6PrimaryIP = "cce.baidubce.com/ipv6-primary-ip"
)

// crossvpc labels
var (
	PodAnnotationCrossVPCEniUserID                          = "cross-vpc-eni.cce.io/userID"
	PodAnnotationCrossVPCEniSubnetID                        = "cross-vpc-eni.cce.io/subnetID"
	PodAnnotationCrossVPCEniSecurityGroupIDs                = "cross-vpc-eni.cce.io/securityGroupIDs"
	PodAnnotationCrossVPCEniPrivateIPAddress                = "cross-vpc-eni.cce.io/privateIPAddress"
	PodAnnotationCrossVPCEniVPCCIDR                         = "cross-vpc-eni.cce.io/vpcCidr"
	PodAnnotationCrossVPCEniDefaultRouteInterfaceDelegation = "cross-vpc-eni.cce.io/defaultRouteInterfaceDelegation"
	PodAnnotationCrossVPCEniDefaultRouteExcludedCidrs       = "cross-vpc-eni.cce.io/defaultRouteExcludedCidrs"

	NodeAnnotationMaxCrossVPCEni = "cross-vpc-eni.cce.io/maxEniNumber"
	NodeLabelMaxCrossVPCEni      = "cross-vpc-eni.cce.io/max-eni-number"

	necessaryAnnoKeyList = []string{
		PodAnnotationCrossVPCEniUserID,
		PodAnnotationCrossVPCEniSubnetID,
		PodAnnotationCrossVPCEniVPCCIDR,
		PodAnnotationCrossVPCEniSecurityGroupIDs,
	}

	PodLabelOwnerNamespace = "cce.io/ownerNamespace"
	PodLabelOwnerName      = "cce.io/ownerName"
	PodLabelOwnerNode      = "cce.io/ownerNode"
	PodLabelOwnerInstance  = "cce.io/ownerInstance"
)

// ExtractFixedIPTTLSeconds Extract the expiration time of the fixed
// IP from the annotation of the pod. The unit is s
func ExtractFixedIPTTLSeconds(pod *corev1.Pod) int64 {
	if len(pod.Annotations) != 0 {
		if v, ok := pod.Annotations[AnnotationFixedIPTTLSeconds]; ok {
			if seconds, e := strconv.ParseInt(v, 10, 64); e == nil {
				return seconds
			}
		}
	}
	return 0
}

func HaveFixedIPLabel(obj metav1.Object) bool {
	if obj == nil {
		return false
	}
	if len(obj.GetLabels()) == 0 {
		return false
	}
	_, ok := obj.GetLabels()[LabelPodUseFixedIP]
	return ok
}

func FinalizerAddRemoteIP(obj metav1.Object) {
	finalizers := obj.GetFinalizers()
	containsRomoteIP := false
	for _, finalizer := range finalizers {
		if finalizer == FinalizerOfCCEEndpointRemoteIP {
			containsRomoteIP = true
			break
		}
	}
	if !containsRomoteIP {
		obj.SetFinalizers(append(finalizers, FinalizerOfCCEEndpointRemoteIP))
	}
}

func FinalizerRemoveRemoteIP(obj metav1.Object) bool {
	update := false
	newFinalizers := make([]string, 0)
	for _, finalizer := range obj.GetFinalizers() {
		if finalizer == FinalizerOfCCEEndpointRemoteIP {
			update = true
			continue
		}
		newFinalizers = append(newFinalizers, finalizer)
	}
	obj.SetFinalizers(newFinalizers)
	return update
}

// HavePrimaryENILabel check if the object has the primary ENI label
func HavePrimaryENILabel(obj metav1.Object) bool {
	if obj == nil {
		return false
	}
	if len(obj.GetLabels()) == 0 {
		return false
	}
	mode, ok := obj.GetLabels()[LabelENIUseMode]
	return ok && mode == string(ccev2.ENIUseModePrimaryIP)

}
