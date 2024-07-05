package bcesg

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/baidubce/bce-sdk-go/model"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/utils"
)

const (
	sgControllerName = "security-sync-manager"
)

const (
	taskLogField = "task"
)

var log = logging.NewSubysLogger("bce-sg-sync-manager")

type VPCSecurityGroupSyncher struct {
	*securityGroupSyncher
}

func (ss *VPCSecurityGroupSyncher) Init(ctx context.Context) error {
	ss.securityGroupSyncher = &securityGroupSyncher{}
	ss.VPCIDs = append(ss.VPCIDs, operatorOption.Config.BCECloudVPCID)
	return ss.securityGroupSyncher.Init(ctx)
}

var _ syncer.SecurityGroupSyncher = &VPCSecurityGroupSyncher{}

type securityGroupSyncher struct {
	VPCIDs       []string
	SyncManager  *bcesync.SyncManager[ccev2alpha1.SecurityGroupSpec]
	updater      syncer.SecurityGroupUpdater
	bceclient    cloud.Interface
	resyncPeriod time.Duration
	log          *logrus.Entry
	haveSync     bool

	vpcCIDR []string
}

// Init initialise the sync manager.
// add vpcIDs to list
func (ss *securityGroupSyncher) Init(ctx context.Context) error {
	ss.bceclient = option.BCEClient()
	ss.resyncPeriod = operatorOption.Config.SecurityGroupSynerDuration
	ss.SyncManager = bcesync.NewSyncManager(sgControllerName, ss.resyncPeriod, ss.syncAllSecurity)
	ss.log = log.WithField(taskLogField, sgControllerName)

	vpc, err := ss.bceclient.DescribeVPC(ctx, operatorOption.Config.BCECloudVPCID)
	if err != nil {
		ss.log.WithError(err).Errorf("failed to get vpc")
	} else {
		ss.vpcCIDR = append(ss.vpcCIDR, vpc.Cidr)
		ss.vpcCIDR = append(ss.vpcCIDR, vpc.SecondaryCidr...)
	}
	return nil
}

func (ss *securityGroupSyncher) StartSecurityGroupSyncer(ctx context.Context, updater syncer.SecurityGroupUpdater) syncer.SecurityGroupEventHandler {
	ss.updater = updater
	ss.SyncManager.Run()

	initSecurityValidator(updater, ss.resyncPeriod, ss.vpcCIDR, operatorOption.Config.ClusterPoolIPv4CIDR, false, false)
	log.WithField(taskLogField, sgControllerName).Infof("securityGroupSyncher is running")
	return ss
}

// sync all security element from vpc
// 1. query all security group from vpc
// 2. query all enterprice security group
// 3. query all acl from vpc
// Return:
// Success: if 1,2,3 all success
func (ss *securityGroupSyncher) syncAllSecurity(ctx context.Context) (result []ccev2alpha1.SecurityGroupSpec, err error) {
	if normal, err := ss.syncSecurityGroupFromVPC(ctx, operatorOption.Config.BCECloudVPCID); err != nil {
		ss.log.WithError(err).Errorf("failed to list enterprise security group")
		return result, err
	} else {
		result = append(result, normal...)
	}

	if acl, err := ss.syncACLFromVPC(ctx, operatorOption.Config.BCECloudVPCID); err != nil {
		ss.log.WithError(err).Errorf("failed to list acl")
		return result, err
	} else {
		result = append(result, acl...)
	}

	if enterprise, err := ss.syncEnterpriseSecurityGroupFromVPC(ctx); err != nil {
		ss.log.WithError(err).Errorf("failed to list enterprise security group")
		return result, err
	} else {
		result = append(result, enterprise...)
	}

	for _, sg := range result {
		_, err := ss.updater.Lister().Get(sg.ID)
		if err != nil && kerrors.IsNotFound(err) {
			ss.log.Infof("security group %s not found, create it", sg.ID)
			_, err = ss.updater.Create(&ccev2alpha1.SecurityGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: sg.ID,
				},
				Spec: sg,
			})
			if err != nil {
				ss.log.WithError(err).Error("failed to create security group resource")
				continue
			}
			ss.log.WithField("securityGroup", logfields.Json(sg)).Info("create security group from vpc")
		}
	}
	ss.haveSync = true
	return
}

