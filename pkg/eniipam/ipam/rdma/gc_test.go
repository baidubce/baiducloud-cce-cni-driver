package rdma

import (
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	mockclient "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/rdma/client/mock"
	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (suite *IPAMTest) Test__gcLeakedPod() {
	// pod-0 exist
	// pod-1 not found
	mwep0 := &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pod-0",
			Namespace:  corev1.NamespaceDefault,
			Finalizers: []string{"cce-cni.cce.io"},
			Labels: map[string]string{
				corev1.LabelInstanceType:             "BCC",
				ipamgeneric.MwepLabelInstanceTypeKey: ipamgeneric.MwepTypeERI,
			},
		},
		Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{{
			IP:    "10.1.1.0",
			EniID: "eni-0",
		}},
	}
	_, err0 := suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Create(suite.ctx, mwep0, metav1.CreateOptions{})
	suite.Assert().Nil(err0)

	mwep1 := &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pod-1",
			Namespace:  corev1.NamespaceDefault,
			Finalizers: []string{"cce-cni.cce.io"},
			Labels: map[string]string{
				corev1.LabelInstanceType:             "BCC",
				ipamgeneric.MwepLabelInstanceTypeKey: ipamgeneric.MwepTypeERI,
			},
		},
		Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{{
			IP:    "10.1.1.1",
			EniID: "eni-0",
		}},
	}
	_, err1 := suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Create(suite.ctx, mwep1, metav1.CreateOptions{})
	suite.Assert().Nil(err1)

	_, podErr := suite.ipam.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).
		Create(suite.ctx, &corev1.Pod{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-0",
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
			},
		}, metav1.CreateOptions{})
	suite.Assert().Nil(podErr)
	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)

	mockInterface := suite.ipam.iaasClient.(*mockclient.MockIaaSClient).EXPECT()
	mockInterface.GetMwepType().Return("eri")
	mockInterface.DeletePrivateIP(gomock.Any(), gomock.Eq("eni-0"), gomock.Eq("10.1.1.1")).Return(nil)

	gcErr := suite.ipam.gcLeakedPod(suite.ctx)
	suite.Assert().Nil(gcErr)

	// should not be deleted
	_, getErr0 := suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Get(suite.ctx, "pod-0", metav1.GetOptions{})
	suite.Assert().Nil(getErr0)
	// should be deleted
	_, getErr1 := suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Get(suite.ctx, "pod-1", metav1.GetOptions{})
	suite.Assert().True(errors.IsNotFound(getErr1))
}
