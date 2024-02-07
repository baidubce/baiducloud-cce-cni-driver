package bcc

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/ipcache"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	k8sutilnet "k8s.io/utils/net"
)

type ipamSubnetTopologySuperTester struct {
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
func (suite *ipamSubnetTopologySuperTester) SetupTest() {
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

func (suite *ipamSubnetTopologySuperTester) setupTest() {
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
func (suite *ipamSubnetTopologySuperTester) TearDownTest() {
	close(suite.stopChan)
	suite.ipam = nil
	suite.ctx = nil
	suite.name = "busybox"
	suite.namespace = corev1.NamespaceDefault
	suite.want = nil
	suite.wantErr = false
}

func (suite *ipamSubnetTopologySuperTester) tearDownTest() {
	close(suite.stopChan)
	suite.ipam = nil
	suite.ctx = nil
	suite.name = "busybox"
	suite.namespace = corev1.NamespaceDefault
	suite.want = nil
	suite.wantErr = false
}

func startInformer(kubeInformer informers.SharedInformerFactory, crdInformer externalversions.SharedInformerFactory, stopChan chan struct{}) {
	kubeInformer.Core().V1().Nodes().Informer()
	kubeInformer.Core().V1().Pods().Informer()
	kubeInformer.Apps().V1().StatefulSets().Informer()
	crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()
	crdInformer.Cce().V1alpha1().IPPools().Informer()
	crdInformer.Cce().V1alpha1().Subnets().Informer()
	crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Informer()

	kubeInformer.Start(stopChan)
	crdInformer.Start(stopChan)
}

func waitCacheSync(ipam *IPAM, stopChan chan struct{}) {
	startInformer(ipam.kubeInformer, ipam.crdInformer, stopChan)
	__waitForCacheSync(ipam.kubeInformer, ipam.crdInformer, stopChan)
}

// assert for test case
func assertSuite(suite *ipamSubnetTopologySuperTester) {
	suite.ipam.datastore.AddNodeToStore("test-node", "i-xxx")
	suite.ipam.datastore.AddENIToStore("test-node", "eni-test")
	suite.ipam.datastore.AddPrivateIPToStore("test-node", "eni-test", "192.168.1.107", false)
	var (
		err error
		got *networkingv1alpha1.WorkloadEndpoint
	)
	for i := 0; i < 3; i++ {
		select {
		case <-suite.stopChan:
			suite.T().Error("unexcept chan closed")
			return
		default:
		}
		waitCacheSync(suite.ipam, suite.stopChan)
		got, err = suite.ipam.Allocate(suite.ctx, suite.name, suite.namespace, suite.containerID)
		if !suite.wantErr && err != nil && strings.Contains(err.Error(), "not found") {
			suite.T().Logf("Warning: kube informer not found error: %v", err)
		} else {
			break
		}
	}
	if !suite.wantErr {
		if suite.Assert().NoError(err, "Allocate()  ip error") {
			// 更新修改时间，避免测试用例不通过
			if suite.want != nil {
				suite.Assert().NotNil(got, "allocate ip return nil")
				got.Spec.UpdateAt = metav1.Time{Time: time.Unix(0, 0)}
				suite.Assert().EqualValues(suite.want, got, "Allocate()  want not euqal ")
			}
		}
	} else {
		suite.Assert().Error(err, "Allocate()  ip error")
	}
}

func (suite *ipamSubnetTopologySuperTester) BeforeTest(suiteName, testName string) {}
func (suite *ipamSubnetTopologySuperTester) AfterTest(suiteName, testName string)  {}

type dynamicIPCrossSubnetTester struct {
	ipamSubnetTopologySuperTester
}

// create a pod use dymamic IP cross subnet
func (suite *dynamicIPCrossSubnetTester) TestAllocateDynamicIPCrossSubnet() {
	subnet := createSubnet(suite.ipam)
	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(suite.ctx, psts, metav1.CreateOptions{})
	suite.ipam.createPodAndNode(suite.podLabel)

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
			Phase:                   networkingv1alpha1.WorkloadEndpointPhasePodRuning,
		},
	}

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type pstsOtherTest struct {
	ipamSubnetTopologySuperTester
}

// not found the psts object
func (suite *pstsOtherTest) TestPSTSNotFound() {
	suite.ipam.createPodAndNode(suite.podLabel)

	suite.wantErr = true
	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

// allocation dynamic ip cross subnet failed
func (suite *pstsOtherTest) TestFailedLimit() {
	ctx := suite.ctx
	subnet := createSubnet(suite.ipam)
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel), metav1.CreateOptions{})
	suite.ipam.createPodAndNode(suite.podLabel)

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{"192.168.1.109"}, fmt.Errorf("RateLimit")).AnyTimes()

	suite.wantErr = true

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

func (suite *pstsOtherTest) TestFailedSubnetHasNoMoreIpException() {
	ctx := suite.ctx
	subnet := createSubnet(suite.ipam)

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel), metav1.CreateOptions{})
	suite.ipam.createPodAndNode(suite.podLabel)

	suite.wantErr = true
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{"192.168.1.109"}, fmt.Errorf("SubnetHasNoMoreIpException")).AnyTimes()
	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

