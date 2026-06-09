package endpoint

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccelister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
)

const pstsConflictBadRequest = "[Code: BadRequest; Message: Bad request parameters or illegal request.; RequestId: req-id]"

type fakeDirectEndpointOperation struct {
	filterFn   func([]string, int) []*bcesync.BorrowedSubnet
	allocateFn func(context.Context, *DirectIPAction) error
	deleteFn   func(context.Context, *DirectIPAction) error
}

func (f *fakeDirectEndpointOperation) FilterAvailableSubnetIds(subnetIds []string, count int) []*bcesync.BorrowedSubnet {
	if f.filterFn != nil {
		return f.filterFn(subnetIds, count)
	}
	return nil
}

func (f *fakeDirectEndpointOperation) AllocateIP(ctx context.Context, action *DirectIPAction) error {
	if f.allocateFn != nil {
		return f.allocateFn(ctx, action)
	}
	return nil
}

func (f *fakeDirectEndpointOperation) DeleteIP(ctx context.Context, allocation *DirectIPAction) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, allocation)
	}
	return nil
}

type fakeDirectIPAllocator struct {
	op  DirectEndpointOperation
	err error
}

func (f *fakeDirectIPAllocator) ResyncPool(ctx context.Context, scopedLog *logrus.Entry) (map[string]ipamTypes.AllocationMap, error) {
	return nil, nil
}

func (f *fakeDirectIPAllocator) NodeEndpoint(cep *ccev2.CCEEndpoint) (DirectEndpointOperation, error) {
	return f.op, f.err
}

type fakePSTSLister struct {
	psts *ccev2.PodSubnetTopologySpread
	err  error
}

func (f *fakePSTSLister) List(selector labels.Selector) ([]*ccev2.PodSubnetTopologySpread, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.psts == nil {
		return nil, nil
	}
	return []*ccev2.PodSubnetTopologySpread{f.psts}, nil
}

func (f *fakePSTSLister) PodSubnetTopologySpreads(namespace string) ccelister.PodSubnetTopologySpreadNamespaceLister {
	return &fakePSTSNamespaceLister{namespace: namespace, psts: f.psts, err: f.err}
}

type fakePSTSNamespaceLister struct {
	namespace string
	psts      *ccev2.PodSubnetTopologySpread
	err       error
}

func (f *fakePSTSNamespaceLister) List(selector labels.Selector) ([]*ccev2.PodSubnetTopologySpread, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.psts == nil || f.psts.Namespace != f.namespace {
		return nil, nil
	}
	return []*ccev2.PodSubnetTopologySpread{f.psts}, nil
}

func (f *fakePSTSNamespaceLister) Get(name string) (*ccev2.PodSubnetTopologySpread, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.psts == nil || f.psts.Namespace != f.namespace || f.psts.Name != name {
		return nil, k8serrors.NewNotFound(ccev2.Resource("podsubnettopologyspread"), name)
	}
	return f.psts, nil
}

func TestSeparateSubnetsByRangeAndHasCustomAllocation(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-range": {{
					Family: ccev2.IPv4Family,
					Range: []ccev2.CustomIPRange{
						{Start: "10.0.0.1", End: "10.0.0.2"},
					},
				}},
				"sbn-norange": {{
					Family: ccev2.IPv4Family,
				}},
			},
		},
	}
	subnets := []*ccev1.Subnet{
		{ObjectMeta: metav1.ObjectMeta{Name: "sbn-range"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "sbn-norange"}},
	}

	rangeSubnets, noRangeSubnets := separateSubnetsByRange(psts, subnets)
	require.Len(t, rangeSubnets, 1)
	require.Len(t, noRangeSubnets, 1)
	require.True(t, hasCustomAllocationForSubnet(psts, "sbn-range"))
	require.False(t, hasCustomAllocationForSubnet(psts, "sbn-norange"))
}

