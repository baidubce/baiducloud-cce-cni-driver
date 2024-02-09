package data

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	eniutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
)

func MockNode(name, mtype, id string) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: make(map[string]string),
			Labels: map[string]string{
				corev1.LabelInstanceType: mtype,
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: id,
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	if mtype == "BCC" {
		node.Annotations[eniutil.NodeAnnotationPreAttachedENINum] = "1"
		node.Annotations[eniutil.NodeAnnotationMaxENINum] = "8"
		node.Annotations[eniutil.NodeAnnotationMaxIPPerENI] = "8"
		node.Annotations[eniutil.NodeAnnotationWarmIPTarget] = "8"
	}
	return node
}
