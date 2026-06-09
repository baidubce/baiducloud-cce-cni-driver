package bcesync

import (
	"context"
	"fmt"
	"time"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/cm"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
)

const (
	eniControllerName = "eni-sync-manager"

	ENIReadyTimeToAttach = 1 * time.Second
	ENIReadyTimeToDelete = 1 * time.Second
	ENIMaxCreateDuration = 5 * time.Minute

	FinalizerENI = "eni-syncer"
)

var eniLog = logging.NewSubysLogger(eniControllerName)

// remoteEniSyncher
type remoteEniSyncher interface {
	syncENI(ctx context.Context) (result []eni.Eni, err error)
	statENI(ctx context.Context, eniID string) (*eni.Eni, error)

	// use eni machine to manager status of eni
	useENIMachine() bool

	setENIUpdater(updater syncer.ENIUpdater)
}

// VPCENISyncerRouter only work with single vpc cluster
type VPCENISyncerRouter struct {
	eni *eniSyncher
}

// NewVPCENISyncer create a new VPCENISyncer
func (es *VPCENISyncerRouter) Init(ctx context.Context) error {
	eventRecorder := k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: eniControllerName})
	bceclient := option.BCEClient()
	hpasclient := option.BCEHPASClient()
	resyncPeriod := operatorOption.Config.ResourceRemoteENIResyncInterval

	// 1. init vpc remote syncer
	vpcRemote := &remoteVpcEniSyncher{
		bceclient:     bceclient,
		hpasclient:    hpasclient,
		eventRecorder: eventRecorder,
		ClusterID:     operatorOption.Config.CCEClusterID,
	}
	es.eni = &eniSyncher{
		bceclient:    bceclient,
		hpasclient:   hpasclient,
		resyncPeriod: resyncPeriod,

		remoteSyncer:  vpcRemote,
		eventRecorder: eventRecorder,
	}
	err := es.eni.Init(ctx)
	if err != nil {
		return fmt.Errorf("init eni syncer failed: %v", err)
	}
	vpcRemote.syncManager = es.eni.syncManager
	vpcRemote.VPCIDs = operatorOption.Config.BCECloudVPCID
	return nil
}

// StartENISyncer implements syncer.ENISyncher
func (es *VPCENISyncerRouter) StartENISyncer(ctx context.Context, updater syncer.ENIUpdater) syncer.ENIEventHandler {
	es.eni.StartENISyncer(ctx, updater)
	return es
}

// Create implements syncer.ENIEventHandler
func (es *VPCENISyncerRouter) Create(resource *ccev2.ENI) error {
	types := resource.Spec.Type
	// Remove "if types == ccev2.ENIForHPC || types == ccev2.ENIForERI return nil",
	// because we need to support the case of RDMA ENI already have RDMA IPs
	if types == ccev2.ENIForBBC {
		return nil
	}

	return es.eni.Create(resource)
}

// Delete implements syncer.ENIEventHandler
func (es *VPCENISyncerRouter) Delete(name string) error {
	return es.eni.Delete(name)
}

// ResyncENI implements syncer.ENIEventHandler
func (es *VPCENISyncerRouter) ResyncENI(ctx context.Context) time.Duration {
	return es.eni.ResyncENI(ctx)
}

// Update implements syncer.ENIEventHandler
func (es *VPCENISyncerRouter) Update(resource *ccev2.ENI) error {
	types := resource.Spec.Type
	// Remove "if types == ccev2.ENIForHPC || types == ccev2.ENIForERI return nil",
	// because we need to support the case of RDMA ENI already have RDMA IPs
	if types == ccev2.ENIForBBC {
		return nil
	}

	return es.eni.Update(resource)
}

var (
	_ syncer.ENISyncher      = &VPCENISyncerRouter{}
	_ syncer.ENIEventHandler = &VPCENISyncerRouter{}
)

