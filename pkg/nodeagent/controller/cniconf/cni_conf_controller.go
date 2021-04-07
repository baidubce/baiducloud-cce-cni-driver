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
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	uuid "github.com/satori/go.uuid"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	v1alpha1network "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/listers/networking/v1alpha1"
	utilpool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	fsutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/fs"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/kernel"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	networkutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/network"
)

const (
	ipvlanRequiredKernelVersion = "4.9"
	ipvlanKernelModuleName      = "ipvlan"
	vethDefaultMTU              = 1500
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

func (c *Controller) SyncNode(nodeKey string, nodeLister corelisters.NodeLister) error {
	ctx := log.NewContext()

	return c.syncCNIConfig(ctx, nodeKey)
}

func (c *Controller) syncCNIConfig(ctx context.Context, nodeName string) error {
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
	templateContent, err := c.filesystem.ReadFile(templateFilePath)
	if err != nil {
		log.Errorf(ctx, "failed to read cni config template file %v: %v", c.config.CNIConfigTemplateFile, err)
		return err
	}

	// render cni config template with data to get final rendered content
	renderedContent, err := renderTemplate(ctx, string(templateContent), dataObj)
	if err != nil {
		return err
	}

	// ensure cni config file is updated
	err = c.createOrUpdateCNIConfigFileContent(ctx, renderedContent)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) getCNIConfigTemplateFilePath(ctx context.Context) (string, error) {
	if c.config.AutoDetectConfigTemplateFile {
		filepath, ok := CCETemplateFilePathMap[c.cniMode]
		if !ok {
			return "", fmt.Errorf("cannot find cni template file for cni mode %v", c.cniMode)
		}
		return filepath, nil
	}

	return c.config.CNIConfigTemplateFile, nil
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

	if types.IsCCECNIModeBasedOnSecondaryIP(c.cniMode) {
		// assemble ipam endpoint from clusterip
		svc, err := c.kubeClient.CoreV1().Services(IPAMServiceNamespace).Get(IPAMServiceName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if len(svc.Spec.Ports) != 0 {
			configData.IPAMEndPoint = fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].Port)
		}

		// get instance type from meta
		if configData.InstanceType == "" {
			insType, err := c.metaClient.GetInstanceTypeEx()
			if err != nil {
				log.Errorf(ctx, "failed to get instance type via metadata: %v", err)
			}
			configData.InstanceType = string(insType)
		}
	}

	if types.IsCCECNIModeBasedOnVPCRoute(c.cniMode) {
		ipPoolName := utilpool.GetDefaultIPPoolName(c.nodeName)
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

	if c.cniMode == types.CCEModeBBCSecondaryIPIPVlan || c.cniMode == types.CCEModeSecondaryIPIPVlan || c.cniMode == types.CCEModeRouteIPVlan {
		configData.MasterInterface, err = c.netutil.DetectDefaultRouteInterfaceName()
		if err != nil {
			return nil, err
		}

		configData.VethMTU, err = c.netutil.DetectInterfaceMTU(configData.MasterInterface)
		if err != nil {
			configData.VethMTU = vethDefaultMTU
		}
	}

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
