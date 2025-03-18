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
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

// ipRelatedError represents an error related to IP validation
type ipRelatedError struct {
	IP     string // The IP address where the error occurred
	reason string // The error message
}

// Error implements the error interface for ipRelatedError
func (e *ipRelatedError) Error() string {
	return fmt.Sprintf("unavailable IP '%s' reason: %s", e.IP, e.reason)
}

// NewIPRelatedError creates and returns a new ipRelatedError instance
func NewIPRelatedError(ip, reason string) error {
	return &ipRelatedError{
		IP:     ip,
		reason: reason,
	}
}

func isIPRelatedError(err error) bool {
	var ipError *ipRelatedError
	return errors.As(err, &ipError)
}

type IPAM interface {
	ExecAdd(ipamType string, stdinData []byte) (types.Result, error)
	ExecDel(ipamType string, stdinData []byte) error
	ExecCheck(plugin string, netconf []byte) error
}

type DefaultIPAM struct{}

func (d *DefaultIPAM) ExecAdd(ipamType string, stdinData []byte) (types.Result, error) {
	return ipam.ExecAdd(ipamType, stdinData)
}

func (d *DefaultIPAM) ExecDel(ipamType string, stdinData []byte) error {
	return ipam.ExecDel(ipamType, stdinData)
}

func (d *DefaultIPAM) ExecCheck(plugin string, netconf []byte) error {
	return ipam.ExecCheck(plugin, netconf)
}

var _ IPAM = &DefaultIPAM{}

// NetworkConfigurer interface defines methods for network configuration
type NetworkConfigurer interface {
	SetupContainerVeth(podname string, namespace string, netns ns.NetNS, ifName string, mtu int, pr *current.Result) (*current.Interface, *current.Interface, error)
	SetupHostVeth(host *current.Interface, container *current.Interface, result *current.Result) error
	RollbackVeth(netns ns.NetNS, hostInterface, containerInterface *current.Interface) error
}

// DefaultNetworkConfigurer implements the NetworkConfigurer interface
type DefaultNetworkConfigurer struct{}

func (d *DefaultNetworkConfigurer) SetupContainerVeth(podname string, namespace string, netns ns.NetNS, ifName string, mtu int, pr *current.Result) (*current.Interface, *current.Interface, error) {
	return setupContainerVethLegacy(podname, namespace, netns, ifName, mtu, pr)
}

func (d *DefaultNetworkConfigurer) SetupHostVeth(host *current.Interface, container *current.Interface, result *current.Result) error {
	return setupHostVeth(host, container, result)
}

func (d *DefaultNetworkConfigurer) RollbackVeth(netns ns.NetNS, hostInterface, containerInterface *current.Interface) error {
	return rollbackVeth(netns, hostInterface, containerInterface)
}

var _ NetworkConfigurer = &DefaultNetworkConfigurer{}

type IPMasqManager interface {
	SetupIPMasq(ipn *net.IPNet, chain, comment string) error
	TeardownIPMasq(ipn *net.IPNet, chain, comment string) error
}

type DefaultIPMasqManager struct{}

func (d *DefaultIPMasqManager) SetupIPMasq(ipn *net.IPNet, chain, comment string) error {
	return ip.SetupIPMasq(ipn, chain, comment)
}

func (d *DefaultIPMasqManager) TeardownIPMasq(ipn *net.IPNet, chain, comment string) error {
	return ip.TeardownIPMasq(ipn, chain, comment)
}

var _ IPMasqManager = &DefaultIPMasqManager{}

var logger *logrus.Entry

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

type NetConf struct {
	types.NetConf
	IPMasq                    bool   `json:"ipMasq"`
	MTU                       int    `json:"mtu"`
	DefaultGW                 string `json:"defaultGW,omitempty"`
	PluginIpRequestRetryTimes int    `json:"retryTimes,omitempty"`
}

