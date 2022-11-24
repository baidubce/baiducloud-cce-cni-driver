package mutating

import (
	"context"
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	cloud_util "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// add affinity zone to pod by subnet
// subnet is describe at psts
// this method only be called when creating pod
func (h *MutatingPodHandler) addPodTopologySpread(ctx context.Context, pod *corev1.Pod, psts *networkv1alpha1.PodSubnetTopologySpread) error {
	if psts == nil {
		return nil
	}

	if _, ok := pod.Annotations[networking.AnnotationDisablePSTSPodAffinity]; ok {
		logger.Warningf(ctx, "ignore the ability to add affinity scheduling for pod(%s/%s)", pod.Namespace, pod.Name)
		return nil
	}

	// 1. find all zone
	availableZoneSet, err := getAvailableZone(psts)
	if err != nil {
		return err
	}

	// 2. add topology spread
	if psts.Spec.EnablePodTopologySpread {
		pod.Spec.TopologySpreadConstraints = append(pod.Spec.TopologySpreadConstraints, corev1.TopologySpreadConstraint{
			MaxSkew:           psts.Spec.MaxSkew,
			TopologyKey:       networking.TopologyKeyOfPod,
			WhenUnsatisfiable: corev1.UnsatisfiableConstraintAction(psts.Spec.WhenUnsatisfiable),
			LabelSelector:     psts.Spec.Selector,
		})
	}

	// add affinity to pod
	// append node selector affinity if need
	addAffinity := addAffinityToPod(availableZoneSet.UnsortedList(), pod)
	if addAffinity {
		logger.Infof(ctx, "add affinity for subnet zone to pod (%s/%s)", pod.Namespace, pod.Name)
	}
	return nil
}

// addAffinityToPod
func addAffinityToPod(zones []string, pod *corev1.Pod) bool {
	matchExpression := corev1.NodeSelectorRequirement{
		Key:      networking.TopologyKeyOfPod,
		Operator: corev1.NodeSelectorOpIn,
		Values:   zones,
	}
	nodeSelectorTerm := corev1.NodeSelectorTerm{
		MatchExpressions: []corev1.NodeSelectorRequirement{matchExpression},
	}
	nodeSelectorTerms := []corev1.NodeSelectorTerm{nodeSelectorTerm}
	nodeSelector := &corev1.NodeSelector{NodeSelectorTerms: nodeSelectorTerms}
	nodeAffinity := &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector}
	affinity := &corev1.Affinity{NodeAffinity: nodeAffinity}
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = affinity
		return true
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = nodeAffinity
		return true
	}
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSelector
		return true
	}

	containsZoneAffinity := false
	for _, nodeSelectorT := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, matchE := range nodeSelectorT.MatchExpressions {
			if matchE.Key == networking.TopologyKeyOfPod {
				containsZoneAffinity = true
				break
			}
		}
	}
	if !containsZoneAffinity {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, matchExpression)
		return true
	}
	return false
}

func getAvailableZone(psts *networkv1alpha1.PodSubnetTopologySpread) (sets.String, error) {
	availableZoneSet := sets.NewString()
	for _, v := range psts.Status.AvailableSubnets {
		if !v.Enable {
			continue
		}
		zone, err := cloud_util.TransZoneNameToAvailableZone(v.AvailabilityZone)
		if err == nil {
			availableZoneSet.Insert(string(zone))
		}
	}

	if availableZoneSet.Len() == 0 {
		return nil, fmt.Errorf("no subnet available in psts(%s/%s) object", psts.Namespace, psts.Name)
	}
	return availableZoneSet, nil
}
