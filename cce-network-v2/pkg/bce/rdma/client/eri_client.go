package client

import (
	"context"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	bceclound "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
)

type EriClient struct {
	cloud     bceclound.Interface
	hpasCloud bceclound.Interface
}

func NewEriClient(cloud, hpasCloud bceclound.Interface) *EriClient {
	return &EriClient{
		cloud:     cloud,
		hpasCloud: hpasCloud,
	}
}

func (c *EriClient) ListEnis(ctx context.Context, vpcID, instanceID string) ([]EniResult, error) {
	listArgs := enisdk.ListEniArgs{
		InstanceId: instanceID,
		VpcId:      vpcID,
	}
	var eriList []enisdk.Eni
	var listErr error

	instanceType, err := utils.GetNrsTypeByInstanceID(instanceID)
	if err != nil {
		return nil, err
	}
	if instanceType == string(metadata.InstanceTypeExHPAS) && !operatorOption.Config.HPASStanderd {
		// 开关为非标准 hpas 接口使用方式，并且实例为 hpas 时，走带 header 的接口
		eriList, listErr = c.hpasCloud.ListERIs(ctx, listArgs)
	} else {
		eriList, listErr = c.cloud.ListERIs(ctx, listArgs)
	}

	if listErr != nil {
		log.Errorf("failed to get eri: %v", listErr)
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
				PrivateIpAddress: eniIP.PrivateIpAddress,
			})
		}

		resultList = append(resultList, EniResult{
			Type:         c.GetRDMAIntType(),
			Id:           eniInfo.EniId,
			MacAddress:   eniInfo.MacAddress,
			VpcID:        eniInfo.VpcId,
			SubnetID:     eniInfo.SubnetId,
			ZoneName:     eniInfo.ZoneName,
			PrivateIpSet: ips,
		})
	}
	return resultList, nil
}

func (c *EriClient) AddPrivateIP(ctx context.Context, eniID, privateIP string) (string, error) {
	return c.cloud.AddPrivateIP(ctx, privateIP, eniID, false)
}

func (c *EriClient) DeletePrivateIP(ctx context.Context, eniID string, privateIP string) error {
	return c.cloud.DeletePrivateIP(ctx, privateIP, eniID, false)
}

func (c *EriClient) BatchAddPrivateIP(ctx context.Context, eniID string, privateIPs []string, count int) ([]string, error) {
	return c.cloud.BatchAddPrivateIP(ctx, privateIPs, count, eniID, false)
}

func (c *EriClient) BatchDeletePrivateIP(ctx context.Context, eniID string, privateIPs []string) error {
	return c.cloud.BatchDeletePrivateIP(ctx, privateIPs, eniID, false)
}

func (c *EriClient) GetRDMAIntType() string {
	return IntTypeERI
}
