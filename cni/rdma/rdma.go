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
	"strconv"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/keymutex"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
	typeswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/cnitypes"
	grpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/grpc"
	ipwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ip"
	ipamwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ipam"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/ns"
	rpcwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/rpc"
	sysctlwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/sysctl"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	utilexec "k8s.io/utils/exec"
)

const (
	logFile                 = "/var/log/cce/cni-rdma.log"
	rtStartIdx              = 100
	fileLock                = "/var/run/cni-rdma.lock"
	roceDevicePrefix        = "roce"
	resourceName            = "rdma"
	defaultKubeConfig       = "/etc/cni/net.d/cce-cni.d/cce-cni.kubeconfig"
	rpFilterSysctlTemplate  = "net.ipv4.conf.%s.rp_filter"
	arpIgnoreSysctlTemplate = "net.ipv4.conf.%s.arp_ignore"
)

var buildConfigFromFlags = clientcmd.BuildConfigFromFlags
var k8sClientSet = func(c *rest.Config) (kubernetes.Interface, error) {
	clientSet, err := kubernetes.NewForConfig(c)
	return clientSet, err
}

type NetConf struct {
	types.NetConf
	Mode         string    `json:"mode"`
	KubeConfig   string    `json:"kubeconfig"`
	Mask         int       `json:"mask"`
	InstanceType string    `json:"instanceType"`
	IPAM         *IPAMConf `json:"ipam,omitempty"`
}

type IPAMConf struct {
	Endpoint string `json:"endpoint"`
}

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func loadConf(bytes []byte) (*NetConf, string, error) {
	n := &NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}

	if n.IPAM == nil {
		return nil, "", fmt.Errorf("IPAM config missing 'ipam' key")
	}

	if n.KubeConfig == "" {
		n.KubeConfig = defaultKubeConfig
	}
	if n.IPAM.Endpoint == "" {
		return nil, "", fmt.Errorf("ipam endpoint is empty")
	}

	if n.Mask <= 0 {
		n.Mask = 24
	}

	return n, n.CNIVersion, nil
}

type rdmaPlugin struct {
	nlink   netlinkwrapper.Interface
	ns      nswrapper.Interface
	ipam    ipamwrapper.Interface
	ip      ipwrapper.Interface
	types   typeswrapper.Interface
	netutil networkutil.Interface
	rpc     rpcwrapper.Interface
	grpc    grpcwrapper.Interface
	exec    utilexec.Interface
	sysctl  sysctlwrapper.Interface
}

func newRdmaPlugin() *rdmaPlugin {
	return &rdmaPlugin{
		nlink:   netlinkwrapper.New(),
		ns:      nswrapper.New(),
		ipam:    ipamwrapper.New(),
		ip:      ipwrapper.New(),
		types:   typeswrapper.New(),
		netutil: networkutil.New(),
		rpc:     rpcwrapper.New(),
		grpc:    grpcwrapper.New(),
		exec:    utilexec.New(),
		sysctl:  sysctlwrapper.New(),
	}
}

