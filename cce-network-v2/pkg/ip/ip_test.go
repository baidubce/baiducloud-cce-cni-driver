package ip

import (
	"net"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountIPsInCIDR(t *testing.T) {
	_, cidr30, _ := net.ParseCIDR("10.0.0.0/30")
	require.Equal(t, int64(2), CountIPsInCIDR(cidr30).Int64())

	_, cidr32, _ := net.ParseCIDR("10.0.0.1/32")
	require.Equal(t, int64(0), CountIPsInCIDR(cidr32).Int64())
}

func TestNetsByMaskSort(t *testing.T) {
	_, cidr24, _ := net.ParseCIDR("10.0.0.0/24")
	_, cidr25, _ := net.ParseCIDR("10.0.0.128/25")
	_, cidr25b, _ := net.ParseCIDR("10.0.0.0/25")

	list := NetsByMask{cidr24, cidr25, cidr25b}
	sort.Sort(list)
	// Smaller prefix first, then by IP
	require.Equal(t, "10.0.0.0/24", list[0].String())
	require.Equal(t, "10.0.0.0/25", list[1].String())
	require.Equal(t, "10.0.0.128/25", list[2].String())
}

func TestRemoveCIDRs(t *testing.T) {
	_, allow, _ := net.ParseCIDR("10.0.0.0/24")
	_, remove, _ := net.ParseCIDR("10.0.0.0/25")
	result := RemoveCIDRs([]*net.IPNet{allow}, []*net.IPNet{remove})
	require.Len(t, result, 1)
	require.Equal(t, "10.0.0.128/25", result[0].String())
}

func TestPrefixCeil(t *testing.T) {
	require.Equal(t, 0, PrefixCeil(0, 8))
	require.Equal(t, 2, PrefixCeil(10, 8))
	require.Equal(t, 2, PrefixCeil(16, 8))
}

func TestPrefixToIps(t *testing.T) {
	ips, err := PrefixToIps("10.0.0.0/30")
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0", "10.0.0.1", "10.0.0.2", "10.0.0.3"}, ips)
}

func TestGetIPAtIndex(t *testing.T) {
	_, cidr30, _ := net.ParseCIDR("10.0.0.0/30")
	require.Equal(t, "10.0.0.0", GetIPAtIndex(*cidr30, 0).String())
	require.Equal(t, "10.0.0.3", GetIPAtIndex(*cidr30, 3).String())
	require.Nil(t, GetIPAtIndex(*cidr30, 4))
	require.Equal(t, "10.0.0.3", GetIPAtIndex(*cidr30, -1).String())
}

func TestPreviousAndNextIP(t *testing.T) {
	zero := net.IPv4(0, 0, 0, 0)
	require.Equal(t, zero.String(), getPreviousIP(zero).String())

	ip := net.ParseIP("10.0.0.0").To4()
	require.Equal(t, "9.255.255.255", getPreviousIP(ip).String())
	require.Equal(t, "10.0.0.1", GetNextIP(ip).String())

	upper := net.IP(upperIPv4)
	require.Equal(t, upper.String(), GetNextIP(upper).String())
}

func TestCoalesceCIDRs(t *testing.T) {
	_, a, _ := net.ParseCIDR("10.0.0.0/25")
	_, b, _ := net.ParseCIDR("10.0.0.128/25")
	v4, v6 := CoalesceCIDRs([]*net.IPNet{a, b})
	require.Empty(t, v6)
	require.Len(t, v4, 1)
	require.Equal(t, "10.0.0.0/24", v4[0].String())
}

func TestIsPublicAddr(t *testing.T) {
	require.False(t, IsPublicAddr(net.ParseIP("10.0.0.1")))
	require.True(t, IsPublicAddr(net.ParseIP("8.8.8.8")))
}

func TestIPRangeToCIDRs(t *testing.T) {
	cidrs, err := IPRangeToCIDRs(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.3"))
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1/32", "10.0.0.2/31"}, cidrs)

	_, err = IPRangeToCIDRs(net.ParseIP("10.0.0.3"), net.ParseIP("10.0.0.1"))
	require.Error(t, err)
}

func TestKeepUniqueIPsAndSort(t *testing.T) {
	ips := []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1")}
	unique := KeepUniqueIPs(ips)
	require.Len(t, unique, 2)
	SortIPList(unique)
	require.True(t, SortedIPListsAreEqual(unique, []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2")}))
	require.True(t, UnsortedIPListsAreEqual([]net.IP{net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1")}, []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2")}))
}

func TestGetIPFromListByFamily(t *testing.T) {
	ips := []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("2001:db8::1")}
	require.Equal(t, "10.0.0.1", GetIPFromListByFamily(ips, true).String())
	require.Equal(t, "2001:db8::1", GetIPFromListByFamily(ips, false).String())
}
