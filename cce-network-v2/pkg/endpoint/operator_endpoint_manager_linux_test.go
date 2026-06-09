//go:build linux
// +build linux

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
	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccelister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
)

type fakeEndpointGetterUpdater struct {
	updated         *ccev2.CCEEndpoint
	updateErr       error
	updateStatus    *ccev2.CCEEndpoint
	updateStatusErr error
	lister          ccelister.CCEEndpointLister
}

func (f *fakeEndpointGetterUpdater) Create(node *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	return node, nil
}

func (f *fakeEndpointGetterUpdater) Update(newResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	f.updated = newResource
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return newResource, nil
}

func (f *fakeEndpointGetterUpdater) UpdateStatus(newResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	f.updateStatus = newResource
	if f.updateStatusErr != nil {
		return nil, f.updateStatusErr
	}
	return newResource, nil
}

func (f *fakeEndpointGetterUpdater) Delete(namespace, name string) error {
	return nil
}

func (f *fakeEndpointGetterUpdater) Lister() ccelister.CCEEndpointLister {
	return f.lister
}

type fakeEndpointLister struct {
	endpoints map[string]*ccev2.CCEEndpoint
	listErr   error
}

func (f *fakeEndpointLister) List(selector labels.Selector) ([]*ccev2.CCEEndpoint, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var items []*ccev2.CCEEndpoint
	for _, ep := range f.endpoints {
		items = append(items, ep)
	}
	return items, nil
}

func (f *fakeEndpointLister) CCEEndpoints(namespace string) ccelister.CCEEndpointNamespaceLister {
	return &fakeEndpointNamespaceLister{namespace: namespace, endpoints: f.endpoints}
}

type fakeEndpointNamespaceLister struct {
	namespace string
	endpoints map[string]*ccev2.CCEEndpoint
}

func (f *fakeEndpointNamespaceLister) List(selector labels.Selector) ([]*ccev2.CCEEndpoint, error) {
	var items []*ccev2.CCEEndpoint
	for key, ep := range f.endpoints {
		if ep != nil && key == f.namespace+"/"+ep.Name {
			items = append(items, ep)
		}
	}
	return items, nil
}

func (f *fakeEndpointNamespaceLister) Get(name string) (*ccev2.CCEEndpoint, error) {
	key := f.namespace + "/" + name
	ep, ok := f.endpoints[key]
	if !ok || ep == nil {
		return nil, k8serrors.NewNotFound(ccev2.Resource("cceendpoint"), name)
	}
	return ep, nil
}

type errorEndpointLister struct {
	err error
}

func (e *errorEndpointLister) List(selector labels.Selector) ([]*ccev2.CCEEndpoint, error) {
	return nil, e.err
}

func (e *errorEndpointLister) CCEEndpoints(namespace string) ccelister.CCEEndpointNamespaceLister {
	return &errorEndpointNamespaceLister{err: e.err}
}

type errorEndpointNamespaceLister struct {
	err error
}

func (e *errorEndpointNamespaceLister) List(selector labels.Selector) ([]*ccev2.CCEEndpoint, error) {
	return nil, e.err
}

func (e *errorEndpointNamespaceLister) Get(name string) (*ccev2.CCEEndpoint, error) {
	return nil, e.err
}

type fakeNRSLister struct {
	getErr error
}

func (f *fakeNRSLister) List(selector labels.Selector) ([]*ccev2.NetResourceSet, error) {
	return nil, nil
}

func (f *fakeNRSLister) Get(name string) (*ccev2.NetResourceSet, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &ccev2.NetResourceSet{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

func TestCompareResourceVersions(t *testing.T) {
	ok, err := compareResourceVersions("2", "1")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = compareResourceVersions("1", "1")
	require.NoError(t, err)
	require.True(t, ok)

	_, err = compareResourceVersions("bad", "1")
	require.Error(t, err)
}

func TestSyncedPoolHelpers(t *testing.T) {
	manager := &EndpointManager{}
	manager.addToSyncedPool("k", "1")
	require.True(t, manager.isInSyncedPool("k"))
	require.Equal(t, "1", manager.getSyncedPoolRevision("k"))
	manager.removeFromSyncedPool("k")
	require.False(t, manager.isInSyncedPool("k"))
	require.Equal(t, "", manager.getSyncedPoolRevision("k"))
}

func TestHandlePodDeletedIPReleaseNoAddressing(t *testing.T) {
	manager := &EndpointManager{}
	cep := &ccev2.CCEEndpoint{}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
}

func TestHandlePodDeletedIPReleaseReserveError(t *testing.T) {
	manager := &EndpointManager{
		directIPAllocator: nil,
	}
	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestHandlePodDeletedIPReleaseOperationError(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn": {{
					Family: ccev2.IPv4Family,
					Range:  []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.1"}},
				}},
			},
		},
	}
	manager := &EndpointManager{
		pstsLister:        &fakePSTSLister{psts: psts},
		directIPAllocator: &fakeDirectIPAllocator{op: nil, err: errors.New("node error")},
	}
	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestHandlePodDeletedIPReleaseNodeEndpointNotFound(t *testing.T) {
	manager := &EndpointManager{
		directIPAllocator: &fakeDirectIPAllocator{
			op:  nil,
			err: k8serrors.NewNotFound(ccev2.Resource("netresourceset"), "node-1"),
		},
	}
	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{
					PSTSName: "psts",
					NodeName: "node-1",
				},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
}