func (p *rdmaPlugin) cmdAdd(args *skel.CmdArgs) error {
	ctx := log.NewContext()
	log.Infof(ctx, "====> CmdAdd Begins <====")
	defer log.Infof(ctx, "====> CmdAdd Ends <====")
	defer log.Flush()
	ipam := NewRoceIPAM(p.grpc, p.rpc)

	n, cniVersion, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}
	netns, err := p.ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	defer netns.Close()
	result := &current.Result{CNIVersion: cniVersion}

	roceDevs, err := p.getAllRoceDevices()
	if err != nil || len(roceDevs) <= 0 {
		log.Infof(ctx, "not found roce devices err: %s", err.Error())
		return types.PrintResult(result, cniVersion)
	}

	log.Infof(ctx, "roce devs:%v", roceDevs)

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		return err
	}
	want, err := wantRoce(string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), n)
	if err != nil {
		log.Errorf(ctx, "check want roce failed: %s", err.Error())
	}

	if !want {
		log.Infof(ctx, "pod: %s,donot want roce", string(k8sArgs.K8S_POD_NAME))
		return types.PrintResult(result, n.CNIVersion)
	}

	l, err := keymutex.GrabFileLock(fileLock)
	if err != nil {
		log.Errorf(ctx, "grad file lock error: %s", err.Error())
		return fmt.Errorf("grad file lock error: %s", err.Error())
	}
	defer l.Close()

	for idx, devName := range roceDevs {
		roceDevName := fmt.Sprintf("%s%d", roceDevicePrefix, idx+1)
		err = p.setupMacvlan(ctx, n, devName, roceDevName, netns, k8sArgs, ipam)
		if err != nil {
			return err
		}
	}

	// err = netns.Do(func(_ ns.NetNS) error {
	// 	return p.disableRPFCheck(ctx, len(roceDevs))
	// })

	return types.PrintResult(result, cniVersion)
}

func (p *rdmaPlugin) cmdDel(args *skel.CmdArgs) error {
	ctx := log.NewContext()
	log.Infof(ctx, "====> Rdma CNI <====")
	log.Infof(ctx, "====> CmdDel Begins <====")
	defer log.Infof(ctx, "====> CmdDel Ends <====")
	log.Infof(ctx, "[cmdDel]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v", args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdDel]: stdinData: %v", string(args.StdinData))
	if args.Netns == "" {
		return nil
	}

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	err := p.ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		return p.delAllMacVlanDevices()
	})

	if err != nil {
		log.Errorf(ctx, "delete macvlan device failed:%s", err.Error())
		return err
	}

	n, _, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	ipamClient := NewRoceIPAM(p.grpc, p.rpc)
	resp, err := ipamClient.ReleaseIP(ctx, k8sArgs, n.IPAM.Endpoint, n.InstanceType)
	if err != nil {
		msg := fmt.Sprintf("failed to delete IP for pod (%v %v): %v", k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_NAME, err)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server release IP error: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return errors.New(msg)
	}
	log.Infof(ctx, "release for pod(%v %v) successfully", k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_NAME)

	return err

}

func (p *rdmaPlugin) delAllMacVlanDevices() error {
	devs, err := p.nlink.LinkList()
	if err != nil {
		return err
	}
	for _, dev := range devs {
		if dev.Type() == "macvlan" {
			if err := p.ip.DelLinkByName(dev.Attrs().Name); err != nil {
				if err != ip.ErrLinkNotFound {
					return err
				}
			}
		}
	}
	return nil
}

func main() {
	initFlags()
	defer log.Flush()

	logDir := filepath.Dir(logFile)
	if err := os.Mkdir(logDir, 0755); err != nil && !os.IsExist(err) {
		fmt.Printf("mkdir %v failed: %v", logDir, err)
		os.Exit(1)
	}

	plugin := newRdmaPlugin()
	if e := skel.PluginMainWithError(plugin.cmdAdd, plugin.cmdCheck, plugin.cmdDel, cni.PluginSupportedVersions, bv.BuildString("rdma")); e != nil {
		log.Flush()
		if err := e.Print(); err != nil {
			log.Errorf(context.TODO(), "Error writing error JSON to stdout: %v", err)
		}
		os.Exit(1)
	}

}

func (p *rdmaPlugin) cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func (p *rdmaPlugin) loadK8SArgs(envArgs string) (*cni.K8SArgs, error) {
	k8sArgs := cni.K8SArgs{}
	if envArgs != "" {
		err := types.LoadArgs(envArgs, &k8sArgs)
		if err != nil {
			return nil, err
		}
	}
	return &k8sArgs, nil
}

func initFlags() {
	log.InitFlags(nil)
	flag.Set("logtostderr", "false")
	flag.Set("log_file", logFile)
	flag.Parse()
}

