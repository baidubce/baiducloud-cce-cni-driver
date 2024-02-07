// Copyright 2017 CNI authors
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

// This is the Source Based Routing plugin that sets up source based routing.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/alexflint/go-filemutex"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/client/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/client"
	plugintypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/hooks"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const firstTableID = 100

var logger *logrus.Entry

// PluginConf is the configuration document passed in.
type PluginConf struct {
	types.NetConf

	// This is the previous result, when called in the context of a chained
	// plugin. Because this plugin supports multiple versions, we'll have to
	// parse this in two passes. If your plugin is not chained, this can be
	// removed (though you may wish to error if a non-chainable plugin is
	// chained).
	RawPrevResult *map[string]interface{} `json:"prevResult"`
	PrevResult    *current.Result         `json:"-"`

	// Add plugin-specific flags here
	EnableDebug bool   `json:"enable-debug"`
	LogFormat   string `json:"log-format"`
	LogFile     string `json:"log-file"`
}

// EIPRoute represents a netlink route.
type EIPRoute struct {
	Scope string `json:"scope,omitempty"`
	Dst   string `json:"dst,omitempty"`
	Src   string `json:"src,omitempty"`
	Gw    string `json:"gw,omitempty"`
	Via   string `json:"via,omitempty"`
}

// Wrapper that does a lock before and unlock after operations to serialise
// this plugin.
func withLockAndNetNS(nspath string, toRun func(_ ns.NetNS) error) error {
	// We lock on the network namespace to ensure that no other instance
	// clashes with this one.
	log.Printf("Network namespace to use and lock: %s", nspath)
	lock, err := filemutex.New(nspath)
	if err != nil {
		return err
	}

	err = lock.Lock()
	if err != nil {
		return err
	}

	err = ns.WithNetNSPath(nspath, toRun)

	if err != nil {
		return err
	}

	// Cleaner to unlock even though about to exit
	err = lock.Unlock()

	return err
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// Parse previous result.
	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %v", err)
		}
		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}
	// End previous result parsing

	return &conf, nil
}

func setupLogging(n *PluginConf, args *skel.CmdArgs, method string) error {
	f := n.LogFormat
	if f == "" {
		f = string(logging.DefaultLogFormat)
	}
	logOptions := logging.LogOptions{
		logging.FormatOpt: f,
	}
	if len(n.LogFile) != 0 {
		err := logging.SetupLogging([]string{}, logOptions, "sbr-eip", n.EnableDebug)
		if err != nil {
			return err
		}
		logging.AddHooks(hooks.NewFileRotationLogHook(n.LogFile,
			hooks.EnableCompression(),
			hooks.WithMaxBackups(1),
		))
	} else {
		logOptions["syslog.facility"] = "local5"
		err := logging.SetupLogging([]string{"syslog"}, logOptions, "sbr-eip", true)
		if err != nil {
			return err
		}
	}
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"containerID": args.ContainerID,
		"netns":       args.Netns,
		"plugin":      "sbr-eip",
		"method":      method,
	})

	return nil
}

// getIPCfgs finds the IPs on the supplied interface, returning as IPConfig structures
func getIPCfgs(iface string, prevResult *current.Result) ([]*current.IPConfig, error) {
	if len(prevResult.IPs) == 0 {
		// No IP addresses; that makes no sense. Pack it in.
		return nil, fmt.Errorf("no IP addresses supplied on interface: %s", iface)
	}

	// We do a single interface name, stored in args.IfName
	logger.Debugf("Checking for relevant interface: %s", iface)

	// ips contains the IPConfig structures that were passed, filtered somewhat
	ipCfgs := make([]*current.IPConfig, 0, len(prevResult.IPs))

	for _, ipCfg := range prevResult.IPs {
		// IPs have an interface that is an index into the interfaces array.
		// We assume a match if this index is missing.
		if ipCfg.Interface == nil {
			logger.Debugf("No interface for IP address %s", ipCfg.Address.IP)
			ipCfgs = append(ipCfgs, ipCfg)
			continue
		}
		logger.Infof("Found IP address %s", ipCfg.Address.IP.String())
		ipCfgs = append(ipCfgs, ipCfg)
	}

	return ipCfgs, nil
}

