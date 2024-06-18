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
	"strconv"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/client/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/client"
	plugintypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	iputils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	gops "github.com/google/gops/agent"
	"github.com/sirupsen/logrus"

	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
)

var logger *logrus.Entry

func main() {
	skel.PluginMain(cmdAdd, nil, cmdDel, version.All, bv.BuildString("enim"))
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	var (
		n        *plugintypes.NetConf
		ipam     *models.IPAMResponse
		ipConfig *current.IPConfig
		routes   []*cnitypes.Route
		inter    *current.Interface
	)

	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "enim",
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

	logger.Info("processing CNI ADD request")

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
		err = fmt.Errorf("unable to connect to CCE ipam v2 daemon: %s", client.Hint(err))
		return
	}

	result := &current.Result{CNIVersion: current.ImplementedSpecVersion}

	var releaseIPsFunc func(context.Context)
	ipam, releaseIPsFunc, err = allocateENIWithCCEAgent(c, cniArgs, args.ContainerID, args.Netns)

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

	wrapperResult := func(ipv6 bool) error {
		ipConfig, routes, inter, err = wrapperENI(ipam, n, ipv6)
		if err != nil {
			return fmt.Errorf("enim cni unable to prepare IP addressing: %v", err)
		}
		result.IPs = append(result.IPs, ipConfig)
		result.Routes = append(result.Routes, routes...)
		result.Interfaces = append(result.Interfaces, inter)
		return nil
	}
	if ipv6IsEnabled(ipam) {
		wrapperResult(true)
	}

	if ipv4IsEnabled(ipam) {
		wrapperResult(false)
	}

	return cnitypes.PrintResult(result, current.ImplementedSpecVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	var err error
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "enim",
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
	var n *plugintypes.NetConf
	n, err = plugintypes.LoadNetConf(args.StdinData)
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
	logger.Infof("Processing CNI DEL request %#v", args)

	cniArgs := plugintypes.ArgsSpec{}
	if err = cnitypes.LoadArgs(args.Args, &cniArgs); err != nil {
		return fmt.Errorf("unable to extract CNI arguments: %s", err)
	}

	var c *client.Client
	c, err = client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		// this error can be recovered from
		return fmt.Errorf("unable to connect to CCE daemon: %s", client.Hint(err))
	}
	owner := cniArgs.K8S_POD_NAMESPACE + "/" + cniArgs.K8S_POD_NAME

	err = releaseENI(c, string(owner), args.ContainerID, args.Netns)
	return err
}

func allocateENIWithCCEAgent(client *client.Client, cniArgs plugintypes.ArgsSpec, containerID, netns string) (
	*models.IPAMResponse, func(context.Context), error) {
	podName := string(cniArgs.K8S_POD_NAMESPACE) + "/" + string(cniArgs.K8S_POD_NAME)
	params := eni.NewPostEniParams().WithOwner(&podName).WithContainerID(&containerID).WithNetns(&netns)

	resp, err := client.Eni.PostEni(params)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to allocate IP via local enim: %w", err)
	}
	payload := resp.Payload

	if payload.Address == nil {
		return nil, nil, fmt.Errorf("Invalid enim response, missing addressing")
	}

	// release ENI while CNI ADD error
	releaseFunc := func(context.Context) {
		releaseENI(client, podName, containerID, netns)
	}

	return payload, releaseFunc, nil
}

func releaseENI(client *client.Client, owner, containerID, netns string) error {
	deleteParams := eni.NewDeleteEniParams().WithOwner(&owner).WithContainerID(&containerID).WithNetns(&netns)
	_, err := client.Eni.DeleteEni(deleteParams)

	return err
}

func wrapperENI(ipam *models.IPAMResponse, n *plugintypes.NetConf, isIPv6 bool) (*current.IPConfig, []*cnitypes.Route, *current.Interface, error) {
	var (
		routes           []*cnitypes.Route
		inter            = &current.Interface{}
		gw               net.IP
		mask             net.IPMask
		defaultDst       *net.IPNet                  = iputils.IPv4ZeroCIDR
		address          *models.IPAMAddressResponse = ipam.IPV4
		ipFamilyFunction                             = func(ip net.IP) bool { return ip != nil && ip.To4() != nil }
	)

	if isIPv6 {
		defaultDst = iputils.IPv6ZeroCIDR
		address = ipam.IPV6
		ipFamilyFunction = func(ip net.IP) bool { return ip != nil && ip.To4() == nil }
	}

	for _, route := range n.IPAM.Routes {
		if ipFamilyFunction(route.GW) {
			routes = append(routes, &cnitypes.Route{Dst: route.Dst, GW: route.GW})
		}
	}

	ip := net.ParseIP(address.IP)

	for i := range address.Cidrs {
		_, ipnet, err := net.ParseCIDR(address.Cidrs[i])
		if err == nil {
			mask = ipnet.Mask
		}
	}

	gw = net.ParseIP(address.Gateway)
	// add default route
	if gw != nil {
		routes = append(routes, &cnitypes.Route{GW: gw, Dst: *defaultDst})
	}
	if gw == nil {
		return nil, nil, nil, fmt.Errorf("invalid gateway address: %s", gw)
	}

	index, err := strconv.Atoi(address.InterfaceNumber)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid interface number: %s", address.InterfaceNumber)
	}

	inter.Mac = address.MasterMac
	return &current.IPConfig{
		Interface: &index,
		Address:   net.IPNet{IP: ip, Mask: mask},
		Gateway:   gw,
	}, routes, inter, nil
}

func ipv6IsEnabled(ipam *models.IPAMResponse) bool {
	if ipam == nil || ipam.Address.IPV6 == "" {
		return false
	}

	return true
}

func ipv4IsEnabled(ipam *models.IPAMResponse) bool {
	if ipam == nil || ipam.Address.IPV4 == "" {
		return false
	}

	return true
}
