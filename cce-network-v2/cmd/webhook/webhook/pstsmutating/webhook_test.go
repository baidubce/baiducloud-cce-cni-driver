package pstsmutating

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	k8sutilnet "k8s.io/utils/net"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/ccegateway"
	cloudtesting "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccefake "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/clientset/versioned/fake"
	cceInformer "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/informers/externalversions"
	ccev1lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
)

type fakeSubnetClient struct {
	sbn *ccev1.Subnet
}

func (f *fakeSubnetClient) Lister() ccev1lister.SubnetLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if f.sbn != nil {
		_ = indexer.Add(f.sbn)
	}
	return ccev1lister.NewSubnetLister(indexer)
}

func TestJoinFirst5(t *testing.T) {
	require.Contains(t, joinFirst5([]string{"a"}), "a")
	require.Contains(t, joinFirst5([]string{"1", "2", "3", "4", "5", "6"}), "more")
}

func TestCalculatePSTSRemovals(t *testing.T) {
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{CIDRs: []string{"10.0.0.0/24"}, Range: []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.2"}}}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{CIDRs: []string{}, Range: []ccev2.CustomIPRange{}}},
			},
		},
	}
	removedSubnets, removedRanges, removedCIDRs := calculatePSTSRemovals(oldObj, newObj)
	require.Empty(t, removedSubnets)
	require.Len(t, removedRanges["sbn-1"], 1)
	require.Len(t, removedCIDRs["sbn-1"], 1)
}

func TestIPInRange(t *testing.T) {
	require.False(t, ipInRange(nil, nil, nil))
	ip := net.ParseIP("10.0.0.2")
	require.True(t, ipInRange(ip, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.3")))
	require.False(t, ipInRange(ip, net.ParseIP("10.0.0.3"), net.ParseIP("10.0.0.4")))
}

func TestDefaultPstsSpec(t *testing.T) {
	h := &MutatingPSTSHandler{}
	spec := &ccev2.PodSubnetTopologySpreadSpec{}
	h.defaultPstsSpec(spec)
	require.NotNil(t, spec.Strategy)
	require.Equal(t, ccev2.IPAllocTypeElastic, spec.Strategy.Type)
}

func TestValidateStrategy(t *testing.T) {
	h := &MutatingPSTSHandler{}
	errs := h.validateStrategy(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic, TTL: &metav1.Duration{Duration: time.Second}}, field.NewPath("strategy"))
	require.NotEmpty(t, errs)
	errs = h.validateStrategy(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed, TTL: &metav1.Duration{Duration: -time.Second}}, field.NewPath("strategy"))
	require.NotEmpty(t, errs)
	errs = h.validateStrategy(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed, TTL: &metav1.Duration{Duration: 0}}, field.NewPath("strategy"))
	require.NotEmpty(t, errs)
	errs = h.validateStrategy(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed, TTL: &metav1.Duration{Duration: time.Second}}, field.NewPath("strategy"))
	require.Empty(t, errs)
	errs = h.validateStrategy(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeNil}, field.NewPath("strategy"))
	require.NotEmpty(t, errs)
}

func TestValidatePstsSpec(t *testing.T) {
	h := &MutatingPSTSHandler{}
	spec := &ccev2.PodSubnetTopologySpreadSpec{
		Priority: -1,
		MaxSkew:  -1,
		Subnets:  map[string]ccev2.CustomAllocationList{},
	}
	errs := h.validatePstsSpec(spec, field.NewPath("spec"))
	require.NotEmpty(t, errs)
}

func TestValidatePstsSpec_FixedRejectNoRangeSubnet(t *testing.T) {
	h := &MutatingPSTSHandler{}
	spec := &ccev2.PodSubnetTopologySpreadSpec{
		Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		Subnets: map[string]ccev2.CustomAllocationList{
			"sbn-1": {
				{Family: ccev2.IPv4Family},
			},
		},
	}

	errs := h.validatePstsSpec(spec, field.NewPath("spec"))
	require.NotEmpty(t, errs)
	require.Contains(t, errs.ToAggregate().Error(), "does not support no-range subnet")
}

func TestValidateFixedStrategyNoRangeSubnets_FixedAllowRangeSubnet(t *testing.T) {
	spec := &ccev2.PodSubnetTopologySpreadSpec{
		Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		Subnets: map[string]ccev2.CustomAllocationList{
			"sbn-1": {
				{Family: ccev2.IPv4Family, Range: []ccev2.CustomIPRange{{Start: "10.0.0.2", End: "10.0.0.10"}}},
			},
		},
	}

	errs := validateFixedStrategyNoRangeSubnets(spec, field.NewPath("spec"))
	require.Empty(t, errs)
}

