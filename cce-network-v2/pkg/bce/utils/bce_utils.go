/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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

package utils

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

const (
	UnderlayRDMA                string = string(ccev2.ENIForHPC) // Underlay RDMA (HPC)
	OverlayRDMA                 string = string(ccev2.ENIForERI) // Overlay RDMA (ERI)
	PodResourceName             string = "rdma"
	rdmaEndpointMiddleSeparator string = "-rdma-"
	eriManagedSuffix            string = "-eri-managed" // ERI managed NRS suffix
	// RDMA resource types
	RDMAResourceTypeHCA  string = "hca"  // HPC RDMA resource type
	RDMAResourceTypeEHCA string = "ehca" // ERI RDMA resource type
	// a name's max length is 253 and a labelSelectorValue's max length is 63 in kubernetes
	// nameValueMaxLength is a name's max length
	nameValueMaxLength int = 253
	// labelValueMaxLength is a label's max length
	labelValueMaxLength int = 63
)

var (
	log = logging.NewSubysLogger("bceutils")

	roceVFs = map[string]struct{}{
		OverlayRDMA:  {},
		UnderlayRDMA: {},
	}
)

// SetDefaultMetaClient sets the default metadata client (for testing purposes)
func SetDefaultMetaClient(client metadata.Interface) {
	metadata.DefaultMetaClient = client
}

type RdmaIfInfo struct {
	MacAddress         string
	NetResourceSetName string
	LabelSelectorValue string
	VifFeatures        string
}

func generateNetResourceSetName(nodeName, nodeInstanceID, macAddress, vifFeatures string) (netResourceSetName string) {
	// a name's max length is 253 in kubernetes, so if nodeName's length is more than the max length like this: 253 - len(string("-fa2700078302-elasticrdma")),
	// we need to use node's InstanceID as node's identification to generate NetResourceSetName
	var nodeIdentification string
	lengthLimit := nameValueMaxLength - len(string("-fa2700078302-elasticrdma"))
	// NetResourceSetName use nodeName for nodeIdentification if lengthLimit < nameValueMaxLength
	// if lengthLimit >= nameValueMaxLength, use node's InstanceID for nodeIdentification
	if len(nodeName) > lengthLimit {
		// cce-ujgjc8tg-lca6hnyh-fa2700078302-rdmaroce for HPC or cce-ujgjc8tg-lca6hnyh-fa2700078302-elasticrdma for ERI
		nodeIdentification = nodeInstanceID
	} else {
		// 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
		nodeIdentification = nodeName
	}

	macStr1 := strings.Replace(macAddress, ":", "", -1)
	macStr2 := strings.Replace(macStr1, "-", "", -1)
	vifFeaturesStr := strings.Replace(vifFeatures, "_", "", -1)
	netResourceSetName = fmt.Sprintf("%s-%s-%s", nodeIdentification, macStr2, vifFeaturesStr)
	// cce-ujgjc8tg-lca6hnyh-fa2700078302-rdmaroce for HPC or cce-ujgjc8tg-lca6hnyh-fa2700078302-elasticrdma for ERI
	// or 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
	return netResourceSetName
}

func GetNrsTypeByInstanceID(instanceID string) (string, error) {
	var nrsList []*ccev2.NetResourceSet
	var err error

	// Determine whether to use GetByHPASID or GetByInstanceID based on instanceID format
	// HPAS IDs start with "hpas-" prefix, regular instance IDs typically start with "i-"
	if strings.HasPrefix(instanceID, "hpas-") {
		nrsList, err = watchers.NetResourceSetClient.GetByHPASID(instanceID)
	} else {
		nrsList, err = watchers.NetResourceSetClient.GetByInstanceID(instanceID)
	}

	if err != nil || len(nrsList) == 0 {
		return "", err
	}

	for _, nrs := range nrsList {
		if nrs.Spec.ENI != nil && nrs.Spec.ENI.InstanceType != string(UnderlayRDMA) && nrs.Spec.ENI.InstanceType != string(OverlayRDMA) {
			return nrs.Spec.ENI.InstanceType, nil
		}
	}

	return "", nil

}

