package bcesync

import (
	"context"
	"fmt"
	"strings"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
)

type remoteVpcEniSyncher struct {
	updater     syncer.ENIUpdater
	bceclient   cloud.Interface
	hpasclient  cloud.Interface
	syncManager *SyncManager[eni.Eni]

	VPCIDs    string
	ClusterID string

	eventRecorder record.EventRecorder
}

func (es *remoteVpcEniSyncher) setENIUpdater(updater syncer.ENIUpdater) {
	es.updater = updater
}

// getClientForENI 根据ENI ID获取对应的客户端类型
func (es *remoteVpcEniSyncher) getClientForENI(ctx context.Context, eniID string) cloud.Interface {
	// 首先尝试通过ENI ID获取ENI资源
	eniResource, err := es.updater.Lister().Get(eniID)
	if err != nil {
		// 如果获取ENI资源失败，使用默认的bceclient
		log.WithField(taskLogField, eniControllerName).
			WithField("eniID", eniID).
			WithError(err).
			Warnf("failed to get ENI resource for eniID %s, using default client", eniID)
		return es.bceclient
	}

	// 通过ENI的InstanceID获取对应的NetResourceSet
	nrsType, err := bceutils.GetNrsTypeByInstanceID(eniResource.Spec.ENI.InstanceID)
	if err != nil {
		log.WithField(taskLogField, eniControllerName).
			WithField("eniID", eniID).
			WithField("instanceID", eniResource.Spec.ENI.InstanceID).
			WithError(err).
			Warnf("failed to get NRS type for instanceID %s, using default client", eniResource.Spec.ENI.InstanceID)
		return es.bceclient
	}

	if nrsType == string(metadata.InstanceTypeExHPAS) {
		return es.hpasclient
	}

	// 默认使用bceclient
	return es.bceclient
}