func TestFilterOutSubnetByName(t *testing.T) {
	subnets := []*ccev1.Subnet{
		{ObjectMeta: metav1.ObjectMeta{Name: "sbn-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "sbn-b"}},
		nil,
	}
	filtered := filterOutSubnetByName(subnets, "sbn-a")
	require.Len(t, filtered, 1)
	require.Equal(t, "sbn-b", filtered[0].Name)
}

func TestIsIPConflictError(t *testing.T) {
	require.False(t, isIPConflictError(nil))
	require.True(t, isIPConflictError(errors.New("PrivateIpInUseException")))
	require.True(t, isIPConflictError(errors.New(pstsConflictBadRequest)))
	require.False(t, isIPConflictError(errors.New("[Code: BadRequest; Message: invalid parameter.]")))
	require.False(t, isIPConflictError(errors.New("other error")))
}

func TestFilterAvailableSubnet(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-1": {},
			},
		},
	}
	log := logrus.NewEntry(logrus.New())

	opEmpty := &fakeDirectEndpointOperation{
		filterFn: func(subnetIds []string, count int) []*bcesync.BorrowedSubnet {
			return nil
		},
	}
	_, err := filterAvailableSubnet(psts, log, opEmpty)
	require.Error(t, err)

	opOk := &fakeDirectEndpointOperation{
		filterFn: func(subnetIds []string, count int) []*bcesync.BorrowedSubnet {
			return []*bcesync.BorrowedSubnet{{Subnet: &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"}}}}
		},
	}
	subnets, err := filterAvailableSubnet(psts, log, opOk)
	require.NoError(t, err)
	require.Len(t, subnets, 1)
	require.Equal(t, "sbn-1", subnets[0].Name)
}

func TestLocalAllocatorAllocateFromLocalPool(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}

	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}
	allocator := &localAllocator{
		localPool: lp,
		subnets:   []*ccev1.Subnet{subnet},
		psts:      psts,
		log:       logrus.NewEntry(logrus.New()),
	}

	// no pool, should return empty
	ips, release := allocator.allocateFromLocalPool(ccev2.IPv4Family, 1)
	require.Empty(t, ips)
	require.Empty(t, release)

	// add pool and allocate
	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	ips, release = allocator.allocateFromLocalPool(ccev2.IPv4Family, 1)
	require.Len(t, ips, 1)
	require.Len(t, release, 1)
	require.Equal(t, "10.0.0.1", ips["sbn"][0].String())
}

func TestLocalAllocatorAllocateNextIPv6Enabled(t *testing.T) {
	oldIPv6 := operatorOption.Config.EnableIPv6
	operatorOption.Config.EnableIPv6 = true
	defer func() { operatorOption.Config.EnableIPv6 = oldIPv6 }()

	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool4 := newExpectedPool("sbn", "4", nil)
	pool4.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool4.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool4.poolID] = pool4

	pool6 := newExpectedPool("sbn", "6", nil)
	pool6.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "6"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool6.addRange(net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::1"))
	lp.pstsLocalPool[pool6.poolID] = pool6

	allocator := &localAllocator{
		localPool: lp,
		subnets:   []*ccev1.Subnet{subnet},
		psts:      psts,
		log:       logrus.NewEntry(logrus.New()),
	}

	ipv4, ipv6, release := allocator.allocateNext()
	require.NotNil(t, ipv4)
	require.NotNil(t, ipv6)
	require.Len(t, release, 2)
}

func TestAllocateFromNoRangeSubnets(t *testing.T) {
	provider := &pstsAllocatorProvider{}
	status := &ccev2.EndpointStatus{Networking: &ccev2.EndpointNetworking{}}
	action := &DirectIPAction{Owner: "ns/pod"}
	log := logrus.NewEntry(logrus.New())

	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			action.Addressing = ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: action.SubnetID, Family: ccev2.IPv4Family}}
			return nil
		},
	}

	err := provider.allocateFromNoRangeSubnets(context.Background(), "psts", nil, op, action, status, "pod-uid", log)
	require.Error(t, err)

	subnets := []*ccev1.Subnet{{ObjectMeta: metav1.ObjectMeta{Name: "sbn-1"}}}
	err = provider.allocateFromNoRangeSubnets(context.Background(), "psts", subnets, op, action, status, "pod-uid", log)
	require.NoError(t, err)
	require.Len(t, status.Networking.Addressing, 1)
	require.Equal(t, "10.0.0.1", status.Networking.Addressing[0].IP)
	require.NotEmpty(t, action.ClientToken)
}

