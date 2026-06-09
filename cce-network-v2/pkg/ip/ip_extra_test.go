package ip

import (
	"net"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveRedundantCIDRs_Variants(t *testing.T) {
	_, a, _ := net.ParseCIDR("10.0.0.0/24")
	_, b, _ := net.ParseCIDR("10.0.1.0/24")
	res := removeRedundantCIDRs([]*net.IPNet{a, b})
	require.Len(t, res, 2)

	_, a25, _ := net.ParseCIDR("10.0.0.0/25")
	res = removeRedundantCIDRs([]*net.IPNet{a, a25})
	require.Len(t, res, 1)
	require.Equal(t, "10.0.0.0/24", res[0].String())

	_, a25b, _ := net.ParseCIDR("10.0.0.128/25")
	res = removeRedundantCIDRs([]*net.IPNet{a, a25, a25b})
	require.Len(t, res, 1)
	require.Equal(t, "10.0.0.0/24", res[0].String())
}

func TestGetNetworkPrefixIPv4(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/24")
	prefix := getNetworkPrefix(cidr)
	require.Equal(t, "10.0.0.0", prefix.String())
}

func TestExcludeContainedCIDR(t *testing.T) {
	_, allow, _ := net.ParseCIDR("10.0.0.0/24")
	_, remove, _ := net.ParseCIDR("10.0.0.0/24")
	require.Nil(t, excludeContainedCIDR(allow, remove))

	_, remove25, _ := net.ParseCIDR("10.0.0.0/25")
	result := excludeContainedCIDR(allow, remove25)
	require.Len(t, result, 1)
	require.Equal(t, "10.0.0.128/25", result[0].String())
}

func TestFlipNthHighestBit(t *testing.T) {
	ip := net.IPv4(0, 0, 0, 0).To4()
	flipNthHighestBit(ip, 0)
	require.Equal(t, byte(0x80), ip[0])
}

func TestIPNetToRangeIPv4(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/30")
	rng := ipNetToRange(*cidr)
	require.Equal(t, "10.0.0.0", rng.First.String())
	require.Equal(t, "10.0.0.3", rng.Last.String())
	require.Equal(t, net.IPv6len, len(*rng.First))
}

func TestGetPreviousIPIPv4Len4(t *testing.T) {
	zero := net.IPv4(0, 0, 0, 0).To4()
	require.Equal(t, "0.0.0.0", getPreviousIP(zero).String())
	ip := net.ParseIP("10.0.0.0").To4()
	require.Equal(t, "9.255.255.255", getPreviousIP(ip).String())
}

func TestGetNextIPIPv6(t *testing.T) {
	ip := net.ParseIP("2001:db8::1")
	next := GetNextIP(ip)
	require.Equal(t, "2001:db8::2", next.String())
	upper := net.IP(upperIPv6)
	require.Equal(t, upper.String(), GetNextIP(upper).String())
}

func TestCreateSpanningCIDRIPv4(t *testing.T) {
	first := net.ParseIP("10.0.0.0")
	last := net.ParseIP("10.0.0.255")
	netw := createSpanningCIDR(netWithRange{First: &first, Last: &last, Network: nil})
	require.Equal(t, "10.0.0.0/24", netw.String())
}

func TestMergeAdjacentCIDRsAndCoalesceRanges(t *testing.T) {
	_, a, _ := net.ParseCIDR("10.0.0.0/25")
	_, b, _ := net.ParseCIDR("10.0.0.128/25")
	r1 := ipNetToRange(*a)
	r2 := ipNetToRange(*b)
	merged := mergeAdjacentCIDRs([]*netWithRange{&r1, &r2})
	require.Len(t, merged, 1)
	require.Nil(t, merged[0].Network)

	coalesced := coalesceRanges(merged)
	require.Len(t, coalesced, 1)
	require.Equal(t, "10.0.0.0/24", coalesced[0].String())
}

func TestRangeToCIDRsPartitions(t *testing.T) {
	first := net.ParseIP("10.0.0.1")
	last := net.ParseIP("10.0.0.2")
	cidrs := rangeToCIDRs(first, last)
	require.Len(t, cidrs, 2)
	got := []string{cidrs[0].String(), cidrs[1].String()}
	sort.Strings(got)
	require.Equal(t, []string{"10.0.0.1/32", "10.0.0.2/32"}, got)
}

func TestPartitionCIDRPaths(t *testing.T) {
	_, target, _ := net.ParseCIDR("10.0.0.0/24")
	_, excludeLeft, _ := net.ParseCIDR("10.1.0.0/24")
	left, mid, right := partitionCIDR(*target, *excludeLeft)
	require.Len(t, left, 1)
	require.Nil(t, mid)
	require.Nil(t, right)

	_, excludeRight, _ := net.ParseCIDR("9.0.0.0/24")
	left, mid, right = partitionCIDR(*target, *excludeRight)
	require.Nil(t, left)
	require.Nil(t, mid)
	require.Len(t, right, 1)

	_, excludeParent, _ := net.ParseCIDR("10.0.0.0/16")
	left, mid, right = partitionCIDR(*target, *excludeParent)
	require.Nil(t, left)
	require.Len(t, mid, 1)
	require.Nil(t, right)

	_, excludeChild, _ := net.ParseCIDR("10.0.0.0/25")
	left, mid, right = partitionCIDR(*target, *excludeChild)
	require.NotNil(t, mid)
	require.True(t, len(left)+len(right) > 0)
}

func TestIsExcluded(t *testing.T) {
	ip := net.ParseIP("10.0.0.1")
	require.True(t, IsExcluded([]net.IP{ip}, ip))
	require.False(t, IsExcluded([]net.IP{net.ParseIP("10.0.0.2")}, ip))
}

func TestGetCIDRPrefixesFromIPs(t *testing.T) {
	require.Nil(t, GetCIDRPrefixesFromIPs(nil))
	ips := []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("2001:db8::1")}
	prefixes := GetCIDRPrefixesFromIPs(ips)
	require.Len(t, prefixes, 2)
}

