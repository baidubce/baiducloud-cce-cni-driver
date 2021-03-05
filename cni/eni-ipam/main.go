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
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/vishvananda/netlink"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
)

const (
	logFile = "/var/log/cce/cni-eni-ipam.log"

	toContainerRulePriority   = 512
	fromContainerRulePriority = 1536

	mainRouteTableID          = 254
	defaultRouteTableIDOffset = 127
)

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

// K8SArgs k8s pod args
type K8SArgs struct {
	types.CommonArgs `json:"commonArgs"`
	// IP is pod's ip address
	IP net.IP `json:"ip"`
	// K8S_POD_NAME is pod's name
	K8S_POD_NAME types.UnmarshallableString `json:"k8s_pod_name"`
	// K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_NAMESPACE types.UnmarshallableString `json:"k8s_pod_namespace"`
	// K8S_POD_INFRA_CONTAINER_ID is pod's container ID
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString `json:"k8s_pod_infra_container_id"`
}

// loadIPAMConf
func loadIPAMConf(stdinData []byte) (*IPAMConf, string, error) {
	n := NetConf{}
	if err := json.Unmarshal(stdinData, &n); err != nil {
		return nil, "", err
	}

	if n.IPAM == nil {
		return nil, "", fmt.Errorf("IPAM config missing 'ipam' key")
	}

	if n.IPAM.Endpoint == "" {
		return nil, "", fmt.Errorf("ipam endpoint is empty")
	}

	if n.IPAM.RouteTableIDOffset == 0 {
		n.IPAM.RouteTableIDOffset = defaultRouteTableIDOffset
	}

	if n.IPAM.ENILinkPrefix == "" {
		n.IPAM.ENILinkPrefix = "eth"
	}

	return n.IPAM, n.CNIVersion, nil
}

func loadK8SArgs(envArgs string) (*K8SArgs, error) {
	k8sArgs := K8SArgs{}
	if envArgs != "" {
		err := types.LoadArgs(envArgs, &k8sArgs)
		if err != nil {
			return nil, err
		}
	}
	return &k8sArgs, nil
}

func main() {
	initFlags()
	defer log.Flush()

	logDir := filepath.Dir(logFile)
	if err := os.Mkdir(logDir, 0755); err != nil && !os.IsExist(err) {
		fmt.Printf("mkdir %v failed: %v", logDir, err)
		os.Exit(1)
	}

	if e := skel.PluginMainWithError(cmdAdd, cmdCheck, cmdDel, cni.PluginSupportedVersions, bv.BuildString("eni-ipam")); e != nil {
		log.Flush()
		if err := e.Print(); err != nil {
			log.Errorf(context.TODO(), "Error writing error JSON to stdout: %v", err)
		}
		os.Exit(1)
	}
}

func cmdAdd(args *skel.CmdArgs) error {
	ctx := log.NewContext()

	log.Infof(ctx, "-----------cmdAdd begins----------")
	defer log.Infof(ctx, "-----------cmdAdd ends----------")
	log.Infof(ctx, "[cmdAdd]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdAdd]: stdinData: %v", string(args.StdinData))

	result := &current.Result{}

	ipamConf, cniVersion, err := loadIPAMConf(args.StdinData)
	if err != nil {
		return err
	}

	k8sArgs, err := loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	name, namespace := string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE)

	resp, err := eniIPAM.AllocIP(ctx, k8sArgs, ipamConf)
	if err != nil {
		log.Errorf(ctx, "failed to allocate IP: %v", err)
		return err
	}
	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server allocate IP error: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	// invoke ipam del if err to avoid ip leak
	defer func() {
		if err != nil {
			_, err := eniIPAM.ReleaseIP(ctx, k8sArgs, ipamConf)
			if err != nil {
				log.Errorf(ctx, "rollback: failed to delete IP for pod (%v %v): %v", namespace, name, err)
			}
		}
	}()

	var networkClient networkClient
	switch resp.IPType {
	case rpc.IPType_BCCMultiENIMultiIPType:
		networkClient = &bccENIMultiIP{name: name, namespace: namespace, netlink: netlinkwrapper.New()}
	case rpc.IPType_BBCPrimaryENIMultiIPType:
		networkClient = &bbcENIMultiIP{name: name, namespace: namespace}
	default:
		return fmt.Errorf("unknown ipType: %v", resp.IPType)
	}

	err = networkClient.SetupNetwork(ctx, result, ipamConf, resp)
	if err != nil {
		log.Errorf(ctx, "SetupNetwork failed: %v", err)
		return err
	}

	return types.PrintResult(result, cniVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	ctx := log.NewContext()

	log.Infof(ctx, "-----------cmdDel begins----------")
	defer log.Infof(ctx, "-----------cmdDel ends----------")
	log.Infof(ctx, "[cmdDel]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdDel]: stdinData: %v", string(args.StdinData))

	ipamConf, _, err := loadIPAMConf(args.StdinData)
	if err != nil {
		return err
	}

	k8sArgs, err := loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	name, namespace := string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE)

	// we cannot rely on NetworkInfo to clean up rule since ipam server will gc leaked pod
	if args.Netns != "" {
		netutil := networkutil.New()
		podIP, _ := netutil.GetIPFromPodNetNS(args.Netns, args.IfName, netlink.FAMILY_V4)
		client := bccENIMultiIP{name: name, namespace: namespace, netlink: netlinkwrapper.New()}
		if podIP != nil {
			addrBits := 32
			if podIP.To4() == nil {
				addrBits = 128
			}
			podIPNet := &net.IPNet{IP: podIP, Mask: net.CIDRMask(addrBits, addrBits)}
			_ = client.delToOrFromContainerRule(true, podIPNet)
			_ = client.delToOrFromContainerRule(false, podIPNet)
			log.Infof(ctx, "clean up ip rule from netns of ip %v for pod (%v %v)", podIPNet.String(), namespace, name)
		}
	}

	resp, err := eniIPAM.ReleaseIP(ctx, k8sArgs, ipamConf)
	if err != nil {
		msg := fmt.Sprintf("failed to delete IP for pod (%v %v): %v", namespace, name, err)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server release IP error: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	var networkClient networkClient
	switch resp.IPType {
	case rpc.IPType_BCCMultiENIMultiIPType:
		networkClient = &bccENIMultiIP{name: name, namespace: namespace, netlink: netlinkwrapper.New()}
	case rpc.IPType_BBCPrimaryENIMultiIPType:
		networkClient = &bbcENIMultiIP{name: name, namespace: namespace}
	default:
		return fmt.Errorf("unknown ipType: %v", resp.IPType)
	}

	err = networkClient.TeardownNetwork(ctx, ipamConf, resp)
	if err != nil {
		log.Errorf(ctx, "TeardownNetwork failed: %v", err)
		return err
	}

	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func initFlags() {
	log.InitFlags(nil)
	flag.Set("logtostderr", "false")
	flag.Set("log_file", logFile)
	flag.Parse()
}
