package mutating

import (
	"context"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (suite *MutatingPodHandlerTester) Test_addPodTopologySpreadWithNoAvailableSubnet() {
	podLabel := labels.Set{"app": "busybox"}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)
	psts.Status.AvailableSubnets = make(map[string]networkv1alpha1.SubnetPodStatus)
	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "busybox",
			Labels: podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	err := suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().Error(err, "no available subnet")
}

func (suite *MutatingPodHandlerTester) Test_addPodTopologySpreadNoPSTS() {
	podLabel := labels.Set{"app": "busybox"}

	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "busybox",
			Labels: podLabel,
			Annotations: map[string]string{
				networking.AnnotationDisablePSTSPodAffinity: "true",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	err := suite.handler.addPodTopologySpread(context.Background(), suite.pod, nil)
	suite.Assert().NoError(err, "no psts")
}

func (suite *MutatingPodHandlerTester) Test_addPodTopologySpreadIgnore() {
	podLabel := labels.Set{"app": "busybox"}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)
	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "busybox",
			Labels: podLabel,
			Annotations: map[string]string{
				networking.AnnotationDisablePSTSPodAffinity: "true",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	err := suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().NoError(err, "no available subnet")
}

func mockPSTSForAfinity(label labels.Set) *networkv1alpha1.PodSubnetTopologySpread {
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", label)
	psts.Status.AvailableSubnets["sbn-test"] = networkv1alpha1.SubnetPodStatus{
		SubenetDetail: networkv1alpha1.SubenetDetail{
			AvailabilityZone: "cn-gz-a",
			Enable:           true,
		},
	}
	return psts
}

func (suite *MutatingPodHandlerTester) Test_addPodTopologySpreadNoAffinity1_Success() {
	podLabel := labels.Set{"app": "busybox"}
	psts := mockPSTSForAfinity(podLabel)
	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "busybox",
			Labels: podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	err := suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().NoError(err, "no available subnet")
}

func (suite *MutatingPodHandlerTester) Test_addPodTopologySpreadNoAffinity2_Success() {
	podLabel := labels.Set{"app": "busybox"}
	psts := mockPSTSForAfinity(podLabel)
	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "busybox",
			Labels: podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
			Affinity: &corev1.Affinity{},
		},
	}
	err := suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().NoError(err, "no available subnet")

	suite.pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	err = suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().NoError(err, "no available subnet")

	suite.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
	err = suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().NoError(err, "no available subnet")

	nodeSelectorTerm := corev1.NodeSelectorTerm{
		MatchExpressions: []corev1.NodeSelectorRequirement{
			{
				Key:      networking.TopologyKeyOfPod,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"zoneA"},
			},
		}}
	suite.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{nodeSelectorTerm},
	}
	err = suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().NoError(err, "no available subnet")

	nodeSelectorTerm2 := corev1.NodeSelectorTerm{
		MatchFields: []corev1.NodeSelectorRequirement{
			{
				Key:      "metadata.name",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"nodeA"},
			},
		}}
	suite.pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{nodeSelectorTerm2},
	}
	err = suite.handler.addPodTopologySpread(context.Background(), suite.pod, psts)
	suite.Assert().NoError(err, "no available subnet")
}