func (suite *pstsOtherTest) TestFailedPrivateIpInUseException() {
	ctx := suite.ctx
	subnet := createSubnet(suite.ipam)
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel), metav1.CreateOptions{})
	suite.ipam.createPodAndNode(suite.podLabel)

	suite.wantErr = true

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{"192.168.1.109"}, fmt.Errorf("PrivateIpInUseException")).AnyTimes()
	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

func (suite *pstsOtherTest) TestFailedResultLenError() {
	ctx := suite.ctx
	subnet := createSubnet(suite.ipamSubnetTopologySuperTester.ipam)
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel), metav1.CreateOptions{})
	suite.ipam.createPodAndNode(suite.podLabel)

	suite.wantErr = true

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(0), 1).Return([]string{}, nil).AnyTimes()
	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

func createSubnet(ipam *IPAM) *networkingv1alpha1.Subnet {
	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(context.Background(), subnet, metav1.CreateOptions{})
	return subnet
}

func (ipam *IPAM) createPodAndNode(l map[string]string) {
	ctx := context.Background()
	ipam.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: "psts-test",
			},
			Labels: l,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, metav1.CreateOptions{})
	ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}, metav1.CreateOptions{})
}

type fixedIPCrossSubnetTester struct {
	ipamSubnetTopologySuperTester
}

// allocation fixed ip cross subnet first
func (suite *fixedIPCrossSubnetTester) TestAllocationFixedIPCrossSubnetFirst() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeFixed
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyNever
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

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
			Phase:                   networkingv1alpha1.WorkloadEndpointPhasePodRuning,
		},
	}

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type fixedIPCrossSubnetTester2 struct {
	ipamSubnetTopologySuperTester
}

func (suite *fixedIPCrossSubnetTester2) TestAllocationFixedIPCrossSubnetAgain() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	endpoint := data.MockFixedWorkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, endpoint, metav1.CreateOptions{})

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeFixed
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyNever
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

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
			Phase:                   networkingv1alpha1.WorkloadEndpointPhasePodRuning,
		},
	}

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type munualIPCrossSubnetTester struct {
	ipamSubnetTopologySuperTester
}

func (suite *munualIPCrossSubnetTester) TestAllocationMunualIPCrossSubnetFirst() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeManual
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyTTL
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

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
			Phase:                   networkingv1alpha1.WorkloadEndpointPhasePodRuning,
		},
	}

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type munualIPCrossSubnetTester2 struct {
	ipamSubnetTopologySuperTester
}

func (suite *munualIPCrossSubnetTester2) Test__rollbackIPAllocated() {
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

type munualIPCrossSubnetTester3 struct {
	ipamSubnetTopologySuperTester
}

func (suite *munualIPCrossSubnetTester3) TestAllocationMunualIPRangeCrossSubnet() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4Range = append(allocation.IPv4, "192.168.1.109/32")
	allocation.Type = networkingv1alpha1.IPAllocTypeManual
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyTTL
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

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
			Phase:                   networkingv1alpha1.WorkloadEndpointPhasePodRuning,
		},
	}

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type munualIPCrossSubnetTester4 struct {
	ipamSubnetTopologySuperTester
}