func modeFromString(s string) (netlink.MacvlanMode, error) {
	switch s {
	case "", "bridge":
		return netlink.MACVLAN_MODE_BRIDGE, nil
	case "private":
		return netlink.MACVLAN_MODE_PRIVATE, nil
	case "vepa":
		return netlink.MACVLAN_MODE_VEPA, nil
	case "passthru":
		return netlink.MACVLAN_MODE_PASSTHRU, nil
	default:
		return 0, fmt.Errorf("unknown macvlan mode: %q", s)
	}
}

func (p *rdmaPlugin) createMacvlan(conf *NetConf, master, ifName string, netns ns.NetNS) (*current.Interface, error) {
	macvlan := &current.Interface{}

	mode, err := modeFromString(conf.Mode)
	if err != nil {
		return nil, err
	}

	m, err := p.nlink.LinkByName(master)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup master %q: %v", master, err)
	}

	// due to kernel bug we have to create with tmpName or it might
	// collide with the name on the host and error out
	tmpName, err := ip.RandomVethName()
	if err != nil {
		return nil, err
	}

	mv := &netlink.Macvlan{
		LinkAttrs: netlink.LinkAttrs{
			//			MTU:         conf.MTU,
			Name:        tmpName,
			ParentIndex: m.Attrs().Index,
			Namespace:   netlink.NsFd(int(netns.Fd())),
		},
		Mode: mode,
	}

	if err := p.nlink.LinkAdd(mv); err != nil {
		return nil, fmt.Errorf("failed to create macvlan for rdma: %v", err)
	}

	err = netns.Do(func(_ ns.NetNS) error {
		return p.setupMacvlanInterface(macvlan, mv, tmpName, ifName, netns)
	})
	if err != nil {
		return nil, err
	}

	return macvlan, nil
}

func (p *rdmaPlugin) setupMacvlanInterface(macvlan *current.Interface, mv *netlink.Macvlan, tmpName, ifName string, netns ns.NetNS) error {
	err := p.ip.RenameLink(tmpName, ifName)
	if err != nil {
		_ = p.nlink.LinkDel(mv)
		return fmt.Errorf("failed to rename macvlan to %q: %v", ifName, err)
	}
	macvlan.Name = ifName

	// Re-fetch macvlan to get all properties/attributes
	contMacvlan, err := p.nlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to refetch macvlan %q: %v", ifName, err)
	}
	macvlan.Mac = contMacvlan.Attrs().HardwareAddr.String()
	macvlan.Sandbox = netns.Path()

	return nil
}

func (p *rdmaPlugin) getAllRoceDevices() ([]string, error) {
	cmd := p.exec.Command("sh", "-c", "ibdev2netdev | awk '{print $5}'")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run ibdev2netdev cmd failed with %s\n", err)
	}

	if strings.Contains(string(out), "not found") {
		return nil, fmt.Errorf("exec command error %s\n", string(out))
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	roceDevs := make([]string, 0, 1)

	defaultRouteInterface, err := getDefaultRouteInterfaceName()
	if err != nil {
		return nil, err
	}

	for _, devName := range parts {
		if devName == defaultRouteInterface {
			continue
		}

		roceDevs = append(roceDevs, devName)
	}

	return roceDevs, nil
}

func getDefaultRouteInterfaceName() (string, error) {
	routeToDstIP, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return "", err
	}

	for _, v := range routeToDstIP {
		if v.Dst == nil {
			l, err := netlink.LinkByIndex(v.LinkIndex)
			if err != nil {
				return "", err
			}
			return l.Attrs().Name, nil
		}
	}

	return "", fmt.Errorf("no default route interface found")
}