func generateRdmaNrsLabelSelectorValue(nodeName, nodeInstanceID, macAddress, vifFeatures string) (labelSelectorValue string) {
	// a labelSelectorValue's max length is 63 in kubernetes, so if nodeName's length is more than the max length like this:
	// 63 - len(string("-fa2700078302-elasticrdma")), we need to use node's InstanceID as node's identification to generate labelSelectorValue
	var labelSelectorIdentification string
	lengthLimit := labelValueMaxLength - len(string("-fa2700078302-elasticrdma"))
	// LabelSelectorValue use nodeName for nodeIdentification if lengthLimit < labelValueMaxLength
	// if lengthLimit >= labelValueMaxLength, use node's InstanceID for nodeIdentification
	if len(nodeName) > lengthLimit {
		// cce-ujgjc8tg-lca6hnyh-fa2700078302-rdmaroce for HPC or cce-ujgjc8tg-lca6hnyh-fa2700078302-elasticrdma for ERI
		labelSelectorIdentification = nodeInstanceID
	} else {
		// 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
		labelSelectorIdentification = nodeName
	}

	macStr1 := strings.Replace(macAddress, ":", "", -1)
	macStr2 := strings.Replace(macStr1, "-", "", -1)
	vifFeaturesStr := strings.Replace(vifFeatures, "_", "", -1)
	labelSelectorValue = fmt.Sprintf("%s-%s-%s", labelSelectorIdentification, macStr2, vifFeaturesStr)
	// cce-ujgjc8tg-lca6hnyh-fa2700078302-rdmaroce for HPC or cce-ujgjc8tg-lca6hnyh-fa2700078302-elasticrdma for ERI
	// or 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
	return labelSelectorValue
}

func generateEriNrsLabelSelectorValue(nodeName, nodeInstanceID string) (labelSelectorValue string) {
	var labelSelectorIdentification string
	lengthLimit := labelValueMaxLength - len(eriManagedSuffix)

	if len(nodeName) > lengthLimit {
		labelSelectorIdentification = nodeInstanceID
	} else {
		labelSelectorIdentification = nodeName
	}
	labelSelectorValue = labelSelectorIdentification + eriManagedSuffix
	return labelSelectorValue
}

func getNodeNameFromRdmaNetResourceSetName(netResourceSetName, rdmaTag string) (nodeName string) {
	// 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
	index1 := strings.Index(netResourceSetName, rdmaTag)
	nodeNameMacStr := netResourceSetName[:index1-1]
	index2 := strings.LastIndex(nodeNameMacStr, "-")
	nodeName = nodeNameMacStr[:index2]
	return nodeName
}

func GetNodeNameFromNetResourceSetName(netResourceSetName, ownerReferenceNodeName, nodeInstanceID string) (nodeName string) {
	// 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
	// 192.168.45.2-eri-managed for ERI managed NRS
	var nodeIdentification string
	underlayRDMA := strings.Replace(string(ccev2.ENIForHPC), "_", "", -1)
	overlayRDMA := strings.Replace(string(ccev2.ENIForERI), "_", "", -1)

	if strings.Contains(netResourceSetName, underlayRDMA) {
		nodeIdentification = getNodeNameFromRdmaNetResourceSetName(netResourceSetName, underlayRDMA)
	} else if strings.Contains(netResourceSetName, overlayRDMA) {
		nodeIdentification = getNodeNameFromRdmaNetResourceSetName(netResourceSetName, overlayRDMA)
	} else if strings.HasSuffix(netResourceSetName, eriManagedSuffix) {
		// Handle ERI managed NRS: 192.168.45.2-eri-managed -> 192.168.45.2
		nodeIdentification = strings.TrimSuffix(netResourceSetName, eriManagedSuffix)
	} else {
		nodeIdentification = netResourceSetName
	}

	if nodeIdentification == nodeInstanceID {
		nodeName = ownerReferenceNodeName
	} else {
		nodeName = nodeIdentification
	}

	return nodeName
}

func GetEriMangedNrsName(nodeName string) string {
	// Return node name with ERI managed suffix directly
	return nodeName + eriManagedSuffix
}