// statENI returns one ENI with the given name from bce cloud
func (es *remoteVpcEniSyncher) statENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	client := es.getClientForENI(ctx, eniID)
	eniCache, err := client.StatENI(ctx, eniID)
	if err != nil {
		log.WithField(taskLogField, eniControllerName).
			WithField("eniID", eniID).
			WithContext(ctx).
			WithError(err).Errorf("stat eni failed")
		return nil, err
	}
	scopeLog := eniLog.WithFields(logrus.Fields{
		taskLogField: eniControllerName,
		eniID:        eniID,
	})
	if eniCache == nil {
		scopeLog.Debug("stat eni not found")
		return nil, fmt.Errorf("%s not found", eniID)
	}
	scopeLog.Debug("stat eni success")
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
	scopedLog := log.WithField(taskLogField, eniControllerName).
		WithField("request", logfields.Json(listArgs))

	// list eni from bce cloud, do not contain the eni which is underlay RDMA (HPC) or overlay RDMA (ERI)
	// the underlay RDMA can be get by GetHPCEniID(ctx context.Context, instanceID string) (*hpc.EniList, error)
	// the overlay RDMA can be get by ListERIs(ctx context.Context, args eni.ListEniArgs) ([]eni.Eni, error)
	var enis []enisdk.Eni
	// 使用hpasclient的时候，返回的结果均为instance-id，
	// 使用bceclient的时候，hpas类型的机器返回hpas-id，其他返回instance-id
	if operatorOption.Config.HPASStanderd {
		enis, err = es.bceclient.ListENIs(context.TODO(), listArgs)
	} else {
		enis, err = es.hpasclient.ListENIs(context.TODO(), listArgs)
	}
	// enis, err := es.bceclient.ListENIs(context.TODO(), listArgs)
	if err != nil {
		scopedLog.WithError(err).Errorf("sync eni failed")
		return result, err
	}

	for i := 0; i < len(enis); i++ {
		result = append(result, eni.Eni{Eni: enis[i]})
		isExisted, objEni := es.createExternalENI(&enis[i])
		if !isExisted && objEni != nil {
			// refresh the status of ENI
			if objEni.Status.VPCStatus != ccev2.VPCENIStatus(enis[i].Status) {
				(&objEni.Status).AppendVPCStatus(ccev2.VPCENIStatus(enis[i].Status))
				_, err = es.updater.UpdateStatus(objEni)
				if err != nil {
					scopedLog.WithError(err).Error("update eni status")
					return
				}
			}
		}
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
// returns:
//
//	isExisted: true: eni object is existed, false: eni object is not existed.
//	result: the ENI object if it is created or existed, nil otherwise.
func (es *remoteVpcEniSyncher) createExternalENI(eni *enisdk.Eni) (isExisted bool, result *ccev2.ENI) {
	// 初始化基础 scopeLog，避免重复初始化
	scopeLog := eniLog.WithFields(logrus.Fields{
		"eniID": eni.EniId,
		"vpcID": eni.VpcId,
	})

	old, err := es.updater.Lister().Get(eni.EniId)
	if err != nil && !kerrors.IsNotFound(err) {
		return false, nil
	}
	if old != nil {
		// 校对现有ENI的spec中的IP是否和IaaS的一致
		isSynced, err := es.validateAndSyncENIIPs(old, eni)
		if err != nil {
			scopeLog.WithError(err).Error("ENI IP consistency validated and sync failed")
		} else if isSynced {
			scopeLog.Info("ENI IP consistency validated and synced successfully")
		} else {
			scopeLog.Info("ENI IP consistency validated and no sync needed")
		}
		return true, old
	}

	if eni.Status == string(ccev2.VPCENIStatusDetaching) || eni.Status == string(ccev2.VPCENIStatusDeleted) {
		return
	}
	// 基于基础 scopeLog 扩展更多字段
	scopeLog = scopeLog.WithFields(logrus.Fields{
		"eniName":    eni.Name,
		"instanceID": eni.InstanceId,
		"status":     eni.Status,
	})

	// find node by instanceID; if HPAS-style ID, query by HPASID directly (only when standard mode is enabled)
	var (
		nrsList []*ccev2.NetResourceSet
	)
	useHPASLookup := operatorOption.Config.HPASStanderd && strings.HasPrefix(eni.InstanceId, "hpas-")
	if useHPASLookup {
		nrsList, err = watchers.NetResourceSetClient.GetByHPASID(eni.InstanceId)
	} else {
		nrsList, err = watchers.NetResourceSetClient.GetByInstanceID(eni.InstanceId)
	}
	if err != nil || len(nrsList) == 0 {
		return
	}
	var resource *ccev2.NetResourceSet
	for _, nrs := range nrsList {
		// es.bceclient.ListENIs(context.TODO(), listArgs) only return the eni which is not RDMA ENI(HPC underlay RDMA/ERI overlay RDMA),
		// so we need to check the instance type of nrs.Spec.ENI.InstanceType to ensure the eni is not RDMA ENI.
		if nrs.Spec.ENI.InstanceType != bceutils.UnderlayRDMA && nrs.Spec.ENI.InstanceType != bceutils.OverlayRDMA {
			resource = nrs
			break
		}
	}

	scopeLog = scopeLog.WithField("nodeName", resource.Name)
	scopeLog.Debugf("find node by instanceID success")
	scopeLog.Infof("start to create external eni")

	effectiveInstanceID := eni.InstanceId
	if eid, e := resource.GetEffectiveInstanceID(operatorOption.Config.HPASStanderd); e != nil {
		scopeLog.WithError(e).Error("failed to get effective instanceID")
		es.eventRecorder.Eventf(resource, corev1.EventTypeWarning, "FailedGetEffectiveInstanceID",
			"failed to get effective instanceID for ENI %s on nrs %s: %v", eni.EniId, resource.Name, e)
		return false, nil
	} else if eid != "" {
		effectiveInstanceID = eid
	}

	newENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: eni.EniId,
			Labels: map[string]string{
				k8s.LabelInstanceID: effectiveInstanceID,
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
				InstanceID:                 effectiveInstanceID,
				VpcID:                      eni.VpcId,
				SubnetID:                   eni.SubnetId,
				SecurityGroupIds:           eni.SecurityGroupIds,
				EnterpriseSecurityGroupIds: eni.EnterpriseSecurityGroupIds,
				MacAddress:                 eni.MacAddress,
				Description:                eni.Description,
				PrivateIPSet:               ToModelPrivateIP(eni.PrivateIpSet, eni.VpcId, eni.SubnetId),
				IPV6PrivateIPSet:           ToModelPrivateIP(eni.Ipv6PrivateIpSet, eni.VpcId, eni.SubnetId),
			},
			Type:                      ccev2.ENIForBCC,
			RouteTableOffset:          resource.Spec.ENI.RouteTableOffset,
			InstallSourceBasedRouting: resource.Spec.ENI.InstallSourceBasedRouting,
		},
	}
	ElectENIIPv6PrimaryIP(newENI)
	result, err = es.updater.Create(newENI)
	if err != nil {
		es.eventRecorder.Eventf(resource, corev1.EventTypeWarning, "FailedCreateExternalENI", "failed to create external ENI on nrs %s: %s", resource.Name, err)
		return false, nil
	}
	es.eventRecorder.Eventf(resource, corev1.EventTypeNormal, "CreateExternalENISuccess", "create external ENI %s on nrs %s success", eni.EniId, resource.Name)
	return true, result
}