func setupContainerVethLegacy(podname string, namespace string, netns ns.NetNS, ifName string, mtu int, pr *current.Result) (*current.Interface, *current.Interface, error) {
	// The IPAM result will be something like IP=192.168.3.5/24, GW=192.168.3.1.
	// What we want is really a point-to-point link but veth does not support IFF_POINTTOPOINT.
	// Next best thing would be to let it ARP but set interface to 192.168.3.5/32 and
	// add a route like "192.168.3.0/24 via 192.168.3.1 dev $ifName".
	// Unfortunately that won't work as the GW will be outside the interface's subnet.

	// Our solution is to configure the interface with 192.168.3.5/24, then delete the
	// "192.168.3.0/24 dev $ifName" route that was automatically added. Then we add
	// "192.168.3.1/32 dev $ifName" and "192.168.3.0/24 via 192.168.3.1 dev $ifName".
	// In other words we force all traffic to ARP via the gateway except for GW itself.

	hostInterface := &current.Interface{}
	containerInterface := &current.Interface{}

	err := netns.Do(func(hostNS ns.NetNS) error {
		var hostInterfaceNameTmp string

		hostInterfaceNameTmp = vethNameForPod(podname, namespace, "veth")
		hostVeth, contVeth0, err := ip.SetupVethWithName(ifName, hostInterfaceNameTmp, mtu, "", hostNS)
		if err != nil {
			return err
		}
		hostInterface.Name = hostVeth.Name
		hostInterface.Mac = hostVeth.HardwareAddr.String()
		containerInterface.Name = contVeth0.Name
		containerInterface.Mac = contVeth0.HardwareAddr.String()
		containerInterface.Sandbox = netns.Path()

		for _, ipc := range pr.IPs {
			// All addresses apply to the container veth interface
			ipc.Interface = current.Int(1)
		}

		pr.Interfaces = []*current.Interface{hostInterface, containerInterface}
		if err = CPTPConfigureIface(ifName, pr); err != nil {
			return fmt.Errorf("failed to configure interface %q with ipam result %+v: %v", ifName, pr, err)
		}

		err = containerSet(&hostVeth, &contVeth0, pr)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return hostInterface, containerInterface, nil
}

// rollbackVeth encapsulates the logic to roll back veth devices
func rollbackVeth(netns ns.NetNS, hostInterface, containerInterface *current.Interface) error {
	var errs []error

	if containerInterface != nil {
		err := netns.Do(func(_ ns.NetNS) error {
			_, err := ip.DelLinkByNameAddr(containerInterface.Name)
			if err != nil && err == ip.ErrLinkNotFound {
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to delete container veth %s: %w", containerInterface.Name, err)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}

	if hostInterface != nil {
		_, err := ip.DelLinkByNameAddr(hostInterface.Name)
		if err != nil && err == ip.ErrLinkNotFound {
		} else if err != nil {
			errs = append(errs, fmt.Errorf("failed to delete host veth %s: %w", hostInterface.Name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("rollback errors: %v", errs)
	}
	return nil
}

func setupHostVeth(host *current.Interface, container *current.Interface, result *current.Result) error {
	// hostVeth moved namespaces and may have a new ifindex
	vethName := host.Name
	veth, err := netlink.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", vethName, err)
	}

	for _, ipc := range result.IPs {
		maskLen := 128
		if ipc.Address.IP.To4() != nil {
			maskLen = 32
		}

		ipn := &net.IPNet{
			IP:   ipc.Address.IP,
			Mask: net.CIDRMask(maskLen, maskLen),
		}
		// dst happens to be the same as IP/net of link veth
		// the host access container can retain the source IP of the host that
		// use link scope instead of host scope
		if err = AddLinkRoute(ipn, nil, veth); err != nil {
			if os.IsExist(err) {
				isRouteExist := false
				routeExist, errGetRoute := netlink.RouteGet(ipn.IP)
				if errGetRoute == nil {
					for _, route := range routeExist {
						// Optimize the judgment conditions for the conflict of the destination
						// address of the routing rules of the veth device, and solve the problem
						// that the network of the newly created pod container is blocked due to
						// the different destination veth devices of the routing rules when the
						// nodes have routing rules of the same destination ip.
						if route.LinkIndex == veth.Attrs().Index {
							isRouteExist = true
							break
						}
					}
				}
				if isRouteExist {
					goto nextStep
				}
			}
			return NewIPRelatedError(ipc.Address.IP.String(), err.Error())
		}

	nextStep:
		hw, err := net.ParseMAC(container.Mac)
		if err == nil {
			// add permanent ARP entry for the gateway on the host veth
			netlink.NeighAdd(&netlink.Neigh{
				LinkIndex: veth.Attrs().Index,
				State:     netlink.NUD_PERMANENT,
				IP:        ipc.Address.IP,
				HardwareAddr: func() net.HardwareAddr {
					return hw
				}(),
			})
		}
	}

	return nil
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	var pK8Sargs *K8SArgs

	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "cptp",
		"mod":     "ADD",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()

	pK8Sargs, err = loadK8SArgs(args.Args)
	if err != nil {
		return fmt.Errorf("failed to load CNI_ARGS: %v", err)
	}

	conf := NetConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	var result *current.Result

	var IpRetryErrorList []error

	defer func() {
		IpRetryErrList := mergeErrors(IpRetryErrorList)
		if len(IpRetryErrorList) > 0 {
			logger.WithError(IpRetryErrList).Error("IP related retry errors")
		}
		if err != nil {
			err = fmt.Errorf("%w; IP related retry errors: %v", err, IpRetryErrList)
		}
	}()

	for i := 0; i <= conf.PluginIpRequestRetryTimes; i++ {
		result, err = configureNetworkWithIPAM(&conf, pK8Sargs, args, netns, &DefaultNetworkConfigurer{}, &DefalutNetworkChecker{}, &DefaultIPAM{}, &DefaultIPMasqManager{})
		if err != nil {
			if isIPRelatedError(err) {
				IpRetryErrorList = append(IpRetryErrorList, fmt.Errorf("retry %d failed: %v", i, err))
				continue
			}
			return err
		}
		break
	}

	if err != nil {
		return fmt.Errorf("failed to set IP Related conf after %d try", conf.PluginIpRequestRetryTimes)
	}

	if result == nil {
		return fmt.Errorf("empty result get from IPAM")
	}

	logger.WithField("result", logfields.Json(result)).Infof("success to exec plugin")
	return types.PrintResult(result, conf.CNIVersion)
}

func dnsConfSet(dnsConf types.DNS) bool {
	return dnsConf.Nameservers != nil ||
		dnsConf.Search != nil ||
		dnsConf.Options != nil ||
		dnsConf.Domain != ""
}

func cmdDel(args *skel.CmdArgs) (err error) {
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "cptp",
		"mod":     "DEL",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Error("failed to exec plugin")
		} else {
			logger.Info("successfully to exec plugin")
		}
	}()

	conf := NetConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	if err := ipam.ExecDel(conf.IPAM.Type, args.StdinData); err != nil {
		return fmt.Errorf("failed to exec ipam del: %v", err)
	}
	logger.Info("success to executing cipam DEL")

	if args.Netns == "" {
		return nil
	}

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	// If the device isn't there then don't try to clean up IP masq either.
	var ipnets []*net.IPNet
	err = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		var err error
		ipnets, err = ip.DelLinkByNameAddr(args.IfName)
		if err != nil && err == ip.ErrLinkNotFound {
			return nil
		}
		return err
	})
	if err != nil {
		//  if NetNs is passed down by the Cloud Orchestration Engine, or if it called multiple times
		// so don't return an error if the device is already removed.
		// https://github.com/kubernetes/kubernetes/issues/43014#issuecomment-287164444
		_, ok := err.(ns.NSPathNotExistErr)
		if ok {
			return nil
		}
		return fmt.Errorf("failed to clean container interface: %v", err)
	}

	if len(ipnets) != 0 && conf.IPMasq {
		chain := utils.FormatChainName(conf.Name, args.ContainerID)
		comment := utils.FormatComment(conf.Name, args.ContainerID)
		for _, ipn := range ipnets {
			err = ip.TeardownIPMasq(ipn, chain, comment)
			if err != nil {
				logger.WithError(err).Error("failed to teardown ip masquerade")
			}
		}
	}

	return err
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("ptp"))
}

func cmdCheck(args *skel.CmdArgs) error {
	conf := NetConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	// run the IPAM plugin and get back the config to apply
	err = ipam.ExecCheck(conf.IPAM.Type, args.StdinData)
	if err != nil {
		return err
	}
	if conf.NetConf.RawPrevResult == nil {
		return fmt.Errorf("ptp: Required prevResult missing")
	}
	if err := version.ParsePrevResult(&conf.NetConf); err != nil {
		return err
	}
	// Convert whatever the IPAM result was into the current Result type
	result, err := current.NewResultFromResult(conf.PrevResult)
	if err != nil {
		return err
	}

	var contMap current.Interface
	// Find interfaces for name whe know, that of host-device inside container
	for _, intf := range result.Interfaces {
		if args.IfName == intf.Name {
			if args.Netns == intf.Sandbox {
				contMap = *intf
				continue
			}
		}
	}

	// The namespace must be the same as what was configured
	if args.Netns != contMap.Sandbox {
		return fmt.Errorf("Sandbox in prevResult %s doesn't match configured netns: %s",
			contMap.Sandbox, args.Netns)
	}

	//
	// Check prevResults for ips, routes and dns against values found in the container
	if err := netns.Do(func(_ ns.NetNS) error {
		// Check interface against values found in the container
		err := validateCniContainerInterface(contMap)
		if err != nil {
			return err
		}

		err = ip.ValidateExpectedInterfaceIPs(args.IfName, result.IPs)
		if err != nil {
			return err
		}

		err = ip.ValidateExpectedRoute(result.Routes)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func validateCniContainerInterface(intf current.Interface) error {
	var link netlink.Link
	var err error

	if intf.Name == "" {
		return fmt.Errorf("Container interface name missing in prevResult: %v", intf.Name)
	}
	link, err = netlink.LinkByName(intf.Name)
	if err != nil {
		return fmt.Errorf("ptp: Container Interface name in prevResult: %s not found", intf.Name)
	}
	if intf.Sandbox == "" {
		return fmt.Errorf("ptp: Error: Container interface %s should not be in host namespace", link.Attrs().Name)
	}

	_, isVeth := link.(*netlink.Veth)
	if !isVeth {
		return fmt.Errorf("Error: Container interface %s not of type veth/p2p", link.Attrs().Name)
	}

	if intf.Mac != "" {
		if intf.Mac != link.Attrs().HardwareAddr.String() {
			return fmt.Errorf("ptp: Interface %s Mac %s doesn't match container Mac: %s", intf.Name, intf.Mac, link.Attrs().HardwareAddr)
		}
	}

	return nil
}

func configureNetworkWithIPAM(conf *NetConf, pK8Sargs *K8SArgs, args *skel.CmdArgs, netns ns.NetNS, networkConfigurer NetworkConfigurer, networkChecker NetworkChecker, ipam IPAM, ipMasqManager IPMasqManager) (*current.Result, error) {
	// Execute the IPAM plugin to allocate IP resources
	r, err := ipam.ExecAdd(conf.IPAM.Type, args.StdinData)
	if err != nil {
		logger.WithError(err).Error("failed to exec IPAM plugin")
		return nil, fmt.Errorf("failed to execute IPAM plugin: %w", err)
	}

	// Ensure IP resources are released in case of an error
	defer func() {
		if err != nil {
			if delErr := ipam.ExecDel(conf.IPAM.Type, args.StdinData); delErr != nil {
				// If the rollback fails, clean original err type, append the rollback error to the main error.
				err = fmt.Errorf("%v; rollback failed: %v", err, delErr)
				logger.WithError(delErr).Error("failed to release IPAM resources")
			}
		}
	}()

	// Convert the IPAM result into the current Result type
	result, err := current.NewResultFromResult(r)
	if err != nil {
		return nil, fmt.Errorf("could not convert result of IPAM plugin: %v", err)
	}
	logger.WithField("ipamResult", result).Infof("got result from IPAM")

	// Validate that the IPAM plugin returned at least one IP configuration
	if len(result.IPs) == 0 {
		return nil, errors.New("IPAM plugin returned missing IP config")
	}

	// Enable IP forwarding for the allocated IPs
	if err := ip.EnableForward(result.IPs); err != nil {
		return nil, fmt.Errorf("could not enable IP forwarding: %v", err)
	}

	// Set the default gateway if specified in the configuration
	if conf.DefaultGW != "" {
		for i := range result.IPs {
			if result.IPs[i].Gateway == nil {
				result.IPs[i].Gateway = net.ParseIP(conf.DefaultGW)
			}
		}
	}

	// Set up the veth pair between the host and the container
	hostInterface, containerInterface, err := networkConfigurer.SetupContainerVeth(string(pK8Sargs.K8S_POD_NAME), string(pK8Sargs.K8S_POD_NAMESPACE), netns, args.IfName, conf.MTU, result)
	if err != nil {
		return nil, fmt.Errorf("failed to setup container veth: %w", err)
	}

	defer func() {
		if err != nil {
			rollbackErr := networkConfigurer.RollbackVeth(netns, hostInterface, containerInterface)
			if rollbackErr != nil {
				// If the rollback fails, clean original err type, append the rollback error to the main error.
				err = fmt.Errorf("%v; rollback failed: %v", err, rollbackErr)
			}
		}
	}()

	// Configure the host-side veth interface
	if err = networkConfigurer.SetupHostVeth(hostInterface, containerInterface, result); err != nil {
		return nil, fmt.Errorf("failed to setup host veth: %w", err)
	}

	// Configure IP masquerading if enabled in the configuration
	if conf.IPMasq {
		chain := utils.FormatChainName(conf.Name, args.ContainerID)
		comment := utils.FormatComment(conf.Name, args.ContainerID)
		for _, ipc := range result.IPs {
			if err = ipMasqManager.SetupIPMasq(&ipc.Address, chain, comment); err != nil {
				return nil, fmt.Errorf("failed to setup IP masquerade: %w", err)
			}
		}
	}
	defer func() {
		if len(result.IPs) != 0 && conf.IPMasq {
			chain := utils.FormatChainName(conf.Name, args.ContainerID)
			comment := utils.FormatComment(conf.Name, args.ContainerID)
			for _, ipc := range result.IPs {
				err = ipMasqManager.TeardownIPMasq(&ipc.Address, chain, comment)
				if err != nil {
					err = fmt.Errorf("failed to roll back IP masquerade: %w", err)
				}
			}
		}
	}()

	// Only override the DNS settings in the previous result if any DNS fields
	// were provided to the ptp plugin. This allows, for example, IPAM plugins
	// to specify the DNS settings instead of the ptp plugin.
	if dnsConfSet(conf.DNS) {
		result.DNS = conf.DNS
	}

	// After the container network is configured, verify the reliability of the
	// IP provided by the IaasS by testing the connectivity between the container
	// and the gateway.
	err = VerifyNetworkConnectivity(result, netns, conf.MTU, networkChecker)
	if err != nil {
		logger.WithError(err).Error("Failed to verify network connectivity")
		return nil, err
	}

	// Return the IPAM result for further use
	return result, nil
}

// VethNameForPod return host-side veth name for pod
// max veth length is 15
func vethNameForPod(name, namespace, prefix string) string {
	// A SHA1 is always 20 bytes long, and so is sufficient for generating the
	// veth name and mac addr.
	h := sha1.New()
	h.Write([]byte(namespace + "." + name))
	return fmt.Sprintf("%s%s", prefix, hex.EncodeToString(h.Sum(nil))[:11])
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

func mergeErrors(errors []error) error {
	var nonNilErrors []string
	for _, err := range errors {
		if err != nil {
			nonNilErrors = append(nonNilErrors, err.Error())
		}
	}
	if len(nonNilErrors) == 0 {
		return nil
	}
	return fmt.Errorf(strings.Join(nonNilErrors, "; "))
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