func GetRdmaNrsLabelSelectorValueFromNetResourceSetName(netResourceSetName, ownerReferenceNodeName, nodeInstanceID, macAddress, vifFeatures string) (labelSelectorValue string) {
	nodeName := GetNodeNameFromNetResourceSetName(netResourceSetName, ownerReferenceNodeName, nodeInstanceID)
	if macAddress == "" {
		return generateEriNrsLabelSelectorValue(nodeName, nodeInstanceID)
	}
	labelSelectorValue = generateRdmaNrsLabelSelectorValue(nodeName, nodeInstanceID, macAddress, vifFeatures)
	return labelSelectorValue
}

func generateCCERdmaEndpointName(podName, macAddress string) (endpointName string) {
	macStr1 := strings.Replace(macAddress, ":", "", -1)
	macStr2 := strings.Replace(macStr1, "-", "", -1)
	return fmt.Sprintf("%s%s%s", podName, rdmaEndpointMiddleSeparator, macStr2)
}

func getPrimaryMacFromCCERdmaEndpointName(cepName string) (macAddress string, err error) {
	index := strings.LastIndex(cepName, rdmaEndpointMiddleSeparator)
	if index == -1 {
		err = fmt.Errorf("invalid cep name %s", cepName)
	} else {
		macAddress = cepName[index+len(rdmaEndpointMiddleSeparator):]
	}
	return macAddress, err
}

func GetRdmaIFsInfo(nodeName string, scopedLog *logrus.Entry) (map[string]RdmaIfInfo, error) {
	var ris = map[string]RdmaIfInfo{}

	// list network interface macs
	macList, macErr := metadata.DefaultMetaClient.ListMacs()
	if macErr != nil {
		if scopedLog != nil {
			scopedLog.WithError(macErr).Errorf("list mac failed")
		} else {
			log.WithError(macErr).Errorf("list mac failed")
		}
		return ris, macErr
	}

	// check whether there is ERI
	for _, macAddress := range macList {
		vifFeatures, vifErr := metadata.DefaultMetaClient.GetVifFeatures(macAddress)
		if vifErr != nil {
			if scopedLog != nil {
				scopedLog.WithError(vifErr).Errorf("get mac %s vif features failed", macAddress)
			} else {
				log.WithError(vifErr).Errorf("get mac %s vif features failed", macAddress)
			}
			return nil, vifErr
		}
		if _, ok := roceVFs[vifFeatures]; ok {
			// Skip processing if ERI management is enabled and current is ERI interface
			// ERI interfaces will be handled separately through ERI management mechanism
			if option.Config.EnableERIManagement && vifFeatures == string(ccev2.ENIForERI) {
				if scopedLog != nil {
					scopedLog.WithField("macAddress", macAddress).Info("Skipping ERI interface due to ERI management enabled")
				}
				continue
			}

			var rdmaIfInfo RdmaIfInfo
			nodeInstanceID, metaAPIErr := metadata.DefaultMetaClient.GetInstanceID()
			if metaAPIErr != nil {
				if scopedLog != nil {
					scopedLog.WithError(metaAPIErr).Errorf("get node InstanceID from meta-data api failed")
				} else {
					log.WithError(macErr).Errorf("get node InstanceID from meta-data api failed")
				}
				return ris, metaAPIErr
			}
			netResourceSetName := generateNetResourceSetName(nodeName, nodeInstanceID, macAddress, vifFeatures)
			labelSelectorValue := generateRdmaNrsLabelSelectorValue(nodeName, nodeInstanceID, macAddress, vifFeatures)
			rdmaIfInfo.MacAddress = macAddress
			rdmaIfInfo.NetResourceSetName = netResourceSetName
			rdmaIfInfo.LabelSelectorValue = labelSelectorValue
			rdmaIfInfo.VifFeatures = vifFeatures
			ris[macAddress] = rdmaIfInfo
		}
	}

	return ris, nil
}

