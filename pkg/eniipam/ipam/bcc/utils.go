package bcc

import (
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	corev1 "k8s.io/api/core/v1"
)

func IsFixIPStatefulSetPod(pod *corev1.Pod) bool {
	if pod.Annotations == nil || !k8sutil.IsStatefulSetPod(pod) {
		return false
	}
	return pod.Annotations[StsPodAnnotationEnableFixIP] == EnableFixIPTrue
}