func (suite *munualIPCrossSubnetTester4) TestAllocationMunualIPRangeCrossSubnetWithEmptySubnet() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	subnet.Name = ""
	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4Range = append(allocation.IPv4, "192.168.1.109/32")
	allocation.Type = networkingv1alpha1.IPAllocTypeManual
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyTTL
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

	// mock cloud api
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", gomock.Len(1), 1).Return([]string{"192.168.1.109"}, nil).AnyTimes()

	suite.wantErr = true
	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type AllocationFixedIPWithDeleteIPFailed struct {
	ipamSubnetTopologySuperTester
}

func (suite *AllocationFixedIPWithDeleteIPFailed) TestAllocationFixedIPWithDeleteIPFailed() {
	ctx := suite.ctx
	suite.name = "busybox-0"
	wep := data.MockFixedWorkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, wep, metav1.CreateOptions{})

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("RateLimit")).AnyTimes()

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeFixed
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyNever
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

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
			Phase:                   networkingv1alpha1.WorkloadEndpointPhasePodRuning,
		},
	}

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

func (suite *AllocationFixedIPWithDeleteIPFailed) TestAllocationFixedIPWithOldNotInSubnet() {
	ctx := suite.ctx
	suite.name = "busybox-0"
	wep := data.MockFixedWorkloadEndpoint()
	wep.Spec.IP = "127.0.0.1"
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, wep, metav1.CreateOptions{})

	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("RateLimit")).AnyTimes()

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	allocation := psts.Spec.Subnets["sbn-test"]
	allocation.IPv4 = append(allocation.IPv4, "192.168.1.109")
	allocation.Type = networkingv1alpha1.IPAllocTypeFixed
	allocation.ReleaseStrategy = networkingv1alpha1.ReleaseStrategyNever
	psts.Spec.Subnets["sbn-test"] = allocation
	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

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
			Phase:                   networkingv1alpha1.WorkloadEndpointPhasePodRuning,
		},
	}

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

func (ipam *IPAM) createFixedIPPodAndNode(l map[string]string) {
	ipam.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(context.TODO(), &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox-0",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: "psts-test",
			},
			Labels: l,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, metav1.CreateOptions{})
	ipam.kubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}, metav1.CreateOptions{})
}

