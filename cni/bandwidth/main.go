package main

import (
	"encoding/json"
	"flag"
	"fmt"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	"math"

	"github.com/vishvananda/netlink"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

const (
	logFile = "/var/log/cce/cni-bandwidth.log"
)

const maxIfbDeviceLength = 15
const ifbDevicePrefix = "bwp"

// BandwidthEntry corresponds to a single entry in the bandwidth argument,
// see CONVENTIONS.md
type BandwidthEntry struct {
	IngressRate  uint64 `json:"ingressRate"`  //Bandwidth rate in bps for traffic through container. 0 for no limit. If ingressRate is set, ingressBurst must also be set
	IngressBurst uint64 `json:"ingressBurst"` //Bandwidth burst in bits for traffic through container. 0 for no limit. If ingressBurst is set, ingressRate must also be set

	EgressRate  uint64 `json:"egressRate"`  //Bandwidth rate in bps for traffic through container. 0 for no limit. If egressRate is set, egressBurst must also be set
	EgressBurst uint64 `json:"egressBurst"` //Bandwidth burst in bits for traffic through container. 0 for no limit. If egressBurst is set, egressRate must also be set
}

func (bw *BandwidthEntry) isZero() bool {
	return bw.IngressBurst == 0 && bw.IngressRate == 0 && bw.EgressBurst == 0 && bw.EgressRate == 0
}

type PluginConf struct {
	types.NetConf

	RuntimeConfig struct {
		Bandwidth *BandwidthEntry `json:"bandwidth,omitempty"`
	} `json:"runtimeConfig,omitempty"`

	*BandwidthEntry
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	bandwidth := getBandwidth(&conf)
	if bandwidth != nil {
		err := validateRateAndBurst(bandwidth.IngressRate, bandwidth.IngressBurst)
		if err != nil {
			return nil, err
		}
		err = validateRateAndBurst(bandwidth.EgressRate, bandwidth.EgressBurst)
		if err != nil {
			return nil, err
		}
	}

	if conf.RawPrevResult != nil {
		var err error
		if err = version.ParsePrevResult(&conf.NetConf); err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}

		_, err = current.NewResultFromResult(conf.PrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}

	return &conf, nil

}

func getBandwidth(conf *PluginConf) *BandwidthEntry {
	if conf.BandwidthEntry == nil && conf.RuntimeConfig.Bandwidth != nil {
		return conf.RuntimeConfig.Bandwidth
	}
	return conf.BandwidthEntry
}

func validateRateAndBurst(rate, burst uint64) error {
	switch {
	case burst < 0 || rate < 0:
		return fmt.Errorf("rate and burst must be a positive integer")
	case burst == 0 && rate != 0:
		return fmt.Errorf("if rate is set, burst must also be set")
	case rate == 0 && burst != 0:
		return fmt.Errorf("if burst is set, rate must also be set")
	case burst/8 >= math.MaxUint32:
		return fmt.Errorf("burst cannot be more than 4GB")
	}

	return nil
}

func getIfbDeviceName(networkName string, containerId string) string {
	return utils.MustFormatHashWithPrefix(maxIfbDeviceLength, ifbDevicePrefix, networkName+containerId)
}

func getMTU(deviceName string) (int, error) {
	link, err := netlink.LinkByName(deviceName)
	if err != nil {
		return -1, err
	}

	return link.Attrs().MTU, nil
}

// get the veth peer of container interface in host namespace
func getHostInterface(interfaces []*current.Interface, containerIfName string, netns ns.NetNS) (*current.Interface, error) {
	if len(interfaces) == 0 {
		return nil, fmt.Errorf("no interfaces provided")
	}

	// get veth peer index of container interface
	var peerIndex int
	var err error
	_ = netns.Do(func(_ ns.NetNS) error {
		_, peerIndex, err = ip.GetVethPeerIfindex(containerIfName)
		return nil
	})
	if peerIndex <= 0 {
		return nil, fmt.Errorf("container interface %s has no veth peer: %v", containerIfName, err)
	}

	// find host interface by index
	link, err := netlink.LinkByIndex(peerIndex)
	if err != nil {
		return nil, fmt.Errorf("veth peer with index %d is not in host ns", peerIndex)
	}
	for _, iface := range interfaces {
		if iface.Sandbox == "" && iface.Name == link.Attrs().Name {
			return iface, nil
		}
	}

	return nil, fmt.Errorf("no veth peer of container interface found in host ns")
}

func cmdAdd(args *skel.CmdArgs) error {
	ctx := log.NewContext()

	log.Infof(ctx, "====> CmdAdd Begins <====")
	defer log.Infof(ctx, "====> CmdAdd Ends <====")
	log.Infof(ctx, "[cmdAdd]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdAdd]: stdinData: %v", string(args.StdinData))

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}

	bandwidth := getBandwidth(conf)
	if bandwidth == nil || bandwidth.isZero() {
		return types.PrintResult(conf.PrevResult, conf.CNIVersion)
	}

	if conf.PrevResult == nil {
		return fmt.Errorf("must be called as chained plugin")
	}

	result, err := current.NewResultFromResult(conf.PrevResult)
	if err != nil {
		return fmt.Errorf("could not convert result to current version: %v", err)
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	defer netns.Close()

	hostInterface, err := getHostInterface(result.Interfaces, args.IfName, netns)
	if err != nil {
		return err
	}

	if bandwidth.EgressBurst == math.MaxInt32 {
		bandwidth.EgressBurst = bandwidth.EgressRate
	}

	if bandwidth.IngressBurst == math.MaxInt32 {
		bandwidth.IngressBurst = bandwidth.IngressRate
	}

	log.Infof(ctx, "bandwidth: %v", log.ToJson(bandwidth))

	if bandwidth.IngressRate > 0 && bandwidth.IngressBurst > 0 {
		err = CreateIngressQdisc(bandwidth.IngressRate, bandwidth.IngressBurst, hostInterface.Name)
		if err != nil {
			return err
		}
	}

	if bandwidth.EgressRate > 0 && bandwidth.EgressBurst > 0 {
		mtu, err := getMTU(hostInterface.Name)
		if err != nil {
			return err
		}

		ifbDeviceName := getIfbDeviceName(conf.Name, args.ContainerID)

		err = CreateIfb(ifbDeviceName, mtu)
		if err != nil {
			return err
		}

		ifbDevice, err := netlink.LinkByName(ifbDeviceName)
		if err != nil {
			return err
		}

		result.Interfaces = append(result.Interfaces, &current.Interface{
			Name: ifbDeviceName,
			Mac:  ifbDevice.Attrs().HardwareAddr.String(),
		})
		err = CreateEgressQdisc(bandwidth.EgressRate, bandwidth.EgressBurst, hostInterface.Name, ifbDeviceName)
		if err != nil {
			return err
		}
	}

	return types.PrintResult(result, conf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	ctx := log.NewContext()

	log.Infof(ctx, "====> CmdDel Begins <====")
	defer log.Infof(ctx, "====> CmdDel Ends <====")
	log.Infof(ctx, "[cmdDel]: containerID: %v, netns: %v, ifName: %v, args: %v, path: %v",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path)
	log.Infof(ctx, "[cmdDel]: stdinData: %v", string(args.StdinData))

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}

	ifbDeviceName := getIfbDeviceName(conf.Name, args.ContainerID)

	if err := TeardownIfb(ifbDeviceName); err != nil {
		return err
	}

	return nil
}

func main() {
	initFlags()
	defer log.Flush()

	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.PluginSupports("0.3.0", "0.3.1", version.Current()), bv.BuildString("bandwidth"))
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
