package endpoint

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	cceclientv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/typed/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
)

type fakeCCEEndpointInterface struct {
	updateFn func(*ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error)
	deleteFn func(string) error
	getFn    func(string) (*ccev2.CCEEndpoint, error)
}

func (f *fakeCCEEndpointInterface) Create(ctx context.Context, ep *ccev2.CCEEndpoint, opts metav1.CreateOptions) (*ccev2.CCEEndpoint, error) {
	return ep, nil
}

func (f *fakeCCEEndpointInterface) Update(ctx context.Context, ep *ccev2.CCEEndpoint, opts metav1.UpdateOptions) (*ccev2.CCEEndpoint, error) {
	if f.updateFn != nil {
		return f.updateFn(ep)
	}
	return ep, nil
}

func (f *fakeCCEEndpointInterface) UpdateStatus(ctx context.Context, ep *ccev2.CCEEndpoint, opts metav1.UpdateOptions) (*ccev2.CCEEndpoint, error) {
	return ep, nil
}

func (f *fakeCCEEndpointInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	if f.deleteFn != nil {
		return f.deleteFn(name)
	}
	return nil
}

func (f *fakeCCEEndpointInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (f *fakeCCEEndpointInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*ccev2.CCEEndpoint, error) {
	if f.getFn != nil {
		return f.getFn(name)
	}
	return nil, k8serrors.NewNotFound(schema.GroupResource{Group: "cce", Resource: "cceendpoint"}, name)
}

func (f *fakeCCEEndpointInterface) List(ctx context.Context, opts metav1.ListOptions) (*ccev2.CCEEndpointList, error) {
	return &ccev2.CCEEndpointList{}, nil
}

func (f *fakeCCEEndpointInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *fakeCCEEndpointInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*ccev2.CCEEndpoint, error) {
	return nil, nil
}

var _ cceclientv2.CCEEndpointInterface = &fakeCCEEndpointInterface{}

func TestCreateDelegateEndpoint_NilPSTS(t *testing.T) {
	e := &EndpointAllocator{}
	newEP := &ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{},
			},
		},
	}
	ep, err := e.createDelegateEndpoint(context.Background(), nil, newEP, nil)
	require.Error(t, err)
	require.Nil(t, ep)
}

func TestCreateDelegateEndpoint_FixedReuseMismatch(t *testing.T) {
	e := &EndpointAllocator{}
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		},
	}
	newEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{},
			},
		},
	}
	oldEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "other"},
			},
		},
	}
	_, err := e.createDelegateEndpoint(context.Background(), psts, newEP, oldEP)
	require.Error(t, err)
}

func TestCreateDelegateEndpoint_FixedReuseSetsTTL(t *testing.T) {
	e := &EndpointAllocator{}
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{
				Type: ccev2.IPAllocTypeFixed,
				TTL:  &metav1.Duration{Duration: 30 * time.Second},
			},
		},
	}
	newEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{},
			},
		},
	}
	oldEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"},
			},
		},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "createReuseIPEndpoint", func(_ *EndpointAllocator, _ context.Context, newEP, _ *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
		return newEP, nil
	})

	ep, err := e.createDelegateEndpoint(context.Background(), psts, newEP, oldEP)
	require.NoError(t, err)
	require.NotNil(t, ep)
	require.Equal(t, "p1", ep.Spec.Network.IPAllocation.PSTSName)
	require.Equal(t, ccev2.IPAllocTypeFixed, ep.Spec.Network.IPAllocation.Type)
	require.NotNil(t, ep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted)
	require.EqualValues(t, 30, *ep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted)
}

func TestCreateDelegateEndpoint_ElasticDeleteOldError(t *testing.T) {
	e := &EndpointAllocator{}
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		},
	}
	newEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{},
			},
		},
	}
	oldEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "ns"},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(DeleteEndpointAndWait, func(_ context.Context, _ *watchers.CCEEndpointClient, _ *ccev2.CCEEndpoint) error {
		return errors.New("delete error")
	})

	_, err := e.createDelegateEndpoint(context.Background(), psts, newEP, oldEP)
	require.Error(t, err)
}