func (ss *securityGroupSyncher) syncEnterpriseSecurityGroupFromVPC(ctx context.Context) (result []ccev2alpha1.SecurityGroupSpec, err error) {
	// query all security group from vpc
	egs, err := ss.bceclient.ListEsg(ctx, "")
	if err != nil {
		return result, fmt.Errorf("failed to list enterprise security group")
	}

	// translate vpc SecurityGroupModel to ccev2alpha1.SecurityGroupSpec
	for _, esg := range egs {
		sg := ccev2alpha1.SecurityGroupSpec{
			ID:   esg.Id,
			Name: esg.Name,
			Desc: esg.Desc,
			Tags: esg.Tags,
			Type: ccev2alpha1.SecurityGroupTypeEnterprise,
		}

		// sort rules by priority
		sort.Slice(esg.Rules, func(i, j int) bool {
			return esg.Rules[i].Priority < esg.Rules[j].Priority
		})
		for _, rule := range esg.Rules {
			sgRule := &bccapi.SecurityGroupRuleModel{
				SourceGroupId: rule.EnterpriseSecurityGroupRuleId,
				SourceIp:      rule.SourceIp,
				DestIp:        rule.DestIp,
				Ethertype:     rule.Ethertype,
				Protocol:      rule.Protocol,
				PortRange:     rule.PortRange,
				Direction:     rule.Direction,
				Remark:        rule.Remark,
			}

			if rule.Direction == string(vpc.ACL_RULE_DIRECTION_INGRESS) {
				if rule.Action == string(vpc.ACL_RULE_ACTION_ALLOW) {
					sg.IngressRule.Allows = append(sg.IngressRule.Allows, sgRule)
				} else {
					sg.IngressRule.Drops = append(sg.IngressRule.Drops, sgRule)
				}
			} else {
				if rule.Action == string(vpc.ACL_RULE_ACTION_ALLOW) {
					sg.EgressRule.Allows = append(sg.EgressRule.Allows, sgRule)
				} else {
					sg.EgressRule.Drops = append(sg.EgressRule.Drops, sgRule)
				}
			}
		}

		result = append(result, sg)
	}
	return
}

func (ss *securityGroupSyncher) syncSecurityGroupFromVPC(ctx context.Context, vpcID string) (result []ccev2alpha1.SecurityGroupSpec, err error) {
	// query all security group from vpc
	vpcSgs, err := ss.bceclient.ListSecurityGroup(ctx, vpcID, "")
	if err != nil {
		return result, fmt.Errorf("failed to list security group from vpc")
	}

	// translate vpc SecurityGroupModel to ccev2alpha1.SecurityGroupSpec
	for _, vpcSg := range vpcSgs {
		sg := vpcSgToSgSpec(vpcSg)
		result = append(result, sg)
	}

	return
}

func vpcSgToSgSpec(vpcSg bccapi.SecurityGroupModel) ccev2alpha1.SecurityGroupSpec {
	sg := ccev2alpha1.SecurityGroupSpec{
		ID:    vpcSg.Id,
		Name:  vpcSg.Name,
		Desc:  vpcSg.Desc,
		Tags:  vpcSg.Tags,
		VpcId: vpcSg.VpcId,
		Type:  ccev2alpha1.SecurityGroupTypeNormal,
	}

	for i := range vpcSg.Rules {
		rule := &vpcSg.Rules[i]
		if rule.Direction == string(vpc.ACL_RULE_DIRECTION_INGRESS) {
			sg.IngressRule.Allows = append(sg.IngressRule.Allows, rule)
		} else {
			sg.EgressRule.Allows = append(sg.EgressRule.Allows, rule)
		}
	}
	return sg
}

