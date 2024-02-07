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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
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
	"github.com/containernetworking/cni/pkg/version"
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
	logFile                 = "/var/log/cce/cni-roce.log"
	rtStartIdx              = 100
	fileLock                = "/var/run/cni-roce.lock"
	roceDevicePrefix        = "rdma"
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

	if n.NetConf.RawPrevResult != nil {
		if err := version.ParsePrevResult(&n.NetConf); err != nil {
			return nil, "", fmt.Errorf("could not parse prevResult: %v", err)
		}
		_, err := current.NewResultFromResult(n.PrevResult)
		if err != nil {
			return nil, "", fmt.Errorf("could not convert result to current version: %v", err)
		}
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

type rocePlugin struct {
	nlink      netlinkwrapper.Interface
	ns         nswrapper.Interface
	ipam       ipamwrapper.Interface
	ip         ipwrapper.Interface
	types      typeswrapper.Interface
	netutil    networkutil.Interface
	rpc        rpcwrapper.Interface
	grpc       grpcwrapper.Interface
	exec       utilexec.Interface
	sysctl     sysctlwrapper.Interface
	metaClient metadata.Interface
}

func newRocePlugin() *rocePlugin {
	return &rocePlugin{
		nlink:      netlinkwrapper.New(),
		ns:         nswrapper.New(),
		ipam:       ipamwrapper.New(),
		ip:         ipwrapper.New(),
		types:      typeswrapper.New(),
		netutil:    networkutil.New(),
		rpc:        rpcwrapper.New(),
		grpc:       grpcwrapper.New(),
		exec:       utilexec.New(),
		sysctl:     sysctlwrapper.New(),
		metaClient: metadata.NewClient(),
	}
}

func (p *rocePlugin) cmdAdd(args *skel.CmdArgs) error {
	ctx := log.NewContext()
	log.Infof(ctx, "====> CmdAdd Begins: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	defer log.Infof(ctx, "====> CmdAdd Ends <====")
	defer log.Flush()
	ipam := NewRoceIPAM(p.grpc, p.rpc)

	n, cniVersion, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}
	if n.PrevResult == nil {
		return fmt.Errorf("unable to get previous network result")
	}
	netns, err := p.ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	defer netns.Close()

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
		return types.PrintResult(n.PrevResult, n.CNIVersion)
	}

	l, err := keymutex.GrabFileLock(fileLock)
	if err != nil {
		log.Errorf(ctx, "grad file lock error: %s", err.Error())
		return fmt.Errorf("grad file lock error: %s", err.Error())
	}

	rdmaDevs, err := p.getAllRdmaDevices(ctx)
	if err != nil || len(rdmaDevs) <= 0 {
		log.Infof(ctx, "not found roce devices err: %v", err)
		return types.PrintResult(n.PrevResult, cniVersion)
	}

	log.Infof(ctx, "rdma devs:%v", rdmaDevs)

	idx := 1
	roceDev2ipVlanInterface := make(map[string]*current.Interface)
	master2slave := make(map[string]string)

	keys := make([]string, 0, len(rdmaDevs))
	for devName := range rdmaDevs {
		keys = append(keys, devName)
	}
	sort.Strings(keys)

	for _, devName := range keys {
		roceDevName := fmt.Sprintf("%s%d", roceDevicePrefix, idx)
		ipvlanInterface, err := p.createIPvlan(n, devName, roceDevName, netns)
		if err != nil {
			log.Errorf(ctx, "create ipvlan error: %v", err)
			return err
		}
		roceDev2ipVlanInterface[devName] = ipvlanInterface
		master2slave[devName] = roceDevName
		idx++
	}

	for _, devName := range keys {
		err = p.setupIPvlan(ctx, n, devName, master2slave[devName], rdmaDevs[devName], netns, k8sArgs, ipam, roceDev2ipVlanInterface[devName])
		if err != nil {
			log.Errorf(ctx, "setup ipvlan error: %v", err)
			break
		}
	}

	if err != nil {
		ip2devMap := make(map[string]string)
		_ = p.ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
			return p.delAllIPVlanDevices(ctx, ip2devMap)
		})
		return err
	}

	l.Close()

	return types.PrintResult(n.PrevResult, cniVersion)
}