func (suite *pstsOtherTest) Test__filterAvailableSubnet() {
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

func (suite *pstsOtherTest) Test__allocateIPCrossSubnet() {
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

type SyncRelationOfWepEniTest struct {
	ipamSubnetTopologySuperTester
}

func (suite *SyncRelationOfWepEniTest) TestSyncRelationOfWepEni() {
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

	eniCache := ipcache.NewCacheMapArray[*enisdk.Eni]()
	eniCache.Append("node-test", eni)
	suite.ipam.eniCache = eniCache

	wep := data.MockFixedWorkloadEndpoint()
	wep.Name = "w1"
	wep.Spec.IP = "10.0.0.1"

	suite.ipam.allocated.Add(wep.Spec.IP, wep)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep = data.MockFixedWorkloadEndpoint()
	wep.Name = "w2"
	wep.Spec.IP = "10.0.0.2"
	wep.Spec.ENIID = "eni-change"
	wep.Spec.UpdateAt = metav1.Time{Time: time.Unix(0, 0)}
	suite.ipam.allocated.Add(wep.Spec.IP, wep)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep = data.MockFixedWorkloadEndpoint()
	wep.Name = "w3"
	wep.Spec.IP = "10.0.0.3"
	wep.Spec.ENIID = "eni-change"
	wep.Spec.UpdateAt = metav1.Time{Time: time.Now()}
	suite.ipam.allocated.Add(wep.Spec.IP, wep)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	wep = data.MockFixedWorkloadEndpoint()
	wep.Spec.EnableFixIP = "false"
	wep.Name = "w4"
	wep.Spec.IP = "10.0.0.4"
	wep.Spec.ENIID = "eni-change"
	wep.Spec.UpdateAt = metav1.Time{Time: time.Now()}
	suite.ipam.allocated.Add(wep.Spec.IP, wep)
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Create(suite.ctx, wep, metav1.CreateOptions{})

	waitCacheSync(suite.ipam, suite.stopChan)
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

type AllocationCustomModeTester struct {
	ipamSubnetTopologySuperTester
}

// allocate IP from custom mode
func (suite *AllocationCustomModeTester) TestAllocationCustomMode() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:            networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy: networkingv1alpha1.ReleaseStrategyTTL,
		TTL:             networkingv1alpha1.DefaultReuseIPTTL,
	}

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

	// mock cloud api, and check the first available ip 192.168.1.2
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", []string{"192.168.1.2"}, 1).Return([]string{"192.168.1.2"}, nil).AnyTimes()

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type AllocationCustomModeFromIPRangeTester struct {
	ipamSubnetTopologySuperTester
}

func (suite *AllocationCustomModeFromIPRangeTester) TestAllocationCustomModeFromIPRange() {
	ctx := suite.ctx
	suite.name = "busybox-0"

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:            networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy: networkingv1alpha1.ReleaseStrategyTTL,
		TTL:             networkingv1alpha1.DefaultReuseIPTTL,
	}
	allocation := psts.Spec.Subnets["sbn-test"]
	custom := networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				Start: "192.168.1.56",
				End:   "192.168.1.56",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

	// mock cloud api, and check the first available ip 192.168.1.2
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", []string{"192.168.1.56"}, 1).Return([]string{"192.168.1.56"}, nil).AnyTimes()

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type AllocationCustomModeReuseOldIPNotInIPRangeTester struct {
	ipamSubnetTopologySuperTester
}

// The previously allocated IP address is not in the scope of the new psts
func (suite *AllocationCustomModeReuseOldIPNotInIPRangeTester) TestAllocationCustomModeReuseOldIPNotInIPRange() {
	ctx := suite.ctx
	suite.name = "busybox-0"
	wep := data.MockFixedWorkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, wep, metav1.CreateOptions{})

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:                 networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy:      networkingv1alpha1.ReleaseStrategyTTL,
		TTL:                  networkingv1alpha1.DefaultReuseIPTTL,
		EnableReuseIPAddress: true,
	}
	allocation := psts.Spec.Subnets["sbn-test"]
	custom := networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				Start: "192.168.1.56",
				End:   "192.168.1.57",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

	// mock cloud api, and check the first available ip 192.168.1.2
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", []string{"192.168.1.56"}, 1).Return([]string{"192.168.1.56"}, nil).AnyTimes()

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type AllocationCustomModeReuseInIPRangeTester struct {
	ipamSubnetTopologySuperTester
}

func (suite *AllocationCustomModeReuseInIPRangeTester) TestAllocationCustomModeReuseInIPRange() {
	ip := "192.168.1.57"
	ctx := suite.ctx
	suite.name = "busybox-0"
	wep := data.MockFixedWorkloadEndpoint()
	wep.Spec.IP = "192.168.10.57"
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, wep, metav1.CreateOptions{})

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:                 networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy:      networkingv1alpha1.ReleaseStrategyTTL,
		TTL:                  networkingv1alpha1.DefaultReuseIPTTL,
		EnableReuseIPAddress: true,
	}
	allocation := psts.Spec.Subnets["sbn-test"]
	custom := networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				Start: ip,
				End:   "192.168.1.57",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

	// mock cloud api, and check the first available ip 192.168.1.2
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", []string{ip}, 1).Return([]string{ip}, nil).AnyTimes()
	suite.wantErr = false
	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

type AllocationCustomModeReuseInIPRangeNoTTLTester struct {
	ipamSubnetTopologySuperTester
}