func TestValidateFixedStrategyNoRangeSubnets_ElasticAllowNoRangeSubnet(t *testing.T) {
	spec := &ccev2.PodSubnetTopologySpreadSpec{
		Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		Subnets: map[string]ccev2.CustomAllocationList{
			"sbn-1": {
				{Family: ccev2.IPv4Family},
			},
		},
	}

	errs := validateFixedStrategyNoRangeSubnets(spec, field.NewPath("spec"))
	require.Empty(t, errs)
}

func TestValidateSubnet_Error(t *testing.T) {
	h := &MutatingPSTSHandler{}
	origGetClusterConfig := getClusterConfig
	origBuildCloudClient := buildCloudClient
	origDescribeSubnet := describeSubnet
	getClusterConfig = func() (clusterConfig, error) {
		return clusterConfig{
			VPCID:     "vpc-test",
			Region:    "bj",
			ClusterID: "cce-test",
		}, nil
	}
	buildCloudClient = func(_ clusterConfig) (cloud.Interface, error) {
		return nil, nil
	}
	describeSubnet = func(_ context.Context, _ cloud.Interface, _ string) (*vpc.Subnet, error) {
		return &vpc.Subnet{VPCId: "vpc-test"}, nil
	}
	defer func() {
		getClusterConfig = origGetClusterConfig
		buildCloudClient = origBuildCloudClient
		describeSubnet = origDescribeSubnet
	}()
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(k8s.CCEClient, func() *k8s.K8sCCEClient {
		fake := ccefake.NewSimpleClientset()
		fake.PrependReactor("create", "subnets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("create fail")
		})
		factory := cceInformer.NewSharedInformerFactory(fake, 0)
		return &k8s.K8sCCEClient{Interface: fake, Informers: factory}
	})

	errs := h.validateSubnet(map[string]ccev2.CustomAllocationList{"sbn-1": {}}, field.NewPath("subnets"))
	require.NotEmpty(t, errs)
}

func TestValidateSubnet_NotInitialized(t *testing.T) {
	h := &MutatingPSTSHandler{}
	origVPCID := vpcID
	origVPCClient := vpcClient
	defer func() {
		vpcID = origVPCID
		vpcClient = origVPCClient
	}()

	vpcID = ""
	vpcClient = nil

	errs := h.validateSubnet(map[string]ccev2.CustomAllocationList{"sbn-1": {}}, field.NewPath("subnets"))
	require.NotEmpty(t, errs)
}

func TestValidateSubnet_DescribeError(t *testing.T) {
	h := &MutatingPSTSHandler{}
	origDescribeSubnet := describeSubnet
	origVPCID := vpcID
	origVPCClient := vpcClient
	defer func() {
		describeSubnet = origDescribeSubnet
		vpcID = origVPCID
		vpcClient = origVPCClient
	}()

	ctrl := gomock.NewController(t)
	mockClient := cloudtesting.NewMockInterface(ctrl)
	vpcID = "vpc-test"
	vpcClient = mockClient
	describeSubnet = func(_ context.Context, _ cloud.Interface, _ string) (*vpc.Subnet, error) {
		return nil, errors.New("describe error")
	}

	errs := h.validateSubnet(map[string]ccev2.CustomAllocationList{"sbn-1": {}}, field.NewPath("subnets"))
	require.NotEmpty(t, errs)
}

func TestValidateSubnet_DescribeEmptyVPCID(t *testing.T) {
	h := &MutatingPSTSHandler{}
	origDescribeSubnet := describeSubnet
	origVPCID := vpcID
	origVPCClient := vpcClient
	defer func() {
		describeSubnet = origDescribeSubnet
		vpcID = origVPCID
		vpcClient = origVPCClient
	}()

	ctrl := gomock.NewController(t)
	mockClient := cloudtesting.NewMockInterface(ctrl)
	vpcID = "vpc-test"
	vpcClient = mockClient
	describeSubnet = func(_ context.Context, _ cloud.Interface, _ string) (*vpc.Subnet, error) {
		return &vpc.Subnet{}, nil
	}

	errs := h.validateSubnet(map[string]ccev2.CustomAllocationList{"sbn-1": {}}, field.NewPath("subnets"))
	require.NotEmpty(t, errs)
}

func TestValidateSubnet_VPCMismatch(t *testing.T) {
	h := &MutatingPSTSHandler{}
	origDescribeSubnet := describeSubnet
	origVPCID := vpcID
	origVPCClient := vpcClient
	defer func() {
		describeSubnet = origDescribeSubnet
		vpcID = origVPCID
		vpcClient = origVPCClient
	}()

	ctrl := gomock.NewController(t)
	mockClient := cloudtesting.NewMockInterface(ctrl)
	vpcID = "vpc-test"
	vpcClient = mockClient
	describeSubnet = func(_ context.Context, _ cloud.Interface, _ string) (*vpc.Subnet, error) {
		return &vpc.Subnet{VPCId: "vpc-other"}, nil
	}

	errs := h.validateSubnet(map[string]ccev2.CustomAllocationList{"sbn-1": {}}, field.NewPath("subnets"))
	require.NotEmpty(t, errs)
}