func (p *rocePlugin) cmdDel(args *skel.CmdArgs) error {
	ctx := log.NewContext()
	log.Infof(ctx, "====> CmdDel Begins <====")
	defer log.Infof(ctx, "====> CmdDel Ends <====")
	log.Infof(ctx, "[cmdDel]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v", args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdDel]: stdinData: %v", string(args.StdinData))
	if args.Netns == "" {
		return nil
	}

	n, _, err := loadConf(args.StdinData)
	if err != nil {
		log.Errorf(ctx, "load conf failed: %w", err)
		return nil
	}

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		log.Errorf(ctx, "load k8s args failed: %w", err)
		return nil
	}

	want, err := wantRoce(string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), n)
	if err != nil {
		log.Errorf(ctx, "check want roce failed: %s", err.Error())
		return nil
	}

	if !want {
		log.Infof(ctx, "pod: %s,donot want roce for del", string(k8sArgs.K8S_POD_NAME))
		return nil
	}

	l, err := keymutex.GrabFileLock(fileLock)
	if err != nil {
		log.Errorf(ctx, "grad file lock error: %s", err.Error())
		return fmt.Errorf("grad file lock error: %s", err.Error())
	}
	ip2devMap := make(map[string]string)

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	err = p.ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		return p.delAllIPVlanDevices(ctx, ip2devMap)
	})

	_ = p.delAllPeranentNeigh(ctx, ip2devMap)

	l.Close()
	if err != nil {
		log.Errorf(ctx, "delete ipVlan device failed:%v", err)
	}

	ipamClient := NewRoceIPAM(p.grpc, p.rpc)
	resp, err := ipamClient.ReleaseIP(ctx, k8sArgs, n.IPAM.Endpoint)
	if err != nil {
		msg := fmt.Sprintf("failed to delete IP for pod (%v %v): %v", k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_NAME, err)
		log.Error(ctx, msg)
		return nil
	}

	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server release IP error: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return nil
	}

	log.Infof(ctx, "release for pod(%v %v) successfully", k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_NAME)
	return nil
}

func (p *rocePlugin) delAllIPVlanDevices(ctx context.Context, ip2dev map[string]string) error {
	links, err := p.nlink.LinkList()
	if err != nil {
		return err
	}
	for _, link := range links {
		if link.Type() == "ipvlan" {
			if strings.HasPrefix(link.Attrs().Name, roceDevicePrefix) {
				addrs, err := p.nlink.AddrList(link, netlink.FAMILY_V4)
				if err != nil {
					log.Errorf(ctx, "get ip address of interface: %s, error:%v", link.Attrs().Name, err)
				}
				for _, addr := range addrs {
					ip2dev[link.Attrs().Name] = addr.IP.String()
				}
				err = p.ip.DelLinkByName(link.Attrs().Name)
				if err != nil {
					log.Infof(ctx, "Delete link error:%v", err)
					continue
				}
				log.Infof(ctx, "Deleted interface:%s", link.Attrs().Name)
			}
		}
	}
	return nil
}

func (p *rocePlugin) delAllPeranentNeigh(ctx context.Context, ip2dev map[string]string) error {
	links, err := p.nlink.LinkList()
	if err != nil {
		log.Errorf(ctx, "LinkList error:", err)
		return err
	}

	allNeighs := make([]netlink.Neigh, 0)
	for _, link := range links {
		if link.Type() == "device" {
			neighs, err := p.nlink.NeighList(link.Attrs().Index, netlink.FAMILY_V4)
			if err != nil {
				log.Errorf(ctx, "NeighList error:", err)
				return err
			}
			allNeighs = append(allNeighs, neighs...)
		}
	}

	for _, ip := range ip2dev {
		for _, neigh := range allNeighs {
			if neigh.IP.String() == ip && neigh.State == netlink.NUD_PERMANENT {
				err = p.nlink.NeighDel(&neigh)
				if err != nil {
					log.Errorf(ctx, "NeighDel error:", err)
				} else {
					log.Infof(ctx, "NeighDel Successfully :%v", neigh)
				}
			}
		}
	}

	return nil
}

