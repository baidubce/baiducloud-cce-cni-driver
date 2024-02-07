package topology_spread

import (
	"context"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	clientv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listercorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

var (
	pstsName = "psts-test"
	sbnName  = "sbn-test"
	podLabel = labels.Set{
		"k8s.io/app": "busybox",
	}
)

// mock and contruct a new TopologySpreadController
// waring it use gomock
func MockTopologySpreadController(t *testing.T) (*TopologySpreadController, kubernetes.Interface) {
	kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, ebc, _ := data.NewMockEnv(gomock.NewController(t))
	sbnc := subnet.NewSubnetController(crdInformer, crdClient, cloudClient, ebc)
	tsc := NewTopologySpreadController(kubeInformer, crdInformer, crdClient, ebc, sbnc)
	return tsc, kubeClient
}

type TopologySpreadControllerTester struct {
	suite.Suite
	tsc        *TopologySpreadController
	kubeClient kubernetes.Interface
	key        string
	wantErr    bool
}

func (suite *TopologySpreadControllerTester) SetupTest() {
	suite.tsc, suite.kubeClient = MockTopologySpreadController(suite.T())
	suite.key = "default/psts-test"
	suite.wantErr = false
}

func (suite *TopologySpreadControllerTester) assert() {
	stopchan := make(chan struct{})
	defer close(stopchan)
	suite.tsc.crdInformer.Start(stopchan)
	suite.tsc.kubeInformer.Start(stopchan)
	suite.tsc.waitCache(stopchan)

	err := suite.tsc.sync(suite.key)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
	err = suite.tsc.sync(suite.key)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
}

func (suite *TopologySpreadControllerTester) TestTSCRun() {
	stopCh := make(chan struct{})
	close(stopCh)
	suite.tsc.Run(stopCh)
}

func (suite *TopologySpreadControllerTester) TestTopologySpreadNotFound() {
	suite.tsc.queue.Add(suite.key)
	b := suite.tsc.processNextWorkItem()
	suite.True(b, "processNextWorkItem return false value")
	suite.assert()
}

func (suite *TopologySpreadControllerTester) TestPSTSSubnetIsNil() {
	tsc := suite.tsc
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, pstsName, sbnName, podLabel)
	psts.Spec.Subnets = make(map[string]networkv1alpha1.SubnetAllocation)
	psts, err := tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts, metav1.CreateOptions{})
	suite.NoError(err, "create psts error")

	suite.assert()

	suite.tsc.queue.Add(suite.key)
	b := suite.tsc.processNextWorkItem()
	suite.True(b, "processNextWorkItem return false value")
}

func (suite *TopologySpreadControllerTester) TestTrySyncSubnetNotFound() {
	tsc := suite.tsc
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, pstsName, sbnName, podLabel)
	_, err := tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts, metav1.CreateOptions{})
	suite.NoError(err, "create psts error")

	stopchan := make(chan struct{})
	defer close(stopchan)
	suite.tsc.crdInformer.Start(stopchan)
	suite.tsc.kubeInformer.Start(stopchan)
	suite.tsc.waitCache(stopchan)

	psts = nil
	for psts == nil {
		psts, err = suite.tsc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Lister().PodSubnetTopologySpreads(corev1.NamespaceDefault).Get(pstsName)
		time.Sleep(50 * time.Millisecond)
	}

	err = suite.tsc.syncPSTSStatus(psts)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
}

func (suite *TopologySpreadControllerTester) TestTrySyncSubnetEnable() {
	suite.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(context.TODO(), &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: pstsName,
			},
			Labels: podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, metav1.CreateOptions{})
	tsc := suite.tsc

	sbn := data.MockSubnet(corev1.NamespaceDefault, sbnName, "10.0.0.1/24")
	tsc.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(context.TODO(), sbn, metav1.CreateOptions{})
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, pstsName, sbnName, podLabel)
	psts, err := tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts, metav1.CreateOptions{})
	suite.NoError(err, "create psts error")

	stopchan := make(chan struct{})
	defer close(stopchan)
	suite.tsc.crdInformer.Start(stopchan)
	suite.tsc.kubeInformer.Start(stopchan)
	suite.tsc.waitCache(stopchan)
	psts = nil
	for psts == nil {
		psts, err = suite.tsc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Lister().PodSubnetTopologySpreads(corev1.NamespaceDefault).Get(pstsName)
		time.Sleep(50 * time.Millisecond)
	}

	err = suite.tsc.syncPSTSStatus(psts)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
}

