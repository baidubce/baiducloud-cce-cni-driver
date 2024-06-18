package bcesync

import (
	"context"
	"fmt"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

// remoteEniSyncher
type remoteERISyncher struct {
	*remoteVpcEniSyncher
}

var _ remoteEniSyncher = &remoteERISyncher{}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (es *remoteERISyncher) syncENI(ctx context.Context) (result []eni.Eni, err error) {
	scopedLog := log.WithField(taskLogField, eniControllerName)

	listArgs := enisdk.ListEniArgs{
		VpcId: es.VPCIDs,
		Name:  fmt.Sprintf("%s/", es.ClusterID),
	}
	enis, err := es.bceclient.ListERIs(context.TODO(), listArgs)
	if err != nil {
		scopedLog.WithField(taskLogField, eniControllerName).
			WithField("request", logfields.Json(listArgs)).
			WithError(err).Errorf("sync eri failed")
		return nil, err
	}

	for i := 0; i < len(enis); i++ {
		result = append(result, eni.Eni{Eni: enis[i]})
	}

	return result, nil
}

// statENI returns one ENI with the given name from bce cloud
func (es *remoteERISyncher) statENI(ctx context.Context, ENIID string) (*eni.Eni, error) {
	eniCache, err := es.bceclient.StatENI(ctx, ENIID)
	if err != nil {
		log.WithField(taskLogField, eniControllerName).
			WithField("ENIID", ENIID).
			WithContext(ctx).
			WithError(err).Errorf("stat eri failed")
		return nil, err
	}
	result := eni.Eni{Eni: *eniCache}
	es.syncManager.AddItems([]eni.Eni{result})
	return &result, nil
}

func (es *remoteERISyncher) useENIMachine() bool {
	return false
}
