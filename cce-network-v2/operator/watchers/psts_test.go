package watchers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	bceoption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccefake "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/fake"
	cceInformer "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/informers/externalversions"
	ccev1lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
	cce_lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
)

func newPSTSLister(objs ...*ccev2.PodSubnetTopologySpread) cce_lister.PodSubnetTopologySpreadLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, obj := range objs {
		_ = indexer.Add(obj)
	}
	return cce_lister.NewPodSubnetTopologySpreadLister(indexer)
}

func newEndpointLister(objs ...*ccev2.CCEEndpoint) cce_lister.CCEEndpointLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, obj := range objs {
		_ = indexer.Add(obj)
	}
	return cce_lister.NewCCEEndpointLister(indexer)
}

func newSubnetLister(objs ...*ccev1.Subnet) ccev1lister.SubnetLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, obj := range objs {
		_ = indexer.Add(obj)
	}
	return ccev1lister.NewSubnetLister(indexer)
}

type errorPSTSLister struct {
	err error
}

func (e *errorPSTSLister) List(selector labels.Selector) ([]*ccev2.PodSubnetTopologySpread, error) {
	return nil, e.err
}

func (e *errorPSTSLister) PodSubnetTopologySpreads(namespace string) cce_lister.PodSubnetTopologySpreadNamespaceLister {
	return &errorPSTSNamespaceLister{err: e.err}
}

type errorPSTSNamespaceLister struct {
	err error
}

func (e *errorPSTSNamespaceLister) List(selector labels.Selector) ([]*ccev2.PodSubnetTopologySpread, error) {
	return nil, e.err
}

func (e *errorPSTSNamespaceLister) Get(name string) (*ccev2.PodSubnetTopologySpread, error) {
	return nil, e.err
}

type pstsReserveEntry struct {
	cidr string
	id   string
	desc string
}

func makePSTSListResult(entries []pstsReserveEntry) *vpc.ListIpeserveResult {
	result := &vpc.ListIpeserveResult{}
	sliceType := reflect.TypeOf(result.IpReserves)
	slice := reflect.MakeSlice(sliceType, len(entries), len(entries))
	for i, entry := range entries {
		val := reflect.New(sliceType.Elem()).Elem()
		val.FieldByName("IpCidr").SetString(entry.cidr)
		val.FieldByName("IpReserveId").SetString(entry.id)
		val.FieldByName("Description").SetString(entry.desc)
		slice.Index(i).Set(val)
	}
	reflect.ValueOf(result).Elem().FieldByName("IpReserves").Set(slice)
	return result
}

func TestCalculateCIDRDiff(t *testing.T) {
	toAdd, toRemove := calculateCIDRDiff([]string{"a", "b"}, map[string]string{"b": "id", "c": "idc"})
	sort.Strings(toAdd)
	require.Equal(t, []string{"a"}, toAdd)
	require.Contains(t, toRemove, "c")
}

func TestCollectIPsInUseForSubnet(t *testing.T) {
	ips := collectIPsInUseForSubnet([]*ccev2.CCEEndpoint{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ep"},
			Status: ccev2.EndpointStatus{
				Networking: &ccev2.EndpointNetworking{
					Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn-1"}},
				},
			},
		},
	}, "sbn-1")
	require.Equal(t, "ns/ep", ips["10.0.0.1"])
}

func TestCheckCIDRsNotInUse(t *testing.T) {
	err := checkCIDRsNotInUse(map[string]string{"10.0.0.0/24": "id"}, map[string]string{"10.0.0.1": "pod"})
	require.Error(t, err)
	err = checkCIDRsNotInUse(map[string]string{"10.0.0.0/24": "id"}, map[string]string{"10.0.1.1": "pod"})
	require.NoError(t, err)
}

func TestHasRangeOrCIDR(t *testing.T) {
	require.False(t, hasRangeOrCIDR(nil))
	require.False(t, hasRangeOrCIDR(ccev2.CustomAllocationList{{Family: ccev2.IPv4Family}}))
	require.True(t, hasRangeOrCIDR(ccev2.CustomAllocationList{{CIDRs: []string{"10.0.0.0/24"}}}))
}

