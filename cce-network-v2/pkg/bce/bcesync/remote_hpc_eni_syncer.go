package bcesync

import (
	"context"
	"errors"
	"fmt"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
)

type remoteHPCSyncher struct {
	updater     syncer.ENIUpdater
	bceclient   cloud.Interface
	syncManager *SyncManager[eni.Eni]
}

var _ remoteEniSyncher = &remoteHPCSyncher{}

func (es *remoteHPCSyncher) setENIUpdater(updater syncer.ENIUpdater) {
	es.updater = updater
}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (es *remoteHPCSyncher) syncENI(ctx context.Context) (result []eni.Eni, err error) {
	scopedLog := log.WithField(taskLogField, eniControllerName)
	label := labels.Set(map[string]string{k8s.LabelENIType: string(ccev2.ENIForHPC)})

	k8senis, err := es.updater.Lister().List(label.AsSelector())
	if err != nil {
		scopedLog.WithError(err).Errorf("list k8s hpc eni failed")
		return result, err
	}

	instanceIDs := sets.NewString()
	for _, k8seni := range k8senis {
		nrsList, err := watchers.NetResourceSetClient.GetByInstanceID(k8seni.Spec.InstanceID)
		if err != nil || len(nrsList) == 0 {
			continue
		}
		instanceIDs.Insert(k8seni.Spec.InstanceID)
	}

	for _, instanceID := range instanceIDs.List() {
		eniList, err := es.statsEnisByInstanceID(context.TODO(), instanceID)
		if err != nil {
			scopedLog.WithFields(logrus.Fields{"instanceID": instanceID}).WithError(err).Errorf("get hpc instance ENIs failed")
			continue
		}
		result = append(result, eniList...)
	}
	return result, nil
}

func (es *remoteHPCSyncher) statsEnisByInstanceID(ctx context.Context, instanceID string) ([]eni.Eni, error) {
	var (
		result []eni.Eni
	)

	// refresh hpc eni cache
	hpcEniList, err := es.bceclient.GetHPCEniID(ctx, instanceID)
	if err != nil {
		if err != nil {
			return nil, fmt.Errorf("failed to get hpc instance ENI %w", err)
		}
	}

	for _, hpceni := range hpcEniList.Result {
		if hpceni.MacAddress == "" {
			return nil, errors.New("vpc mac address is empty")
		}
		trancelateENI := eni.Eni{
			Eni: enisdk.Eni{
				EniId:       hpceni.EniID,
				Name:        hpceni.Name,
				Description: hpceni.Description,
				InstanceId:  instanceID,
				MacAddress:  hpceni.MacAddress,
				Status:      hpceni.Status,
			},
		}
		for _, ips := range hpceni.PrivateIPSet {
			trancelateENI.PrivateIpSet = append(trancelateENI.PrivateIpSet, enisdk.PrivateIp{
				PrivateIpAddress: ips.PrivateIPAddress,
				Primary:          ips.Primary,
			})
		}
		result = append(result, trancelateENI)
	}
	es.syncManager.AddItems(result)

	return result, nil
}

// statENI returns one ENI with the given name from bce cloud
func (es *remoteHPCSyncher) statENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	k8seni, err := es.updater.Lister().Get(eniID)
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s hpc eni %w", err)
	}
	result, err := es.statsEnisByInstanceID(ctx, k8seni.Spec.ENI.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hpc instance ENI %w", err)
	}

	for _, eni := range result {
		if eni.EniId == eniID {
			return &eni, nil
		}
	}

	return nil, fmt.Errorf("failed to get hpc instance ENI with eniID: %s", eniID)
}

func (es *remoteHPCSyncher) useENIMachine() bool {
	return false
}
