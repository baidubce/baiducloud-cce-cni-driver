package bbc

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/golang/mock/gomock"
	"github.com/juju/ratelimit"
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

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore"
	ipamtypes "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/keymutex"
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

	crdClient := crdfake.NewSimpleClientset()
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, time.Minute)

	cloudClient := mockcloud.NewMockInterface(ctrl)

	kubeClient := kubefake.NewSimpleClientset()
	kubeInformer := informers.NewSharedInformerFactory(kubeClient, time.Minute)

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-ipam"})

	return kubeClient, kubeInformer,
		crdClient, crdInformer, cloudClient,
		eventBroadcaster, recorder
}

func startInformers(kubeInformer informers.SharedInformerFactory, crdInformer crdinformers.SharedInformerFactory) {
	nodeInformer := kubeInformer.Core().V1().Nodes().Informer()
	podInformer := kubeInformer.Core().V1().Pods().Informer()
	stsInformer := kubeInformer.Apps().V1().StatefulSets().Informer()
	wepInformer := crdInformer.Cce().V1alpha1().WorkloadEndpoints().Informer()
	ippoolInformer := crdInformer.Cce().V1alpha1().IPPools().Informer()
	subnetInformer := crdInformer.Cce().V1alpha1().Subnets().Informer()

	kubeInformer.Start(wait.NeverStop)
	crdInformer.Start(wait.NeverStop)

	cache.WaitForNamedCacheSync(
		"cce-ipam",
		wait.NeverStop,
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		stsInformer.HasSynced,
		wepInformer.HasSynced,
		ippoolInformer.HasSynced,
		subnetInformer.HasSynced,
	)
}

