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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	osexec "os/exec"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/j-keck/arping"
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	logFile           = "/var/log/cce/cni-roce.log"
	rtStartIdx        = 100
	fileLock          = "/var/run/cni-roce.lock"
	roceDevicePrefix  = "rdma"
	resourceName      = "rdma"
	defaultKubeConfig = "/etc/cni/net.d/cce-cni.d/cce-cni.kubeconfig"
)

var buildConfigFromFlags = clientcmd.BuildConfigFromFlags
var k8sClientSet = func(c *rest.Config) (kubernetes.Interface, error) {
	clientSet, err := kubernetes.NewForConfig(c)
	return clientSet, err
}

type NetConf struct {
	types.NetConf
	IPAM IPAM `json:"ipam,omitempty"` // Shadows the JSON field "ipam" in cniTypes.NetConf.
	MTU  int  `json:"mtu"`
	//	Mask int `json:"mask"`
}

// IPAM is the CCE specific CNI IPAM configuration
type IPAM struct {
	types.IPAM
	ipamTypes.IPAMSpec
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

	return n, n.CNIVersion, nil
}

type rocePlugin struct {
	metaClient metadata.Interface
}

func newRocePlugin() *rocePlugin {
	return &rocePlugin{
		metaClient: metadata.NewClient(),
	}
}

func setupLogging(method string) (*logrus.Entry, error) {
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: "2006-01-02 15:03:04",
		CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
			fileName := path.Base(frame.File)
			return frame.Function, fmt.Sprintf("%s:%d", fileName, frame.Line)
		},
	})

	logDir := filepath.Dir(logFile)
	if err := os.Mkdir(logDir, 0755); err != nil && !os.IsExist(err) {
		return nil, err
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		logrus.SetOutput(file)
		loggerEntry := logrus.WithFields(logrus.Fields{
			"reqID":  uuid.NewV4().String(),
			"method": method,
		})

		return loggerEntry, nil
	}
	return nil, err
}

func (p *rocePlugin) cmdAdd(args *skel.CmdArgs) error {
	logger, err := setupLogging("ADD")
	if err != nil {
		return fmt.Errorf("failed to set up logging: %v", err)
	}
	logger.Infof("====> CmdAdd Begins: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	defer logger.Infof("====> CmdAdd Ends <====")

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("cmdAdd panic message:%v", r)
			logger.Error(debug.Stack())
		}
	}()

	n, cniVersion, err := loadConf(args.StdinData)
	if err != nil {
		logger.Errorf("loadConf err: %v", err)
		return err
	}
	if n.PrevResult == nil {
		logger.Errorf("unable to get previous network result.")
		return fmt.Errorf("unable to get previous network result.")
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	defer netns.Close()

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	ipConfigs, err := AllocateIP(string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), args, logger)
	if err != nil {
		logger.Errorf("AllocateIP failed: %v", err)
		return fmt.Errorf("AllocateIP failed:%v", err)
	}

	if len(ipConfigs) == 0 {
		logger.Info("not rdmaip found.")
		return types.PrintResult(n.PrevResult, cniVersion)
	}
	logger.Infof("ipConfigs: %v", ipConfigs)

	for _, ipconf := range ipConfigs {
		logger.WithFields(logrus.Fields{
			"ipconf": ipconf,
		}).Info("ipconf information")
	}

	l, err := GrabFileLock(fileLock)
	defer l.Close()
	if err != nil {
		logger.Errorf("grad file lock error: %s", err.Error())
		return fmt.Errorf("grad file lock error: %s", err.Error())
	}

	idx := 1
	for _, ipconf := range ipConfigs {
		roceDevName := fmt.Sprintf("%s%d", roceDevicePrefix, idx)
		ipvlanInterface, err := p.createIPvlan(n, ipconf.MasterMac, roceDevName, netns, logger)
		if err != nil {
			logger.Errorf("create ipvlan error: %v", err)
			return err
		}
		err = p.setupIPvlan(n, ipconf.MasterMac, roceDevName, netns, ipconf.Address, ipvlanInterface, ipconf.VifFeatures, logger)
		if err != nil {
			logger.Errorf("setup ipvlan error: %v", err)
			break
		}
		idx++
	}

	if err != nil {
		ip2devMap := make(map[string]string)
		_ = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
			return p.delAllIPVlanDevices(ip2devMap, logger)
		})
		return err
	}

	return types.PrintResult(n.PrevResult, cniVersion)
}

