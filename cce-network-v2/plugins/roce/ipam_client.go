/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package main

import (
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/client"
	plugintypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	"github.com/sirupsen/logrus"
)

type RdmaIPConfig struct {
	Address     net.IP
	MasterMac   string
	VifFeatures string
}

func AllocateIP(ns, podName string, args *skel.CmdArgs, logger *logrus.Entry) ([]*RdmaIPConfig, error) {
	var (
		ipam []*models.RDMAIPAMResponse
	)

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		err = fmt.Errorf("unable to connect to network-v2-agent: %s", client.Hint(err))
		return nil, err
	}

	var releaseIPsFunc func()
	ipam, releaseIPsFunc, err = allocateRDMAIPsWithCCEAgent(c, ns, podName, args.ContainerID, args.Netns, logger)

	// release addresses on failure
	defer func() {
		if err != nil && releaseIPsFunc != nil {
			releaseIPsFunc()
		}
	}()

	if err != nil {
		return nil, err
	}
	if ipam == nil || len(ipam) <= 0 {
		return []*RdmaIPConfig{}, nil
	}

	if !ipv6IsEnabled(ipam) && !ipv4IsEnabled(ipam) {
		return []*RdmaIPConfig{}, nil
	}
	return prepareIP(ipam, ipv6IsEnabled(ipam))
}

func ReleaseIP(args *skel.CmdArgs, logger *logrus.Entry) error {
	cniArgs := plugintypes.ArgsSpec{}
	if err := cnitypes.LoadArgs(args.Args, &cniArgs); err != nil {
		return fmt.Errorf("unable to extract CNI arguments: %s", err)
	}
	logger.Debugf("CNI Args: %#v", cniArgs)

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		// this error can be recovered from
		return fmt.Errorf("unable to connect to CCE daemon: %s", client.Hint(err))
	}
	owner := cniArgs.K8S_POD_NAMESPACE + "/" + cniArgs.K8S_POD_NAME
	return releaseIP(c, string(owner), args.ContainerID, args.Netns, logger)
}

func allocateRDMAIPsWithCCEAgent(client *client.Client, ns, podName string, containerID, netns string, logger *logrus.Entry) ([]*models.RDMAIPAMResponse, func(), error) {
	podName = ns + "/" + podName
	logger.Infof("Allocated RDMAIPAM for pod %s in ns %s for container %s ", podName, netns, containerID)
	ipam, err := client.IPAMCNIRDMAAllocate("", podName, containerID, netns) //([]*models.RDMAIPAMResponse, error)

	if err != nil {
		return nil, nil, fmt.Errorf("unable to allocate IP via local cce agent: %v", err)
	}

	// if ipam == nil || len(ipam) <= 0 {
	// 	return nil, nil, fmt.Errorf("Invalid RdmaIPAM response, missing addressing")
	// }

	releaseFunc := func() {
		releaseIP(client, podName, containerID, netns, logger)
	}

	return ipam, releaseFunc, nil
}

func releaseIP(client *client.Client, owner, containerID, netns string, logger *logrus.Entry) error {
	if owner != "" {
		if err := client.IPAMCNIRDMAReleaseIP(owner, containerID, netns); err != nil {
			logger.WithError(err).WithField(logfields.Object, owner).Warn("Unable to release Rdma IP")
			return err
		}
	}
	return nil
}

func prepareIP(ipams []*models.RDMAIPAMResponse, isIPv6 bool) ([]*RdmaIPConfig, error) {
	var (
		rdmaIpConfig []*RdmaIPConfig
	)

	if isIPv6 {
		for _, ipam := range ipams {
			targetIP := net.ParseIP(ipam.IPV6.IP)
			mac := ipam.IPV6.MasterMac
			rdmaIpConfig = append(rdmaIpConfig, &RdmaIPConfig{Address: targetIP, MasterMac: mac, VifFeatures: ipam.IPV4.InterfaceNumber})
		}
	} else {
		for _, ipam := range ipams {
			targetIP := net.ParseIP(ipam.IPV4.IP)
			mac := ipam.IPV4.MasterMac
			rdmaIpConfig = append(rdmaIpConfig, &RdmaIPConfig{Address: targetIP, MasterMac: mac, VifFeatures: ipam.IPV4.InterfaceNumber})
		}
	}

	return rdmaIpConfig, nil
}

func ipv6IsEnabled(ipam []*models.RDMAIPAMResponse) bool {
	if ipam == nil || len(ipam) <= 0 || ipam[0].Address.IPV6 == "" {
		return false
	}

	if ipam[0].HostAddressing != nil && ipam[0].HostAddressing.IPV6 != nil {
		return ipam[0].HostAddressing.IPV6.Enabled
	}

	return true
}

func ipv4IsEnabled(ipam []*models.RDMAIPAMResponse) bool {
	if ipam == nil || len(ipam) <= 0 || ipam[0].Address.IPV4 == "" {
		return false
	}

	if ipam[0].HostAddressing != nil && ipam[0].HostAddressing.IPV4 != nil {
		return ipam[0].HostAddressing.IPV4.Enabled
	}

	return true
}
