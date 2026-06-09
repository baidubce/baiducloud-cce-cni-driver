package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

type reserveEntry struct {
	cidr string
	id   string
	desc string
}

func makeListResult(entries []reserveEntry) *vpc.ListIpeserveResult {
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

func TestCalculateCIDRsForSubnetReserve_Empty(t *testing.T) {
	cidrs, origins, err := calculateCIDRsForSubnetReserve("sbn-1", nil)
	require.NoError(t, err)
	require.Empty(t, cidrs)
	require.Nil(t, origins)
}

func TestCalculateCIDRsForSubnetReserve_InvalidRange(t *testing.T) {
	allocs := ccev2.CustomAllocationList{
		{Range: []ccev2.CustomIPRange{{Start: "bad", End: "10.0.0.2"}}},
	}
	_, _, err := calculateCIDRsForSubnetReserve("sbn-1", allocs)
	require.Error(t, err)
}

func TestCalculateCIDRsForSubnetReserve_InvalidCIDR(t *testing.T) {
	allocs := ccev2.CustomAllocationList{
		{CIDRs: []string{"bad-cidr"}},
	}
	_, _, err := calculateCIDRsForSubnetReserve("sbn-1", allocs)
	require.Error(t, err)
}

func TestCalculateCIDRsForSubnetReserve_RangeAndCIDR(t *testing.T) {
	allocs := ccev2.CustomAllocationList{
		{
			Range: []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.2"}},
			CIDRs: []string{"10.0.1.0/30"},
		},
	}
	cidrs, origins, err := calculateCIDRsForSubnetReserve("sbn-1", allocs)
	require.NoError(t, err)
	require.NotEmpty(t, cidrs)
	require.NotNil(t, origins)
	foundRangeOrigin := false
	for _, origin := range origins {
		if origin != "" {
			foundRangeOrigin = true
			break
		}
	}
	require.True(t, foundRangeOrigin)
}

func TestGetReserveDescription(t *testing.T) {
	orig := operatorOption.Config.CCEClusterID
	operatorOption.Config.CCEClusterID = "cluster-1"
	t.Cleanup(func() { operatorOption.Config.CCEClusterID = orig })

	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p1",
			Namespace: "ns",
		},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "a"},
			},
		},
	}
	desc := getReserveDescription(psts)
	require.Contains(t, desc, "cluster-1")
	require.Contains(t, desc, "app=a")
}

func TestIsSelfReserve(t *testing.T) {
	ok := isSelfReserve("Reserved by CCE PSTS ns/p1 in cluster c1 for labels x", "ns", "p1", "c1")
	require.True(t, ok)
	require.False(t, isSelfReserve("other", "ns", "p1", "c1"))
}

func TestGetAllReserveInfoFromAnnotation(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{}
	info, err := getAllReserveInfoFromAnnotation(psts)
	require.NoError(t, err)
	require.Nil(t, info)

	psts.Annotations = map[string]string{
		AnnotationIPReserveIDs: "bad-json",
	}
	_, err = getAllReserveInfoFromAnnotation(psts)
	require.Error(t, err)

	data := map[string]IPReserveInfo{
		"sbn-1": {SubnetID: "sbn-1", Reserves: map[string]string{"10.0.0.0/24": "rid"}},
	}
	raw, _ := json.Marshal(data)
	psts.Annotations[AnnotationIPReserveIDs] = string(raw)
	info, err = getAllReserveInfoFromAnnotation(psts)
	require.NoError(t, err)
	require.Contains(t, info, "sbn-1")
}

func TestGetReserveInfoFromAnnotationBySubnet(t *testing.T) {
	data := map[string]IPReserveInfo{
		"sbn-1": {SubnetID: "sbn-1", Reserves: map[string]string{"10.0.0.0/24": "rid"}},
	}
	raw, _ := json.Marshal(data)
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationIPReserveIDs: string(raw)}},
	}
	info, err := getReserveInfoFromAnnotationBySubnet(psts, "sbn-1")
	require.NoError(t, err)
	require.Equal(t, "rid", info["10.0.0.0/24"])

	info, err = getReserveInfoFromAnnotationBySubnet(psts, "missing")
	require.NoError(t, err)
	require.Nil(t, info)
}

func TestSetReserveInfoToAnnotation(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{}
	err := setReserveInfoToAnnotation(psts, "sbn-1", map[string]string{"10.0.0.0/24": "rid"})
	require.NoError(t, err)
	require.NotNil(t, psts.Annotations)

	psts.Annotations[AnnotationIPReserveIDs] = "bad-json"
	err = setReserveInfoToAnnotation(psts, "sbn-1", map[string]string{})
	require.NoError(t, err)
	var got map[string]IPReserveInfo
	require.NoError(t, json.Unmarshal([]byte(psts.Annotations[AnnotationIPReserveIDs]), &got))
	require.Contains(t, got, "sbn-1")
}

