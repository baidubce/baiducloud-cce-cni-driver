package client

import (
	"context"

	bceclound "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
)

type EriClient struct {
	cloud bceclound.Interface
}

func NewEriClient(cloud bceclound.Interface) *EriClient {
	return &EriClient{
		cloud: cloud,
	}
}

func (c *EriClient) ListEnis(ctx context.Context, vpcID, instanceID string) ([]EniResult, error) {
	listArgs := enisdk.ListEniArgs{
		InstanceId: instanceID,
		VpcId:      vpcID,
	}
	eriList, listErr := c.cloud.ListERIs(ctx, listArgs)
	if listErr != nil {
		log.Errorf(ctx, "failed to get eri: %v", listErr)
		return nil, listErr
	}

	resultList := make([]EniResult, 0)
	for i := range eriList {
		eniInfo := eriList[i]

		ips := make([]PrivateIP, 0, len(eniInfo.PrivateIpSet))
		for j := range eniInfo.PrivateIpSet {
			eniIP := eniInfo.PrivateIpSet[j]
			ips = append(ips, PrivateIP{
				Primary:          eniIP.Primary,
				PrivateIPAddress: eniIP.PrivateIpAddress,
			})
		}

		resultList = append(resultList, EniResult{
			EniID:        eniInfo.EniId,
			MacAddress:   eniInfo.MacAddress,
			PrivateIPSet: ips,
		})
	}
	return resultList, nil
}

func (c *EriClient) AddPrivateIP(ctx context.Context, eniID, privateIP string) (string, error) {
	return c.cloud.AddPrivateIP(ctx, privateIP, eniID)
}

func (c *EriClient) DeletePrivateIP(ctx context.Context, eniID string, privateIP string) error {
	return c.cloud.DeletePrivateIP(ctx, privateIP, eniID)
}

func (c *EriClient) GetMwepType() string {
	return ipamgeneric.MwepTypeERI
}
