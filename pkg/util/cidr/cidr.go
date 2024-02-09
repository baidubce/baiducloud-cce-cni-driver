package cidr

import (
	"net"

	k8sutilnet "k8s.io/utils/net"
)

var (
	_, PrivateIPv4Net1, _ = net.ParseCIDR("10.0.0.0/8")
	_, PrivateIPv4Net2, _ = net.ParseCIDR("172.16.0.0/12")
	_, PrivateIPv4Net3, _ = net.ParseCIDR("192.168.0.0/16")
	_, PrivateIPv6Net, _  = net.ParseCIDR("fc00::/7")

	PrivateIPv4Nets       = []*net.IPNet{PrivateIPv4Net1, PrivateIPv4Net2, PrivateIPv4Net3}
	PrivateIPv4NetsString = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

	PrivateIPv6Nets       = []*net.IPNet{PrivateIPv6Net}
	PrivateIPv6NetsString = []string{"fc00::/7"}

	_, IPv4ZeroCIDR, _ = net.ParseCIDR("0.0.0.0/0")
	_, IPv6ZeroCIDR, _ = net.ParseCIDR("::/0")

	_, LinkLocalCIDR, _ = net.ParseCIDR("169.254.0.0/16")
)

func IsUnicastIP(ip net.IP, subnet string) bool {
	if !ip.IsGlobalUnicast() {
		return false
	}
	cidrs, err := k8sutilnet.ParseCIDRs([]string{subnet})
	if err != nil {
		return false
	}
	for _, ranges := range cidrs {
		if !ranges.Contains(ip) {
			return false
		}
	}
	reservedIPs := ListFirtstAndLastIPStringFromCIDR(subnet)
	for _, reserved := range reservedIPs {
		if ip.Equal(reserved) {
			return false
		}
	}
	return true
}

func ListIPsFromCIDR(cidr *net.IPNet) []net.IP {
	var ipList []net.IP
	size := k8sutilnet.RangeSize(cidr)
	if size == 0 {
		return []net.IP{cidr.IP}
	}
	var i int64
	for i = 0; i < size; i++ {
		ip, err := k8sutilnet.GetIndexedIP(cidr, int(i))
		if err != nil {
			continue
		}
		ipList = append(ipList, ip)
	}
	return ipList
}

func ListIPsFromCIDRString(cidr string) []net.IP {
	cidrs, err := k8sutilnet.ParseCIDRs([]string{cidr})
	if err != nil {
		return []net.IP{}
	}
	return ListIPsFromCIDR(cidrs[0])
}

func ListIPsStringFromCIDR(cidr string) []string {
	var ipList []string
	cidrs, err := k8sutilnet.ParseCIDRs([]string{cidr})
	if err != nil {
		return ipList
	}
	for _, cidr := range cidrs {
		ips := ListIPsFromCIDR(cidr)
		for _, ip := range ips {
			ipList = append(ipList, ip.String())
		}
	}

	return ipList
}

func ListFirtstAndLastIPStringFromCIDR(cidr string) []net.IP {
	var ipList []net.IP
	cidrs, err := k8sutilnet.ParseCIDRs([]string{cidr})
	if err != nil {
		return ipList
	}
	for _, cidr := range cidrs {
		size := k8sutilnet.RangeSize(cidr)
		if size == 0 {
			return []net.IP{cidr.IP}
		}
		ip, err := k8sutilnet.GetIndexedIP(cidr, 0)
		if err == nil {
			ipList = append(ipList, ip)
		}
		// Exclude first ip
		// The first IP address of the subnet is usually the gateway address
		if size > 1 {
			ip, err := k8sutilnet.GetIndexedIP(cidr, 1)
			if err == nil {
				ipList = append(ipList, ip)
			}
		}
		ip, err = k8sutilnet.GetIndexedIP(cidr, int(size-1))
		if err == nil {
			ipList = append(ipList, ip)
		}
	}

	return ipList
}
