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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	eniControllerName = "eni-sync-manager"

	ENIReadyTimeToAttach = 5 * time.Second
	ENIMaxCreateDuration = 10 * time.Minute

	FinalizerENI = "eni-syncer"
)

var eniLog = logging.NewSubysLogger("bce-sync-manager")

// VPCENISyncer only work with single vpc cluster
type VPCENISyncer struct {
	eni    *eniSyncher
	bbceni *bbcENISyncer
}

// NewVPCENISyncer create a new VPCENISyncer
func (es *VPCENISyncer) Init(ctx context.Context) error {
	es.eni = &eniSyncher{}
	es.eni.VPCIDs = append(es.eni.VPCIDs, operatorOption.Config.BCECloudVPCID)
	es.eni.ClusterID = operatorOption.Config.CCEClusterID
	err := es.eni.Init(ctx)
	if err != nil {
		return err
	}

	es.bbceni = &bbcENISyncer{}
	return es.bbceni.Init(ctx)
}

// StartENISyncer implements syncer.ENISyncher
func (es *VPCENISyncer) StartENISyncer(ctx context.Context, updater syncer.ENIUpdater) syncer.ENIEventHandler {
	es.bbceni.StartENISyncer(ctx, updater)
	es.eni.StartENISyncer(ctx, updater)
	return es
}

// Create implements syncer.ENIEventHandler
func (es *VPCENISyncer) Create(resource *ccev2.ENI) error {
	if resource.Spec.Type == ccev2.ENIForBBC {
		return es.bbceni.Create(resource)
	}
	return es.eni.Create(resource)
}

// Delete implements syncer.ENIEventHandler
func (es *VPCENISyncer) Delete(name string) error {
	es.bbceni.Delete(name)
	return es.eni.Delete(name)
}

// ResyncENI implements syncer.ENIEventHandler
func (es *VPCENISyncer) ResyncENI(ctx context.Context) time.Duration {
	es.bbceni.ResyncENI(ctx)
	return es.eni.ResyncENI(ctx)
}

// Update implements syncer.ENIEventHandler
func (es *VPCENISyncer) Update(resource *ccev2.ENI) error {
	if resource.Spec.Type == ccev2.ENIForBBC {
		return es.bbceni.Update(resource)
	}
	return es.eni.Update(resource)
}

var (
	_ syncer.ENISyncher      = &VPCENISyncer{}
	_ syncer.ENIEventHandler = &VPCENISyncer{}
)

// eniSyncher create SyncerManager for ENI
type eniSyncher struct {
	VPCIDs       []string
	ClusterID    string
	SyncManager  *SyncManager[eni.Eni]
	updater      syncer.ENIUpdater
	bceclient    cloud.Interface
	resyncPeriod time.Duration
}

// Init initialise the sync manager.
// add vpcIDs to list
func (ss *eniSyncher) Init(ctx context.Context) error {
	ss.bceclient = option.BCEClient()
	ss.resyncPeriod = operatorOption.Config.ResourceResyncInterval
	ss.SyncManager = NewSyncManager(eniControllerName, ss.resyncPeriod, ss.syncENI)

	return nil
}

func (ss *eniSyncher) StartENISyncer(ctx context.Context, updater syncer.ENIUpdater) syncer.ENIEventHandler {
	ss.updater = updater
	ss.SyncManager.Run()
	log.WithField(taskLogField, eniControllerName).Infof("ENISyncher is running")
	return ss
}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (ss *eniSyncher) syncENI(ctx context.Context) (result []eni.Eni, err error) {
	for _, vpcID := range ss.VPCIDs {
		listArgs := enisdk.ListEniArgs{
			VpcId: vpcID,
			Name:  fmt.Sprintf("%s/", ss.ClusterID),
		}
		enis, err := ss.bceclient.ListENIs(context.TODO(), listArgs)
		if err != nil {
			log.WithField(taskLogField, eniControllerName).
				WithField("request", logfields.Json(listArgs)).
				WithError(err).Errorf("sync eni failed")
			return result, err
		}

		for i := 0; i < len(enis); i++ {
			result = append(result, eni.Eni{Eni: enis[i]})
			ss.createExternalENI(&enis[i])
		}
	}
	return
}

// eni is not created on cce, we should create it?
// If this ENI is missing, CCE will continue to try to create new ENIs.
// This will result in the inability to properly identify the capacity
// of the ENI
func (ss *eniSyncher) createExternalENI(eni *enisdk.Eni) {
	old, err := ss.updater.Lister().Get(eni.EniId)
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
	scopeLog.Infof("start to create external eni")

	// find node by instanceID
	nodeList, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().List(labels.Everything())
	if err != nil {
		return
	}
	var resource *ccev2.NetResourceSet
	for _, node := range nodeList {
		if node.Spec.InstanceID == eni.InstanceId {
			resource = node
			break
		}
	}
	if resource == nil {
		return
	}
	scopeLog = scopeLog.WithField("nodeName", resource.Name)
	scopeLog.Debugf("find node by instanceID success")

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
	_, err = ss.updater.Create(newENI)
	if err != nil {
		scopeLog.WithError(err).Errorf("create external eni failed")
		return
	}
	scopeLog.Infof("create external eni success")
}