func TestBuildSubnetStatus(t *testing.T) {
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
		Spec: ccev1.SubnetSpec{
			ID:               "sbn-1",
			Name:             "sbn-1",
			CIDR:             "10.0.0.0/24",
			AvailabilityZone: "zone",
		},
		Status: ccev1.SubnetStatus{AvailableIPNum: 10, Enable: true},
	}
	ep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ep"},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn-1"}},
			},
		},
	}
	status := buildSubnetStatus(sbn, []*ccev2.CCEEndpoint{ep}, "sbn-1", nil)
	require.Equal(t, int32(1), status.PodCount)
	require.Equal(t, "ns/ep", status.IPAllocations["10.0.0.1"])
	require.Equal(t, 10, status.SubenetDetail.AvailableIPNum)
}

func TestBuildSubnetStatusWithReservedCIDRs(t *testing.T) {
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
		Spec: ccev1.SubnetSpec{
			ID:               "sbn-1",
			Name:             "sbn-1",
			CIDR:             "10.0.0.0/24",
			AvailabilityZone: "zone",
		},
		Status: ccev1.SubnetStatus{AvailableIPNum: 10, Enable: true},
	}
	ep1 := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ep-1"},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn-1"}},
			},
		},
	}
	ep2 := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ep-2"},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.2", Subnet: "sbn-1"}},
			},
		},
	}

	status := buildSubnetStatus(
		sbn,
		[]*ccev2.CCEEndpoint{ep1, ep2},
		"sbn-1",
		map[string]string{"10.0.0.0/30": "rid"},
	)
	require.Equal(t, int32(2), status.PodCount)
	require.Equal(t, 2, status.SubenetDetail.AvailableIPNum)
	require.False(t, status.SubenetDetail.HasNoMoreIP)
}

func TestBuildSubnetStatusWithReservedCIDRsExhausted(t *testing.T) {
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
		Spec: ccev1.SubnetSpec{
			ID:               "sbn-1",
			Name:             "sbn-1",
			CIDR:             "10.0.0.0/24",
			AvailabilityZone: "zone",
		},
		Status: ccev1.SubnetStatus{AvailableIPNum: 10, Enable: true},
	}
	ep1 := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ep-1"},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.0", Subnet: "sbn-1"}},
			},
		},
	}
	ep2 := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ep-2"},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn-1"}},
			},
		},
	}

	status := buildSubnetStatus(
		sbn,
		[]*ccev2.CCEEndpoint{ep1, ep2},
		"sbn-1",
		map[string]string{"10.0.0.0/31": "rid"},
	)
	require.Equal(t, 0, status.SubenetDetail.AvailableIPNum)
	require.True(t, status.SubenetDetail.HasNoMoreIP)
}

func TestUnreserveSingleCIDR(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "DeleteIpreserve", func(_ *vpc.Client, _ string, _ string) error {
		return nil
	})
	err := syncher.unreserveSingleCIDR(&ccev2.PodSubnetTopologySpread{}, "sbn", "10.0.0.0/24", "rid", logrus.NewEntry(logrus.New()))
	require.NoError(t, err)

	patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "DeleteIpreserve", func(_ *vpc.Client, _ string, _ string) error {
		return fmt.Errorf("IpReserve.IpReserveIdInvalid")
	})
	err = syncher.unreserveSingleCIDR(&ccev2.PodSubnetTopologySpread{}, "sbn", "10.0.0.0/24", "rid", logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
}

func TestReserveNewCIDRs(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	callCount := 0
	patches.ApplyFunc(reserveCIDRsForSubnet, func(_ context.Context, _ *vpc.Client, _ *ccev2.PodSubnetTopologySpread, _ string, _ []string, _ map[string]string, _ *logrus.Entry) (map[string]string, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("reserve fail")
		}
		return map[string]string{"10.0.0.0/24": "rid"}, nil
	})

	_, err := syncher.reserveNewCIDRs(&ccev2.PodSubnetTopologySpread{}, "sbn", []string{"10.0.0.0/24"}, nil, logrus.NewEntry(logrus.New()))
	require.Error(t, err)

	res, err := syncher.reserveNewCIDRs(&ccev2.PodSubnetTopologySpread{}, "sbn", []string{"10.0.0.0/24"}, nil, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.Equal(t, "rid", res["10.0.0.0/24"])
}

