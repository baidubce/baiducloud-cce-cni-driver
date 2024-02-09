package client

import (
	"context"
	"fmt"

	bceclound "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

type RoCEClient struct {
	cloud bceclound.Interface
}

func NewRoCEClient(cloud bceclound.Interface) *RoCEClient {
	return &RoCEClient{
		cloud: cloud,
	}
}

func (c *RoCEClient) ListEnis(ctx context.Context, _, instanceID string) ([]EniResult, error) {
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
				PrivateIPAddress: hpcIP.PrivateIPAddress,
			})
		}

		resultList = append(resultList, EniResult{
			EniID:        hpcEni.EniID,
			MacAddress:   hpcEni.MacAddress,
			PrivateIPSet: ips,
		})
	}
	return resultList, nil
}

func (c *RoCEClient) AddPrivateIP(ctx context.Context, eniID, _ string) (string, error) {
	log.Infof(ctx, "start to add hpc privateIP of eniID: %v", eniID)
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
	log.Infof(ctx, "batch add HpcEni privateIp is %v", hpcEni.PrivateIPAddresses)

	return hpcEni.PrivateIPAddresses[0], nil
}

func (c *RoCEClient) DeletePrivateIP(ctx context.Context, eniID, privateIP string) error {
	log.Infof(ctx, "start to delete roce privateIP from %s eniID and %s ip.", eniID, privateIP)
	args := &hpc.EniBatchDeleteIPArgs{
		EniID:              eniID,
		PrivateIPAddresses: []string{privateIP},
	}
	log.Infof(ctx, "delete hpc privateIP is %v", args)

	err := c.cloud.BatchDeleteHpcEniPrivateIP(ctx, args)
	if err != nil {
		log.Errorf(ctx, "failed to delete hpc private ip: %v", err)
		return err
	}
	return nil
}

func (c *RoCEClient) GetMwepType() string {
	return ipamgeneric.MwepTypeRoce
}