// validateAndSyncENIIPs 校对现有ENI的spec中的IP是否和IaaS的一致，如不一致则同步
func (es *remoteVpcEniSyncher) validateAndSyncENIIPs(existingENI *ccev2.ENI, iaasENI *enisdk.Eni) (bool, error) {
	scopeLog := eniLog.WithFields(logrus.Fields{
		"eniID":            existingENI.Name,
		"vpcID":            existingENI.Spec.ENI.VpcID,
		"iaas instance-id": iaasENI.InstanceId,
	})

	// 比较K8s ENI CRD中的IP集合
	k8sIPs := make(map[string]bool)
	for _, ip := range existingENI.Spec.ENI.PrivateIPSet {
		k8sIPs[ip.PrivateIPAddress] = true
	}
	for _, ip := range existingENI.Spec.ENI.IPV6PrivateIPSet {
		k8sIPs[ip.PrivateIPAddress] = true
	}

	// 比较IaaS中的IP集合
	iaasIPs := make(map[string]bool)
	for _, ip := range iaasENI.PrivateIpSet {
		iaasIPs[ip.PrivateIpAddress] = true
	}
	for _, ip := range iaasENI.Ipv6PrivateIpSet {
		iaasIPs[ip.PrivateIpAddress] = true
	}

	// 检查是否存在不一致
	var inconsistencies []string
	needsUpdate := false

	// IPs in K8s but not in IaaS
	for ip := range k8sIPs {
		if !iaasIPs[ip] {
			inconsistencies = append(inconsistencies, fmt.Sprintf("IP %s exists in K8s but not in IaaS", ip))
			needsUpdate = true
		}
	}

	// IPs in IaaS but not in K8s
	for ip := range iaasIPs {
		if !k8sIPs[ip] {
			inconsistencies = append(inconsistencies, fmt.Sprintf("IP %s exists in IaaS but not in K8s", ip))
			needsUpdate = true
		}
	}

	if existingENI.Spec.InstanceID != iaasENI.InstanceId {
		existingENI.Spec.InstanceID = iaasENI.InstanceId
		needsUpdate = true
	}

	if !needsUpdate {
		scopeLog.Debug("ENI IP sets are consistent")
		return false, nil
	}

	// 记录不一致并进行同步
	var k8sIPList, iaasIPList []string
	for ip := range k8sIPs {
		k8sIPList = append(k8sIPList, ip)
	}
	for ip := range iaasIPs {
		iaasIPList = append(iaasIPList, ip)
	}
	scopeLog.WithFields(logrus.Fields{
		"inconsistencies": inconsistencies,
		"k8sIPs":          k8sIPList,
		"iaasIPs":         iaasIPList,
	}).Warning("validateAndSyncENIIPs: inconsistencies detected, will overwrite K8s ENI from IaaS")

	// 更新ENI CRD，使用IaaS数据作为权威数据源
	updatedENI := existingENI.DeepCopy()
	updatedENI.Spec.ENI.PrivateIPSet = ToModelPrivateIP(iaasENI.PrivateIpSet, iaasENI.VpcId, iaasENI.SubnetId)
	updatedENI.Spec.ENI.IPV6PrivateIPSet = ToModelPrivateIP(iaasENI.Ipv6PrivateIpSet, iaasENI.VpcId, iaasENI.SubnetId)
	// 同步 InstanceID 变更时的标签
	if updatedENI.Labels == nil {
		updatedENI.Labels = map[string]string{}
	}
	updatedENI.Labels[k8s.LabelInstanceID] = iaasENI.InstanceId

	// 更新ENI CRD
	_, err := es.updater.Update(updatedENI)
	if err != nil {
		scopeLog.WithError(err).Error("Failed to sync ENI IPs from IaaS")
		return false, err
	}

	scopeLog.WithFields(logrus.Fields{
		"syncedIPv4Count": len(iaasENI.PrivateIpSet),
		"syncedIPv6Count": len(iaasENI.Ipv6PrivateIpSet),
	}).Info("Successfully synced ENI IPs from IaaS")

	return true, nil
}