func (p *rocePlugin) cmdDel(args *skel.CmdArgs) error {
	logger, err := setupLogging("Del")
	if err != nil {
		return nil
	}
	logger.Infof("====> Delete Begins: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	if args.Netns == "" {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("cmdDel panic message:%v", r)
			logger.Error(debug.Stack())
		}
	}()

	k8sArgs, err := p.loadK8SArgs(args.Args)
	if err != nil {
		logger.Errorf("load k8s args failed: %v", err)
		return nil
	}

	l, err := GrabFileLock(fileLock)
	if err != nil {
		logger.Errorf("grad file lock error: %s", err.Error())
		return fmt.Errorf("grad file lock error: %s", err.Error())
	}
	ip2devMap := make(map[string]string)

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	err = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		return p.delAllIPVlanDevices(ip2devMap, logger)
	})

	_ = p.delAllPeranentNeigh(ip2devMap, logger)

	l.Close()
	if err != nil {
		logger.Errorf("delete ipVlan device failed:%v", err)
	}

	err = ReleaseIP(args, logger)
	if err != nil {
		msg := fmt.Sprintf("failed to release IP for pod (%v %v): %v", k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_NAME, err)
		logger.Error(msg)
		return nil
	}
	logger.Infof("release for pod(%v %v) successfully", k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_NAME)
	return nil
}

func (p *rocePlugin) delAllIPVlanDevices(ip2dev map[string]string, logger *logrus.Entry) error {
	links, err := netlink.LinkList()
	if err != nil {
		return err
	}
	for _, link := range links {
		if link.Type() == "ipvlan" {
			if strings.HasPrefix(link.Attrs().Name, roceDevicePrefix) {
				addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
				if err != nil {
					logger.Errorf("get ip address of interface: %s, error:%v", link.Attrs().Name, err)
				}
				for _, addr := range addrs {
					ip2dev[link.Attrs().Name] = addr.IP.String()
				}
				err = ip.DelLinkByName(link.Attrs().Name)
				if err != nil {
					logger.Infof("Delete link error:%v", err)
					continue
				}
				logger.Infof("Deleted interface:%s", link.Attrs().Name)
			}
		}
	}
	return nil
}

func (p *rocePlugin) delAllPeranentNeigh(ip2dev map[string]string, logger *logrus.Entry) error {
	links, err := netlink.LinkList()
	if err != nil {
		logger.Errorf("LinkList error:%v", err)
		return err
	}

	allNeighs := make([]netlink.Neigh, 0)
	for _, link := range links {
		if link.Type() == "device" {
			neighs, err := netlink.NeighList(link.Attrs().Index, netlink.FAMILY_V4)
			if err != nil {
				logger.Errorf("NeighList error:%v", err)
				return err
			}
			allNeighs = append(allNeighs, neighs...)
		}
	}

	for _, ip := range ip2dev {
		for _, neigh := range allNeighs {
			if neigh.IP.String() == ip && neigh.State == netlink.NUD_PERMANENT {
				err = netlink.NeighDel(&neigh)
				if err != nil {
					logger.Errorf("NeighDel error:%v", err)
				} else {
					logger.Infof("NeighDel Successfully :%v", neigh)
				}
			}
		}
	}

	return nil
}

func main() {
	plugin := newRocePlugin()
	skel.PluginMain(plugin.cmdAdd, plugin.cmdCheck, plugin.cmdDel, version.All, bv.BuildString("roce"))
}