// Create Process synchronization of new enis
// For a new eni, we should generally query the details of the eni directly
// and synchronously
func (ss *eniSyncher) Create(resource *ccev2.ENI) error {
	log.WithField(taskLogField, eniControllerName).
		Infof("create a new eni(%s) crd", resource.Name)
	return ss.Update(resource)
}

func (ss *eniSyncher) Update(resource *ccev2.ENI) error {
	if resource.Spec.Type != ccev2.ENIForBCC && resource.Spec.Type != ccev2.ENIDefaultBCC {
		return nil
	}
	var (
		newObj    = resource.DeepCopy()
		eniStatus *ccev2.ENIStatus
		err       error
		ctx       = logfields.NewContext()
	)

	scopeLog := eniLog.WithFields(logrus.Fields{
		"eniID":      newObj.Name,
		"vpcID":      newObj.Spec.ENI.VpcID,
		"eniName":    newObj.Spec.ENI.Name,
		"instanceID": newObj.Spec.ENI.InstanceID,
		"status":     newObj.Status.VPCStatus,
		"method":     "eniSyncher.Update",
	})

	skipRefresh := ss.mangeFinalizer(newObj)
	if !skipRefresh {
		scopeLog.Debug("start eni machine")
		// start machine
		machine := eniStateMachine{
			ss:       ss,
			ctx:      ctx,
			resource: newObj,
		}

		err = machine.start()
		if _, ok := err.(*cm.DelayEvent); err != nil && !ok {
			scopeLog.WithError(err).Error("eni machine failed")
			return err
		}

		scopeLog.Debug("start refresh eni")
		err = ss.refreshENI(ctx, newObj)
		if err != nil {
			scopeLog.WithError(err).Error("refresh eni failed")
			return err
		}
	}

	eniStatus = &newObj.Status
	// update spec and status
	if !reflect.DeepEqual(&newObj.Spec, &resource.Spec) ||
		!reflect.DeepEqual(newObj.Labels, resource.Labels) ||
		!reflect.DeepEqual(newObj.Finalizers, resource.Finalizers) {
		scopeLog.Debug("start update eni spec")
		newObj, err = ss.updater.Update(newObj)
		if err != nil {
			scopeLog.WithError(err).Error("update eni spec failed")
			return err
		}
		scopeLog.Info("update eni spec success")
	}

	if !reflect.DeepEqual(eniStatus, &resource.Status) {
		newObj.Status = *eniStatus
		scopeLog.Debug("start update eni status")
		_, err = ss.updater.UpdateStatus(newObj)
		if err != nil {
			scopeLog.WithError(err).Error("update eni status failed")
			return err
		}
		scopeLog.Info("update eni status success")
	}
	return nil
}

// mangeFinalizer except for node deletion, direct deletion of ENI objects is prohibited
// return true: should delete this object
func (*eniSyncher) mangeFinalizer(newObj *ccev2.ENI) bool {
	if newObj.DeletionTimestamp == nil && len(newObj.Finalizers) == 0 {
		newObj.Finalizers = append(newObj.Finalizers, FinalizerENI)
	}
	if newObj.DeletionTimestamp != nil && len(newObj.Finalizers) != 0 {
		node, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().Get(newObj.Spec.NodeName)
		if kerrors.IsNotFound(err) ||
			(node != nil && node.DeletionTimestamp != nil) ||
			(node != nil && len(newObj.GetOwnerReferences()) != 0 && node.GetUID() != newObj.GetOwnerReferences()[0].UID) {
			var finalizers []string
			for _, f := range newObj.Finalizers {
				if f == FinalizerENI {
					continue
				}
				finalizers = append(finalizers, f)
			}
			newObj.Finalizers = finalizers
			log.Infof("remove finalizer from deletable ENI %s on NetResourceSet %s ", newObj.Name, newObj.Spec.NodeName)
			return true
		}
	}
	return false
}

func (ss *eniSyncher) Delete(name string) error {
	log.WithField(taskLogField, eniControllerName).
		Infof("eni(%s) have been deleted", name)
	eni, _ := ss.updater.Lister().Get(name)
	if eni == nil {
		return nil
	}

	if ss.mangeFinalizer(eni) {
		_, err := ss.updater.Update(eni)
		return err
	}
	return nil
}

