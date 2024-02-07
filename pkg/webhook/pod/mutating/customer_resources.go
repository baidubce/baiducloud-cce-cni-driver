package mutating

import (
	"context"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// add cce ip resource to pod while create pod
// add cce.baidubce.com/ip to the first container of pod resource
func (h *MutatingPodHandler) addIPResource2Pod(ctx context.Context, pod *corev1.Pod) error {
	if pod.Spec.HostNetwork {
		return nil
	}
	if len(pod.Spec.Containers) == 0 {
		return nil
	}

	for i := 0; i < len(pod.Spec.Containers); i++ {
		removeResource(&pod.Spec.Containers[i].Resources, networking.ResourceENIForNode)
		removeResource(&pod.Spec.Containers[i].Resources, networking.ResourceIPForNode)
	}
	firstContainerResource := &pod.Spec.Containers[0].Resources
	if len(firstContainerResource.Requests) == 0 {
		firstContainerResource.Requests = make(corev1.ResourceList)
	}
	if len(firstContainerResource.Limits) == 0 {
		firstContainerResource.Limits = make(corev1.ResourceList)
	}

	resourceValue := *resource.NewQuantity(1, resource.DecimalSI)
	firstContainerResource.Requests[networking.ResourceIPForNode] = resourceValue
	firstContainerResource.Limits[networking.ResourceIPForNode] = resourceValue
	logger.Infof(ctx, "addIPResource2Pod add 1 ip resource to resouces requests of pod(%s/%s)", pod.Namespace, pod.Name)

	return nil
}

func removeResource(rs *corev1.ResourceRequirements, key corev1.ResourceName) {
	delete(rs.Requests, key)
	delete(rs.Limits, key)
}
