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

package cniconf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	uuid "github.com/satori/go.uuid"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	ipamutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	v1alpha1network "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	utilenv "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/env"
	utilpool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	fsutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/kernel"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
)

const (
	ipvlanRequiredKernelVersion = "4.9"
	ipvlanKernelModuleName      = "ipvlan"
	forceRecreatePeriod         = 60 * time.Second

	pluginsConfigKey = "plugins"
	localDNSAddress  = "169.254.20.10"
)

var (
	roceVF = map[string]struct{}{
		"elastic_rdma": {},
		"rdma_roce":    {},
	}
)

func New(
	kubeClient kubernetes.Interface,
	ippoolLister v1alpha1network.IPPoolLister,
	cniMode types.ContainerNetworkMode,
	nodeName string,
	config *v1alpha1.CNIConfigControllerConfiguration,
) *Controller {
	return &Controller{
		kubeClient:    kubeClient,
		metaClient:    metadata.NewClient(),
		ippoolLister:  ippoolLister,
		cniMode:       cniMode,
		nodeName:      nodeName,
		config:        config,
		netutil:       networkutil.New(),
		kernelhandler: kernel.NewLinuxKernelHandler(),
		filesystem:    fsutil.DefaultFS{},
	}
}

func (c *Controller) SyncNode(nodeKey string, _ corelisters.NodeLister) error {
	ctx := log.NewContext()

	return c.syncCNIConfig(ctx, nodeKey)
}

func (c *Controller) ReconcileCNIConfig() {
	ctx := log.NewContext()

	waitErr := wait.PollImmediateInfinite(forceRecreatePeriod, func() (bool, error) {
		ctx := log.NewContext()
		syncErr := c.syncCNIConfig(ctx, c.nodeName)
		if syncErr != nil {
			log.Errorf(ctx, "syncCNIConfig error: %s", syncErr)
		}
		return false, nil
	})
	if waitErr != nil {
		log.Errorf(ctx, "execute syncCNIConfig error: %s", waitErr)
	}
}

func (c *Controller) syncCNIConfig(ctx context.Context, nodeName string) error {
	log.V(6).Infof(ctx, "syncCNIConfig for node: %v begin", nodeName)
	defer log.V(6).Infof(ctx, "syncCNIConfig for node: %v end", nodeName)
	if nodeName != c.nodeName {
		return nil
	}

	// auto detect, prepare data for template render
	dataObj, err := c.fillCNIConfigData(ctx)
	if err != nil {
		log.Errorf(ctx, "failed to fill cni config data: %v", err)
		return err
	}

	// get cni config template file path
	templateFilePath, err := c.getCNIConfigTemplateFilePath(ctx)
	if err != nil {
		log.Errorf(ctx, "failed to get cni config template file path: %v", err)
		return err
	}

	// read cni config template
	rawTemplateContent, err := c.filesystem.ReadFile(templateFilePath)
	if err != nil {
		log.Errorf(ctx, "failed to read cni config template file %v: %v", c.config.CNIConfigTemplateFile, err)
		return err
	}

	// render cni config template with data to get final rendered content
	renderedContent, err := renderTemplate(ctx, string(rawTemplateContent), dataObj)
	if err != nil {
		return err
	}

	renderedContent, err = c.patchCNIConfig(ctx, renderedContent, dataObj)
	if err != nil {
		log.Errorf(ctx, "failed to patch CNI config template. err:[%+v]", err)
		return err
	}

	// ensure cni config file is updated
	err = c.createOrUpdateCNIConfigFileContent(ctx, renderedContent)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) getCNIConfigTemplateFilePath(_ context.Context) (string, error) {
	if c.config.AutoDetectConfigTemplateFile {
		tplFilePath, ok := CCETemplateFilePathMap[c.cniMode]
		if !ok {
			return "", fmt.Errorf("cannot find cni template file for cni mode %v", c.cniMode)
		}
		return tplFilePath, nil
	}

	return c.config.CNIConfigTemplateFile, nil
}

