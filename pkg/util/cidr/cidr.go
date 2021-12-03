package cidr

import "net"

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
