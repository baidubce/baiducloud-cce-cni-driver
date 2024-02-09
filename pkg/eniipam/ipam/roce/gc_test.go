package roce

import (
	"context"
	"fmt"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	"k8s.io/apimachinery/pkg/util/clock"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
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

// mock a ipam server
func mockIPAM(t *testing.T, stopChan chan struct{}) *IPAM {
	ctrl := gomock.NewController(t)
	kubeClient, _, crdClient, _, cloudClient, _, _ := setupEnv(ctrl)
	ipam, _ := NewIPAM(
		kubeClient,
		crdClient,
		cloudClient,
		20*time.Second,
		300*time.Second,
		5*time.Second,
		true,
	)
	ipamServer := ipam.(*IPAM)
	ipamServer.cacheHasSynced = true
	nodeCache := map[string]*corev1.Node{
		"eni-df8888fs": &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
			},
		},
	}
	ipamServer.nodeCache = nodeCache
	ipamServer.eniSyncPeriod = 5 * time.Second
	ipamServer.hpcEniCache = make(map[string][]hpc.Result)

	ipamServer.clock = clock.NewFakeClock(time.Unix(0, 0))
	ipamServer.kubeInformer.Start(stopChan)
	ipamServer.crdInformer.Start(stopChan)
	return ipam.(*IPAM)
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

func MockMultiworkloadEndpoint() *networkingv1alpha1.MultiIPWorkloadEndpoint {
	return &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"beta.kubernetes.io/instance-type": "BBC",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{{
			IP:       "192.168.1.199",
			EniID:    "eni-dfsfs",
			UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
		},
			{
				IP:       "192.168.1.189",
				EniID:    "eni-df8888fs",
				UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
			},
			{
				IP:       "192.168.1.179",
				EniID:    "eni-df8888fdfghds",
				UpdateAt: metav1.Time{Time: time.Unix(0, 0)},
			},
		},
	}
}

func (suite *IPAMGC) Test__gcLeakedPod() {
	mwep := MockMultiworkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep, metav1.CreateOptions{})

	mwep1 := MockMultiworkloadEndpoint()
	mwep1.Name = "busybox-1"
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep1, metav1.CreateOptions{})

	mwep2 := MockMultiworkloadEndpoint()
	mwep2.Name = "busybox-2"
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep2, metav1.CreateOptions{})

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockInterface.BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(fmt.Errorf("cannot delete ip")).AnyTimes()
	suite.ipam.gcLeakedPod(suite.ctx, []*networkingv1alpha1.MultiIPWorkloadEndpoint{mwep, mwep1, mwep2})

	// should not be deleted
	_, err := suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-0", metav1.GetOptions{})
	suite.Assert().Error(err, "multiipworkloadendpoints.cce.io \"busybox-0\" not found")
	// should be deleted
	_, err = suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-1", metav1.GetOptions{})
	suite.Assert().True(errors.IsNotFound(err), "get mwep error")
	// delete error
	_, err = suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-2", metav1.GetOptions{})
	suite.Assert().Error(err, "multiipworkloadendpoints.cce.io \"busybox-2\" not found")
}

func (suite *IPAMGC) Test__gcLeakedNode() {
	mwep := MockMultiworkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep, metav1.CreateOptions{})

	mwep1 := MockMultiworkloadEndpoint()
	mwep1.Name = "busybox-1"
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep1, metav1.CreateOptions{})

	mwep2 := MockMultiworkloadEndpoint()
	mwep2.Name = "busybox-2"
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep2, metav1.CreateOptions{})

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockInterface.BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(fmt.Errorf("cannot delete ip")).AnyTimes()
	suite.ipam.gcLeakedPod(suite.ctx, []*networkingv1alpha1.MultiIPWorkloadEndpoint{mwep, mwep1, mwep2})

	// should not be deleted
	_, err := suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-0", metav1.GetOptions{})
	suite.Assert().Error(err, "multiipworkloadendpoints.cce.io \"busybox-0\" not found")
	// should be deleted
	_, err = suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-1", metav1.GetOptions{})
	suite.Assert().True(errors.IsNotFound(err), "get mwep error")
	// delete error
	_, err = suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Get(suite.ctx, "busybox-2", metav1.GetOptions{})
	suite.Assert().Error(err, "multiipworkloadendpoints.cce.io \"busybox-2\" not found")
}

func (suite *IPAMGC) Test__gcDeletedNode() {
	suite.ipam.nodeCache = map[string]*corev1.Node{"i-cdcac": &corev1.Node{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "",
			Namespace: "",
		},
	}}
	err := suite.ipam.gcDeletedNode(suite.ctx)
	suite.Assert().NoError(err, "get mwep error")
}

func (suite *IPAMGC) Test_gc() {
	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.GetHPCEniID(gomock.Any(), gomock.Any()).Return(&hpc.EniList{
		Result: []hpc.Result{{
			EniID: "eni-df8888fs",
			PrivateIPSet: []hpc.PrivateIP{{
				Primary:          false,
				PrivateIPAddress: "192.168.1.179",
			},
			},
		},
		},
	}, nil).AnyTimes()
	mockInterface.BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockInterface.ListENIs(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("list error")).AnyTimes()
	go suite.ipam.gc(suite.stopChan)
	time.Sleep(3 * time.Second)
}

func (suite *IPAMGC) Test__gcLeakedIP() {
	// 1. empty wep
	suite.ipam.nodeCache = map[string]*corev1.Node{
		"i-0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-0",
			},
		},
	}

	cloudClient := suite.ipam.cloud.(*mockcloud.MockInterface)
	eniList := &hpc.EniList{
		Result: []hpc.Result{
			{
				EniID: "eni-0",
				PrivateIPSet: []hpc.PrivateIP{
					{
						Primary:          true,
						PrivateIPAddress: "10.1.1.0",
					},
					{
						Primary:          false,
						PrivateIPAddress: "10.1.1.1",
					},
					{
						Primary:          false,
						PrivateIPAddress: "10.1.1.2",
					},
				},
			},
		},
	}
	args1 := &hpc.EniBatchDeleteIPArgs{
		EniID:              "eni-0",
		PrivateIPAddresses: []string{"10.1.1.1"},
	}
	args2 := &hpc.EniBatchDeleteIPArgs{
		EniID:              "eni-0",
		PrivateIPAddresses: []string{"10.1.1.2"},
	}
	gomock.InOrder(
		cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Eq("i-0")).Return(eniList, nil),
		cloudClient.EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Eq(args1)).Return(nil),
		cloudClient.EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Eq(args2)).Return(nil),
	)

	gcErr := suite.ipam.gcLeakedIP(suite.ctx)
	suite.Assert().Nil(gcErr)

	// 2. delete leaked ip 10.1.1.2
	gomock.InOrder(
		cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Eq("i-0")).Return(eniList, nil),
		cloudClient.EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Eq(args2)).Return(nil),
	)

	mwep0 := &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pod-0",
			Namespace:  corev1.NamespaceDefault,
			Finalizers: []string{"cce-cni.cce.io"},
			Labels: map[string]string{
				corev1.LabelInstanceType:             "BBC",
				ipamgeneric.MwepLabelInstanceTypeKey: ipamgeneric.MwepTypeRoce,
			},
		},
		NodeName:   "",
		InstanceID: "i-0",
		Type:       ipamgeneric.MwepTypeRoce,
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

func TestIPAMGC(t *testing.T) {
	suite.Run(t, new(IPAMGC))
}