func (c *Controller) getK8sNode(ctx context.Context) (*v1.Node, error) {
	if c.kubeClient == nil {
		return nil, fmt.Errorf("kubeclient is nil")
	}

	nodeName, err := utilenv.GetNodeName(ctx)
	if err != nil {
		return nil, err
	}

	n, err := c.kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (c *Controller) getNodeInstanceTypeEx(ctx context.Context) (metadata.InstanceTypeEx, error) {
	instanceType, err := c.metaClient.GetInstanceTypeEx()
	// the instanceTypeEx must be "bcc" liked
	if err == nil && len(instanceType) == 3 {
		return instanceType, nil
	}

	log.Warning(ctx, "metadata error, fallback to get instance type from node labels")
	node, err := c.getK8sNode(ctx)
	if err == nil {
		instanceType = ipamutil.GetNodeInstanceType(node)
		return instanceType, nil
	}

	return "", fmt.Errorf("failed to get instance type from metadata api: %v", err)
}

func (c *Controller) fillCNIConfigData(ctx context.Context) (*CNIConfigData, error) {
	var err error
	configData := CNIConfigData{NetworkName: c.config.CNINetworkName}

	if !c.config.AutoDetectConfigTemplateFile {
		return &configData, nil
	}

	if types.IsCCECNIModeAutoDetect(c.cniMode) {
		log.V(6).Infof(ctx, "auto detect kernel version due to cni mode is: %v", c.cniMode)

		// detect kernel version
		kernelVersionStr, err := c.kernelhandler.DetectKernelVersion(ctx)
		if err != nil {
			log.Errorf(ctx, "detect kernel version failed: %v", err)
		}
		kernelVersion, err := version.ParseGeneric(kernelVersionStr)
		if err != nil {
			log.Errorf(ctx, "parse kernel version failed: %v", err)
		}

		// load kernel module
		kernelModules, err := c.kernelhandler.GetModules(ctx, []string{ipvlanKernelModuleName})
		if err != nil {
			log.Errorf(ctx, "get kernel modules failed: %v", err)
		}

		log.V(6).Infof(ctx, "kernel version: %v, kernel modules: %v", kernelVersion.String(), kernelModules)

		if canUseIPVlan(kernelVersion, kernelModules) {
			switch c.cniMode {
			case types.CCEModeSecondaryIPAutoDetect:
				c.cniMode = types.CCEModeSecondaryIPIPVlan
			case types.CCEModeBBCSecondaryIPAutoDetect:
				c.cniMode = types.CCEModeBBCSecondaryIPIPVlan
			case types.CCEModeRouteAutoDetect:
				c.cniMode = types.CCEModeRouteIPVlan
			}
		} else {
			switch c.cniMode {
			case types.CCEModeSecondaryIPAutoDetect:
				c.cniMode = types.CCEModeSecondaryIPVeth
			case types.CCEModeBBCSecondaryIPAutoDetect:
				c.cniMode = types.CCEModeBBCSecondaryIPVeth
			case types.CCEModeRouteAutoDetect:
				c.cniMode = types.CCEModeRouteVeth
			}
		}
	}

	// assemble ipam endpoint from clusterip
	svc, err := c.kubeClient.CoreV1().Services(IPAMServiceNamespace).Get(ctx, IPAMServiceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if len(svc.Spec.Ports) != 0 {
		configData.IPAMEndPoint = fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].Port)
	}

	// get instance type from meta
	if configData.InstanceType == "" {
		insType, err := c.getNodeInstanceTypeEx(ctx)
		if err != nil {
			log.Errorf(ctx, "failed to get instance type via metadata: %v", err)
		}
		configData.InstanceType = string(insType)
	}

	if types.IsCCECNIModeBasedOnSecondaryIP(c.cniMode) || types.IsCrossVPCEniMode(c.cniMode) {
		if configData.InstanceType == string(metadata.InstanceTypeExBCC) {
			if c.cniMode == types.CCEModeBBCSecondaryIPIPVlan {
				c.cniMode = types.CCEModeSecondaryIPIPVlan
			}

			if c.cniMode == types.CCEModeBBCSecondaryIPVeth {
				c.cniMode = types.CCEModeSecondaryIPVeth
			}
		}
	}

	if types.IsCCECNIModeBasedOnVPCRoute(c.cniMode) || types.IsCrossVPCEniMode(c.cniMode) {
		ipPoolName := utilpool.GetNodeIPPoolName(c.nodeName)
		ipPool, err := c.ippoolLister.IPPools(v1.NamespaceDefault).Get(ipPoolName)
		if err != nil {
			return nil, err
		}

		if len(ipPool.Spec.IPv4Ranges) != 0 {
			configData.Subnet = ipPool.Spec.IPv4Ranges[0].CIDR
		} else if len(ipPool.Spec.IPv6Ranges) != 0 {
			configData.Subnet = ipPool.Spec.IPv6Ranges[0].CIDR
		} else {
			return nil, fmt.Errorf("ippool %s spec subnet is empty", ipPoolName)
		}
	}

	configData.MasterInterface, err = c.netutil.DetectDefaultRouteInterfaceName()
	if err != nil {
		log.Errorf(ctx, "failed to detect default route interface: %v", err)
		return nil, err
	}

	configData.VethMTU, err = c.netutil.DetectInterfaceMTU(configData.MasterInterface)
	if err != nil {
		configData.VethMTU = DefaultMTU
	}
	configData.LocalDNSAddress = localDNSAddress
	return &configData, nil
}

