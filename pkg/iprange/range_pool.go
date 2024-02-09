package iprange

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sort"

	k8sutilnet "k8s.io/utils/net"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/cidr"
)

// IPFilter filte unavailable IP addresses
// this interface is used for filtering IP addresses which have been allocated by ipam
type IPFilter interface {
	FilterIP(ip net.IP) bool
}

type subnetUnivaluedIP struct {
	cidr *net.IPNet
}

func (p *subnetUnivaluedIP) FilterIP(ip net.IP) bool {
	if !p.cidr.Contains(ip) {
		return true
	}
	return cidr.IsUnicastIP(ip, p.cidr.String())
}

// RangePool manager ip pool control by range of ip
type RangePool struct {
	sbnRange map[string][]*big.Int
	ipFilter []IPFilter
}

// NewCustomRangePool create new ip range manager
// This object is used by custom mode of ipam
func NewCustomRangePool(psts *networkingv1alpha1.PodSubnetTopologySpread, ipFilter ...IPFilter) (*RangePool, error) {
	manager := &RangePool{
		sbnRange: make(map[string][]*big.Int),
		ipFilter: ipFilter,
	}
	for sbnID, sa := range psts.Spec.Subnets {
		if sbnStatus, ok := psts.Status.AvailableSubnets[sbnID]; ok {
			_, subnetCIDR, err := net.ParseCIDR(sbnStatus.CIDR)
			if err != nil {
				return nil, err
			}
			ipRange, err := ListAllCustomIPRangeIndexs(subnetCIDR, sa.Custom)
			if err != nil {
				return nil, fmt.Errorf("sbnnet %s %w", sbnID, err)
			}
			manager.sbnRange[sbnID] = ipRange
			manager.ipFilter = append(manager.ipFilter, &subnetUnivaluedIP{cidr: subnetCIDR})

		}
	}
	if len(manager.sbnRange) == 0 {
		return nil, errors.New("no index range")
	}
	return manager, nil
}

// IPInRange Whether to include the specified IP in the custom IPRange
func (pool *RangePool) IPInRange(ip string) bool {
	netIP := net.ParseIP(ip)
	if netIP == nil {
		return false
	}

	ipIndex := k8sutilnet.BigForIP(netIP)

	for _, indexRange := range pool.sbnRange {
		if isInRangeIndex(ipIndex, indexRange) {
			return true
		}
	}

	return false
}

// FirstAvailableIP try to get the first available IP
// Return: IP address can be use for pod
//
//	nil if no ip can be used
func (pool *RangePool) FirstAvailableIP(sbnID string) net.IP {
	indexRange, ok := pool.sbnRange[sbnID]
	if ok {
		for i := 0; i+1 < len(indexRange); i = i + 2 {
			for ipInt := indexRange[i]; ipInt.Cmp(indexRange[i+1]) <= 0; ipInt = big.NewInt(0).Add(ipInt, big.NewInt(int64(1))) {
				ip := BytesToIP(ipInt.Bytes())
				if pool.FilterIP(ip) {
					return ip
				}
			}

		}
	}
	return nil
}

// FilteIP return true if the IP can be used
func (pool *RangePool) FilterIP(ip net.IP) bool {
	for _, filter := range pool.ipFilter {
		if !filter.FilterIP(ip) {
			return false
		}
	}
	return true
}

func isInRangeIndex(origin *big.Int, indexRange []*big.Int) bool {
	for i := 0; i+1 < len(indexRange); i = i + 2 {
		if origin.Cmp(indexRange[i]) >= 0 && origin.Cmp(indexRange[i+1]) <= 0 {
			return true
		}
	}
	return false
}

func (pool *RangePool) String() string {
	pooklMap := make(map[string][]string)
	for sbnID, rs := range pool.sbnRange {
		for _, bint := range rs {
			pooklMap[sbnID] = append(pooklMap[sbnID], BytesToIP(bint.Bytes()).String())
		}
	}
	b, _ := json.Marshal(pooklMap)
	return string(b)
}

// ListAllCustomIPRangeIndexs The IP address range is listed in a list. Every two elements of the returned array are a group
// eg: [1,10, 10,100, 109,107]
// In the example array:
// if v=2, then v is in this element list.
// If v=101, v is not in this list
func ListAllCustomIPRangeIndexs(subnetCIDR *net.IPNet, customs []networkingv1alpha1.CustomAllocation) (indexRanges []*big.Int, err error) {
	var ipVersion k8sutilnet.IPFamily = k8sutilnet.IPv4
	if subnetCIDR.IP.To4() == nil {
		ipVersion = k8sutilnet.IPv6
	}

	for _, custom := range customs {
		if custom.Family == ipVersion {
			for _, r := range custom.CustomIPRange {
				start, end := customerRangeToBigInt(r)
				if start == nil || end == nil {
					return nil, fmt.Errorf("")
				}
				if !subnetCIDR.Contains(BytesToIP(start.Bytes())) || !subnetCIDR.Contains(BytesToIP(end.Bytes())) {
					return nil, fmt.Errorf("ip %s %s not in cidr range %s", r.Start, r.End, subnetCIDR.String())
				}
				indexRanges = append(indexRanges, start, end)
			}
		}
	}

	if !overlappingCheck(indexRanges) {
		return nil, fmt.Errorf("overlapping IP Range")
	}

	// fill ip ranges
	sort.Slice(indexRanges, func(i, j int) bool {
		return indexRanges[i].Cmp(indexRanges[j]) == -1
	})

	if len(indexRanges) == 0 {
		// first ip of cidr
		firstIndex := k8sutilnet.BigForIP(subnetCIDR.IP)
		lastIndex := big.NewInt(0).Add(firstIndex, big.NewInt(k8sutilnet.RangeSize(subnetCIDR)))
		indexRanges = append(indexRanges, firstIndex, lastIndex)
	}

	return
}

