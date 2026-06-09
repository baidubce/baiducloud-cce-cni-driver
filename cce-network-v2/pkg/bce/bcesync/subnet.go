package bcesync

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	subnetControllerName = "subnet-sync-manager"
)

var validate = validator.New()

type VPCSubnetSyncher struct {
	*subnetSyncher
}

func (ss *VPCSubnetSyncher) Init(ctx context.Context) error {
	InitBSM()
	ss.subnetSyncher = &subnetSyncher{}
	ss.VPCIDs = append(ss.VPCIDs, operatorOption.Config.BCECloudVPCID)
	return ss.subnetSyncher.Init(ctx)
}

var _ syncer.SubnetSyncher = &VPCSubnetSyncher{}

type subnetSyncher struct {
	VPCIDs       []string
	SyncManager  *SyncManager[vpc.Subnet]
	updater      syncer.SubnetUpdater
	bceclient    cloud.Interface
	hpasclient   cloud.Interface
	resyncPeriod time.Duration
}

// Init initialise the sync manager.
// add vpcIDs to list
func (ss *subnetSyncher) Init(ctx context.Context) error {
	ss.bceclient = option.BCEClient()
	ss.hpasclient = option.BCEHPASClient()
	ss.resyncPeriod = operatorOption.Config.ResourceResyncInterval
	ss.SyncManager = NewSyncManager(subnetControllerName, ss.resyncPeriod, ss.syncSubnet)
	return nil
}

func (ss *subnetSyncher) StartSubnetSyncher(ctx context.Context, updater syncer.SubnetUpdater) syncer.SubnetEventHandler {
	ss.updater = updater
	ss.SyncManager.Run()
	log.WithField(taskLogField, subnetControllerName).Infof("SubnetSyncher is running")
	return ss
}

// sync subnet from vpc
// Use ListSubnets method with subnetIds parameter to get subnets in batch
// This is more efficient than querying by VPC as it only returns the subnets we need
func (ss *subnetSyncher) syncSubnet(ctx context.Context) (result []vpc.Subnet, err error) {
	cluserSubnets, err := ss.updater.Lister().List(labels.Everything())
	if err != nil {
		return
	}

	// If no cluster subnets, return empty result
	if len(cluserSubnets) == 0 {
		return
	}

	// Collect all subnet IDs that need to be queried
	var subnetIDs []string
	for i := range cluserSubnets {
		if !cluserSubnets[i].Status.Enable && cluserSubnets[i].Status.Reason != "" {
			continue
		}
		subnetIDs = append(subnetIDs, cluserSubnets[i].Name)
	}

	if len(subnetIDs) == 0 {
		return
	}

	log.WithField(taskLogField, subnetControllerName).
		WithField("totalSubnets", len(subnetIDs)).
		Debug("syncing subnets using batch query with subnetIds parameter")

	// Use ListSubnets with subnetIds parameter to get all subnets in batch
	// API supports multiple subnet IDs separated by comma
	result, err = ss.listSubnetsByIDs(ctx, subnetIDs)
	if err != nil {
		log.WithField(taskLogField, subnetControllerName).
			WithField("totalSubnets", len(subnetIDs)).
			WithError(err).
			Errorf("failed to list subnets by IDs")
		return
	}

	log.WithField(taskLogField, subnetControllerName).
		WithField("requested", len(subnetIDs)).
		WithField("found", len(result)).
		Debug("completed subnet batch query using subnetIds parameter")

	resyncBSM()
	return
}