// eniSyncher create SyncerManager for ENI
type eniSyncher struct {
	syncManager  *SyncManager[eni.Eni]
	updater      syncer.ENIUpdater
	bceclient    cloud.Interface
	hpasclient   cloud.Interface
	resyncPeriod time.Duration

	remoteSyncer  remoteEniSyncher
	eventRecorder record.EventRecorder
}

// Init initialise the sync manager.
// add vpcIDs to list
func (es *eniSyncher) Init(ctx context.Context) error {
	es.syncManager = NewSyncManager(eniControllerName, es.resyncPeriod, es.remoteSyncer.syncENI)
	return nil
}

func (es *eniSyncher) StartENISyncer(ctx context.Context, updater syncer.ENIUpdater) syncer.ENIEventHandler {
	es.remoteSyncer.setENIUpdater(updater)
	es.updater = updater
	es.syncManager.Run()
	eniLog.WithField(taskLogField, eniControllerName).Infof("ENISyncher is running")
	return es
}

// Create Process synchronization of new enis
// For a new eni, we should generally query the details of the eni directly
// and synchronously
func (es *eniSyncher) Create(resource *ccev2.ENI) error {
	eniLog.WithField(taskLogField, eniControllerName).
		Infof("create a new eni(%s) crd", resource.Name)
	return es.Update(resource)
}

func (es *eniSyncher) Update(resource *ccev2.ENI) error {
	var err error

	scopeLog := eniLog.WithFields(logrus.Fields{
		"eniID":      resource.Name,
		"vpcID":      resource.Spec.ENI.VpcID,
		"eniName":    resource.Spec.ENI.Name,
		"instanceID": resource.Spec.ENI.InstanceID,
		"oldStatus":  resource.Status.VPCStatus,
		"method":     "eniSyncher.Update",
	})

	// refresh eni from vpc and retry if k8s resource is expired
	for retry := 0; retry < 3; retry++ {
		if retry > 0 {
			// refresh new k8s resource when resource is expired
			resource, err = k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), resource.Name, metav1.GetOptions{})
			if err != nil {
				scopeLog.WithError(err).Error("get eni failed")
				return err
			}
		}
		scopeLog = scopeLog.WithField("retry", retry)
		err := es.handleENIUpdate(resource, scopeLog)
		if kerrors.IsConflict(err) || kerrors.IsResourceExpired(err) {
			continue
		}
		return err
	}

	return nil
}

