package bcesync

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	enilisterv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
)

var (
	physicalENILog            = logging.NewSubysLogger("physical-eni-sync-manager")
	physicalENIControllerName = "physical" + eniControllerName
)

// physicalENISyncer create SyncerManager for ENI
type physicalENISyncer struct {
	VPCIDs       []string
	ClusterID    string
	SyncManager  *SyncManager[bbc.GetInstanceEniResult]
	updater      syncer.ENIUpdater
	bceclient    cloud.Interface
	resyncPeriod time.Duration
	enilister    enilisterv2.ENILister

	eventRecorder record.EventRecorder
}

// Init initialise the sync manager.
// add vpcIDs to list
func (pes *physicalENISyncer) Init(ctx context.Context) error {
	pes.bceclient = option.BCEClient()
	pes.resyncPeriod = operatorOption.Config.ResourceResyncInterval
	pes.SyncManager = NewSyncManager(physicalENIControllerName, pes.resyncPeriod, pes.syncENI)
	pes.enilister = k8s.CCEClient().Informers.Cce().V2().ENIs().Lister()
	return nil
}

func (pes *physicalENISyncer) StartENISyncer(ctx context.Context, updater syncer.ENIUpdater) syncer.ENIEventHandler {
	pes.updater = updater
	pes.SyncManager.Run()
	log.WithField(taskLogField, physicalENIControllerName).Infof("physicalENISyncer is running")
	return pes
}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (pes *physicalENISyncer) syncENI(ctx context.Context) (result []bbc.GetInstanceEniResult, err error) {
	var (
		results []bbc.GetInstanceEniResult
	)
	// Select only the ENI of the local node
	selector, _ := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelENIType: string(ccev2.ENIForBBC),
	}))
	enis, err := pes.enilister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("list ENIs failed: %w", err)
	}
	for _, eni := range enis {
		instanceId := eni.Spec.ENI.InstanceID
		if instanceId == "" && eni.Labels != nil {
			instanceId = eni.Labels[k8s.LabelInstanceID]
		}
		eniResult, err := pes.bceclient.GetBBCInstanceENI(ctx, instanceId)
		if err != nil {
			physicalENILog.WithError(err).WithField("node", eni.Spec.NodeName).Errorf("get physical ENI %s failed", eni.Name)
			continue
		}
		results = append(results, *eniResult)
	}

	return results, nil
}

// Create Process synchronization of new enis
// For a new eni, we should generally query the details of the eni directly
// and synchronously
func (pes *physicalENISyncer) Create(resource *ccev2.ENI) error {
	log.WithField(taskLogField, physicalENIControllerName).
		Infof("create a new eni(%s) crd", resource.Name)
	return pes.Update(resource)
}

func (pes *physicalENISyncer) Update(resource *ccev2.ENI) error {
	if resource.Spec.Type != ccev2.ENIForBBC {
		return nil
	}
	var (
		err error

		newObj    = resource.DeepCopy()
		eniStatus = &newObj.Status
		ctx       = logfields.NewContext()
	)

	scopeLog := physicalENILog.WithFields(logrus.Fields{
		"eniID":      newObj.Name,
		"vpcID":      newObj.Spec.ENI.VpcID,
		"eniName":    newObj.Spec.ENI.Name,
		"instanceID": newObj.Spec.ENI.InstanceID,
		"status":     newObj.Status.VPCStatus,
		"method":     "physicalENISyncer.Update",
	})

	pes.mangeFinalizer(newObj)

	scopeLog.Debug("start refresh physical eni")
	err = pes.refreshENI(ctx, newObj)
	if err != nil {
		scopeLog.WithError(err).Error("refresh physical eni failed")
		return err
	}

	// update spec and status
	if !reflect.DeepEqual(&newObj.Spec, &resource.Spec) || !reflect.DeepEqual(newObj.Labels, resource.Labels) {
		newObj, err = pes.updater.Update(newObj)
		if err != nil {
			scopeLog.WithError(err).Error("update physical eni spec failed")
			return err
		}
		scopeLog.Info("update physical eni spec success")
	}

	if !reflect.DeepEqual(eniStatus, &resource.Status) {
		newObj.Status = *eniStatus
		_, err = pes.updater.UpdateStatus(newObj)
		if err != nil {
			scopeLog.WithError(err).Error("update physical eni status failed")
			return err
		}
		scopeLog.Info("update physical eni status success")
	}
	return nil
}

// mangeFinalizer except for node deletion, direct deletion of ENI objects is prohibited
func (*physicalENISyncer) mangeFinalizer(newObj *ccev2.ENI) {
	if newObj.DeletionTimestamp == nil && len(newObj.Finalizers) == 0 {
		newObj.Finalizers = append(newObj.Finalizers, FinalizerENI)
	}
	if newObj.DeletionTimestamp != nil && len(newObj.Finalizers) != 0 {
		node, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().Get(newObj.Spec.NodeName)
		if kerrors.IsNotFound(err) || (node != nil && node.DeletionTimestamp != nil) {
			var finalizers []string
			for _, f := range newObj.Finalizers {
				if f == FinalizerENI {
					continue
				}
				finalizers = append(finalizers, f)
			}
			newObj.Finalizers = finalizers
		}
	}
}