func TestListPSTSReservedCIDRsBySubnetIDWithReserveID(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		return makePSTSListResult([]pstsReserveEntry{{cidr: "10.0.0.0/24", id: "rid", desc: "Reserved by CCE PSTS ns/p1 in cluster c1 for labels "}}), nil
	})

	orig := operatorOption.Config.CCEClusterID
	operatorOption.Config.CCEClusterID = "c1"
	t.Cleanup(func() { operatorOption.Config.CCEClusterID = orig })

	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	res, err := syncher.listPSTSReservedCIDRsBySubnetIDWithReserveID(psts, "sbn", "c1", logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.Equal(t, "rid", res["10.0.0.0/24"])
}

func TestListPSTSReservedCIDRsBySubnetID(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		return makePSTSListResult([]pstsReserveEntry{{cidr: "10.0.0.0/24", id: "rid", desc: "Reserved by CCE PSTS ns/p1 in cluster c1 for labels "}}), nil
	})

	orig := operatorOption.Config.CCEClusterID
	operatorOption.Config.CCEClusterID = "c1"
	t.Cleanup(func() { operatorOption.Config.CCEClusterID = orig })

	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	res, err := syncher.listPSTSReservedCIDRsBySubnetID(psts, "sbn", "c1", logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/24"}, res)
}

func TestRebuildReserveInfoFromVPC(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "Lister", func(_ *subnetUpdaterImpl) ccev1lister.SubnetLister {
		return newSubnetLister(&ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"}})
	})
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		return makePSTSListResult([]pstsReserveEntry{{cidr: "10.0.0.0/24", id: "rid", desc: "Reserved by CCE PSTS ns/p1 in cluster c1 for labels "}}), nil
	})

	orig := operatorOption.Config.CCEClusterID
	operatorOption.Config.CCEClusterID = "c1"
	t.Cleanup(func() { operatorOption.Config.CCEClusterID = orig })

	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	info, err := syncher.rebuildReserveInfoFromVPC(psts, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.Contains(t, info, "sbn-1")
}

func TestCleanupRemovedSubnets_RebuildAndClear(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyFunc(getAllReserveInfoFromAnnotation, func(_ *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
		return nil, errors.New("bad")
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "rebuildReserveInfoFromVPC", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ *logrus.Entry) (map[string]IPReserveInfo, error) {
		return map[string]IPReserveInfo{}, nil
	})

	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationIPReserveIDs: "bad"}},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{}},
	}
	updated, err := syncher.cleanupRemovedSubnets(psts, nil, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.True(t, updated)
	require.Nil(t, psts.Annotations)
}

func TestCleanupRemovedSubnets_Unreserve(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyFunc(getAllReserveInfoFromAnnotation, func(_ *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
		return map[string]IPReserveInfo{
			"sbn-1": {SubnetID: "sbn-1", Reserves: map[string]string{"10.0.0.0/24": "rid"}},
		}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "listPSTSReservedCIDRsBySubnetID", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ string, _ *logrus.Entry) ([]string, error) {
		return []string{"10.0.0.0/24"}, nil
	})
	patches.ApplyFunc(unreserveCIDRsForSubnet, func(_ context.Context, _ *vpc.Client, _ *ccev2.PodSubnetTopologySpread, _ string, _ *logrus.Entry) error {
		return nil
	})

	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{}},
	}
	updated, err := syncher.cleanupRemovedSubnets(psts, nil, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.True(t, updated)
}

func TestProcessSubnetReserve_ErrorsAndSuccess(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "EnsureSubnet", func(_ *subnetUpdaterImpl, _ string, _ string) (*ccev1.Subnet, error) {
		return &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
			Spec:       ccev1.SubnetSpec{ID: "sbn-1", Name: "sbn-1"},
			Status:     ccev1.SubnetStatus{Enable: true, AvailableIPNum: 10},
		}, nil
	})

	patches.ApplyFunc(calculateCIDRsForSubnetReserve, func(_ string, _ ccev2.CustomAllocationList) ([]string, map[string]string, error) {
		return []string{"10.0.0.0/24"}, map[string]string{}, nil
	})
	patches.ApplyFunc(getReserveInfoFromAnnotationBySubnet, func(_ *ccev2.PodSubnetTopologySpread, _ string) (map[string]string, error) {
		return map[string]string{}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "reserveNewCIDRs", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ []string, _ map[string]string, _ *logrus.Entry) (map[string]string, error) {
		return map[string]string{"10.0.0.0/24": "rid"}, nil
	})

	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}}},
	}
	psts.Annotations = map[string]string{
		AnnotationIPReserveIDs: `{"sbn-1":{"subnetId":"sbn-1","reserves":{"10.0.0.0/24":"rid"}}}`,
	}
	status, hasCIDRs, updated, err := syncher.processSubnetReserve(psts, "sbn-1", ccev2.CustomAllocationList{}, nil, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.True(t, hasCIDRs)
	require.True(t, updated)
	require.Equal(t, "sbn-1", status.SubenetDetail.ID)
}

