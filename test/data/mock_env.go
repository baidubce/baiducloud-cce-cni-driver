package data

import (
	"time"

	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
)

func NewMockEnv(ctrl *gomock.Controller) (
	kubernetes.Interface,
	informers.SharedInformerFactory,
	versioned.Interface,
	crdinformers.SharedInformerFactory,
	*mockcloud.MockInterface,
	record.EventBroadcaster,
	record.EventRecorder,
) {
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformer := informers.NewSharedInformerFactory(kubeClient, time.Minute)
	crdClient := crdfake.NewSimpleClientset()
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, time.Minute)
	cloudClient := mockcloud.NewMockInterface(ctrl)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "cce-cni"})

	return kubeClient, kubeInformer,
		crdClient, crdInformer, cloudClient,
		eventBroadcaster, recorder
}
