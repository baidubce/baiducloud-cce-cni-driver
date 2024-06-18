package vpceni

import (
	"context"
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ebc is a wrapper of bcc node, which is used to distinguish ebc node
//
// warning: due to the fact that the EBC primary interface with secondary IP mode does not
// support extension functions such as PSTS, and its scalability is weak, it is particularly
// dependent on the number of subnet IPs
type ebcNode struct {
	*bccNode

	primaryENISubnetID   string
	haveCreatePrimaryENI bool
}

func newEBCNode(super *bccNode) *ebcNode {
	node := &ebcNode{
		bccNode: super,
	}
	node.instanceType = string(metadata.InstanceTypeExEBC)
	return node
}

func (n *ebcNode) prepareIPAllocation(scopedLog *logrus.Entry) (a *ipam.AllocationAction, err error) {
	a, err = n.bccNode.prepareIPAllocation(scopedLog)
	if err != nil || n.haveCreatePrimaryENI {
		return a, err
	}
	err = n.refreshPrimarySubnet()
	if err != nil {
		return a, err
	}

	// may should create a new primary ENI
	if a.AvailableInterfaces == 0 && a.InterfaceID == "" && n.usePrimaryENIWithSecondaryMode {
		n.manager.ForeachInstance(n.instanceID, n.k8sObj.Name,
			func(instanceID, interfaceID string, iface ipamTypes.InterfaceRevision) error {
				_, ok := iface.Resource.(*eniResource)
				if !ok {
					return nil
				}
				n.haveCreatePrimaryENI = true
				return nil
			})
		if !n.haveCreatePrimaryENI {
			// this is an important opportunity to create eni
			a.AvailableInterfaces = 1
		}
	}
	return a, err
}

func (n *ebcNode) refreshPrimarySubnet() error {
	// get customer quota from cloud
	err := n.refreshBCCInfo()
	if err != nil {
		return err
	}
	n.primaryENISubnetID = n.bccInfo.NicInfo.SubnetId
	subnets := n.FilterAvailableSubnetIds([]string{n.primaryENISubnetID})
	n.availableSubnets = subnets
	return nil
}

// CreateInterface create a new ENI
func (n *ebcNode) createInterface(ctx context.Context, allocation *ipam.AllocationAction, scopedLog *logrus.Entry) (interfaceNum int, msg string, err error) {
	if n.usePrimaryENIWithSecondaryMode {
		scopedLog.Infof("The maximum number of ENIs is 0, use primary interface with seconary IP mode")
		err := n.createPrimaryENIOnCluster(ctx, scopedLog, n.k8sObj)
		if err != nil {
			return 0, "", err
		}
		return 1, "create primary ENI on cluster", nil
	}
	return n.bccNode.createInterface(ctx, allocation, scopedLog)
}

func (n *ebcNode) createPrimaryENIOnCluster(ctx context.Context, scopedLog *logrus.Entry, resource *ccev2.NetResourceSet) error {
	// get customer quota from cloud
	// TODO: we will use vpc data to set ip quota
	err := n.refreshBCCInfo()
	if err != nil {
		return err
	}
	bccInfo := n.bccInfo
	err = n.refreshPrimarySubnet()
	if err != nil {
		return err
	}

	// create subnet object
	zone := api.TransAvailableZoneToZoneName(operatorOption.Config.BCECloudContry, operatorOption.Config.BCECloudRegion, resource.Spec.ENI.AvailabilityZone)

	newENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				k8s.LabelInstanceID: n.instanceID,
				k8s.LabelNodeName:   resource.Name,
				k8s.LabelENIType:    resource.Spec.ENI.InstanceType,
				k8s.LabelENIUseMode: string(ccev2.ENIUseModePrimaryWithSecondaryIP),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: ccev2.SchemeGroupVersion.String(),
				Kind:       ccev2.NRSKindDefinition,
				Name:       resource.Name,
				UID:        resource.UID,
			}},
			Name: n.bccInfo.NicInfo.EniId,
		},
		Spec: ccev2.ENISpec{
			NodeName: resource.Name,
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				ID:               bccInfo.NicInfo.EniId,
				Name:             bccInfo.NicInfo.Name,
				ZoneName:         zone,
				InstanceID:       n.instanceID,
				VpcID:            bccInfo.NicInfo.VpcId,
				SubnetID:         bccInfo.NicInfo.SubnetId,
				SecurityGroupIds: bccInfo.NicInfo.SecurityGroups,
			},
			RouteTableOffset:          resource.Spec.ENI.RouteTableOffset,
			InstallSourceBasedRouting: false,
			Type:                      ccev2.ENIType(resource.Spec.ENI.InstanceType),
		},
	}
	_, err = k8s.CCEClient().CceV2().ENIs().Create(ctx, newENI, metav1.CreateOptions{})
	if err != nil {
		scopedLog.Errorf("failed to create primary ENI %s with secondary IP: %v", newENI.Name, err)
		return fmt.Errorf("failed to create primary ENI %s on k8s", newENI.Name)
	}
	n.haveCreatePrimaryENI = true
	return nil
}