func main() {
	cni.InitFlags(logFile)
	defer log.Flush()

	logDir := filepath.Dir(logFile)
	if err := os.Mkdir(logDir, 0755); err != nil && !os.IsExist(err) {
		fmt.Printf("mkdir %v failed: %v", logDir, err)
		os.Exit(1)
	}

	plugin := newRocePlugin()
	if e := skel.PluginMainWithError(plugin.cmdAdd, plugin.cmdCheck, plugin.cmdDel, cni.PluginSupportedVersions, bv.BuildString("roce")); e != nil {
		log.Flush()
		if err := e.Print(); err != nil {
			log.Errorf(context.TODO(), "Error writing error JSON to stdout: %v", err)
		}
		os.Exit(1)
	}

}

func (p *rocePlugin) cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func (p *rocePlugin) loadK8SArgs(envArgs string) (*cni.K8SArgs, error) {
	k8sArgs := cni.K8SArgs{}
	if envArgs != "" {
		err := types.LoadArgs(envArgs, &k8sArgs)
		if err != nil {
			return nil, err
		}
	}
	return &k8sArgs, nil
}

func modeFromString(s string) (netlink.IPVlanMode, error) {
	switch s {
	case "", "l3":
		return netlink.IPVLAN_MODE_L3, nil
	case "l2":
		return netlink.IPVLAN_MODE_L2, nil
	case "l3s":
		return netlink.IPVLAN_MODE_L3S, nil
	default:
		return 0, fmt.Errorf("unknown ipvlan mode: %q", s)
	}
}

func (p *rocePlugin) createIPvlan(conf *NetConf, master, ifName string, netns ns.NetNS) (*current.Interface, error) {
	ipvlan := &current.Interface{}

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

	iv := &netlink.IPVlan{
		LinkAttrs: netlink.LinkAttrs{
			MTU:         m.Attrs().MTU,
			Name:        tmpName,
			ParentIndex: m.Attrs().Index,
			Namespace:   netlink.NsFd(int(netns.Fd())),
		},
		Mode: netlink.IPVLAN_MODE_L2,
		//		Flag: netlink.IPVLAN_FLAG_VEPA,
	}

	if err := p.nlink.LinkAdd(iv); err != nil {
		return nil, fmt.Errorf("failed to create ipvlan for eri rdma: %v", err)
	}

	err = netns.Do(func(_ ns.NetNS) error {
		return p.setupIPvlanInterface(ipvlan, iv, tmpName, ifName, netns)
	})
	if err != nil {
		return nil, err
	}

	return ipvlan, nil
}

func (p *rocePlugin) setupIPvlanInterface(ipvlan *current.Interface, iv *netlink.IPVlan, tmpName, ifName string, netns ns.NetNS) error {
	err := p.ip.RenameLink(tmpName, ifName)
	if err != nil {
		_ = p.nlink.LinkDel(iv)
		return fmt.Errorf("failed to rename ipvlan to %q: %v", ifName, err)
	}
	ipvlan.Name = ifName

	// Re-fetch macvlan to get all properties/attributes
	contIPvlan, err := p.nlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to refetch ipvlan %q: %v", ifName, err)
	}
	ipvlan.Mac = contIPvlan.Attrs().HardwareAddr.String()
	ipvlan.Sandbox = netns.Path()

	return nil
}