func TestTryAllocateFromRangeSubnetsSuccess(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return nil
		},
	}
	action := &DirectIPAction{}
	log := logrus.NewEntry(logrus.New())

	allocated, release, err := provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op, action, "pod-uid", log)
	require.NoError(t, err)
	require.True(t, allocated)
	require.NotEmpty(t, release)
	require.Len(t, action.Addressing, 1)
	require.Equal(t, "10.0.0.1", action.Addressing[0].IP)
}

func TestTryAllocateFromRangeSubnetsConflict(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return errors.New("PrivateIpInUseException")
		},
	}
	action := &DirectIPAction{}
	log := logrus.NewEntry(logrus.New())

	allocated, _, err := provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op, action, "pod-uid", log)
	require.False(t, allocated)
	require.NoError(t, err)
}

func TestTryAllocateFromRangeSubnetsNonConflictError(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return errors.New("other error")
		},
	}
	action := &DirectIPAction{}
	log := logrus.NewEntry(logrus.New())

	allocated, _, err := provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op, action, "pod-uid", log)
	require.False(t, allocated)
	require.Error(t, err)
}

func TestTryAllocateFromRangeSubnetsBadRequestAsConflict(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return errors.New(pstsConflictBadRequest)
		},
	}
	action := &DirectIPAction{}
	log := logrus.NewEntry(logrus.New())

	allocated, _, err := provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op, action, "pod-uid", log)
	require.False(t, allocated)
	require.NoError(t, err)
}

func TestTryAllocateFromRangeSubnetsBadRequestConflictCooldown(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))
	frozenNow := time.Date(2026, 3, 17, 9, 0, 0, 0, time.UTC)
	pool.nowFunc = func() time.Time { return frozenNow }
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	var firstRound []string
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			ip := action.Addressing[0].IP
			firstRound = append(firstRound, ip)
			if ip == "10.0.0.1" {
				return errors.New(pstsConflictBadRequest)
			}
			return nil
		},
	}
	action := &DirectIPAction{}
	log := logrus.NewEntry(logrus.New())

	allocated, release, err := provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op, action, "pod-uid-1", log)
	require.NoError(t, err)
	require.True(t, allocated)
	require.Len(t, firstRound, 2)
	require.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, firstRound)
	require.Len(t, release, 1)
	require.Equal(t, "10.0.0.2", action.Addressing[0].IP)

	for _, releaseFunc := range release {
		releaseFunc()
	}

	var secondRound []string
	op2 := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			secondRound = append(secondRound, action.Addressing[0].IP)
			return nil
		},
	}
	action2 := &DirectIPAction{}
	allocated, _, err = provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op2, action2, "pod-uid-1", log)
	require.NoError(t, err)
	require.True(t, allocated)
	require.Equal(t, []string{"10.0.0.2"}, secondRound)
	require.Equal(t, "10.0.0.2", action2.Addressing[0].IP)
}

