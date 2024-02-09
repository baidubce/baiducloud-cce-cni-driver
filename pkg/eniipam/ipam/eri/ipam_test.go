package eri

import (
	"context"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"reflect"
	"sync"
	"testing"
	"time"
)

type IPAMTest struct {
	suite.Suite
	ipam        *IPAM
	wantErr     bool
	want        *v1alpha1.WorkloadEndpoint
	ctx         context.Context
	name        string
	namespace   string
	containerID string
	podLabel    labels.Set
	stopChan    chan struct{}
}

// 每次测试前设置上下文
func (suite *IPAMTest) SetupTest() {
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

// mock a ipam server
func mockIPAM(t *testing.T, stopChan chan struct{}) *IPAM {
	ctrl := gomock.NewController(t)
	kubeClient, _, crdClient, _, cloudClient := setupEnv(ctrl)
	ipam, _ := NewIPAM(
		"test-vpcid",
		kubeClient,
		crdClient,
		cloudClient,
		20*time.Second,
		300*time.Second,
	)
	ipamServer := ipam.(*IPAM)
	ipamServer.cacheHasSynced = true
	nodeCache := map[string]*corev1.Node{
		"eni-df8888fs": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
			},
		},
	}
	ipamServer.nodeCache = nodeCache

	ipamServer.kubeInformer.Start(stopChan)
	ipamServer.crdInformer.Start(stopChan)
	return ipam.(*IPAM)
}

func setupEnv(ctrl *gomock.Controller) (
	kubernetes.Interface,
	informers.SharedInformerFactory,
	versioned.Interface,
	crdinformers.SharedInformerFactory,
	*mockcloud.MockInterface,
) {
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformer := informers.NewSharedInformerFactory(kubeClient, time.Minute)
	crdClient := crdfake.NewSimpleClientset()
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, time.Minute)
	cloudClient := mockcloud.NewMockInterface(ctrl)
	return kubeClient, kubeInformer,
		crdClient, crdInformer, cloudClient
}

func (suite *IPAMTest) TearDownTest() {
	close(suite.stopChan)
}

func (suite *IPAMTest) TestIPAMRun() {
	mwep := mockMultiWorkloadEndpoint()
	_, _ = suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(
		suite.ctx, mwep, metav1.CreateOptions{})

	mwep1 := mockMultiWorkloadEndpoint()
	mwep1.Name = "busybox-1"
	_, _ = suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(
		suite.ctx, mwep1, metav1.CreateOptions{})

	mwep2 := mockMultiWorkloadEndpoint()
	mwep2.Name = "busybox-2"
	_, _ = suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(
		suite.ctx, mwep2, metav1.CreateOptions{})

	suite.ipam.nodeCache = make(map[string]*corev1.Node)
	go func() {
		_ = suite.ipam.Run(suite.ctx, suite.stopChan)
	}()
	time.Sleep(3 * time.Second)
}