func TestValidateReuseSubnet_InvalidRange(t *testing.T) {
	h := &MutatingPSTSHandler{}
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
		Spec:       ccev1.SubnetSpec{CIDR: "10.0.0.0/24"},
	}
	errs := h.validateReuseSubnet(sbn, ccev2.CustomAllocationList{{Range: []ccev2.CustomIPRange{{Start: "", End: ""}}}}, field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)
}

func TestValidateReuseSubnet_InvalidCIDR(t *testing.T) {
	h := &MutatingPSTSHandler{}
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
		Spec:       ccev1.SubnetSpec{CIDR: "10.0.0.0/24"},
	}
	errs := h.validateReuseSubnet(sbn, ccev2.CustomAllocationList{{CIDRs: []string{"bad"}}}, field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)
}

func TestCidrContains(t *testing.T) {
	_, parent, _ := net.ParseCIDR("10.0.0.0/24")
	_, child, _ := net.ParseCIDR("10.0.0.0/25")
	_, other, _ := net.ParseCIDR("10.0.1.0/24")
	require.True(t, cidrContains(parent, child))
	require.False(t, cidrContains(parent, other))
}

func TestLastIPInCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/30")
	last := lastIPInCIDR(cidr)
	require.Equal(t, "10.0.0.3", last.String())
}

func TestOverlappingCheck(t *testing.T) {
	a := k8sutilnet.BigForIP(net.ParseIP("10.0.0.1"))
	b := k8sutilnet.BigForIP(net.ParseIP("10.0.0.5"))
	c := k8sutilnet.BigForIP(net.ParseIP("10.0.0.4"))
	d := k8sutilnet.BigForIP(net.ParseIP("10.0.0.6"))
	i, j := overlappingCheck([]*big.Int{a, b, c, d})
	require.Equal(t, 0, i)
	require.Equal(t, 1, j)
}

func TestValidatePSTSUpdate_Conflict(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.1": "pod"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.Error(t, err)
}

func TestValidatePSTSUpdate_RejectSwitchElasticToFixedWhenRunning(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			PodMatchedCount: 1,
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		},
	}

	err := h.validatePSTSUpdate(oldObj, newObj)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot change strategy.type")
}

func TestValidatePSTSUpdate_RejectSwitchFixedToElasticWhenRunning(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.2": "pod-a"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		},
	}

	err := h.validatePSTSUpdate(oldObj, newObj)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot change strategy.type")
}

func TestValidatePSTSUpdate_AllowSwitchElasticFixedWhenNotRunning(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		},
	}

	err := h.validatePSTSUpdate(oldObj, newObj)
	require.NoError(t, err)
}

func TestValidatePSTSUpdate_DefaultElasticWhenStrategyNil(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: nil,
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			PodMatchedCount: 1,
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
		},
	}

	err := h.validatePSTSUpdate(oldObj, newObj)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot change strategy.type")
}

func TestEffectiveStrategyType(t *testing.T) {
	require.Equal(t, ccev2.IPAllocTypeElastic, effectiveStrategyType(nil))
	require.Equal(t, ccev2.IPAllocTypeElastic, effectiveStrategyType(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeNil}))
	require.Equal(t, ccev2.IPAllocTypeFixed, effectiveStrategyType(&ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed}))
}

func TestIsElasticFixedSwitch(t *testing.T) {
	require.True(t, isElasticFixedSwitch(ccev2.IPAllocTypeElastic, ccev2.IPAllocTypeFixed))
	require.True(t, isElasticFixedSwitch(ccev2.IPAllocTypeFixed, ccev2.IPAllocTypeElastic))
	require.False(t, isElasticFixedSwitch(ccev2.IPAllocTypeElastic, ccev2.IPAllocTypeElastic))
}

func TestIsPSTSRunning(t *testing.T) {
	require.False(t, isPSTSRunning(nil))

	require.True(t, isPSTSRunning(&ccev2.PodSubnetTopologySpread{
		Status: ccev2.PodSubnetTopologySpreadStatus{
			UnavailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.2": "pod-a"}},
			},
		},
	}))

	require.False(t, isPSTSRunning(&ccev2.PodSubnetTopologySpread{
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{}},
			},
			UnavailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-2": {IPAllocations: map[string]string{}},
			},
		},
	}))
}

func TestHandle_CreatePatch(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, ccev2.AddToScheme(s))
	decoder, err := admission.NewDecoder(s)
	require.NoError(t, err)

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(&MutatingPSTSHandler{}), "validateSubnet",
		func(_ *MutatingPSTSHandler, _ map[string]ccev2.CustomAllocationList, _ *field.Path) field.ErrorList {
			return field.ErrorList{}
		})

	h := &MutatingPSTSHandler{Decoder: decoder}
	obj := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}}},
	}
	raw, _ := json.Marshal(obj)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}, Namespace: "ns"}}

	resp := h.Handle(context.Background(), req)
	require.True(t, resp.Allowed)
	require.True(t, len(resp.Patches) > 0)
}

