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
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	typeswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes"
	grpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/grpc"
	ipwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip"
	ipamwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
	rpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc"
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
)

const (
	logFile = "/var/log/cce/crossvpc-eni.log"
)

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

type crossVpcEniPlugin struct {
	nlink   netlinkwrapper.Interface
	ns      nswrapper.Interface
	ipam    ipamwrapper.Interface
	ip      ipwrapper.Interface
	types   typeswrapper.Interface
	netutil networkutil.Interface
	rpc     rpcwrapper.Interface
	grpc    grpcwrapper.Interface
	sysctl  sysctlwrapper.Interface
}

func newCrossVpcEniPlugin() *crossVpcEniPlugin {
	return &crossVpcEniPlugin{
		nlink:   netlinkwrapper.New(),
		ns:      nswrapper.New(),
		ipam:    ipamwrapper.New(),
		ip:      ipwrapper.New(),
		types:   typeswrapper.New(),
		netutil: networkutil.New(),
		rpc:     rpcwrapper.New(),
		grpc:    grpcwrapper.New(),
		sysctl:  sysctlwrapper.New(),
	}
}

type ipamClient interface {
}

func (p *crossVpcEniPlugin) loadK8SArgs(envArgs string) (*cni.K8SArgs, error) {
	k8sArgs := cni.K8SArgs{}
	if envArgs != "" {
		err := types.LoadArgs(envArgs, &k8sArgs)
		if err != nil {
			return nil, err
		}
	}
	return &k8sArgs, nil
}

type NetConf struct {
	types.NetConf
	IfName   string `json:"ifName"`
	Endpoint string `json:"endpoint"`
}

func initFlags() {
	log.InitFlags(nil)
	flag.Set("logtostderr", "false")
	flag.Set("log_file", logFile)
	flag.Parse()
}

func main() {
	initFlags()
	defer log.Flush()

	logDir := filepath.Dir(logFile)
	if err := os.Mkdir(logDir, 0755); err != nil && !os.IsExist(err) {
		fmt.Printf("mkdir %v failed: %v", logDir, err)
		os.Exit(1)
	}

	plugin := newCrossVpcEniPlugin()
	if e := skel.PluginMainWithError(plugin.cmdAdd, plugin.cmdCheck, plugin.cmdDel, cni.PluginSupportedVersions, bv.BuildString("crossvpc-eni")); e != nil {
		log.Flush()
		if err := e.Print(); err != nil {
			log.Errorf(context.TODO(), "Error writing error JSON to stdout: %v", err)
		}
		os.Exit(1)
	}
}

func loadConf(bytes []byte) (*NetConf, string, error) {
	var (
		n   = &NetConf{}
		err error
	)

	err = json.Unmarshal(bytes, n)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}

	if n.NetConf.RawPrevResult != nil {
		if err = version.ParsePrevResult(&n.NetConf); err != nil {
			return nil, "", fmt.Errorf("could not parse prevResult: %v", err)
		}
	}

	return n, n.CNIVersion, nil
}

func (p *crossVpcEniPlugin) cmdAdd(args *skel.CmdArgs) error {
	var (
		ctx        = log.NewContext()
		result     = &current.Result{}
		n          *NetConf
		cniVersion string
		target     netlink.Link
		ipam       = NewCrossVpcEniIPAM(p.grpc, p.rpc)
		netns      ns.NetNS
		err        error
		linkErr    error
	)

	log.Infof(ctx, "====> CmdAdd Begins <====")
	defer log.Infof(ctx, "====> CmdAdd Ends <====")
	log.Infof(ctx, "[cmdAdd]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdAdd]: stdinData: %v", string(args.StdinData))

	n, cniVersion, err = loadConf(args.StdinData)
	if err != nil {
		return err
	}

	if n.PrevResult != nil {
		log.Info(ctx, "crossvpc-eni is chained after other main plugin")
		result, err = current.NewResultFromResult(n.PrevResult)
		if err != nil {
			return fmt.Errorf("could not convert result to current version: %v", err)
		}
	}

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	var (
		isCrossVPCEniPrimaryInterface = n.IfName == args.IfName
	)

	resp, err := ipam.CreateEni(ctx, k8sArgs, n.Endpoint)
	if err != nil {
		return fmt.Errorf("ipam creates eni error: %v", err)
	}

	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server creates eni failed: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	if resp.GetCrossVPCENI() == nil {
		log.Info(ctx, "ipam returns empty response")
		goto done
	}

	netns, err = p.ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	// the kernel would take some time to detect eni insertion.
	err = wait.ExponentialBackoff(wait.Backoff{
		Duration: time.Millisecond * 500,
		Factor:   1,
		Steps:    6,
	}, func() (done bool, err error) {
		target, linkErr = p.netutil.GetLinkByMacAddress(resp.GetCrossVPCENI().Mac)
		if linkErr != nil {
			log.Warningf(ctx, "host netns: %v", linkErr)
		} else {
			log.Infof(ctx, "host netns: found link %v with mac address %v", target.Attrs().Name, resp.GetCrossVPCENI().Mac)
			return true, nil
		}

		return false, nil
	})
	if err != nil && err == wait.ErrWaitTimeout {
		return fmt.Errorf("host netns: %v", linkErr)
	}

	if err = p.nlink.LinkSetNsFd(target, int(netns.Fd())); err != nil {
		return fmt.Errorf("failed to move %v to container netns: %v", target.Attrs().Name, err)
	}

	err = netns.Do(func(_ ns.NetNS) error {
		return p.setupEni(resp, n.IfName, args)
	})
	if err != nil {
		return err
	}

	if n.PrevResult == nil || isCrossVPCEniPrimaryInterface {
		log.Info(ctx, "crossvpc-eni is the main plugin or in exclusive mode")
		result.Interfaces = []*current.Interface{{
			Name:    n.IfName,
			Mac:     resp.GetCrossVPCENI().GetIP(),
			Sandbox: args.Netns,
		}}
		result.IPs = []*current.IPConfig{
			{
				Version:   "4",
				Interface: current.Int(0),
				Address:   net.IPNet{IP: net.ParseIP(resp.GetCrossVPCENI().GetIP()), Mask: net.CIDRMask(32, 32)},
			},
		}
	}

done:
	return p.types.PrintResult(result, cniVersion)
}