// cmdAdd is called for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return fmt.Errorf("sbnr-eip failed to parse config: %v", err)
	}
	err = setupLogging(conf, args, "ADD")
	if err != nil {
		return fmt.Errorf("sbr-eip failed to set up logging: %v", err)
	}
	defer func() {
		if err != nil {
			logger.Errorf("cni plugin failed: %v", err)
		}
	}()
	logger.Debugf("configure SBR for new interface %s - previous result: %v",
		args.IfName, conf.PrevResult)

	if conf.PrevResult == nil {
		return fmt.Errorf("this plugin must be called as chained plugin")
	}

	// Get the list of relevant IPs.
	ipCfgs, err := getIPCfgs(args.IfName, conf.PrevResult)
	if err != nil {
		return err
	}

	eipConfig, err := publicIPStatus(args)
	if err != nil {
		return fmt.Errorf("failed to exec sbr-ext: %v", err)
	}
	if eipConfig == nil {
		logger.Debugf("no public IP found, skipping")
		goto out
	}

	for _, ipCfg := range ipCfgs {
		if ipCfg.Gateway != nil && ipCfg.Gateway.To4() != nil {
			eipConfig.Gateway = ipCfg.Gateway
			break
		}
	}

	// Do the actual work.
	err = withLockAndNetNS(args.Netns, func(_ ns.NetNS) error {
		return doRoutes(eipConfig, args.IfName)
	})
	if err != nil {
		return err
	}

out:
	// Pass through the result for the next plugin
	return types.PrintResult(conf.PrevResult, conf.CNIVersion)
}

func publicIPStatus(args *skel.CmdArgs) (*current.IPConfig, error) {
	cniArgs := plugintypes.ArgsSpec{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return nil, fmt.Errorf("unable to extract CNI arguments: %s", err)
	}
	var (
		extPluginData map[string]map[string]string
		publicIPData  map[string]string
		ok            bool
		err           error
		owner         = string(cniArgs.K8S_POD_NAMESPACE + "/" + cniArgs.K8S_POD_NAME)
		containerID   = args.ContainerID
	)

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		err = fmt.Errorf("unable to connect to cce-network-v2-agent: %s", client.Hint(err))
		return nil, err
	}
	param := endpoint.NewGetEndpointExtpluginStatusParams().WithOwner(&owner).WithContainerID(&containerID)
	result, err := c.Endpoint.GetEndpointExtpluginStatus(param)
	if err != nil {
		err = fmt.Errorf("unable to get endpoint extplugin status: %s", client.Hint(err))
		return nil, err
	}
	extPluginData = result.Payload
	if extPluginData == nil {
		return nil, nil
	}

	if publicIPData, ok = extPluginData["publicIP"]; !ok {
		return nil, nil
	}
	logger.WithField("publicIPData", logfields.Repr(publicIPData)).Infof("found public IP data")
	eipstr := publicIPData["eip"]

	mode := publicIPData["mode"]

	if mode == "direct" {
		eip := net.ParseIP(eipstr)
		if eip == nil {
			return nil, fmt.Errorf("failed to parse external plugin EIP: %s", eipstr)
		}
		return &current.IPConfig{Address: net.IPNet{IP: eip, Mask: net.CIDRMask(32, 32)}}, nil
	}
	return nil, nil
}

// getNextTableID picks the first free table id from a giveen candidate id
func getNextTableID(rules []netlink.Rule, routes []netlink.Route, candidateID int) int {
	table := candidateID
	for {
		foundExisting := false
		for _, rule := range rules {
			if rule.Table == table {
				foundExisting = true
				break
			}
		}

		for _, route := range routes {
			if route.Table == table {
				foundExisting = true
				break
			}
		}

		if foundExisting {
			table++
		} else {
			break
		}
	}
	return table
}