func TestTryAllocateFromRangeSubnetsNonConflictErrorDoesNotMutateOriginalAction(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	origNodeSelector := []corev1.NodeSelectorRequirement{
		{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-a"}},
	}
	action := &DirectIPAction{
		NodeName:                "node-a",
		Owner:                   "ns/pod",
		SubnetID:                "orig-subnet",
		Interface:               "eth-orig",
		NodeSelectorRequirement: origNodeSelector,
	}
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, trial *DirectIPAction) error {
			require.NotSame(t, action, trial)
			trial.SubnetID = "trial-subnet"
			trial.Interface = "eth-trial"
			trial.NodeSelectorRequirement[0].Key = "mutated-key"
			trial.NodeSelectorRequirement = append(trial.NodeSelectorRequirement, corev1.NodeSelectorRequirement{
				Key: "kubernetes.io/hostname", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-b"},
			})
			return errors.New("other error")
		},
	}
	log := logrus.NewEntry(logrus.New())

	allocated, _, err := provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op, action, "pod-uid", log)
	require.False(t, allocated)
	require.Error(t, err)
	require.Equal(t, "orig-subnet", action.SubnetID)
	require.Equal(t, "eth-orig", action.Interface)
	require.Len(t, action.NodeSelectorRequirement, 1)
	require.Equal(t, "topology.kubernetes.io/zone", action.NodeSelectorRequirement[0].Key)
}

func TestTryAllocateFromRangeSubnetsExhaustedDoesNotMutateAction(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	// Empty range pool: allocator returns nil immediately and caller should fallback.
	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return nil
		},
	}

	originalAddressing := ccev2.AddressPairList{
		&ccev2.AddressPair{IP: "192.168.0.1", Subnet: "orig", Family: ccev2.IPv4Family},
	}
	action := &DirectIPAction{
		Addressing: originalAddressing,
		SubnetID:   "orig",
		Interface:  "eth-orig",
	}
	log := logrus.NewEntry(logrus.New())

	allocated, release, err := provider.tryAllocateFromRangeSubnets(context.Background(), psts, []*ccev1.Subnet{subnet}, op, action, "pod-uid", log)
	require.NoError(t, err)
	require.False(t, allocated)
	require.Nil(t, release)
	require.Equal(t, "orig", action.SubnetID)
	require.Equal(t, "eth-orig", action.Interface)
	require.Len(t, action.Addressing, 1)
	require.Equal(t, "192.168.0.1", action.Addressing[0].IP)
}

func TestAllocateNewIP(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn": {},
			},
		},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn": {{
					Family: ccev2.IPv4Family,
					Range: []ccev2.CustomIPRange{
						{Start: "10.0.0.1", End: "10.0.0.1"},
					},
				}},
			},
		},
	}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	status := &ccev2.EndpointStatus{Networking: &ccev2.EndpointNetworking{}}
	action := &DirectIPAction{}
	log := logrus.NewEntry(logrus.New())

	op := &fakeDirectEndpointOperation{
		filterFn: func(subnetIds []string, count int) []*bcesync.BorrowedSubnet {
			return []*bcesync.BorrowedSubnet{{Subnet: subnet}}
		},
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return nil
		},
	}

	err := provider.allocateNewIP(context.Background(), psts, op, action, status, "pod-uid", log)
	require.NoError(t, err)
	require.Len(t, status.Networking.Addressing, 1)

	// range exhausted and no fallback
	status2 := &ccev2.EndpointStatus{Networking: &ccev2.EndpointNetworking{}}
	err = provider.allocateNewIP(context.Background(), psts, op, action, status2, "pod-uid", log)
	require.Error(t, err)
}

func TestAllocateNewIPNoRangeFallback(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn": {},
			},
		},
	}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn"}}

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: newLocalPool()}}
	status := &ccev2.EndpointStatus{Networking: &ccev2.EndpointNetworking{}}
	action := &DirectIPAction{}
	log := logrus.NewEntry(logrus.New())

	op := &fakeDirectEndpointOperation{
		filterFn: func(subnetIds []string, count int) []*bcesync.BorrowedSubnet {
			return []*bcesync.BorrowedSubnet{{Subnet: subnet}}
		},
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			action.Addressing = ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family}}
			return nil
		},
	}

	err := provider.allocateNewIP(context.Background(), psts, op, action, status, "pod-uid", log)
	require.NoError(t, err)
	require.Len(t, status.Networking.Addressing, 1)
}