func (c *Controller) createOrUpdateCNIConfigFileContent(ctx context.Context, cniConfigContent string) error {
	// create name for temp file
	targetFile := filepath.Join(c.config.CNIConfigDir, c.config.CNIConfigFileName)
	tempFile := targetFile + uuid.NewV4().String()[0:6]

	// write cni config to a temp file
	err := c.filesystem.WriteFile(tempFile, []byte(cniConfigContent), 0644)
	if err != nil {
		log.Errorf(ctx, "failed to write rendered content into temporary cni file %v: %v", tempFile, err)
		return err
	}

	// get temp file md5
	tempFileMD5, err := c.filesystem.MD5Sum(tempFile)
	if err != nil {
		log.Errorf(ctx, "failed to hash %v md5sum: %v", tempFile, err)
		return err
	}

	// get target file md5
	targetFileMD5, err := c.filesystem.MD5Sum(targetFile)
	if err != nil {
		if !os.IsNotExist(err) {
			// other fs error
			log.Errorf(ctx, "failed to hash %v md5sum: %v", tempFile, err)
			return err
		}
		log.Infof(ctx, "cni config not exists, will create")
	}

	// if cni config not exists or md5sum not equal, just replace cni config with template file
	if err != nil || tempFileMD5 != targetFileMD5 {
		if err := c.filesystem.Rename(tempFile, targetFile); err != nil {
			log.Errorf(ctx, "failed to mv %v to %v: %v", tempFile, targetFile, err)
			// fall back to clean up tempfile
			if err := c.filesystem.Remove(tempFile); err != nil && !os.IsNotExist(err) {
				log.Errorf(ctx, "failed to clean up temp cni config file %v: %v", tempFile, err)
			}
			return err
		}
		log.Infof(ctx, "create/update cni config file %v successfully", targetFile)
	}

	// clean up temp file
	if err := c.filesystem.Remove(tempFile); err != nil && !os.IsNotExist(err) {
		log.Errorf(ctx, "failed to clean up temp cni config file %v: %v", tempFile, err)
	}

	return nil
}

func renderTemplate(ctx context.Context, tplContent string, dataObject *CNIConfigData) (string, error) {
	tmpl, err := template.New("cni-template").Parse(tplContent)
	if err != nil {
		log.Errorf(ctx, "parse cni template failed: %v", err)
		return "", err
	}

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, dataObject); err != nil {
		log.Errorf(ctx, "execute cni template failed: %v", err)
		return "", err
	}

	return buf.String(), nil
}

func (c *Controller) patchCNIConfig(ctx context.Context, oriYamlStr string, dataObject *CNIConfigData) (string, error) {
	// has roce?
	if c.hasRoCE(ctx) {
		return c.patchCNIConfigForRoCE(ctx, oriYamlStr, dataObject)
	}
	return oriYamlStr, nil
}

func (c *Controller) patchCNIConfigForRoCE(ctx context.Context, oriYamlStr string,
	dataObject *CNIConfigData) (string, error) {
	// patch eri info to cni config
	oriConfigMap := make(map[string]interface{})
	jsonErr := json.Unmarshal([]byte(oriYamlStr), &oriConfigMap)
	if jsonErr != nil {
		log.Errorf(ctx, "invalid format oriYamlStr: [%s]", oriYamlStr)
		return "", jsonErr
	}
	if _, ok := oriConfigMap[pluginsConfigKey]; !ok {
		oriConfigMap[pluginsConfigKey] = make([]interface{}, 0)
	}

	eriConfigMap := map[string]interface{}{
		"type": "roce",
		"ipam": map[string]string{
			"endpoint": dataObject.IPAMEndPoint,
		},
		"instanceType": dataObject.InstanceType,
	}
	oriConfigMap[pluginsConfigKey] = append(oriConfigMap[pluginsConfigKey].([]interface{}), eriConfigMap)

	newConfig, jsonErr := json.MarshalIndent(&oriConfigMap, "", "  ")
	return string(newConfig), nil
}

func (c *Controller) hasRoCE(ctx context.Context) bool {
	// list network interface macs
	macList, macErr := c.metaClient.ListMacs()
	if macErr != nil {
		log.Errorf(ctx, "list mac failed: %w", macErr)
		return false
	}

	// check whether there is ERI
	for _, macAddress := range macList {
		vifFeatures, vifErr := c.metaClient.GetVifFeatures(macAddress)
		if vifErr != nil {
			log.Errorf(ctx, "get mac %s vif features failed: %w", macAddress, vifErr)
			continue
		}
		if _, ok := roceVF[vifFeatures]; ok {
			return true
		}
	}
	return false
}

func canUseIPVlan(kernelVersion *version.Version, kernelModules []string) bool {
	if kernelVersion == nil || kernelVersion.LessThan(version.MustParseGeneric(ipvlanRequiredKernelVersion)) {
		return false
	}

	var ipvlanLoaded bool

	for _, module := range kernelModules {
		if module == ipvlanKernelModuleName {
			ipvlanLoaded = true
			break
		}
	}

	return ipvlanLoaded
}