// doRoutes does all the work to set up routes and rules during an add.
func doRoutes(ipCfg *current.IPConfig, iface string) error {
	// Get a list of rules and routes ready.
	rules, err := netlink.RuleList(netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list all rules: %w", err)
	}

	routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list all routes: %w", err)
	}

	// Pick a table ID to use. We pick the first table ID from firstTableID
	// on that has no existing rules mapping to it and no existing routes in
	// it.
	table := getNextTableID(rules, routes, firstTableID)
	logger.Debugf("first unreferenced table: %d", table)

	link, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("cannot find network interface %s: %w", iface, err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("cannot list addresses for interface %s: %w", iface, err)
	}
	for _, addr := range addrs {
		if addr.IP.Equal(ipCfg.Address.IP) {
			// This is the address we're interested in.
			return nil
		}
	}
	err = netlink.AddrAdd(link, &netlink.Addr{IPNet: &ipCfg.Address, Label: fmt.Sprintf("%s: %s", iface, "eip")})
	if err != nil {
		return fmt.Errorf("cannot add address %s to interface %s: %w", ipCfg.Address, iface, err)
	}

	linkIndex := link.Attrs().Index

	// Get all routes for the interface in the default routing table
	routes, err = netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("unable to list routes: %w", err)
	}

	// // Loop through setting up source based rules and default routes.
	logger.Infof("set rule for source %s", ipCfg.String())
	rule := netlink.NewRule()
	rule.Table = table

	// Source must be restricted to a single IP, not a full subnet
	var src net.IPNet
	src.IP = ipCfg.Address.IP
	if src.IP.To4() != nil {
		src.Mask = net.CIDRMask(32, 32)
	} else {
		src.Mask = net.CIDRMask(128, 128)
	}

	logger.Infof("source to use %s", src.String())
	rule.Src = &src

	if err = netlink.RuleAdd(rule); err != nil {
		return fmt.Errorf("failed to add rule: %w", err)
	}

	// Add a default route, since this may have been removed by previous
	// plugin.
	if ipCfg.Gateway != nil {
		linkRoute := netlink.Route{
			Dst: &net.IPNet{
				IP:   ipCfg.Gateway,
				Mask: net.CIDRMask(32, 32),
			},
			Table:     table,
			LinkIndex: linkIndex,
			Scope:     netlink.SCOPE_LINK,
		}
		err = netlink.RouteAdd(&linkRoute)
		if err != nil {
			return fmt.Errorf("failed to add link route : %w", err)
		}
		logger.Infof("Adding default route to gateway %s", ipCfg.Gateway.String())

		var dest net.IPNet
		if ipCfg.Address.IP.To4() != nil {
			dest.IP = net.IPv4zero
			dest.Mask = net.CIDRMask(0, 32)
		} else {
			dest.IP = net.IPv6zero
			dest.Mask = net.CIDRMask(0, 128)
		}

		route := netlink.Route{
			Dst:       &dest,
			Gw:        ipCfg.Gateway,
			Table:     table,
			LinkIndex: linkIndex,
		}

		err = netlink.RouteAdd(&route)
		if err != nil {
			return fmt.Errorf("failed to add default route to %s: %v",
				ipCfg.Gateway.String(),
				err)
		}
	}

	// Copy the previously added routes for the interface to the correct
	// table; all the routes have been added to the interface anyway but
	// in the wrong table, so instead of removing them we just move them
	// to the table we want them in.
	for _, r := range routes {
		if ipCfg.Address.Contains(r.Src) {
			// (r.Src == nil && r.Gw == nil) is inferred as a generic route
			log.Printf("Copying route %s from table %d to %d",
				r.String(), r.Table, table)

			r.Table = table

			// Reset the route flags since if it is dynamically created,
			// adding it to the new table will fail with "invalid argument"
			r.Flags = 0

			// We use route replace in case the route already exists, which
			// is possible for the default gateway we added above.
			err = netlink.RouteReplace(&r)
			if err != nil {
				return fmt.Errorf("failed to readd route: %w", err)
			}
		}
	}

	return nil
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {
	// We care a bit about config because it sets log level.
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	err = setupLogging(conf, args, "DEL")
	if err != nil {
		return fmt.Errorf("sbr-eip failed to set up logging: %v", err)
	}
	defer func() {
		if err != nil {
			logger.Errorf("cni plugin failed: %v", err)
		}
	}()

	logger.Infof("Cleaning up SBR for %s", args.IfName)
	err = withLockAndNetNS(args.Netns, func(_ ns.NetNS) error {
		return tidyRules(args.IfName)
	})

	return err
}

// Tidy up the rules for the deleted interface
func tidyRules(iface string) error {
	// We keep on going on rule deletion error, but return the last failure.
	var errReturn error

	rules, err := netlink.RuleList(netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list all rules to tidy: %v", err)
	}

	link, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("failed to get link %s: %v", iface, err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list all addrs: %v", err)
	}

RULE_LOOP:
	for _, rule := range rules {
		logger.Debugf("Check rule: %v", rule)
		if rule.Src == nil {
			continue
		}

		if len(addrs) < 2 {
			return nil
		}

		for _, addr := range addrs {
			if rule.Src.IP.Equal(addr.IP) {
				logger.Infof("Delete rule %v", rule)
				err := netlink.RuleDel(&rule)
				if err != nil {
					errReturn = fmt.Errorf("failed to delete rule %v", err)
					logger.Errorf("... Failed! %v", err)
				}
				continue RULE_LOOP
			}
		}

	}
	return errReturn
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("sbr-eip"))
}

func cmdCheck(_ *skel.CmdArgs) error {
	return nil
}
