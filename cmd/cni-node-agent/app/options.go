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
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
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
	defaultInformerResyncPeriod = 10 * time.Hour
	defaultENISyncPeriod        = 30 * time.Second

	securityGroupIDPrefix           = "g-"
	enterpriseSecurityGroupIDPrefix = "esg-"
)

const (
	// CNIPatchConfigLabel is the label name for derived config
	CNIPatchConfigLabel = "cce-cni-patch-config"
)

// newOptions returns initialized Options
func newOptions() *Options {
	opts := &Options{
		config:     new(agentconfig.NodeAgentConfiguration),
		errCh:      make(chan error),
		metaClient: metadata.NewClient(),
	}

	ctx := log.NewContext()

	// try best to create kubeclient
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Errorf(ctx, "newOptions: failed to get in cluster config: %v", err)
	} else {
		opts.kubeClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			log.Errorf(ctx, "newOptions: failed to create kube client: %v", err)
		}
	}

	return opts
}

// complete completes all the required options.
func (o *Options) complete(ctx context.Context, args []string) error {
	if len(o.configFile) > 0 {
		log.Infof(ctx, "agent using config file %v", o.configFile)
		c, globalCfg, err := o.loadConfigFromFile(ctx, o.configFile)
		if err != nil {
			log.Errorf(ctx, "error parsing node agent config file %v: %v", o.configFile, err)
			return err
		}

		needPatch, patchName, err := o.getPatchConfigName(ctx)
		if err == nil && needPatch {
			log.Infof(ctx, "node agent detected patch configmap: %v", patchName)
			patch, err := o.getPatchConfigData(ctx, patchName)
			if err == nil {
				log.Infof(ctx, "node agent patch config: %v", string(patch))
				c, err = o.mergeConfigAndUnmarshal(ctx, globalCfg, []byte(patch))
				if err != nil {
					log.Errorf(ctx, "merge json error: %v", err)
					return err
				}
			} else {
				log.Warningf(ctx, "fallback to default config due to error: %v", err)
			}
		}
		// if we encounter an error here, eg. kube-proxy starts later than agent, thus io timeout
		// we just return error, make this agent restarts.
		// otherwise, we might misconfigure what user really expects.
		if err != nil {
			msg := fmt.Sprintf("get patch config error: %v", err)
			log.Error(ctx, msg)
			return errors.New(msg)
		}
		if !needPatch {
			log.Infof(ctx, "no patch config specified on node")
		}

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
func (o *Options) loadConfigFromFile(ctx context.Context, file string) (*agentconfig.NodeAgentConfiguration, []byte, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}

	json, err := utilyaml.ToJSON(data)
	if err != nil {
		return nil, nil, err
	}

	c := &agentconfig.NodeAgentConfiguration{}
	if err := utiljson.Unmarshal(json, c); err != nil {
		return nil, nil, err
	}

	log.Infof(ctx, "node agent config: %v", string(json))

	return c, json, nil
}

// addFlags adds flags to fs and binds them to options.
func (o *Options) addFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.configFile, "config", defaultAgentConfigFile, "The path to the configuration file.")
}

func (o *Options) getNodeInstanceTypeEx(ctx context.Context) (metadata.InstanceTypeEx, error) {
	instanceType, err := o.metaClient.GetInstanceTypeEx()
	// the instanceID must be "i-QwUTCSuA" liked
	if err == nil && len(instanceType) == 3 {
		return instanceType, nil
	}

	log.Warning(ctx, "metadata error, fallback to get instance type from node labels")
	node, err := o.getK8sNode(ctx)
	if err == nil {
		instanceType = ipamutil.GetNodeInstanceType(node)
		return instanceType, nil
	}

	return "", fmt.Errorf("failed to get instance type from metadata api: %v", err)
}

