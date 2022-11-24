package bcc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
)

type IPAMSubnetTopologyAllocates struct {
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
func (suite *IPAMSubnetTopologyAllocates) SetupTest() {
	suite.stopChan = make(chan struct{})
	suite.ipam = mockIPAM(suite.T(), suite.stopChan)
	suite.ctx = context.TODO()
	suite.name = "busybox"
	suite.namespace = corev1.NamespaceDefault
	suite.podLabel = labels.Set{
		"k8s.io/app": "busybox",
	}

	runtime.ReallyCrash = false
	suite.waitCacheSync()
}

func (suite *IPAMSubnetTopologyAllocates) waitCacheSync() {
	__waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer, suite.stopChan)
}

// assert for test case
func assertSuite(suite *IPAMSubnetTopologyAllocates) {
	suite.waitCacheSync()
	suite.ipam.datastore.AddNodeToStore("test-node", "i-xxx")
	suite.ipam.datastore.AddENIToStore("test-node", "eni-test")
	suite.ipam.datastore.AddPrivateIPToStore("test-node", "eni-test", "192.168.1.107", false)
	var (
		pod *corev1.Pod
		err error
	)
	i := 0
	for pod == nil || err != nil {
		pod, err = suite.ipam.kubeInformer.Core().V1().Pods().Lister().Pods(corev1.NamespaceDefault).Get(suite.name)
		suite.waitCacheSync()
		i++
		if i > 100 {
			return
		}
	}
	time.Sleep(time.Second)

	got, err := suite.ipam.Allocate(suite.ctx, suite.name, suite.namespace, suite.containerID)
	if !suite.wantErr {
		suite.Assert().NoError(err, "Allocate()  ip error")
		// 更新修改时间，避免测试用例不通过
		if suite.want != nil {
			suite.Assert().NotNil(got, "allocate ip return nil")
			got.Spec.UpdateAt = metav1.Time{Time: time.Unix(0, 0)}
			suite.Assert().EqualValues(suite.want, got, "Allocate()  want not euqal ")
		}
	} else {
		suite.Assert().Error(err, "Allocate()  ip error")
	}

}

// 每次测试后执行清理
func (suite *IPAMSubnetTopologyAllocates) TearDownTest() {
	suite.ipam = nil
	suite.ctx = nil
	suite.name = "busybox"
	suite.namespace = corev1.NamespaceDefault
	suite.want = nil
	suite.wantErr = false
	close(suite.stopChan)
}

func (suite *IPAMSubnetTopologyAllocates) BeforeTest(suiteName, testName string) {}
func (suite *IPAMSubnetTopologyAllocates) AfterTest(suiteName, testName string)  {}

// create a pod use dymamic IP cross subnet
func (suite *IPAMSubnetTopologyAllocates) TestAllocateDynamicIPCrossSubnet() {
	suite.createSubnet()

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(suite.ctx, data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel), metav1.CreateOptions{})

	suite.createPodAndNode()

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.want = &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":     "sbn-test",
				"cce.io/instance-type": "bcc",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypePod,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "false",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       "TTL",
		},
	}

	suite.waitCacheSync()
	pod, err := suite.ipam.kubeInformer.Core().V1().Pods().Lister().Pods(corev1.NamespaceDefault).Get("busybox")
	if pod == nil || err != nil {
		time.Sleep(time.Second)
	}
	assertSuite(suite)
}

// not found the psts object
func (suite *IPAMSubnetTopologyAllocates) TestPSTSNotFound() {
	suite.createPodAndNode()

	suite.wantErr = true
	assertSuite(suite)
}

// allocation dynamic ip cross subnet failed
func (suite *IPAMSubnetTopologyAllocates) TestFailedLimit() {
	ctx := suite.ctx

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel), metav1.CreateOptions{})
	suite.createPodAndNode()

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{"192.168.1.109"}, fmt.Errorf("RateLimit")).AnyTimes()

	suite.wantErr = true

	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) TestFailedSubnetHasNoMoreIpException() {
	ctx := suite.ctx

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel), metav1.CreateOptions{})
	suite.createPodAndNode()

	suite.wantErr = true
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{"192.168.1.109"}, fmt.Errorf("SubnetHasNoMoreIpException")).AnyTimes()
	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) TestFailedPrivateIpInUseException() {
	ctx := suite.ctx

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel), metav1.CreateOptions{})
	suite.createPodAndNode()

	suite.wantErr = true

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{"192.168.1.109"}, fmt.Errorf("PrivateIpInUseException")).AnyTimes()
	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) TestFailedResultLenError() {
	ctx := suite.ctx

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel), metav1.CreateOptions{})
	suite.createPodAndNode()

	suite.wantErr = true

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{}, nil).AnyTimes()
	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) createSubnet() {
	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(suite.ctx, subnet, metav1.CreateOptions{})
}

func (suite *IPAMSubnetTopologyAllocates) createPodAndNode() {
	ctx := suite.ctx
	suite.ipam.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(ctx, &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: "psts-test",
			},
			Labels: suite.podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, metav1.CreateOptions{})
	suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}, metav1.CreateOptions{})
}

