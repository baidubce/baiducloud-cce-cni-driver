package tc

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func FilterAdd(filter netlink.Filter) error {
	log := tcLog.WithField("filter", filter.Type())
	if u32F, ok := filter.(*netlink.U32); ok {
		ipnet := U32KeyToIPNet(u32F.Sel.Keys)
		if ipnet != nil {
			log = log.WithField("ipnet", ipnet.String())
		}
	}

	log.Infof("tc filter add %s", filter.Attrs().String())
	err := netlink.FilterAdd(filter)
	if err != nil {
		return fmt.Errorf("faild to add filter %s:%w", filter.Attrs().String(), err)
	}
	return nil
}

func FilterDel(filter netlink.Filter) error {
	log := tcLog.WithField("filter", filter.Type())
	if u32F, ok := filter.(*netlink.U32); ok {
		ipnet := U32KeyToIPNet(u32F.Sel.Keys)
		if ipnet != nil {
			log = log.WithField("ipnet", ipnet.String())
		}
	}

	log.Infof("tc filter del %s", filter.Attrs().String())
	err := netlink.FilterDel(filter)
	if err != nil {
		return fmt.Errorf("failed to del filter %s: %w", filter.Attrs().String(), err)
	}
	return nil
}

func NewU32RuleBySrcIP(linkIndex int, parentID uint32, ipnet *net.IPNet) *netlink.U32 {
	keys := U32MatchSrc(ipnet)
	u32 := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: linkIndex,
			Parent:    parentID,
			Priority:  1,
			Protocol:  unix.ETH_P_IP,
		},
		Sel: &netlink.TcU32Sel{
			Flags: nl.TC_U32_TERMINAL,
			Keys:  keys,
			Nkeys: uint8(len(keys)),
		},
	}
	return u32
}

// U32MatchSrc return u32 match key by src ip
func U32MatchSrc(ipNet *net.IPNet) []netlink.TcU32Key {
	if ipNet.IP.To4() == nil {
		return U32IPv6Src(ipNet)
	}
	return []netlink.TcU32Key{U32IPv4Src(ipNet)}
}

func U32IPv4Src(ipNet *net.IPNet) netlink.TcU32Key {
	mask := net.IP(ipNet.Mask).To4()
	val := ipNet.IP.Mask(ipNet.Mask).To4()
	return netlink.TcU32Key{
		Mask: binary.BigEndian.Uint32(mask),
		Val:  binary.BigEndian.Uint32(val),
		// 12 is the offset of the source address field in the IPv4 header.
		Off: 12,
	}
}

func U32IPv6Src(ipNet *net.IPNet) []netlink.TcU32Key {
	mask := ipNet.Mask
	val := ipNet.IP.Mask(ipNet.Mask)

	r := make([]netlink.TcU32Key, 0, 4)
	for i := 0; i < 4; i++ {
		m := binary.BigEndian.Uint32(mask)
		if m != 0 {
			r = append(r, netlink.TcU32Key{
				Mask: m,
				Val:  binary.BigEndian.Uint32(val),
				// 8 is the offset of the source address field in the IPv6 header.
				Off: int32(8 + 4*i),
			})
		}
		mask = mask[4:]
		val = val[4:]
	}
	return r
}

// FilterBySrcIP found u32 filter by pod ip
// used for prio only
func FilterListBySrcIP(link netlink.Link, parent uint32, ipNets []*net.IPNet) ([]*netlink.U32, error) {
	filters, err := netlink.FilterList(link, parent)
	if err != nil {
		return nil, err
	}

	var (
		matches        []nl.TcU32Key
		matchedFilters []*netlink.U32
	)
	for _, ipNet := range ipNets {
		matches = append(matches, U32MatchSrc(ipNet)...)
	}
	if len(matches) == 0 {
		return nil, nil
	}

	for _, f := range filters {
		u32, ok := f.(*netlink.U32)
		if !ok {
			continue
		}
		if u32.Attrs().LinkIndex != link.Attrs().Index ||
			u32.Protocol != unix.ETH_P_IP ||
			u32.Sel == nil {
			continue
		}

		// check all matches is satisfied with current
		for _, m := range matches {
			for _, key := range u32.Sel.Keys {
				if key.Off != m.Off || key.Val != m.Val || key.Mask != m.Mask {
					continue
				}
				matchedFilters = append(matchedFilters, u32)
				break
			}
		}
	}
	return matchedFilters, nil
}

// U32KeyToIPNet convert u32 key to ipnet
func U32KeyToIPNet(keys []nl.TcU32Key) *net.IPNet {
	var (
		iparr  []byte
		ipmask []byte
	)

	if len(keys) == 0 {
		return nil
	} else if len(keys) == 1 {
		iparr = make([]byte, 4)
		ipmask = make([]byte, 4)
	} else {
		iparr = make([]byte, 16)
		ipmask = make([]byte, 16)
	}

	for _, key := range keys {
		binary.BigEndian.PutUint32(ipmask, key.Mask)
		binary.BigEndian.PutUint32(iparr, key.Val)
	}
	return &net.IPNet{
		IP:   net.IP(iparr),
		Mask: net.IPMask(ipmask),
	}
}
