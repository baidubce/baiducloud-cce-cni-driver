package agent

import (
	"fmt"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

var defaultMetaClient = metadata.NewClient()

// GetInstanceMetadata get basic metadata information
func GetInstanceMetadata() (vpcID, instanceType, instanceSpecType, availabilityZone string, err error) {
	vpcID, err = defaultMetaClient.GetVPCID()
	if err != nil {
		return
	}

	instanceTypeEX, err := defaultMetaClient.GetInstanceTypeEx()
	if err != nil {
		return
	}
	instanceType = string(instanceTypeEX)

	specType, err := defaultMetaClient.GetInstanceType()
	if err != nil {
		return
	}
	instanceSpecType = string(specType)

	availabilityZone, err = defaultMetaClient.GetAvailabilityZone()
	if err != nil {
		return
	}

	return vpcID, instanceType, instanceSpecType, availabilityZone, err
}

func GetInstanceType() (metadata.InstanceType, error) {
	return defaultMetaClient.GetInstanceType()
}

func GetInstanceTypeEx() (metadata.InstanceTypeEx, error) {
	return defaultMetaClient.GetInstanceTypeEx()
}

func getVpcIDByName() (string, error) {
	return defaultMetaClient.GetVPCID()
}

func GetInstanceID() (string, error) {
	return defaultMetaClient.GetInstanceID()
}

func GenerateENISpec() (eni *api.ENISpec, err error) {
	vpcID, instanceType, instanceSpecType, availabilityZone, err := GetInstanceMetadata()
	if err != nil {
		return
	}

	if option.Config.ENI == nil {
		err = fmt.Errorf("ENIConfig is nil")
		return
	}

	eni = option.Config.ENI.DeepCopy()
	eni.VpcID = vpcID
	eni.InstanceType = instanceType
	eni.AvailabilityZone = availabilityZone

	// ENI is supported default for bcc and ebc/ehc, but not supported for bbc
	// instanceType == "bbc" && instanceSpecType is in the same format as "ebc.l5c.c128m256.1d"
	// or "ehc.lgn5.c128m1024.8a100.8re.4d", set it to true to support ENI
	typeExStr := strings.TrimSpace(instanceType)
	if typeExStr == string(metadata.InstanceTypeExBBC) {
		typeStr := strings.TrimSpace(instanceSpecType)
		if strings.HasPrefix(typeStr, string(metadata.InstanceTypeExEBC)) {
			eni.InstanceType = string(metadata.InstanceTypeExEBC)
		} else if strings.HasPrefix(typeStr, string(metadata.InstanceTypeExEHC)) {
			eni.InstanceType = string(metadata.InstanceTypeExEHC)
		} else {
			eni.PreAllocateENI = 0
			eni.MaxAllocateENI = 0

			// set bbc primary eni subnet id
			var sbnID string
			sbnID, err = defaultMetaClient.GetSubnetID()
			if err != nil {
				return
			}
			eni.SubnetIDs = append(eni.SubnetIDs, sbnID)
		}
	}
	return
}