func TestCreateDelegateEndpoint_ElasticRecreateSuccess(t *testing.T) {
	e := &EndpointAllocator{}
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		},
	}
	newEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{},
			},
		},
	}
	oldEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "ns"},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(DeleteEndpointAndWait, func(_ context.Context, _ *watchers.CCEEndpointClient, _ *ccev2.CCEEndpoint) error {
		return nil
	})
	patches.ApplyFunc(recreateCEP, func(_ context.Context, _ *watchers.CCEEndpointClient, ep *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
		return ep, nil
	})

	ep, err := e.createDelegateEndpoint(context.Background(), psts, newEP, oldEP)
	require.NoError(t, err)
	require.NotNil(t, ep)
	require.Equal(t, ccev2.IPAllocTypeElastic, ep.Spec.Network.IPAllocation.Type)
	require.Nil(t, ep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted)
}

func TestTryDeleteEndpointAfterPodDeleted_PSTSNotFound(t *testing.T) {
	e := &EndpointAllocator{
		pstsLister: &fakePSTSLister{psts: nil},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{K8sPodName: "pod"},
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"},
			},
		},
	}

	called := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "deleteEndpointSafely", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint) error {
		called = true
		return nil
	})

	err := e.tryDeleteEndpointAfterPodDeleted(ep, true, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.True(t, called)
}

func TestTryDeleteEndpointAfterPodDeleted_PSTSElastic(t *testing.T) {
	e := &EndpointAllocator{
		pstsLister: &fakePSTSLister{
			psts: &ccev2.PodSubnetTopologySpread{
				ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
				Spec: ccev2.PodSubnetTopologySpreadSpec{
					Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
				},
			},
		},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{K8sPodName: "pod"},
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"},
			},
		},
	}

	deleted := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "deleteEndpointSafely", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint) error {
		deleted = true
		return nil
	})

	err := e.tryDeleteEndpointAfterPodDeleted(ep, true, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.True(t, deleted)
}

func TestTryDeleteEndpointAfterPodDeleted_PSTSFixedTTL(t *testing.T) {
	e := &EndpointAllocator{
		pstsLister: &fakePSTSLister{
			psts: &ccev2.PodSubnetTopologySpread{
				ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
				Spec: ccev2.PodSubnetTopologySpreadSpec{
					Strategy: &ccev2.IPAllocationStrategy{
						Type: ccev2.IPAllocTypeFixed,
						TTL:  &metav1.Duration{Duration: 10 * time.Second},
					},
				},
			},
		},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{K8sPodName: "pod"},
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"},
			},
		},
	}

	marked := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "markEndpointAsPodDeleted", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint, _ string) (bool, error) {
		marked = true
		return true, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "deleteEndpointSafely", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint) error {
		return errors.New("should not delete")
	})

	err := e.tryDeleteEndpointAfterPodDeleted(ep, true, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.True(t, marked)
}

func TestTryDeleteEndpointAfterPodDeleted_PSTSFixedTTLSkipsStaleContainer(t *testing.T) {
	e := &EndpointAllocator{
		pstsLister: &fakePSTSLister{
			psts: &ccev2.PodSubnetTopologySpread{
				ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
				Spec: ccev2.PodSubnetTopologySpreadSpec{
					Strategy: &ccev2.IPAllocationStrategy{
						Type: ccev2.IPAllocTypeFixed,
						TTL:  &metav1.Duration{Duration: 10 * time.Second},
					},
				},
			},
		},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sPodName:  "pod",
				ContainerID: "container-old",
			},
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"},
			},
		},
	}

	deleted := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "markEndpointAsPodDeleted", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint, expectedContainerID string) (bool, error) {
		require.Equal(t, "container-old", expectedContainerID)
		return false, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "deleteEndpointSafely", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint) error {
		deleted = true
		return nil
	})

	err := e.tryDeleteEndpointAfterPodDeleted(ep, true, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.False(t, deleted)
}

func TestTryDeleteEndpointAfterPodDeleted_NonPSTSReleaseByOwner(t *testing.T) {
	mockIPAM := &MockIPAMAllocator{}
	mockIPAM.On("ReleaseIPWithOwner", "10.0.0.1", "ns/pod").Return(nil)
	mockIPAM.On("ReleaseIPWithOwner", "10.0.0.2", "ns/pod").Return(nil)

	e := &EndpointAllocator{
		dynamicIPAM:       mockIPAM,
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pod",
			Namespace:  "ns",
			Finalizers: []string{"finalizer"},
		},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{K8sPodName: "pod"},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1"},
					&ccev2.AddressPair{IP: "10.0.0.2"},
				},
			},
		},
	}

	updateCalled := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "CCEEndpoints", func(_ *watchers.CCEEndpointClient, _ string) cceclientv2.CCEEndpointInterface {
		return &fakeCCEEndpointInterface{
			updateFn: func(updated *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
				updateCalled = true
				require.Empty(t, updated.Finalizers)
				return updated, nil
			},
		}
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(e), "deleteEndpointSafely", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint) error {
		return nil
	})

	err := e.tryDeleteEndpointAfterPodDeleted(ep, true, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.True(t, updateCalled)
	mockIPAM.AssertExpectations(t)
}