func TestIPAM_Allocate(t *testing.T) {
	type fields struct {
		ctrl             *gomock.Controller
		lock             sync.RWMutex
		nodeLock         keymutex.KeyMutex
		datastore        *datastore.DataStore
		allocated        map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		bucket           *ratelimit.Bucket
		batchAddIPNum    int
		gcPeriod         time.Duration
	}
	type args struct {
		ctx         context.Context
		name        string
		namespace   string
		containerID string
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
			name: "pod not found",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					datastore:        datastore.NewDataStore(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated:        map[string]*v1alpha1.WorkloadEndpoint{},
					bucket:           ratelimit.NewBucket(100, 100),
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
			name: "node not found",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				// add a pod to test environment
				_, _ = kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(&v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				})
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					datastore:        datastore.NewDataStore(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated:        map[string]*v1alpha1.WorkloadEndpoint{},
					bucket:           ratelimit.NewBucket(100, 100),
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
			name: "add pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				// add a pod to test environment
				_, _ = kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(&v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				})
				kubeClient.CoreV1().Nodes().Create(&v1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://test-node-id",
					},
				})

				gomock.InOrder(
					cloudClient.EXPECT().BBCGetInstanceENI(gomock.Any(), "test-node-id").Return(
						&bbc.GetInstanceEniResult{
							Id: "test-node-id",
							PrivateIpSet: []bbc.PrivateIP{
								{
									PrivateIpAddress: "10.1.1.1",
								},
								{
									PrivateIpAddress: "10.1.1.2",
								},
							},
						}, nil),
				)
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					datastore:        datastore.NewDataStore(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated: map[string]*v1alpha1.WorkloadEndpoint{
						"10.1.1.2": {},
					},
					bucket: ratelimit.NewBucket(100, 100),
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
					Labels: map[string]string{
						ipamtypes.WepLabelInstanceTypeKey: "bbc",
					},
				},
				Spec: v1alpha1.WorkloadEndpointSpec{
					IP:       "10.1.1.1",
					ENIID:    "test-node-id",
					Node:     "test-node",
					UpdateAt: metav1.Time{time.Unix(0, 0)},
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
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				cloud:            tt.fields.cloud,
				lock:             tt.fields.lock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				datastore:        tt.fields.datastore,
				allocated:        tt.fields.allocated,
				bucket:           tt.fields.bucket,
				batchAddIPNum:    tt.fields.batchAddIPNum,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				gcPeriod:         tt.fields.gcPeriod,
				clock:            clock.NewFakeClock(time.Unix(0, 0)),
			}
			got, err := ipam.Allocate(tt.args.ctx, tt.args.name, tt.args.namespace, tt.args.containerID)
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
		ctrl             *gomock.Controller
		lock             sync.RWMutex
		nodeLock         keymutex.KeyMutex
		datastore        *datastore.DataStore
		allocated        map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		bucket           *ratelimit.Bucket
		batchAddIPNum    int
		gcPeriod         time.Duration
	}
	type args struct {
		ctx         context.Context
		name        string
		namespace   string
		containerID string
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
			name: "delete a pod that has been deleted",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(&v1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://test-node-id",
					},
				})
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					datastore:        datastore.NewDataStore(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated:        map[string]*v1alpha1.WorkloadEndpoint{},
					bucket:           ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				ctx:       context.TODO(),
				name:      "busybox",
				namespace: "default",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "delete pod",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				// add a wep to test environment
				crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(&v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						IP:       "10.1.1.2",
						ENIID:    "test-node-id",
						Node:     "test-node",
						UpdateAt: metav1.Time{time.Unix(0, 0)},
					},
				})
				kubeClient.CoreV1().Nodes().Create(&v1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://test-node-id",
					},
				})

				gomock.InOrder(
					cloudClient.EXPECT().BBCBatchDelIP(gomock.Any(), gomock.Any()).Return(nil),
				)
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					datastore:        datastore.NewDataStore(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated: map[string]*v1alpha1.WorkloadEndpoint{
						"10.1.1.2": {},
					},
					bucket: ratelimit.NewBucket(100, 100),
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
					IP:       "10.1.1.2",
					ENIID:    "test-node-id",
					Node:     "test-node",
					UpdateAt: metav1.Time{time.Unix(0, 0)},
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
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				cloud:            tt.fields.cloud,
				lock:             tt.fields.lock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				datastore:        tt.fields.datastore,
				allocated:        tt.fields.allocated,
				bucket:           tt.fields.bucket,
				batchAddIPNum:    tt.fields.batchAddIPNum,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				gcPeriod:         tt.fields.gcPeriod,
				clock:            clock.NewFakeClock(time.Unix(0, 0)),
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

func TestIPAM_gcLeakedPod(t *testing.T) {
	type fields struct {
		ctrl             *gomock.Controller
		lock             sync.RWMutex
		nodeLock         keymutex.KeyMutex
		datastore        *datastore.DataStore
		allocated        map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		bucket           *ratelimit.Bucket
		batchAddIPNum    int
		gcPeriod         time.Duration
	}
	type args struct {
		ctx     context.Context
		wepList []*v1alpha1.WorkloadEndpoint
	}

	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "normal",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				// add one pod for test environment
				_, _ = kubeClient.CoreV1().Pods(v1.NamespaceDefault).Create(&v1.Pod{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "busybox",
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
					},
				})
				// add two wep for test environment
				crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(&v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						IP:       "10.1.1.2",
						ENIID:    "test-node-id",
						Node:     "test-node",
						UpdateAt: metav1.Time{time.Unix(0, 0)},
					},
				})
				crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(&v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox2",
						Namespace: "default",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						IP:       "10.1.1.3",
						ENIID:    "test-node-id",
						Node:     "test-node",
						UpdateAt: metav1.Time{time.Unix(0, 0)},
					},
				})
				startInformers(kubeInformer, crdInformer)

				gomock.InOrder(
					cloudClient.EXPECT().BBCBatchDelIP(gomock.Any(), gomock.Any()).Return(nil),
				)

				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					datastore: func() *datastore.DataStore {
						dt := datastore.NewDataStore()
						err := dt.AddNodeToStore("test-node", "test-node-id")
						if err != nil {
							return nil
						}
						err = dt.AddENIToStore("test-node", "test-node-id")
						if err != nil {
							return nil
						}
						err = dt.AddPrivateIPToStore("test-node", "test-node-id", "10.1.1.2", true)
						if err != nil {
							return nil
						}
						err = dt.AddPrivateIPToStore("test-node", "test-node-id", "10.1.1.3", true)
						if err != nil {
							return nil
						}
						return dt
					}(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated:        map[string]*v1alpha1.WorkloadEndpoint{},
					bucket:           ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				wepList: []*v1alpha1.WorkloadEndpoint{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "busybox",
							Namespace: "default",
						},
						Spec: v1alpha1.WorkloadEndpointSpec{
							IP:       "10.1.1.2",
							ENIID:    "test-node-id",
							Node:     "test-node",
							UpdateAt: metav1.Time{time.Unix(0, 0)},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "busybox2",
							Namespace: "default",
						},
						Spec: v1alpha1.WorkloadEndpointSpec{
							IP:       "10.1.1.3",
							ENIID:    "test-node-id",
							Node:     "test-node",
							UpdateAt: metav1.Time{time.Unix(0, 0)},
						},
					},
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
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				cloud:            tt.fields.cloud,
				lock:             tt.fields.lock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				datastore:        tt.fields.datastore,
				allocated:        tt.fields.allocated,
				bucket:           tt.fields.bucket,
				batchAddIPNum:    tt.fields.batchAddIPNum,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				gcPeriod:         tt.fields.gcPeriod,
				clock:            clock.NewFakeClock(time.Unix(0, 0)),
			}
			err := ipam.gcLeakedPod(tt.args.ctx, tt.args.wepList)
			if (err != nil) != tt.wantErr {
				t.Errorf("gcLeakedPod() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_gcDeletedNode(t *testing.T) {
	type fields struct {
		ctrl             *gomock.Controller
		lock             sync.RWMutex
		nodeLock         keymutex.KeyMutex
		datastore        *datastore.DataStore
		allocated        map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		bucket           *ratelimit.Bucket
		batchAddIPNum    int
		gcPeriod         time.Duration
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "normal",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)
				kubeClient.CoreV1().Nodes().Create(&v1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://test-node-id",
					},
				})
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl: ctrl,
					lock: sync.RWMutex{},
					datastore: func() *datastore.DataStore {
						dt := datastore.NewDataStore()
						err := dt.AddNodeToStore("test-node", "test-node-id")
						if err != nil {
							return nil
						}
						err = dt.AddNodeToStore("test-node-not-exist", "test-node-not-exist-id")
						if err != nil {
							return nil
						}
						return dt
					}(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated:        map[string]*v1alpha1.WorkloadEndpoint{},
					bucket:           ratelimit.NewBucket(100, 100),
				}
			}(),
			args:    args{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				eventBroadcaster: tt.fields.eventBroadcaster,
				eventRecorder:    tt.fields.eventRecorder,
				kubeInformer:     tt.fields.kubeInformer,
				kubeClient:       tt.fields.kubeClient,
				crdInformer:      tt.fields.crdInformer,
				crdClient:        tt.fields.crdClient,
				cloud:            tt.fields.cloud,
				lock:             tt.fields.lock,
				cniMode:          tt.fields.cniMode,
				vpcID:            tt.fields.vpcID,
				clusterID:        tt.fields.clusterID,
				datastore:        tt.fields.datastore,
				allocated:        tt.fields.allocated,
				bucket:           tt.fields.bucket,
				batchAddIPNum:    tt.fields.batchAddIPNum,
				cacheHasSynced:   tt.fields.cacheHasSynced,
				gcPeriod:         tt.fields.gcPeriod,
				clock:            clock.NewFakeClock(time.Unix(0, 0)),
			}
			err := ipam.gcDeletedNode(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("gcDeletedNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_poolTooLow(t *testing.T) {
	ipam := IPAM{}
	type args struct {
		total int
		used  int
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Normal More",
			args: args{
				total: 5,
				used:  2,
			},
			want: false,
		},
		{
			name: "Normal Equal",
			args: args{
				total: 5,
				used:  5,
			},
			want: true,
		},
		{
			name: "Normal Less - Corrupted",
			args: args{
				total: 5,
				used:  7,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ipam.poolTooLow(tt.args.total, tt.args.used); got != tt.want {
				t.Errorf("ipam.poolTooLow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_poolCorrupted(t *testing.T) {
	ipam := IPAM{}
	type args struct {
		total int
		used  int
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Normal More",
			args: args{
				total: 5,
				used:  2,
			},
			want: false,
		},
		{
			name: "Normal Equal",
			args: args{
				total: 5,
				used:  5,
			},
			want: false,
		},
		{
			name: "Corrupted Less",
			args: args{
				total: 5,
				used:  7,
			},
			want: true,
		},
		{
			name: "Corrupted Negative total",
			args: args{
				total: -1,
				used:  7,
			},
			want: true,
		},
		{
			name: "Corrupted Negative used",
			args: args{
				total: 5,
				used:  -1,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ipam.poolCorrupted(tt.args.total, tt.args.used); got != tt.want {
				t.Errorf("ipam.poolCorrupted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAM_increasePool(t *testing.T) {
	type fields struct {
		ctrl             *gomock.Controller
		lock             sync.RWMutex
		nodeLock         keymutex.KeyMutex
		datastore        *datastore.DataStore
		allocated        map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		bucket           *ratelimit.Bucket
		batchAddIPNum    int
		gcPeriod         time.Duration
	}
	type args struct {
		ctx        context.Context
		node       string
		instanceID string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "normal",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				gomock.InOrder(
					cloudClient.EXPECT().BBCBatchAddIP(gomock.Any(), gomock.Any()).Return(&bbc.BatchAddIpResponse{
						PrivateIps: []string{"10.0.0.1", "10.0.0.2"},
					}, nil),
				)
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:             ctrl,
					batchAddIPNum:    5,
					lock:             sync.RWMutex{},
					datastore:        datastore.NewDataStore(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated: map[string]*v1alpha1.WorkloadEndpoint{
						"10.0.0.1": {},
					},
					bucket: ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				node:       "test-node",
				instanceID: "test-node-id",
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
				batchAddIPNum:  tt.fields.batchAddIPNum,
				cloud:          tt.fields.cloud,
				crdInformer:    tt.fields.crdInformer,
				allocated:      tt.fields.allocated,
				cacheHasSynced: tt.fields.cacheHasSynced,
				clock:          clock.NewFakeClock(time.Unix(0, 0)),
				datastore: func() *datastore.DataStore {
					dt := datastore.NewDataStore()
					dt.AddNodeToStore("test-node", "test-node-id")
					dt.AddENIToStore("test-node", "test-node-id")
					return dt
				}(),
				bucket: tt.fields.bucket,
			}
			err := ipam.increasePool(tt.args.ctx, tt.args.node, tt.args.instanceID)
			if (err != nil) != tt.wantErr {
				t.Errorf("rebuildNodeDataStoreCache() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_rebuildNodeDataStoreCache(t *testing.T) {
	type fields struct {
		ctrl             *gomock.Controller
		lock             sync.RWMutex
		nodeLock         keymutex.KeyMutex
		datastore        *datastore.DataStore
		allocated        map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced   bool
		eventBroadcaster record.EventBroadcaster
		eventRecorder    record.EventRecorder
		kubeInformer     informers.SharedInformerFactory
		kubeClient       kubernetes.Interface
		crdInformer      crdinformers.SharedInformerFactory
		crdClient        versioned.Interface
		cloud            cloud.Interface
		clock            clock.Clock
		cniMode          types.ContainerNetworkMode
		vpcID            string
		clusterID        string
		bucket           *ratelimit.Bucket
		batchAddIPNum    int
		gcPeriod         time.Duration
	}
	type args struct {
		ctx        context.Context
		node       *v1.Node
		instanceID string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "normal",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				kubeClient, kubeInformer, crdClient, crdInformer, cloudClient, brdcaster, recorder := setupEnv(ctrl)

				gomock.InOrder(
					cloudClient.EXPECT().BBCGetInstanceENI(gomock.Any(), gomock.Any()).Return(&bbc.GetInstanceEniResult{
						PrivateIpSet: []bbc.PrivateIP{
							{PrivateIpAddress: "10.0.0.1"},
							{PrivateIpAddress: "10.0.0.2"},
						},
					}, nil),
				)
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:             ctrl,
					lock:             sync.RWMutex{},
					datastore:        datastore.NewDataStore(),
					cacheHasSynced:   true,
					eventBroadcaster: brdcaster,
					eventRecorder:    recorder,
					kubeInformer:     kubeInformer,
					kubeClient:       kubeClient,
					crdInformer:      crdInformer,
					crdClient:        crdClient,
					cloud:            cloudClient,
					allocated: map[string]*v1alpha1.WorkloadEndpoint{
						"10.0.0.1": {},
					},
					bucket: ratelimit.NewBucket(100, 100),
				}
			}(),
			args: args{
				node: &v1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Spec: v1.NodeSpec{
						ProviderID: "cce://test-node-id",
					},
				},
				instanceID: "test-node-id",
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
				cloud:          tt.fields.cloud,
				crdInformer:    tt.fields.crdInformer,
				allocated:      tt.fields.allocated,
				cacheHasSynced: tt.fields.cacheHasSynced,
				clock:          clock.NewFakeClock(time.Unix(0, 0)),
				datastore:      datastore.NewDataStore(),
				bucket:         tt.fields.bucket,
			}
			err := ipam.rebuildNodeDataStoreCache(tt.args.ctx, tt.args.node, tt.args.instanceID)
			if (err != nil) != tt.wantErr {
				t.Errorf("rebuildNodeDataStoreCache() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestIPAM_buildAllocatedCache(t *testing.T) {
	type fields struct {
		ctrl           *gomock.Controller
		allocated      map[string]*v1alpha1.WorkloadEndpoint
		cacheHasSynced bool
		crdInformer    crdinformers.SharedInformerFactory
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "normal",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, kubeInformer, crdClient, crdInformer, _, _, _ := setupEnv(ctrl)
				crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(&v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox",
						Namespace: "default",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						IP:       "10.1.1.2",
						ENIID:    "test-node-id",
						Node:     "test-node",
						UpdateAt: metav1.Time{time.Unix(0, 0)},
					},
				})
				crdClient.CceV1alpha1().WorkloadEndpoints("default").Create(&v1alpha1.WorkloadEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "busybox2",
						Namespace: "default",
					},
					Spec: v1alpha1.WorkloadEndpointSpec{
						IP:       "10.1.1.3",
						ENIID:    "test-node-id",
						Node:     "test-node",
						UpdateAt: metav1.Time{time.Unix(0, 0)},
					},
				})

				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:           ctrl,
					cacheHasSynced: true,
					crdInformer:    crdInformer,
					allocated:      map[string]*v1alpha1.WorkloadEndpoint{},
				}
			}(),
			wantErr: false,
		},
		{
			name: "empty wep",
			fields: func() fields {
				ctrl := gomock.NewController(t)
				_, kubeInformer, _, crdInformer, _, _, _ := setupEnv(ctrl)
				startInformers(kubeInformer, crdInformer)
				return fields{
					ctrl:           ctrl,
					cacheHasSynced: true,
					crdInformer:    crdInformer,
					allocated:      map[string]*v1alpha1.WorkloadEndpoint{},
				}
			}(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}
			ipam := &IPAM{
				crdInformer:    tt.fields.crdInformer,
				allocated:      tt.fields.allocated,
				cacheHasSynced: tt.fields.cacheHasSynced,
				clock:          clock.NewFakeClock(time.Unix(0, 0)),
			}
			err := ipam.buildAllocatedCache(context.TODO())
			if (err != nil) != tt.wantErr {
				t.Errorf("buildAllocatedCache() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