func TestInjectDecoder(t *testing.T) {
	h := &MutatingPSTSHandler{}
	err := h.InjectDecoder(&admission.Decoder{})
	require.NoError(t, err)
	require.NotNil(t, h.Decoder)
}

func TestHandle_DecodeError(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, ccev2.AddToScheme(s))
	decoder, err := admission.NewDecoder(s)
	require.NoError(t, err)

	h := &MutatingPSTSHandler{Decoder: decoder}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: []byte("not-json")},
	}}

	resp := h.Handle(context.Background(), req)
	require.False(t, resp.Allowed)
	require.Equal(t, int32(400), resp.Result.Code)
}

func TestHandle_UpdateOldObjectUnmarshalError(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, ccev2.AddToScheme(s))
	decoder, err := admission.NewDecoder(s)
	require.NoError(t, err)

	obj := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}}},
	}
	raw, _ := json.Marshal(obj)

	h := &MutatingPSTSHandler{Decoder: decoder}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Object:    runtime.RawExtension{Raw: raw},
		OldObject: runtime.RawExtension{Raw: []byte("{bad-json")},
		Namespace: "ns",
	}}

	resp := h.Handle(context.Background(), req)
	require.False(t, resp.Allowed)
	require.Equal(t, int32(400), resp.Result.Code)
}

func TestHandle_UpdateValidationDenied(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, ccev2.AddToScheme(s))
	decoder, err := admission.NewDecoder(s)
	require.NoError(t, err)

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(&MutatingPSTSHandler{}), "validateSubnet",
		func(_ *MutatingPSTSHandler, _ map[string]ccev2.CustomAllocationList, _ *field.Path) field.ErrorList {
			return field.ErrorList{}
		})
	patches.ApplyPrivateMethod(reflect.TypeOf(&MutatingPSTSHandler{}), "validatePSTSUpdate",
		func(_ *MutatingPSTSHandler, _ *ccev2.PodSubnetTopologySpread, _ *ccev2.PodSubnetTopologySpread) error {
			return errors.New("deny")
		})

	obj := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec:       ccev2.PodSubnetTopologySpreadSpec{Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}}},
	}
	raw, _ := json.Marshal(obj)

	h := &MutatingPSTSHandler{Decoder: decoder}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Object:    runtime.RawExtension{Raw: raw},
		OldObject: runtime.RawExtension{Raw: raw},
		Namespace: "ns",
	}}

	resp := h.Handle(context.Background(), req)
	require.False(t, resp.Allowed)
}

func TestHandle_NoMutation(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, ccev2.AddToScheme(s))
	decoder, err := admission.NewDecoder(s)
	require.NoError(t, err)

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(&MutatingPSTSHandler{}), "validateSubnet",
		func(_ *MutatingPSTSHandler, _ map[string]ccev2.CustomAllocationList, _ *field.Path) field.ErrorList {
			return field.ErrorList{}
		})

	obj := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "p1",
			Namespace:   "ns",
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Name:     "p1",
			Subnets:  map[string]ccev2.CustomAllocationList{"sbn-1": {}},
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
		},
	}
	raw, _ := json.Marshal(obj)

	h := &MutatingPSTSHandler{Decoder: decoder}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: raw},
		Namespace: "ns",
	}}

	resp := h.Handle(context.Background(), req)
	require.True(t, resp.Allowed)
	require.Len(t, resp.Patches, 0)
}

func TestValidatePSTSUpdate_NoRemovals(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{"sbn-1": {}},
		},
	}
	require.NoError(t, h.validatePSTSUpdate(oldObj, newObj))
}

func TestCalculatePSTSRemovals_RemovedSubnet(t *testing.T) {
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
				"sbn-2": {},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
			},
		},
	}
	removedSubnets, _, _ := calculatePSTSRemovals(oldObj, newObj)
	require.Equal(t, []string{"sbn-2"}, removedSubnets)
}

func TestValidatePstsSpec_SelectorErrors(t *testing.T) {
	h := &MutatingPSTSHandler{}
	spec := &ccev2.PodSubnetTopologySpreadSpec{
		Selector: &metav1.LabelSelector{},
		Subnets:  map[string]ccev2.CustomAllocationList{},
	}
	errs := h.validatePstsSpec(spec, field.NewPath("spec"))
	require.NotEmpty(t, errs)
	require.Contains(t, errs.ToAggregate().Error(), "selector")

	spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"bad key": "v"}}
	errs = h.validatePstsSpec(spec, field.NewPath("spec"))
	require.NotEmpty(t, errs)
}