func (es *eniSyncher) handleENIUpdate(resource *ccev2.ENI, scopeLog *logrus.Entry) error {
	var (
		newObj            = resource.DeepCopy()
		err               error
		ctx               = logfields.NewContext()
		eniStatus         *ccev2.ENIStatus
		updateStatusError error
	)
	defer func() {
		if err == nil {
			GlobalBSM().ForceBorrowForENI(newObj)
		}
	}()

	// delete old eni
	if newObj.Status.VPCStatus == ccev2.VPCENIStatusDeleted {
		return nil
	}

	isNeedSkipUpdate := false

	isNeedUpdateFinalizer := es.mangeFinalizer(newObj)
	if isNeedUpdateFinalizer {
		if newObj.Spec.Type == ccev2.ENIForBBC ||
			newObj.Spec.UseMode == ccev2.ENIUseModePrimaryWithSecondaryIP {
			scopeLog.WithContext(ctx).WithField("eni", newObj.Name).Info("eni use mode is primary with secondary ip need update finalizer")
		} else {
			scopeLog.WithContext(ctx).WithField("eni", newObj.Name).Info("eni should remove finalizers")
		}
		newObj, err = es.updater.Update(newObj)
		if err != nil {
			scopeLog.WithError(err).Errorf("patch eni %s failed", newObj.Name)
			return err
		}
	}

	if es.remoteSyncer.useENIMachine() {
		scopeLog.Debug("start eni machine")
		// start machine
		machine := eniStateMachine{
			es:       es,
			ctx:      ctx,
			resource: newObj,
			scopeLog: scopeLog,
			isSync:   false,
		}
		// When the state machine returns a non-delay type error, the retry mechanism is triggered.
		// When the state machine returns a delay type error, it indicates that the status of the
		// ENI CR is temporarily consistent with the VPC side status, and no further state changes
		// are needed in this processing.
		// When the state machine does not return an error, the final state flag（isSync）determines
		// whether its status needs to be updated.
		err = machine.start()
		isNeedSkipUpdate = machine.isSync
		eniStatus = &machine.resource.Status
		delayEvent, isDelayError := err.(*cm.DelayEvent)
		if err != nil {
			if isDelayError {
				scopeLog.WithField("eni", newObj.Name).Infof("eni receive delay event, will retry after %v seconds", delayEvent.Duration)
			}
			return err
		}
	}

	if isNeedSkipUpdate {
		return nil
	}
	obj, err := es.updater.Lister().Get(newObj.Name)
	if err != nil || obj == nil {
		scopeLog.WithError(err).Error("get eni failed")
		return err
	}
	objEniToUpdateStatus := obj.DeepCopy()
	if logfields.Json(eniStatus) != logfields.Json(&resource.Status) &&
		eniStatus != nil {
		objEniToUpdateStatus.Status = *eniStatus
		scopeLog = scopeLog.WithFields(logrus.Fields{
			"vpcStatus": objEniToUpdateStatus.Status.VPCStatus,
			"oldStatus": resource.Status.VPCStatus,
			"cceStatus": objEniToUpdateStatus.Status.CCEStatus,
		})

		_, updateStatusError = es.updater.UpdateStatus(objEniToUpdateStatus)
		if updateStatusError != nil {
			scopeLog.WithError(updateStatusError).WithField("eni", objEniToUpdateStatus.Name).Error("update eni status failed")
			return updateStatusError
		}
		scopeLog.Info("update eni status success")
	}
	return err
}

func removeENI(es *eniSyncher, eni *ccev2.ENI) error {
	ctx := logfields.NewContext()

	eniGet, err := es.bceclient.StatENI(ctx, eni.Name)
	if err != nil {
		eniLog.WithError(err).Errorf("stat eni %s failed", eni.Name)
		return err
	}

	if eniGet.Status == string(ccev2.VPCENIStatusInuse) {
		eniLog.WithField("eni", eni.Name).Info("eni status is in-use, will be detached first")
		args := &enisdk.EniInstance{
			EniId:      eni.Name,
			InstanceId: eni.Spec.InstanceID,
		}
		err = es.bceclient.DetachENI(ctx, args)
		if err != nil {
			eniLog.WithError(err).Errorf("detach eni %s failed", eni.Name)
			return err
		}
		(&eni.Status).AppendVPCStatus(ccev2.VPCENIStatusDetaching)
		_, err = es.updater.UpdateStatus(eni)
		if err != nil {
			eniLog.WithError(err).Errorf("patch eni %s failed", eni.Name)
		}
		return err
	}

	if eniGet.Status == string(ccev2.VPCENIStatusDetaching) {
		eniLog.WithField("eni", eni.Name).Info("eni is detaching, will be deleted after detached")
		return cm.NewDelayEvent(
			eni.Name,
			ENIReadyTimeToDelete,
			fmt.Sprintf("ENI %s status is not can be deleted, status: %s", eni.Name, eniGet.Status),
		)
	}

	if eniGet.Status == string(ccev2.VPCENIStatusAvailable) {
		eniLog.WithField("eni", eni.Name).Info("eni is available, will be deleted")
		err = es.bceclient.DeleteENI(ctx, eni.Name)
		if err != nil && !cloud.IsErrorReasonNoSuchObject(err) && !cloud.IsErrorENINotFound(err) {
			eniLog.WithError(err).Errorf("delete eni %s failed", eni.Name)
			return err
		}
		// remove interface index for eni
		eni.Status.InterfaceIndex = -1
		_, err = es.updater.UpdateStatus(eni)
		if err != nil {
			eniLog.WithError(err).Errorf("patch eni %s failed", eni.Name)
		}
		return err
	}

	return nil
}