func (ss *eniSyncher) ResyncENI(context.Context) time.Duration {
	log.WithField(taskLogField, eniControllerName).Infof("start to resync eni")
	ss.SyncManager.RunImmediately()
	return ss.resyncPeriod
}

// override ENI spec
// convert private IP set
// 1. set ip family by private ip address
// 2. set subnet by priveip search subnet of the private IP from subnets
// 3. override eni status
func (es *eniSyncher) refreshENI(ctx context.Context, newObj *ccev2.ENI) error {
	var (
		eniCache *eni.Eni
		err      error
	)

	// should refresh eni
	if newObj.Spec.VPCVersion != newObj.Status.VPCVersion {
		eniCache, err = es.statENI(ctx, newObj.Name)
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
		newObj.Spec.ENI.ID = eniCache.EniId
		newObj.Spec.ENI.Name = eniCache.Name
		newObj.Spec.ENI.MacAddress = eniCache.MacAddress
		newObj.Spec.ENI.SecurityGroupIds = eniCache.SecurityGroupIds
		newObj.Spec.ENI.EnterpriseSecurityGroupIds = eniCache.EnterpriseSecurityGroupIds
		newObj.Spec.ENI.Description = eniCache.Description
		newObj.Spec.ENI.VpcID = eniCache.VpcId
		newObj.Spec.ENI.ZoneName = eniCache.ZoneName
		newObj.Spec.ENI.SubnetID = eniCache.SubnetId

		if len(newObj.Labels) == 0 {
			newObj.Labels = map[string]string{}
		}
		newObj.Labels[k8s.VPCIDLabel] = eniCache.VpcId
		newObj.Labels[k8s.LabelInstanceID] = eniCache.InstanceId
		newObj.Labels[k8s.LabelNodeName] = newObj.Spec.NodeName

		newObj.Spec.ENI.PrivateIPSet = toModelPrivateIP(eniCache.PrivateIpSet, eniCache.VpcId, eniCache.SubnetId)
		newObj.Spec.ENI.IPV6PrivateIPSet = toModelPrivateIP(eniCache.Ipv6PrivateIpSet, eniCache.VpcId, eniCache.SubnetId)
		ElectENIIPv6PrimaryIP(newObj)

		(&newObj.Status).VPCVersion = newObj.Spec.VPCVersion
	}

	(&newObj.Status).AppendVPCStatus(ccev2.VPCENIStatus(eniCache.Status))
	return nil
}

// getENIWithCache gets a ENI from the cache if it is there, otherwise
func (ss *eniSyncher) getENIWithCache(ctx context.Context, resource *ccev2.ENI) (*eni.Eni, error) {
	var err error
	eniCache := ss.SyncManager.Get(resource.Name)
	// Directly request VPC back to the source
	if eniCache == nil {
		eniCache, err = ss.statENI(ctx, resource.Name)
	}
	if err == nil && eniCache == nil {
		return nil, errors.New(string(cloud.ErrorReasonNoSuchObject))
	}
	return eniCache, err
}

// statENI returns one ENI with the given name from bce cloud
func (ss *eniSyncher) statENI(ctx context.Context, ENIID string) (*eni.Eni, error) {
	eniCache, err := ss.bceclient.StatENI(ctx, ENIID)
	if err != nil {
		log.WithField(taskLogField, eniControllerName).
			WithField("ENIID", ENIID).
			WithContext(ctx).
			WithError(err).Errorf("stat eni failed")
		return nil, err
	}
	result := eni.Eni{Eni: *eniCache}
	ss.SyncManager.AddItems([]eni.Eni{result})
	return &result, nil
}

// eniStateMachine ENI state machine, used to control the state flow of ENI
type eniStateMachine struct {
	ss       *eniSyncher
	ctx      context.Context
	resource *ccev2.ENI
}

// Start state machine flow
func (esm *eniStateMachine) start() error {
	var err error
	if esm.resource.Status.VPCStatus != ccev2.VPCENIStatusInuse {

		switch esm.resource.Status.VPCStatus {
		case ccev2.VPCENIStatusNone:
			// refresh status of ENI
			_, err = esm.ss.statENI(esm.ctx, esm.resource.Name)
		case ccev2.VPCENIStatusAvailable:
			err = esm.attachENI()
		case ccev2.VPCENIStatusAttaching:
			err = esm.attachingENI()
		case ccev2.VPCENIStatusDetaching:
			// TODO: do nothing or try to delete eni
			// err = esm.deleteENI()
		}
		if err != nil {
			log.WithField(taskLogField, eniControllerName).
				WithContext(esm.ctx).
				WithError(err).
				Errorf("failed to run eni(%s) with status %s status machine", esm.resource.Spec.ENI.ID, esm.resource.Status.VPCStatus)
			return err
		}

		// not the final status, will retry later
		return cm.NewDelayEvent(esm.resource.Name, ENIReadyTimeToAttach, fmt.Sprintf("eni %s status is not final: %s", esm.resource.Spec.ENI.ID, esm.resource.Status.VPCStatus))
	}
	return nil
}

