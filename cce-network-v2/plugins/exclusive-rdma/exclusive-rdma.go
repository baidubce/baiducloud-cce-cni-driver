package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/plugins/exclusive-rdma/keymutex"
	netlinkutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/plugins/exclusive-rdma/netlink"
)

var logger *logrus.Entry

const (
	resourceName           = "rdma"
	rdmaDeviceNamePrefix   = "rdma"
	fileLock               = "/var/run/cni-exclusive-rdma.lock"
	rpFilterSysctlTemplate = "net.ipv4.conf.%s.rp_filter"
	noMoreDevice           = "no more exclusive-rdma device for pod"
	udsSocketFilePath      = "/var/run/exclusive-rdma/exclusive-rdma-cni-plugin.sock"
)

// K8SArgs k8s pod args
type K8SArgs struct {
	types.CommonArgs `json:"commonArgs"`

	IP                         net.IP                     `json:"ip"`                         // IP is pod's ip address
	K8S_POD_NAME               types.UnmarshallableString `json:"k8s_pod_name"`               // K8S_POD_NAME is pod's name
	K8S_POD_NAMESPACE          types.UnmarshallableString `json:"k8s_pod_namespace"`          // K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString `json:"k8s_pod_infra_container_id"` // K8S_POD_INFRA_CONTAINER_ID is pod's container ID
}