// If ENI type is not ENIForHPC(underlay-RDMA), when ReleaseENIs is true, we need release ENIs when node is deleted.
// We need infomation from ENI object to determine when to remove the ENI from IaaS.
// We should remove finalizer from ENI object after ENI interface is deleted successfully.
// So we can not remove finalizer from ENI object before it is realy deleted.
func isCanRemoveENIFinalizer(eniType ccev2.ENIType, eniInterfaceIndex int) bool {
	// if ENI type is not ENIForHPC(underlay-RDMA), when release ENIs when node is deleted, we should remove finalizer after eni is deleted.
	// if ENI is detached, we should set InterfaceIndex to -1, then we can deleted ENI object by remove finalizer.
	return eniType == ccev2.ENIForHPC || !operatorOption.Config.ReleaseENIs || eniInterfaceIndex == -1
}

// When the Finalizer is null, only patching is allowed, updates will ignore changes.
// mangeFinalizer except for node deletion, direct deletion of ENI objects is prohibited
// Only when the ENI is not in use, the finalizer will be removed. Then, return true.
// return true: should patch this object (*ccev2.ENI)
func (es *eniSyncher) mangeFinalizer(newObj *ccev2.ENI) (isNeedUpdateFinalizer bool) {
	isNeedUpdateFinalizer = false
	if newObj.DeletionTimestamp == nil && len(newObj.Finalizers) == 0 {
		newObj.Finalizers = append(newObj.Finalizers, FinalizerENI)
		isNeedUpdateFinalizer = (newObj.Spec.Type == ccev2.ENIForBBC ||
			newObj.Spec.UseMode == ccev2.ENIUseModePrimaryWithSecondaryIP)
	}
	var finalizers []string

	if newObj.DeletionTimestamp != nil && len(newObj.Finalizers) != 0 {
		node, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().Get(newObj.Spec.NodeName)
		// if release ENIs when node is deleted, we should remove finalizer after eni is deleted
		if kerrors.IsNotFound(err) && isCanRemoveENIFinalizer(newObj.Spec.Type, newObj.Status.InterfaceIndex) {
			goto removeFinalizer
		}
		// if release ENIs when node is deleted, we should remove finalizer after eni is deleted
		if node != nil && node.DeletionTimestamp != nil && isCanRemoveENIFinalizer(newObj.Spec.Type, newObj.Status.InterfaceIndex) {
			goto removeFinalizer
		}
		if node != nil && len(newObj.GetOwnerReferences()) != 0 && node.GetUID() != newObj.GetOwnerReferences()[0].UID {
			goto removeFinalizer
		}

		// eni is not inuse
		if newObj.Status.VPCStatus != ccev2.VPCENIStatusDeleted &&
			newObj.Status.VPCStatus != ccev2.VPCENIStatusInuse {
			goto removeFinalizer
		}
	}
	return

removeFinalizer:
	for _, f := range newObj.Finalizers {
		if f == FinalizerENI {
			continue
		}
		finalizers = append(finalizers, f)
	}
	newObj.Finalizers = finalizers
	eniLog.Infof("remove finalizer from deletable ENI %s on NetResourceSet %s ", newObj.Name, newObj.Spec.NodeName)
	if len(newObj.Finalizers) == 0 {
		newObj.Finalizers = nil
		isNeedUpdateFinalizer = true
	}
	return
}

