package bcesync

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	eniapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	enilisterv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
)

var (
	physicalENILog            = logging.NewSubysLogger("physical-eni-sync-manager")
	physicalENIControllerName = "physical" + eniControllerName
)

// physicalENISyncer create SyncerManager for ENI
type physicalENISyncer struct {
	VPCIDs       []string
	ClusterID    string
	SyncManager  *SyncManager[PhysicalEni]
	updater      syncer.ENIUpdater
	bceclient    cloud.Interface
	resyncPeriod time.Duration
	enilister    enilisterv2.ENILister

	eventRecorder record.EventRecorder
}

// Init initialise the sync manager.
// add vpcIDs to list
func (pes *physicalENISyncer) Init(ctx context.Context) error {
	pes.VPCIDs = append(pes.VPCIDs, operatorOption.Config.BCECloudVPCID)
	pes.ClusterID = operatorOption.Config.CCEClusterID

	bceClient := option.BCEClient()
	pes.bceclient = bceClient

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

func getPhysicalEni(ctx context.Context, bceclient cloud.Interface, vpcIDs []string,
	clusterID, instanceID, eniID string, eniType ccev2.ENIType) (physicalEni PhysicalEni, err error) {
	var (
		eniResult PhysicalEni
	)

	switch eniType {
	case ccev2.ENIForBBC:
		eniResult.bbcEni, err = bceclient.GetBBCInstanceENI(ctx, instanceID)
	case ccev2.ENIForHPC:
		hpcEniList, err := bceclient.GetHPCEniID(ctx, instanceID)
		if err != nil {
			log.WithField(taskLogField, eniControllerName).
				WithField("request instanceID", instanceID).
				WithError(err).Errorf("sync hpc eni failed")
			return eniResult, err
		}
		for _, v := range hpcEniList.Result {
			if v.EniID == eniID {
				eniResult.hpcEni = &v
				break
			}
		}
	case ccev2.ENIForERI:
		for _, vpcID := range vpcIDs {
			listArgs := enisdk.ListEniArgs{
				VpcId:      vpcID,
				InstanceId: instanceID,
			}
			enis, err := bceclient.ListERIs(context.TODO(), listArgs)
			if err != nil {
				log.WithField(taskLogField, eniControllerName).
					WithField("request eri", logfields.Json(listArgs)).
					WithError(err).Errorf("sync eri failed")
				continue
			}

			for _, v := range enis {
				if v.EniId == eniID {
					eniResult.eri = &eniapi.Eni{Eni: v}
					break
				}
			}
		}
	default:
		return eniResult, errors.New("invalid eni type")
	}

	return eniResult, err
}

// syncENI Sync eni from BCE Cloud, and all eni data are subject to BCE Cloud
func (pes *physicalENISyncer) syncENI(ctx context.Context) (result []PhysicalEni, err error) {
	var (
		results []PhysicalEni
	)

	requirement := metav1.LabelSelectorRequirement{
		Key:      k8s.LabelENIType,
		Operator: metav1.LabelSelectorOpIn,
		Values: []string{
			string(ccev2.ENIForBBC),
			string(ccev2.ENIForHPC),
			string(ccev2.ENIForERI),
		},
	}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{requirement},
	}
	// Select only the ENI of the local node
	selector, _ := metav1.LabelSelectorAsSelector(labelSelector)
	enis, err := pes.enilister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("list ENIs failed: %w", err)
	}
	for _, eni := range enis {
		instanceId := eni.Spec.ENI.InstanceID
		if instanceId == "" && eni.Labels != nil {
			instanceId = eni.Labels[k8s.LabelInstanceID]
		}

		eniResult, err := getPhysicalEni(ctx, pes.bceclient, pes.VPCIDs, pes.ClusterID, instanceId, eni.Spec.ID, eni.Spec.Type)
		if err != nil {
			physicalENILog.WithError(err).WithField("node", eni.Spec.NodeName).Errorf("get physical ENI %s failed", eni.Name)
			continue
		}
		results = append(results, eniResult)
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
	if resource.Spec.Type != ccev2.ENIForBBC && resource.Spec.Type != ccev2.ENIForHPC && resource.Spec.Type != ccev2.ENIForERI {
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
		eniCache  *PhysicalEni
		err       error
		eniStatus string
	)

	// should refresh eni
	if newObj.Spec.VPCVersion != newObj.Status.VPCVersion {
		instanceId := newObj.Spec.ENI.InstanceID
		if instanceId == "" && newObj.Labels != nil {
			instanceId = newObj.Labels[k8s.LabelInstanceID]
		}
		eniCache, err = es.statENI(ctx, instanceId, newObj.Spec.ENI.ID, newObj.Spec.Type)
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

	if eniCache != nil {
		var (
			eniID       string
			eniName     string
			macAddress  string
			description string
			vpcID       string
			zoneName    string
			subnetID    string
		)

		type privateIPType struct {
			publicIpAddress  string
			primary          bool
			privateIpAddress string
			ipv6Address      string
			subnetId         string
		}

		var (
			privateIP    privateIPType
			privateIPSet []privateIPType
		)

		if eniCache.bbcEni != nil {
			if eniCache.bbcEni.MacAddress != "" {
				eniID = eniCache.bbcEni.Id
				eniName = eniCache.bbcEni.Name
				macAddress = eniCache.bbcEni.MacAddress
				description = eniCache.bbcEni.Description
				vpcID = eniCache.bbcEni.VpcId
				zoneName = eniCache.bbcEni.ZoneName
				subnetID = eniCache.bbcEni.SubnetId
				eniStatus = eniCache.bbcEni.Status
				for _, v := range eniCache.bbcEni.PrivateIpSet {
					privateIP = privateIPType{
						publicIpAddress:  v.PublicIpAddress,
						primary:          v.Primary,
						privateIpAddress: v.PrivateIpAddress,
						subnetId:         v.SubnetId,
						ipv6Address:      v.Ipv6Address,
					}
					privateIPSet = append(privateIPSet, privateIP)
				}
			}
		} else if eniCache.eri != nil {
			if eniCache.eri.MacAddress != "" {
				eniID = eniCache.eri.EniId
				eniName = eniCache.eri.Name
				macAddress = eniCache.eri.MacAddress
				description = eniCache.eri.Description
				vpcID = eniCache.eri.VpcId
				zoneName = eniCache.eri.ZoneName
				subnetID = eniCache.eri.SubnetId
				eniStatus = eniCache.eri.Status
				for _, v := range eniCache.eri.PrivateIpSet {
					privateIP = privateIPType{
						publicIpAddress:  v.PublicIpAddress,
						primary:          v.Primary,
						privateIpAddress: v.PrivateIpAddress,
						subnetId:         subnetID,
						ipv6Address:      "",
					}
					privateIPSet = append(privateIPSet, privateIP)
				}
			}
		} else if eniCache.hpcEni != nil {
			if eniCache.hpcEni.MacAddress != "" {
				eniID = eniCache.hpcEni.EniID
				eniName = eniCache.hpcEni.Name
				macAddress = eniCache.hpcEni.MacAddress
				description = eniCache.hpcEni.Description
				vpcID = ""
				zoneName = ""
				subnetID = ""
				eniStatus = eniCache.hpcEni.Status
				for _, v := range eniCache.hpcEni.PrivateIPSet {
					privateIP = privateIPType{
						publicIpAddress:  "",
						primary:          v.Primary,
						privateIpAddress: v.PrivateIPAddress,
						subnetId:         "",
						ipv6Address:      "",
					}
					privateIPSet = append(privateIPSet, privateIP)
				}
			}
		} else {
			return errors.New("unknown eni type")
		}

		newObj.Spec.ENI.ID = eniID
		newObj.Spec.ENI.Name = eniName
		newObj.Spec.ENI.MacAddress = macAddress
		newObj.Spec.ENI.Description = description
		newObj.Spec.ENI.VpcID = vpcID
		newObj.Spec.ENI.ZoneName = zoneName
		newObj.Spec.ENI.SubnetID = subnetID

		if len(newObj.Labels) == 0 {
			newObj.Labels = map[string]string{}
		}
		newObj.Labels[k8s.VPCIDLabel] = vpcID
		newObj.Labels[k8s.LabelNodeName] = newObj.Spec.NodeName

		// convert private IP set
		newObj.Spec.ENI.PrivateIPSet = []*models.PrivateIP{}
		newObj.Spec.ENI.IPV6PrivateIPSet = []*models.PrivateIP{}
		for _, iPrivateIP := range privateIPSet {
			newObj.Spec.ENI.PrivateIPSet = append(newObj.Spec.ENI.PrivateIPSet, &models.PrivateIP{
				PrivateIPAddress: iPrivateIP.privateIpAddress,
				Primary:          iPrivateIP.primary,
				SubnetID:         iPrivateIP.subnetId,
				PublicIPAddress:  iPrivateIP.publicIpAddress,
			})

			if iPrivateIP.ipv6Address != "" {
				newObj.Spec.ENI.IPV6PrivateIPSet = append(newObj.Spec.ENI.IPV6PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: iPrivateIP.ipv6Address,
					Primary:          iPrivateIP.primary,
					SubnetID:         iPrivateIP.subnetId,
				})
			}
		}

		(&newObj.Status).VPCVersion = newObj.Spec.VPCVersion
	}

	(&newObj.Status).AppendVPCStatus(ccev2.VPCENIStatus(eniStatus))
	return nil
}

