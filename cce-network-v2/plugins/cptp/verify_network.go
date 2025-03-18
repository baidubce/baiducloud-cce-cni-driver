package main

import (
	"fmt"
	"net"
	"os"
	"time"

	current "github.com/containernetworking/cni/pkg/types/100"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// icmpProbeTimeout for sending ICMP packets
var icmpProbeTimeout = 500 * time.Millisecond

// NetworkChecker interface defines methods for network utilities
type NetworkChecker interface {
	GetDefaultGateway() (net.IP, error)
	SendIcmpProbeInNetNS(netns ns.NetNS, srcIP net.IP, targetIP net.IP, mtu int) (bool, error)
}

// DefalutNetworkChecker implements the NetworkUtils interface
type DefalutNetworkChecker struct{}

func (d *DefalutNetworkChecker) GetDefaultGateway() (net.IP, error) {
	return getDefaultGateway()
}

func (d *DefalutNetworkChecker) SendIcmpProbeInNetNS(netns ns.NetNS, srcIP net.IP, targetIP net.IP, mtu int) (bool, error) {
	return sendIcmpProbeInNetNS(netns, srcIP, targetIP, mtu)
}

// Get the default gateway for IPv4
func getDefaultGateway() (net.IP, error) {
	// Get all IPv4 routes
	routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to get routes: %v", err)
	}

	// Find the default route
	for _, route := range routes {
		// Check if the route is the default route (Dst is 0.0.0.0/0)
		if route.Dst == nil || (route.Dst != nil && route.Dst.IP.Equal(net.IPv4zero) && route.Dst.IP.String() == "0.0.0.0") {
			if route.Gw != nil {
				return route.Gw, nil
			}
		}
	}

	return nil, fmt.Errorf("no default gateway found")
}

// sendIcmpProbeInNetNS sends ICMP Echo Requests from the specified source IP to the target IP
// within the given network namespace (netns). It sends multiple ICMP packets and returns
// true if it receives at least one valid ICMP Echo Reply within the timeout period.
// The function returns false if no reply is received or if an error occurs
func sendIcmpProbeInNetNS(netns ns.NetNS, srcIP net.IP, targetIP net.IP, mtu int) (bool, error) {
	reachable := false
	var err error

	// Execute operations in the target network namespace
	err = netns.Do(func(_ ns.NetNS) error {
		// Validate source and target IPs
		if srcIP == nil || srcIP.To4() == nil {
			return fmt.Errorf("invalid source IP: %s", srcIP)
		}
		if targetIP == nil || targetIP.To4() == nil {
			return fmt.Errorf("invalid target IP: %s", targetIP)
		}

		// Create an ICMP connection
		conn, err := icmp.ListenPacket("ip4:icmp", srcIP.String())
		if err != nil {
			return fmt.Errorf("failed to create ICMP connection: %v", err)
		}
		defer conn.Close()

		// Send multiple ICMP Echo Requests
		targetAddr := &net.IPAddr{IP: targetIP}
		numPings := 3 // Send 3 ICMP packets
		for i := 0; i < numPings; i++ {
			// Construct an ICMP Echo Request
			msg := icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Code: 0,
				Body: &icmp.Echo{
					ID:   os.Getpid() & 0xffff,
					Seq:  i + 1, // Increment sequence number
					Data: []byte("HELLO"),
				},
			}
			msgBytes, err := msg.Marshal(nil)
			if err != nil {
				return fmt.Errorf("failed to marshal ICMP message: %v", err)
			}

			// Send the ICMP Echo Request
			_, err = conn.WriteTo(msgBytes, targetAddr)
			if err != nil {
				return fmt.Errorf("failed to send ICMP message: %v", err)
			}

			// Set a timeout for receiving
			if err := conn.SetReadDeadline(time.Now().Add(icmpProbeTimeout)); err != nil {
				return fmt.Errorf("failed to set read deadline: %v", err)
			}

			// Receive an ICMP Echo Reply
			reply := make([]byte, mtu)
			n, _, err := conn.ReadFrom(reply)
			if err != nil {
				// If timeout occurs, continue to send the next ICMP packet
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				return fmt.Errorf("failed to receive ICMP reply: %v", err)
			}

			// Parse the ICMP Echo Reply
			parsedReply, err := icmp.ParseMessage(1, reply[:n]) // ICMPv4
			if err != nil {
				return fmt.Errorf("failed to parse ICMP reply: %v", err)
			}

			// Check if it's an Echo Reply
			if parsedReply.Type == ipv4.ICMPTypeEchoReply {
				reachable = true
				break // Exit the loop upon receiving a valid Echo Reply
			}
		}

		return nil
	})

	return reachable, err
}

// VerifyNetworkConnectivity verifies network connectivity for a list of IP addresses.
// It skips non-IPv4 addresses, determines the appropriate gateway, and pings the gateway
// to ensure connectivity.
func VerifyNetworkConnectivity(result *current.Result, netns ns.NetNS, mtu int, nc NetworkChecker) error {
	for _, IP := range result.IPs {
		// Skip non-IPv4 addresses
		if IP.Address.IP.To4() == nil {
			logger.Debugf("skipping non-IPv4 address: %s", IP.Address)
			continue
		}
		startTime := time.Now()

		// Determine the gateway to use
		gateway := IP.Gateway
		maskSize, _ := IP.Address.Mask.Size()

		// If the mask size is 32, the cni mode is vpc route
		if maskSize == 32 {
			defaultGateway, err := nc.GetDefaultGateway()
			if err != nil {
				return fmt.Errorf("failed to get default gateway for vpc route mode: %w", err)
			}
			gateway = defaultGateway
		}

		// Ping the gateway to verify network connectivity
		reachable, err := nc.SendIcmpProbeInNetNS(netns, IP.Address.IP, gateway, mtu)
		if err != nil {
			return NewIPRelatedError(IP.Address.String(), fmt.Sprintf("failed to verify network connectivity: %v", err))
		}
		if !reachable {
			return NewIPRelatedError(IP.Address.String(), fmt.Sprintf("failed to ping gateway %s", gateway))
		}

		executionTime := time.Since(startTime)

		logger.Infof("successfully verified network connectivity for IP %s, time cost %s", IP.Address, executionTime.String())
	}
	return nil
}