func TestValidateReuseSubnet_WaitTimeout(t *testing.T) {
	h := &MutatingPSTSHandler{}
	origPoll := poll
	poll = func(_ time.Duration, _ time.Duration, _ wait.ConditionFunc) error {
		return errors.New("timeout")
	}
	defer func() { poll = origPoll }()

	sbn := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"}}
	errs := h.validateReuseSubnet(sbn, ccev2.CustomAllocationList{}, field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)
}

func TestValidateReuseSubnet_RangeAndCIDRValidation(t *testing.T) {
	h := &MutatingPSTSHandler{}
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
		Spec: ccev1.SubnetSpec{
			CIDR: "10.0.0.0/24",
		},
	}

	errs := h.validateReuseSubnet(sbn,
		ccev2.CustomAllocationList{{Family: ccev2.IPv6Family}},
		field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)

	sbn.Spec.IPv6CIDR = "2001:db8::/64"
	errs = h.validateReuseSubnet(sbn,
		ccev2.CustomAllocationList{{Range: []ccev2.CustomIPRange{{Start: "10.0.1.1", End: "10.0.1.2"}}}},
		field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)

	errs = h.validateReuseSubnet(sbn,
		ccev2.CustomAllocationList{{Range: []ccev2.CustomIPRange{{Start: "10.0.0.3", End: "10.0.0.2"}}}},
		field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)

	errs = h.validateReuseSubnet(sbn,
		ccev2.CustomAllocationList{{CIDRs: []string{"10.0.1.0/24"}}},
		field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)

	errs = h.validateReuseSubnet(sbn,
		ccev2.CustomAllocationList{{Family: ccev2.IPv6Family, CIDRs: []string{"2001:db8::/64"}}},
		field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)
}

func TestValidateReuseSubnet_OverlapAndCIDRConversion(t *testing.T) {
	h := &MutatingPSTSHandler{}
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"},
		Spec:       ccev1.SubnetSpec{CIDR: "10.0.0.0/24"},
	}

	errs := h.validateReuseSubnet(sbn,
		ccev2.CustomAllocationList{{Range: []ccev2.CustomIPRange{
			{Start: "10.0.0.1", End: "10.0.0.10"},
			{Start: "10.0.0.5", End: "10.0.0.6"},
		}}},
		field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(k8sutilnet.BigForIP, func(_ net.IP) *big.Int { return nil })
	errs = h.validateReuseSubnet(sbn,
		ccev2.CustomAllocationList{{CIDRs: []string{"10.0.0.0/24"}}},
		field.NewPath("subnets"), field.ErrorList{}, &fakeSubnetClient{sbn: sbn})
	require.NotEmpty(t, errs)
}

func TestValidatePSTSUpdate_RemovedRangeConflictAvailable(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{
					Range: []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.3"}},
				}},
			},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.2": "pod-a"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
			},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.Error(t, err)
}

func TestValidatePSTSUpdate_RangeExpansionAllowed(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{
					Range: []ccev2.CustomIPRange{{Start: "13.0.0.1", End: "13.0.0.4"}},
				}},
			},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"13.0.0.2": "pod-a"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{
					Range: []ccev2.CustomIPRange{{Start: "13.0.0.1", End: "13.0.0.5"}},
				}},
			},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.NoError(t, err)
}

func TestValidatePSTSUpdate_RemovedRangeNoConflictUnavailable(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{
					Range: []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.3"}},
				}},
			},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			UnavailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.9": "pod-b"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
			},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.NoError(t, err)
}

func TestValidatePSTSUpdate_RemovedCIDRConflictAvailable(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{CIDRs: []string{"10.0.0.0/24"}}},
			},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.8": "pod-c"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeFixed},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
			},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.Error(t, err)
}

func TestValidatePSTSUpdate_ElasticAllowRemovedRangeWithInUseIP(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{
					Range: []ccev2.CustomIPRange{{Start: "10.0.0.1", End: "10.0.0.3"}},
				}},
			},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.2": "pod-a"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
			},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.NoError(t, err)
}

func TestValidatePSTSUpdate_ElasticAllowRemovedCIDRWithInUseIP(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{CIDRs: []string{"10.0.0.0/24"}}},
			},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.8": "pod-c"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Strategy: &ccev2.IPAllocationStrategy{Type: ccev2.IPAllocTypeElastic},
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
			},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.NoError(t, err)
}

func TestValidateSubnetStackCompatibility(t *testing.T) {
	errs := validateSubnetStackCompatibility(false, &vpc.Subnet{Cidr: "10.0.0.0/24"}, field.NewPath("spec", "subnets"), "sbn-1")
	require.Empty(t, errs)

	errs = validateSubnetStackCompatibility(true, &vpc.Subnet{Cidr: "10.0.0.0/24", Ipv6Cidr: "2001:db8::/64"}, field.NewPath("spec", "subnets"), "sbn-1")
	require.Empty(t, errs)

	errs = validateSubnetStackCompatibility(true, &vpc.Subnet{Cidr: "10.0.0.0/24", Ipv6Cidr: ""}, field.NewPath("spec", "subnets"), "sbn-1")
	require.NotEmpty(t, errs)
	require.Contains(t, errs.ToAggregate().Error(), "dual-stack cluster requires dual-stack subnet")
}