func (p *rdmaPlugin) setupMacvlan(ctx context.Context, conf *NetConf, master, ifName string, netns ns.NetNS, k8sArgs *cni.K8SArgs, ipamClient *roceIPAM) error {
	macvlanInterface, err := p.createMacvlan(conf, master, ifName, netns)
	if err != nil {
		return err
	}
	log.Infof(ctx, "create macvlan dev: %s ,mac: %s successfully,master:%s", ifName, macvlanInterface.Mac, master)

	defer func() {
		if err != nil {
			err = netns.Do(func(_ ns.NetNS) error {
				return ip.DelLinkByName(ifName)
			})
			if err != nil {
				log.Errorf(ctx, "delete link error in defer, device name: %s ,error:%s", ifName, err.Error())
			}
		}
	}()

	m, err := p.nlink.LinkByName(master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", master, err)
	}

	masterMac := m.Attrs().HardwareAddr.String()
	masterMask := p.getDeviceMask(conf, master)

	gw, err := p.getDeviceGw(master)
	if err != nil {
		log.Errorf(ctx, "get device gw error: %s ", err.Error())
		return fmt.Errorf("get device gw error: %s ", err.Error())
	}

	log.Infof(ctx, "gw: %s,master mac: %s,master mask: %v,for dev: %s", gw.String(), masterMac, masterMask, master)

	err = netns.Do(func(_ ns.NetNS) error {
		return p.setupMacvlanNetworkInfo(ctx, conf, masterMac, masterMask, gw, ifName, macvlanInterface, k8sArgs, ipamClient)
	})

	return err
}

func (p *rdmaPlugin) getDeviceMask(conf *NetConf, devName string) net.IPMask {
	link, err := p.nlink.LinkByName(devName)
	if err != nil {
		return net.CIDRMask(conf.Mask, 32)
	}

	addrList, err := p.nlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return net.CIDRMask(conf.Mask, 32)
	}

	for _, addr := range addrList {
		return addr.IPNet.Mask
	}
	return net.CIDRMask(conf.Mask, 32)
}