func TestPersistPSTSUpdate(t *testing.T) {
	var lister cce_lister.PodSubnetTopologySpreadLister
	updateCalled := 0
	updateStatusCalled := 0
	conflictMode := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return lister
	})
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Update", func(_ *pstsUpdaterImpl, obj *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
		updateCalled++
		if conflictMode && updateCalled == 1 {
			return nil, k8serrors.NewConflict(schema.GroupResource{Group: "cce", Resource: "psts"}, "p1", errors.New("conflict"))
		}
		return obj, nil
	})
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "UpdateStatus", func(_ *pstsUpdaterImpl, obj *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
		updateStatusCalled++
		return obj, nil
	})

	syncher := &pstsSyncher{}

	// Case 1: metadata + status update
	updateCalled = 0
	updateStatusCalled = 0
	conflictMode = false
	current := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	lister = newPSTSLister(current)
	psts := current.DeepCopy()
	psts.Annotations = map[string]string{"a": "b"}
	psts.Status.Name = "p1"
	err := syncher.persistPSTSUpdate(psts, true, true, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.Equal(t, 1, updateCalled)
	require.Equal(t, 1, updateStatusCalled)

	// Case 2: conflict retry on metadata update
	updateCalled = 0
	updateStatusCalled = 0
	conflictMode = true
	current = &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	lister = newPSTSLister(current)
	psts = current.DeepCopy()
	psts.Annotations = map[string]string{"a": "b"}
	err = syncher.persistPSTSUpdate(psts, true, false, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.GreaterOrEqual(t, updateCalled, 2)
}

func TestUpdate_AddFinalizerAndPersist(t *testing.T) {
	syncher := &pstsSyncher{}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(CCEEndpointClient), "Lister", func(_ *CCEEndpointUpdaterImpl) cce_lister.CCEEndpointLister {
		return newEndpointLister()
	})
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "EnsureSubnet", func(_ *subnetUpdaterImpl, _ string, _ string) (*ccev1.Subnet, error) {
		return &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
			Spec:       ccev1.SubnetSpec{ID: "sbn-1", Name: "sbn-1"},
			Status:     ccev1.SubnetStatus{Enable: true, AvailableIPNum: 1},
		}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "cleanupRemovedSubnets", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ []*ccev2.CCEEndpoint, _ *logrus.Entry) (bool, error) {
		return false, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "processSubnetReserve", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ ccev2.CustomAllocationList, _ []*ccev2.CCEEndpoint, _ *logrus.Entry) (ccev2.SubnetPodStatus, bool, bool, error) {
		return ccev2.SubnetPodStatus{SubenetDetail: ccev2.SubenetDetail{Enable: true, AvailableIPNum: 1}}, false, false, nil
	})

	called := 0
	sawFinalizer := false
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "persistPSTSUpdate", func(_ *pstsSyncher, p *ccev2.PodSubnetTopologySpread, _ bool, _ bool, _ *logrus.Entry) error {
		called++
		sawFinalizer = hasPSTSFinalizer(p)
		return nil
	})

	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}}},
	}
	err := syncher.Update(psts)
	require.NoError(t, err)
	require.True(t, sawFinalizer)
	require.Equal(t, 1, called)
}