type NetConf struct {
	types.NetConf
	RdmaDriverName string `json:"driver,omitempty"`
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
	return n, n.CNIVersion, nil
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "exclusive-rdma",
		"mod":     "ADD",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()

	if args.Netns == "" {
		logger.Error("args netns is empty")
		return nil
	}

	n, _, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	if n.RawPrevResult == nil {
		return errors.New("required prevResult is missing")
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

	need, err := isNeededExclusiveRdma(string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
	if err != nil {
		logger.Errorf("check exclusive-rdma device limit error: %s", err.Error())
		return fmt.Errorf("check exclusive-rdma device limit error: %s", err.Error())
	}

	if !need {
		logger.Infof("pod is not required exclusive-rdma device")
		return types.PrintResult(n.PrevResult, n.CNIVersion)
	}

	// To prevent other pods from invoking this plugin at the same time, which would cause the RDMA network devices to be non-exclusive.
	l, err := keymutex.GrabFileLock(fileLock)
	if err != nil {
		logger.Errorf("grad file lock error: %s", err.Error())
		return fmt.Errorf("grad file lock error: %s", err.Error())
	}
	defer l.Close()

	// Move each RDMA network device to the corresponding pod's network namespace
	exclusiveRdmaDeviceCount := 0
	for {
		exclusiveRdma, addrList, err := chooseExclusiveRdmaDevice(n)
		// If the rdma network device cannot be found for the first time, it indicates a "no exclusive-rdma device" error.
		// If it's not the first time, and the error is still "network device not found",
		// It is assumed that the operation has been completed and all network devices have been moved in.
		// Otherwise, an error is returned
		if err != nil && exclusiveRdmaDeviceCount == 0 {
			logger.Errorf("there is no exclusive-rdma device for pod(%s), error: %s", string(k8sArgs.K8S_POD_NAME), err.Error())
			return fmt.Errorf("there is no exclusive-rdma device error: %s", err.Error())
		} else if err == errors.New(noMoreDevice) {
			break
		} else if err != nil {
			logger.Errorf("pod: %s get exclusive-rdma device error: %s", string(k8sArgs.K8S_POD_NAME), err.Error())
			return fmt.Errorf("get exclusive-rdma device error: %s", err.Error())
		}

		// move exclusive-rdma device to pod's network namespace and set it up
		err = acquireExclusiveRdma(exclusiveRdma, netns, addrList)
		if err != nil {
			logger.Errorf("set up exclusive-rdma error for pod(%s): %s", string(k8sArgs.K8S_POD_NAME), err.Error())
			return fmt.Errorf("set up exclusive-rdma device for %s error: %s", exclusiveRdma.Attrs().Name, err.Error())
		}

		// set up host veth in host network namespace
		err = setUpHostVeth(addrList, netns)
		if err != nil {
			logger.Errorf("set up host veth error for pod(%s): %s", string(k8sArgs.K8S_POD_NAME), err.Error())
			return fmt.Errorf("set up host veth error: %s", err.Error())
		}
		exclusiveRdmaDeviceCount++
	}
	return types.PrintResult(n.PrevResult, n.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) (err error) {
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "exclusive-rdma",
		"mod":     "DEL",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()

	if args.Netns == "" {
		return nil
	}

	_, _, err = loadConf(args.StdinData)
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
		logger.Errorf("grad file lock error for del cmd: %s", err.Error())
		return fmt.Errorf("grad file lock error for del cmd: %s", err.Error())
	}
	defer l.Close()

	err = tearDown(logger, netns)
	if err != nil {
		logger.Errorf("release exclusive-rdma device error: %s", err.Error())
		return fmt.Errorf("release exclusive-rdma devuce error: %s", err.Error())
	}

	return nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("execulsive-rdma"))
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func probeLinkName(prefix string) (string, error) {
	idx := 0
	// The max number of devices is 9999
	for idx <= 9999 {
		devName := fmt.Sprintf("%s%d", prefix, idx)
		_, err := netlink.LinkByName(devName)
		if err != nil {
			if _, ok := err.(netlink.LinkNotFoundError); ok {
				return devName, nil
			}
			return "", fmt.Errorf("failed to lookup %q: %v", devName, err)
		}
		idx++
	}
	return "", errors.New("error get exclusive-rdma name in pod")
}

// choose an rdma device and return, together with its address list
func chooseExclusiveRdmaDevice(n *NetConf) (netlink.Link, *[]netlink.Addr, error) {
	linkList, err := netlink.LinkList()
	if err != nil {
		return nil, nil, err
	}

	// in some case, the ethernet device, which is used to connect to cluster, is also probably using the same driver with rdma device
	// so we should skip it, by checking if the device is binding to default route. if it is, skip it
	var defaultLinkName string
	routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, nil, err
	}
	for _, route := range routes {
		// default route is nil
		if route.Dst == nil {
			link, err := netlink.LinkByIndex(route.LinkIndex)
			if err != nil {
				err = fmt.Errorf("failed to get default route link: %v", err)
				return nil, nil, err
			}
			defaultLinkName = link.Attrs().Name
			break
		}
	}

	for _, link := range linkList {
		linkName := link.Attrs().Name
		// skip the default link
		if linkName == defaultLinkName {
			continue
		}
		driverPath := filepath.Join("/sys/class/net", linkName, "device/driver")
		driver, err := os.Readlink(driverPath)
		// if the device is a virtual device, skip it
		if err != nil {
			continue
		}
		// if the device is a rdma device, get its addrlist and return it
		if filepath.Base(driver) == n.RdmaDriverName {
			addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
			if err != nil || len(addrs) < 1 {
				// mabye we should do somthing here
				continue
			}
			return link, &addrs, nil
		}
	}

	return nil, nil, errors.New(noMoreDevice)
}

func acquireExclusiveRdma(l netlink.Link, netNS ns.NetNS, addrList *[]netlink.Addr) error {
	if l == nil {
		return errors.New("exclusive-rdma device is nil, please check")
	}

	newName, err := randomDeviceName()
	if err != nil {
		return fmt.Errorf("falied to get host random device name: %s", err.Error())
	}

	if err = netlink.LinkSetDown(l); err != nil {
		return fmt.Errorf("failed to set %q down: %v", l.Attrs().Name, err)
	}

	err = netlink.LinkSetName(l, newName)
	if err != nil {
		return fmt.Errorf("failed to set host link name: %s", err.Error())
	}

	if err = netlink.LinkSetUp(l); err != nil {
		return fmt.Errorf("failed to set %q up: %v", l.Attrs().Name, err)
	}

	err = netlink.LinkSetNsFd(l, int(netNS.Fd()))
	if err != nil {
		return fmt.Errorf("failed to move exclusive-rdma device to pod : %s", err.Error())
	}

	err = netNS.Do(func(netNS ns.NetNS) error {
		contlink, err := netlink.LinkByName(newName)
		if err != nil {
			return fmt.Errorf("find link error: %s, %s", l.Attrs().Name, err.Error())
		}

		linkName, err := probeLinkName(rdmaDeviceNamePrefix)
		if err != nil {
			return fmt.Errorf("probe link name error: %s", err.Error())
		}

		if err = netlink.LinkSetDown(contlink); err != nil {
			return fmt.Errorf("failed to set %q down: %v", l.Attrs().Name, err)
		}

		err = netlink.LinkSetName(contlink, linkName)
		if err != nil {
			return fmt.Errorf("set link name error: %s", err.Error())
		}

		if err = netlink.LinkSetUp(contlink); err != nil {
			return fmt.Errorf("failed to set %q up: %v", l.Attrs().Name, err)
		}

		for _, addr := range *addrList {
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

		// TODO:If there is a strategic route for different rdma devices, it needs to be added here.
		//		(It may be necessary to add policy routing for the RDMA network card.)

		return nil
	})

	return err
}

func releaseExclusiveRdma(l netlink.Link) error {
	if l == nil {
		return errors.New("exclusive-rdma device is nil, please check")
	}

	hostNetNS, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("err get host net ns, %w", err)
	}
	defer hostNetNS.Close()

	err = netlink.LinkSetNsFd(l, int(hostNetNS.Fd()))
	if err != nil {
		return fmt.Errorf("error when set exclusive-rdma device to host: %s", err.Error())
	}
	return nil
}

// this func is used to judge whether the pod needs exclusive-rdma devices.
// actually, it is sending an http request (using uds) to agent, then get and read response.
func isNeededExclusiveRdma(podNs, podName string) (bool, error) {
	// create a client to send http request using unix domain socket
	transport := &http.Transport{
		Dial: func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", udsSocketFilePath)
		},
	}
	client := &http.Client{
		Transport: transport,
	}
	// pack infomation to url and send to agent
	params := url.Values{}
	params.Add("podNs", podNs)
	params.Add("podName", podName)

	resp, err := client.Get("http://unix/?" + params.Encode())
	if err != nil {
		return false, fmt.Errorf("send http request error: %s", err.Error())
	}
	defer resp.Body.Close()

	// read response body, which is expected to be a string "true" or "false", true means need exclusive-rdma device, false means no
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read response body error: %s", err.Error())
	}
	if string(body) == "true" {
		return true, nil
	} else if string(body) == "false" {
		return false, nil
	} else {
		return false, fmt.Errorf("agent returns an error: %s", string(body))
	}
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

