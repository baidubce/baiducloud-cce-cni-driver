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

package app

import (
	"context"
	utiljson "encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	agentconfig "github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	ipamutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	utilenv "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/env"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	defaultAgentConfigFile      = "/etc/kubernetes/cni-node-agent.yaml"
	defaultCNINetworkName       = "cce-cni"
	defaultCNIConfigFileName    = "00-cce-cni.conflist"
	defaultCNIConfigDir         = "/etc/cni/net.d/"
	defaultInformerResyncPeriod = 20 * time.Second
	defaultENISyncPeriod        = 30 * time.Second
)

// newOptions returns initialized Options
func newOptions() *Options {
	opts := &Options{
		config:     new(agentconfig.NodeAgentConfiguration),
		errCh:      make(chan error),
		metaClient: metadata.NewClient(),
	}

	// try best to create kubeclient
	config, err := rest.InClusterConfig()
	if err == nil {
		opts.kubeClient, _ = kubernetes.NewForConfig(config)
	}

	return opts
}

// complete completes all the required options.
func (o *Options) complete(ctx context.Context, args []string) error {
	if len(o.configFile) > 0 {
		c, err := o.loadConfigFromFile(ctx, o.configFile)
		if err != nil {
			log.Errorf(ctx, "error parsing node agent config file %v: %v", o.configFile, err)
			return err
		}

		log.Infof(ctx, "agent using config file %v", o.configFile)

		o.config = c
	}

	return o.applyDefaults(ctx)
}

// TODO:
// validate validates agent options
func (o *Options) validate() error {
	if types.IsKubenetMode(o.config.CNIMode) || types.IsCCECNIMode(o.config.CNIMode) {
		return o.validateCCE()
	}

	return fmt.Errorf("validate error: unknown cni mode: %v", o.config.CNIMode)
}

// run runs the agent
func (o *Options) run(ctx context.Context) error {
	defer close(o.errCh)

	nodeAgent, err := newNodeAgent(o)
	if err != nil {
		return err
	}

	go func() {
		err := nodeAgent.run(ctx)
		o.errCh <- err
	}()

	for {
		err := <-o.errCh
		if err != nil {
			return err
		}
	}
}

// loadConfigFromFile loads the contents of file and decodes it as a NodeAgentConfiguration object.
func (o *Options) loadConfigFromFile(ctx context.Context, file string) (*agentconfig.NodeAgentConfiguration, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	json, err := utilyaml.ToJSON(data)
	if err != nil {
		return nil, err
	}

	c := &agentconfig.NodeAgentConfiguration{}
	if err := utiljson.Unmarshal(json, c); err != nil {
		return nil, err
	}

	return c, nil
}

// addFlags adds flags to fs and binds them to options.
func (o *Options) addFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.configFile, "config", defaultAgentConfigFile, "The path to the configuration file.")
}