// getENIWithCache gets a ENI from the cache if it is there, otherwise
func (pes *physicalENISyncer) getENIWithCache(ctx context.Context, resource *ccev2.ENI) (*PhysicalEni, error) {
	var err error
	eniCache := pes.SyncManager.Get(resource.Name)
	// Directly request VPC back to the source
	if eniCache == nil {
		instanceId := resource.Spec.ENI.InstanceID
		if instanceId == "" && resource.Labels != nil {
			instanceId = resource.Labels[k8s.LabelInstanceID]
		}
		eniCache, err = pes.statENI(ctx, instanceId, resource.Spec.ENI.ID, resource.Spec.Type)
	}
	if err == nil && eniCache == nil {
		return nil, errors.New(string(cloud.ErrorReasonNoSuchObject))
	}
	return eniCache, err
}

// statENI returns one ENI with the given name from bce cloud
func (pes *physicalENISyncer) statENI(ctx context.Context, instanceID, eniID string, eniType ccev2.ENIType) (*PhysicalEni, error) {
	var (
		eniCache PhysicalEni
		err      error
	)

	eniCache, err = getPhysicalEni(ctx, pes.bceclient, pes.VPCIDs, pes.ClusterID, instanceID, eniID, eniType)
	if err != nil {
		physicalENILog.WithField(taskLogField, physicalENIControllerName).
			WithField("instanceID", instanceID).
			WithContext(ctx).
			WithError(err).Errorf("stat eni failed")
		return &eniCache, err
	}
	pes.SyncManager.AddItems([]PhysicalEni{eniCache})
	return &eniCache, nil
}
