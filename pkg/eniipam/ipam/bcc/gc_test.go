package bcc

import (
	"context"
	"fmt"
	"testing"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
)

type IPAMGC struct {
	suite.Suite
	ipam        *IPAM
	wantErr     bool
	want        *networkingv1alpha1.WorkloadEndpoint
	ctx         context.Context
	name        string
	namespace   string
	containerID string
	podLabel    labels.Set
	stopChan    chan struct{}
}

// 每次测试前设置上下文
func (suite *IPAMGC) SetupTest() {
	suite.stopChan = make(chan struct{})
	suite.ipam = mockIPAM(suite.T(), suite.stopChan)
	suite.ctx = context.TODO()
	suite.name = "busybox"
	suite.namespace = corev1.NamespaceDefault
	suite.podLabel = labels.Set{
		"k8s.io/app": "busybox",
	}

	runtime.ReallyCrash = false
}

// 每次测试后执行清理
func (suite *IPAMGC) TearDownTest() {
	suite.ipam = nil
	suite.ctx = nil
	suite.name = "busybox"
	suite.namespace = corev1.NamespaceDefault
	suite.want = nil
	suite.wantErr = false
	close(suite.stopChan)
}

func (suite *IPAMGC) Test__gcDeletedSts() {
	wep := data.MockFixedWorkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep1 := data.MockFixedWorkloadEndpoint()
	wep1.Name = "busybox-1"
	wep1.Spec.FixIPDeletePolicy = string(networkingv1alpha1.ReleaseStrategyTTL)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep1, metav1.CreateOptions{})

	wep2 := data.MockFixedWorkloadEndpoint()
	wep2.Name = "busybox-2"
	wep2.Spec.IP = "192.168.1.110"
	wep2.Spec.FixIPDeletePolicy = string(networkingv1alpha1.ReleaseStrategyTTL)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep2, metav1.CreateOptions{})

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.DeletePrivateIP(gomock.Any(), "192.168.1.109", gomock.Any()).Return(nil).AnyTimes()
	mockInterface.DeletePrivateIP(gomock.Any(), "192.168.1.110", gomock.Any()).Return(fmt.Errorf("cannot delete ip")).AnyTimes()
	suite.ipam.gcDeletedSts(suite.ctx, []*networkingv1alpha1.WorkloadEndpoint{wep, wep1, wep2})

	// should not be deleted
	_, err := suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-0", metav1.GetOptions{})
	suite.Assert().NoError(err, "get wep error")
	// should be deleted
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-1", metav1.GetOptions{})
	suite.Assert().True(errors.IsNotFound(err), "get wep error")
	// delete error
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-2", metav1.GetOptions{})
	suite.Assert().NoError(err, "get wep error")
}

func (suite *IPAMGC) Test__gcScaledDownSts() {
	var replicas int32 = 3
	stsList := []*appsv1.StatefulSet{{
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			Replicas: &replicas,
		},
	}}

	wep := data.MockFixedWorkloadEndpoint()
	wep.Spec.EnableFixIP = "false"
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep1 := data.MockFixedWorkloadEndpoint()
	wep1.Name = "busybox-1"
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep1, metav1.CreateOptions{})

	wep1 = data.MockFixedWorkloadEndpoint()
	wep1.Name = "busybox-2"
	wep1.Spec.FixIPDeletePolicy = string(networkingv1alpha1.ReleaseStrategyTTL)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep1, metav1.CreateOptions{})

	wep2 := data.MockFixedWorkloadEndpoint()
	wep2.Name = "busybox-3"
	wep2.Spec.IP = "192.168.1.110"
	wep2.Spec.FixIPDeletePolicy = string(networkingv1alpha1.ReleaseStrategyTTL)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep2, metav1.CreateOptions{})

	wep2 = data.MockFixedWorkloadEndpoint()
	wep2.Name = "busybox-4"
	wep2.Spec.IP = "192.168.1.109"
	wep2.Spec.FixIPDeletePolicy = string(networkingv1alpha1.ReleaseStrategyTTL)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep2, metav1.CreateOptions{})

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.DeletePrivateIP(gomock.Any(), "192.168.1.109", gomock.Any()).Return(nil).AnyTimes()
	mockInterface.DeletePrivateIP(gomock.Any(), "192.168.1.110", gomock.Any()).Return(fmt.Errorf("cannot delete ip")).AnyTimes()

	waitCacheSync(suite.ipam, suite.stopChan)
	suite.ipam.gcScaledDownSts(suite.ctx, stsList)

	// should not be deleted
	_, err := suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-0", metav1.GetOptions{})
	suite.Assert().NoError(err, "get wep error")
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-1", metav1.GetOptions{})
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-2", metav1.GetOptions{})

	// should be deleted
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-4", metav1.GetOptions{})
	suite.Assert().True(errors.IsNotFound(err), "get wep error")
	// delete error
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-3", metav1.GetOptions{})
	suite.Assert().NoError(err, "get wep error")
}

