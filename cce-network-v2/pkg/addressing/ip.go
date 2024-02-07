/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package addressing

import (
	"encoding/json"
	"fmt"
	"net"
)

type CCEIP interface {
	EndpointPrefix() *net.IPNet
	IP() net.IP
	String() string
	IsIPv6() bool
	GetFamilyString() string
	IsSet() bool
}

type CCEIPv6 []byte

// NewCCEIPv6 returns a IPv6 if the given `address` is an IPv6 address.
func NewCCEIPv6(address string) (CCEIPv6, error) {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
		if ip == nil {
			return nil, fmt.Errorf("Invalid IPv6 address: %s", address)
		}
	}

	// As result of ParseIP, ip is either a valid IPv6 or IPv4 address. net.IP
	// represents both versions on 16 bytes, so a more reliable way to tell
	// IPv4 and IPv6 apart is to see if it fits 4 bytes
	ip4 := ip.To4()
	if ip4 != nil {
		return nil, fmt.Errorf("Not an IPv6 address: %s", address)
	}
	return DeriveCCEIPv6(ip.To16()), nil
}

func DeriveCCEIPv6(src net.IP) CCEIPv6 {
	ip := make(CCEIPv6, 16)
	copy(ip, src.To16())
	return ip
}

// IsSet returns true if the IP is set
func (ip CCEIPv6) IsSet() bool {
	return ip.String() != ""
}

func (ip CCEIPv6) IsIPv6() bool {
	return true
}

func (ip CCEIPv6) EndpointPrefix() *net.IPNet {
	return &net.IPNet{
		IP:   ip.IP(),
		Mask: net.CIDRMask(128, 128),
	}
}

func (ip CCEIPv6) IP() net.IP {
	return net.IP(ip)
}

func (ip CCEIPv6) String() string {
	if ip == nil {
		return ""
	}

	return net.IP(ip).String()
}

func (ip CCEIPv6) MarshalJSON() ([]byte, error) {
	return json.Marshal(net.IP(ip))
}

func (ip *CCEIPv6) UnmarshalJSON(b []byte) error {
	if len(b) < len(`""`) {
		return fmt.Errorf("Invalid CCEIPv6 '%s'", string(b))
	}

	str := string(b[1 : len(b)-1])
	if str == "" {
		return nil
	}

	c, err := NewCCEIPv6(str)
	if err != nil {
		return fmt.Errorf("Invalid CCEIPv6 '%s': %s", str, err)
	}

	*ip = c
	return nil
}

type CCEIPv4 []byte

func NewCCEIPv4(address string) (CCEIPv4, error) {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		ip = net.ParseIP(address)
		if ip == nil {
			return nil, fmt.Errorf("Invalid IPv4 address: %s", address)
		}
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("Not an IPv4 address")
	}
	return DeriveCCEIPv4(ip4), nil
}

func DeriveCCEIPv4(src net.IP) CCEIPv4 {
	ip := make(CCEIPv4, 4)
	copy(ip, src.To4())
	return ip
}

// IsSet returns true if the IP is set
func (ip CCEIPv4) IsSet() bool {
	return ip.String() != ""
}

func (ip CCEIPv4) IsIPv6() bool {
	return false
}

func (ip CCEIPv4) EndpointPrefix() *net.IPNet {
	return &net.IPNet{
		IP:   net.IP(ip),
		Mask: net.CIDRMask(32, 32),
	}
}

func (ip CCEIPv4) IP() net.IP {
	return net.IP(ip)
}

func (ip CCEIPv4) String() string {
	if ip == nil {
		return ""
	}

	return net.IP(ip).String()
}

func (ip CCEIPv4) MarshalJSON() ([]byte, error) {
	return json.Marshal(net.IP(ip))
}

func (ip *CCEIPv4) UnmarshalJSON(b []byte) error {
	if len(b) < len(`""`) {
		return fmt.Errorf("Invalid CCEIPv4 '%s'", string(b))
	}

	str := string(b[1 : len(b)-1])
	if str == "" {
		return nil
	}

	c, err := NewCCEIPv4(str)
	if err != nil {
		return fmt.Errorf("Invalid CCEIPv4 '%s': %s", str, err)
	}

	*ip = c
	return nil
}

// GetFamilyString returns the address family of ip as a string.
func (ip CCEIPv4) GetFamilyString() string {
	return "IPv4"
}

// GetFamilyString returns the address family of ip as a string.
func (ip CCEIPv6) GetFamilyString() string {
	return "IPv6"
}