func (es *eniSyncher) Delete(name string) error {
	eniLog.WithField(taskLogField, eniControllerName).
		Infof("eni(%s) have been deleted", name)
	eni, _ := es.updater.Lister().Get(name)
	if eni == nil {
		eniLog.WithField(taskLogField, eniControllerName).
			Infof("eni(%s) not found", name)
		return nil
	}

	// If the corresponding Kubernetes Node still exists, skip the IP release
	// to avoid releasing IPs while the node is still present.
	if nodeObj, nodeErr := watchers.NodeClient.Lister().Get(eni.Spec.NodeName); nodeErr == nil && nodeObj != nil {
		eniLog.WithField("nodeName", eni.Spec.NodeName).Info("Node still exists, ",
			"skip DetachENI and secondary IP release for ENI ", eni.Name)
	} else if nodeErr != nil && kerrors.IsNotFound(nodeErr) {
		// If the node does not exist, we need to release the secondary IP and delete the ENI.
		if operatorOption.Config.ReleaseENIs && eni.Spec.UseMode != ccev2.ENIUseModePrimaryWithSecondaryIP && eni.Spec.Type != ccev2.ENIForHPC {
			err := removeENI(es, eni)
			if err != nil && !cloud.IsErrorReasonNoSuchObject(err) && !cloud.IsErrorENINotFound(err) {
				eniLog.WithError(err).Errorf("delete eni %s failed", name)
				return err
			}
		}

		if err := es.releaseSecondaryIPsFromPrimaryENI(eni); err != nil {
			return err
		}

	} else {
		// The reason for not directly returning nodeErr is to prevent a situation where node is nil and nodeErr is also nil,
		// which could cause the result to be mistakenly interpreted as a success.
		// Therefore, a new error is constructed.
		return fmt.Errorf("get node with error %v", nodeErr)
	}

	if es.mangeFinalizer(eni) {
		eniLog.WithField("eni", eni.Name).Info("eni should remove finalizers")
		_, err := es.updater.Update(eni)
		if err != nil {
			eniLog.WithError(err).Errorf("patch eni %s failed", eni.Name)
		}
		return err
	}
	return nil
}

// releaseSecondaryIPsFromPrimaryENI releases secondary IPs from primary ENI with secondary IP mode
func (es *eniSyncher) releaseSecondaryIPsFromPrimaryENI(eni *ccev2.ENI) error {
	if eni.Spec.UseMode != ccev2.ENIUseModePrimaryWithSecondaryIP {
		return nil
	}

	var secondaryIPs []string
	for _, ip := range eni.Spec.ENI.PrivateIPSet {
		if !ip.Primary {
			secondaryIPs = append(secondaryIPs, ip.PrivateIPAddress)
		}
	}

	if len(secondaryIPs) == 0 {
		return nil
	}
	var err error
	if eni.Spec.Type == ccev2.ENIForBBC {
		err = es.bceclient.BBCBatchDelIP(logfields.NewContext(), &bbc.BatchDelIpArgs{
			InstanceId: eni.Spec.InstanceID,
			PrivateIps: secondaryIPs,
		})
	} else {
		// EBC is supported yet
		err = es.bceclient.BCCBatchDelIP(logfields.NewContext(), &bccapi.BatchDelIpArgs{
			InstanceId: eni.Spec.InstanceID,
			PrivateIps: secondaryIPs,
		})
	}
	if err != nil {
		eniLog.WithError(err).Errorf("delete secondary ips %v from eni %s failed", secondaryIPs, eni.Name)
		return err
	}
	return nil
}

func (es *eniSyncher) ResyncENI(context.Context) time.Duration {
	eniLog.WithField(taskLogField, eniControllerName).Infof("start to resync eni")
	if es.syncManager != nil {
		es.syncManager.RunImmediately()
	}
	return es.resyncPeriod
}

// eniStateMachine ENI state machine, used to control the state flow of ENI
type eniStateMachine struct {
	es       *eniSyncher
	ctx      context.Context
	resource *ccev2.ENI
	vpceni   *eni.Eni
	scopeLog *logrus.Entry
	isSync   bool
}

