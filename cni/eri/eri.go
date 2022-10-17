package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/keymutex"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/pkg/wrapper/netlink"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultKubeConfig      = "/etc/cni/net.d/cce-cni.d/cce-cni.kubeconfig"
	logFile                = "/var/log/cce/cni-eri.log"
	resourceName           = "rdma"
	fileLock               = "/var/run/cni-eri.lock"
	rpFilterSysctlTemplate = "net.ipv4.conf.%s.rp_filter"
)

var linkClient netlinkwrapper.Interface = netlinkwrapper.New()

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

type NetConf struct {
	types.NetConf
	KubeConfig string `json:"kubeconfig"`
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
	if n.KubeConfig == "" {
		n.KubeConfig = defaultKubeConfig
	}
	return n, n.CNIVersion, nil
}

func cmdAdd(args *skel.CmdArgs) error {
	ctx := log.NewContext()

	log.Infof(ctx, "====> CmdAdd Begins <====")
	defer log.Infof(ctx, "====> CmdAdd Ends <====")
	defer log.Flush()
	log.Infof(ctx, "[cmdAdd]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v", args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdAdd]: stdinData: %v", string(args.StdinData))

	if args.Netns == "" {
		log.Error(ctx, "args netns is empty")
		return nil
	}

	n, _, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	if n.RawPrevResult == nil {
		return fmt.Errorf("required prevResult is missing")
	}
	if err := version.ParsePrevResult(&n.NetConf); err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	defer netns.Close()

	k8sArgs, err := loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	need, err := isNeededERI(string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), n)
	if err != nil {
		log.Errorf(ctx, "check eri rdma limit error: %s", err.Error())
		return fmt.Errorf("check eri rdma limit error: %s", err.Error())
	}

	if !need {
		log.Infof(ctx, "pod is not required eri device")
		return types.PrintResult(n.PrevResult, n.CNIVersion)
	}

	l, err := keymutex.GrabFileLock(fileLock)
	if err != nil {
		log.Errorf(ctx, "grad file lock error: %s", err.Error())
		return fmt.Errorf("grad file lock error: %s", err.Error())
	}
	defer l.Close()

	eri, err := chooseERI(ctx)
	if err != nil {
		log.Errorf(ctx, "pod: %s get eri device error: %s", string(k8sArgs.K8S_POD_NAME), err.Error())
		return fmt.Errorf("get eri device error: %s", err.Error())
	}
	addrList, err := netlink.AddrList(eri, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list address from if %s, %w", eri.Attrs().Name, err)
	}
	err = acquireERI(eri, netns)
	if err != nil {
		log.Errorf(ctx, "set up eri error: %s for pod: %s: %s", string(k8sArgs.K8S_POD_NAME), err.Error())
		return fmt.Errorf("set up eri:%s error: %s", eri.Attrs().Name, err.Error())
	}

	err = setUpHostVeth(addrList, netns)
	if err != nil {
		log.Errorf(ctx, "set up host veth error: %s for pod: %s: %s", string(k8sArgs.K8S_POD_NAME), err.Error())
		return fmt.Errorf("set up host veth error: %s", err.Error())
	}
	return types.PrintResult(n.PrevResult, n.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	if args.Netns == "" {
		return nil
	}

	ctx := log.NewContext()
	log.Infof(ctx, "====> CmdDel Begins <====")
	defer log.Infof(ctx, "====> CmdDel Ends <====")
	log.Infof(ctx, "[cmdDel]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdDel]: stdinData: %v", string(args.StdinData))
	defer log.Flush()

	_, _, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	defer netns.Close()

	l, err := keymutex.GrabFileLock(fileLock)
	if err != nil {
		log.Errorf(ctx, "grad file lock error for del cmd: %s", err.Error())
		return fmt.Errorf("grad file lock error for del cmd: %s", err.Error())
	}
	defer l.Close()

	err = tearDown(ctx, netns)
	if err != nil {
		log.Errorf(ctx, "release eri error: %s", err.Error())
		return fmt.Errorf("release eri error: %s", err.Error())
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

	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("macvlan"))
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

// creates a k8s client
func newClient(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := buildConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func probeLinkName(prefix string) (string, error) {
	idx := 0
	for {
		devName := fmt.Sprintf("%s%d", prefix, idx)
		idx++
		_, err := netlink.LinkByName(devName)
		if err != nil {
			if _, ok := err.(netlink.LinkNotFoundError); ok {
				return devName, nil

			}
			return "", fmt.Errorf("failed to lookup %q: %v", devName, err)
		}
	}
	return "", fmt.Errorf("error get eri name in pod")
}

func chooseERI(ctx context.Context) (netlink.Link, error) {
	linkList, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	defaultRouteInterface, err := getDefaultRouteInterfaceName()
	if err != nil {
		return nil, err
	}

	for _, l := range linkList {
		if l.Attrs().Name == "lo" || l.Attrs().Name == defaultRouteInterface {
			continue
		}

		if l.Type() == "device" {
			addrs, err := netlink.AddrList(l, netlink.FAMILY_V4)
			if err != nil {
				continue
			}

			if len(addrs) < 1 {
				continue
			}
			return l, nil
		}
	}

	return nil, fmt.Errorf("there are not enouch eri device for pod")
}

func acquireERI(l netlink.Link, netNS ns.NetNS) error {
	if l == nil {
		return fmt.Errorf("eri device is nil!")
	}

	newName, err := randomDeviceName()
	if err != nil {
		return fmt.Errorf("falied to get host random device name: %s", err.Error())
	}
	//FAMILY_V4  FAMILY_ALL
	addrList, err := netlink.AddrList(l, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("error list address from if %s, %w", l.Attrs().Name, err)
	}

	if err = linkClient.LinkSetDown(l); err != nil {
		return fmt.Errorf("failed to set %q down: %v", l.Attrs().Name, err)
	}

	err = linkClient.LinkSetName(l, newName)
	if err != nil {
		return fmt.Errorf("failed to set host link name: %s", err.Error())
	}

	if err = linkClient.LinkSetUp(l); err != nil {
		return fmt.Errorf("failed to set %q up: %v", l.Attrs().Name, err)
	}

	err = linkClient.LinkSetNsFd(l, int(netNS.Fd()))
	if err != nil {
		return fmt.Errorf("failed to move eri device to pod : %s", err.Error())
	}

	err = netNS.Do(func(netNS ns.NetNS) error {
		contlink, err := netlink.LinkByName(newName)
		if err != nil {
			return fmt.Errorf("find link error: %s, %s", l.Attrs().Name, err.Error())
		}

		linkName, err := probeLinkName("eth")
		if err != nil {
			return fmt.Errorf("probe link name error: %s", err.Error())
		}

		if err = linkClient.LinkSetDown(contlink); err != nil {
			return fmt.Errorf("failed to set %q down: %v", l.Attrs().Name, err)
		}

		err = linkClient.LinkSetName(contlink, linkName)
		if err != nil {
			return fmt.Errorf("set link name error: %s", err.Error())
		}

		if err = linkClient.LinkSetUp(contlink); err != nil {
			return fmt.Errorf("failed to set %q up: %v", l.Attrs().Name, err)
		}

		for _, addr := range addrList {
			address, err := netlink.ParseAddr(addr.IPNet.String())
			if err != nil {
				return fmt.Errorf("failed to parse addr %s, %w", addr.IPNet.String(), err)
			}
			err = netlink.AddrReplace(contlink, address)
			if err != nil {
				return fmt.Errorf("failed to add addr %s, %s", addr.IPNet.String(), err.Error())
			}
		}

		sysctlValueName := fmt.Sprintf(rpFilterSysctlTemplate, linkName)
		if _, err := sysctl.Sysctl(sysctlValueName, "0"); err != nil {
			return fmt.Errorf("failed to set rp_filter to 0 on dev:%s, error:%s", linkName, err.Error())
		}

		if err = delScopeLinkRoute(contlink); err != nil {
			return err
		}
		return nil
	})

	return err
}

func releaseERI(l netlink.Link) error {
	if l == nil {
		return fmt.Errorf("eri device is nil!")
	}

	hostNetNS, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("err get host net ns, %w", err)
	}
	defer hostNetNS.Close()

	err = linkClient.LinkSetNsFd(l, int(hostNetNS.Fd()))
	if err != nil {
		return fmt.Errorf("error when set eri device to host: %s", err.Error())
	}
	return nil
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

func isNeededERI(podNs, podName string, n *NetConf) (bool, error) {
	client, err := newClient(n.KubeConfig)
	if err != nil {
		return false, fmt.Errorf("build crd client error: %s", err.Error())
	}

	pod, err := client.CoreV1().Pods(podNs).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	for _, container := range pod.Spec.Containers {
		if hasRDMARequest(container.Resources.Limits) || hasRDMARequest(container.Resources.Requests) {
			return true, nil
		}
	}

	return false, nil
}

func hasRDMARequest(rl v1.ResourceList) bool {
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

func initFlags() {
	log.InitFlags(nil)
	flag.Set("logtostderr", "false")
	flag.Set("log_file", logFile)
	flag.Parse()
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

func tearDown(ctx context.Context, netNS ns.NetNS) error {
	hostNetNS, err := ns.GetCurrentNS()
	if err != nil {
		log.Errorf(ctx, "get host ns error: %s", err.Error())
		return fmt.Errorf("get host ns error, %s", err.Error())
	}
	device2Address := make(map[string][]netlink.Addr)
	err = netNS.Do(func(hostNS ns.NetNS) error {
		linkList, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("get link list error:%s", err.Error())
		}
		for _, l := range linkList {
			if l.Attrs().Name == "lo" {
				continue
			}
			if l.Type() == "device" {
				name, err := randomDeviceName()
				if err != nil {
					log.Errorf(ctx, "get random device name error: %s", err.Error())
					continue
				}

				addrList, err := netlink.AddrList(l, netlink.FAMILY_V4)
				if err != nil {
					return fmt.Errorf("error list address from if %s, %w", l.Attrs().Name, err)
				}

				if err = linkClient.LinkSetDown(l); err != nil {
					return fmt.Errorf("failed to set %q down: %v", l.Attrs().Name, err)
				}
				err = linkClient.LinkSetName(l, name)
				if err != nil {
					log.Errorf(ctx, "set link rename name: %s to %s error: %s", l.Attrs().Name, name, err.Error())
					continue
				}
				if err = linkClient.LinkSetUp(l); err != nil {
					return fmt.Errorf("failed to set %q up: %v", l.Attrs().Name, err)
				}

				log.Infof(ctx, "set link rename name: %s to %s", l.Attrs().Name, name)
				err = linkClient.LinkSetNsFd(l, int(hostNetNS.Fd()))
				if err != nil {
					log.Errorf(ctx, "ip link set : %v host netns:%s error: %s", l.Attrs().Name, netNS.Path(), err.Error())
					continue
				}
				log.Infof(ctx, "ip link set : %v host netns:%s", l.Attrs().Name, netNS.Path())

				device2Address[name] = addrList
			}
		}
		return nil
	})

	for key, val := range device2Address {
		l, err := netlink.LinkByName(key)
		if err != nil {
			log.Errorf(ctx, "failed to find link: %s, %s", key, err.Error())
			return fmt.Errorf("failed to find link: %s, %s", key, err.Error())
		}

		sysctlValueName := fmt.Sprintf(rpFilterSysctlTemplate, key)
		if _, err := sysctl.Sysctl(sysctlValueName, "0"); err != nil {
			log.Errorf(ctx, "failed to set rp_filter to 0 on dev:%s, error:%s", key, err.Error())
		}

		for _, addr := range val {
			address, err := netlink.ParseAddr(addr.IPNet.String())
			if err != nil {
				log.Errorf(ctx, "failed to parse addr in host ns %s, %w", addr.IPNet.String(), err)
				return fmt.Errorf("failed to parse addr in host ns %s, %w", addr.IPNet.String(), err)
			}
			err = netlink.AddrReplace(l, address)
			if err != nil {
				log.Errorf(ctx, "failed to add addr in host ns %s, %w", addr.IPNet.String(), err)
				return fmt.Errorf("failed to add addr in host ns %s, %w", addr.IPNet.String(), err)
			}
		}
		if err = linkClient.LinkSetUp(l); err != nil {
			log.Errorf(ctx, "in host ns,failed to set %q up: %v", l.Attrs().Name, err)
			return fmt.Errorf("in host ns,failed to set %q up: %v", l.Attrs().Name, err)
		}
	}

	return err
}

// get the veth peer of container interface in host namespace
func getHostInterface(netns ns.NetNS) (netlink.Link, error) {
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
		return nil, fmt.Errorf("veth peer with index %d is not in host ns", peerIndex)
	}

	return link, nil
}

func setUpHostVeth(addrs []netlink.Addr, netns ns.NetNS) error {
	if len(addrs) < 1 {
		return fmt.Errorf("no address configured inside container")
	}

	addrBits := 32
	vethHost, err := getHostInterface(netns)
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		eriIP := addr.IPNet.IP
		err = netlink.RouteAdd(&netlink.Route{
			LinkIndex: vethHost.Attrs().Index,
			Scope:     netlink.SCOPE_LINK,
			Dst: &net.IPNet{
				IP:   eriIP,
				Mask: net.CIDRMask(addrBits, addrBits),
			},
		})

		if err != nil {
			return fmt.Errorf("failed to add host route dst %v: %v", eriIP, err)
		}
	}
	return nil
}

func delScopeLinkRoute(intf netlink.Link) error {
	addrs, err := netlink.AddrList(intf, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		dst := net.IPNet{
			IP:   addr.IP.Mask(addr.Mask),
			Mask: addr.Mask,
		}
		err = netlink.RouteDel(&netlink.Route{
			Dst:       &dst,
			Scope:     netlink.SCOPE_LINK,
			LinkIndex: intf.Attrs().Index,
		})
		if err != nil && !netlinkwrapper.IsNotExistError(err) {
			return err
		}
	}

	return nil
}

func randomDeviceName() (string, error) {
	entropy := make([]byte, 4)
	_, err := rand.Read(entropy)
	if err != nil {
		return "", fmt.Errorf("failed to generate random eri name: %v", err)
	}
	return fmt.Sprintf("eri%x", entropy), nil
}
