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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/hooks"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	gops "github.com/google/gops/agent"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	plugintypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
)

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

	n, err = plugintypes.LoadNetConf(args.StdinData)
	if err != nil {
		err = fmt.Errorf("unable to parse CNI configuration \"%s\": %s", args.StdinData, err)
		return
	}
	if innerErr := setupLogging(n); innerErr != nil {
		err = fmt.Errorf("unable to setup logging: %w", innerErr)
		return
	}
	logger := logging.DefaultLogger.WithField("mod", "ADD")
	logger = logger.WithField("eventUUID", uuid.New()).
		WithField("containerID", args.ContainerID)

	if n.IPAM.EnableDebug {
		if err := gops.Listen(gops.Options{}); err != nil {
			log.WithError(err).Warn("Unable to start gops")
		} else {
			defer gops.Close()
		}
	}

	logger.Debugf("Processing CNI ADD request %#v", args)

	logger.Debugf("CNI NetConf: %#v", n)
	if n.PrevResult != nil {
		logger.Debugf("CNI Previous result: %#v", n.PrevResult)
	}

	cniArgs := plugintypes.ArgsSpec{}
	if err = cnitypes.LoadArgs(args.Args, &cniArgs); err != nil {
		err = fmt.Errorf("unable to extract CNI arguments: %s", err)
		return
	}
	logger.Debugf("CNI Args: %#v", cniArgs)

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		err = fmt.Errorf("unable to connect to network-v2-agent: %s", client.Hint(err))
		return
	}

	result := &current.Result{CNIVersion: current.ImplementedSpecVersion}
	result.Interfaces = []*current.Interface{{Name: args.IfName, Sandbox: args.Netns}}

	var releaseIPsFunc func(context.Context)
	ipam, releaseIPsFunc, err = allocateIPsWithCCEAgent(c, cniArgs, args.ContainerID, args.Netns)

	// release addresses on failure
	defer func() {
		if err != nil && releaseIPsFunc != nil {
			releaseIPsFunc(context.TODO())
		}
	}()

	if err != nil {
		return
	}

	if !ipv6IsEnabled(ipam) && !ipv4IsEnabled(ipam) {
		err = fmt.Errorf("IPAM did not provide IPv4 or IPv6 address")
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
			return
		}
		ipConfig.Interface = &zoreInterface
		result.IPs = append(result.IPs, ipConfig)
		result.Routes = append(result.Routes, routes...)
	}

	return cnitypes.PrintResult(result, current.ImplementedSpecVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	// Note about when to return errors: kubelet will retry the deletion
	// for a long time. Therefore, only return an error for errors which
	// are guaranteed to be recoverable.
	n, err := plugintypes.LoadNetConf(args.StdinData)
	if err != nil {
		err = fmt.Errorf("unable to parse CNI configuration \"%s\": %s", args.StdinData, err)
		return err
	}

	if err := setupLogging(n); err != nil {
		return fmt.Errorf("unable to setup logging: %w", err)
	}
	logger := logging.DefaultLogger.WithField("mod", "DEL")
	logger = logger.WithField("eventUUID", uuid.New()).
		WithField("containerID", args.ContainerID)

	if n.IPAM.EnableDebug {
		if err := gops.Listen(gops.Options{}); err != nil {
			log.WithError(err).Warn("Unable to start gops")
		} else {
			defer gops.Close()
		}
	}
	logger.Debugf("Processing CNI DEL request %#v", args)

	logger.Debugf("CNI NetConf: %#v", n)

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
	return releaseIP(c, string(owner), args.ContainerID, args.Netns)
}

func allocateIPsWithCCEAgent(client *client.Client, cniArgs plugintypes.ArgsSpec, containerID, netns string) (*models.IPAMResponse, func(context.Context), error) {
	podName := string(cniArgs.K8S_POD_NAMESPACE) + "/" + string(cniArgs.K8S_POD_NAME)
	ipam, err := client.IPAMCNIAllocate("", podName, containerID, netns)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to allocate IP via local cce agent: %w", err)
	}

	if ipam.Address == nil {
		return nil, nil, fmt.Errorf("Invalid IPAM response, missing addressing")
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

func setupLogging(n *plugintypes.NetConf) error {
	f := n.IPAM.LogFormat
	if f == "" {
		f = string(logging.DefaultLogFormat)
	}
	logOptions := logging.LogOptions{
		logging.FormatOpt: f,
	}
	err := logging.SetupLogging([]string{}, logOptions, "cipam", n.IPAM.EnableDebug)
	if err != nil {
		return err
	}

	if len(n.IPAM.LogFile) != 0 {
		logging.AddHooks(hooks.NewFileRotationLogHook(n.IPAM.LogFile,
			hooks.EnableCompression(),
			hooks.WithMaxBackups(1),
		))
	}

	return nil
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
