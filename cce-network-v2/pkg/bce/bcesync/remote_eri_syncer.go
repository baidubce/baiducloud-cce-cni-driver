package bcesync

import (
	"context"
	"fmt"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

// eniGetter defines the interface for getting an ENI resource.
// This is used to allow mocking the Kubernetes client in tests.
type eniGetter interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*ccev2.ENI, error)
}

const (
	// maxRetryAttempts 最大重试次数
	maxRetryAttempts = 3
	// baseRetryDelay 基础重试延迟
	baseRetryDelay = 1 * time.Second
)

// remoteEniSyncher
type remoteERISyncher struct {
	*remoteVpcEniSyncher
	eniClient eniGetter
}

var _ remoteEniSyncher = &remoteERISyncher{}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (es *remoteERISyncher) syncENI(ctx context.Context) (result []eni.Eni, err error) {
	scopedLog := log.WithField(taskLogField, eniControllerName)

	listArgs := enisdk.ListEniArgs{
		VpcId: es.VPCIDs,
		Name:  fmt.Sprintf("%s/", es.ClusterID),
	}
	var enis []enisdk.Eni
	// 使用hpasclient的时候，返回的结果均为instance-id
	// 使用bceclient的时候，hpas机器返回hpas-id，其他返回instance-id
	if operatorOption.Config.HPASStanderd {
		enis, err = es.bceclient.ListERIs(context.TODO(), listArgs)
	} else {
		enis, err = es.hpasclient.ListERIs(context.TODO(), listArgs)
	}
	if err != nil {
		scopedLog.WithField(taskLogField, eniControllerName).
			WithField("request", logfields.Json(listArgs)).
			WithError(err).Errorf("sync eri failed")
		return nil, err
	}

	for i := 0; i < len(enis); i++ {
		result = append(result, eni.Eni{Eni: enis[i]})
		// 比对并更新 instance-id
		es.validateAndSyncERIInstanceID(&enis[i])
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

// validateAndSyncERIInstanceID 校对现有ERI的spec中的instance-id是否和IaaS的一致，如不一致则同步（整个过程置于冲突重试中）
func (es *remoteERISyncher) validateAndSyncERIInstanceID(iaasERI *enisdk.Eni) {
	scopeLog := eniLog.WithFields(logrus.Fields{
		"eriID": iaasERI.EniId,
		"vpcID": iaasERI.VpcId,
	})
	log.WithField("ERI", iaasERI.EniId).Debug("start to validate and sync ERI instance-id")

	// Lazy initialize the eniClient if it's not already set.
	// This allows tests to inject a mock client.
	if es.eniClient == nil {
		es.eniClient = k8s.CCEClient().CceV2().ENIs()
	}

	// 将整个获取与比对、更新的流程置于 RetryOnConflict 中
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// 获取最新版本的 ERI CR
		latest, err := es.eniClient.Get(context.TODO(), iaasERI.EniId, metav1.GetOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				scopeLog.WithError(err).Warning("ERI not found, skip sync")
				// 资源不存在，直接结束重试
				return nil
			}
			// 其他错误进入重试
			scopeLog.WithError(err).
				Warning("Failed to get ERI, retrying...")
			return err
		}

		// 比对 instance-id，一致则无需更新
		if latest.Spec.InstanceID == iaasERI.InstanceId {
			scopeLog.Debug("ERI instance-id is consistent")
			return nil
		}
		latest = latest.DeepCopy()
		scopeLog.WithFields(logrus.Fields{
			"k8sInstanceID":  latest.Spec.InstanceID,
			"iaasInstanceID": iaasERI.InstanceId,
		}).Warning("ERI instance-id inconsistency detected, syncing from IaaS")

		// 使用 IaaS 数据作为权威数据源更新 ERI CR
		latest.Spec.InstanceID = iaasERI.InstanceId
		if latest.Labels == nil {
			scopeLog.Debug("ERI labels is nil, create new one")
			latest.Labels = map[string]string{}
		}
		latest.Labels[k8s.LabelInstanceID] = iaasERI.InstanceId

		_, err = es.updater.Update(latest)
		return err
	}); err != nil {
		scopeLog.WithError(err).
			Error("Failed to sync ERI instance-id from IaaS with conflict retry")
	}
}
