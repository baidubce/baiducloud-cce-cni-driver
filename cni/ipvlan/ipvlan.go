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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	typeswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes"
	ipwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip"
	ipamwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
)

type ipvlanPlugin struct {
	nlink     netlinkwrapper.Interface
	ns        nswrapper.Interface
	ipam      ipamwrapper.Interface
	ip        ipwrapper.Interface
	types     typeswrapper.Interface
	netutil   networkutil.Interface
	crdClient versioned.Interface
}

func newIPVlanPlugin() *ipvlanPlugin {
	return &ipvlanPlugin{
		nlink:   netlinkwrapper.New(),
		ns:      nswrapper.New(),
		ipam:    ipamwrapper.New(),
		ip:      ipwrapper.New(),
		types:   typeswrapper.New(),
		netutil: networkutil.New(),
	}
}

type MasterType string

const (
	MasterTypePrimary   MasterType = "primary"
	MasterTypeSecondary MasterType = "secondary"
)

const (
	defaultKubeConfig = "/etc/cni/net.d/cce-cni.d/cce-cni.kubeconfig"
)

type NetConf struct {
	types.NetConf
	Master      string     `json:"master"`
	MasterType  MasterType `json:"masterType"`
	Mode        string     `json:"mode"`
	MTU         int        `json:"mtu"`
	OmitGateway bool       `json:"omitGateway"` // OmitGateway indicates whether to set Gw IP for default route
	KubeConfig  string     `json:"kubeconfig"`
}

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func (p *ipvlanPlugin) loadK8SArgs(envArgs string) (*cni.K8SArgs, error) {
	k8sArgs := cni.K8SArgs{}
	if envArgs != "" {
		err := p.types.LoadArgs(envArgs, &k8sArgs)
		if err != nil {
			return nil, err
		}
	}
	return &k8sArgs, nil
}

func (p *ipvlanPlugin) loadConf(bytes []byte) (*NetConf, string, error) {
	n := &NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}

	var result *current.Result
	var err error
	// Parse previous result
	if n.NetConf.RawPrevResult != nil {
		if err = version.ParsePrevResult(&n.NetConf); err != nil {
			return nil, "", fmt.Errorf("could not parse prevResult: %v", err)
		}

		result, err = current.NewResultFromResult(n.PrevResult)
		if err != nil {
			return nil, "", fmt.Errorf("could not convert result to current version: %v", err)
		}
	}

	if n.MasterType == "" {
		n.MasterType = MasterTypePrimary
	}

	if n.MasterType == MasterTypePrimary {
		if n.Master == "" {
			if result == nil {
				defaultRouteInterface, err := p.netutil.DetectDefaultRouteInterfaceName()
				if err != nil {
					return nil, "", err
				}
				n.Master = defaultRouteInterface
			} else {
				if len(result.Interfaces) == 1 && result.Interfaces[0].Name != "" {
					n.Master = result.Interfaces[0].Name
				} else {
					return nil, "", fmt.Errorf("chained master failure. PrevResult lacks a single named interface")
				}
			}
		}
	}

	if n.KubeConfig == "" {
		n.KubeConfig = defaultKubeConfig
	}

	return n, n.CNIVersion, nil
}

func modeFromString(s string) (netlink.IPVlanMode, error) {
	switch s {
	case "", "l2":
		return netlink.IPVLAN_MODE_L2, nil
	case "l3":
		return netlink.IPVLAN_MODE_L3, nil
	case "l3s":
		return netlink.IPVLAN_MODE_L3S, nil
	default:
		return 0, fmt.Errorf("unknown ipvlan mode: %q", s)
	}
}

func (p *ipvlanPlugin) createIpvlan(conf *NetConf, ifName string, netns ns.NetNS) (*current.Interface, error) {
	ipvlan := &current.Interface{}

	mode, err := modeFromString(conf.Mode)
	if err != nil {
		return nil, err
	}

	m, err := p.nlink.LinkByName(conf.Master)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup master %q: %v", conf.Master, err)
	}

	// due to kernel bug we have to create with tmpname or it might
	// collide with the name on the host and error out
	tmpName, err := p.ip.RandomVethName()
	if err != nil {
		return nil, err
	}

	iv := &netlink.IPVlan{
		LinkAttrs: netlink.LinkAttrs{
			MTU:         conf.MTU,
			Name:        tmpName,
			ParentIndex: m.Attrs().Index,
			Namespace:   netlink.NsFd(int(netns.Fd())),
		},
		Mode: mode,
	}

	if err := p.nlink.LinkAdd(iv); err != nil {
		return nil, fmt.Errorf("failed to create ipvlan: %v", err)
	}

	err = netns.Do(func(_ ns.NetNS) error {
		err := p.ip.RenameLink(tmpName, ifName)
		if err != nil {
			_ = p.ip.DelLinkByName(tmpName)
			return fmt.Errorf("failed to rename ipvlan to %q: %v", ifName, err)
		}
		ipvlan.Name = ifName

		// Re-fetch ipvlan to get all properties/attributes
		contIpvlan, err := p.nlink.LinkByName(ipvlan.Name)
		if err != nil {
			return fmt.Errorf("failed to refetch ipvlan %q: %v", ipvlan.Name, err)
		}
		ipvlan.Mac = contIpvlan.Attrs().HardwareAddr.String()
		ipvlan.Sandbox = netns.Path()

		return nil
	})
	if err != nil {
		return nil, err
	}

	return ipvlan, nil
}