// allocation fixed ip cross subnet first
func (suite *IPAMSubnetTopologyAllocates) TestAllocationFixedIPCrossSubnetFirst() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeFixed
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyNever
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.createFixedIPPodAndNode()

	// mock cloud api
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.want = &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":              "sbn-test",
				"cce.io/instance-type":          "bcc",
				ipamgeneric.WepLabelStsOwnerKey: "busybox",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypeSts,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "True",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       string(networkingv1alpha1.ReleaseStrategyNever),
		},
	}

	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) TestAllocationFixedIPCrossSubnetAgain() {
	ctx := suite.ctx
	suite.name = "busybox-0"
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, data.MockFixedWorkloadEndpoint(), metav1.CreateOptions{})

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeFixed
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyNever
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.createFixedIPPodAndNode()

	// mock cloud api
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.want = &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":              "sbn-test",
				"cce.io/instance-type":          "bcc",
				ipamgeneric.WepLabelStsOwnerKey: "busybox",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypeSts,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "True",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       string(networkingv1alpha1.ReleaseStrategyNever),
		},
	}

	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) TestAllocationMunualIPCrossSubnetFirst() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeManual
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyTTL
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.createFixedIPPodAndNode()

	// mock cloud api
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.want = &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":     "sbn-test",
				"cce.io/instance-type": "bcc",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypePod,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "false",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       string(networkingv1alpha1.ReleaseStrategyTTL),
		},
	}

	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) Test__rollbackIPAllocated() {
	wep := &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":     "sbn-test",
				"cce.io/instance-type": "bcc",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypePod,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "false",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       string(networkingv1alpha1.ReleaseStrategyTTL),
		},
	}

	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox-0",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: "psts-test",
			},
			Labels: suite.podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	suite.ipam.rollbackIPAllocated(suite.ctx, &ipToAllocate{pod: pod, ipv4Result: "192.168.1.109"}, fmt.Errorf("test error"), wep)
}

func (suite *IPAMSubnetTopologyAllocates) TestAllocationMunualIPRangeCrossSubnet() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4Range = append(allocation.IPv4, "192.168.1.109/32")
	allocation.Type = networkingv1alpha1.IPAllocTypeManual
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyTTL
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.createFixedIPPodAndNode()

	// mock cloud api
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.want = &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":     "sbn-test",
				"cce.io/instance-type": "bcc",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypePod,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "false",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       string(networkingv1alpha1.ReleaseStrategyTTL),
		},
	}

	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) TestAllocationMunualIPRangeCrossSubnetWithEmptySubnet() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	subnet.Name = ""
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4Range = append(allocation.IPv4, "192.168.1.109/32")
	allocation.Type = networkingv1alpha1.IPAllocTypeManual
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyTTL
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.createFixedIPPodAndNode()

	// mock cloud api
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.wantErr = true
	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) TestAllocationFixedIPWithDeleteIPFailed() {
	ctx := suite.ctx
	suite.name = "busybox-0"
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, data.MockFixedWorkloadEndpoint(), metav1.CreateOptions{})

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("RateLimit")).AnyTimes()

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeFixed
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyNever
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.createFixedIPPodAndNode()

	// mock cloud api
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.want = &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"cce.io/subnet-id":              "sbn-test",
				"cce.io/instance-type":          "bcc",
				ipamgeneric.WepLabelStsOwnerKey: "busybox",
			},
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			IP:                      "192.168.1.109",
			SubnetID:                "sbn-test",
			Type:                    ipamgeneric.WepTypeSts,
			ENIID:                   "eni-test",
			Node:                    "test-node",
			UpdateAt:                metav1.Time{Time: time.Unix(0, 0)},
			EnableFixIP:             "True",
			SubnetTopologyReference: "psts-test",
			FixIPDeletePolicy:       string(networkingv1alpha1.ReleaseStrategyNever),
		},
	}

	assertSuite(suite)
}

func (suite *IPAMSubnetTopologyAllocates) createFixedIPPodAndNode() {
	suite.ipam.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(context.TODO(), &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox-0",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: "psts-test",
			},
			Labels: suite.podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, metav1.CreateOptions{})
	suite.ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}, metav1.CreateOptions{})
}

func (suite *IPAMSubnetTopologyAllocates) Test__filterAvailableSubnet() {
	eni := &enisdk.Eni{
		ZoneName: "zoneF",
	}

	availableSubnets := map[string]networkingv1alpha1.SubnetPodStatus{
		"sbn-1": {
			SubenetDetail: networkingv1alpha1.SubenetDetail{AvailabilityZone: "zoneF", AvailableIPNum: 100},
		}, "sbn-2": {
			SubenetDetail: networkingv1alpha1.SubenetDetail{AvailabilityZone: "zoneF", HasNoMoreIP: true},
		}, "sbn-3": {
			SubenetDetail: networkingv1alpha1.SubenetDetail{AvailabilityZone: "zoneA"},
		}, "sbn-4": {
			SubenetDetail: networkingv1alpha1.SubenetDetail{AvailabilityZone: "zoneF"},
		},
	}
	result := filterAvailableSubnet(suite.ctx, eni, availableSubnets)
	suite.Assert().EqualValues([]string{"sbn-1", "sbn-4"}, result, "filter available subnet")
}