func (pes *physicalENISyncer) Delete(name string) error {
	physicalENILog.WithField(taskLogField, physicalENIControllerName).
		Infof("eni(%s) have been deleted", name)
	return nil
}

func (pes *physicalENISyncer) ResyncENI(context.Context) time.Duration {
	log.WithField(taskLogField, physicalENIControllerName).Infof("start to resync physical eni")
	pes.SyncManager.RunImmediately()
	return pes.resyncPeriod
}

// override ENI spec
// convert private IP set
// 1. set ip family by private ip address
// 2. set subnet by priveip search subnet of the private IP from subnets
// 3. override eni status
func (es *physicalENISyncer) refreshENI(ctx context.Context, newObj *ccev2.ENI) error {
	var (
		eniCache *bbc.GetInstanceEniResult
		err      error
	)

	// should refresh eni
	if newObj.Spec.VPCVersion != newObj.Status.VPCVersion {
		instanceId := newObj.Spec.ENI.InstanceID
		if instanceId == "" && newObj.Labels != nil {
			instanceId = newObj.Labels[k8s.LabelInstanceID]
		}
		eniCache, err = es.statENI(ctx, instanceId)
	} else {
		eniCache, err = es.getENIWithCache(ctx, newObj)
	}
	if err != nil {
		if cloud.IsErrorENINotFound(err) || cloud.IsErrorReasonNoSuchObject(err) {
			if newObj.Status.VPCStatus != ccev2.VPCENIStatusNone {
				newObj.Status.VPCStatus = ccev2.VPCENIStatusDeleted
			}
		}
		return err
	}

	if eniCache != nil && eniCache.MacAddress != "" {
		newObj.Spec.ENI.ID = eniCache.Id
		newObj.Spec.ENI.Name = eniCache.Name
		newObj.Spec.ENI.MacAddress = eniCache.MacAddress
		newObj.Spec.ENI.Description = eniCache.Description
		newObj.Spec.ENI.VpcID = eniCache.VpcId
		newObj.Spec.ENI.ZoneName = eniCache.ZoneName
		newObj.Spec.ENI.SubnetID = eniCache.SubnetId

		if len(newObj.Labels) == 0 {
			newObj.Labels = map[string]string{}
		}
		newObj.Labels[k8s.VPCIDLabel] = eniCache.VpcId
		newObj.Labels[k8s.LabelNodeName] = newObj.Spec.NodeName

		// convert private IP set
		newObj.Spec.ENI.PrivateIPSet = []*models.PrivateIP{}
		newObj.Spec.ENI.IPV6PrivateIPSet = []*models.PrivateIP{}
		for _, bbcPrivateIP := range eniCache.PrivateIpSet {
			newObj.Spec.ENI.PrivateIPSet = append(newObj.Spec.ENI.PrivateIPSet, &models.PrivateIP{
				PrivateIPAddress: bbcPrivateIP.PrivateIpAddress,
				Primary:          bbcPrivateIP.Primary,
				SubnetID:         bbcPrivateIP.SubnetId,
				PublicIPAddress:  bbcPrivateIP.PublicIpAddress,
			})

			if bbcPrivateIP.Ipv6Address != "" {
				newObj.Spec.ENI.IPV6PrivateIPSet = append(newObj.Spec.ENI.IPV6PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: bbcPrivateIP.Ipv6Address,
					Primary:          bbcPrivateIP.Primary,
					SubnetID:         bbcPrivateIP.SubnetId,
				})
			}
		}

		(&newObj.Status).VPCVersion = newObj.Spec.VPCVersion
	}

	(&newObj.Status).AppendVPCStatus(ccev2.VPCENIStatus(eniCache.Status))
	return nil
}

// getENIWithCache gets a ENI from the cache if it is there, otherwise
func (pes *physicalENISyncer) getENIWithCache(ctx context.Context, resource *ccev2.ENI) (*bbc.GetInstanceEniResult, error) {
	var err error
	eniCache := pes.SyncManager.Get(resource.Name)
	// Directly request VPC back to the source
	if eniCache == nil {
		instanceId := resource.Spec.ENI.InstanceID
		if instanceId == "" && resource.Labels != nil {
			instanceId = resource.Labels[k8s.LabelInstanceID]
		}
		eniCache, err = pes.statENI(ctx, instanceId)
	}
	if err == nil && eniCache == nil {
		return nil, errors.New(string(cloud.ErrorReasonNoSuchObject))
	}
	return eniCache, err
}

// statENI returns one ENI with the given name from bce cloud
func (pes *physicalENISyncer) statENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error) {
	var (
		eniCache *bbc.GetInstanceEniResult
		err      error
	)
	eniCache, err = pes.bceclient.GetBBCInstanceENI(ctx, instanceID)
	if err != nil {
		physicalENILog.WithField(taskLogField, physicalENIControllerName).
			WithField("instanceID", instanceID).
			WithContext(ctx).
			WithError(err).Errorf("stat eni failed")
		return nil, err
	}
	pes.SyncManager.AddItems([]bbc.GetInstanceEniResult{*eniCache})
	return eniCache, nil
}