func (ss *securityGroupSyncher) syncACLFromVPC(ctx context.Context, vpcID string) (result []ccev2alpha1.SecurityGroupSpec, err error) {
	listAclRulesResult, err := ss.bceclient.ListAclEntrys(ctx, vpcID)
	if err != nil {
		return result, fmt.Errorf("failed to list acl from vpc")
	}
	for _, aclEntry := range listAclRulesResult {
		sg := aclToSGSpec(aclEntry, vpcID)
		result = append(result, sg)
	}

	return
}

func aclToSGSpec(aclEntry vpc.AclEntry, vpcID string) ccev2alpha1.SecurityGroupSpec {
	sg := ccev2alpha1.SecurityGroupSpec{
		ID:   aclEntry.SubnetId,
		Name: aclEntry.SubnetCidr,
		Type: ccev2alpha1.SecurityGroupTypeACL,
		Desc: aclEntry.SubnetId,
		Tags: []model.TagModel{
			{
				TagKey:   "SubnetId",
				TagValue: aclEntry.SubnetId,
			},
			{
				TagKey:   "SubnetCIDR",
				TagValue: aclEntry.SubnetCidr,
			},
		},
		VpcId: vpcID,
	}
	for _, aclRule := range aclEntry.AclRules {
		sgRule := &bccapi.SecurityGroupRuleModel{
			Protocol:        string(aclRule.Protocol),
			SourceIp:        aclRule.SourceIpAddress,
			DestIp:          aclRule.DestinationIpAddress,
			PortRange:       aclRule.SourcePort + "," + aclRule.DestinationPort,
			Direction:       string(aclRule.Direction),
			SecurityGroupId: aclRule.Id,
			Remark:          aclRule.SubnetId,
		}

		if aclRule.Direction == vpc.ACL_RULE_DIRECTION_INGRESS {
			if aclRule.Action == vpc.ACL_RULE_ACTION_ALLOW {
				sg.IngressRule.Allows = append(sg.IngressRule.Allows, sgRule)
			} else {
				sg.IngressRule.Drops = append(sg.IngressRule.Drops, sgRule)
			}
		} else {
			if aclRule.Action == vpc.ACL_RULE_ACTION_ALLOW {
				sg.EgressRule.Allows = append(sg.EgressRule.Allows, sgRule)
			} else {
				sg.EgressRule.Drops = append(sg.EgressRule.Drops, sgRule)
			}
		}
	}
	return sg
}

// Create Process synchronization of new subnets
// For a new subnet, we should generally query the details of the subnet directly
// and synchronously
func (ss *securityGroupSyncher) Create(resource *ccev2alpha1.SecurityGroup) error {
	return ss.Update(resource)
}

func (ss *securityGroupSyncher) Update(resource *ccev2alpha1.SecurityGroup) error {
	var (
		newObj = resource.DeepCopy()
		// newStatus *ccev2alpha1.SecurityGroupStatus
		id = resource.Name
	)

	sgCache, err := ss.getSecurityWithCache(resource.Name)
	if err != nil || sgCache == nil {
		// If the scurity group does not exist on the corresponding VPC,
		// create an unavailable subnet object
		if kerrors.IsNotFound(err) {
			goto deleteSg
		}
		return err
	}

	if logfields.Json(sgCache.EgressRule) != logfields.Json(newObj.Spec.EgressRule) ||
		logfields.Json(sgCache.IngressRule) != logfields.Json(newObj.Spec.IngressRule) {
		newObj.Spec = *sgCache
		newObj.Spec.Version = resource.Spec.Version
		goto patchSpec
	}

	// Counting the number of affected elements
	// 1. Statistics of affected machine instances
	// 2. Statistics of affected eni
	// newStatus, err = ss.refreshStatus(newObj)
	// if err != nil {
	// 	return err
	// }

	// update subnet status
	// newObj.Status = *newStatus
	if logfields.Json(newObj.Status) != logfields.Json(resource.Status) {
		goto patchStatus
	}
	return nil

deleteSg:
	return ss.Delete(id)
patchSpec:
	_, err = ss.updater.Update(newObj)
	if err != nil {
		ss.log.WithError(err).Error("failed to update security group")
		return err
	}
	ss.log.Infof("security group %s updated", newObj.Name)
	return err

patchStatus:
	_, err = ss.updater.UpdateStatus(newObj)
	if err != nil {
		ss.log.WithError(err).Error("failed to update status")
	}
	ss.log.Infof("security group %s updated", newObj.Name)
	return err
}

