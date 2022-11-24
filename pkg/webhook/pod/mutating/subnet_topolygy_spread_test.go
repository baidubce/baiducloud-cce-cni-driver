package mutating

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type MutatingPodHandlerTester struct {
	suite.Suite
	handler     *MutatingPodHandler
	crdClient   versioned.Interface
	crdInformer externalversions.SharedInformerFactory
	pod         *corev1.Pod
	wantErr     bool
}

func (suite *MutatingPodHandlerTester) SetupTest() {
	_, kubeInformer, crdClient, crdInformer, _, _, _ := data.NewMockEnv(gomock.NewController(suite.T()))
	kubeInformer.Start(make(<-chan struct{}))
	crdInformer.Start(make(<-chan struct{}))

	pstsInfomer := crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads()
	suite.handler = &MutatingPodHandler{
		crdClient:    crdClient,
		pstsInformer: pstsInfomer,
		cniMode:      types.CCEModeSecondaryIPVeth,
	}
	suite.crdClient = crdClient
	suite.crdInformer = crdInformer
	suite.wantErr = false
	suite.pod = nil
}

func (suite *MutatingPodHandlerTester) assertAddRelatedPodSubnetTopologySpread() {
	go suite.handler.pstsInformer.Informer().Run(wait.NeverStop)
	cache.WaitForNamedCacheSync("topology-spread-controller", wait.NeverStop, suite.handler.pstsInformer.Informer().HasSynced)
	_, err := suite.handler.addRelatedPodSubnetTopologySpread(context.TODO(), suite.pod)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
}

func (suite *MutatingPodHandlerTester) Test_addRelatedPodSubnetTopologySpreadNotMatch() {
	podLabel := labels.Set{"app": "busybox"}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)

	suite.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.Background(), psts, metav1.CreateOptions{})

	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}
	suite.assertAddRelatedPodSubnetTopologySpread()
}

func (suite *MutatingPodHandlerTester) Test_addRelatedPodSubnetTopologySpreadMatch() {
	podLabel := labels.Set{"app": "busybox"}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)

	suite.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.Background(), psts, metav1.CreateOptions{})

	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "busybox",
			Labels:      podLabel,
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}
	suite.assertAddRelatedPodSubnetTopologySpread()

}

func (suite *MutatingPodHandlerTester) Test_addRelatedPodSubnetTopologySpreadMatchAndDefault() {
	podLabel := labels.Set{"app": "busybox"}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)

	suite.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.Background(), psts, metav1.CreateOptions{})

	pstsDefault := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-default", "sbn-test", podLabel)
	pstsDefault.Spec.Selector = nil
	suite.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.Background(), pstsDefault, metav1.CreateOptions{})

	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "busybox",
			Labels:      podLabel,
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}
	suite.assertAddRelatedPodSubnetTopologySpread()

}

func (suite *MutatingPodHandlerTester) TestWebhook() {
	podLabel := labels.Set{"app": "busybox"}
	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "busybox",
			Labels:      podLabel,
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	raw, _ := json.Marshal(suite.pod)
	schema := runtime.NewScheme()
	networkv1alpha1.SchemeBuilder.AddToScheme(schema)
	suite.handler.Decoder, _ = admission.NewDecoder(schema)

	req := admission.Request{
		AdmissionRequest: admissionv1beta1.AdmissionRequest{
			Object: runtime.RawExtension{
				Raw: raw,
			},
			Operation: admissionv1beta1.Create,
		},
	}
	suite.handler.Handle(context.Background(), req)
}

func (suite *MutatingPodHandlerTester) TestWebhookRewrite() {
	podLabel := labels.Set{"app": "busybox"}
	suite.pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "busybox",
			Labels:      podLabel,
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	raw, _ := json.Marshal(suite.pod)
	schema := runtime.NewScheme()
	networkv1alpha1.SchemeBuilder.AddToScheme(schema)
	suite.handler.Decoder, _ = admission.NewDecoder(schema)

	req := admission.Request{
		AdmissionRequest: admissionv1beta1.AdmissionRequest{
			Object: runtime.RawExtension{
				Raw: raw,
			},
			Operation: admissionv1beta1.Create,
			Resource: metav1.GroupVersionResource{
				Resource: "pods",
			},
		},
	}
	resp := suite.handler.Handle(context.Background(), req)
	suite.True(resp.Allowed, "mutating allowed")

}

func (suite *MutatingPodHandlerTester) TestWebhookRewritePSTS() {
	podLabel := labels.Set{"app": "busybox"}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)
	sbn := psts.Status.AvailableSubnets["sbn-test"]
	sbn.AvailabilityZone = "cn-gz-a"
	sbn.Enable = true
	psts.Status.AvailableSubnets["sbn-test"] = sbn
	suite.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.Background(), psts, metav1.CreateOptions{})

	go suite.handler.pstsInformer.Informer().Run(wait.NeverStop)
	cache.WaitForNamedCacheSync("topology-spread-controller", wait.NeverStop, suite.handler.pstsInformer.Informer().HasSynced)

	suite.TestWebhookRewrite()
}

func TestSelectPriorityPSTS(t *testing.T) {
	suite.Run(t, &MutatingPodHandlerTester{})
}

func TestMutatingPodHandler_addRelatedPodSubnetTopologySpread(t *testing.T) {

	type args struct {
		ctx context.Context
		pod *corev1.Pod
	}
	tests := []struct {
		name string

		args    args
		want    *networkv1alpha1.PodSubnetTopologySpread
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctl := gomock.NewController(t)
			h := buildMutatingPodHandler(ctl)
			got, err := h.addRelatedPodSubnetTopologySpread(tt.args.ctx, tt.args.pod)
			if (err != nil) != tt.wantErr {
				t.Errorf("MutatingPodHandler.addRelatedPodSubnetTopologySpread() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MutatingPodHandler.addRelatedPodSubnetTopologySpread() = %v, want %v", got, tt.want)
			}
		})
	}
}
