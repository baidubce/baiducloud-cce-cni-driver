package topology_spread

import (
	"context"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	cache "k8s.io/client-go/tools/cache"

	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
)

func MocktopologySpreadTableControllerTester(t *testing.T) *topologySpreadTableController {
	_, kubeInformer, crdClient, crdInformer, _, _, ebc := data.NewMockEnv(gomock.NewController(t))

	psttc := NewTopologySpreadTableController(crdInformer, crdClient, ebc)
	kubeInformer.Start(make(<-chan struct{}))
	crdInformer.Start(make(<-chan struct{}))
	return psttc
}

type topologySpreadTableControllerTester struct {
	suite.Suite
	psttc   *topologySpreadTableController
	key     string
	wantErr bool
}

func (suite *topologySpreadTableControllerTester) SetupTest() {
	suite.psttc = MocktopologySpreadTableControllerTester(suite.T())
	suite.key = "default"
	suite.wantErr = false
}

func (suite *topologySpreadTableControllerTester) assert() {
	suite.waitForCache()
	err := suite.psttc.sync(suite.key)
	if suite.wantErr {
		suite.Error(err, "sync error is not match")
	} else {
		suite.NoError(err, "sync have error")
	}
}

func (suite *topologySpreadTableControllerTester) waitForCache() {
	tsc := suite.psttc
	pstsInfomer := tsc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreads().Informer()
	psttInfomer := tsc.crdInformer.Cce().V1alpha1().PodSubnetTopologySpreadTables().Informer()
	cache.WaitForNamedCacheSync("topology-spread-controller", wait.NeverStop, pstsInfomer.HasSynced, psttInfomer.HasSynced)
}

func (suite *topologySpreadTableControllerTester) TestTSCRun() {
	stopCh := make(chan struct{})
	close(stopCh)
	suite.psttc.Run(stopCh)
}

func (suite *topologySpreadTableControllerTester) TestCreatePSTS() {
	pstt := &networkv1alpha1.PodSubnetTopologySpreadTable{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: corev1.NamespaceDefault,
			Name:      "pstt-test",
		},
	}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)
	psts.Spec.Name = psts.Name
	pstt.Spec = append(pstt.Spec, psts.Spec)

	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreadTables(corev1.NamespaceDefault).Create(context.TODO(), pstt, metav1.CreateOptions{})

	suite.assert()
	suite.waitForCache()

	suite.psttc.queue.Add(corev1.NamespaceDefault)
	suite.psttc.processNextWorkItem()
	suite.psttc.queue.Add(corev1.NamespaceDefault)
	suite.psttc.processNextWorkItem()
}

func (suite *topologySpreadTableControllerTester) TestSyncPSTSStatus() {
	pstt := &networkv1alpha1.PodSubnetTopologySpreadTable{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: corev1.NamespaceDefault,
			Name:      "pstt-test",
		},
	}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)
	psts.Spec.Name = "psts-test"
	psts.Status.Name = "psts-test"
	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts, metav1.CreateOptions{})
	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).UpdateStatus(context.TODO(), psts, metav1.UpdateOptions{})

	pstt.Spec = append(pstt.Spec, psts.Spec)

	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreadTables(corev1.NamespaceDefault).Create(context.TODO(), pstt, metav1.CreateOptions{})

	suite.assert()

	suite.waitForCache()
	newpstt, _ := suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreadTables(corev1.NamespaceDefault).Get(context.Background(), "pstt-test", metav1.GetOptions{})
	suite.Assert().Len(newpstt.Status, 1, "len of pstt statuss")
}

func (suite *topologySpreadTableControllerTester) TestCleanOldPSTS() {
	pstt := &networkv1alpha1.PodSubnetTopologySpreadTable{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: corev1.NamespaceDefault,
			Name:      "pstt-test",
		},
	}
	psts := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test", "sbn-test", podLabel)
	psts.Spec.Name = "psts-test"
	psts.Status.Name = "psts-test"

	pstt.Spec = append(pstt.Spec, psts.Spec)

	// to delete
	psts1 := data.MockPodSubnetTopologySpread(corev1.NamespaceDefault, "psts-test-1", "sbn-test", podLabel)
	psts1.ObjectMeta.OwnerReferences = append(psts1.ObjectMeta.OwnerReferences, *metav1.NewControllerRef(pstt, networkv1alpha1.SchemeGroupVersion.WithKind("PodSubnetTopologySpreadTable")))
	psts1.Status.Name = "psts-test-1"
	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts1, metav1.CreateOptions{})

	pstt.Status = append(pstt.Status, psts1.Status)

	// to update
	psts = psts.DeepCopy()
	psts.Spec.Priority = 5
	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).Create(context.TODO(), psts, metav1.CreateOptions{})
	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreads(corev1.NamespaceDefault).UpdateStatus(context.TODO(), psts, metav1.UpdateOptions{})

	suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreadTables(corev1.NamespaceDefault).Create(context.TODO(), pstt, metav1.CreateOptions{})

	suite.assert()

	suite.waitForCache()
	newpstt, _ := suite.psttc.crdClient.CceV1alpha1().PodSubnetTopologySpreadTables(corev1.NamespaceDefault).Get(context.Background(), "pstt-test", metav1.GetOptions{})
	suite.Assert().Len(newpstt.Status, 1, "len of pstt statuss")
}

func Test_topologySpreadTableControllerTester_sync(t *testing.T) {
	suite.Run(t, new(topologySpreadTableControllerTester))
}
