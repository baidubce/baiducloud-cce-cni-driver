package rdma

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/rdma/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/scheme"
	"github.com/baidubce/bce-sdk-go/services/eni"
)

func Test__gcLeakedPod(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	kubeClient, kubeInformer, crdClient, crdInformer, bceClient := setupEnv(ctrl)

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "cce-ipam"})

	ipam := &IPAM{
		lock:           sync.RWMutex{},
		vpcID:          "",
		cacheHasSynced: true,
		eventRecorder:  recorder,
		gcPeriod:       time.Second,
		kubeInformer:   kubeInformer,
		kubeClient:     kubeClient,
		crdInformer:    crdInformer,
		crdClient:      crdClient,
		eriClient:      client.NewEriClient(bceClient),
		roceClient:     client.NewRoCEClient(bceClient),
	}

	// pod-0 exist
	// pod-1 not found
	mwep0 := &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pod-0",
			Namespace:  corev1.NamespaceDefault,
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{{
			IP:    "10.1.1.0",
			EniID: "eni-0",
		}},
	}
	_, err0 := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Create(ctx, mwep0, metav1.CreateOptions{})
	assert.NilError(t, err0)

	mwep1 := &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pod-1",
			Namespace:  corev1.NamespaceDefault,
			Finalizers: []string{"cce-cni.cce.io"},
		},
		Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{{
			IP:    "10.1.1.1",
			EniID: "eni-0",
		}},
	}
	_, err1 := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Create(ctx, mwep1, metav1.CreateOptions{})
	assert.NilError(t, err1)

	_, podErr := kubeClient.CoreV1().Pods(corev1.NamespaceDefault).
		Create(ctx, &corev1.Pod{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-0",
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
			},
		}, metav1.CreateOptions{})
	assert.NilError(t, podErr)
	waitForCacheSync(kubeInformer, crdInformer)

	bceClient.EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil)

	gcErr := ipam.gcLeakedPod(ctx)
	assert.NilError(t, gcErr)

	// should not be deleted
	_, getErr0 := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Get(ctx, "pod-0", metav1.GetOptions{})
	assert.NilError(t, getErr0)
	// should be deleted
	_, getErr1 := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Get(ctx, "pod-1", metav1.GetOptions{})
	assert.Assert(t, errors.IsNotFound(getErr1))
}

func Test__gcLeakedIP(t *testing.T) {
	// init
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	kubeClient, kubeInformer, crdClient, crdInformer, bceClient := setupEnv(ctrl)

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "cce-ipam"})

	ipam := &IPAM{
		lock:           sync.RWMutex{},
		vpcID:          "",
		cacheHasSynced: true,
		eventRecorder:  recorder,
		gcPeriod:       time.Second,
		kubeInformer:   kubeInformer,
		kubeClient:     kubeClient,
		crdInformer:    crdInformer,
		crdClient:      crdClient,
		eriClient:      client.NewEriClient(bceClient),
		roceClient:     client.NewRoCEClient(bceClient),
	}

	// create rdma node
	_, nodeErr0 := kubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-rdma",
			Labels: map[string]string{
				ipamgeneric.RDMANodeLabelAvailableKey: "true",
				ipamgeneric.RDMANodeLabelCapableKey:   "true",
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: "cce://i-rdma",
		},
	}, metav1.CreateOptions{})
	assert.NilError(t, nodeErr0)

	// create not rdma node, gc will ignore this node
	_, nodeErr1 := kubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-normal",
			Labels: map[string]string{},
		},
		Spec: corev1.NodeSpec{
			ProviderID: "cce://i-normal",
		},
	}, metav1.CreateOptions{})
	assert.NilError(t, nodeErr1)

	// exist eri ip 1.1.1.1, roce ip 2.2.2.2
	// delete eri ip 3.3.3.3, roce ip 4.4.4.4
	mwep0 := &networkingv1alpha1.MultiIPWorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pod-0",
			Namespace:  corev1.NamespaceDefault,
			Finalizers: []string{"cce-cni.cce.io"},
		},
		InstanceID: "i-rdma",
		Spec: []networkingv1alpha1.MultiIPWorkloadEndpointSpec{
			{
				Type:  ipamgeneric.MwepTypeERI,
				IP:    "1.1.1.1",
				EniID: "eri-1",
			},
			{
				Type:  ipamgeneric.MwepTypeRoce,
				IP:    "2.2.2.2",
				EniID: "roce-2",
			},
		},
	}
	_, err0 := crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(corev1.NamespaceDefault).
		Create(ctx, mwep0, metav1.CreateOptions{})
	assert.NilError(t, err0)

	waitForCacheSync(kubeInformer, crdInformer)

	// mock bceClient
	// eri ip 1.1.1.1, 3.3.3.3
	bceClient.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]eni.Eni{
		{
			EniId:      "eri-1",
			MacAddress: "mac-eri-1",
			PrivateIpSet: []eni.PrivateIp{
				{
					Primary:          true,
					PrivateIpAddress: "9.9.9.9", // gc will ignore this ip, because it's primary ip
				},
				{
					Primary:          false,
					PrivateIpAddress: "1.1.1.1",
				},
				{
					Primary:          false,
					PrivateIpAddress: "3.3.3.3", // leaked ip
				},
			},
		},
	}, nil)
	bceClient.EXPECT().DeletePrivateIP(gomock.Any(), gomock.Eq("3.3.3.3"), gomock.Eq("eri-1")).Return(nil)

	// roce ip 2.2.2.2, 4.4.4.4
	bceClient.EXPECT().GetHPCEniID(gomock.Any(), gomock.Eq("i-rdma")).Return(&hpc.EniList{
		Result: []hpc.Result{
			{
				EniID:      "roce-1",
				MacAddress: "mac-roce-1",
				PrivateIPSet: []hpc.PrivateIP{
					{
						Primary:          true,
						PrivateIPAddress: "8.8.8.8", // ignore primary ip
					}, {
						Primary:          false,
						PrivateIPAddress: "2.2.2.2",
					}, {
						Primary:          false,
						PrivateIPAddress: "4.4.4.4", // leaked ip
					},
				},
			},
		},
	}, nil)
	bceClient.EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil)

	gcErr := ipam.gcLeakedIP(ctx)
	assert.NilError(t, gcErr)
}