func TestHandlePodDeletedIPReleaseOperationNil(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn": {{
					Family: ccev2.IPv4Family,
					Range:  []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.1"}},
				}},
			},
		},
	}
	manager := &EndpointManager{
		pstsLister:        &fakePSTSLister{psts: psts},
		directIPAllocator: &fakeDirectIPAllocator{op: nil, err: nil},
	}
	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestHandlePodDeletedIPReleaseDeleteIPNotFound(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn": {{
					Family: ccev2.IPv4Family,
					Range:  []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.1"}},
				}},
			},
		},
	}
	op := &fakeDirectEndpointOperation{
		deleteFn: func(ctx context.Context, action *DirectIPAction) error {
			return k8serrors.NewNotFound(ccev2.Resource("eni"), "eni")
		},
	}
	manager := &EndpointManager{
		pstsLister:        &fakePSTSLister{psts: psts},
		directIPAllocator: &fakeDirectIPAllocator{op: op},
	}
	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
}

func TestHandlePodDeletedIPReleaseDeleteIPAlreadyDeleted(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn": {{
					Family: ccev2.IPv4Family,
					Range:  []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.1"}},
				}},
			},
		},
	}
	op := &fakeDirectEndpointOperation{
		deleteFn: func(ctx context.Context, action *DirectIPAction) error {
			return errors.New("eni not found")
		},
	}
	manager := &EndpointManager{
		pstsLister:        &fakePSTSLister{psts: psts},
		directIPAllocator: &fakeDirectIPAllocator{op: op},
	}
	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
}

func TestHandlePodDeletedIPReleaseDeleteIPError(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn": {{
					Family: ccev2.IPv4Family,
					Range:  []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.1"}},
				}},
			},
		},
	}
	op := &fakeDirectEndpointOperation{
		deleteFn: func(ctx context.Context, action *DirectIPAction) error {
			return errors.New("delete failed")
		},
	}
	manager := &EndpointManager{
		pstsLister:        &fakePSTSLister{psts: psts},
		directIPAllocator: &fakeDirectIPAllocator{op: op},
	}
	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	err := manager.handlePodDeletedIPRelease(context.Background(), cep, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestCreateAndResync(t *testing.T) {
	manager := &EndpointManager{}
	err := manager.Create(&ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{},
			},
		},
	})
	require.NoError(t, err)

	err = manager.Resync(context.Background())
	require.NoError(t, err)
}

func TestDeleteNotFound(t *testing.T) {
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{}}
	manager := &EndpointManager{
		k8sAPI:    &fakeEndpointGetterUpdater{lister: lister},
		nrsLister: &fakeNRSLister{},
		localPool: newLocalPool(),
	}
	err := manager.Delete("ns", "missing")
	require.NoError(t, err)
}

func TestDeleteNoAddressingRemovesFinalizer(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
			Finalizers: []string{
				k8s.FinalizerOfCCEEndpointRemoteIP,
			},
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{},
		},
	}
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{"ns/cep": cep}}
	updater := &fakeEndpointGetterUpdater{lister: lister}
	manager := &EndpointManager{
		k8sAPI:    updater,
		localPool: newLocalPool(),
	}
	err := manager.Delete("ns", "cep")
	require.NoError(t, err)
	require.NotNil(t, updater.updated)
	require.Empty(t, updater.updated.Finalizers)
}

func TestDeleteDeleteIPError(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{"ns/cep": cep}}
	updater := &fakeEndpointGetterUpdater{lister: lister}

	op := &fakeDirectEndpointOperation{
		deleteFn: func(ctx context.Context, action *DirectIPAction) error {
			return errors.New("delete failed")
		},
	}
	manager := &EndpointManager{
		k8sAPI:            updater,
		localPool:         newLocalPool(),
		directIPAllocator: &fakeDirectIPAllocator{op: op},
	}
	err := manager.Delete("ns", "cep")
	require.Error(t, err)
}