func (p *rocePlugin) cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func (p *rocePlugin) loadK8SArgs(envArgs string) (*K8SArgs, error) {
	k8sArgs := K8SArgs{}
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

func (p *rocePlugin) createIPvlan(conf *NetConf, masterMac, ifName string, netns ns.NetNS, logger *logrus.Entry) (*current.Interface, error) {
	ipvlan := &current.Interface{}

	//m, err := netlink.LinkByName(master)
	m, err := GetLinkByMacAddress(masterMac)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup master %q: %v", masterMac, err)
	}

	// due to kernel bug we have to create with tmpName or it might
	// collide with the name on the host and error out
	tmpName, err := ip.RandomVethName()
	if err != nil {
		return nil, err
	}

	logger.Infof("create ipvlan args masterMac: %s,ifName:%s,tmpName:%s,MTU:%d,ParentIndex:%d", masterMac, ifName, tmpName, m.Attrs().MTU, m.Attrs().Index)
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

	if err := netlink.LinkAdd(iv); err != nil {
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
	err := ip.RenameLink(tmpName, ifName)
	if err != nil {
		_ = netlink.LinkDel(iv)
		return fmt.Errorf("failed to rename ipvlan to %q: %v", ifName, err)
	}
	ipvlan.Name = ifName

	// Re-fetch macvlan to get all properties/attributes
	contIPvlan, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to refetch ipvlan %q: %v", ifName, err)
	}
	ipvlan.Mac = contIPvlan.Attrs().HardwareAddr.String()
	ipvlan.Sandbox = netns.Path()

	return nil
}

func (p *rocePlugin) setupIPvlan(conf *NetConf, masterMac, ifName string, netns ns.NetNS,
	address net.IP, ipvlanInterface *current.Interface, VifFeatures string, logger *logrus.Entry) error {

	//m, err := netlink.LinkByName(master)
	m, err := GetLinkByMacAddress(masterMac)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", masterMac, err)
	}
	logger.Infof("create ipvlan dev: %s successfully,master:%s", ifName, m.Attrs().Name)

	defer func() {
		if err != nil {
			err = netns.Do(func(_ ns.NetNS) error {
				return ip.DelLinkByName(ifName)
			})
			if err != nil {
				logger.Errorf("delete link error in defer, device name: %s ,error:%v", ifName, err)
			}
		}
	}()

	masterMask := p.getDeviceMask(conf, masterMac)

	logger.Infof("master mac: %s,master mask: %v,for dev: %s", masterMac, masterMask, m.Attrs().Name)

	var allocIP *netlink.Addr
	err = netns.Do(func(_ ns.NetNS) error {
		allocIP, err = p.setupIPvlanNetworkInfo(masterMask, ifName, ipvlanInterface, address, VifFeatures, logger)
		return err
	})

	if err != nil {
		return err
	}

	if allocIP != nil {
		p.setUpPermanentARPByCMD(m, masterMask, *allocIP, logger)
	}
	return err
}

func (p *rocePlugin) getDeviceMask(conf *NetConf, devMac string) net.IPMask {
	link, err := GetLinkByMacAddress(devMac)
	if err != nil {
		return net.CIDRMask(28, 32)
	}

	addrList, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return net.CIDRMask(28, 32)
	}

	for _, addr := range addrList {
		return addr.IPNet.Mask
	}
	return net.CIDRMask(28, 32)
}