// AllocateIPs is called after invoking PrepareIPAllocation and needs
// to perform the actual allocation.
func (n *ebcNode) allocateIPs(ctx context.Context, scopedLog *logrus.Entry, allocation *ipam.AllocationAction, ipv4ToAllocate, ipv6ToAllocate int) (
	ipv4PrivateIPSet, ipv6PrivateIPSet []*models.PrivateIP, err error) {
	// case1: use bcc eni with secondary ip mode
	if !n.usePrimaryENIWithSecondaryMode {
		return n.bccNode.allocateIPs(ctx, scopedLog, allocation, ipv4ToAllocate, ipv6ToAllocate)
	}

	// case2: use primary eni with secondary ip mode
	if ipv4ToAllocate > 0 {
		// allocate ip
		resp, err := n.manager.bceclient.BCCBatchAddIP(ctx, &bccapi.BatchAddIpArgs{
			InstanceId:                     n.instanceID,
			SecondaryPrivateIpAddressCount: ipv4ToAllocate,
			AllocateMultiIpv6Addr:          ipv6ToAllocate > 0,
		})
		err = n.manager.HandlerVPCError(scopedLog, err, string(allocation.PoolID))
		if err != nil {
			return nil, nil, fmt.Errorf("allocate ip to eni %s failed: %v", allocation.InterfaceID, err)
		}
		scopedLog.WithField("ips", resp.PrivateIps).Debug("allocate ip to eni success")

		for _, ipstring := range resp.PrivateIps {
			ip := net.ParseIP(ipstring)
			if ip.To4() == nil {
				ipv6PrivateIPSet = append(ipv6PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: ipstring,
					SubnetID:         string(allocation.PoolID),
				})
			} else {
				ipv4PrivateIPSet = append(ipv4PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: ipstring,
					SubnetID:         string(allocation.PoolID),
				})
			}
		}
	}
	return
}

// ReleaseIPs is called after invoking PrepareIPRelease and needs to
// perform the release of IPs.
func (n *ebcNode) releaseIPs(ctx context.Context, release *ipam.ReleaseAction, ipv4ToRelease, ipv6ToRelease []string) error {
	if !n.usePrimaryENIWithSecondaryMode {
		return n.bccNode.releaseIPs(ctx, release, ipv4ToRelease, ipv6ToRelease)
	}
	if len(ipv4ToRelease) > 0 {
		err := n.manager.bceclient.BCCBatchDelIP(ctx, &bccapi.BatchDelIpArgs{
			InstanceId: n.instanceID,
			PrivateIps: ipv4ToRelease,
		})
		if err != nil {
			return fmt.Errorf("release ipv4 %v from eni %s failed: %v", ipv4ToRelease, release.InterfaceID, err)
		}
	}
	if len(ipv6ToRelease) > 0 {
		err := n.manager.bceclient.BCCBatchDelIP(ctx, &bccapi.BatchDelIpArgs{
			InstanceId: n.instanceID,
			PrivateIps: ipv6ToRelease,
		})
		if err != nil {
			return fmt.Errorf("release ipv4 %v from eni %s failed: %v", ipv4ToRelease, release.InterfaceID, err)
		}
	}
	return nil
}

func (n *ebcNode) allocateIPCrossSubnet(ctx context.Context, sbnID string) (result []*models.PrivateIP, eniID string, err error) {
	if !n.usePrimaryENIWithSecondaryMode {
		return n.bccNode.allocateIPCrossSubnet(ctx, sbnID)
	}
	return nil, "", fmt.Errorf("ebc primary interface with secondary IP mode not support allocate ip cross subnet")
}

func (n *ebcNode) reuseIPs(ctx context.Context, ips []*models.PrivateIP, owner string) (eniID string, err error) {
	if !n.usePrimaryENIWithSecondaryMode {
		return n.bccNode.reuseIPs(ctx, ips, owner)
	}
	return "", fmt.Errorf("ebc primary interface with secondary IP mode not support allocate ip cross subnet")
}