func TestIPToPrefixFamilies(t *testing.T) {
	p4 := IPToPrefix(net.ParseIP("10.0.0.1"))
	ones4, bits4 := p4.Mask.Size()
	require.Equal(t, 32, ones4)
	require.Equal(t, 32, bits4)

	p6 := IPToPrefix(net.ParseIP("2001:db8::1"))
	ones6, bits6 := p6.Mask.Size()
	require.Equal(t, 128, ones6)
	require.Equal(t, 128, bits6)
}

func TestIsIPv4IPv6(t *testing.T) {
	require.True(t, IsIPv4(net.ParseIP("10.0.0.1")))
	require.False(t, IsIPv6(net.ParseIP("10.0.0.1")))
	require.True(t, IsIPv6(net.ParseIP("2001:db8::1")))
	require.False(t, IsIPv4(net.ParseIP("2001:db8::1")))
	require.False(t, IsIPv6(nil))
}

func TestSortAndCompareIPLists(t *testing.T) {
	ips := []net.IP{net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1")}
	SortIPList(ips)
	require.Equal(t, "10.0.0.1", ips[0].String())

	unsorted := []net.IP{net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1")}
	sorted := getSortedIPList(unsorted)
	require.Equal(t, "10.0.0.1", sorted[0].String())
	require.Equal(t, "10.0.0.2", unsorted[0].String())

	require.False(t, SortedIPListsAreEqual([]net.IP{net.ParseIP("10.0.0.1")}, []net.IP{net.ParseIP("10.0.0.2")}))
	require.False(t, UnsortedIPListsAreEqual([]net.IP{net.ParseIP("10.0.0.1")}, []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2")}))
}

func TestIPRangeToCIDRs_ErrorAndIPv6(t *testing.T) {
	_, err := IPRangeToCIDRs(nil, net.ParseIP("10.0.0.1"))
	require.Error(t, err)
	_, err = IPRangeToCIDRs(net.ParseIP("10.0.0.1"), net.ParseIP("2001:db8::1"))
	require.Error(t, err)
	_, err = IPRangeToCIDRs(net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1"))
	require.Error(t, err)

	cidrs, err := IPRangeToCIDRs(net.ParseIP("2001:db8::"), net.ParseIP("2001:db8::ffff"))
	require.NoError(t, err)
	require.Equal(t, []string{"2001:db8::/112"}, cidrs)
}

func TestIPv6Helpers(t *testing.T) {
	require.Equal(t, -1, bytesCompare([]byte{0x00}, []byte{0x01}))
	require.Equal(t, 1, bytesCompare([]byte{0x02}, []byte{0x01}))
	require.Equal(t, 0, bytesCompare([]byte{0x01}, []byte{0x01}))

	ip := net.ParseIP("2001:db8::").To16()
	require.True(t, isAligned(ip, 120))
	ip[15] = 0x01
	require.False(t, isAligned(ip, 120))

	ip = net.ParseIP("2001:db8::").To16()
	end := getBlockEnd(ip, 120)
	require.Equal(t, "2001:db8::ff", net.IP(end).String())

	ip = net.ParseIP("2001:db8::").To16()
	require.True(t, incrementIP(ip))
	require.Equal(t, "2001:db8::1", net.IP(ip).String())

	max := make([]byte, 16)
	for i := range max {
		max[i] = 0xff
	}
	require.False(t, incrementIP(max))

	ip = net.ParseIP("2001:db8::").To16()
	addToIP(ip, 0)
	require.Equal(t, "2001:db8::", net.IP(ip).String())
	addToIP(ip, 1)
	require.Equal(t, "2001:db8::2", net.IP(ip).String())
}

func TestUint32IPConversions(t *testing.T) {
	ip := net.ParseIP("10.0.0.1")
	val := ipToUint32(ip)
	require.Equal(t, ip.String(), uint32ToIP(val).String())
}