func TestAllocateNewIPFallbackWhenRangeSubnetHasNoMoreIP(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
		Status: ccev2.PodSubnetTopologySpreadStatus{
			AvailableSubnets: map[string]ccev2.SubnetPodStatus{
				"sbn-range":   {},
				"sbn-norange": {},
			},
		},
		Spec: ccev2.PodSubnetTopologySpreadSpec{
			Subnets: map[string]ccev2.CustomAllocationList{
				"sbn-range": {{
					Family: ccev2.IPv4Family,
					Range: []ccev2.CustomIPRange{
						{Start: "10.0.0.1", End: "10.0.0.1"},
					},
				}},
				"sbn-norange": {},
			},
		},
	}

	rangeSubnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-range"}}
	noRangeSubnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-norange"}}

	pool := newExpectedPool("sbn-range", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn-range", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	status := &ccev2.EndpointStatus{Networking: &ccev2.EndpointNetworking{}}
	action := &DirectIPAction{Owner: "ns/pod"}
	log := logrus.NewEntry(logrus.New())

	op := &fakeDirectEndpointOperation{
		filterFn: func(subnetIds []string, count int) []*bcesync.BorrowedSubnet {
			return []*bcesync.BorrowedSubnet{
				{Subnet: rangeSubnet},
				{Subnet: noRangeSubnet},
			}
		},
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			if action.SubnetID == "sbn-range" {
				return errors.New("SubnetHasNoMoreIpException")
			}
			action.Addressing = ccev2.AddressPairList{
				&ccev2.AddressPair{IP: "10.0.1.1", Subnet: action.SubnetID, Family: ccev2.IPv4Family},
			}
			return nil
		},
	}

	err := provider.allocateNewIP(context.Background(), psts, op, action, status, "pod-uid", log)
	require.NoError(t, err)
	require.Len(t, status.Networking.Addressing, 1)
	require.Equal(t, "sbn-norange", status.Networking.Addressing[0].Subnet)
	require.Equal(t, "10.0.1.1", status.Networking.Addressing[0].IP)
}

func TestTryAllocateFromRangeSubnetsSkipSubnetHasNoMoreIP(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}

	subnetA := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-a"}}
	subnetB := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-b"}}

	poolA := newExpectedPool("sbn-a", "4", nil)
	poolA.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn-a", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	poolA.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[poolA.poolID] = poolA

	poolB := newExpectedPool("sbn-b", "4", nil)
	poolB.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn-b", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	poolB.addRange(net.ParseIP("10.0.1.1"), net.ParseIP("10.0.1.1"))
	lp.pstsLocalPool[poolB.poolID] = poolB

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	action := &DirectIPAction{Owner: "ns/pod"}
	log := logrus.NewEntry(logrus.New())

	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			if action.SubnetID == "sbn-a" {
				return errors.New("SubnetHasNoMoreIpException")
			}
			return nil
		},
	}

	allocated, release, err := provider.tryAllocateFromRangeSubnets(
		context.Background(),
		psts,
		[]*ccev1.Subnet{subnetA, subnetB},
		op,
		action,
		"pod-uid",
		log,
	)
	require.NoError(t, err)
	require.True(t, allocated)
	require.NotEmpty(t, release)
	require.Len(t, action.Addressing, 1)
	require.Equal(t, "sbn-b", action.SubnetID)
	require.Equal(t, "10.0.1.1", action.Addressing[0].IP)
}

func TestTryAllocateFromRangeSubnetsNoMoreIPReturnsFallbackImmediately(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"}}
	subnet := &ccev1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "sbn-a"}}

	pool := newExpectedPool("sbn-a", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "psts", "sbn-a", "4"), "ns", "psts", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	provider := &pstsAllocatorProvider{EndpointManager: &EndpointManager{localPool: lp}}
	action := &DirectIPAction{Owner: "ns/pod"}
	log := logrus.NewEntry(logrus.New())

	callCount := 0
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			callCount++
			return errors.New("SubnetHasNoMoreIpException")
		},
	}

	allocated, release, err := provider.tryAllocateFromRangeSubnets(
		context.Background(),
		psts,
		[]*ccev1.Subnet{subnet},
		op,
		action,
		"pod-uid",
		log,
	)
	require.NoError(t, err)
	require.False(t, allocated)
	require.Nil(t, release)
	require.Equal(t, 1, callCount)
}

