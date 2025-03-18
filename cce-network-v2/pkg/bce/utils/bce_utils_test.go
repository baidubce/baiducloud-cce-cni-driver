package utils

import (
	"encoding/json"
	"fmt"
	"testing"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/stretchr/testify/assert"
)

// test IsAgentMgrENI,eg bcc/bbc/ebc/edma eni
func TestIsAgentMgrENI(t *testing.T) {
	{
		// rdma secondary eni can be managered by agent
		rdmaENI := new(ccev2.ENI)
		rdmaENISpecJson := `{
			"id": "eni-x8p7yanyj6du",
			"instanceID": "i-XTI7e9fM",
			"macAddress": "b8:3f:d2:04:1b:90",
			"name": "eni-x8p7yanyj6du",
			"nodeName": "cce-rdma-test-test-test-test-test-test-test-test1-b83fd2041b90-rdmaroce",
			"routeTableOffset": 0,
			"type": "rdma_roce",
			"useMode": "Secondary",
			"vpcID": "vpc-ggypsz662xdi"
		}`
		var rdmaENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(rdmaENISpecJson), &rdmaENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		rdmaENI.Spec = rdmaENISpec
		fmt.Printf("rdma: %s %s\n", rdmaENI.Spec.Description, rdmaENI.Spec.ID)
		assert.Equal(t, rdmaENI.Spec.Description, "")
		assert.True(t, IsAgentMgrENI(rdmaENI))
	}
	{
		// bbc primary eni can be managered by agent
		bbcPrimaryENI := new(ccev2.ENI)
		bbcPrimaryENISpecJson := `{
			"routeTableOffset": 127,
			"subnetID": "sbn-49qjy0pk2e8r",
			"type": "bbc",
			"useMode": "PrimaryWithSecondaryIP",
			"vpcID": "vpc-t686xe1isp02",
			"vpcVersion": 1,
			"zoneName": "cn-bj-d"
		}`
		var bbcPrimaryENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(bbcPrimaryENISpecJson), &bbcPrimaryENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		bbcPrimaryENI.Spec = bbcPrimaryENISpec
		assert.Equal(t, bbcPrimaryENI.Spec.Description, "")
		assert.True(t, IsAgentMgrENI(bbcPrimaryENI))
	}

	{
		// bcc sencondary eni can be managered by agent
		bccENI := new(ccev2.ENI)
		bccENISpecJson := `{
			"description": "auto created by cce-cni, do not modify",
			"id": "eni-1j44nye7pqux",
			"installSourceBasedRouting": true,
			"instanceID": "i-8E7pHkPa",
			"macAddress": "fa:20:20:31:a4:da",
			"name": "cce-cqa5ku5o/i-8E7pHkPa/cce-ig-x4ysdmuq-75469f7e-160-05/30c144",
			"nodeName": "cce-ig-x4ysdmuq-75469f7e-160-05",
			"securityGroupIds": [
				"g-acxm5je3u8s7"
			],
			"subnetID": "sbn-rin3bhse568z",
			"type": "bcc",
			"useMode": "Secondary",
			"vpcID": "vpc-xxht933t76fq",
			"zoneName": "cn-bj-d"
		}`
		var bccENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(bccENISpecJson), &bccENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		bccENI.Spec = bccENISpec
		fmt.Printf("bccENI.Spec.Description: %s\n", bccENI.Spec.Description)
		assert.Equal(t, bccENI.Spec.Description, "auto created by cce-cni, do not modify")
		assert.True(t, IsAgentMgrENI(bccENI))
	}
	{
		// bcc sencondary eni without description can be managered by agent
		bccENI := new(ccev2.ENI)
		bccENISpecJson := `{
			"id": "eni-1j44nye7pqux",
			"installSourceBasedRouting": true,
			"instanceID": "i-8E7pHkPa",
			"macAddress": "fa:20:20:31:a4:da",
			"name": "cce-cqa5ku5o/i-8E7pHkPa/cce-ig-x4ysdmuq-75469f7e-160-05/30c144",
			"nodeName": "cce-ig-x4ysdmuq-75469f7e-160-05",
			"privateIPSet": [
				{
					"primary": true,
					"privateIPAddress": "10.194.165.34",
					"subnetID": "sbn-rin3bhse568z"
				}
			],
			"routeTableOffset": 127,
			"securityGroupIds": [
				"g-acxm5je3u8s7"
			],
			"subnetID": "sbn-rin3bhse568z",
			"type": "bcc",
			"useMode": "Secondary",
			"vpcID": "vpc-xxht933t76fq",
			"zoneName": "cn-bj-d"
		}`
		var bccENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(bccENISpecJson), &bccENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		bccENI.Spec = bccENISpec
		assert.Equal(t, bccENI.Spec.Description, "")
		assert.False(t, IsAgentMgrENI(bccENI))
	}
	{
		// ebc sencondary eni with description can be managered by agent
		ebcENI := new(ccev2.ENI)
		ebcENISpecJson := `{
			"description": "auto created by cce-cni, do not modify",
			"type": "ebc",
			"useMode": "Secondary"
		}`
		var ebcENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(ebcENISpecJson), &ebcENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		ebcENI.Spec = ebcENISpec
		assert.Equal(t, ebcENI.Spec.Description, "auto created by cce-cni, do not modify")
		assert.True(t, IsAgentMgrENI(ebcENI))
	}
	{
		// ebc sencondary eni without description can't be managered by agent
		ebcENI := new(ccev2.ENI)
		ebcENISpecJson := `{
			"type": "ebc",
			"useMode": "Secondary"
		}`
		var ebcENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(ebcENISpecJson), &ebcENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		ebcENI.Spec = ebcENISpec
		assert.Equal(t, ebcENI.Spec.Description, "")
		assert.False(t, IsAgentMgrENI(ebcENI))
	}
	{
		// ebc primary eni with description can be managered by agent
		ebcENI := new(ccev2.ENI)
		ebcENISpecJson := `{
			"type": "ebc",
			"useMode": "PrimaryWithSecondaryIP"
		}`
		var ebcENISpec ccev2.ENISpec
		err := json.Unmarshal([]byte(ebcENISpecJson), &ebcENISpec)
		if err != nil {
			fmt.Println(err)
			return
		}
		ebcENI.Spec = ebcENISpec
		assert.Equal(t, ebcENI.Spec.Description, "")
		assert.True(t, IsAgentMgrENI(ebcENI))
	}
}
