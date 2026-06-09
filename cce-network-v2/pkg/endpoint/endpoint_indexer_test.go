package endpoint

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

func buildReserveAnnotation(t *testing.T, data map[string]IPReserveInfo) string {
	b, err := json.Marshal(data)
	require.NoError(t, err)
	return string(b)
}

func TestParseReserveAnnotation(t *testing.T) {
	lp := newLocalPool()

	psts := &ccev2.PodSubnetTopologySpread{}
	res, err := lp.parseReserveAnnotation(psts)
	require.NoError(t, err)
	require.Nil(t, res)

	psts.Annotations = map[string]string{AnnotationIPReserveIDs: "bad-json"}
	_, err = lp.parseReserveAnnotation(psts)
	require.Error(t, err)

	psts.Annotations = map[string]string{AnnotationIPReserveIDs: buildReserveAnnotation(t, map[string]IPReserveInfo{
		"sbn": {SubnetID: "sbn", Reserves: map[string]string{"10.0.0.0/30": "rid"}},
	})}
	res, err = lp.parseReserveAnnotation(psts)
	require.NoError(t, err)
	require.Contains(t, res, "sbn")
}

func TestBuildExpectedPoolsFromAnnotation(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p1"}}
	psts.Annotations = map[string]string{AnnotationIPReserveIDs: buildReserveAnnotation(t, map[string]IPReserveInfo{
		"sbn": {
			SubnetID: "sbn",
			Reserves: map[string]string{
				"10.0.0.0/30":    "rid",
				"2001:db8::/126": "rid6",
			},
		},
	})}

	pools, err := lp.buildExpectedPoolsFromAnnotation(psts)
	require.NoError(t, err)
	require.Len(t, pools, 2)

	p4 := pools[getPSTSPoolID("ns", "p1", "sbn", string(ccev2.IPv4Family))]
	require.NotNil(t, p4)
	require.Equal(t, "sbn", p4.subnetID)
	require.Len(t, p4.ranges, 1)

	p6 := pools[getPSTSPoolID("ns", "p1", "sbn", string(ccev2.IPv6Family))]
	require.NotNil(t, p6)
	require.Len(t, p6.ranges, 1)
}

func TestBuildExpectedPoolsForSubnetInvalidCIDR(t *testing.T) {
	lp := newLocalPool()
	reserve := IPReserveInfo{SubnetID: "sbn", Reserves: map[string]string{"bad": "rid"}}
	pools := make(expectedPoolMap)
	err := lp.buildExpectedPoolsForSubnet("sbn", reserve, "ns", "p1", pools)
	require.Error(t, err)
}

func TestParseCIDRToIPRange(t *testing.T) {
	lp := newLocalPool()
	family, rng, err := lp.parseCIDRToIPRange("10.0.0.0/30")
	require.NoError(t, err)
	require.Equal(t, string(ccev2.IPv4Family), family)
	require.Equal(t, "10.0.0.0", rng.startIP.String())
	require.Equal(t, "10.0.0.3", rng.endIP.String())

	family, rng, err = lp.parseCIDRToIPRange("2001:db8::/126")
	require.NoError(t, err)
	require.Equal(t, string(ccev2.IPv6Family), family)
	require.Equal(t, "2001:db8::", rng.startIP.String())
	require.Equal(t, "2001:db8::3", rng.endIP.String())
}