func TestWaitEndpointIPAllocated_InvalidStatus(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateInvalid),
			Log: []*models.EndpointStatusChange{
				{Code: models.EndpointStatusChangeCodeFailed, Message: "boom"},
			},
		},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "Get", func(_ *watchers.CCEEndpointClient, _ string, _ string) (*ccev2.CCEEndpoint, error) {
		return ep, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := e.waitEndpointIPAllocated(ctx, ep)
	require.Error(t, err)
}

func TestWaitEndpointIPAllocated_InvalidStatusUsesDefaultMessage(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateInvalid),
			Log: []*models.EndpointStatusChange{
				{Code: models.EndpointStatusChangeCodeFailed},
			},
		},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "Get", func(_ *watchers.CCEEndpointClient, _ string, _ string) (*ccev2.CCEEndpoint, error) {
		return ep, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := e.waitEndpointIPAllocated(ctx, ep)
	require.Error(t, err)
	require.Contains(t, err.Error(), "operator failed to allocate IP")
}

func TestWaitEndpointIPAllocated_GetErrorTimesOut(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "Get", func(_ *watchers.CCEEndpointClient, _ string, _ string) (*ccev2.CCEEndpoint, error) {
		return nil, errors.New("temporary get error")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	_, _, err := e.waitEndpointIPAllocated(ctx, &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out waiting for the condition")
}

func TestEndpointStatusMatchesSpec_NilInput(t *testing.T) {
	require.False(t, endpointStatusMatchesSpec(nil, nil))
	require.False(t, endpointStatusMatchesSpec(&ccev2.CCEEndpoint{}, nil))
}

func TestEndpointStatusMatchesSpec_Mismatch(t *testing.T) {
	ep := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateIPAllocated),
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-old",
				Netns:       "/var/run/netns/old",
			},
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.2", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	expected := &ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-new",
				Netns:       "/var/run/netns/new",
			},
		},
	}

	require.False(t, endpointStatusMatchesSpec(ep, expected))
}

func TestEndpointStatusMatchesSpec_StateMismatch(t *testing.T) {
	ep := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStatePodDeleted),
		},
	}
	expected := &ccev2.CCEEndpoint{}

	require.False(t, endpointStatusMatchesSpec(ep, expected))
}

func TestEndpointStatusMatchesSpec_MissingNetworking(t *testing.T) {
	ep := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateIPAllocated),
		},
	}
	expected := &ccev2.CCEEndpoint{}

	require.False(t, endpointStatusMatchesSpec(ep, expected))
}

func TestEndpointStatusMatchesSpec_MissingExternalIdentifiers(t *testing.T) {
	ep := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateIPAllocated),
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.2", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	expected := &ccev2.CCEEndpoint{}

	require.False(t, endpointStatusMatchesSpec(ep, expected))
}

func TestEndpointStatusMatchesSpec_Success(t *testing.T) {
	ep := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateIPAllocated),
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-new",
				Netns:       "/var/run/netns/new",
			},
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.2", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	expected := &ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-new",
				Netns:       "/var/run/netns/new",
			},
		},
	}

	require.True(t, endpointStatusMatchesSpec(ep, expected))
}

func TestWaitEndpointIPAllocated_RequiresMatchingContainerAndNetns(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	expected := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-new",
				Netns:       "/var/run/netns/new",
			},
		},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateIPAllocated),
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-old",
				Netns:       "/var/run/netns/old",
			},
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.2", Family: ccev2.IPv4Family},
				},
			},
		},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "Get", func(_ *watchers.CCEEndpointClient, _ string, _ string) (*ccev2.CCEEndpoint, error) {
		return ep, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	_, _, err := e.waitEndpointIPAllocated(ctx, expected)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out waiting for the condition")
}