func (p *rdmaPlugin) getDeviceGw(devName string) (net.IP, error) {
	link, err := p.nlink.LinkByName(devName)
	if err != nil {
		return nil, fmt.Errorf("failed to get link by name: %v", err)
	}

	addrList, err := p.nlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to get addr list: %v", err)
	}

	if len(addrList) < 1 {
		return nil, fmt.Errorf("device:%s,addr list length < 1, ", devName)
	}

	deviceIP := addrList[0].IP.String() + "/32"

	rules, err := p.nlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to get rule list: %v", err)
	}

	for _, r := range rules {
		if r.Src != nil && r.Src.String() == deviceIP {
			rList, err := p.nlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: r.Table}, netlink.RT_FILTER_TABLE)
			if err != nil {
				return nil, fmt.Errorf("route list filter failed: %s ", err.Error())
			}
			for _, ro := range rList {
				if ro.Gw != nil {
					return ro.Gw, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("not gw ip found for dev: %s ", devName)
}

func (p *rdmaPlugin) setupMacvlanNetworkInfo(ctx context.Context, conf *NetConf, masterMac string, masterMask net.IPMask, gw net.IP, ifName string,
	macvlanInterface *current.Interface, k8sArgs *cni.K8SArgs, ipamClient *roceIPAM) error {
	macvlanInterfaceLink, err := p.nlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to find interface name %q: %v", macvlanInterface.Name, err)
	}

	if err := p.nlink.LinkSetUp(macvlanInterfaceLink); err != nil {
		return fmt.Errorf("failed to set %q UP: %v", ifName, err)
	}
	name, namespace := string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE)

	resp, err := ipamClient.AllocIP(ctx, k8sArgs, conf.IPAM.Endpoint, masterMac, conf.InstanceType)
	if err != nil {
		log.Errorf(ctx, "failed to allocate IP: %v", err)
		return err
	}
	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server allocate IP error: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return errors.New(msg)
	}

	allocRespNetworkInfo := resp.GetENIMultiIP()
	if allocRespNetworkInfo == nil {
		err := errors.New(fmt.Sprintf("failed to allocate IP for pod (%v %v): NetworkInfo is nil", namespace, name))
		log.Errorf(ctx, err.Error())
		return err
	}

	log.Infof(ctx, "allocate IP %v, for pod(%v %v) successfully", allocRespNetworkInfo.IP, namespace, name)

	defer func() {
		if err != nil {
			_, err := ipamClient.ReleaseIP(ctx, k8sArgs, conf.IPAM.Endpoint, conf.InstanceType)
			if err != nil {
				log.Errorf(ctx, "rollback: failed to delete IP for pod (%v %v): %v", namespace, name, err)
			}
		}
	}()

	addr := &netlink.Addr{IPNet: &net.IPNet{
		IP:   net.ParseIP(allocRespNetworkInfo.IP),
		Mask: masterMask,
	}}

	err = p.nlink.AddrAdd(macvlanInterfaceLink, addr)
	if err != nil {
		log.Errorf(ctx, "failed to add IP %v to device : %v", addr.String(), err)
		return err
	}

	idx := macvlanInterfaceLink.Attrs().Index
	ruleSrc := &net.IPNet{
		IP:   addr.IPNet.IP,
		Mask: net.CIDRMask(32, 32),
	}

	err = p.addFromRule(ruleSrc, 10000, rtStartIdx+idx)
	if err != nil {
		log.Errorf(ctx, "add from rule failed: %v", err)
		return err
	}
	log.Infof(ctx, "add rule table: %d,src ip: %s", rtStartIdx+idx, allocRespNetworkInfo.IP)

	err = p.addOIFRule(ifName, 10000, rtStartIdx+idx)
	if err != nil {
		log.Errorf(ctx, "add from rule failed: %v", err)
		return err
	}
	log.Infof(ctx, "add rule table: %d,oif: %s", rtStartIdx+idx, ifName)

	_, cidr, err := net.ParseCIDR(addr.IPNet.String())
	if err != nil {
		log.Errorf(ctx, "parse cidr:%s, failed: %s", addr.IPNet.String(), err.Error())
		return err
	}

	err = p.addRouteByCmd(ctx, cidr, ifName, gw.String(), ruleSrc.IP.String(), rtStartIdx+idx)
	if err != nil {
		log.Errorf(ctx, "add route failed: %s", err.Error())
		return err
	}

	arpMacvlanInterface, err := p.netutil.InterfaceByName(ifName)
	if err != nil {
		log.Errorf(ctx, "failed to look up %s: %v", ifName, err)
		return fmt.Errorf("failed to look up %s: %v", ifName, err)
	}
	if err = p.netutil.GratuitousArpOverIface(addr.IPNet.IP, *arpMacvlanInterface); err != nil {
		log.Errorf(ctx, "send free arp failed,device: %s,ip: %s, err:%s", arpMacvlanInterface.Name, addr.IPNet.IP.String(), err)
	}
	return nil
}

func (p *rdmaPlugin) addFromRule(addr *net.IPNet, priority int, rtTable int) error {
	rule := netlink.NewRule()
	rule.Table = rtTable
	rule.Priority = priority
	rule.Src = addr // ip rule add from `addr` lookup `table` prio `xxx`
	err := p.nlink.RuleDel(rule)
	if err != nil && !netlinkwrapper.IsNotExistError(err) {
		return err
	}

	if err := p.nlink.RuleAdd(rule); err != nil {
		return err
	}
	return nil
}

func (p *rdmaPlugin) addOIFRule(oifName string, priority int, rtTable int) error {
	rule := netlink.NewRule()
	rule.Table = rtTable
	rule.Priority = priority
	rule.OifName = oifName // ip rule add oif `oifName` lookup `table` prio `xxx`
	err := p.nlink.RuleDel(rule)
	if err != nil && !netlinkwrapper.IsNotExistError(err) {
		return err
	}

	if err := p.nlink.RuleAdd(rule); err != nil {
		return err
	}
	return nil
}

