package bcesync

import (
	"context"
	"errors"
	"fmt"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
)

type remoteBBCPrimarySyncher struct {
	updater     syncer.ENIUpdater
	bceclient   cloud.Interface
	syncManager *SyncManager[eni.Eni]
}

var _ remoteEniSyncher = &remoteBBCPrimarySyncher{}

func (es *remoteBBCPrimarySyncher) setENIUpdater(updater syncer.ENIUpdater) {
	es.updater = updater
}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (es *remoteBBCPrimarySyncher) syncENI(ctx context.Context) (result []eni.Eni, err error) {
	scopedLog := log.WithField(taskLogField, eniControllerName)
	label := labels.Set(map[string]string{k8s.LabelENIType: string(ccev2.ENIForBBC)})

	k8senis, err := es.updater.Lister().List(label.AsSelector())
	if err != nil {
		scopedLog.WithError(err).Errorf("list k8s primary bbc eni failed")
		return result, err
	}

	for _, k8seni := range k8senis {
		eniresult, err := es.statENI(ctx, k8seni.Spec.ID)
		if err != nil {
			scopedLog.WithFields(logrus.Fields{
				"eniID":      k8seni.Name,
				"instanceID": k8seni.Spec.ENI.InstanceID,
				"node":       k8seni.Spec.NodeName,
			}).
				WithError(err).Errorf("stat bbc primary eni failed")
			continue
		}

		result = append(result, *eniresult)
	}
	return result, nil
}

// statENI returns one ENI with the given name from bce cloud
func (es *remoteBBCPrimarySyncher) statENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	k8seni, err := es.updater.Lister().Get(eniID)
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s bbc eni %w", err)
	}
	intanceID := k8seni.Spec.ENI.InstanceID
	var result []eni.Eni
	eniResult, err := es.bceclient.GetBBCInstanceENI(ctx, intanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bbc instance ENI %w", err)
	}
	if eniResult.MacAddress == "" {
		return nil, errors.New("vpc mac address is empty")
	}
	trancelateENI := eni.Eni{
		Eni: enisdk.Eni{
			EniId:       eniResult.Id,
			Name:        eniResult.Name,
			ZoneName:    eniResult.ZoneName,
			Description: eniResult.Description,
			InstanceId:  eniResult.InstanceId,
			MacAddress:  eniResult.MacAddress,
			VpcId:       eniResult.VpcId,
			SubnetId:    eniResult.SubnetId,
			Status:      eniResult.Status,
		},
	}
	for _, ips := range eniResult.PrivateIpSet {
		trancelateENI.PrivateIpSet = append(trancelateENI.PrivateIpSet, enisdk.PrivateIp{
			PrivateIpAddress: ips.PrivateIpAddress,
			Primary:          ips.Primary,
			PublicIpAddress:  ips.PublicIpAddress,
		})
		if ips.Ipv6Address != "" {
			trancelateENI.Ipv6PrivateIpSet = append(trancelateENI.Ipv6PrivateIpSet, enisdk.PrivateIp{
				PrivateIpAddress: ips.Ipv6Address,
				Primary:          ips.Primary,
			})
		}
	}
	result = append(result, trancelateENI)
	es.syncManager.AddItems(result)
	return &trancelateENI, nil
}

func (es *remoteBBCPrimarySyncher) useENIMachine() bool {
	return false
}