func (suite *AllocationCustomModeReuseInIPRangeNoTTLTester) TestAllocationCustomModeReuseInIPRange() {
	ip := "192.168.1.57"
	ctx := suite.ctx
	suite.name = "busybox-0"
	wep := data.MockFixedWorkloadEndpoint()
	wep.Spec.IP = ip
	suite.ipam.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(ctx, wep, metav1.CreateOptions{})

	subnet := data.MockSubnet(corev1.NamespaceDefault, "sbn-test", "192.168.1.0/24")
	subnet.Spec.Exclusive = true
	suite.ipam.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(ctx, subnet, metav1.CreateOptions{})

	psts := data.MockPodSubnetTopologySpreadWithSubnet(corev1.NamespaceDefault, "psts-test", subnet, suite.podLabel)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:                 networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy:      networkingv1alpha1.ReleaseStrategyTTL,
		EnableReuseIPAddress: true,
	}
	allocation := psts.Spec.Subnets["sbn-test"]
	custom := networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				Start: ip,
				End:   "192.168.1.57",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation

	suite.ipam.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(ctx, psts, metav1.CreateOptions{})

	suite.ipam.createFixedIPPodAndNode(suite.podLabel)

	// mock cloud api, and check the first available ip 192.168.1.2
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().DeletePrivateIP(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddPrivateIpCrossSubnet(gomock.Any(), "eni-test", "sbn-test", []string{ip}, 1).Return([]string{ip}, nil).AnyTimes()

	assertSuite(&suite.ipamSubnetTopologySuperTester)
}

func TestIPAM_subnetTopologyAllocates(t *testing.T) {
	t.Parallel()
	test := new(ipamSubnetTopologySuperTester)

	suite.Run(t, test)

}

func TestIPAM2(t *testing.T) {
	t.Parallel()
	test := new(dynamicIPCrossSubnetTester)

	suite.Run(t, test)

}

func TestIPAM3(t *testing.T) {
	t.Parallel()
	test := new(fixedIPCrossSubnetTester)

	suite.Run(t, test)

}

func TestIPAM4(t *testing.T) {
	t.Parallel()
	test := new(fixedIPCrossSubnetTester2)

	suite.Run(t, test)

}

func TestIPAM5(t *testing.T) {
	t.Parallel()
	test := new(AllocationFixedIPWithDeleteIPFailed)

	suite.Run(t, test)

}

func TestIPAM6(t *testing.T) {
	t.Parallel()
	test := new(munualIPCrossSubnetTester)

	suite.Run(t, test)

}

func TestIPAM7(t *testing.T) {
	t.Parallel()
	test := new(munualIPCrossSubnetTester2)

	suite.Run(t, test)

}

func TestIPAM8(t *testing.T) {
	t.Parallel()
	test := new(munualIPCrossSubnetTester3)

	suite.Run(t, test)

}

func TestIPAM9(t *testing.T) {
	t.Parallel()
	test := new(munualIPCrossSubnetTester4)

	suite.Run(t, test)

}

func TestIPAM10(t *testing.T) {
	t.Parallel()
	test := new(SyncRelationOfWepEniTest)

	suite.Run(t, test)

}

func TestIPAM11(t *testing.T) {
	t.Parallel()
	test := new(AllocationCustomModeTester)

	suite.Run(t, test)

}

func TestIPAM12(t *testing.T) {
	t.Parallel()
	test := new(AllocationCustomModeFromIPRangeTester)

	suite.Run(t, test)

}

func TestIPAM13(t *testing.T) {
	t.Parallel()
	test := new(AllocationCustomModeReuseOldIPNotInIPRangeTester)

	suite.Run(t, test)

}

func TestIPAM14(t *testing.T) {
	t.Parallel()
	test := new(AllocationCustomModeReuseInIPRangeTester)

	suite.Run(t, test)

}

func TestIPAM15(t *testing.T) {
	t.Parallel()
	test := new(pstsOtherTest)
	suite.Run(t, test)
}
