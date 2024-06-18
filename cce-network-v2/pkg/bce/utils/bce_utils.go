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

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

const (
	UnderlayRDMA                string = string(ccev2.ENIForHPC) // Underlay RDMA (HPC)
	OverlayRDMA                 string = string(ccev2.ENIForERI) // Overlay RDMA (ERI)
	PodResourceName             string = "rdma"
	rdmaEndpointMiddleSeparator string = "-rdma-"
)

var (
	log = logging.NewSubysLogger("bceutils")

	roceVFs = map[string]struct{}{
		OverlayRDMA:  {},
		UnderlayRDMA: {},
	}
)

type RdmaIfInfo struct {
	MacAddress         string
	NetResourceSetName string
	VifFeatures        string
}

func generateNetResourceSetName(nodeName, macAddress, vifFeatures string) (netResourceSetName string) {
	macStr1 := strings.Replace(macAddress, ":", "", -1)
	macStr2 := strings.Replace(macStr1, "-", "", -1)
	vifFeaturesStr := strings.Replace(vifFeatures, "_", "", -1)
	netResourceSetName = fmt.Sprintf("%s-%s-%s", nodeName, macStr2, vifFeaturesStr)
	// 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
	return netResourceSetName
}

func getNodeNameFromRdmaNetResourceSetName(netResourceSetName, rdmaTag string) (nodeName string) {
	// 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
	index1 := strings.Index(netResourceSetName, rdmaTag)
	nodeNameMacStr := netResourceSetName[:index1-1]
	index2 := strings.LastIndex(nodeNameMacStr, "-")
	nodeName = nodeNameMacStr[:index2]
	return nodeName
}

func GetNodeNameFromNetResourceSetName(netResourceSetName string) (nodeName string) {
	// 10.0.2.2-fa2700078302-rdmaroce for HPC or 10.0.2.2-fa2700078302-elasticrdma for ERI
	underlayRDMA := strings.Replace(string(ccev2.ENIForHPC), "_", "", -1)
	overlayRDMA := strings.Replace(string(ccev2.ENIForERI), "_", "", -1)
	if strings.Contains(netResourceSetName, underlayRDMA) {
		nodeName = getNodeNameFromRdmaNetResourceSetName(netResourceSetName, underlayRDMA)
	} else if strings.Contains(netResourceSetName, overlayRDMA) {
		nodeName = getNodeNameFromRdmaNetResourceSetName(netResourceSetName, overlayRDMA)
	} else {
		nodeName = netResourceSetName
	}

	return nodeName
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
	var defaultMetaClient = metadata.NewClient()

	// list network interface macs
	macList, macErr := defaultMetaClient.ListMacs()
	if macErr != nil {
		if scopedLog != nil {
			scopedLog.WithError(macErr).Errorf("list mac failed")
		}
		return ris, macErr
	}

	// check whether there is ERI
	for _, macAddress := range macList {
		vifFeatures, vifErr := defaultMetaClient.GetVifFeatures(macAddress)
		if vifErr != nil {
			if scopedLog != nil {
				scopedLog.WithError(vifErr).Errorf("get mac %s vif features failed", macAddress)
			}
			continue
		}
		if _, ok := roceVFs[vifFeatures]; ok {
			var rdmaIfInfo RdmaIfInfo
			rdmaIfInfo.MacAddress = macAddress
			rdmaIfInfo.NetResourceSetName = generateNetResourceSetName(nodeName, macAddress, vifFeatures)
			rdmaIfInfo.VifFeatures = vifFeatures
			ris[macAddress] = rdmaIfInfo
		}
	}

	return ris, nil
}

func IsCCERdmaEndpointName(cepName string) bool {
	return strings.Contains(cepName, rdmaEndpointMiddleSeparator)
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
