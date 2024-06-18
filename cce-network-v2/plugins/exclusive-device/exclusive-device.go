// Copyright 2015 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/vishvananda/netlink"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/sirupsen/logrus"
)

const (
	defaultLinkNameInPod = "eth0"
)

var logger *logrus.Entry

// NetConf for exclusive-device config, look the README to learn how to use those parameters
type NetConf struct {
	types.NetConf
}

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func loadConf(bytes []byte) (*NetConf, error) {
	n := &NetConf{}
	var err error
	if err = json.Unmarshal(bytes, n); err != nil {
		return nil, fmt.Errorf("exclusive-device failed to load netconf: %v", err)
	}

	return n, nil
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "exclusive-device",
		"mod":     "ADD",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()

	cfg, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}
	if cfg.IPAM.Type == "" {
		return fmt.Errorf("ipam must be specifed")
	}

	// run the IPAM plugin and get back the config to apply
	r, err := ipam.ExecAdd(cfg.IPAM.Type, args.StdinData)
	if err != nil {
		return fmt.Errorf("exclusive-device failed to set up IPAM plugin type %q: %v", cfg.IPAM.Type, err)
	}
	logger.WithField("ipamResult", logfields.Repr(r)).Infof("exec add ipam success")
	// Invoke ipam del if err to avoid ip leak
	defer func() {
		if err != nil {
			err = ipam.ExecDel(cfg.IPAM.Type, args.StdinData)
			if err != nil {
				logger.Errorf("exclusive-device failed to clean up IPAM plugin type %q: %v", cfg.IPAM.Type, err)
			} else {
				logger.Infof("exclusive-device clean up IPAM plugin type %q success", cfg.IPAM.Type)
			}
		}
	}()

	// Convert whatever the IPAM result was into the current Result type
	newResult, err := current.NewResultFromResult(r)
	if err != nil {
		return fmt.Errorf("exclusive-device could not convert result to current version: %v", err)
	}
	if len(newResult.IPs) == 0 {
		return errors.New("IPAM plugin returned missing IP config")
	}
	if len(newResult.Interfaces) == 0 {
		return errors.New("IPAM plugin returned missing Interface config")
	}

	// get host device
	hostDevIndex := *(newResult.IPs[0].Interface)
	hostDev, err := netlink.LinkByIndex(hostDevIndex)
	if err != nil {
		return fmt.Errorf("failed to find host device with index %d: %v", hostDevIndex, err)
	}

	// move device from host into container ns
	containerNs, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer containerNs.Close()
	_, err = moveLinkIn(hostDev, containerNs, defaultLinkNameInPod)
	if err != nil {
		return fmt.Errorf("failed to move link %v", err)
	}
	logger.Infof("move link %s into container ns %s success", hostDev.Attrs().Name, args.Netns)

	err = containerNs.Do(func(_ ns.NetNS) error {
		if err := ConfigureIface(args.IfName, newResult); err != nil {
			return fmt.Errorf("exclusive-device failed to configure interface %q: %v", args.IfName, err)
		}
		logger.Infof("configure interface %s success", args.IfName)
		return nil
	})
	if err != nil {
		return err
	}

	// it is necessary to repleace the interface index while the container runtime is containerd
	// otherwise, containerd will occur the error: invalid interface config: invalid IP configuration
	// (interface number 11 is > number of interfaces 1): invalid result
	defaultIndex := 0
	newResult.Interfaces[0].Name = args.IfName
	newResult.IPs[0].Interface = &defaultIndex
	newResult.DNS = cfg.DNS

	logger.WithField("result", newResult).Infof("successfully exec plugin")
	return types.PrintResult(newResult, cfg.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	cfg, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "exclusive-device",
		"mod":     "DEL",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()

	if args.Netns == "" {
		return nil
	}
	containerNs, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer containerNs.Close()

	if cfg.IPAM.Type != "" {
		if err := ipam.ExecDel(cfg.IPAM.Type, args.StdinData); err != nil {
			return fmt.Errorf("failed to clean up IPAM plugin type %q: %v", cfg.IPAM.Type, err)
		}
		logger.Infof("clean up IPAM plugin type %q success", cfg.IPAM.Type)
	}

	if err := moveLinkOut(containerNs, defaultLinkNameInPod); err != nil {
		return err
	}
	logger.Infof("move link %s out of container ns %s success", defaultLinkNameInPod, args.Netns)
	return nil
}