// Start state machine flow
func (esm *eniStateMachine) start() error {
	// set the sync flag to true, indicating that the status needs to be updated only in non-final states.
	esm.isSync = true
	// ENI for RDMA (ccev2.ENIForHPC or ccev2.ENIForERI) need do nothing, so return nil directly.
	// esm.es.remoteSyncer.statENI(esm.ctx, esm.resource.Name) can not stat ENI for RDMA, it will return
	// error like [Code: EniNotFoundException; Message: eni:eni-tzjatpp7gbh6 resource not exist;
	// RequestId: 148ca1d1-174f-494a-8192-5bae2a3bf0c7]". So we need to check ENI type first.
	if esm.resource.Spec.Type == ccev2.ENIForHPC {
		return esm.handleRdmaENI()
	}
	var err error
	switch esm.resource.Status.VPCStatus {
	case ccev2.VPCENIStatusInuse:
		esm.scopeLog.Debugf("eni %s is already in use", esm.resource.Name)
	case ccev2.VPCENIStatusDeleted:
		esm.scopeLog.Debugf("eni %s is already deleted, ingonre it", esm.resource.Name)
	default:
		err = esm.handleNonFinalStateMachine()
	}
	return err
}

func (esm *eniStateMachine) handleNonFinalStateMachine() error {
	// In non-final state, the final state flag is set to false
	esm.isSync = false

	// TODO: The preCreated ENI's OwnerReferences maybe can be managed by instance-group in the feature.
	owners := esm.resource.OwnerReferences
	managedByMe := false
	if len(owners) != 0 {
		for _, o := range owners {
			if o.APIVersion == ccev2.SchemeGroupVersion.String() && o.Kind == ccev2.NRSKindDefinition {
				managedByMe = true
				break
			}
		}
	}
	if !managedByMe {
		esm.scopeLog.Infof("eni object %s has not been managed by cce-cni yet (OwnerReference not initialized), skip state machine",
			esm.resource.Name)
		esm.isSync = true
		return nil
	}

	ifNeedUpdate, err := esm.handlePreOption()
	if err != nil || ifNeedUpdate {
		return err
	}

	switch esm.resource.Status.VPCStatus {
	case ccev2.VPCENIStatusAvailable:
		err = esm.attachENI()
	case ccev2.VPCENIStatusAttaching:
		err = esm.attachingENI()
	case ccev2.VPCENIStatusDetaching:
		// TODO: do nothing or try to delete ENI
		// err = esm.deleteENI()
	}

	if err != nil {
		eniLog.WithField(taskLogField, eniControllerName).
			WithContext(esm.ctx).
			WithError(err).
			Errorf("failed to run ENI(%s) with status %s state machine", esm.resource.Spec.ENI.ID, esm.resource.Status.VPCStatus)
		return err
	}
	// there is no need to update eni status now, so wait for next delay event to list eni state again
	esm.isSync = true

	return cm.NewDelayEvent(
		esm.resource.Name,
		ENIReadyTimeToAttach,
		fmt.Sprintf("ENI %s status is not final: %s", esm.resource.Spec.ENI.ID, esm.resource.Status.VPCStatus),
	)
}

func (esm *eniStateMachine) handlePreOption() (bool, error) {
	var err error
	// step1: get the actual status of eni from iaas
	esm.vpceni, err = esm.es.remoteSyncer.statENI(esm.ctx, esm.resource.Name)
	if err != nil {
		if cloud.IsErrorReasonNoSuchObject(err) {
			// ENI not found, will delete it if not in use
			eniLog.WithField("eniID", esm.resource.Name).Error("not in use ENI not found in VPC, will delete it")
			return false, esm.deleteENI()
		}
		return false, fmt.Errorf("eni state machine failed to refresh eni(%s) status: %v", esm.resource.Name, err)
	}

	// step2: If the eni current status is inuse, perform an atomic update of its spec and append the status.
	if esm.vpceni.Status == string(ccev2.VPCENIStatusInuse) {
		updateENISpecFromVPCENI(esm)
		// Update spec
		esm.resource, err = esm.es.updater.Update(esm.resource)
		if err != nil {
			esm.scopeLog.WithError(err).Error("update ENI spec failed")
			return false, err
		}
		// Append status
		esm.resource.Status.AppendVPCStatus(ccev2.VPCENIStatus(esm.vpceni.Status))
		esm.resource, err = esm.es.updater.UpdateStatus(esm.resource)
		if err != nil {
			return false, err
		}
		esm.scopeLog.Info("Successfully updated the ENI status to inuse and populated its content.")
		// After atomically setting its status, the ENI reaches a final state, and the final state flag is set to true
		esm.isSync = true
		return true, nil
	}

	// step3: if ENI cr status is not equal with the actual status, refresh the status of ENI
	if esm.resource.Status.VPCStatus != ccev2.VPCENIStatus(esm.vpceni.Status) {
		esm.resource.Status.AppendVPCStatus(ccev2.VPCENIStatus(esm.vpceni.Status))
		return true, nil
	}
	return false, nil
}