// attachENI attach a  ENI to instance
// Only accept calls whose ENI status is "available"
func (esm *eniStateMachine) attachENI() error {
	eniCache, err := esm.ss.statENI(esm.ctx, esm.resource.Spec.ENI.ID)
	if err != nil {
		return err
	}
	// status is not match
	if eniCache.Status != string(ccev2.VPCENIStatusAvailable) {
		return nil
	}

	// eni is expired, do rollback
	if esm.resource.CreationTimestamp.Add(ENIMaxCreateDuration).Before(time.Now()) {
		return esm.deleteENI()
	}

	// try to attach eni to bcc instance
	err = esm.ss.bceclient.AttachENI(esm.ctx, &enisdk.EniInstance{
		InstanceId: esm.resource.Spec.ENI.InstanceID,
		EniId:      esm.resource.Spec.ENI.ID,
	})
	if err != nil {
		err2 := esm.deleteENI()
		err = fmt.Errorf("failed to attach eni(%s) to instance(%s): %s, delete eni crd: %s", esm.resource.Spec.ENI.ID, esm.resource.Spec.ENI.InstanceID, err.Error(), err2.Error())

		return err
	}

	log.WithField(taskLogField, eniControllerName).
		WithContext(esm.ctx).
		Infof("attach eni(%s) to instance(%s) success", esm.resource.Spec.ENI.InstanceID, esm.resource.Spec.ENI.ID)
	return nil
}

// deleteENI roback to delete eni
func (esm *eniStateMachine) deleteENI() error {
	err := esm.ss.bceclient.DeleteENI(esm.ctx, esm.resource.Spec.ENI.ID)
	if err != nil {
		return fmt.Errorf("failed to delete eni(%s): %s", esm.resource.Spec.ENI.ID, err.Error())
	}

	// delete resource after delete eni in cloud
	err = esm.ss.updater.Delete(esm.resource.Name)
	if err != nil {
		return fmt.Errorf("failed to delete eni(%s) crd resource: %s", esm.resource.Name, err.Error())
	}

	return fmt.Errorf("eni(%s) delete success", esm.resource.Spec.ENI.ID)
}

// attachingENI Processing ENI in the attaching state
// ENI may be stuck in the attaching state for a long time and need to be manually deleted
func (esm *eniStateMachine) attachingENI() error {
	eniCache, err := esm.ss.statENI(esm.ctx, esm.resource.Spec.ENI.ID)
	if err != nil {
		return err
	}
	// status is not match
	if eniCache.Status != string(ccev2.VPCENIStatusAttaching) {
		return nil
	}

	if esm.resource.CreationTimestamp.Add(ENIMaxCreateDuration).Before(time.Now()) {
		return esm.deleteENI()
	}
	return nil
}

// toModelPrivateIP convert private ip to model
func toModelPrivateIP(ipset []enisdk.PrivateIp, vpcID, subnetID string) []*models.PrivateIP {
	var pIPSet []*models.PrivateIP
	for _, pip := range ipset {
		newPIP := &models.PrivateIP{
			PublicIPAddress:  pip.PublicIpAddress,
			PrivateIPAddress: pip.PrivateIpAddress,
			Primary:          pip.Primary,
		}
		newPIP.SubnetID = SearchSubnetID(vpcID, subnetID, pip.PrivateIpAddress)
		pIPSet = append(pIPSet, newPIP)
	}
	return pIPSet
}

// ElectENIIPv6PrimaryIP elect a ipv6 primary ip for eni
// set primary ip for IPv6 if not set
// by default, all IPv6 IPs are secondary IPs
func ElectENIIPv6PrimaryIP(newObj *ccev2.ENI) {
	if len(newObj.Spec.ENI.IPV6PrivateIPSet) > 0 {
		if newObj.Annotations == nil {
			newObj.Annotations = make(map[string]string)
		}
		old := newObj.Annotations[k8s.AnnotationENIIPv6PrimaryIP]
		havePromaryIPv6 := false
		for _, ipv6PrivateIP := range newObj.Spec.ENI.IPV6PrivateIPSet {
			if ipv6PrivateIP.PrivateIPAddress == old {
				ipv6PrivateIP.Primary = true
				break
			}
			if ipv6PrivateIP.Primary {
				havePromaryIPv6 = true
				break
			}
		}
		if !havePromaryIPv6 {
			newObj.Spec.ENI.IPV6PrivateIPSet[0].Primary = true
			newObj.Annotations[k8s.AnnotationENIIPv6PrimaryIP] = newObj.Spec.ENI.IPV6PrivateIPSet[0].PrivateIPAddress
		}
	}
}