func TestValidatePSTSUpdate_RemovedCIDRInvalidCIDR(t *testing.T) {
	h := &MutatingPSTSHandler{}
	oldObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {{CIDRs: []string{"bad"}}},
			},
		},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {IPAllocations: map[string]string{"10.0.0.1": "pod-d"}},
			},
		},
	}
	newObj := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-1": {},
			},
		},
	}
	err := h.validatePSTSUpdate(oldObj, newObj)
	require.NoError(t, err)
}

func TestGetClusterConfigFromFile_Success(t *testing.T) {
	tmp, err := os.CreateTemp("", "cluster-config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString("bce-cloud-vpc-id: vpc-test\nbce-cloud-region: bj\ncce-cluster-id: cce-test\n")
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	cfg, err := getClusterConfigFromFile(tmp.Name())
	require.NoError(t, err)
	require.Equal(t, "vpc-test", cfg.VPCID)
	require.Equal(t, "bj", cfg.Region)
	require.Equal(t, "cce-test", cfg.ClusterID)
}

func TestGetClusterConfigFromFile_ParseError(t *testing.T) {
	tmp, err := os.CreateTemp("", "cluster-config-bad-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(":::bad-yaml")
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	_, err = getClusterConfigFromFile(tmp.Name())
	require.Error(t, err)
}

func TestGetClusterConfigFromConfigMap_Success(t *testing.T) {
	fake := k8sfake.NewSimpleClientset()
	origGetK8sClient := getK8sClient
	getK8sClient = func() *k8s.K8sClient {
		return &k8s.K8sClient{Interface: fake}
	}
	defer func() { getK8sClient = origGetK8sClient }()

	_, err := fake.CoreV1().ConfigMaps(metav1.NamespaceSystem).Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: clusterConfigMapName, Namespace: metav1.NamespaceSystem},
		Data: map[string]string{
			clusterConfigKey: "bce-cloud-vpc-id: vpc-test\nbce-cloud-region: bj\ncce-cluster-id: cce-test\n",
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	cfg, err := getClusterConfigFromConfigMap(metav1.NamespaceSystem)
	require.NoError(t, err)
	require.Equal(t, "vpc-test", cfg.VPCID)
}

func TestGetClusterConfigFromConfigMap_MissingKey(t *testing.T) {
	fake := k8sfake.NewSimpleClientset()
	origGetK8sClient := getK8sClient
	getK8sClient = func() *k8s.K8sClient {
		return &k8s.K8sClient{Interface: fake}
	}
	defer func() { getK8sClient = origGetK8sClient }()

	_, err := fake.CoreV1().ConfigMaps(metav1.NamespaceSystem).Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: clusterConfigMapName, Namespace: metav1.NamespaceSystem},
		Data:       map[string]string{},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = getClusterConfigFromConfigMap(metav1.NamespaceSystem)
	require.Error(t, err)
}

func TestGetClusterConfigFromConfigMap_NotFound(t *testing.T) {
	fake := k8sfake.NewSimpleClientset()
	origGetK8sClient := getK8sClient
	getK8sClient = func() *k8s.K8sClient {
		return &k8s.K8sClient{Interface: fake}
	}
	defer func() { getK8sClient = origGetK8sClient }()

	_, err := getClusterConfigFromConfigMap(metav1.NamespaceSystem)
	require.Error(t, err)
}

func TestParseClusterConfig_EnvOverride(t *testing.T) {
	t.Setenv(envKey("bce-cloud-vpc-id"), "vpc-override")
	t.Setenv(envKey("bce-cloud-region"), "gz")
	t.Setenv(envKey("cce-cluster-id"), "cce-override")
	t.Setenv(envKey("bce-cloud-access-key"), "ak-override")
	t.Setenv(envKey("bce-cloud-secure-key"), "sk-override")
	t.Setenv(envKey("bce-cloud-host"), "host-override")
	t.Setenv(envKey("bce-cloud-force-via-cce-gateway"), "true")
	t.Setenv(envKey("enable-ipv6"), "true")

	cfg, err := parseClusterConfig([]byte("bce-cloud-vpc-id: vpc-test\nbce-cloud-region: bj\ncce-cluster-id: cce-test\n"), "test")
	require.NoError(t, err)
	require.Equal(t, "vpc-override", cfg.VPCID)
	require.Equal(t, "gz", cfg.Region)
	require.Equal(t, "cce-override", cfg.ClusterID)
	require.Equal(t, "ak-override", cfg.AccessKey)
	require.Equal(t, "sk-override", cfg.SecretKey)
	require.Equal(t, "host-override", cfg.BaseHost)
	require.True(t, cfg.ForceViaGateway)
	require.True(t, cfg.EnableIPv6)
}

func TestDefaultGetClusterConfig_FallbackToConfigMap(t *testing.T) {
	origReadConfigFile := readConfigFile
	origReadConfigMap := readConfigMap
	readConfigFile = func(_ string) (clusterConfig, error) {
		return clusterConfig{}, os.ErrNotExist
	}
	readConfigMap = func(_ string) (clusterConfig, error) {
		return clusterConfig{VPCID: "vpc-from-cm"}, nil
	}
	defer func() {
		readConfigFile = origReadConfigFile
		readConfigMap = origReadConfigMap
	}()

	cfg, err := defaultGetClusterConfig()
	require.NoError(t, err)
	require.Equal(t, "vpc-from-cm", cfg.VPCID)
}

func TestDefaultGetClusterConfig_FilePreferred(t *testing.T) {
	origReadConfigFile := readConfigFile
	origReadConfigMap := readConfigMap
	readConfigFile = func(_ string) (clusterConfig, error) {
		return clusterConfig{VPCID: "vpc-from-file"}, nil
	}
	readConfigMap = func(_ string) (clusterConfig, error) {
		return clusterConfig{VPCID: "vpc-from-cm"}, nil
	}
	defer func() {
		readConfigFile = origReadConfigFile
		readConfigMap = origReadConfigMap
	}()

	cfg, err := defaultGetClusterConfig()
	require.NoError(t, err)
	require.Equal(t, "vpc-from-file", cfg.VPCID)
}

func TestDefaultGetClusterConfig_FileError(t *testing.T) {
	origReadConfigFile := readConfigFile
	readConfigFile = func(_ string) (clusterConfig, error) {
		return clusterConfig{}, os.ErrPermission
	}
	defer func() { readConfigFile = origReadConfigFile }()

	_, err := defaultGetClusterConfig()
	require.Error(t, err)
}

func TestInit_SetsGlobals(t *testing.T) {
	origGetClusterConfig := getClusterConfig
	origBuildCloudClient := buildCloudClient
	origVPCID := vpcID
	origVPCClient := vpcClient
	defer func() {
		getClusterConfig = origGetClusterConfig
		buildCloudClient = origBuildCloudClient
		vpcID = origVPCID
		vpcClient = origVPCClient
	}()

	ctrl := gomock.NewController(t)
	mockClient := cloudtesting.NewMockInterface(ctrl)

	getClusterConfig = func() (clusterConfig, error) {
		return clusterConfig{VPCID: "vpc-test", Region: "bj", ClusterID: "cce-test"}, nil
	}
	buildCloudClient = func(_ clusterConfig) (cloud.Interface, error) {
		return mockClient, nil
	}

	err := Init()
	require.NoError(t, err)
	require.Equal(t, "vpc-test", vpcID)
	require.Equal(t, mockClient, vpcClient)
}

func TestInit_GetClusterConfigError(t *testing.T) {
	origGetClusterConfig := getClusterConfig
	origBuildCloudClient := buildCloudClient
	defer func() {
		getClusterConfig = origGetClusterConfig
		buildCloudClient = origBuildCloudClient
	}()

	getClusterConfig = func() (clusterConfig, error) {
		return clusterConfig{}, errors.New("boom")
	}
	buildCloudClient = func(_ clusterConfig) (cloud.Interface, error) {
		return nil, errors.New("should not be called")
	}

	err := Init()
	require.Error(t, err)
}

func TestInit_BuildCloudClientError(t *testing.T) {
	origGetClusterConfig := getClusterConfig
	origBuildCloudClient := buildCloudClient
	defer func() {
		getClusterConfig = origGetClusterConfig
		buildCloudClient = origBuildCloudClient
	}()

	getClusterConfig = func() (clusterConfig, error) {
		return clusterConfig{VPCID: "vpc-test", Region: "bj", ClusterID: "cce-test"}, nil
	}
	buildCloudClient = func(_ clusterConfig) (cloud.Interface, error) {
		return nil, errors.New("build error")
	}

	err := Init()
	require.Error(t, err)
}

func TestInit_ErrorOnEmptyVPCID(t *testing.T) {
	origGetClusterConfig := getClusterConfig
	origBuildCloudClient := buildCloudClient
	defer func() {
		getClusterConfig = origGetClusterConfig
		buildCloudClient = origBuildCloudClient
	}()

	getClusterConfig = func() (clusterConfig, error) {
		return clusterConfig{}, nil
	}
	buildCloudClient = func(_ clusterConfig) (cloud.Interface, error) {
		return nil, errors.New("should not be called")
	}

	err := Init()
	require.Error(t, err)
}

func TestEnvKey(t *testing.T) {
	require.Equal(t, "CCE_BCE_CLOUD_VPC_ID", envKey("bce-cloud-vpc-id"))
	require.Equal(t, "CCE_FOO", envKey("foo"))
}

func TestGetConfigString(t *testing.T) {
	require.Equal(t, "", getConfigString(nil, "k"))
	require.Equal(t, "", getConfigString(map[string]interface{}{}, "k"))
	require.Equal(t, "", getConfigString(map[string]interface{}{"k": nil}, "k"))
	require.Equal(t, "v", getConfigString(map[string]interface{}{"k": "v"}, "k"))
	require.Equal(t, "123", getConfigString(map[string]interface{}{"k": 123}, "k"))
}

func TestGetConfigBool(t *testing.T) {
	require.False(t, getConfigBool(nil, "k"))
	require.False(t, getConfigBool(map[string]interface{}{}, "k"))
	require.False(t, getConfigBool(map[string]interface{}{"k": nil}, "k"))
	require.True(t, getConfigBool(map[string]interface{}{"k": true}, "k"))
	require.True(t, getConfigBool(map[string]interface{}{"k": "true"}, "k"))
	require.False(t, getConfigBool(map[string]interface{}{"k": "not-bool"}, "k"))
	require.False(t, getConfigBool(map[string]interface{}{"k": 1}, "k"))
}

func TestDefaultBuildCloudClient_RegionRequired(t *testing.T) {
	cfg := clusterConfig{VPCID: "vpc-test", ClusterID: "cce-test"}
	_, err := defaultBuildCloudClient(cfg)
	require.Error(t, err)
}

func TestDefaultBuildCloudClient_CredentialRequired(t *testing.T) {
	cfg := clusterConfig{Region: "bj"}
	_, err := defaultBuildCloudClient(cfg)
	require.Error(t, err)
}

func TestDefaultBuildCloudClient_NewClientError(t *testing.T) {
	origNewCloudClient := newCloudClient
	origNewFlowControl := newFlowControl
	defer func() {
		newCloudClient = origNewCloudClient
		newFlowControl = origNewFlowControl
	}()

	newCloudClient = func(_ string, _ string, _ string, _ string, _ bool, _ kubernetes.Interface, _ bool, _ time.Duration) (cloud.Interface, error) {
		return nil, errors.New("new client error")
	}
	newFlowControl = func(c cloud.Interface, _ float64, _ int, _ time.Duration) (cloud.Interface, error) {
		return c, nil
	}

	cfg := clusterConfig{Region: "bj", ClusterID: "cce-test"}
	_, err := defaultBuildCloudClient(cfg)
	require.Error(t, err)
}

func TestDefaultBuildCloudClient_FlowControlError(t *testing.T) {
	origNewCloudClient := newCloudClient
	origNewFlowControl := newFlowControl
	defer func() {
		newCloudClient = origNewCloudClient
		newFlowControl = origNewFlowControl
	}()

	ctrl := gomock.NewController(t)
	mockClient := cloudtesting.NewMockInterface(ctrl)

	newCloudClient = func(_ string, _ string, _ string, _ string, _ bool, _ kubernetes.Interface, _ bool, _ time.Duration) (cloud.Interface, error) {
		return mockClient, nil
	}
	newFlowControl = func(_ cloud.Interface, _ float64, _ int, _ time.Duration) (cloud.Interface, error) {
		return nil, errors.New("flow control error")
	}

	cfg := clusterConfig{Region: "bj", ClusterID: "cce-test"}
	_, err := defaultBuildCloudClient(cfg)
	require.Error(t, err)
}

func TestDefaultBuildCloudClient_Success(t *testing.T) {
	origNewCloudClient := newCloudClient
	origNewFlowControl := newFlowControl
	defer func() {
		newCloudClient = origNewCloudClient
		newFlowControl = origNewFlowControl
	}()

	ctrl := gomock.NewController(t)
	mockClient := cloudtesting.NewMockInterface(ctrl)

	newCloudClient = func(_ string, _ string, _ string, _ string, _ bool, _ kubernetes.Interface, _ bool, _ time.Duration) (cloud.Interface, error) {
		return mockClient, nil
	}
	newFlowControl = func(c cloud.Interface, _ float64, _ int, _ time.Duration) (cloud.Interface, error) {
		return c, nil
	}

	cfg := clusterConfig{
		Region:    "bj",
		VPCID:     "vpc-test",
		ClusterID: "cce-test",
		BaseHost:  "host-override",
	}
	t.Setenv(ccegateway.EndpointOverrideEnv, "")
	client, err := defaultBuildCloudClient(cfg)
	require.NoError(t, err)
	require.Equal(t, mockClient, client)
	require.Equal(t, "host-override", os.Getenv(ccegateway.EndpointOverrideEnv))
}