func (p *rocePlugin) setupIPvlanNetworkInfo(masterMask net.IPMask, ifName string,
	ipvlanInterface *current.Interface, address net.IP, VifFeatures string, logger *logrus.Entry) (*netlink.Addr, error) {
	ipvlanInterfaceLink, err := netlink.LinkByName(ifName)
	if err != nil {
		return nil, fmt.Errorf("failed to find interface name %q: %v", ipvlanInterface.Name, err)
	}

	if err := netlink.LinkSetUp(ipvlanInterfaceLink); err != nil {
		return nil, fmt.Errorf("failed to set %q UP: %v", ifName, err)
	}

	addr := &netlink.Addr{IPNet: &net.IPNet{
		IP:   address,
		Mask: masterMask,
	}}

	err = netlink.AddrAdd(ipvlanInterfaceLink, addr)
	if err != nil {
		logger.Errorf("failed to add IP %v to device : %v", addr.String(), err)
		return nil, err
	}

	idx := ipvlanInterfaceLink.Attrs().Index
	ruleSrc := &net.IPNet{
		IP:   addr.IPNet.IP,
		Mask: net.CIDRMask(32, 32),
	}

	err = p.addFromRule(ruleSrc, 10000, rtStartIdx+idx)
	if err != nil {
		logger.Errorf("add from rule failed: %v", err)
		return nil, err
	}
	logger.Infof("add rule table: %d,src ip: %s", rtStartIdx+idx, address.String())

	err = p.addOIFRule(ifName, 10000, rtStartIdx+idx)
	if err != nil {
		logger.Errorf("add from rule failed: %v", err)
		return nil, err
	}
	logger.Infof("add rule table: %d,oif: %s", rtStartIdx+idx, ifName)

	err = p.addRouteByCmd(masterMask, ifName, ruleSrc.IP.String(), rtStartIdx+idx, logger)
	if err != nil {
		logger.Errorf("add route failed: %s", err.Error())
		return nil, err
	}

	if VifFeatures == eriVifFeatures {
		logger.Infof("eri vif features ,src ip: %s", address.String())
		route := netlink.Route{
			LinkIndex: idx,
			Dst: &net.IPNet{
				IP:   address.Mask(masterMask),
				Mask: masterMask,
			},
			//Src:   address,
			Scope: netlink.SCOPE_LINK,
		}

		if err := netlink.RouteDel(&route); err != nil {
			logger.Errorf("failed to delete rdma route: %v", err)
			return nil, err
		}
	}

	return addr, nil
}

func (p *rocePlugin) addFromRule(addr *net.IPNet, priority int, rtTable int) error {
	rule := netlink.NewRule()
	rule.Table = rtTable
	rule.Priority = priority
	rule.Src = addr // ip rule add from `addr` lookup `table` prio `xxx`
	err := netlink.RuleDel(rule)
	if err != nil && !IsNotExistError(err) {
		return err
	}

	if err := netlink.RuleAdd(rule); err != nil {
		return err
	}
	return nil
}

func (p *rocePlugin) addOIFRule(oifName string, priority int, rtTable int) error {
	rule := netlink.NewRule()
	rule.Table = rtTable
	rule.Priority = priority
	rule.OifName = oifName // ip rule add oif `oifName` lookup `table` prio `xxx`
	err := netlink.RuleDel(rule)
	if err != nil && !IsNotExistError(err) {
		return err
	}

	if err := netlink.RuleAdd(rule); err != nil {
		return err
	}
	return nil
}

func (p *rocePlugin) addRouteByCmd(masterMask net.IPMask, ifName, srcIP string, rtable int, logger *logrus.Entry) error {
	gw, err := p.getGW(srcIP, masterMask)
	if err != nil {
		return err
	}

	strDefaultRoute := fmt.Sprintf("ip route add default dev %s via %s src %s table %d onlink", ifName, gw, srcIP, rtable)
	logger.Infof("add route: %s", strDefaultRoute)
	defaultCmd := osexec.Command("ip", "route", "add", "default", "dev", ifName,
		"via", gw, "src", srcIP, "table", strconv.Itoa(rtable), "onlink")
	defaultCmd.Stdout = os.Stdout
	defaultCmd.Stderr = os.Stderr
	if err := defaultCmd.Run(); err != nil {
		return fmt.Errorf("add default route failed: %v", err)
	}

	return nil
}