func (p *crossVpcEniPlugin) setupEni(
	resp *rpc.AllocateIPReply,
	ifName string,
	args *skel.CmdArgs,
) error {
	var (
		isCrossVPCEniPrimaryInterface bool = (ifName == args.IfName)
	)

	target, err := p.netutil.GetLinkByMacAddress(resp.GetCrossVPCENI().Mac)
	if err != nil {
		return fmt.Errorf("container netns: %v", err)
	}

	if isCrossVPCEniPrimaryInterface {
		oldVeth, err := p.nlink.LinkByName(args.IfName)
		if err != nil {
			return err
		}
		err = p.nlink.LinkDel(oldVeth)
		if err != nil {
			return err
		}
	}

	if ifName != "" {
		err = p.nlink.LinkSetDown(target)
		if err != nil {
			return err
		}

		err = p.nlink.LinkSetName(target, ifName)
		if err != nil {
			return err
		}
	}

	err = p.nlink.LinkSetUp(target)
	if err != nil {
		return err
	}

	_, cidr, err := net.ParseCIDR(resp.GetCrossVPCENI().VPCCIDR)
	if err != nil {
		return err
	}

	err = p.nlink.AddrAdd(target, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(resp.GetCrossVPCENI().GetIP()),
			Mask: cidr.Mask,
		},
	})
	if err != nil {
		return err
	}

	if isCrossVPCEniPrimaryInterface {
		_, defaultPrefix, _ := net.ParseCIDR("0.0.0.0/0")
		err = p.nlink.RouteReplace(&netlink.Route{
			Dst:       defaultPrefix,
			LinkIndex: target.Attrs().Index,
			Scope:     netlink.SCOPE_UNIVERSE,
		})
		if err != nil {
			return err
		}
	}

	// process user specified routes
	if !isCrossVPCEniPrimaryInterface {
		err = p.processUserSpecifiedRoutes(resp, target, args)
		if err != nil {
			return err
		}
	}

	// for security concern, forbid ip forward
	_, err = p.sysctl.Sysctl("net/ipv4/ip_forward", "0")
	if err != nil {
		return err
	}

	return nil
}

func (p *crossVpcEniPlugin) processUserSpecifiedRoutes(
	resp *rpc.AllocateIPReply,
	eniLink netlink.Link,
	args *skel.CmdArgs,
) error {
	var (
		eniDelegation bool
		vethLink      netlink.Link
		err           error
	)

	vethLink, err = p.nlink.LinkByName(args.IfName)
	if err != nil {
		return err
	}

	if resp.GetCrossVPCENI().GetDefaultRouteInterfaceDelegation() == "eni" {
		eniDelegation = true
	}

	if eniDelegation {
		_, defaultPrefix, _ := net.ParseCIDR("0.0.0.0/0")
		err = p.nlink.RouteReplace(&netlink.Route{
			Dst:       defaultPrefix,
			LinkIndex: eniLink.Attrs().Index,
			Scope:     netlink.SCOPE_UNIVERSE,
		})
		if err != nil {
			return err
		}
	}

	for _, cidr := range resp.GetCrossVPCENI().GetDefaultRouteExcludedCidrs() {
		_, dst, err := net.ParseCIDR(cidr)
		if err != nil {
			return err
		}

		if eniDelegation {
			err = p.nlink.RouteReplace(&netlink.Route{
				Dst:       dst,
				LinkIndex: vethLink.Attrs().Index,
				Gw:        net.IPv4(169, 254, 1, 1),
			})
		} else {
			err = p.nlink.RouteReplace(&netlink.Route{
				Dst:       dst,
				LinkIndex: eniLink.Attrs().Index,
				Scope:     netlink.SCOPE_LINK,
			})
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (p *crossVpcEniPlugin) cmdDel(args *skel.CmdArgs) error {
	var (
		ctx  = log.NewContext()
		n    *NetConf
		resp *rpc.ReleaseIPReply
		ipam = NewCrossVpcEniIPAM(p.grpc, p.rpc)
		err  error
	)

	log.Infof(ctx, "====> CmdDel Begins <====")
	defer log.Infof(ctx, "====> CmdDel Ends <====")
	log.Infof(ctx, "[cmdDel]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdDel]: stdinData: %v", string(args.StdinData))

	n, _, err = loadConf(args.StdinData)
	if err != nil {
		return err
	}

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	resp, err = ipam.DeleteEni(ctx, k8sArgs, n.Endpoint)
	if err != nil {
		return fmt.Errorf("ipam deletes eni error: %v", err)
	}

	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server deletes eni failed: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	return nil
}

func (p *crossVpcEniPlugin) cmdCheck(args *skel.CmdArgs) error {
	return nil
}