// listSubnetsByIDs lists subnets by their IDs using the subnetIds parameter
// This is the most efficient way as it only returns the subnets we need without filtering
// API supports multiple subnet IDs separated by comma: subnetIds=sbn-xxx,sbn-yyy,sbn-zzz
// Note: We limit to 100 subnet IDs per request, and API maxKeys=1000, so pagination is not needed
func (ss *subnetSyncher) listSubnetsByIDs(ctx context.Context, subnetIDs []string) ([]vpc.Subnet, error) {
	if len(subnetIDs) == 0 {
		return nil, nil
	}

	// Split into batches if needed (conservative limit: 100 IDs per request)
	// Since we limit to 100 IDs and API maxKeys=1000, no pagination is needed
	const maxSubnetIDsPerRequest = 100
	var result []vpc.Subnet

	for i := 0; i < len(subnetIDs); i += maxSubnetIDsPerRequest {
		end := i + maxSubnetIDsPerRequest
		if end > len(subnetIDs) {
			end = len(subnetIDs)
		}
		batch := subnetIDs[i:end]

		// Join subnet IDs with comma
		subnetIDsParam := strings.Join(batch, ",")

		log.WithField(taskLogField, subnetControllerName).
			WithField("batchSize", len(batch)).
			WithField("totalBatches", (len(subnetIDs)+maxSubnetIDsPerRequest-1)/maxSubnetIDsPerRequest).
			WithField("currentBatch", i/maxSubnetIDsPerRequest+1).
			Debug("querying subnets by IDs")

		// Call API with subnetIds parameter
		listResult, err := ss.bceclient.ListSubnets(ctx, &vpc.ListSubnetArgs{
			SubnetIds: subnetIDsParam,
			MaxKeys:   1000, // API limit, much larger than our batch size
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list subnets by IDs, index starting from 0: (index %d-%d, IDs: %v): %w",
				i, end-1, batch, err)
		}

		result = append(result, listResult.Subnets...)

		log.WithField(taskLogField, subnetControllerName).
			WithField("batchSize", len(batch)).
			WithField("returned", len(listResult.Subnets)).
			Debug("completed batch query")
	}

	// Check if we got all requested subnets
	if len(result) != len(subnetIDs) {
		foundIDs := make(map[string]bool)
		for _, subnet := range result {
			foundIDs[subnet.SubnetId] = true
		}

		var missingIDs []string
		for _, reqID := range subnetIDs {
			if !foundIDs[reqID] {
				missingIDs = append(missingIDs, reqID)
			}
		}

		if len(missingIDs) > 0 {
			log.WithField(taskLogField, subnetControllerName).
				WithField("requested", len(subnetIDs)).
				WithField("found", len(result)).
				WithField("missing", missingIDs).
				Warning("some requested subnets not found")
		}
	}

	return result, nil
}

// Create Process synchronization of new subnets
// For a new subnet, we should generally query the details of the subnet directly
// and synchronously
func (ss *subnetSyncher) Create(resource *ccev1.Subnet) error {
	return ss.Update(resource)
}

func (ss *subnetSyncher) Update(resource *ccev1.Subnet) error {
	var (
		newObj    = resource.DeepCopy()
		newStatus *ccev1.SubnetStatus
	)
	defer func() {
		GlobalBSM().updateSubnet(newObj)
	}()
	sbnCache, err := ss.getSubnetWithCache(resource.Name)
	if err != nil || sbnCache == nil {
		// If the subnet does not exist on the corresponding VPC,
		// create an unavailable subnet object
		if cloud.IsErrorReasonNoSuchObject(err) {
			newObj.Status = ccev1.SubnetStatus{
				Enable:         false,
				Reason:         string(cloud.ErrorReasonNoSuchObject),
				AvailableIPNum: 0,
				HasNoMoreIP:    true,
			}
			goto patchStatus
		}
		return err
	}

	newObj.Spec.ID = sbnCache.SubnetId
	newObj.Spec.Name = sbnCache.Name
	newObj.Spec.CIDR = sbnCache.Cidr
	newObj.Spec.AvailabilityZone = sbnCache.ZoneName
	newObj.Spec.VPCID = sbnCache.VPCId
	newObj.Spec.Description = sbnCache.Description
	newObj.Spec.IPv6CIDR = sbnCache.Ipv6Cidr
	if newObj.Labels == nil {
		newObj.Labels = make(map[string]string)
		newObj.Labels[k8s.VPCIDLabel] = newObj.Spec.VPCID
	}

	if len(sbnCache.Tags) > 0 {
		newObj.Spec.Tags = make(map[string]string)
		for i := 0; i < len(sbnCache.Tags); i++ {
			newObj.Spec.Tags[sbnCache.Tags[i].TagKey] = sbnCache.Tags[i].TagValue
		}
	}
	if !reflect.DeepEqual(newObj.Spec, resource.Spec) {
		newObj, err = ss.updater.Update(newObj)
		if err != nil {
			return err
		}
	}

	// update subnet status
	newStatus = newCCESubnetStatus(sbnCache)
	newObj.Status = *newStatus

patchStatus:
	if reflect.DeepEqual(&newObj.Status, &resource.Status) {
		return nil
	}
	_, err = ss.updater.UpdateStatus(newObj)
	return err
}
func (ss *subnetSyncher) Delete(name string) error {
	return nil
}
func (ss *subnetSyncher) ResyncSubnet(context.Context) time.Duration {
	log.WithField(taskLogField, subnetControllerName).Infof("start to resync subnet")
	ss.SyncManager.RunImmediately()
	return ss.resyncPeriod
}