func TestWaitEndpointIPAllocated_SuccessWithMatchingContainerAndNetns(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	expected := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-new",
				Netns:       "/var/run/netns/new",
			},
		},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateIPAllocated),
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "pod-uid",
				ContainerID: "container-new",
				Netns:       "/var/run/netns/new",
			},
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{
						IP:        "10.0.0.2",
						Family:    ccev2.IPv4Family,
						Interface: "eni-test",
						Gateway:   "10.0.0.1",
						CIDRs:     []string{"10.0.0.0/24"},
					},
				},
			},
		},
	}

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "Get", func(_ *watchers.CCEEndpointClient, _ string, _ string) (*ccev2.CCEEndpoint, error) {
		return ep, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ipv4, ipv6, err := e.waitEndpointIPAllocated(ctx, expected)
	require.NoError(t, err)
	require.NotNil(t, ipv4)
	require.Nil(t, ipv6)
	require.Equal(t, "10.0.0.2", ipv4.IP.String())
	require.Equal(t, "10.0.0.1", ipv4.GatewayIP)
	require.Equal(t, "eni-test", ipv4.InterfaceNumber)
	require.Equal(t, []string{"10.0.0.0/24"}, ipv4.CIDRs)
}

func TestMarkEndpointAsPodDeleted_Success(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
		},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				ContainerID: "container-old",
			},
		},
	}

	updateCalled := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "CCEEndpoints", func(_ *watchers.CCEEndpointClient, namespace string) cceclientv2.CCEEndpointInterface {
		require.Equal(t, "ns", namespace)
		return &fakeCCEEndpointInterface{
			getFn: func(name string) (*ccev2.CCEEndpoint, error) {
				require.Equal(t, "cep", name)
				return ep, nil
			},
			updateFn: func(updated *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
				updateCalled = true
				require.Equal(t, string(models.EndpointStatePodDeleted), updated.Status.State)
				require.NotEmpty(t, updated.Status.Log)
				last := updated.Status.Log[len(updated.Status.Log)-1]
				require.Equal(t, models.EndpointStatePodDeleted, last.State)
				require.Equal(t, models.EndpointStatusChangeCodeOk, last.Code)
				require.Equal(t, "Pod deleted, IP will be released by Operator", last.Message)
				return updated, nil
			},
		}
	})

	marked, err := e.markEndpointAsPodDeleted(ep, "container-old")
	require.NoError(t, err)
	require.True(t, marked)
	require.True(t, updateCalled)
}

func TestMarkEndpointAsPodDeleted_UpdateError(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
		},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				ContainerID: "container-old",
			},
		},
	}

	updateErr := errors.New("update failed")
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "CCEEndpoints", func(_ *watchers.CCEEndpointClient, _ string) cceclientv2.CCEEndpointInterface {
		return &fakeCCEEndpointInterface{
			getFn: func(name string) (*ccev2.CCEEndpoint, error) {
				return ep, nil
			},
			updateFn: func(updated *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
				require.Equal(t, string(models.EndpointStatePodDeleted), updated.Status.State)
				return nil, updateErr
			},
		}
	})

	marked, err := e.markEndpointAsPodDeleted(ep, "container-old")
	require.Error(t, err)
	require.False(t, marked)
	require.ErrorIs(t, err, updateErr)
	require.Contains(t, err.Error(), "failed to update CEP status to PodDeleted")
}

func TestMarkEndpointAsPodDeleted_SkipsStaleContainer(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
		},
	}

	updateCalled := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "CCEEndpoints", func(_ *watchers.CCEEndpointClient, _ string) cceclientv2.CCEEndpointInterface {
		return &fakeCCEEndpointInterface{
			getFn: func(name string) (*ccev2.CCEEndpoint, error) {
				return &ccev2.CCEEndpoint{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
					Spec: ccev2.EndpointSpec{
						ExternalIdentifiers: &models.EndpointIdentifiers{
							ContainerID: "container-new",
						},
					},
				}, nil
			},
			updateFn: func(updated *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
				updateCalled = true
				return updated, nil
			},
		}
	})

	marked, err := e.markEndpointAsPodDeleted(ep, "container-old")
	require.NoError(t, err)
	require.False(t, marked)
	require.False(t, updateCalled)
}

func TestMarkEndpointAsPodDeleted_SkipsWhenLatestHasNoExternalIdentifiers(t *testing.T) {
	e := &EndpointAllocator{
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
		},
	}

	updateCalled := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(e.cceEndpointClient), "CCEEndpoints", func(_ *watchers.CCEEndpointClient, _ string) cceclientv2.CCEEndpointInterface {
		return &fakeCCEEndpointInterface{
			getFn: func(name string) (*ccev2.CCEEndpoint, error) {
				return &ccev2.CCEEndpoint{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
				}, nil
			},
			updateFn: func(updated *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
				updateCalled = true
				return updated, nil
			},
		}
	})

	marked, err := e.markEndpointAsPodDeleted(ep, "container-old")
	require.NoError(t, err)
	require.False(t, marked)
	require.False(t, updateCalled)
}
