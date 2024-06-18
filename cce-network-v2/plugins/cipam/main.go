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
	"context"
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/netns"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	gops "github.com/google/gops/agent"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"

	plugintypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
)

var logger *log.Entry

func main() {
	skel.PluginMain(cmdAdd, nil, cmdDel, version.All, bv.BuildString("cipam"))
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	var (
		n        *plugintypes.NetConf
		ipam     *models.IPAMResponse
		ipConfig *current.IPConfig
		routes   []*cnitypes.Route
	)

	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "cipam",
		"mod":     "ADD",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()

	n, err = plugintypes.LoadNetConf(args.StdinData)
	if err != nil {
		err = fmt.Errorf("unable to parse CNI configuration \"%s\": %s", args.StdinData, err)
		return
	}

	if n.IPAM.EnableDebug {
		if err := gops.Listen(gops.Options{}); err != nil {
			logger.WithError(err).Warn("Unable to start gops")
		} else {
			defer gops.Close()
		}
	}

	logger.WithField("netConf", logfields.Json(n)).Infof("Processing CNI ADD request %#v", args)

	cniArgs := plugintypes.ArgsSpec{}
	if err = cnitypes.LoadArgs(args.Args, &cniArgs); err != nil {
		err = fmt.Errorf("unable to extract CNI arguments: %s", err)
		return
	}

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		err = fmt.Errorf("unable to connect to network-v2-agent: %s", client.Hint(err))
		logger.WithError(err).Error("unable to connect to network-v2-agent")
		return
	}

	result := &current.Result{CNIVersion: current.ImplementedSpecVersion}
	result.Interfaces = []*current.Interface{{Name: args.IfName, Sandbox: args.Netns}}

	nns, err := ns.GetNS(args.Netns)
	nns.Path()
	ns, err := netns.GetProcNSPath(args.Netns)
	if err != nil {
		logger.Warning("unable to get netns path from procfs, use the netns from args")
		ns = args.Netns
	}
	var releaseIPsFunc func(context.Context)
	ipam, releaseIPsFunc, err = allocateIPsWithCCEAgent(c, cniArgs, args.ContainerID, ns)
	// release addresses on failure
	defer func() {
		if err != nil && releaseIPsFunc != nil {
			logger.WithError(err).Warn("do release IPs")
			releaseIPsFunc(context.TODO())
		}
	}()
	if err != nil {
		logger.WithError(err).Error("unable to allocate IP addresses with cce agent")
		return
	}
	logger.WithField("agentResult", logfields.Json(ipam)).Infof("success allocated IP addresses with cce agent")

	if !ipv6IsEnabled(ipam) && !ipv4IsEnabled(ipam) {
		err = fmt.Errorf("IPAM did not provide IPv4 or IPv6 address")
		logger.WithError(err).Error("unable to allocate IP addresses with cce agent")
		return
	}

	zoreInterface := 0
	if ipv6IsEnabled(ipam) {
		ipConfig, routes, err = prepareIP(ipam, n, true)
		if err != nil {
			err = fmt.Errorf("unable to prepare IP addressing for '%s': %s", ipam.IPV6.IP, err)
			return
		}
		ipConfig.Interface = &zoreInterface
		result.IPs = append(result.IPs, ipConfig)
		result.Routes = append(result.Routes, routes...)
	}

	if ipv4IsEnabled(ipam) {
		ipConfig, routes, err = prepareIP(ipam, n, false)
		if err != nil {
			err = fmt.Errorf("unable to prepare IP addressing for '%s': %s", ipam.IPV4.IP, err)
			logger.WithError(err).Error("unable to prepare IP addresses")
			return
		}
		ipConfig.Interface = &zoreInterface
		result.IPs = append(result.IPs, ipConfig)
		result.Routes = append(result.Routes, routes...)
	}

	logger.WithField("result", logfields.Json(result)).Info("success to exec ipam add")

	return cnitypes.PrintResult(result, current.ImplementedSpecVersion)
}

func cmdDel(args *skel.CmdArgs) (err error) {
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "cipam",
		"mod":     "DEL",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()
	// Note about when to return errors: kubelet will retry the deletion
	// for a long time. Therefore, only return an error for errors which
	// are guaranteed to be recoverable.
	n, err := plugintypes.LoadNetConf(args.StdinData)
	if err != nil {
		err = fmt.Errorf("unable to parse CNI configuration \"%s\": %s", args.StdinData, err)
		return err
	}

	if n.IPAM.EnableDebug {
		if err := gops.Listen(gops.Options{}); err != nil {
			logger.WithError(err).Warn("Unable to start gops")
		} else {
			defer gops.Close()
		}
	}
	logger.Info("Processing CNI DEL request")

	cniArgs := plugintypes.ArgsSpec{}
	if err = cnitypes.LoadArgs(args.Args, &cniArgs); err != nil {
		return fmt.Errorf("unable to extract CNI arguments: %s", err)
	}
	logger.Debugf("CNI Args: %#v", cniArgs)

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		// this error can be recovered from
		return fmt.Errorf("unable to connect to CCE daemon: %s", client.Hint(err))
	}
	owner := cniArgs.K8S_POD_NAMESPACE + "/" + cniArgs.K8S_POD_NAME
	err = releaseIP(c, string(owner), args.ContainerID, args.Netns)
	if err != nil {
		return fmt.Errorf("unable to release IP: %w", err)
	}

	logger.Info("success to exec ipam del")
	return nil
}