func TestUpdate_CountAndAffectEndpointsByDifferentRules(t *testing.T) {
	syncher := &pstsSyncher{}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyMethod(reflect.TypeOf(CCEEndpointClient), "Lister", func(_ *CCEEndpointUpdaterImpl) cce_lister.CCEEndpointLister {
		return newEndpointLister(
			&ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{Name: "ep-match-psts", Namespace: "ns", Labels: map[string]string{"app": "demo"}},
				Spec:       ccev2.EndpointSpec{Network: ccev2.EndpointNetworkSpec{IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"}}},
			},
			&ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{Name: "ep-nomatch-psts", Namespace: "ns", Labels: map[string]string{"app": "other"}},
				Spec:       ccev2.EndpointSpec{Network: ccev2.EndpointNetworkSpec{IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"}}},
			},
			&ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{Name: "ep-match-no-psts", Namespace: "ns", Labels: map[string]string{"app": "demo"}},
			},
			&ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{Name: "ep-other-ns", Namespace: "other", Labels: map[string]string{"app": "demo"}},
				Spec:       ccev2.EndpointSpec{Network: ccev2.EndpointNetworkSpec{IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"}}},
			},
		)
	})
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "EnsureSubnet", func(_ *subnetUpdaterImpl, _ string, _ string) (*ccev1.Subnet, error) {
		return &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
			Spec:       ccev1.SubnetSpec{ID: "sbn-1", Name: "sbn-1"},
			Status:     ccev1.SubnetStatus{Enable: true, AvailableIPNum: 1},
		}, nil
	})

	assertMatched := func(eps []*ccev2.CCEEndpoint) {
		got := make([]string, 0, len(eps))
		for _, ep := range eps {
			got = append(got, ep.Namespace+"/"+ep.Name)
		}
		require.ElementsMatch(t, []string{"ns/ep-match-psts", "ns/ep-nomatch-psts"}, got)
	}
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "cleanupRemovedSubnets", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, matchedEndpoints []*ccev2.CCEEndpoint, _ *logrus.Entry) (bool, error) {
		assertMatched(matchedEndpoints)
		return false, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "processSubnetReserve", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ ccev2.CustomAllocationList, matchedEndpoints []*ccev2.CCEEndpoint, _ *logrus.Entry) (ccev2.SubnetPodStatus, bool, bool, error) {
		assertMatched(matchedEndpoints)
		return ccev2.SubnetPodStatus{SubenetDetail: ccev2.SubenetDetail{Enable: true, AvailableIPNum: 1}}, false, false, nil
	})

	persistCalled := 0
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "persistPSTSUpdate", func(_ *pstsSyncher, resource *ccev2.PodSubnetTopologySpread, _ bool, _ bool, _ *logrus.Entry) error {
		persistCalled++
		require.Equal(t, int32(2), resource.Status.PodMatchedCount)
		return nil
	})

	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
			Subnets:  map[string]ccev2.CustomAllocationList{"sbn-1": {}},
		},
	}
	err := syncher.Update(psts)
	require.NoError(t, err)
	require.Equal(t, 1, persistCalled)
}

func TestDelete_PSTSNotFound(t *testing.T) {
	lister := newPSTSLister()
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return lister
	})

	syncher := &pstsSyncher{}
	err := syncher.Delete("ns", "missing")
	require.NoError(t, err)
}

func TestDelete_UnreserveAndFinalizer(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns", Finalizers: []string{PSTSFinalizer}},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}}},
	}
	lister := newPSTSLister(psts)

	updated := 0
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return lister
	})
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Update", func(_ *pstsUpdaterImpl, obj *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
		updated++
		return obj, nil
	})

	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches.ApplyFunc(unreserveCIDRsForSubnet, func(_ context.Context, _ *vpc.Client, _ *ccev2.PodSubnetTopologySpread, _ string, _ *logrus.Entry) error {
		return nil
	})
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		return &vpc.ListIpeserveResult{}, nil
	})

	origCluster := operatorOption.Config.CCEClusterID
	operatorOption.Config.CCEClusterID = "c1"
	t.Cleanup(func() { operatorOption.Config.CCEClusterID = origCluster })

	err := syncher.Delete("ns", "p1")
	require.NoError(t, err)
	require.Equal(t, 1, updated)
}

func TestDelete_CorruptedAnnotationRebuildFromVPC(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "p1",
			Namespace:   "ns",
			Finalizers:  []string{PSTSFinalizer},
			Annotations: map[string]string{AnnotationIPReserveIDs: "bad-json"},
		},
		Spec: ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{}},
	}
	lister := newPSTSLister(psts)

	updated := 0
	unreserveCalled := 0
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return lister
	})
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Update", func(_ *pstsUpdaterImpl, obj *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
		updated++
		return obj, nil
	})

	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "rebuildReserveInfoFromVPC", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ *logrus.Entry) (map[string]IPReserveInfo, error) {
		return map[string]IPReserveInfo{
			"sbn-rebuild": {SubnetID: "sbn-rebuild", Reserves: map[string]string{"10.0.0.0/24": "rid"}},
		}, nil
	})
	patches.ApplyFunc(unreserveCIDRsForSubnet, func(_ context.Context, _ *vpc.Client, _ *ccev2.PodSubnetTopologySpread, sbnID string, _ *logrus.Entry) error {
		if sbnID == "sbn-rebuild" {
			unreserveCalled++
		}
		return nil
	})
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		return &vpc.ListIpeserveResult{}, nil
	})

	err := syncher.Delete("ns", "p1")
	require.NoError(t, err)
	require.Equal(t, 1, unreserveCalled)
	require.Equal(t, 1, updated)
}