func (p *rocePlugin) setupIPvlan(ctx context.Context, conf *NetConf, master,
	ifName string, vifFeature string, netns ns.NetNS,
	k8sArgs *cni.K8SArgs, ipamClient *roceIPAM, ipvlanInterface *current.Interface) error {

	log.Infof(ctx, "create ipvlan dev: %s successfully,master:%s", ifName, master)

	m, err := p.nlink.LinkByName(master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", master, err)
	}

	defer func() {
		if err != nil {
			err = netns.Do(func(_ ns.NetNS) error {
				return ip.DelLinkByName(ifName)
			})
			if err != nil {
				log.Errorf(ctx, "delete link error in defer, device name: %s ,error:%v", ifName, err)
			}
		}
	}()

	masterMac := m.Attrs().HardwareAddr.String()
	masterMask := p.getDeviceMask(conf, master)

	log.Infof(ctx, "master mac: %s,master mask: %v,for dev: %s", masterMac, masterMask, master)

	var allocIP *netlink.Addr
	err = netns.Do(func(_ ns.NetNS) error {
		allocIP, err = p.setupIPvlanNetworkInfo(ctx, conf, masterMac, masterMask, ifName, vifFeature, ipvlanInterface, k8sArgs, ipamClient)
		return err
	})

	if err != nil {
		return err
	}

	if allocIP != nil {
		p.setUpPermanentARPByCMD(ctx, m, masterMask, *allocIP)
	}
	return err
}

func (p *rocePlugin) getDeviceMask(conf *NetConf, devName string) net.IPMask {
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

func (p *rocePlugin) setupIPvlanNetworkInfo(ctx context.Context, conf *NetConf, masterMac string, masterMask net.IPMask, ifName string, vifFeature string,
	ipvlanInterface *current.Interface, k8sArgs *cni.K8SArgs, ipamClient *roceIPAM) (*netlink.Addr, error) {
	ipvlanInterfaceLink, err := p.nlink.LinkByName(ifName)
	if err != nil {
		return nil, fmt.Errorf("failed to find interface name %q: %v", ipvlanInterface.Name, err)
	}

	if err := p.nlink.LinkSetUp(ipvlanInterfaceLink); err != nil {
		return nil, fmt.Errorf("failed to set %q UP: %v", ifName, err)
	}
	name, namespace := string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE)
	startT := time.Now()
	resp, err := ipamClient.AllocIP(ctx, k8sArgs, conf.IPAM.Endpoint, masterMac, vifFeature)
	log.Infof(ctx, "AllocIP cost: %v", time.Since(startT))
	if err != nil {
		log.Errorf(ctx, "failed to allocate IP: %v", err)
		return nil, err
	}
	if !resp.IsSuccess {
		msg := fmt.Sprintf("ipam server allocate IP error: %v", resp.ErrMsg)
		log.Error(ctx, msg)
		return nil, errors.New(msg)
	}

	allocRespNetworkInfo := resp.GetENIMultiIP()
	if allocRespNetworkInfo == nil {
		err := errors.New(fmt.Sprintf("failed to allocate IP for pod (%v %v): NetworkInfo is nil", namespace, name))
		log.Errorf(ctx, err.Error())
		return nil, err
	}

	log.Infof(ctx, "allocate IP %v, for pod(%v %v) successfully", allocRespNetworkInfo.IP, namespace, name)

	defer func() {
		if err != nil {
			_, err := ipamClient.ReleaseIP(ctx, k8sArgs, conf.IPAM.Endpoint)
			if err != nil {
				log.Errorf(ctx, "rollback: failed to delete IP for pod (%v %v): %v", namespace, name, err)
			}
		}
	}()

	addr := &netlink.Addr{IPNet: &net.IPNet{
		IP:   net.ParseIP(allocRespNetworkInfo.IP),
		Mask: masterMask,
	}}

	err = p.nlink.AddrAdd(ipvlanInterfaceLink, addr)
	if err != nil {
		log.Errorf(ctx, "failed to add IP %v to device : %v", addr.String(), err)
		return nil, err
	}

	idx := ipvlanInterfaceLink.Attrs().Index
	ruleSrc := &net.IPNet{
		IP:   addr.IPNet.IP,
		Mask: net.CIDRMask(32, 32),
	}

	err = p.addFromRule(ruleSrc, 10000, rtStartIdx+idx)
	if err != nil {
		log.Errorf(ctx, "add from rule failed: %v", err)
		return nil, err
	}
	log.Infof(ctx, "add rule table: %d,src ip: %s", rtStartIdx+idx, allocRespNetworkInfo.IP)

	err = p.addOIFRule(ifName, 10000, rtStartIdx+idx)
	if err != nil {
		log.Errorf(ctx, "add from rule failed: %v", err)
		return nil, err
	}
	log.Infof(ctx, "add rule table: %d,oif: %s", rtStartIdx+idx, ifName)

	err = p.addRouteByCmd(ctx, masterMask, ifName, ruleSrc.IP.String(), rtStartIdx+idx)
	if err != nil {
		log.Errorf(ctx, "add route failed: %s", err.Error())
		return nil, err
	}

	return addr, nil
}