func TestReserveCIDRsForSubnet(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	listCalls := 0
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		listCalls++
		switch listCalls {
		case 1:
			return makeListResult([]reserveEntry{{cidr: "10.0.0.0/24", id: "rid", desc: "other"}}), nil
		case 2:
			return makeListResult([]reserveEntry{{cidr: "10.0.0.0/24", id: "rid-self", desc: "Reserved by CCE PSTS ns/p1 in cluster c1 for labels "}}), nil
		default:
			return &vpc.ListIpeserveResult{}, nil
		}
	})

	createCalls := 0
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "CreateIpreserve", func(_ *vpc.Client, _ *vpc.CreateIpreserveArgs) (*vpc.CreateIpreserveResult, error) {
		createCalls++
		if createCalls == 1 {
			return &vpc.CreateIpreserveResult{IpReserveId: "rid-new"}, nil
		}
		return nil, &bce.BceServiceError{Code: "IpReserve.IpCidrConflict"}
	})

	orig := operatorOption.Config.CCEClusterID
	operatorOption.Config.CCEClusterID = "c1"
	t.Cleanup(func() { operatorOption.Config.CCEClusterID = orig })

	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	_, err := reserveCIDRsForSubnet(context.Background(), &vpc.Client{}, psts, "sbn-1", []string{"10.0.0.0/24"}, map[string]string{}, logrus.NewEntry(logrus.New()))
	require.Error(t, err)

	reserves, err := reserveCIDRsForSubnet(context.Background(), &vpc.Client{}, psts, "sbn-1", []string{"10.0.0.0/24", "10.0.1.0/24"}, map[string]string{}, logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.Equal(t, "rid-self", reserves["10.0.0.0/24"])
	require.Equal(t, "rid-new", reserves["10.0.1.0/24"])
	_, err = reserveCIDRsForSubnet(context.Background(), &vpc.Client{}, psts, "sbn-1", []string{"10.0.0.0/24"}, map[string]string{}, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}

func TestFindConflictOwner(t *testing.T) {
	existing := map[string]struct {
		reserveID   string
		isSelf      bool
		description string
	}{
		"10.0.0.0/24": {reserveID: "rid", description: "desc"},
	}
	msg := findConflictOwner("10.0.0.0/25", existing, map[string]string{})
	require.Contains(t, msg, "desc")
}

func TestUnreserveCIDRsForSubnet(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	listCalls := 0
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		listCalls++
		return makeListResult([]reserveEntry{
			{cidr: "10.0.0.0/24", id: "rid-1", desc: "Reserved by CCE PSTS ns/p1 in cluster c1 for labels "},
			{cidr: "10.0.1.0/24", id: "rid-2", desc: "Reserved by CCE PSTS ns/p1 in cluster c1 for labels "},
		}), nil
	})

	deleteCalls := 0
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "DeleteIpreserve", func(_ *vpc.Client, _ string, _ string) error {
		deleteCalls++
		if deleteCalls == 2 {
			return fmt.Errorf("IpReserve.IpReserveIdInvalid")
		}
		return nil
	})

	orig := operatorOption.Config.CCEClusterID
	operatorOption.Config.CCEClusterID = "c1"
	t.Cleanup(func() { operatorOption.Config.CCEClusterID = orig })

	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	err := unreserveCIDRsForSubnet(context.Background(), &vpc.Client{}, psts, "sbn-1", logrus.NewEntry(logrus.New()))
	require.NoError(t, err)
	require.Equal(t, 1, listCalls)
	require.Equal(t, 2, deleteCalls)
}

func TestHasPSTSFinalizer(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{PSTSFinalizer}}}
	require.True(t, hasPSTSFinalizer(psts))
	require.True(t, removePSTSFinalizer(psts))
	require.False(t, hasPSTSFinalizer(psts))
	addPSTSFinalizer(psts)
	require.True(t, hasPSTSFinalizer(psts))
}

func TestFindConflictOwner_InvalidCIDR(t *testing.T) {
	msg := findConflictOwner("bad", map[string]struct {
		reserveID   string
		isSelf      bool
		description string
	}{}, map[string]string{})
	require.Contains(t, msg, "already used")
}

func TestCalculateCIDRsForSubnetReserve_SortedCIDRs(t *testing.T) {
	allocs := ccev2.CustomAllocationList{
		{CIDRs: []string{"10.0.1.0/24", "10.0.0.0/24"}},
	}
	cidrs, _, err := calculateCIDRsForSubnetReserve("sbn-1", allocs)
	require.NoError(t, err)
	sort.Strings(cidrs)
	require.Equal(t, []string{"10.0.0.0/24", "10.0.1.0/24"}, cidrs)
}

func TestGetAllReserveInfoFromAnnotation_EmptyKey(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
	}
	info, err := getAllReserveInfoFromAnnotation(psts)
	require.NoError(t, err)
	require.Nil(t, info)
}

func TestSetReserveInfoToAnnotation_MarshalError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	patches.ApplyFunc(json.Marshal, func(_ interface{}) ([]byte, error) {
		return nil, errors.New("marshal error")
	})

	psts := &ccev2.PodSubnetTopologySpread{}
	err := setReserveInfoToAnnotation(psts, "sbn-1", map[string]string{"10.0.0.0/24": "rid"})
	require.Error(t, err)
}

func TestReserveCIDRsForSubnet_ListError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(&vpc.Client{}), "ListIpreserve", func(_ *vpc.Client, _ *vpc.ListIpeserveArgs) (*vpc.ListIpeserveResult, error) {
		return nil, errors.New("list error")
	})
	_, err := reserveCIDRsForSubnet(context.Background(), &vpc.Client{}, &ccev2.PodSubnetTopologySpread{}, "sbn-1", []string{"10.0.0.0/24"}, nil, logrus.NewEntry(logrus.New()))
	require.Error(t, err)
}