func allocateIPsWithCCEAgent(client *client.Client, cniArgs plugintypes.ArgsSpec, containerID, netns string) (*models.IPAMResponse, func(context.Context), error) {
	podName := string(cniArgs.K8S_POD_NAMESPACE) + "/" + string(cniArgs.K8S_POD_NAME)
	ipam, err := client.IPAMCNIAllocate("", podName, containerID, netns)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to allocate IP via local cce agent: %w", err)
	}

	if ipam.Address == nil {
		return nil, nil, fmt.Errorf("invalid IPAM response, missing addressing")
	}

	releaseFunc := func(context.Context) {
		if ipam.Address != nil {
			releaseIP(client, podName, containerID, netns)
		}
	}

	return ipam, releaseFunc, nil
}

func releaseIP(client *client.Client, owner, containerID, netns string) error {
	if owner != "" {
		if err := client.IPAMCNIReleaseIP(owner, containerID, netns); err != nil {
			log.WithError(err).WithField(logfields.Object, owner).Warn("Unable to release IP")
			return err
		}
	}
	return nil
}

func prepareIP(ipam *models.IPAMResponse, n *plugintypes.NetConf, isIPv6 bool) (*current.IPConfig, []*cnitypes.Route, error) {
	var (
		routes   []*cnitypes.Route
		targetIP net.IP
		ipnet    *net.IPNet
		gw       net.IP
		mask     net.IPMask
	)

	if isIPv6 {
		targetIP = net.ParseIP(ipam.IPV6.IP)
		if !n.IPAM.UseHostGateWay {
			for _, cidrstr := range ipam.IPV6.Cidrs {
				_, ipnets, err := net.ParseCIDR(cidrstr)
				if err == nil {
					mask = ipnets.Mask
				}
			}
		}

		// If we don't have a remote route, we need to use the host's route
		if len(mask) == 0 {
			mask = net.CIDRMask(128, 128)
		}
		ipnet = &net.IPNet{
			IP:   targetIP,
			Mask: mask,
		}

		gw = net.ParseIP(ipam.IPV6.Gateway)
		if gw == nil {
			// If we don't have a remote route, we need to use the host's route
			gw = net.ParseIP(ipam.HostAddressing.IPV6.IP)
		}
		if gw != nil {
			routes = append(routes, &cnitypes.Route{GW: gw, Dst: *ip.IPv6ZeroCIDR})
		}
	} else {
		targetIP = net.ParseIP(ipam.IPV4.IP)
		if !n.IPAM.UseHostGateWay {
			for _, cidrstr := range ipam.IPV4.Cidrs {
				_, ipnets, err := net.ParseCIDR(cidrstr)
				if err == nil {
					mask = ipnets.Mask
				}
			}
		}
		if len(mask) == 0 {
			mask = net.CIDRMask(32, 32)
		}
		ipnet = &net.IPNet{
			IP:   targetIP,
			Mask: mask,
		}

		gw = net.ParseIP(ipam.IPV4.Gateway)
		if gw == nil {
			// If we don't have a remote route, we need to use the host's route
			gw = net.ParseIP(ipam.HostAddressing.IPV4.IP)
		}
		if gw != nil {
			routes = append(routes, &cnitypes.Route{GW: gw, Dst: *ip.IPv4ZeroCIDR})
		}
	}

	for _, route := range n.IPAM.Routes {
		routes = append(routes, &cnitypes.Route{Dst: route.Dst, GW: route.GW})
	}

	if gw == nil {
		return nil, nil, fmt.Errorf("invalid gateway address: %s", gw)
	}

	return &current.IPConfig{
		Address: *ipnet,
		Gateway: gw,
	}, routes, nil
}

func ipv6IsEnabled(ipam *models.IPAMResponse) bool {
	if ipam == nil || ipam.Address.IPV6 == "" {
		return false
	}

	if ipam.HostAddressing != nil && ipam.HostAddressing.IPV6 != nil {
		return ipam.HostAddressing.IPV6.Enabled
	}

	return true
}

func ipv4IsEnabled(ipam *models.IPAMResponse) bool {
	if ipam == nil || ipam.Address.IPV4 == "" {
		return false
	}

	if ipam.HostAddressing != nil && ipam.HostAddressing.IPV4 != nil {
		return ipam.HostAddressing.IPV4.Enabled
	}

	return true
}