// handleRdmaENI handle ENI for RDMA type
// For ENIForHPC, if status is not inuse, directly update to inuse
func (esm *eniStateMachine) handleRdmaENI() error {
	if esm.resource.Status.VPCStatus != ccev2.VPCENIStatusInuse {
		(&esm.resource.Status).AppendVPCStatus(ccev2.VPCENIStatusInuse)
		esm.isSync = false
	}
	return nil
}

func updateENISpecFromVPCENI(esm *eniStateMachine) {
	esm.resource.Spec.ENI.ID = esm.vpceni.EniId
	esm.resource.Spec.ENI.Name = esm.vpceni.Name
	esm.resource.Spec.ENI.MacAddress = esm.vpceni.MacAddress
	esm.resource.Spec.ENI.InstanceID = esm.vpceni.InstanceId
	esm.resource.Spec.ENI.SecurityGroupIds = esm.vpceni.SecurityGroupIds
	esm.resource.Spec.ENI.EnterpriseSecurityGroupIds = esm.vpceni.EnterpriseSecurityGroupIds
	esm.resource.Spec.ENI.Description = esm.vpceni.Description
	esm.resource.Spec.ENI.VpcID = esm.vpceni.VpcId
	esm.resource.Spec.ENI.ZoneName = esm.vpceni.ZoneName
	esm.resource.Spec.ENI.SubnetID = esm.vpceni.SubnetId
	esm.resource.Spec.ENI.PrivateIPSet = ToModelPrivateIP(esm.vpceni.PrivateIpSet, esm.vpceni.VpcId, esm.vpceni.SubnetId)
	esm.resource.Spec.ENI.IPV6PrivateIPSet = ToModelPrivateIP(esm.vpceni.Ipv6PrivateIpSet, esm.vpceni.VpcId, esm.vpceni.SubnetId)
	ElectENIIPv6PrimaryIP(esm.resource)
}

// attachENI attach a  ENI to instance
// Only accept calls whose ENI status is "available"
func (esm *eniStateMachine) attachENI() error {
	// eni is expired, do rollback
	if esm.resource.CreationTimestamp.Add(ENIMaxCreateDuration).Before(time.Now()) {
		return esm.deleteENI()
	}

	// Dynamically select client based on instance type
	client := esm.getClientForENI()

	// try to attach eni to bcc instance
	err := client.AttachENI(esm.ctx, &enisdk.EniInstance{
		InstanceId: esm.resource.Spec.ENI.InstanceID,
		EniId:      esm.resource.Spec.ENI.ID,
	})
	if err != nil {
		esm.es.eventRecorder.Eventf(esm.resource, corev1.EventTypeWarning, "AttachENIFailed", "failed attach eni(%s) to %s, will delete it: %v", esm.resource.Spec.ENI.ID, esm.resource.Spec.ENI.InstanceID, err)

		err2 := esm.deleteENI()
		err = fmt.Errorf("failed to attach eni(%s) to instance(%s): %s, will delete eni crd", esm.resource.Spec.ENI.ID, esm.resource.Spec.ENI.InstanceID, err.Error())
		if err2 != nil {
			eniLog.WithField("eniID", esm.resource.Name).Errorf("failed to delete eni crd: %v", err2)
		}
		return err
	}

	eniLog.WithField(taskLogField, eniControllerName).
		WithContext(esm.ctx).
		Infof("attach eni(%s) to instance(%s) success", esm.resource.Spec.ENI.InstanceID, esm.resource.Spec.ENI.ID)
	return nil
}

