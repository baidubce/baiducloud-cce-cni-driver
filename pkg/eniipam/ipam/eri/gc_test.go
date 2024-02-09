package eri

import (
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
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

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.DeletePrivateIP(gomock.Any(), gomock.Eq("10.1.1.1"), gomock.Eq("eni-0")).Return(nil)

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

func (suite *IPAMTest) Test__gcDeletedNode() {
	// node-0 exist
	// node-1 not found
	suite.ipam.nodeCache = map[string]*corev1.Node{
		"i-xxxx0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-0",
			},
			Spec: corev1.NodeSpec{
				ProviderID: "cce://i-xxxx0",
			},
		},
		"i-xxxx1": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
			Spec: corev1.NodeSpec{
				ProviderID: "cce://i-xxxx1",
			},
		},
	}

	_, nodeErr := suite.ipam.kubeClient.CoreV1().Nodes().
		Create(suite.ctx, &corev1.Node{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-0",
			},
			Spec: corev1.NodeSpec{
				ProviderID: "cce://i-xxxx0",
			},
		}, metav1.CreateOptions{})
	suite.Assert().Nil(nodeErr)
	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)

	gcErr := suite.ipam.gcDeletedNode(suite.ctx)
	suite.Assert().Nil(gcErr)

	suite.Assert().Equal(1, len(suite.ipam.nodeCache))

	_, node1Exist := suite.ipam.nodeCache["i-xxxx1"]
	suite.Assert().True(!node1Exist, "expect node 1 not found")
}

func (suite *IPAMTest) Test__gcLeakedIP() {
	// 1. empty wep
	suite.ipam.nodeCache = map[string]*corev1.Node{
		"node-0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-0",
			},
		},
	}

	cloudClient := suite.ipam.cloud.(*mockcloud.MockInterface)
	eriList := []enisdk.Eni{
		{
			EniId: "eni-0",
			PrivateIpSet: []enisdk.PrivateIp{
				{
					Primary:          true,
					PrivateIpAddress: "10.1.1.0",
				},
				{
					Primary:          false,
					PrivateIpAddress: "10.1.1.1",
				},
				{
					Primary:          false,
					PrivateIpAddress: "10.1.1.2",
				},
			},
		},
	}
	gomock.InOrder(
		cloudClient.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return(eriList, nil),
		cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Eq("10.1.1.1"), gomock.Eq("eni-0")).Return(nil),
		cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Eq("10.1.1.2"), gomock.Eq("eni-0")).Return(nil),
	)

	gcErr := suite.ipam.gcLeakedIP(suite.ctx)
	suite.Assert().Nil(gcErr)

	// 2. delete leaked ip 10.1.1.2
	gomock.InOrder(
		cloudClient.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return(eriList, nil),
		cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Eq("10.1.1.2"), gomock.Eq("eni-0")).Return(nil),
	)

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
		NodeName:   "",
		InstanceID: "node-0",
		Type:       ipamgeneric.MwepTypeERI,
		Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{
			{
				EniID: "eni-0",
				IP:    "10.1.1.1",
			},
		},
	}
	_, err0 := suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Create(suite.ctx, mwep0, metav1.CreateOptions{})
	suite.Assert().Nil(err0)
	waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer)

	gcErr2 := suite.ipam.gcLeakedIP(suite.ctx)
	suite.Assert().Nil(gcErr2)
}