func (p *rdmaPlugin) addRoute(idx, rtTable int, gw, src net.IP, dst *net.IPNet) error {
	ro := &netlink.Route{
		LinkIndex: idx,
		Scope:     netlink.SCOPE_LINK,
		Dst:       dst,
		Src:       src,
		Table:     rtTable,
		Gw:        gw,
	}
	ro.SetFlag(netlink.FLAG_ONLINK)
	err := p.nlink.RouteAdd(ro)

	if err != nil {
		return fmt.Errorf("failed to add container route dst %v: %v", dst, err)
	}
	return nil
}

func (p *rdmaPlugin) addRouteByCmd(ctx context.Context, dst *net.IPNet, ifName, gw, srcIP string, rtable int) error {
	strRoute := fmt.Sprintf("ip route add %s dev %s via %s src %s table %d onlink", dst.String(), ifName, gw, srcIP, rtable)
	log.Infof(ctx, "add route: %s", strRoute)
	cmd := p.exec.Command("ip", "route", "add", dst.String(), "dev", ifName,
		"via", gw, "src", srcIP, "table", strconv.Itoa(rtable), "onlink")
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("add route failed: %v", err)
	}

	strDefaultRoute := fmt.Sprintf("ip route add default dev %s via %s src %s table %d onlink", ifName, gw, srcIP, rtable)
	log.Infof(ctx, "add route: %s", strDefaultRoute)
	defaultCmd := p.exec.Command("ip", "route", "add", "default", "dev", ifName,
		"via", gw, "src", srcIP, "table", strconv.Itoa(rtable), "onlink")
	defaultCmd.SetStdout(os.Stdout)
	defaultCmd.SetStderr(os.Stderr)
	if err := defaultCmd.Run(); err != nil {
		return fmt.Errorf("add default route failed: %v", err)
	}

	return nil
}

func (p *rdmaPlugin) disableRPFCheck(ctx context.Context, devNums int) error {
	var errs []error
	rpfDevs := []string{"all", "default"}
	for idx := 0; idx < devNums; idx++ {
		rpfDevs = append(rpfDevs, fmt.Sprintf("%s%d", roceDevicePrefix, idx+1))
	}

	for _, name := range rpfDevs {
		if name != "" {
			if _, err := p.sysctl.Sysctl(fmt.Sprintf(rpFilterSysctlTemplate, name), "0"); err != nil {
				errs = append(errs, err)
				log.Errorf(ctx, "failed to disable RP filter for interface %v: %v", name, err)
			}
		}
	}

	for _, name := range []string{"all", "default"} {
		if name != "" {
			if _, err := p.sysctl.Sysctl(fmt.Sprintf(arpIgnoreSysctlTemplate, name), "0"); err != nil {
				errs = append(errs, err)
				log.Errorf(ctx, "failed to disable arp ignore for interface %v: %v", name, err)
			}
		}
	}

	return utilerrors.NewAggregate(errs)
}

func wantRoce(podNs, podName string, n *NetConf) (bool, error) {
	client, err := newClient(n.KubeConfig)
	if err != nil {
		return false, fmt.Errorf("build k8s client error: %s", err.Error())
	}

	pod, err := client.CoreV1().Pods(podNs).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	for _, container := range pod.Spec.Containers {
		if hasRDMAResource(container.Resources.Limits) || hasRDMAResource(container.Resources.Requests) {
			return true, nil
		}
	}

	return false, nil
}

func hasRDMAResource(rl v1.ResourceList) bool {
	for key, _ := range rl {
		arr := strings.Split(string(key), "/")
		if len(arr) != 2 {
			continue
		}
		if arr[0] == resourceName {
			return true
		}
	}
	return false
}

func newClient(kubeconfig string) (kubernetes.Interface, error) {
	config, err := buildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return k8sClientSet(config)
}