// deleteENI roback to delete eni
func (esm *eniStateMachine) deleteENI() error {
	client := esm.getClientForENI()
	err := client.DeleteENI(esm.ctx, esm.resource.Spec.ENI.ID)
	if err != nil && !cloud.IsErrorReasonNoSuchObject(err) && !cloud.IsErrorENINotFound(err) {
		esm.es.eventRecorder.Eventf(esm.resource, corev1.EventTypeWarning, "DeleteENIFailed", "failed to delete eni(%s): %v", esm.resource.Spec.ENI.ID, err)
		return fmt.Errorf("failed to delete eni(%s): %s", esm.resource.Spec.ENI.ID, err.Error())
	}
	esm.es.eventRecorder.Eventf(esm.resource, corev1.EventTypeWarning, "DeleteENISuccess", "delete eni(%s) success", esm.resource.Spec.ENI.ID)
	// delete resource after delete eni in cloud
	err = esm.es.updater.Delete(esm.resource.Name)
	if err != nil {
		return fmt.Errorf("failed to delete eni(%s) crd resource: %s", esm.resource.Name, err.Error())
	}

	eniLog.WithField("eniID", esm.resource.Name).Info("delete eni crd resource success")
	// After successfully deleting the ENI, it reaches a final state, and the synchronization flag is set to true
	esm.isSync = true
	return nil
}

// attachingENI Processing ENI in the attaching state
// ENI may be stuck in the attaching state for a long time and need to be manually deleted
func (esm *eniStateMachine) attachingENI() error {
	if esm.resource.CreationTimestamp.Add(ENIMaxCreateDuration).Before(time.Now()) {
		esm.es.eventRecorder.Eventf(esm.resource, corev1.EventTypeWarning, "AttachingENIError", "eni(%s) is in attaching status more than %s, will delete it", esm.resource.Spec.ENI.ID, ENIMaxCreateDuration.String())
		return esm.deleteENI()
	}
	return nil
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

// ToModelPrivateIP convert private ip to model
func ToModelPrivateIP(ipset []enisdk.PrivateIp, vpcID, subnetID string) []*models.PrivateIP {
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

// getClientForENI selects the appropriate client based on the instance type of the NetResourceSet associated with the ENI resource
func (esm *eniStateMachine) getClientForENI() cloud.Interface {
	// 通过ENI的InstanceID获取对应的NetResourceSet
	nrsList, err := watchers.NetResourceSetClient.GetByInstanceID(esm.resource.Spec.ENI.InstanceID)
	if err != nil || len(nrsList) == 0 {
		// 如果获取失败，使用默认的bceclient
		esm.scopeLog.WithError(err).Warnf("failed to get NetResourceSet for instanceID %s, using default client", esm.resource.Spec.ENI.InstanceID)
		return esm.es.bceclient
	}

	// 查找匹配的NetResourceSet（非RDMA类型）
	for _, nrs := range nrsList {
		if nrs.Spec.ENI != nil && nrs.Spec.ENI.InstanceType == string(metadata.InstanceTypeExHPAS) {
			// 如果是HPAS实例，使用hpasclient
			esm.scopeLog.Infof("using HPAS client for instanceID %s (instance-type: %s)", esm.resource.Spec.ENI.InstanceID, nrs.Spec.ENI.InstanceType)
			return esm.es.hpasclient
		}
	}

	// 默认使用bceclient
	return esm.es.bceclient
}