func (suite *IPAMGC) Test__gcLeakedPod() {
	wep := data.MockFixedWorkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep1 := data.MockFixedWorkloadEndpoint()
	wep1.Name = "busybox-1"
	wep1.Spec.FixIPDeletePolicy = string(networkingv1alpha1.ReleaseStrategyTTL)
	wep1.Spec.EnableFixIP = "false"
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep1, metav1.CreateOptions{})

	wep2 := data.MockFixedWorkloadEndpoint()
	wep2.Name = "busybox-2"
	wep2.Spec.IP = "192.168.1.110"
	wep1.Spec.EnableFixIP = "false"
	wep2.Spec.FixIPDeletePolicy = string(networkingv1alpha1.ReleaseStrategyTTL)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, wep2, metav1.CreateOptions{})

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.DeletePrivateIP(gomock.Any(), "192.168.1.109", gomock.Any()).Return(nil).AnyTimes()
	mockInterface.DeletePrivateIP(gomock.Any(), "192.168.1.110", gomock.Any()).Return(fmt.Errorf("cannot delete ip")).AnyTimes()
	suite.ipam.gcLeakedPod(suite.ctx, []*networkingv1alpha1.WorkloadEndpoint{wep, wep1, wep2})

	// should not be deleted
	_, err := suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-0", metav1.GetOptions{})
	suite.Assert().NoError(err, "get wep error")
	// should be deleted
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-1", metav1.GetOptions{})
	suite.Assert().True(errors.IsNotFound(err), "get wep error")
	// delete error
	_, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-2", metav1.GetOptions{})
	suite.Assert().NoError(err, "get wep error")
}

func (suite *IPAMGC) Test__gcLeakedIPPool() {
	ippool := data.MockIPPool()
	suite.ipam.crdClient.CceV1alpha1().IPPools(corev1.NamespaceDefault).Create(suite.ctx, ippool, metav1.CreateOptions{})

	ippool = data.MockIPPool()
	ippool.Name = "ippool-10-0-0-9"
	suite.ipam.crdClient.CceV1alpha1().IPPools(corev1.NamespaceDefault).Create(suite.ctx, ippool, metav1.CreateOptions{})

	waitCacheSync(suite.ipam, suite.stopChan)
	suite.ipam.gcLeakedIPPool(suite.ctx)
}

func (suite *IPAMGC) Test__gcDeletedNode() {
	suite.ipam.datastore.AddNodeToStore("10.0.0.2", "10.0.0.2")
	suite.ipam.datastore.AddENIToStore("10.0.0.2", "eni-1")
	suite.ipam.increasePoolEventChan["10.0.0.2"] = make(chan *event)
	suite.ipam.datastore.AddNodeToStore("10.0.0.3", "10.0.0.3")
	suite.ipam.datastore.AddENIToStore("10.0.0.3", "eni-2")
	suite.ipam.gcDeletedNode(suite.ctx)

	_, ok := suite.ipam.increasePoolEventChan["10.0.0.2"]
	suite.Assert().False(ok, "node be gc")
}

func TestIPAMGC(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(IPAMGC))
}