func TestReuseExistingIP(t *testing.T) {
	status := &ccev2.EndpointStatus{
		Networking: &ccev2.EndpointNetworking{
			Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family}},
		},
	}
	action := &DirectIPAction{Owner: "default/pod"}
	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return nil
		},
	}
	log := logrus.NewEntry(logrus.New())

	provider := &pstsAllocatorProvider{}
	err := provider.reuseExistingIP(context.Background(), op, action, status, "psts", "pod-uid", log)
	require.NoError(t, err)
	require.NotEmpty(t, action.ClientToken)
}

func TestGeneratePSTSAllocateClientTokenIncludesLifecycle(t *testing.T) {
	addressing := ccev2.AddressPairList{
		&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
	}
	token1 := generatePSTSAllocateClientToken("ns/pod", "psts", "pod-uid-1", "sbn", addressing)
	token2 := generatePSTSAllocateClientToken("ns/pod", "psts", "pod-uid-2", "sbn", addressing)
	token3 := generatePSTSAllocateClientToken("ns/pod", "psts", "pod-uid-1", "sbn", addressing)

	require.NotEmpty(t, token1)
	require.NotEqual(t, token1, token2)
	require.Equal(t, token1, token3)
}

func TestPSTSAllocationLifecycleIDRequiresContainerID(t *testing.T) {
	resource := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cep",
			Namespace: "ns",
			UID:       "cep-uid",
		},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				ContainerID: "sandbox-1",
				Netns:       "/var/run/netns/sandbox-1",
				K8sObjectID: "pod-uid",
			},
		},
	}

	id, err := pstsAllocationLifecycleID(resource)
	require.NoError(t, err)
	require.Equal(t, "sandbox-1", id)

	_, err = pstsAllocationLifecycleID(&ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
	})
	require.EqualError(t, err, "missing external identifiers for PSTS endpoint ns/cep")

	_, err = pstsAllocationLifecycleID(&ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{},
		},
	})
	require.EqualError(t, err, "missing container-id for PSTS endpoint ns/cep")

	_, err = pstsAllocationLifecycleID(nil)
	require.EqualError(t, err, "nil CCEEndpoint when resolving PSTS lifecycle identifier")
}

func TestPSTSAllocationLifecycleIDChangesAcrossSandboxRebuilds(t *testing.T) {
	resource1 := &ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				ContainerID: "sandbox-a",
				Netns:       "/var/run/netns/a",
				K8sObjectID: "same-pod-uid",
			},
		},
	}
	resource2 := &ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				ContainerID: "sandbox-b",
				Netns:       "/var/run/netns/b",
				K8sObjectID: "same-pod-uid",
			},
		},
	}

	id1, err := pstsAllocationLifecycleID(resource1)
	require.NoError(t, err)
	id2, err := pstsAllocationLifecycleID(resource2)
	require.NoError(t, err)
	require.NotEqual(t, id1, id2)
}

func TestAllocateIPReusePath(t *testing.T) {
	psts := &ccev2.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{Name: "psts", Namespace: "ns"},
	}
	pstsLister := &fakePSTSLister{psts: psts}

	resource := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "cep", Namespace: "ns"},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				ContainerID: "sandbox-reuse",
				K8sObjectID: "pod-uid-new",
			},
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{
					PSTSName: "psts",
				},
			},
		},
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family}},
			},
		},
	}

	op := &fakeDirectEndpointOperation{
		allocateFn: func(ctx context.Context, action *DirectIPAction) error {
			return nil
		},
	}

	provider := &pstsAllocatorProvider{
		EndpointManager: &EndpointManager{
			pstsLister:        pstsLister,
			directIPAllocator: &fakeDirectIPAllocator{op: op},
		},
	}

	err := provider.AllocateIP(context.Background(), logrus.NewEntry(logrus.New()), resource)
	require.NoError(t, err)
}