func TestStartSynchronizingPSTS(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	vpcCalls := 0
	patches.ApplyFunc(bceoption.VPCClient, func() *vpc.Client {
		vpcCalls++
		if vpcCalls == 1 {
			return nil
		}
		return &vpc.Client{}
	})
	patches.ApplyFunc(cm.NewResyncController, func(_ string, _ int, _ float32, _ int, _ cache.SharedIndexInformer, _ cm.ReconcileFunc) *cm.ResyncController {
		return &cm.ResyncController{}
	})
	patches.ApplyMethod(reflect.TypeOf(&cm.ResyncController{}), "RunWithResync", func(_ *cm.ResyncController, _ time.Duration) {})

	// Patch k8s.CCEClient to return a client with a non-nil informer factory.
	patches.ApplyFunc(k8s.CCEClient, func() *k8s.K8sCCEClient {
		fake := ccefake.NewSimpleClientset()
		factory := cceInformer.NewSharedInformerFactory(fake, 0)
		return &k8s.K8sCCEClient{Interface: fake, Informers: factory}
	})
	patches.ApplyFunc(k8s.EventBroadcaster, func() record.EventBroadcaster {
		return record.NewBroadcaster()
	})

	err := StartSynchronizingPSTS(context.Background())
	require.Error(t, err)

	err = StartSynchronizingPSTS(context.Background())
	require.NoError(t, err)
}

func TestCheckCIDRsNotInUse_InvalidCIDR(t *testing.T) {
	err := checkCIDRsNotInUse(map[string]string{"bad": "id"}, map[string]string{"10.0.0.1": "pod"})
	require.NoError(t, err)
}

func TestShouldCheckReserveShrinkInUse(t *testing.T) {
	require.True(t, shouldCheckReserveShrinkInUse(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed}))
	require.False(t, shouldCheckReserveShrinkInUse(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic}))
	require.False(t, shouldCheckReserveShrinkInUse(nil))
}

func TestProcessSubnetReserve_CheckCIDRsInUse(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "EnsureSubnet", func(_ *subnetUpdaterImpl, _ string, _ string) (*ccev1.Subnet, error) {
		return &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
			Spec:       ccev1.SubnetSpec{ID: "sbn-1", Name: "sbn-1"},
			Status:     ccev1.SubnetStatus{Enable: true, AvailableIPNum: 10},
		}, nil
	})

	patches.ApplyFunc(calculateCIDRsForSubnetReserve, func(_ string, _ ccev2.CustomAllocationList) ([]string, map[string]string, error) {
		return []string{}, map[string]string{}, nil
	})
	patches.ApplyFunc(getReserveInfoFromAnnotationBySubnet, func(_ *ccev2.PodSubnetTopologySpread, _ string) (map[string]string, error) {
		return map[string]string{"10.0.0.0/24": "rid"}, nil
	})
	matchedEndpoints := []*ccev2.CCEEndpoint{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "ns"},
			Status: ccev2.EndpointStatus{
				Networking: &ccev2.EndpointNetworking{
					Addressing: ccev2.AddressPairList{
						&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn-1"},
					},
				},
			},
		},
	}

	_, _, _, err := syncher.processSubnetReserve(&ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		},
	}, "sbn-1", nil, matchedEndpoints, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestProcessSubnetReserve_ElasticSkipsCIDRsInUseCheck(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "EnsureSubnet", func(_ *subnetUpdaterImpl, _ string, _ string) (*ccev1.Subnet, error) {
		return &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
			Spec:       ccev1.SubnetSpec{ID: "sbn-1", Name: "sbn-1"},
			Status:     ccev1.SubnetStatus{Enable: true, AvailableIPNum: 10},
		}, nil
	})
	patches.ApplyFunc(calculateCIDRsForSubnetReserve, func(_ string, _ ccev2.CustomAllocationList) ([]string, map[string]string, error) {
		return []string{}, map[string]string{}, nil
	})
	patches.ApplyFunc(getReserveInfoFromAnnotationBySubnet, func(_ *ccev2.PodSubnetTopologySpread, _ string) (map[string]string, error) {
		return map[string]string{"10.0.0.0/24": "rid"}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "unreserveSingleCIDR", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ string, _ string, _ *logrus.Entry) error {
		return nil
	})

	matchedEndpoints := []*ccev2.CCEEndpoint{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "ns"},
			Status: ccev2.EndpointStatus{
				Networking: &ccev2.EndpointNetworking{
					Addressing: ccev2.AddressPairList{
						&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn-1"},
					},
				},
			},
		},
	}

	_, _, _, err := syncher.processSubnetReserve(&ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		},
	}, "sbn-1", nil, matchedEndpoints, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
}