func (p *rocePlugin) addFromRule(addr *net.IPNet, priority int, rtTable int) error {
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

func (p *rocePlugin) addOIFRule(oifName string, priority int, rtTable int) error {
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

func (p *rocePlugin) addRouteByCmd(ctx context.Context, masterMask net.IPMask, ifName, srcIP string, rtable int) error {
	gw, err := p.getGW(srcIP, masterMask)
	if err != nil {
		return err
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

func (p *rocePlugin) addRoute(ctx context.Context, idx, rtTable int, src net.IP, masterMask net.IPMask) error {
	ones, _ := masterMask.Size()
	_, network, err := net.ParseCIDR(src.String() + "/" + strconv.Itoa(ones))
	if err != nil {
		return fmt.Errorf("parse CIDR error: %v", err)
	}
	gw := make(net.IP, len(network.IP))
	ipU32 := binary.BigEndian.Uint32(network.IP)
	ipU32++
	binary.BigEndian.PutUint32(gw, ipU32)
	log.Infof(ctx, "add route by gw: %v,src:%v,mask:%v", gw, src, ones)
	ro := &netlink.Route{
		LinkIndex: idx,
		Scope:     netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   net.IPv4(25, 11, 0, 0),
			Mask: net.CIDRMask(16, 32),
		},
		Src:   src,
		Table: rtTable,
		Gw:    gw,
	}
	ro.SetFlag(netlink.FLAG_ONLINK)
	err = p.nlink.RouteAdd(ro)

	if err != nil {
		return fmt.Errorf("failed to add container route dst: %v", err)
	}
	return nil
}

func (p *rocePlugin) getGW(ip string, masterMask net.IPMask) (string, error) {
	ones, _ := masterMask.Size()
	_, network, err := net.ParseCIDR(ip + "/" + strconv.Itoa(ones))
	if err != nil {
		return "", fmt.Errorf("parse CIDR error: %v", err)
	}
	gw := make(net.IP, len(network.IP))
	ipU32 := binary.BigEndian.Uint32(network.IP)
	ipU32++
	binary.BigEndian.PutUint32(gw, ipU32)

	return gw.String(), nil
}

func (p *rocePlugin) getGWMac(ctx context.Context, master netlink.Link, masterMask net.IPMask, eriAddr netlink.Addr) (string, error) {
	neighs, err := netlink.NeighList(master.Attrs().Index, netlink.FAMILY_V4)
	if err != nil {

	}
	strGw, err := p.getGW(eriAddr.IPNet.IP.String(), masterMask)
	if err != nil {
		return "", err
	}
	for _, neigh := range neighs {
		if neigh.IP.Equal(net.ParseIP(strGw)) {
			log.Infof(ctx, "gw neigh is existed: gw: %v,mac:%v", strGw, neigh.HardwareAddr)
			return neigh.HardwareAddr.String(), nil
		}
	}

	gwmac, dur, err := p.netutil.PingOverIfaceByName(net.ParseIP(strGw), master.Attrs().Name)
	if err != nil {
		log.Errorf(ctx, "send arp over iface:%v failed: %v", master.Attrs().Name, err)
		return "", err
	}

	log.Infof(ctx, "send arp over iface:%v,dstip:%v,mac:%v,duration:%v", master.Attrs().Name, strGw, gwmac, dur)
	return gwmac.String(), nil
}

func (p *rocePlugin) setUpPermanentARP(ctx context.Context, master netlink.Link, masterMask net.IPMask, eriAddr netlink.Addr) error {
	gwMacStr, err := p.getGWMac(ctx, master, masterMask, eriAddr)
	if err != nil {
		return err
	}

	gwMac, err := net.ParseMAC(gwMacStr)
	if err != nil {
		return err
	}
	req := &netlink.Neigh{
		LinkIndex:    master.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           eriAddr.IPNet.IP,
		HardwareAddr: gwMac,
	}
	if err := p.nlink.NeighAdd(req); err != nil {
		log.Errorf(ctx, "add ipvlan neigh error: %v", err)
		return err
	}
	log.Infof(ctx, "add master ipvlan neigh successfully, gw: %v,dst:%v", gwMac, eriAddr.IPNet.IP)
	defer func() {
		if err := p.nlink.NeighDel(req); err != nil {
			log.Errorf(ctx, "defer delete ipvlan neigh error: %v", err)
			return
		}
	}()
	return nil
}

// ip neigh add 25.11.128.45 lladdr ec:b9:70:b4:81:04 nud permanent dev eth6
func (p *rocePlugin) setUpPermanentARPByCMD(ctx context.Context, master netlink.Link, masterMask net.IPMask, eriAddr netlink.Addr) error {
	gwMacStr, err := p.getGWMac(ctx, master, masterMask, eriAddr)
	if err != nil {
		return err
	}

	strAddNeigh := fmt.Sprintf("ip neigh add %s lladdr %s nud permanent dev %s", eriAddr.IPNet.IP.String(), gwMacStr, master.Attrs().Name)
	log.Infof(ctx, "add neigh: %s", strAddNeigh)
	defaultCmd := p.exec.Command("ip", "neigh", "add", eriAddr.IPNet.IP.String(), "lladdr", gwMacStr,
		"nud", "permanent", "dev", master.Attrs().Name)
	defaultCmd.SetStdout(os.Stdout)
	defaultCmd.SetStderr(os.Stderr)
	if err := defaultCmd.Run(); err != nil {
		log.Errorf(ctx, "add neith failed:%v", err)
		return fmt.Errorf("add neith failed: %v", err)
	}

	log.Infof(ctx, "add master neigh successfully, gw: %v,dst:%v", gwMacStr, eriAddr.IPNet.IP)
	return nil
}
func (p *rocePlugin) disableRPFCheck(ctx context.Context, devNums int) error {
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

func (p *rocePlugin) setUpHostVethRoute(ctx context.Context, master netlink.Link, eriAddr netlink.Addr, netns ns.NetNS) error {
	addrs, err := p.nlink.AddrList(master, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	if len(addrs) < 1 {
		return fmt.Errorf("there is not ip for master,index: %d", master.Attrs().Index)
	}
	addrBits := 32
	vethHost, err := p.getVethHostInterface(netns)
	if err != nil {
		return err
	}

	log.Infof(ctx, "add host veth route: src: %v,dst: %v", addrs[0].IP, eriAddr.IPNet.IP)

	err = p.nlink.RouteAdd(&netlink.Route{
		LinkIndex: vethHost.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   eriAddr.IPNet.IP,
			Mask: net.CIDRMask(addrBits, addrBits),
		},
		Src: addrs[0].IP,
	})

	if err != nil {
		return fmt.Errorf("failed to add host route src: %v, dst: %v, error: %v", addrs[0].IP, eriAddr.IPNet.IP, err)
	}
	return err
}

// get the veth peer of container interface in host namespace
func (p *rocePlugin) getVethHostInterface(netns ns.NetNS) (netlink.Link, error) {
	var peerIndex int
	var err error
	_ = netns.Do(func(_ ns.NetNS) error {
		linkList, err := p.nlink.LinkList()
		if err != nil {
			return err
		}
		for _, l := range linkList {
			if l.Type() != "veth" {
				continue
			}
			_, peerIndex, err = p.ip.GetVethPeerIfindex(l.Attrs().Name)
			break
		}
		return nil
	})
	if peerIndex <= 0 {
		return nil, fmt.Errorf("has no veth peer: %v", err)
	}

	// find host interface by index
	link, err := p.nlink.LinkByIndex(peerIndex)
	if err != nil {
		return nil, fmt.Errorf("veth peer with index %d is not in host ns,error: %s", peerIndex, err.Error())
	}

	return link, nil
}