func tearDown(logger logrus.FieldLogger, netNS ns.NetNS) error {
	hostNetNS, err := ns.GetCurrentNS()
	if err != nil {
		logger.Errorf("get host ns error: %s", err.Error())
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
					logger.Errorf("get random device name error: %s", err.Error())
					continue
				}

				addrList, err := netlink.AddrList(l, netlink.FAMILY_V4)
				if err != nil {
					return fmt.Errorf("error list address from if %s, %w", l.Attrs().Name, err)
				}

				if err = netlink.LinkSetDown(l); err != nil {
					return fmt.Errorf("failed to set %q down: %v", l.Attrs().Name, err)
				}
				err = netlink.LinkSetName(l, name)
				if err != nil {
					logger.Errorf("set link rename name: %s to %s error: %s", l.Attrs().Name, name, err.Error())
					continue
				}
				if err = netlink.LinkSetUp(l); err != nil {
					return fmt.Errorf("failed to set %q up: %v", l.Attrs().Name, err)
				}

				logger.Infof("set link rename name: %s to %s", l.Attrs().Name, name)
				err = netlink.LinkSetNsFd(l, int(hostNetNS.Fd()))
				if err != nil {
					logger.Errorf("ip link set : %v host netns:%s error: %s", l.Attrs().Name, netNS.Path(), err.Error())
					continue
				}
				logger.Infof("ip link set : %v host netns:%s", l.Attrs().Name, netNS.Path())

				device2Address[name] = addrList
			}
		}
		return nil
	})
	if err != nil {
		logger.Errorf("failed to move device to host netns: %s", err.Error())
		return fmt.Errorf("failed to move device to host netns: %s", err.Error())
	}

	for key, val := range device2Address {
		l, err := netlink.LinkByName(key)
		if err != nil {
			logger.Errorf("failed to find link: %s, %s", key, err.Error())
			return fmt.Errorf("failed to find link: %s, %s", key, err.Error())
		}

		sysctlValueName := fmt.Sprintf(rpFilterSysctlTemplate, key)
		if _, err := sysctl.Sysctl(sysctlValueName, "0"); err != nil {
			logger.Errorf("failed to set rp_filter to 0 on dev:%s, error:%s", key, err.Error())
		}

		for _, addr := range val {
			address, err := netlink.ParseAddr(addr.IPNet.String())
			if err != nil {
				logger.Errorf("failed to parse addr in host ns %s, %w", addr.IPNet.String(), err)
				return fmt.Errorf("failed to parse addr in host ns %s, %w", addr.IPNet.String(), err)
			}
			err = netlink.AddrReplace(l, address)
			if err != nil {
				logger.Errorf("failed to add addr in host ns %s, %w", addr.IPNet.String(), err)
				return fmt.Errorf("failed to add addr in host ns %s, %w", addr.IPNet.String(), err)
			}
		}
		if err = netlink.LinkSetUp(l); err != nil {
			logger.Errorf("in host ns, failed to set %q up: %v", l.Attrs().Name, err)
			return fmt.Errorf("in host ns, failed to set %q up: %v", l.Attrs().Name, err)
		}

		// TODO:If there is a strategic route for different rdma devices, it needs to be added here.
		//		(It may be necessary to add policy routing for the RDMA network card.)
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
			if err != nil {
				return err
			}
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

func setUpHostVeth(addrs *[]netlink.Addr, netns ns.NetNS) error {
	if len(*addrs) < 1 {
		return errors.New("no address configured inside container")
	}

	addrBits := 32
	vethHost, err := getHostInterface(netns)
	if err != nil {
		return err
	}

	for _, addr := range *addrs {
		exclusiveRdmaIP := addr.IPNet.IP
		err = netlink.RouteAdd(&netlink.Route{
			LinkIndex: vethHost.Attrs().Index,
			Scope:     netlink.SCOPE_LINK,
			Dst: &net.IPNet{
				IP:   exclusiveRdmaIP,
				Mask: net.CIDRMask(addrBits, addrBits),
			},
		})

		if err != nil {
			return fmt.Errorf("failed to add host route dst %v: %v", exclusiveRdmaIP, err)
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
		if err != nil && !netlinkutils.IsNotExistError(err) {
			return err
		}
	}

	return nil
}

func randomDeviceName() (string, error) {
	entropy := make([]byte, 4)
	_, err := rand.Read(entropy)
	if err != nil {
		return "", fmt.Errorf("failed to generate random exclusive-rdma name: %v", err)
	}
	return fmt.Sprintf("%s%x", rdmaDeviceNamePrefix, entropy), nil
}
