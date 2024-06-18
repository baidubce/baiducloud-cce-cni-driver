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
	bbcENILog            = logging.NewSubysLogger("bbc-eni-sync-manager")
	bbcENIControllerName = "bbc" + eniControllerName
)

// bbcENISyncer create SyncerManager for ENI
type bbcENISyncer struct {
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
func (ss *bbcENISyncer) Init(ctx context.Context) error {
	ss.bceclient = option.BCEClient()
	ss.resyncPeriod = operatorOption.Config.ResourceResyncInterval
	ss.SyncManager = NewSyncManager(bbcENIControllerName, ss.resyncPeriod, ss.syncENI)
	ss.enilister = k8s.CCEClient().Informers.Cce().V2().ENIs().Lister()
	return nil
}

func (ss *bbcENISyncer) StartENISyncer(ctx context.Context, updater syncer.ENIUpdater) syncer.ENIEventHandler {
	ss.updater = updater
	ss.SyncManager.Run()
	log.WithField(taskLogField, bbcENIControllerName).Infof("bbcENISyncer is running")
	return ss
}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (ss *bbcENISyncer) syncENI(ctx context.Context) (result []bbc.GetInstanceEniResult, err error) {
	var (
		results []bbc.GetInstanceEniResult
	)
	// Select only the ENI of the local node
	selector, _ := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set{
		k8s.LabelENIType: string(ccev2.ENIForBBC),
	}))
	enis, err := ss.enilister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("list ENIs failed: %w", err)
	}
	for _, eni := range enis {
		instanceId := eni.Spec.ENI.InstanceID
		if instanceId == "" && eni.Labels != nil {
			instanceId = eni.Labels[k8s.LabelInstanceID]
		}
		eniResult, err := ss.bceclient.GetBBCInstanceENI(ctx, instanceId)
		if err != nil {
			bbcENILog.WithError(err).WithField("node", eni.Spec.NodeName).Errorf("get bbc ENI %s failed", eni.Name)
			continue
		}
		results = append(results, *eniResult)
	}

	return results, nil
}

// Create Process synchronization of new enis
// For a new eni, we should generally query the details of the eni directly
// and synchronously
func (ss *bbcENISyncer) Create(resource *ccev2.ENI) error {
	log.WithField(taskLogField, bbcENIControllerName).
		Infof("create a new eni(%s) crd", resource.Name)
	return ss.Update(resource)
}

func (ss *bbcENISyncer) Update(resource *ccev2.ENI) error {
	if resource.Spec.Type != ccev2.ENIForBBC {
		return nil
	}
	var (
		err error

		newObj    = resource.DeepCopy()
		eniStatus = &newObj.Status
		ctx       = logfields.NewContext()
	)

	scopeLog := bbcENILog.WithFields(logrus.Fields{
		"eniID":      newObj.Name,
		"vpcID":      newObj.Spec.ENI.VpcID,
		"eniName":    newObj.Spec.ENI.Name,
		"instanceID": newObj.Spec.ENI.InstanceID,
		"status":     newObj.Status.VPCStatus,
		"method":     "bbcENISyncer.Update",
	})

	ss.mangeFinalizer(newObj)

	scopeLog.Debug("start refresh bbc eni")
	err = ss.refreshENI(ctx, newObj)
	if err != nil {
		scopeLog.WithError(err).Error("refresh bbc eni failed")
		return err
	}

	// update spec and status
	if !reflect.DeepEqual(&newObj.Spec, &resource.Spec) || !reflect.DeepEqual(newObj.Labels, resource.Labels) {
		newObj, err = ss.updater.Update(newObj)
		if err != nil {
			scopeLog.WithError(err).Error("update bbc eni spec failed")
			return err
		}
		scopeLog.Info("update bbc eni spec success")
	}

	if !reflect.DeepEqual(eniStatus, &resource.Status) {
		newObj.Status = *eniStatus
		_, err = ss.updater.UpdateStatus(newObj)
		if err != nil {
			scopeLog.WithError(err).Error("update bbc eni status failed")
			return err
		}
		scopeLog.Info("update bbc eni status success")
	}
	return nil
}

// mangeFinalizer except for node deletion, direct deletion of ENI objects is prohibited
func (*bbcENISyncer) mangeFinalizer(newObj *ccev2.ENI) {
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

func (ss *bbcENISyncer) Delete(name string) error {
	bbcENILog.WithField(taskLogField, bbcENIControllerName).
		Infof("eni(%s) have been deleted", name)
	return nil
}

func (ss *bbcENISyncer) ResyncENI(context.Context) time.Duration {
	log.WithField(taskLogField, bbcENIControllerName).Infof("start to resync bbc eni")
	ss.SyncManager.RunImmediately()
	return ss.resyncPeriod
}

// override ENI spec
// convert private IP set
// 1. set ip family by private ip address
// 2. set subnet by priveip search subnet of the private IP from subnets
// 3. override eni status
func (es *bbcENISyncer) refreshENI(ctx context.Context, newObj *ccev2.ENI) error {
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
func (ss *bbcENISyncer) getENIWithCache(ctx context.Context, resource *ccev2.ENI) (*bbc.GetInstanceEniResult, error) {
	var err error
	eniCache := ss.SyncManager.Get(resource.Name)
	// Directly request VPC back to the source
	if eniCache == nil {
		instanceId := resource.Spec.ENI.InstanceID
		if instanceId == "" && resource.Labels != nil {
			instanceId = resource.Labels[k8s.LabelInstanceID]
		}
		eniCache, err = ss.statENI(ctx, instanceId)
	}
	if err == nil && eniCache == nil {
		return nil, errors.New(string(cloud.ErrorReasonNoSuchObject))
	}
	return eniCache, err
}

// statENI returns one ENI with the given name from bce cloud
func (ss *bbcENISyncer) statENI(ctx context.Context, instanceID string) (*bbc.GetInstanceEniResult, error) {
	var (
		eniCache *bbc.GetInstanceEniResult
		err      error
	)
	eniCache, err = ss.bceclient.GetBBCInstanceENI(ctx, instanceID)
	if err != nil {
		bbcENILog.WithField(taskLogField, bbcENIControllerName).
			WithField("instanceID", instanceID).
			WithContext(ctx).
			WithError(err).Errorf("stat eni failed")
		return nil, err
	}
	ss.SyncManager.AddItems([]bbc.GetInstanceEniResult{*eniCache})
	return eniCache, nil
}
