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
	"context"
	"encoding/json"
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/client/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/client"
	plugintypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
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
		err := logging.SetupLogging([]string{}, logOptions, "endpoint-probe", n.EnableDebug)
		if err != nil {
			return err
		}
		logging.AddHooks(hooks.NewFileRotationLogHook(n.LogFile,
			hooks.EnableCompression(),
			hooks.WithMaxBackups(1),
		))
	} else {
		logOptions["syslog.facility"] = "local5"
		err := logging.SetupLogging([]string{"syslog"}, logOptions, "endpoint-probe", true)
		if err != nil {
			return err
		}
	}
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"containerID": args.ContainerID,
		"netns":       args.Netns,
		"plugin":      "endpoint-probe",
		"method":      method,
	})

	return nil
}

// cmdAdd is called for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return fmt.Errorf("endpoint-probe failed to parse config: %v", err)
	}
	err = setupLogging(conf, args, "ADD")
	if err != nil {
		return fmt.Errorf("endpoint-probe failed to set up logging: %v", err)
	}
	defer func() {
		if err != nil {
			logger.Errorf("cni plugin failed: %v", err)
		}
	}()
	logger.Debugf("configure endpoint-probe for new interface %s - previous result: %v",
		args.IfName, conf.PrevResult)

	if conf.PrevResult == nil {
		return fmt.Errorf("this plugin must be called as chained plugin")
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	cctx, err := link.NewContainerContext(args.ContainerID, args.Netns)
	if err != nil {
		return fmt.Errorf("failed to create container context: %v", err)
	}

	response, err := probeEndpointFeature(args, cctx.Driver)
	if err != nil {
		return fmt.Errorf("failed to exec endpoint probe: %v", err)
	}
	err = handleBandwidth(cctx, response.BandWidth)
	if err != nil {
		return fmt.Errorf("failed to set bandwidth: %v", err)
	}
	// Pass through the result for the next plugin
	return types.PrintResult(conf.PrevResult, conf.CNIVersion)
}

func checkExtStatus(ctx context.Context, args *skel.CmdArgs, featureGates []*models.EndpointProbeFeatureGate) (bool, error) {
	cniArgs := plugintypes.ArgsSpec{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return false, fmt.Errorf("unable to extract CNI arguments: %s", err)
	}
	var (
		extPluginData map[string]map[string]string
		err           error
		owner         = string(cniArgs.K8S_POD_NAMESPACE + "/" + cniArgs.K8S_POD_NAME)
		containerID   = args.ContainerID
	)

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		err = fmt.Errorf("unable to connect to cce-network-v2-agent: %s", client.Hint(err))
		return false, err
	}
	param := endpoint.NewGetEndpointExtpluginStatusParams().WithOwner(&owner).WithContainerID(&containerID).WithContext(ctx)
	result, err := c.Endpoint.GetEndpointExtpluginStatus(param)
	if err != nil {
		err = fmt.Errorf("unable to get endpoint extplugin status: %s", client.Hint(err))
		return false, err
	}
	extPluginData = result.Payload
	for _, featureGate := range featureGates {
		if featureGate.WaitReady {
			pluginData, ok := extPluginData[featureGate.Name]
			if !ok {
				return false, nil
			}
			logger.WithField("feature", featureGate).WithField("pluginData", logfields.Repr(pluginData)).Infof("found feature gate")
		}
	}
	return true, nil
}

func probeEndpointFeature(args *skel.CmdArgs, driver string) (*models.EndpointProbeResponse, error) {
	cniArgs := plugintypes.ArgsSpec{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return nil, fmt.Errorf("unable to extract CNI arguments: %s", err)
	}
	var (
		err         error
		owner       = string(cniArgs.K8S_POD_NAMESPACE + "/" + cniArgs.K8S_POD_NAME)
		containerID = args.ContainerID
	)

	c, err := client.NewDefaultClientWithTimeout(defaults.ClientConnectTimeout)
	if err != nil {
		err = fmt.Errorf("unable to connect to cce-network-v2-agent: %s", client.Hint(err))
		return nil, err
	}
	param := endpoint.NewPutEndpointProbeParams().WithOwner(&owner).WithContainerID(&containerID).WithCniDriver(&driver)
	result, err := c.Endpoint.PutEndpointProbe(param)
	if err != nil {
		err = fmt.Errorf("unable to probe endpoint extplugin: %s", client.Hint(err))
		return nil, err
	}
	return result.Payload, nil
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {

	return nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("sbr-eip"))
}

func cmdCheck(_ *skel.CmdArgs) error {
	return nil
}