func TestProcessSubnetReserve_UnreserveError(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "EnsureSubnet", func(_ *subnetUpdaterImpl, _ string, _ string) (*ccev1.Subnet, error) {
		return &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
			Spec:       ccev1.SubnetSpec{ID: "sbn-1", Name: "sbn-1"},
			Status:     ccev1.SubnetStatus{Enable: true, AvailableIPNum: 10},
		}, nil
	})

	patches.ApplyFunc(calculateCIDRsForSubnetReserve, func(_ string, _ ccev2.CustomAllocationList) ([]string, map[string]string, error) {
		return []string{}, map[string]string{}, nil
	})
	patches.ApplyFunc(getReserveInfoFromAnnotationBySubnet, func(_ *ccev2.PodSubnetTopologySpread, _ string) (map[string]string, error) {
		return map[string]string{"10.0.0.0/24": "rid"}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "unreserveSingleCIDR", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ string, _ string, _ *logrus.Entry) error {
		return errors.New("unreserve failed")
	})

	_, _, _, err := syncher.processSubnetReserve(&ccev2.PodSubnetTopologySpread{}, "sbn-1", nil, nil, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestCleanupRemovedSubnets_CheckInUse(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyFunc(getAllReserveInfoFromAnnotation, func(_ *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
		return map[string]IPReserveInfo{
			"sbn-1": {SubnetID: "sbn-1", Reserves: map[string]string{"10.0.0.0/24": "rid"}},
			"sbn-2": {SubnetID: "sbn-2", Reserves: map[string]string{"10.0.1.0/24": "rid-2"}},
		}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "listPSTSReservedCIDRsBySubnetID", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ string, _ *logrus.Entry) ([]string, error) {
		return []string{"10.0.0.0/24"}, nil
	})

	endpoints := []*ccev2.CCEEndpoint{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ep"},
			Status: ccev2.EndpointStatus{
				Networking: &ccev2.EndpointNetworking{
					Addressing: ccev2.AddressPairList{&ccev2.AddressPair{Subnet: "sbn-1", IP: "10.0.0.1"}},
				},
			},
		},
	}
	psts := &ccev2.PodSubnetTopologySpread{Spec: ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-2": {}}}}
	_, err := syncher.cleanupRemovedSubnets(psts, endpoints, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestDelete_VerifyRemainingReserves(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}}},
	}
	psts.Annotations = map[string]string{
		AnnotationIPReserveIDs: `{"sbn-1":{"subnetId":"sbn-1","reserves":{"10.0.0.0/24":"rid"}}}`,
	}
	lister := newPSTSLister(psts)
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return lister
	})

	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches.ApplyFunc(unreserveCIDRsForSubnet, func(_ context.Context, _ *vpc.Client, _ *ccev2.PodSubnetTopologySpread, _ string, _ *logrus.Entry) error {
		return nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "listPSTSReservedCIDRsBySubnetID", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ string, _ *logrus.Entry) ([]string, error) {
		return []string{"10.0.0.0/24"}, nil
	})

	err := syncher.Delete("ns", "p1")
	require.Error(t, err)
}

