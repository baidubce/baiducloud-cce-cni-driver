package roce

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/stretchr/testify/suite"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

func setupEnv(ctrl *gomock.Controller) (
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
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	return kubeClient, kubeInformer,
		crdClient, crdInformer, cloudClient,
		eventBroadcaster, recorder
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

func TestIPAM_Allocate(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		lock                 sync.RWMutex
		hpcEniCache          map[string][]hpc.Result
		nodeCache            map[string]*corev1.Node
		privateIPNumCache    map[string]int
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.MultiIPWorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		clock                clock.Clock
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
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
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					hpcEniCache:      map[string][]hpc.Result{},
					nodeCache:        make(map[string]*corev1.Node),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
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
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					hpcEniCache:      map[string][]hpc.Result{},
					nodeCache:        make(map[string]*corev1.Node),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
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
			name: "create normal pod without allocate ip",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})

				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Any()).Return(&hpc.EniList{
						Result: []hpc.Result{
							{
								EniID: "eni-test",
							},
						},
					}, nil),
					cloudClient.EXPECT().BatchAddHpcEniPrivateIP(
						gomock.Any(), gomock.Any()).
						Return(&hpc.BatchAddPrivateIPResult{
							PrivateIPAddresses: []string{"10.1.1.1"}}, nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					nodeCache:         make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: "default",
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:    "10.1.1.1",
					ENIID: "eni-test",
				},
			},
			wantErr: false,
		},
		{
			name: "create normal pod without allocate ip",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})

				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Type:     ipamgeneric.MwepType,
					NodeName: "test-node",
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})
				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Any()).Return(&hpc.EniList{
						Result: []hpc.Result{
							{
								EniID: "eni-test",
							},
						},
					}, nil),
					cloudClient.EXPECT().BatchAddHpcEniPrivateIP(gomock.Any(),
						gomock.Any()).Return(&hpc.BatchAddPrivateIPResult{
						PrivateIPAddresses: []string{"10.1.1.1"}}, nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					nodeCache:         make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: "default",
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:    "10.1.1.1",
					ENIID: "eni-test",
				},
			},
			wantErr: false,
		},
		{
			name: "create normal pod without allocate ip",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(context.TODO(), &v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "busybox",
						Labels: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				}, metav1.CreateOptions{})

				kubeClient.CoreV1().Nodes().Create(context.TODO(), &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "cce://i-xxxxx",
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Any()).Return(&hpc.EniList{
						Result: []hpc.Result{
							{
								EniID: "eni-test",
							},
						},
					}, nil),
					cloudClient.EXPECT().BatchAddHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(&hpc.BatchAddPrivateIPResult{PrivateIPAddresses: []string{}}, nil),
					cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Any()).Return(&hpc.EniList{
						Result: []hpc.Result{
							{
								EniID: "eni-test",
							},
						},
					}, nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:    true,
					allocated:         map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					privateIPNumCache: map[string]int{},
					eventBroadcaster:  brdcaster,
					eventRecorder:     recorder,
					kubeInformer:      kubeInformer,
					kubeClient:        kubeClient,
					crdInformer:       crdInformer,
					crdClient:         crdClient,
					cloud:             cloudClient,
					clock:             clock.NewFakeClock(time.Unix(0, 0)),
					nodeCache:         make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want: &v1alpha1.WorkloadEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "busybox",
					Namespace: "default",
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					ENIID: "eni-test",
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
				lock:                 tt.fields.lock,
				hpcEniCache:          tt.fields.hpcEniCache,
				nodeCache:            tt.fields.nodeCache,
				cacheHasSynced:       tt.fields.cacheHasSynced,
				allocated:            tt.fields.allocated,
				eventBroadcaster:     tt.fields.eventBroadcaster,
				eventRecorder:        tt.fields.eventRecorder,
				kubeInformer:         tt.fields.kubeInformer,
				kubeClient:           tt.fields.kubeClient,
				crdInformer:          tt.fields.crdInformer,
				crdClient:            tt.fields.crdClient,
				cloud:                tt.fields.cloud,
				clock:                tt.fields.clock,
				eniSyncPeriod:        tt.fields.eniSyncPeriod,
				informerResyncPeriod: tt.fields.informerResyncPeriod,
				gcPeriod:             tt.fields.gcPeriod,
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

func TestIPAM_Release(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		lock                 sync.RWMutex
		hpcEniCache          map[string][]hpc.Result
		nodeCache            map[string]*corev1.Node
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.MultiIPWorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		clock                clock.Clock
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
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
			name: "delete normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: false,
		},
		{
			name: "delete normal pod batch delete HpcEni privateIP has error",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(fmt.Errorf("")),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: false,
		},
		{
			name: "delete a pod that has been deleted",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": {{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:                 tt.fields.lock,
				hpcEniCache:          tt.fields.hpcEniCache,
				cacheHasSynced:       tt.fields.cacheHasSynced,
				allocated:            tt.fields.allocated,
				eventBroadcaster:     tt.fields.eventBroadcaster,
				eventRecorder:        tt.fields.eventRecorder,
				kubeInformer:         tt.fields.kubeInformer,
				kubeClient:           tt.fields.kubeClient,
				crdInformer:          tt.fields.crdInformer,
				crdClient:            tt.fields.crdClient,
				cloud:                tt.fields.cloud,
				clock:                tt.fields.clock,
				eniSyncPeriod:        tt.fields.eniSyncPeriod,
				informerResyncPeriod: tt.fields.informerResyncPeriod,
				gcPeriod:             tt.fields.gcPeriod,
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

func TestIPAM_buildAllocatedCache(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		lock                 sync.RWMutex
		hpcEniCache          map[string][]hpc.Result
		nodeCache            map[string]*corev1.Node
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.MultiIPWorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		clock                clock.Clock
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
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
			name: "buildAllocatedCache normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
						Labels: map[string]string{
							ipamgeneric.NodeInstanceType: "",
						},
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:                 tt.fields.lock,
				hpcEniCache:          tt.fields.hpcEniCache,
				cacheHasSynced:       tt.fields.cacheHasSynced,
				allocated:            tt.fields.allocated,
				eventBroadcaster:     tt.fields.eventBroadcaster,
				eventRecorder:        tt.fields.eventRecorder,
				kubeInformer:         tt.fields.kubeInformer,
				kubeClient:           tt.fields.kubeClient,
				crdInformer:          tt.fields.crdInformer,
				crdClient:            tt.fields.crdClient,
				cloud:                tt.fields.cloud,
				clock:                tt.fields.clock,
				eniSyncPeriod:        tt.fields.eniSyncPeriod,
				informerResyncPeriod: tt.fields.informerResyncPeriod,
				gcPeriod:             tt.fields.gcPeriod,
			}
			err := ipam.buildAllocatedCache(context.TODO())
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_buildInstanceIdToNodeNameMap(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		lock                 sync.RWMutex
		hpcEniCache          map[string][]hpc.Result
		nodeCache            map[string]*corev1.Node
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.MultiIPWorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		clock                clock.Clock
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
		nodes                []*v1.Node
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
			name: "buildAllocatedCache normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				nodes := []*v1.Node{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: "test-node1",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fshfhsfs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: "test-node2",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fsfsgreeefs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test3",
						Namespace: "test-node3",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fswwqqqqfsfs",
					},
				},
				}

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
					nodes:            nodes,
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:                 tt.fields.lock,
				hpcEniCache:          tt.fields.hpcEniCache,
				cacheHasSynced:       tt.fields.cacheHasSynced,
				allocated:            tt.fields.allocated,
				eventBroadcaster:     tt.fields.eventBroadcaster,
				eventRecorder:        tt.fields.eventRecorder,
				kubeInformer:         tt.fields.kubeInformer,
				kubeClient:           tt.fields.kubeClient,
				crdInformer:          tt.fields.crdInformer,
				crdClient:            tt.fields.crdClient,
				cloud:                tt.fields.cloud,
				clock:                tt.fields.clock,
				eniSyncPeriod:        tt.fields.eniSyncPeriod,
				informerResyncPeriod: tt.fields.informerResyncPeriod,
				gcPeriod:             tt.fields.gcPeriod,
			}
			ipam.buildInstanceIdToNodeNameMap(context.TODO(), tt.fields.nodes)
		})
	}
}