// refreshStatus refresh
// Return：
// 1. new status
// 3. error
func (ss *securityGroupSyncher) refreshStatus(newObj *ccev2alpha1.SecurityGroup) (*ccev2alpha1.SecurityGroupStatus, error) {
	var (
		newStatus = &ccev2alpha1.SecurityGroupStatus{}
	)
	newStatus.Version = newObj.Spec.Version
	nodeList, err := k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Lister().List(labels.Everything())
	if err != nil {
		ss.log.WithError(err).Error("failed to list net resource set")
		return nil, err
	}
	if newObj.Spec.Type == ccev2alpha1.SecurityGroupTypeACL {
		_, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(newObj.Name)
		if err != nil && kerrors.IsNotFound(err) {
			ss.log.Warnf("subnet %s not found, we will delete acl", newObj.Name)
			return nil, err
		}
		newStatus.UsedBySubnet = true
		newStatus.UserCount++
	} else {
		for _, node := range nodeList {
			if len(node.Annotations) > 0 && node.Spec.ENI != nil &&
				(strings.Contains(node.Annotations[k8s.AnnotationUseSecurityGroupIDs], newObj.Name) ||
					strings.Contains(node.Annotations[k8s.AnnotationUseEnterpriseSecurityGroupIDs], newObj.Name)) {
				newStatus.UserCount++
				newStatus.MachineCount++
				if len(node.Labels) > 0 {
					if role, ok := node.Labels[k8s.LabelClusterRoleValueMaster]; ok && role == k8s.LabelClusterRoleValueMaster {
						newStatus.UsedByMater = true
					} else {
						newStatus.UsedByNode = true
					}
				}
			}
		}

		eniList, err := k8s.CCEClient().Informers.Cce().V2().ENIs().Lister().List(labels.Everything())
		if err != nil {
			ss.log.WithError(err).Error("failed to list eni")
			return nil, err
		}
		for _, eni := range eniList {
			if utils.StrArrContains(eni.Spec.SecurityGroupIds, newObj.Name) || utils.StrArrContains(eni.Spec.EnterpriseSecurityGroupIds, newObj.Name) {
				newStatus.UserCount++
				newStatus.ENICount++
				newStatus.UsedByPod = true
			}
		}
	}
	return newStatus, nil
}

func (ss *securityGroupSyncher) Delete(name string) error {
	return nil
}
func (ss *securityGroupSyncher) ResyncSecurityGroup(context.Context) time.Duration {
	ss.log.Info("start to resync security group")
	ss.SyncManager.RunImmediately()
	return ss.resyncPeriod
}

// getSubnetWithCache gets a Subnet from the cache if it is there, otherwise
func (ss *securityGroupSyncher) getSecurityWithCache(name string) (*ccev2alpha1.SecurityGroupSpec, error) {
	if !ss.haveSync {
		return nil, fmt.Errorf("security group cache has not been synced")
	}
	if sgSpec := ss.SyncManager.Get(name); sgSpec != nil {
		return sgSpec, nil
	}
	return nil, kerrors.NewNotFound(ccev2alpha1.Resource("securitygroup"), name)
}