func GetRdmaIFsInfoAll(nodeName string, scopedLog *logrus.Entry) (map[string]RdmaIfInfo, error) {
	var ris = map[string]RdmaIfInfo{}

	// list network interface macs
	macList, macErr := metadata.DefaultMetaClient.ListMacs()
	if macErr != nil {
		if scopedLog != nil {
			scopedLog.WithError(macErr).Errorf("list mac failed")
		} else {
			log.WithError(macErr).Errorf("list mac failed")
		}
		return ris, macErr
	}

	// check whether there is ERI
	for _, macAddress := range macList {
		vifFeatures, vifErr := metadata.DefaultMetaClient.GetVifFeatures(macAddress)
		if vifErr != nil {
			if scopedLog != nil {
				scopedLog.WithError(vifErr).Errorf("get mac %s vif features failed", macAddress)
			} else {
				log.WithError(vifErr).Errorf("get mac %s vif features failed", macAddress)
			}
			return nil, vifErr
		}
		if _, ok := roceVFs[vifFeatures]; ok {
			var rdmaIfInfo RdmaIfInfo
			nodeInstanceID, metaAPIErr := metadata.DefaultMetaClient.GetInstanceID()
			if metaAPIErr != nil {
				if scopedLog != nil {
					scopedLog.WithError(metaAPIErr).Errorf("get node InstanceID from meta-data api failed")
				} else {
					log.WithError(macErr).Errorf("get node InstanceID from meta-data api failed")
				}
				return ris, metaAPIErr
			}
			netResourceSetName := generateNetResourceSetName(nodeName, nodeInstanceID, macAddress, vifFeatures)
			labelSelectorValue := generateRdmaNrsLabelSelectorValue(nodeName, nodeInstanceID, macAddress, vifFeatures)
			rdmaIfInfo.MacAddress = macAddress
			rdmaIfInfo.NetResourceSetName = netResourceSetName
			rdmaIfInfo.LabelSelectorValue = labelSelectorValue
			rdmaIfInfo.VifFeatures = vifFeatures
			ris[macAddress] = rdmaIfInfo
		}
	}

	return ris, nil
}

func IsCCERdmaEndpointName(cepName string) bool {
	return strings.Contains(cepName, rdmaEndpointMiddleSeparator)
}

func IsCCERdmaNetRourceSetName(netResourceSetName string) bool {
	underlayRDMA := strings.Replace(string(ccev2.ENIForHPC), "_", "", -1)
	overlayRDMA := strings.Replace(string(ccev2.ENIForERI), "_", "", -1)
	return strings.Contains(netResourceSetName, underlayRDMA) || strings.Contains(netResourceSetName, overlayRDMA)
}

func IsThisMasterMacCCERdmaEndpointName(cepName, masterMac string) bool {
	macAddress, err := getPrimaryMacFromCCERdmaEndpointName(cepName)
	if err != nil {
		return false
	}
	macStr1 := strings.Replace(masterMac, ":", "", -1)
	macStr2 := strings.Replace(macStr1, "-", "", -1)
	return macAddress == macStr2
}

func GetCEPNameFromPodName(isRdmaEndpointAllocator bool, podName, primaryMacAddress string) (name string) {
	if isRdmaEndpointAllocator {
		name = generateCCERdmaEndpointName(podName, primaryMacAddress)
	} else {
		name = podName
	}
	return
}

// Get PodName from Ethernet CCEEndpointName or RDMA CCEEndpointName
func GetPodNameFromCEPName(cepName string) (podName string) {
	index := strings.LastIndex(cepName, rdmaEndpointMiddleSeparator)
	if index == -1 {
		podName = cepName
	} else {
		podName = cepName[:index]
	}
	return podName
}

// agent is enable to manager current eni
// todo hmm
func IsAgentMgrENI(eni *ccev2.ENI) (enable bool) {
	enable = true
	switch eni.Spec.Type {
	case ccev2.ENIForBCC, ccev2.ENIForBBC, ccev2.ENIForEBC:
		if eni.Spec.UseMode == ccev2.ENIUseModeSecondaryIP && eni.Spec.Description != defaults.DefaultENIDescription {
			enable = false
		}
	default:
		enable = true
	}
	return enable
}

func IsSecondaryIPMode(eniUseMode string) bool {
	if eniUseMode == string(ccev2.ENIUseModeSecondaryIP) || eniUseMode == string(ccev2.ENIUseModePrimaryWithSecondaryIP) {
		return true
	}
	return false
}