func (p *ipvlanPlugin) updateMasterFromWep(k8sArgs *cni.K8SArgs, n *NetConf) error {
	name, namespace := string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE)
	wep, err := p.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get wep (%v/%v): %v", namespace, name, err)
	}

	master, err := p.netutil.GetLinkByMacAddress(wep.Spec.Mac)
	if err != nil {
		return err
	}

	n.Master = master.Attrs().Name

	return nil
}

func (p *ipvlanPlugin) cmdAdd(args *skel.CmdArgs) error {
	n, cniVersion, err := p.loadConf(args.StdinData)
	if err != nil {
		return err
	}

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	netns, err := p.ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	var result *current.Result
	// Configure iface from PrevResult if we have IPs and an IPAM
	// block has not been configured
	haveResult := false
	if n.IPAM.Type == "" && n.PrevResult != nil {
		result, err = current.NewResultFromResult(n.PrevResult)
		if err != nil {
			return err
		}
		if len(result.IPs) > 0 {
			haveResult = true
		}
	}
	if !haveResult {
		// run the IPAM plugin and get back the config to apply
		r, err := p.ipam.ExecAdd(n.IPAM.Type, args.StdinData)
		if err != nil {
			return err
		}

		// Invoke ipam del if err to avoid ip leak
		defer func() {
			if err != nil {
				p.ipam.ExecDel(n.IPAM.Type, args.StdinData)
			}
		}()

		// Convert whatever the IPAM result was into the current Result type
		result, err = current.NewResultFromResult(r)
		if err != nil {
			return err
		}

		if len(result.IPs) == 0 {
			return errors.New("IPAM plugin returned missing IP config")
		}
	}

	// 多网卡根据分配结果确定 master interface
	if n.MasterType == MasterTypeSecondary {
		p.crdClient, err = newCrdClient(n.KubeConfig)
		if err != nil {
			return fmt.Errorf("failed to create k8s client with kubeconfig %v: %v", n.KubeConfig, err)
		}

		err = p.updateMasterFromWep(k8sArgs, n)
		if err != nil {
			return err
		}
	}

	ipvlanInterface, err := p.createIpvlan(n, args.IfName, netns)
	if err != nil {
		return err
	}

	for _, ipc := range result.IPs {
		// All addresses belong to the ipvlan interface
		ipc.Interface = current.Int(0)
	}

	result.Interfaces = []*current.Interface{ipvlanInterface}

	err = netns.Do(func(_ ns.NetNS) error {
		if err := p.ipam.ConfigureIface(args.IfName, result); err != nil {
			return err
		}
		// 如果用户指定 OmitGateway，去掉默认路由的网关
		if n.OmitGateway {
			link, err := p.nlink.LinkByName(args.IfName)
			if err != nil {
				return fmt.Errorf("failed to lookup %q: %v", args.IfName, err)
			}
			_, defNet, _ := net.ParseCIDR("0.0.0.0/0")
			defaultRoute := &netlink.Route{
				LinkIndex: link.Attrs().Index,
				Scope:     netlink.SCOPE_UNIVERSE,
				Dst:       defNet,
			}
			if err := p.nlink.RouteReplace(defaultRoute); err != nil {
				return fmt.Errorf("failed to replace route '%v dev %v': %v", defNet, args.IfName, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	result.DNS = n.DNS

	return p.types.PrintResult(result, cniVersion)
}

func (p *ipvlanPlugin) cmdDel(args *skel.CmdArgs) error {
	n, _, err := p.loadConf(args.StdinData)
	if err != nil {
		return err
	}

	// On chained invocation, IPAM block can be empty
	if n.IPAM.Type != "" {
		err = p.ipam.ExecDel(n.IPAM.Type, args.StdinData)
		if err != nil {
			return err
		}
	}

	if args.Netns == "" {
		return nil
	}

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	err = p.ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		if err := p.ip.DelLinkByName(args.IfName); err != nil {
			if err != ip.ErrLinkNotFound {
				return err
			}
		}
		return nil
	})

	return err
}

func main() {
	plugin := newIPVlanPlugin()
	skel.PluginMain(plugin.cmdAdd, plugin.cmdCheck, plugin.cmdDel, cni.PluginSupportedVersions, bv.BuildString("ipvlan"))
}

func (p *ipvlanPlugin) cmdCheck(args *skel.CmdArgs) error {
	return nil
}

// newCrdClient creates a k8s client
func newCrdClient(kubeconfig string) (versioned.Interface, error) {
	config, err := buildConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return versioned.NewForConfig(config)
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