func TestIPAM_buildAllocatedNodeCachep(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		lock                 sync.RWMutex
		hpcEniCache          map[string][]hpc.Result
		nodeCache            map[string]*corev1.Node
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.MultiIPWorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		clock                clock.Clock
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
		nodes                []*v1.Node
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
			name: "buildAllocatedCache normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				nodes := []*v1.Node{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: "test-node1",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fshfhsfs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: "test-node2",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fsfsgreeefs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test3",
						Namespace: "test-node3",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fswwqqqqfsfs",
					},
				},
				}

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
					nodes:            nodes,
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:                 tt.fields.lock,
				hpcEniCache:          tt.fields.hpcEniCache,
				cacheHasSynced:       tt.fields.cacheHasSynced,
				allocated:            tt.fields.allocated,
				eventBroadcaster:     tt.fields.eventBroadcaster,
				eventRecorder:        tt.fields.eventRecorder,
				kubeInformer:         tt.fields.kubeInformer,
				kubeClient:           tt.fields.kubeClient,
				crdInformer:          tt.fields.crdInformer,
				crdClient:            tt.fields.crdClient,
				cloud:                tt.fields.cloud,
				clock:                tt.fields.clock,
				eniSyncPeriod:        tt.fields.eniSyncPeriod,
				informerResyncPeriod: tt.fields.informerResyncPeriod,
				gcPeriod:             tt.fields.gcPeriod,
			}
			err := ipam.buildAllocatedNodeCache(context.TODO())
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_rebuildNodeInfoCache(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		lock                 sync.RWMutex
		hpcEniCache          map[string][]hpc.Result
		nodeCache            map[string]*corev1.Node
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.MultiIPWorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		clock                clock.Clock
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
		nodes                []*v1.Node
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
			name: "buildAllocatedCache normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)
				gomock.InOrder(
					cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Any()).Return(&hpc.EniList{
						Result: []hpc.Result{
							{
								EniID: "eni-test",
							},
						},
					}, nil),
				)
				nodes := []*v1.Node{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: "test-node1",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fshfhsfs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: "test-node2",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fsfsgreeefs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test3",
						Namespace: "test-node3",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fswwqqqqfsfs",
					},
				},
				}

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
					nodes:            nodes,
					nodeCache:        make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: false,
		},
		{
			name: "buildAllocatedCache normal pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)
				gomock.InOrder(
					cloudClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Any()).Return(&hpc.EniList{
						Result: []hpc.Result{
							{
								EniID: "eni-test",
							},
						},
					}, fmt.Errorf("has error")),
				)
				nodes := []*v1.Node{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: "test-node1",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fshfhsfs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: "test-node2",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fsfsgreeefs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test3",
						Namespace: "test-node3",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fswwqqqqfsfs",
					},
				},
				}

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
					nodes:            nodes,
					nodeCache:        make(map[string]*corev1.Node),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:                 tt.fields.lock,
				hpcEniCache:          tt.fields.hpcEniCache,
				cacheHasSynced:       tt.fields.cacheHasSynced,
				allocated:            tt.fields.allocated,
				eventBroadcaster:     tt.fields.eventBroadcaster,
				eventRecorder:        tt.fields.eventRecorder,
				kubeInformer:         tt.fields.kubeInformer,
				kubeClient:           tt.fields.kubeClient,
				crdInformer:          tt.fields.crdInformer,
				crdClient:            tt.fields.crdClient,
				cloud:                tt.fields.cloud,
				clock:                tt.fields.clock,
				eniSyncPeriod:        tt.fields.eniSyncPeriod,
				informerResyncPeriod: tt.fields.informerResyncPeriod,
				gcPeriod:             tt.fields.gcPeriod,
				nodeCache:            tt.fields.nodeCache,
			}
			err := ipam.rebuildNodeInfoCache(context.TODO(), tt.fields.nodes[0], "i-fswwqqqqfsfs")
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_buildENIIDCache(t *testing.T) {
	type fields struct {
		ctrl                 *gomock.Controller
		lock                 sync.RWMutex
		hpcEniCache          map[string][]hpc.Result
		nodeCache            map[string]*corev1.Node
		cacheHasSynced       bool
		allocated            map[string]*v1alpha1.MultiIPWorkloadEndpoint
		eventBroadcaster     record.EventBroadcaster
		eventRecorder        record.EventRecorder
		kubeInformer         informers.SharedInformerFactory
		kubeClient           kubernetes.Interface
		crdInformer          crdinformers.SharedInformerFactory
		crdClient            versioned.Interface
		cloud                cloud.Interface
		clock                clock.Clock
		eniSyncPeriod        time.Duration
		informerResyncPeriod time.Duration
		gcPeriod             time.Duration
		nodes                []*v1.Node
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
			name: "buildENIIDCache",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				crdClient.CceV1alpha1().WorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.WorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{},
				}, metav1.CreateOptions{})

				crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(v1.NamespaceDefault).Create(context.TODO(), &v1alpha1.MultiIPWorkloadEndpoint{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: []v1alpha1.MultiIPWorkloadEndpointSpec{
						{
							EniID: "eni-1",
							IP:    "10.1.1.1",
						},
					},
				}, metav1.CreateOptions{})

				waitForCacheSync(kubeInformer, crdInformer)

				nodes := []*v1.Node{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: "test-node1",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fshfhsfs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: "test-node2",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fsfsgreeefs",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test3",
						Namespace: "test-node3",
						Labels: map[string]string{
							"beta.kubernetes.io/instance-type": "BBC",
						},
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://i-fswwqqqqfsfs",
					},
				},
				}

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					hpcEniCache: map[string][]hpc.Result{
						"test-node": []hpc.Result{{
							EniID: "eni-test",
						}},
					},
					cacheHasSynced:   true,
					allocated:        map[string]*v1alpha1.MultiIPWorkloadEndpoint{},
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					clock:            clock.NewFakeClock(time.Unix(0, 0)),
					nodes:            nodes,
					nodeCache:        make(map[string]*corev1.Node),
					eniSyncPeriod:    5 * time.Second,
					gcPeriod:         5 * time.Second,
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: v1.NamespaceDefault,
			},
			want:    &v1alpha1.WorkloadEndpoint{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				lock:                 tt.fields.lock,
				hpcEniCache:          tt.fields.hpcEniCache,
				cacheHasSynced:       tt.fields.cacheHasSynced,
				allocated:            tt.fields.allocated,
				eventBroadcaster:     tt.fields.eventBroadcaster,
				eventRecorder:        tt.fields.eventRecorder,
				kubeInformer:         tt.fields.kubeInformer,
				kubeClient:           tt.fields.kubeClient,
				crdInformer:          tt.fields.crdInformer,
				crdClient:            tt.fields.crdClient,
				cloud:                tt.fields.cloud,
				clock:                tt.fields.clock,
				eniSyncPeriod:        tt.fields.eniSyncPeriod,
				informerResyncPeriod: tt.fields.informerResyncPeriod,
				gcPeriod:             tt.fields.gcPeriod,
				nodeCache:            tt.fields.nodeCache,
			}

			err := ipam.buildENIIDCache("", &hpc.EniList{})
			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

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

func (suite *IPAMTest) TearDownTest() {
	close(suite.stopChan)
}

func (suite *IPAMTest) TestIPAMRun() {
	mwep := MockMultiworkloadEndpoint()
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep, metav1.CreateOptions{})

	mwep1 := MockMultiworkloadEndpoint()
	mwep1.Name = "busybox-1"
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep1, metav1.CreateOptions{})

	mwep2 := MockMultiworkloadEndpoint()
	mwep2.Name = "busybox-2"
	suite.ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).Create(suite.ctx, mwep2, metav1.CreateOptions{})

	mockInterface := suite.ipam.cloud.(*mockcloud.MockInterface).EXPECT()
	mockInterface.ListENIs(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("list error")).AnyTimes()
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
	suite.ipam.nodeCache = make(map[string]*corev1.Node)
	mockInterface.BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	go suite.ipam.Run(suite.ctx, suite.stopChan)
	time.Sleep(3 * time.Second)
}

func TestIPAM(t *testing.T) {
	suite.Run(t, new(IPAMTest))
}
