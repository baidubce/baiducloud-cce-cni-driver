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

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/client/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/client"
	plugintypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/sirupsen/logrus"
)

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

// cmdAdd is called for ADD requests
func cmdAdd(args *skel.CmdArgs) (err error) {
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "endpoint-probe",
		"mod":     "ADD",
	})
	defer func() {
		if err != nil {
			logger.WithError(err).Errorf("cni plugin failed")
		}
	}()

	var conf *PluginConf
	conf, err = parseConfig(args.StdinData)
	if err != nil {
		return fmt.Errorf("endpoint-probe failed to parse config: %v", err)
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

	var netns ns.NetNS
	netns, err = ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	var cctx *link.ContainerContext
	cctx, err = link.NewContainerContext(args.ContainerID, args.Netns)
	if err != nil {
		return fmt.Errorf("failed to create container context: %v", err)
	}

	var response *models.EndpointProbeResponse
	response, err = probeEndpointFeature(args, cctx.Driver)
	if err != nil {
		return fmt.Errorf("failed to exec endpoint probe: %v", err)
	}
	err = handleBandwidth(cctx, response.BandWidth)
	if err != nil {
		return fmt.Errorf("failed to set bandwidth: %v", err)
	}
	logger.WithField("result", logfields.Json(conf.PrevResult)).Info("success to exec plugin")
	// Pass through the result for the next plugin
	return types.PrintResult(conf.PrevResult, conf.CNIVersion)
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
	logging.SetupCNILogging("cni", true)
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json(args),
		"plugin":  "endpoint-probe",
		"mod":     "DEL",
	})
	logger.Infof("success to exec endpoint-probe")
	return nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("sbr-eip"))
}

func cmdCheck(_ *skel.CmdArgs) error {
	return nil
}
