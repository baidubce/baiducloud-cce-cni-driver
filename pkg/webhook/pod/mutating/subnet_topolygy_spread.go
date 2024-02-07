package mutating

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

// Select the best podsubnettopologyspreads object associated with the pod
// And record the object at the annotation of the pod
func (h *MutatingPodHandler) addRelatedPodSubnetTopologySpread(ctx context.Context, pod *corev1.Pod) (*networkv1alpha1.PodSubnetTopologySpread, error) {

	pstsList, err := h.pstsInformer.Lister().PodSubnetTopologySpreads(pod.Namespace).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list PodSubnetTopologySpreads at namespace %s error %w", pod.Namespace, err)
	}

	psts := SelectPriorityPSTS(pstsList, labels.Set(pod.Labels))
	if psts != nil {
		pod.Annotations[networking.AnnotationPodSubnetTopologySpread] = psts.Name
		klog.V(3).Infof("addRelatedPodSubnetTopologySpread pod （%s/%s) matched psts %s", pod.GetNamespace(), pod.GetName(), psts.GetName())
	}
	return psts, nil
}

// Select the highest priority and matching pSTS object
// label: pod labels to be mached
// pstsList: list of PodSubnetTopologySpread
// return:
// - nil: if no PodSubnetTopologySpread matched
// - psts: the highest priority PodSubnetTopologySpread object
func SelectPriorityPSTS(pstsList []*networkv1alpha1.PodSubnetTopologySpread, label labels.Labels) *networkv1alpha1.PodSubnetTopologySpread {
	var matchedPstses []*networkv1alpha1.PodSubnetTopologySpread

	for i := 0; i < len(pstsList); i++ {
		psts := pstsList[i]
		if psts.Spec.Selector == nil {
			matchedPstses = append(matchedPstses, psts)
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(psts.Spec.Selector)
		if err != nil {
			continue
		}
		if selector.Matches(label) {
			matchedPstses = append(matchedPstses, psts)
		}
	}

	if len(matchedPstses) == 0 {
		return nil
	}
	sort.Slice(matchedPstses, func(i, j int) bool {
		pstsI := matchedPstses[i]
		pstsJ := matchedPstses[j]
		return pstsI.Spec.Priority < pstsJ.Spec.Priority
	})
	return matchedPstses[len(matchedPstses)-1]
}