func TestUpdateSyncedPoolInvalidRevision(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	manager := &EndpointManager{}
	manager.addToSyncedPool("ns/cep", "bad")

	resource := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "ns",
			Name:            "cep",
			ResourceVersion: "1",
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
	}

	err := manager.Update(resource)
	require.Error(t, err)
}

func TestUpdatePodDeletedHandleError(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "handlePodDeletedIPRelease", func(_ *EndpointManager, _ context.Context, _ *ccev2.CCEEndpoint, _ *logrus.Entry) error {
		return errors.New("release failed")
	})
	defer patches.Reset()

	manager := &EndpointManager{}
	resource := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "cep",
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStatePodDeleted),
		},
	}

	err := manager.Update(resource)
	require.Error(t, err)
}

func TestDeleteGetError(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	updater := &fakeEndpointGetterUpdater{lister: &errorEndpointLister{err: errors.New("boom")}}
	manager := &EndpointManager{
		k8sAPI:    updater,
		localPool: newLocalPool(),
		nrsLister: &fakeNRSLister{},
	}
	err := manager.Delete("ns", "cep")
	require.Error(t, err)
}

func TestDeleteNonPSTSNodeNotFoundRemovesFinalizer(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
			Finalizers: []string{
				k8s.FinalizerOfCCEEndpointRemoteIP,
			},
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{NodeName: "node-1"},
			},
		},
	}
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{"ns/cep": cep}}
	updater := &fakeEndpointGetterUpdater{lister: lister}
	manager := &EndpointManager{
		k8sAPI:    updater,
		localPool: newLocalPool(),
		nrsLister: &fakeNRSLister{getErr: k8serrors.NewNotFound(ccev2.Resource("netresourceset"), "node-1")},
	}
	err := manager.Delete("ns", "cep")
	require.NoError(t, err)
	require.NotNil(t, updater.updated)
	require.Empty(t, updater.updated.Finalizers)
}

func TestDeletePSTSNodeEndpointNotFoundRemovesFinalizer(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
			Finalizers: []string{
				k8s.FinalizerOfCCEEndpointRemoteIP,
			},
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{
					PSTSName: "psts",
					NodeName: "node-1",
				},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{"ns/cep": cep}}
	updater := &fakeEndpointGetterUpdater{lister: lister}
	manager := &EndpointManager{
		k8sAPI:            updater,
		localPool:         newLocalPool(),
		directIPAllocator: &fakeDirectIPAllocator{err: k8serrors.NewNotFound(ccev2.Resource("netresourceset"), "node-1")},
	}
	err := manager.Delete("ns", "cep")
	require.NoError(t, err)
	require.NotNil(t, updater.updated)
	require.Empty(t, updater.updated.Finalizers)
}

func TestDeleteRemoveFinalizerUpdateError(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
			Finalizers: []string{
				k8s.FinalizerOfCCEEndpointRemoteIP,
			},
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{},
		},
	}
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{"ns/cep": cep}}
	updater := &fakeEndpointGetterUpdater{lister: lister, updateErr: errors.New("update failed")}
	manager := &EndpointManager{
		k8sAPI:    updater,
		localPool: newLocalPool(),
	}
	err := manager.Delete("ns", "cep")
	require.Error(t, err)
}

func TestDeleteNodeEndpointNil(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
			Finalizers: []string{
				k8s.FinalizerOfCCEEndpointRemoteIP,
			},
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{"ns/cep": cep}}
	updater := &fakeEndpointGetterUpdater{lister: lister}
	manager := &EndpointManager{
		k8sAPI:            updater,
		localPool:         newLocalPool(),
		directIPAllocator: &fakeDirectIPAllocator{op: nil, err: nil},
	}
	err := manager.Delete("ns", "cep")
	require.NoError(t, err)
	require.NotNil(t, updater.updated)
	require.Empty(t, updater.updated.Finalizers)
}

func TestDeleteIPAlreadyDeletedTreatAsSuccess(t *testing.T) {
	patches := gomonkey.NewPatches()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})
	defer patches.Reset()

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
			Finalizers: []string{
				k8s.FinalizerOfCCEEndpointRemoteIP,
			},
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
				},
			},
		},
	}
	lister := &fakeEndpointLister{endpoints: map[string]*ccev2.CCEEndpoint{"ns/cep": cep}}
	updater := &fakeEndpointGetterUpdater{lister: lister}
	op := &fakeDirectEndpointOperation{
		deleteFn: func(ctx context.Context, action *DirectIPAction) error {
			return errors.New("eni not found")
		},
	}
	manager := &EndpointManager{
		k8sAPI:            updater,
		localPool:         newLocalPool(),
		directIPAllocator: &fakeDirectIPAllocator{op: op},
	}
	err := manager.Delete("ns", "cep")
	require.NoError(t, err)
	require.NotNil(t, updater.updated)
	require.Empty(t, updater.updated.Finalizers)
}