// applyDefaults apply default options for node agent
func (o *Options) applyDefaults(ctx context.Context) error {
	var err error

	// we are unlikely to hit this
	if o.metaClient == nil {
		return fmt.Errorf("meta client is nil")
	}

	// get instance type
	o.instanceType, err = o.getNodeInstanceTypeEx(ctx)
	log.Infof(ctx, "get instance type: %v", o.instanceType)
	if err != nil {
		log.Fatalf(ctx, "failed to get instance type via metadata: %v", err)
	}

	if o.config.CNIMode == "" {
		switch o.instanceType {
		case metadata.InstanceTypeExBCC:
			o.config.CNIMode = types.CCEModeSecondaryIPAutoDetect
		case metadata.InstanceTypeExBBC:
			o.config.CNIMode = types.CCEModeBBCSecondaryIPAutoDetect
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
	log.Infof(ctx, "cni mode is %v", o.config.CNIMode)

	var err error

	if o.instanceID == "" {
		o.instanceID, err = o.getNodeInstanceID(ctx)
		log.Infof(ctx, "get instance id: %v", o.instanceID)
		if err != nil {
			return err
		}
	}

	if o.config.CCE.Region == "" {
		o.config.CCE.Region, err = o.metaClient.GetRegion()
		log.Infof(ctx, "get region: %v", o.config.CCE.Region)
		if err != nil {
			return fmt.Errorf("failed to get region from metadata api: %v", err)
		}
	}

	var subnetIDUnavailable, vpcIDUnavailable bool

	if o.subnetID == "" {
		o.subnetID, err = o.metaClient.GetSubnetID()
		log.Infof(ctx, "get subnet id: %v", o.subnetID)
		if err != nil {
			subnetIDUnavailable = true
		}
	}

	if o.config.CCE.VPCID == "" {
		o.config.CCE.VPCID, err = o.metaClient.GetVPCID()
		log.Infof(ctx, "get vpc id: %v", o.config.CCE.VPCID)
		if err != nil {
			vpcIDUnavailable = true
		}
	}

	if subnetIDUnavailable || vpcIDUnavailable {
		log.Warningf(ctx, "metadata error, fallback to get instance info from open api...")
	}
	o.bceClient, err = cloud.New(
		o.config.CCE.Region,
		o.config.CCE.ClusterID,
		o.config.CCE.AccessKeyID,
		o.config.CCE.SecretAccessKey,
		o.kubeClient,
		false)
	if err == nil {
		instance, err := o.bceClient.GetBCCInstanceDetail(ctx, o.instanceID)
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

	if o.config.CCE.ENIController.ENISyncPeriod <= 0 {
		o.config.CCE.ENIController.ENISyncPeriod = types.Duration(defaultENISyncPeriod)
	}

	if o.config.CCE.ENIController.RouteTableOffset == 0 {
		o.config.CCE.ENIController.RouteTableOffset = 127
	}

	if o.config.CCE.ENIController.PreAttachedENINum <= 0 {
		o.config.CCE.ENIController.PreAttachedENINum = 1
	}

	return nil
}

// TODO:
func (o *Options) validateCCE() error {
	if !(types.IsCCECNIModeBasedOnVPCRoute(o.config.CNIMode) || types.IsKubenetMode(o.config.CNIMode) || types.IsCrossVPCEniMode(o.config.CNIMode)) {
		err := validateSecurityGroups(o.config.CCE.ENIController.SecurityGroupList)
		if err != nil {
			return err
		}

		err = validateEnterpriseSecurityGroups(o.config.CCE.ENIController.EnterpriseSecurityGroupList)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateSecurityGroups(secgroups []string) error {
	for _, s := range secgroups {
		if s == "" || !strings.HasPrefix(s, securityGroupIDPrefix) {
			return fmt.Errorf("security group id %v is not valid", s)
		}
	}
	return nil
}

func validateEnterpriseSecurityGroups(esecgroups []string) error {
	for _, s := range esecgroups {
		if s == "" || !strings.HasPrefix(s, enterpriseSecurityGroupIDPrefix) {
			return fmt.Errorf("enterprise security group id %v is not valid", s)
		}
	}
	return nil
}

func (o *Options) getK8sNode(ctx context.Context) (*v1.Node, error) {
	if o.node != nil {
		return o.node, nil
	}

	if o.kubeClient == nil {
		return nil, fmt.Errorf("kubeclient is nil")
	}

	nodeName, err := utilenv.GetNodeName(ctx)
	if err != nil {
		return nil, err
	}

	n, err := o.kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	o.node = n

	return n, nil
}

func (o *Options) getNodeInstanceID(ctx context.Context) (string, error) {
	instanceID, err := o.metaClient.GetInstanceID()
	// the instanceID must be "i-QwUTCSuA" liked
	if err == nil && len(instanceID) == 10 {
		return instanceID, nil
	}

	log.Warning(ctx, "metadata error, fallback to get instance id from node providerID")
	node, err := o.getK8sNode(ctx)
	if err == nil {
		instanceID, err = ipamutil.GetInstanceIDFromNode(node)
		if err == nil {
			return instanceID, nil
		}
	}

	return "", fmt.Errorf("failed to get instance id from metadata api: %v", err)
}

func (o *Options) getPatchConfigName(ctx context.Context) (bool, string, error) {
	node, err := o.getK8sNode(ctx)
	if err != nil {
		return false, "", err
	}

	patchConfigName, patchConfigSpecified := node.Labels[CNIPatchConfigLabel]

	return patchConfigSpecified, patchConfigName, nil
}

func (o *Options) getPatchConfigData(ctx context.Context, name string) (string, error) {
	cm, err := o.kubeClient.CoreV1().ConfigMaps("kube-system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	patch := cm.Data["config"]
	json, err := utilyaml.ToJSON([]byte(patch))

	return string(json), err
}

func (o *Options) mergeConfigAndUnmarshal(ctx context.Context, config, patch []byte) (*agentconfig.NodeAgentConfiguration, error) {
	var (
		jsonBytes []byte = config
		err       error
	)

	if len(patch) != 0 {
		// MergePatch in RFC7386
		jsonBytes, err = jsonpatch.MergePatch(config, patch)
		if err != nil {
			return nil, err
		}
		log.Infof(ctx, "node agent merged config result: %v", string(jsonBytes))
	}

	c := &agentconfig.NodeAgentConfiguration{}
	if err := utiljson.Unmarshal(jsonBytes, c); err != nil {
		return nil, err
	}

	return c, nil
}