func (suite *IPAMSubnetTopologyAllocates) Test__allocateIPCrossSubnet() {
	eni := &enisdk.Eni{
		ZoneName: "zoneF",
		EniId:    "eni-test",
	}

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()

	mockInterface.BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, fmt.Errorf("VmMemoryCanNotAttachMoreIpException")).AnyTimes()
	_, err := suite.ipam.__allocateIPCrossSubnet(suite.ctx, eni, "192.168.1.109", "sbn-test", time.Microsecond, "10.0.0.1")
	suite.Assert().Error(err, "VmMemoryCanNotAttachMoreIpException")

	mockInterface.BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test1", gomock.Len(1), 1).Return(nil, fmt.Errorf("SubnetHasNoMoreIpException")).AnyTimes()
	_, err = suite.ipam.__allocateIPCrossSubnet(suite.ctx, eni, "192.168.1.109", "sbn-test1", time.Microsecond, "10.0.0.1")
	suite.Assert().Error(err, "SubnetHasNoMoreIpException")

	mockInterface.BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test2", gomock.Len(1), 1).Return([]string{}, fmt.Errorf("RateLimit")).AnyTimes()
	_, err = suite.ipam.__allocateIPCrossSubnet(suite.ctx, eni, "192.168.1.109", "sbn-test2", time.Microsecond, "10.0.0.1")
	suite.Assert().Error(err, "RateLimit")

	mockInterface.BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test3", gomock.Len(1), 1).Return([]string{}, nil).AnyTimes()
	_, err = suite.ipam.__allocateIPCrossSubnet(suite.ctx, eni, "192.168.1.109", "sbn-test3", time.Microsecond, "10.0.0.1")
	suite.Assert().Error(err, "NoIP")

}

func (suite *IPAMSubnetTopologyAllocates) TestSyncRelationOfWepEni() {
	eni := &enisdk.Eni{
		ZoneName: "zoneF",
		EniId:    "eni-test",
		PrivateIpSet: []enisdk.PrivateIp{
			{
				PrivateIpAddress: "10.0.0.1",
			},
			{
				PrivateIpAddress: "10.0.0.2",
			},
			{
				PrivateIpAddress: "10.0.0.3",
			},
			{
				PrivateIpAddress: "10.0.0.4",
			},
		},
	}
	suite.ipam.eniCache = make(map[string][]*enisdk.Eni)
	suite.ipam.eniCache["node-test"] = append(suite.ipam.eniCache["node-test"], eni)

	wep := data.MockFixedWorkloadEndpoint()
	wep.Name = "w1"
	wep.Spec.IP = "10.0.0.1"
	suite.ipam.allocated[wep.Spec.IP] = wep
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep = data.MockFixedWorkloadEndpoint()
	wep.Name = "w2"
	wep.Spec.IP = "10.0.0.2"
	wep.Spec.ENIID = "eni-change"
	wep.Spec.UpdateAt = metav1.Time{Time: time.Unix(0, 0)}
	suite.ipam.allocated[wep.Spec.IP] = wep
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep = data.MockFixedWorkloadEndpoint()
	wep.Name = "w3"
	wep.Spec.IP = "10.0.0.3"
	wep.Spec.ENIID = "eni-change"
	wep.Spec.UpdateAt = metav1.Time{Time: time.Now()}
	suite.ipam.allocated[wep.Spec.IP] = wep
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep = data.MockFixedWorkloadEndpoint()
	wep.Spec.EnableFixIP = "false"
	wep.Name = "w4"
	wep.Spec.IP = "10.0.0.4"
	wep.Spec.ENIID = "eni-change"
	wep.Spec.UpdateAt = metav1.Time{Time: time.Now()}
	suite.ipam.allocated[wep.Spec.IP] = wep
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	__waitForCacheSync(suite.ipam.kubeInformer, suite.ipam.crdInformer, suite.stopChan)
	// resync to update wep
	suite.ipam.syncRelationOfWepEni()

	wep, err := suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Get(suite.ctx, "w2", metav1.GetOptions{})
	if suite.Assert().NoError(err, "get wep error") {
		suite.Assert().Equal("eni-test", wep.Spec.ENIID, "eni id not change")
	}
	wep, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Get(suite.ctx, "w4", metav1.GetOptions{})
	if suite.Assert().NoError(err, "get wep error") {
		suite.Assert().Equal("eni-change", wep.Spec.ENIID, "eni id change")
	}
	wep, err = suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Get(suite.ctx, "w3", metav1.GetOptions{})
	if suite.Assert().NoError(err, "get wep error") {
		suite.Assert().Equal("eni-change", wep.Spec.ENIID, "eni id change")
	}
}

func TestIPAM_subnetTopologyAllocates(t *testing.T) {
	suite.Run(t, new(IPAMSubnetTopologyAllocates))
}