func TestUpdateAndDeletePSTS(t *testing.T) {
	lp := newLocalPool()
	psts := &ccev2.PodSubnetTopologySpread{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p1"}}
	psts.Annotations = map[string]string{AnnotationIPReserveIDs: buildReserveAnnotation(t, map[string]IPReserveInfo{
		"sbn": {SubnetID: "sbn", Reserves: map[string]string{"10.0.0.0/30": "rid"}},
	})}

	lp.updatePSTS(nil, psts)
	poolID := getPSTSPoolID("ns", "p1", "sbn", string(ccev2.IPv4Family))
	pool := lp.getPSTSPoolByID(poolID)
	require.NotNil(t, pool)
	require.Equal(t, "p1", pool.pstsName)
	require.Equal(t, "ns", pool.pstsNamespace)
	require.NotNil(t, pool.realOccupied)

	// Update ranges
	psts.Annotations = map[string]string{AnnotationIPReserveIDs: buildReserveAnnotation(t, map[string]IPReserveInfo{
		"sbn": {SubnetID: "sbn", Reserves: map[string]string{"10.0.1.0/30": "rid"}},
	})}
	lp.updatePSTS(nil, psts)
	pool = lp.getPSTSPoolByID(poolID)
	require.NotNil(t, pool)
	require.Equal(t, "10.0.1.0", pool.ranges[0].startIP.String())

	lp.deletePSTS(psts)
	require.Nil(t, lp.getPSTSPoolByID(poolID))
}

func TestDeleteEndpointAddressRelease(t *testing.T) {
	lp := newLocalPool()
	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "p1", "sbn", "4"), "ns", "p1", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	pool.markOccupied(net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	cep := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "ep", Namespace: "ns"},
		Spec:       ccev2.EndpointSpec{Network: ccev2.EndpointNetworkSpec{IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"}}},
		Status: ccev2.EndpointStatus{Networking: &ccev2.EndpointNetworking{Addressing: ccev2.AddressPairList{
			&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
		}}},
	}
	lp.deleteEndpointAddress(cep)
	require.False(t, pool.realOccupied["10.0.0.1"])
}

func TestRestoreOccupiedFromCEPs(t *testing.T) {
	lp := newLocalPool()
	pool := newExpectedPool("sbn", "4", nil)
	pool.initPoolRuntime(getPSTSPoolID("ns", "p1", "sbn", "4"), "ns", "p1", logrus.NewEntry(logrus.New()))
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))
	lp.pstsLocalPool[pool.poolID] = pool

	ceps := []*ccev2.CCEEndpoint{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "ep", Namespace: "ns"},
			Spec:       ccev2.EndpointSpec{Network: ccev2.EndpointNetworkSpec{IPAllocation: &ccev2.IPAllocation{PSTSName: "p1"}}},
			Status: ccev2.EndpointStatus{Networking: &ccev2.EndpointNetworking{Addressing: ccev2.AddressPairList{
				&ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family},
			}}},
		},
		{ObjectMeta: metav1.ObjectMeta{Name: "ep2", Namespace: "ns"}},
	}

	total, marked := lp.restoreOccupiedFromCEPs(ceps)
	require.Equal(t, 1, total)
	require.Equal(t, 1, marked)
	require.True(t, pool.realOccupied["10.0.0.1"])
}

func TestReleaseLocalIPsNoPool(t *testing.T) {
	lp := newLocalPool()
	lp.releaseLocalIPs("ns", "p1", &ccev2.AddressPair{IP: "10.0.0.1", Subnet: "sbn", Family: ccev2.IPv4Family})
}

func TestLastIPInCIDR(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/30")
	last := lastIPInCIDR(ipnet)
	require.Equal(t, "10.0.0.3", last.String())
}

func TestGetPSTSPoolID(t *testing.T) {
	id := getPSTSPoolID("ns", "p1", "sbn", "4")
	require.Equal(t, "psts|ns:2:ns|name:2:p1|subnet:3:sbn|family:1:4", id)
}

func TestGetPSTSPoolIDNoCollisionForHyphenatedFields(t *testing.T) {
	id1 := getPSTSPoolID("a-b", "c-d", "sbn", "4")
	id2 := getPSTSPoolID("a", "b-c-d", "sbn", "4")
	require.NotEqual(t, id1, id2)
}

func TestBuildExpectedPoolsForSubnet(t *testing.T) {
	lp := newLocalPool()
	pools := make(expectedPoolMap)
	err := lp.buildExpectedPoolsForSubnet("sbn", IPReserveInfo{SubnetID: "sbn", Reserves: map[string]string{"10.0.0.0/30": "rid"}}, "ns", "p1", pools)
	require.NoError(t, err)
	require.Len(t, pools, 1)
}

func TestUpdatePSTSSkipInvalidObj(t *testing.T) {
	lp := newLocalPool()
	lp.updatePSTS(nil, "bad")
}

func TestUpdateSubnetNoop(t *testing.T) {
	lp := newLocalPool()
	lp.updateSubnet(nil, &ccev1.Subnet{})
}