// applyDefaults apply default options for node agent
func (o *Options) applyDefaults(ctx context.Context) error {
	var err error

	if o.config.CNIMode == "" {
		typeEx, err := o.metaClient.GetInstanceTypeEx()
		if err == nil {
			switch typeEx {
			case metadata.InstanceTypeExBCC:
				o.config.CNIMode = types.CCEModeSecondaryIPAutoDetect
			case metadata.InstanceTypeExBBC:
				o.config.CNIMode = types.CCEModeBBCSecondaryIPAutoDetect
			}
		}
	}
	if o.config.Workers <= 0 {
		o.config.Workers = 1
	}
	if o.config.ResyncPeriod <= 0 {
		o.config.ResyncPeriod = types.Duration(defaultInformerResyncPeriod)
	}

	o.applyCNIConfigDefaults(&o.config.CNIConfig)

	switch {
	case types.IsKubenetMode(o.config.CNIMode) || types.IsCCECNIMode(o.config.CNIMode):
		if err := o.applyCCEConfigDefaults(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown cni mode: %v", o.config.CNIMode)
	}

	o.hostName, err = utilenv.GetNodeName(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (o *Options) applyCNIConfigDefaults(config *agentconfig.CNIConfigControllerConfiguration) {
	cniMode := o.config.CNIMode

	if types.IsCCECNIMode(cniMode) {
		if config.CNINetworkName == "" {
			config.CNINetworkName = defaultCNINetworkName
		}
		if config.CNIConfigFileName == "" {
			config.CNIConfigFileName = defaultCNIConfigFileName
		}
		if config.CNIConfigDir == "" {
			config.CNIConfigDir = defaultCNIConfigDir
		}

		config.AutoDetectConfigTemplateFile = true
	}

}

func (o *Options) applyCCEConfigDefaults(ctx context.Context) error {
	log.Infof(ctx, "cni mode is %v, assume agent runs in CCE", o.config.CNIMode)

	var err error

	if o.instanceID == "" {
		o.instanceID, err = o.getNodeInstanceID(ctx)
		if err != nil {
			return err
		}
	}

	if o.config.CCE.Region == "" {
		o.config.CCE.Region, err = o.metaClient.GetRegion()
		if err != nil {
			return fmt.Errorf("failed to get region from metadata api: %v", err)
		}
	}

	var subnetIDUnavailable, vpcIDUnavailable bool

	if o.subnetID == "" {
		o.subnetID, err = o.metaClient.GetSubnetID()
		if err != nil {
			subnetIDUnavailable = true
		}
	}

	if o.config.CCE.VPCID == "" {
		o.config.CCE.VPCID, err = o.metaClient.GetVPCID()
		if err != nil {
			vpcIDUnavailable = true
		}
	}

	if subnetIDUnavailable || vpcIDUnavailable {
		log.Warningf(ctx, "metadata error, fallback to get instance info from open api...")
	}
	o.bceClient, err = cloud.New(o.config.CCE.Region, o.config.CCE.ClusterID, o.config.CCE.AccessKeyID, o.config.CCE.SecretAccessKey, false)
	if err == nil {
		instance, err := o.bceClient.DescribeInstance(ctx, o.instanceID)
		if err == nil {
			if subnetIDUnavailable {
				o.subnetID = instance.SubnetId
				subnetIDUnavailable = false
			}

			if vpcIDUnavailable {
				o.config.CCE.VPCID = instance.VpcId
				vpcIDUnavailable = false
			}
		} else {
			log.Errorf(ctx, "failed to describe instance %v: %v", o.instanceID, err)
		}
	} else {
		log.Errorf(ctx, "failed to new bce client: %v", err)
	}

	if subnetIDUnavailable {
		return errors.New("failed to get subnet id from metadata and open api")
	}
	if vpcIDUnavailable {
		return errors.New("failed to get vpc id from metadata and open api")
	}

	// route mode
	if types.IsCCECNIModeBasedOnVPCRoute(o.config.CNIMode) {

	}

	// secondary ip mode
	if types.IsCCECNIModeBasedOnBCCSecondaryIP(o.config.CNIMode) {
		if o.config.CCE.ENIController.ENISyncPeriod <= 0 {
			o.config.CCE.ENIController.ENISyncPeriod = types.Duration(defaultENISyncPeriod)
		}
	}

	return nil
}

func (o *Options) validateCCE() error {
	return nil
}

func (o *Options) getNodeInstanceID(ctx context.Context) (string, error) {
	instanceID, err := o.metaClient.GetInstanceID()
	if err == nil {
		return instanceID, nil
	}

	if o.kubeClient != nil {
		log.Warning(ctx, "metadata error, fallback to get instance id from node providerID")
		nodeName, err := utilenv.GetNodeName(ctx)
		if err == nil {
			n, err := o.kubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
			if err == nil {
				instanceID = ipamutil.GetInstanceIDFromNode(n)
				if instanceID != "" {
					return instanceID, nil
				}
			}
		}
	}

	return "", fmt.Errorf("failed to get instance id from metadata api: %v", err)
}