func moveLinkIn(hostDev netlink.Link, containerNs ns.NetNS, ifName string) (netlink.Link, error) {
	if err := netlink.LinkSetNsFd(hostDev, int(containerNs.Fd())); err != nil {
		return nil, err
	}

	var contDev netlink.Link
	if err := containerNs.Do(func(_ ns.NetNS) error {
		var err error
		contDev, err = netlink.LinkByName(hostDev.Attrs().Name)
		if err != nil {
			return fmt.Errorf("failed to find %q: %v", hostDev.Attrs().Name, err)
		}
		// Devices can be renamed only when down
		if err = netlink.LinkSetDown(contDev); err != nil {
			return fmt.Errorf("failed to set %q down: %v", hostDev.Attrs().Name, err)
		}
		// Save host device name into the container device's alias property
		if hostDev.Attrs().Alias == "" {
			if err := netlink.LinkSetAlias(contDev, hostDev.Attrs().Name); err != nil {
				return fmt.Errorf("failed to set alias to %q: %v", hostDev.Attrs().Name, err)
			}
		}
		// Rename container device to respect args.IfName
		if err := netlink.LinkSetName(contDev, ifName); err != nil {
			return fmt.Errorf("failed to rename device %q to %q: %v", hostDev.Attrs().Name, ifName, err)
		}
		// Bring container device up
		if err = netlink.LinkSetUp(contDev); err != nil {
			return fmt.Errorf("failed to set %q up: %v", ifName, err)
		}
		// Retrieve link again to get up-to-date name and attributes
		contDev, err = netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to find %q: %v", ifName, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return contDev, nil
}

func moveLinkOut(containerNs ns.NetNS, ifName string) error {
	defaultNs, err := ns.GetCurrentNS()
	if err != nil {
		return err
	}
	defer defaultNs.Close()

	return containerNs.Do(func(_ ns.NetNS) error {
		dev, err := netlink.LinkByName(ifName)
		if err != nil {
			// Do not attempt to move if dev does not exist
			// dev may has been moved by enim
			return nil
		}
		if dev.Attrs().Alias == "" {
			return nil
		}
		return link.MoveAndRenameLink(dev, defaultNs, dev.Attrs().Alias)
	})
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("exclusive-device"))
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

// ConfigureIface takes the result of IPAM plugin and
// applies to the ifName interface
func ConfigureIface(ifName string, res *current.Result) error {
	if len(res.Interfaces) == 0 {
		return fmt.Errorf("no interfaces to configure")
	}

	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", ifName, err)
	}

	var v4gw, v6gw net.IP
	var has_enabled_ipv6 bool = false
	for _, ipc := range res.IPs {
		if ipc.Interface == nil {
			continue
		}

		// Make sure sysctl "disable_ipv6" is 0 if we are about to add
		// an IPv6 address to the interface
		if !has_enabled_ipv6 && ipc.Address.IP.To4() == nil {
			// Enabled IPv6 for loopback "lo" and the interface
			// being configured
			for _, iface := range [2]string{"lo", ifName} {
				ipv6SysctlValueName := fmt.Sprintf(ipam.DisableIPv6SysctlTemplate, iface)

				// Read current sysctl value
				value, err := sysctl.Sysctl(ipv6SysctlValueName)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ipam_linux: failed to read sysctl %q: %v\n", ipv6SysctlValueName, err)
					continue
				}
				if value == "0" {
					continue
				}

				// Write sysctl to enable IPv6
				_, err = sysctl.Sysctl(ipv6SysctlValueName, "0")
				if err != nil {
					return fmt.Errorf("failed to enable IPv6 for interface %q (%s=%s): %v", iface, ipv6SysctlValueName, value, err)
				}
			}
			has_enabled_ipv6 = true
		}

		addr := &netlink.Addr{IPNet: &ipc.Address, Label: ""}
		if err = netlink.AddrAdd(link, addr); err != nil {
			return fmt.Errorf("failed to add IP addr %v to %q: %v", ipc, ifName, err)
		}

		gwIsV4 := ipc.Gateway.To4() != nil
		if gwIsV4 && v4gw == nil {
			v4gw = ipc.Gateway
		} else if !gwIsV4 && v6gw == nil {
			v6gw = ipc.Gateway
		}
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set %q UP: %v", ifName, err)
	}

	if v6gw != nil {
		ip.SettleAddresses(ifName, 10)
	}

	for _, r := range res.Routes {
		routeIsV4 := r.Dst.IP.To4() != nil
		gw := r.GW
		if gw == nil {
			if routeIsV4 && v4gw != nil {
				gw = v4gw
			} else if !routeIsV4 && v6gw != nil {
				gw = v6gw
			}
		}
		route := netlink.Route{
			Dst:       &r.Dst,
			LinkIndex: link.Attrs().Index,
			Gw:        gw,
		}

		if err = netlink.RouteAddEcmp(&route); err != nil {
			return fmt.Errorf("failed to add route '%v via %v dev %v': %v", r.Dst, gw, ifName, err)
		}
	}

	return nil
}