func TestCleanupRemovedSubnets_MarshalError(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyFunc(getAllReserveInfoFromAnnotation, func(_ *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
		return map[string]IPReserveInfo{
			"sbn-1": {SubnetID: "sbn-1", Reserves: map[string]string{"10.0.0.0/24": "rid"}},
			"sbn-2": {SubnetID: "sbn-2", Reserves: map[string]string{"10.0.1.0/24": "rid-2"}},
		}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "listPSTSReservedCIDRsBySubnetID", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ string, _ *logrus.Entry) ([]string, error) {
		return nil, nil
	})
	patches.ApplyFunc(jsonMarshal, func(_ interface{}) ([]byte, error) {
		return nil, errors.New("marshal error")
	})

	psts := &ccev2.PodSubnetTopologySpread{Spec: ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-2": {}}}}
	_, err := syncher.cleanupRemovedSubnets(psts, nil, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestProcessSubnetReserve_SetAnnotationError(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(CCESubnetClient), "EnsureSubnet", func(_ *subnetUpdaterImpl, _ string, _ string) (*ccev1.Subnet, error) {
		return &ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
			Spec:       ccev1.SubnetSpec{ID: "sbn-1", Name: "sbn-1"},
			Status:     ccev1.SubnetStatus{Enable: true, AvailableIPNum: 10},
		}, nil
	})

	patches.ApplyFunc(calculateCIDRsForSubnetReserve, func(_ string, _ ccev2.CustomAllocationList) ([]string, map[string]string, error) {
		return []string{"10.0.0.0/24"}, map[string]string{}, nil
	})
	patches.ApplyFunc(getReserveInfoFromAnnotationBySubnet, func(_ *ccev2.PodSubnetTopologySpread, _ string) (map[string]string, error) {
		return map[string]string{}, nil
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "reserveNewCIDRs", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ string, _ []string, _ map[string]string, _ *logrus.Entry) (map[string]string, error) {
		return map[string]string{"10.0.0.0/24": "rid"}, nil
	})
	patches.ApplyFunc(setReserveInfoToAnnotation, func(_ *ccev2.PodSubnetTopologySpread, _ string, _ map[string]string) error {
		return errors.New("set error")
	})

	_, _, _, err := syncher.processSubnetReserve(&ccev2.PodSubnetTopologySpread{}, "sbn-1", nil, nil, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestCleanupRemovedSubnets_ListReservesError(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(getAllReserveInfoFromAnnotation, func(_ *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
		return map[string]IPReserveInfo{"sbn-1": {SubnetID: "sbn-1", Reserves: map[string]string{"10.0.0.0/24": "rid"}}}, nil
	})
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		return nil, errors.New("list error")
	})

	psts := &ccev2.PodSubnetTopologySpread{Spec: ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{}}}
	_, err := syncher.cleanupRemovedSubnets(psts, nil, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestPersistPSTSUpdate_GetError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return &errorPSTSLister{err: errors.New("get error")}
	})

	syncher := &pstsSyncher{}
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	err := syncher.persistPSTSUpdate(psts, true, true, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestPersistPSTSUpdate_UpdateError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	current := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return newPSTSLister(current)
	})
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Update", func(_ *pstsUpdaterImpl, _ *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
		return nil, errors.New("update error")
	})

	syncher := &pstsSyncher{}
	psts := current.DeepCopy()
	psts.Annotations = map[string]string{"a": "b"}
	err := syncher.persistPSTSUpdate(psts, true, false, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestPersistPSTSUpdate_UpdateStatusError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	current := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "Lister", func(_ *pstsUpdaterImpl) cce_lister.PodSubnetTopologySpreadLister {
		return newPSTSLister(current)
	})
	patches.ApplyMethod(reflect.TypeOf(pstsClient), "UpdateStatus", func(_ *pstsUpdaterImpl, _ *ccev2.PodSubnetTopologySpread) (*ccev2.PodSubnetTopologySpread, error) {
		return nil, errors.New("status update error")
	})

	syncher := &pstsSyncher{}
	psts := current.DeepCopy()
	psts.Status.Name = "p1"
	err := syncher.persistPSTSUpdate(psts, false, true, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestCleanupRemovedSubnets_RebuildError(t *testing.T) {
	syncher := &pstsSyncher{vpcClient: &vpc.Client{}}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyFunc(getAllReserveInfoFromAnnotation, func(_ *ccev2.PodSubnetTopologySpread) (map[string]IPReserveInfo, error) {
		return nil, errors.New("bad")
	})
	patches.ApplyPrivateMethod(reflect.TypeOf(syncher), "rebuildReserveInfoFromVPC", func(_ *pstsSyncher, _ *ccev2.PodSubnetTopologySpread, _ *logrus.Entry) (map[string]IPReserveInfo, error) {
		return nil, errors.New("rebuild failed")
	})

	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationIPReserveIDs: "bad"}},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{}},
	}
	_, err := syncher.cleanupRemovedSubnets(psts, nil, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}