func (p *rocePlugin) addRoute(idx, rtTable int, src net.IP, masterMask net.IPMask, logger *logrus.Entry) error {
	ones, _ := masterMask.Size()
	_, network, err := net.ParseCIDR(src.String() + "/" + strconv.Itoa(ones))
	if err != nil {
		return fmt.Errorf("parse CIDR error: %v", err)
	}
	gw := make(net.IP, len(network.IP))
	ipU32 := binary.BigEndian.Uint32(network.IP)
	ipU32++
	binary.BigEndian.PutUint32(gw, ipU32)
	logger.Infof("add route by gw: %v,src:%v,mask:%v", gw, src, ones)
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
	err = netlink.RouteAdd(ro)

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

func (p *rocePlugin) getGWMac(master netlink.Link, masterMask net.IPMask, eriAddr netlink.Addr, logger *logrus.Entry) (string, error) {
	neighs, err := netlink.NeighList(master.Attrs().Index, netlink.FAMILY_V4)
	if err != nil {

	}
	strGw, err := p.getGW(eriAddr.IPNet.IP.String(), masterMask)
	if err != nil {
		return "", err
	}
	for _, neigh := range neighs {
		if neigh.IP.Equal(net.ParseIP(strGw)) {
			logger.Infof("gw neigh is existed: gw: %v,mac:%v", strGw, neigh.HardwareAddr)
			return neigh.HardwareAddr.String(), nil
		}
	}

	gwmac, dur, err := arping.PingOverIfaceByName(net.ParseIP(strGw), master.Attrs().Name)
	if err != nil {
		logger.Errorf("send arp over iface:%v failed: %v", master.Attrs().Name, err)
		return "", err
	}

	logger.Infof("send arp over iface:%v,dstip:%v,mac:%v,duration:%v", master.Attrs().Name, strGw, gwmac, dur)
	return gwmac.String(), nil
}

func (p *rocePlugin) setUpPermanentARP(master netlink.Link, masterMask net.IPMask, eriAddr netlink.Addr, logger *logrus.Entry) error {
	gwMacStr, err := p.getGWMac(master, masterMask, eriAddr, logger)
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
	if err := netlink.NeighAdd(req); err != nil {
		logger.Errorf("add ipvlan neigh error: %v", err)
		return err
	}
	logger.Infof("add master ipvlan neigh successfully, gw: %v,dst:%v", gwMac, eriAddr.IPNet.IP)
	defer func() {
		if err := netlink.NeighDel(req); err != nil {
			logger.Errorf("defer delete ipvlan neigh error: %v", err)
			return
		}
	}()
	return nil
}

// ip neigh add 25.11.128.45 lladdr ec:b9:70:b4:81:04 nud permanent dev eth6
func (p *rocePlugin) setUpPermanentARPByCMD(master netlink.Link, masterMask net.IPMask, eriAddr netlink.Addr, logger *logrus.Entry) error {
	gwMacStr, err := p.getGWMac(master, masterMask, eriAddr, logger)
	if err != nil {
		return err
	}

	strAddNeigh := fmt.Sprintf("ip neigh add %s lladdr %s nud permanent dev %s", eriAddr.IPNet.IP.String(), gwMacStr, master.Attrs().Name)
	logger.Infof("add neigh: %s", strAddNeigh)
	defaultCmd := osexec.Command("ip", "neigh", "add", eriAddr.IPNet.IP.String(), "lladdr", gwMacStr,
		"nud", "permanent", "dev", master.Attrs().Name)
	defaultCmd.Stdout = os.Stdout
	defaultCmd.Stderr = os.Stderr
	if err := defaultCmd.Run(); err != nil {
		logger.Errorf("add neith failed:%v", err)
		return fmt.Errorf("add neith failed: %v", err)
	}

	logger.Infof("add master neigh successfully, gw: %v,dst:%v", gwMacStr, eriAddr.IPNet.IP)
	return nil
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

func (p *rocePlugin) setUpHostVethRoute(master netlink.Link, eriAddr netlink.Addr, netns ns.NetNS, logger *logrus.Entry) error {
	addrs, err := netlink.AddrList(master, netlink.FAMILY_V4)
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

	logger.Infof("add host veth route: src: %v,dst: %v", addrs[0].IP, eriAddr.IPNet.IP)

	err = netlink.RouteAdd(&netlink.Route{
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
		linkList, err := netlink.LinkList()
		if err != nil {
			return err
		}
		for _, l := range linkList {
			if l.Type() != "veth" {
				continue
			}
			_, peerIndex, err = ip.GetVethPeerIfindex(l.Attrs().Name)
			break
		}
		return nil
	})
	if peerIndex <= 0 {
		return nil, fmt.Errorf("has no veth peer: %v", err)
	}

	// find host interface by index
	link, err := netlink.LinkByIndex(peerIndex)
	if err != nil {
		return nil, fmt.Errorf("veth peer with index %d is not in host ns,error: %s", peerIndex, err.Error())
	}

	return link, nil
}