func customerRangeToBigInt(r networkingv1alpha1.CustomIPRange) (start, end *big.Int) {
	if startIP := net.ParseIP(r.Start); startIP != nil {
		start = k8sutilnet.BigForIP(startIP)
	}
	if endIP := net.ParseIP(r.End); endIP != nil {
		end = k8sutilnet.BigForIP(endIP)
	}
	return
}

// BytesToIP translate []bytes to net.IP
func BytesToIP(r []byte) net.IP {
	r = append(make([]byte, 16), r...)
	return net.IP(r[len(r)-16:])
}

// NewCIDRRangePool create new ip range manager
// This object is used by fixed IP and manually assigned IP
func NewCIDRRangePool(psts *networkingv1alpha1.PodSubnetTopologySpread, ipFilter ...IPFilter) (*RangePool, error) {
	manager := &RangePool{
		sbnRange: make(map[string][]*big.Int),
		ipFilter: ipFilter,
	}
	for sbnID, sa := range psts.Spec.Subnets {
		if sbnStatus, ok := psts.Status.AvailableSubnets[sbnID]; ok {
			_, subnetCIDR, err := net.ParseCIDR(sbnStatus.CIDR)
			if err != nil {
				return nil, err
			}
			ipRange, err := ListAllCIDRIPRangeIndexs(subnetCIDR, sa)
			if err != nil {
				return nil, err
			}
			manager.sbnRange[sbnID] = ipRange
			manager.ipFilter = append(manager.ipFilter, &subnetUnivaluedIP{cidr: subnetCIDR})

		}
	}
	if len(manager.sbnRange) == 0 {
		return nil, errors.New("no index range")
	}
	return manager, nil
}

// ListAllCIDRIPRangeIndexs The IP address range is listed in a list. Every two elements of the returned array are a group
// eg: [1,10, 10,100, 109,107]
// In the example array:
// if v=2, then v is in this element list.
// If v=101, v is not in this list
func ListAllCIDRIPRangeIndexs(subnetCIDR *net.IPNet, sa networkingv1alpha1.SubnetAllocation) (indexRanges []*big.Int, err error) {
	ipVersion := 4
	if subnetCIDR.IP.To4() == nil {
		ipVersion = 6
	}

	if ipVersion == 4 {
		for _, str := range sa.IPv4 {
			ip := net.ParseIP(str)
			if ip != nil {
				ipint := k8sutilnet.BigForIP(ip)
				if !subnetCIDR.Contains(ip) {
					return nil, fmt.Errorf("ip not in cidr range")
				}
				indexRanges = append(indexRanges, ipint, ipint)
			}
		}

		cidrs, err := k8sutilnet.ParseCIDRs(sa.IPv4Range)
		if err != nil {
			return nil, err
		}
		for _, cidr := range cidrs {
			first := k8sutilnet.BigForIP(cidr.IP)
			last := big.NewInt(0).Add(first, big.NewInt(k8sutilnet.RangeSize(cidr)-1))
			if !subnetCIDR.Contains(BytesToIP(first.Bytes())) || !subnetCIDR.Contains(BytesToIP(last.Bytes())) {
				return nil, fmt.Errorf("ip not in cidr range")
			}
			indexRanges = append(indexRanges, first, last)
		}
	}

	if !overlappingCheck(indexRanges) {
		return nil, fmt.Errorf("overlapping IP Range")
	}

	// fill ip ranges
	sort.Slice(indexRanges, func(i, j int) bool {
		return indexRanges[i].Cmp(indexRanges[j]) == -1
	})

	if len(indexRanges) == 0 {
		// first ip of cidr
		firstIndex := k8sutilnet.BigForIP(subnetCIDR.IP)
		lastIndex := big.NewInt(0).Add(firstIndex, big.NewInt(k8sutilnet.RangeSize(subnetCIDR)-1))
		indexRanges = append(indexRanges, firstIndex, lastIndex)
	}

	return
}

// Overlapping detection range
// Compare each group of ranges. The start and end of other groups cannot overlap
// return true if each goup does not overlap
func overlappingCheck(indexRanges []*big.Int) bool {
	for i := 0; i+1 < len(indexRanges); i = i + 2 {
		for j := i + 2; j+1 < len(indexRanges); j = j + 2 {
			a := indexRanges[i].Cmp(indexRanges[j])
			b := indexRanges[i+1].Cmp(indexRanges[j])
			c := indexRanges[i].Cmp(indexRanges[j+1])
			d := indexRanges[i+1].Cmp(indexRanges[j+1])
			if a != b || b != c || c != d {
				return false
			}
		}
	}
	return true
}