func mockMultiWorkloadEndpoint() *networkingv1alpha1.MultiIPWorkloadEndpoint {
	return &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busybox-0",
			Namespace: "default",
			Labels: map[string]string{
				"beta.kubernetes.io/instance-type": "BCC",
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

func TestIPAM(t *testing.T) {
	suite.Run(t, new(IPAMTest))
}

func TestIPAM_Allocate(t *testing.T) {
	type fields struct {
		ctrl              *gomock.Controller
		lock              sync.RWMutex
		nodeCache         map[string]*corev1.Node
		privateIPNumCache map[string]int
		cacheHasSynced    bool

		kubeInformer informers.SharedInformerFactory
		kubeClient   kubernetes.Interface
		crdInformer  crdinformers.SharedInformerFactory
		crdClient    versioned.Interface
		cloud        cloud.Interface

		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
		eventRecorder        record.EventRecorder
	}
	type args struct {
		ctx         context.Context
		name        string
		namespace   string
		containerID string
		mac         string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *v1alpha1.WorkloadEndpoint
		wantErr bool
	}{
		{
			name: "ipam has not synced cache",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				return fields{
					ctrl:           ctrl,
					cacheHasSynced: false,
				}
			}(),
			args:    args{},
			want:    nil,
			wantErr: true,
		},
		{
			name: "node has no pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient := setupEnv(ctrl)

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:           ctrl,
					lock:           sync.RWMutex{},
					nodeCache:      make(map[string]*corev1.Node),
					cacheHasSynced: true,
					kubeInformer:   kubeInformer,
					kubeClient:     kubeClient,
					crdInformer:    crdInformer,
					crdClient:      crdClient,
					cloud:          cloudClient,
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "node has no eni",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient := setupEnv(ctrl)

				_, podErr := kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, podErr)

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:           ctrl,
					lock:           sync.RWMutex{},
					nodeCache:      make(map[string]*corev1.Node),
					cacheHasSynced: true,
					kubeInformer:   kubeInformer,
					kubeClient:     kubeClient,
					crdInformer:    crdInformer,
					crdClient:      crdClient,
					cloud:          cloudClient,
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "invalid mac",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient := setupEnv(ctrl)

				_, podErr := kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, podErr)

				_, nodeErr := kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, nodeErr)

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]eni.Eni{
						{
							EniId:      "eni-test",
							MacAddress: "mac-test",
						},
					}, nil),
				)

				return fields{
					ctrl:              ctrl,
					lock:              sync.RWMutex{},
					cacheHasSynced:    true,
					privateIPNumCache: map[string]int{},
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					nodeCache:         make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
				mac:       "invalid",
			},
			wantErr: true,
		},
		{
			name: "allocate first ip",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient := setupEnv(ctrl)

				_, podErr := kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, podErr)

				_, nodeErr := kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, nodeErr)

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]eni.Eni{
						{
							EniId:      "eni-test",
							MacAddress: "mac-test",
						},
					}, nil),
					cloudClient.EXPECT().AddPrivateIP(gomock.Any(), gomock.Eq(""), gomock.Eq("eni-test")).
						Return("10.1.1.1", nil),
				)

				return fields{
					ctrl:              ctrl,
					lock:              sync.RWMutex{},
					cacheHasSynced:    true,
					privateIPNumCache: map[string]int{},
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					nodeCache:         make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
				mac:       "mac-test",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: "default",
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:    "10.1.1.1",
					ENIID: "eni-test",
					Mac:   "mac-test",
				},
			},
			wantErr: false,
		},
		{
			name: "already allocated",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient := setupEnv(ctrl)

				_, podErr := kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, podErr)

				_, nodeErr := kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, nodeErr)

				// 准备 mwep
				_, mwepErr := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(
					context.TODO(), &networkingv1alpha1.MultiIPWorkloadEndpoint{
						TypeMeta: metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{
							Name: "busybox",
						},
						NodeName: "test-node",
						Type:     ipamgeneric.MwepTypeERI,
						Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{
							{
								EniID: "eni-test-1",
								IP:    "10.1.1.1",
								Mac:   "mac-test-1",
							},
						},
					}, metav1.CreateOptions{})
				assert.Nil(t, mwepErr)

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]eni.Eni{
						{
							EniId:      "eni-test-1",
							MacAddress: "mac-test-1",
						},
					}, nil),
				)

				return fields{
					ctrl:              ctrl,
					lock:              sync.RWMutex{},
					cacheHasSynced:    true,
					privateIPNumCache: map[string]int{},
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					nodeCache:         make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
				mac:       "mac-test-1",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: v1.NamespaceDefault,
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:    "10.1.1.1",
					ENIID: "eni-test-1",
					Mac:   "mac-test-1",
				},
			},
			wantErr: false,
		},
		{
			name: "allocate other ip",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient := setupEnv(ctrl)

				_, podErr := kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, podErr)

				_, nodeErr := kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				}, metav1.CreateOptions{})
				assert.Nil(t, nodeErr)

				// 准备 mwep
				_, mwepErr := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(
					context.TODO(), &networkingv1alpha1.MultiIPWorkloadEndpoint{
						TypeMeta: metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{
							Name: "busybox",
						},
						NodeName: "test-node",
						Type:     ipamgeneric.MwepTypeERI,
						Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{
							{
								EniID: "eni-test-1",
								IP:    "10.1.1.1",
								Mac:   "mac-test-1",
							},
						},
					}, metav1.CreateOptions{})
				assert.Nil(t, mwepErr)

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]eni.Eni{
						{
							EniId:      "eni-test-1",
							MacAddress: "mac-test-1",
						},
						{
							EniId:      "eni-test-2",
							MacAddress: "mac-test-2",
						},
					}, nil),
					cloudClient.EXPECT().AddPrivateIP(gomock.Any(), gomock.Eq(""), gomock.Eq("eni-test-2")).
						Return("10.1.1.2", nil),
				)

				return fields{
					ctrl:              ctrl,
					lock:              sync.RWMutex{},
					cacheHasSynced:    true,
					privateIPNumCache: map[string]int{},
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					nodeCache:         make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
				mac:       "mac-test-2",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: v1.NamespaceDefault,
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:    "10.1.1.2",
					ENIID: "eni-test-2",
					Mac:   "mac-test-2",
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:           tt.fields.lock,
				nodeCache:      tt.fields.nodeCache,
				cacheHasSynced: tt.fields.cacheHasSynced,
				eventRecorder:  tt.fields.eventRecorder,
				kubeInformer:   tt.fields.kubeInformer,
				kubeClient:     tt.fields.kubeClient,
				crdInformer:    tt.fields.crdInformer,
				crdClient:      tt.fields.crdClient,
				cloud:          tt.fields.cloud,
				gcPeriod:       tt.fields.gcPeriod,
			}
			got, err := ipam.Allocate(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID, tt.args.mac)
			if (err != nil) != tt.wantErr {
				t.Errorf("Allocate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Allocate() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func waitForCacheSync(kubeInformer informers.SharedInformerFactory, crdInformer crdinformers.SharedInformerFactory) {
	nodeInformer := kubeInformer.Core().V1().Nodes().Informer()
	podInformer := kubeInformer.Core().V1().Pods().Informer()
	mwepInformer := crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Informer()
	ippoolInformer := crdInformer.Cce().V1alpha1().IPPools().Informer()
	subnetInformer := crdInformer.Cce().V1alpha1().Subnets().Informer()

	kubeInformer.Start(wait.NeverStop)
	crdInformer.Start(wait.NeverStop)

	cache.WaitForNamedCacheSync(
		"cce-ipam",
		wait.NeverStop,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		mwepInformer.HasSynced,
		ippoolInformer.HasSynced,
		subnetInformer.HasSynced,
	)
}

func TestIPAM_Release(t *testing.T) {
	type fields struct {
		ctrl           *gomock.Controller
		lock           sync.RWMutex
		nodeCache      map[string]*corev1.Node
		cacheHasSynced bool
		kubeInformer   informers.SharedInformerFactory
		kubeClient     kubernetes.Interface
		crdInformer    crdinformers.SharedInformerFactory
		crdClient      versioned.Interface
		cloud          cloud.Interface
		gcPeriod       time.Duration
	}
	type args struct {
		ctx         context.Context
		name        string
		namespace   string
		containerID string
		mac         string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *v1alpha1.WorkloadEndpoint
		wantErr bool
	}{
		{
			name: "delete ip success",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient := setupEnv(ctrl)

				_, mwepErr := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).
					Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
						TypeMeta: metav1.TypeMeta{},
						ObjectMeta: metav1.ObjectMeta{
							Name: "busybox",
						},
						Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
							{
								EniID: "eni-1",
								IP:    "10.1.1.1",
							},
							{
								EniID: "eni-2",
								IP:    "10.1.1.2",
							},
						},
					}, metav1.CreateOptions{})
				assert.Nil(t, mwepErr)

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Eq("10.1.1.1"), gomock.Eq("eni-1")).Return(nil),
					cloudClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Eq("10.1.1.2"), gomock.Eq("eni-2")).Return(nil),
				)

				return fields{
					ctrl:           ctrl,
					lock:           sync.RWMutex{},
					cacheHasSynced: true,
					kubeInformer:   kubeInformer,
					kubeClient:     kubeClient,
					crdInformer:    crdInformer,
					crdClient:      crdClient,
					cloud:          cloudClient,
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: v1.NamespaceDefault,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:           tt.fields.lock,
				cacheHasSynced: tt.fields.cacheHasSynced,
				kubeInformer:   tt.fields.kubeInformer,
				kubeClient:     tt.fields.kubeClient,
				crdInformer:    tt.fields.crdInformer,
				crdClient:      tt.fields.crdClient,
				cloud:          tt.fields.cloud,
				gcPeriod:       tt.fields.gcPeriod,
			}
			got, err := ipam.Release(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Release() got = %v, want %v", got, tt.want)
			}
		})
	}
}
