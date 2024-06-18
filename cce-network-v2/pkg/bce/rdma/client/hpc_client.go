package client

import (
	"context"
	"fmt"

	bceclound "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/hpc"
)

type HpcClient struct {
	cloud bceclound.Interface
}

func NewHpcClient(cloud bceclound.Interface) *HpcClient {
	return &HpcClient{
		cloud: cloud,
	}
}

func (c *HpcClient) ListEnis(ctx context.Context, _, instanceID string) ([]EniResult, error) {
	hpcEniList, err := c.cloud.GetHPCEniID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	resultList := make([]EniResult, 0)
	for i := range hpcEniList.Result {
		hpcEni := hpcEniList.Result[i]

		ips := make([]PrivateIP, 0, len(hpcEni.PrivateIPSet))
		for j := range hpcEni.PrivateIPSet {
			hpcIP := hpcEni.PrivateIPSet[j]
			ips = append(ips, PrivateIP{
				Primary:          hpcIP.Primary,
				PrivateIpAddress: hpcIP.PrivateIPAddress,
			})
		}

		resultList = append(resultList, EniResult{
			Type:         c.GetRDMAIntType(),
			Id:           hpcEni.EniID,
			MacAddress:   hpcEni.MacAddress,
			PrivateIpSet: ips,
		})
	}
	return resultList, nil
}

func (c *HpcClient) AddPrivateIP(ctx context.Context, eniID, _ string) (string, error) {
	log.Infof("start to add hpc privateIP of eniID: %v", eniID)
	args := &hpc.EniBatchPrivateIPArgs{
		EniID:                 eniID,
		PrivateIPAddressCount: 1, // 当前指定一个
	}
	hpcEni, err := c.cloud.BatchAddHpcEniPrivateIP(ctx, args)
	if err != nil {
		return "", err
	}
	if len(hpcEni.PrivateIPAddresses) == 0 {
		return "", fmt.Errorf("failed to batch add privateIp in %s hpcEni", eniID)
	}
	log.Infof("batch add HpcEni privateIp is %v", hpcEni.PrivateIPAddresses)

	return hpcEni.PrivateIPAddresses[0], nil
}

func (c *HpcClient) DeletePrivateIP(ctx context.Context, eniID, privateIP string) error {
	log.Infof("start to delete hpc privateIP from %s eniID and %s ip.", eniID, privateIP)
	args := &hpc.EniBatchDeleteIPArgs{
		EniID:              eniID,
		PrivateIPAddresses: []string{privateIP},
	}
	log.Infof("delete hpc privateIP is %v", args)

	err := c.cloud.BatchDeleteHpcEniPrivateIP(ctx, args)
	if err != nil {
		log.Errorf("failed to delete hpc private ip: %v", err)
		return err
	}
	return nil
}

func (c *HpcClient) BatchAddPrivateIP(ctx context.Context, eniID string, privateIPs []string, count int) ([]string, error) {
	log.Infof("start to batch add hpc privateIP of eniID: %v", eniID)
	args := &hpc.EniBatchPrivateIPArgs{
		EniID:                 eniID,
		PrivateIPAddressCount: count,
	}
	hpcEni, err := c.cloud.BatchAddHpcEniPrivateIP(ctx, args)
	if err != nil {
		return hpcEni.PrivateIPAddresses, err
	}
	if len(hpcEni.PrivateIPAddresses) == 0 {
		return hpcEni.PrivateIPAddresses, fmt.Errorf("failed to batch add private ips in %s hpcEni", eniID)
	}
	log.Infof("batch add HpcEni private ips are %v", hpcEni.PrivateIPAddresses)

	return hpcEni.PrivateIPAddresses, nil
}

func (c *HpcClient) BatchDeletePrivateIP(ctx context.Context, eniID string, privateIPs []string) error {
	log.Infof("start to batch delete hpc privateIP of eniID: %v", eniID)
	args := &hpc.EniBatchDeleteIPArgs{
		EniID:              eniID,
		PrivateIPAddresses: privateIPs,
	}
	log.Infof("batch delete hpc private ips are %v", args)

	err := c.cloud.BatchDeleteHpcEniPrivateIP(ctx, args)
	if err != nil {
		log.Errorf("failed to batch delete hpc private ips: %v", err)
		return err
	}
	return nil
}

func (c *HpcClient) GetRDMAIntType() string {
	return IntTypeHPC
}
