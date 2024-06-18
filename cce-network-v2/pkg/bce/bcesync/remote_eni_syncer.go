package bcesync

import (
	"context"
	"fmt"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
)

type remoteVpcEniSyncher struct {
	updater     syncer.ENIUpdater
	bceclient   cloud.Interface
	syncManager *SyncManager[eni.Eni]

	VPCIDs    string
	ClusterID string

	eventRecorder record.EventRecorder
}

func (es *remoteVpcEniSyncher) setENIUpdater(updater syncer.ENIUpdater) {
	es.updater = updater
}

// statENI returns one ENI with the given name from bce cloud
func (es *remoteVpcEniSyncher) statENI(ctx context.Context, ENIID string) (*eni.Eni, error) {
	eniCache, err := es.bceclient.StatENI(ctx, ENIID)
	if err != nil {
		log.WithField(taskLogField, eniControllerName).
			WithField("ENIID", ENIID).
			WithContext(ctx).
			WithError(err).Errorf("stat eni failed")
		return nil, err
	}
	result := eni.Eni{Eni: *eniCache}
	es.syncManager.AddItems([]eni.Eni{result})
	return &result, nil
}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (es *remoteVpcEniSyncher) syncENI(ctx context.Context) (result []eni.Eni, err error) {
	listArgs := enisdk.ListEniArgs{
		VpcId: es.VPCIDs,
		Name:  fmt.Sprintf("%s/", es.ClusterID),
	}
	enis, err := es.bceclient.ListENIs(context.TODO(), listArgs)
	if err != nil {
		log.WithField(taskLogField, eniControllerName).
			WithField("request", logfields.Json(listArgs)).
			WithError(err).Errorf("sync eni failed")
		return result, err
	}

	for i := 0; i < len(enis); i++ {
		result = append(result, eni.Eni{Eni: enis[i]})
		es.createExternalENI(&enis[i])
	}
	return
}

func (es *remoteVpcEniSyncher) useENIMachine() bool {
	return true
}

// eni is not created on cce, we should create it?
// If this ENI is missing, CCE will continue to try to create new ENIs.
// This will result in the inability to properly identify the capacity
// of the ENI
func (es *remoteVpcEniSyncher) createExternalENI(eni *enisdk.Eni) {
	old, err := es.updater.Lister().Get(eni.EniId)
	if err != nil && !kerrors.IsNotFound(err) {
		return
	}
	if old != nil {
		return
	}

	if eni.Status == string(ccev2.VPCENIStatusDetaching) || eni.Status == string(ccev2.VPCENIStatusDeleted) {
		return
	}
	scopeLog := eniLog.WithFields(logrus.Fields{
		"eniID":      eni.EniId,
		"vpcID":      eni.VpcId,
		"eniName":    eni.Name,
		"instanceID": eni.InstanceId,
		"status":     eni.Status,
	})

	// find node by instanceID
	nrsList, err := watchers.NetResourceSetClient.GetByInstanceID(eni.InstanceId)

	if len(nrsList) == 0 {
		return
	}
	resource := nrsList[0]
	scopeLog = scopeLog.WithField("nodeName", resource.Name)
	scopeLog.Debugf("find node by instanceID success")
	scopeLog.Infof("start to create external eni")

	newENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: eni.EniId,
			Labels: map[string]string{
				k8s.LabelInstanceID: eni.InstanceId,
				k8s.LabelNodeName:   resource.Name,
			},
			Annotations: map[string]string{
				k8s.AnnotationExternalENI: eni.CreatedTime,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: ccev2.SchemeGroupVersion.String(),
				Kind:       ccev2.NRSKindDefinition,
				Name:       resource.Name,
				UID:        resource.UID,
			}},
		},
		Spec: ccev2.ENISpec{
			NodeName: resource.Name,
			UseMode:  ccev2.ENIUseMode(resource.Spec.ENI.UseMode),
			ENI: models.ENI{
				ID:                         eni.EniId,
				Name:                       eni.Name,
				ZoneName:                   eni.ZoneName,
				InstanceID:                 eni.InstanceId,
				VpcID:                      eni.VpcId,
				SubnetID:                   eni.SubnetId,
				SecurityGroupIds:           eni.SecurityGroupIds,
				EnterpriseSecurityGroupIds: eni.EnterpriseSecurityGroupIds,
			},
			Type:                      ccev2.ENIForBCC,
			RouteTableOffset:          resource.Spec.ENI.RouteTableOffset,
			InstallSourceBasedRouting: resource.Spec.ENI.InstallSourceBasedRouting,
		},
	}
	_, err = es.updater.Create(newENI)
	if err != nil {
		es.eventRecorder.Eventf(resource, corev1.EventTypeWarning, "FailedCreateExternalENI", "failed to create external ENI on nrs %s: %s", resource.Name, err)
		return
	}
	es.eventRecorder.Eventf(resource, corev1.EventTypeNormal, "CreateExternalENISuccess", "create external ENI %s on nrs %s success", eni.EniId, resource.Name)
}