// getSubnetWithCache gets a Subnet from the cache if it is there, otherwise
func (ss *subnetSyncher) getSubnetWithCache(name string) (*vpc.Subnet, error) {
	sbnCache := ss.SyncManager.Get(name)
	// Directly request VPC back to the source
	if sbnCache == nil {
		sbn, err := ss.bceclient.DescribeSubnet(context.TODO(), name)
		if err != nil {
			return nil, err
		}
		// Validate response to detect API degradation
		if err := validate.Struct(sbn); err != nil {
			return nil, fmt.Errorf("subnet response validation failed (possible API degradation): %w", err)
		}
		ss.SyncManager.AddItems([]vpc.Subnet{*sbn})
		sbnCache = sbn
	}
	return sbnCache, nil
}

func newCCESubnetStatus(vsbn *vpc.Subnet) *ccev1.SubnetStatus {
	return &ccev1.SubnetStatus{
		Enable:         true,
		AvailableIPNum: vsbn.AvailableIp,
		HasNoMoreIP:    vsbn.AvailableIp == 0,
	}
}

// Search the subnet ID of the IP address
func SearchSubnetID(vpcID, defaultSbnID, privateIPStr string) string {
	if defaultSbnID == "" {
		return ""
	}
	sbn, err := EnsureSubnet(vpcID, defaultSbnID)
	if err != nil {
		log.WithField(taskLogField, eniControllerName).
			WithError(err).Errorf("failed to get subnet %s", defaultSbnID)
		return defaultSbnID
	}
	if ccev1.IsInSubnet(sbn, privateIPStr) {
		return defaultSbnID
	} else {
		sbnList, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().List(labels.SelectorFromSet(labels.Set{k8s.VPCIDLabel: vpcID}))
		if err != nil || len(sbnList) == 0 {
			log.WithField(taskLogField, eniControllerName).
				WithError(err).
				Errorf("list subnets failed")
			return ""
		}
		for _, sbn := range sbnList {
			if ccev1.IsInSubnet(sbn, privateIPStr) {
				return sbn.Name
			}
		}
	}
	log.WithFields(logrus.Fields{
		"vpcID":        vpcID,
		"defaultSbnID": defaultSbnID,
		"privateIPStr": privateIPStr,
		"defaultSbn":   logfields.Repr(sbn.Spec),
	}).Warnf("can not search subnet")
	return ""
}

func EnsureSubnet(vpcID, sbnID string) (*ccev1.Subnet, error) {
	sbn, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(sbnID)
	if err != nil {
		newSubnet := ccev1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: sbnID,
			},
			Spec: ccev1.SubnetSpec{
				ID:    sbnID,
				VPCID: vpcID,
			},
			Status: ccev1.SubnetStatus{
				Enable: false,
			},
		}
		sbn, err = k8s.CCEClient().CceV1().Subnets().Create(context.TODO(), &newSubnet, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}
	return sbn, nil
}