func (suite *TopologySpreadControllerTester) TestTrySyncWithWEP() {
	suite.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(context.TODO(), &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox-0",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: pstsName,
			},
			Labels: podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, metav1.CreateOptions{})
	tsc := suite.tsc

	wep := data.MockFixedWorkloadEndpoint()
	tsc.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(context.TODO(), wep, metav1.CreateOptions{})

	sbn := data.MockSubnet(corev1.NamespaceDefault, sbnName, "192.168.1.0/24")
	tsc.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(context.TODO(), sbn, metav1.CreateOptions{})
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, pstsName, sbnName, podLabel)
	psts, err := tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts, metav1.CreateOptions{})
	suite.NoError(err, "create psts error")

	stopchan := make(chan struct{})
	defer close(stopchan)
	suite.tsc.crdInformer.Start(stopchan)
	suite.tsc.kubeInformer.Start(stopchan)
	suite.tsc.waitCache(stopchan)

	psts = nil
	for psts == nil {
		psts, err = suite.tsc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Lister().PodSubnetTopologySpreads(corev1.NamespaceDefault).Get(pstsName)
		time.Sleep(50 * time.Millisecond)
	}

	err = suite.tsc.syncPSTSStatus(psts)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
}

func (suite *TopologySpreadControllerTester) TestTrySyncWithUpdatePod() {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busybox-0",
			Annotations: map[string]string{
				networking.AnnotationPodSubnetTopologySpread: pstsName,
			},
			Labels: podLabel,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}
	suite.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(context.TODO(), pod, metav1.CreateOptions{})

	pod1 := pod.DeepCopy()
	pod1.Name = "pod-1"
	pod1.Spec.HostNetwork = true
	suite.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(context.TODO(), pod1, metav1.CreateOptions{})

	pod2 := pod.DeepCopy()
	pod2.Name = "pod-2"
	suite.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Create(context.TODO(), pod2, metav1.CreateOptions{})

	tsc := suite.tsc

	wep := data.MockFixedWorkloadEndpoint()
	tsc.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(context.TODO(), wep, metav1.CreateOptions{})
	wep2 := data.MockFixedWorkloadEndpoint()
	wep2.Name = "pod-2"
	wep2.Spec.SubnetTopologyReference = "psts-test-2"
	tsc.crdClient.CceV1alpha1().WorkloadEndpoints(corev1.NamespaceDefault).Create(context.TODO(), wep2, metav1.CreateOptions{})

	sbn := data.MockSubnet(corev1.NamespaceDefault, sbnName, "192.168.1.0/24")
	tsc.crdClient.CceV1alpha1().Subnets(corev1.NamespaceDefault).Create(context.TODO(), sbn, metav1.CreateOptions{})
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, pstsName, sbnName, podLabel)
	psts, err := tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts, metav1.CreateOptions{})
	suite.NoError(err, "create psts error")

	stopchan := make(chan struct{})
	defer close(stopchan)
	suite.tsc.crdInformer.Start(stopchan)
	suite.tsc.kubeInformer.Start(stopchan)
	suite.tsc.waitCache(stopchan)

	psts = nil
	for psts == nil {
		psts, err = suite.tsc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Lister().PodSubnetTopologySpreads(corev1.NamespaceDefault).Get(pstsName)
		time.Sleep(50 * time.Millisecond)
	}

	err = suite.tsc.syncPSTSStatus(psts)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}

	pod = pod.DeepCopy()
	pod.Annotations["ipdate"] = "true"
	suite.kubeClient.CoreV1().Pods(corev1.NamespaceDefault).Update(context.TODO(), pod, metav1.UpdateOptions{})

	tsc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Delete(context.TODO(), pstsName, metav1.DeleteOptions{})
}

func TestTopologySpreadController_sync(t *testing.T) {
	suite.Run(t, new(TopologySpreadControllerTester))
}

func TestTopologySpreadController_syncPSTSStatus(t *testing.T) {
	type fields struct {
		queue         workqueue.RateLimitingInterface
		eventRecorder record.EventRecorder
		kubeInformer  informers.SharedInformerFactory
		crdInformer   crdinformers.SharedInformerFactory
		crdClient     versioned.Interface
		pstsLister    clientv1alpha1.PodSubnetTopologySpreadLister
		subnetLister  clientv1alpha1.SubnetLister
		wepLister     clientv1alpha1.WorkloadEndpointLister
		podLister     listercorev1.PodLister
		sbnController *subnet.SubnetController
		psttc         *topologySpreadTableController
	}
	type args struct {
		psts *networkv1alpha1.PodSubnetTopologySpread
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tsc := &TopologySpreadController{
				queue:         tt.fields.queue,
				eventRecorder: tt.fields.eventRecorder,
				kubeInformer:  tt.fields.kubeInformer,
				crdInformer:   tt.fields.crdInformer,
				crdClient:     tt.fields.crdClient,
				pstsLister:    tt.fields.pstsLister,
				subnetLister:  tt.fields.subnetLister,
				wepLister:     tt.fields.wepLister,
				podLister:     tt.fields.podLister,
				sbnController: tt.fields.sbnController,
				psttc:         tt.fields.psttc,
			}
			if err := tsc.syncPSTSStatus(tt.args.psts); (err != nil) != tt.wantErr {
				t.Errorf("TopologySpreadController.syncPSTSStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
