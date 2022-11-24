/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package ippool

import (
	"context"
	"errors"
	"fmt"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	bbcapi "github.com/baidubce/bce-sdk-go/services/bbc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	k8sutilnet "k8s.io/utils/net"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	ccetypes "github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	utilpool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

// Controller manipulates IPPool CRDs
type Controller struct {
	// Common
	kubeClient    kubernetes.Interface
	crdClient     clientset.Interface
	metaClient    metadata.Interface
	eventRecorder record.EventRecorder
	ippoolName    string
	cniMode       types.ContainerNetworkMode
	instanceID    string
	instanceType  metadata.InstanceTypeEx
	nodeName      string
	bccInstance   *bccapi.InstanceModel
	bbcInstance   *bbcapi.GetInstanceEniResult
	// ENI
	cloudClient                 cloud.Interface
	eniSubnetCandidates         []string // cluster-level candidate subnets for eni
	eniSecurityGroups           []string
	eniEnterpriseSecurityGroups []string
	preAttachedENINum           int
	// Primary ENI Secondary IP
	podSubnetCandidates []string // cluster-level candidate subnets for pod
	// misc
	subnetZoneCache map[string]string // cached subnet and zone map
	// Manage IP address resources
	ipResourceManager IPResourceManager
}

func New(
	kubeClient kubernetes.Interface,
	cloudClient cloud.Interface,
	crdClient clientset.Interface,
	cniMode ccetypes.ContainerNetworkMode,
	nodeName string,
	instanceID string,
	instanceType metadata.InstanceTypeEx,
	eniSubnetList []string,
	securityGroupList []string,
	enterpriseSecurityGroupList []string,
	preAttachedENINum int,
	podSubnetList []string,
) *Controller {
	ctx := log.NewContext()
	c := &Controller{
		kubeClient:      kubeClient,
		cloudClient:     cloudClient,
		crdClient:       crdClient,
		metaClient:      metadata.NewClient(),
		cniMode:         cniMode,
		nodeName:        nodeName,
		instanceType:    instanceType,
		ippoolName:      utilpool.GetNodeIPPoolName(nodeName),
		instanceID:      instanceID,
		subnetZoneCache: make(map[string]string),
	}

	c.eniSubnetCandidates = eniSubnetList
	c.eniSecurityGroups = securityGroupList
	c.eniEnterpriseSecurityGroups = enterpriseSecurityGroupList
	c.podSubnetCandidates = podSubnetList
	c.preAttachedENINum = preAttachedENINum

	if !types.IsCCECNIModeBasedOnVPCRoute(cniMode) {
		log.Infof(ctx, "cluster-level eni candidate subnets are: %v", c.eniSubnetCandidates)
		log.Infof(ctx, "security groups bound to eni are: %v", c.eniSecurityGroups)
		log.Infof(ctx, "enterprise security groups bound to eni are: %v", c.eniEnterpriseSecurityGroups)
		log.Infof(ctx, "cluster-level pod candidate subnets are: %v", c.podSubnetCandidates)
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	c.eventRecorder = eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cce-cni-node-agent"})

	return c
}

func (c *Controller) SyncNode(nodeKey string, nodeLister corelisters.NodeLister) error {
	var (
		err  error
		node *v1.Node
		ctx  = log.NewContext()
	)

	isLocalNode := nodeKey == c.nodeName

	if isLocalNode {
		node, err = nodeLister.Get(nodeKey)
		if err != nil {
			return err
		}

		if node.Status.Phase == v1.NodeTerminated {
			log.Infof(ctx, "node %v is terminated", node.Name)
			return nil
		}

		// node exists, then ensure pool exists
		if err = c.createOrUpdateIPPool(ctx); err != nil {
			log.Errorf(ctx, "failed to create ippool %v: %v", c.ippoolName, err)
			return err
		}
		nodeCopy := node.DeepCopy()

		//  current we only support BCC
		if c.instanceType == metadata.InstanceTypeExBCC {
			// cache instance
			if c.bccInstance == nil {
				c.bccInstance, err = c.cloudClient.GetBCCInstanceDetail(ctx, c.instanceID)
				if err != nil {
					log.Errorf(ctx, "failed to describe instance %v: %v", c.instanceID, err)
					return err
				}
				log.Infof(ctx, "instance %v detail: %v", c.instanceID, log.ToJson(c.bccInstance))
			}
		}

		switch {
		case types.IsCCECNIModeBasedOnVPCRoute(c.cniMode) || types.IsKubenetMode(c.cniMode):
			c.ipResourceManager = NewRangeIPResourceManager(c.kubeClient, c.preAttachedENINum, node)
			return c.syncRangeSpec(ctx, nodeCopy)
		case types.IsCrossVPCEniMode(c.cniMode):
			c.ipResourceManager = NewCrossVPCEniResourceManager(c.kubeClient, node, c.bccInstance)
			return c.syncRangeSpec(ctx, nodeCopy)
		case types.IsCCECNIModeBasedOnBCCSecondaryIP(c.cniMode):
			c.ipResourceManager = NewBCCIPResourceManager(c.kubeClient, c.preAttachedENINum, node, c.bccInstance)
			return c.syncENISpec(ctx, nodeCopy)
		case types.IsCCECNIModeBasedOnBBCSecondaryIP(c.cniMode):
			c.ipResourceManager = NewBBCIPResourceManager(c.kubeClient, c.preAttachedENINum, node)
			e1 := c.syncENISpec(ctx, nodeCopy)
			e2 := c.syncPodSubnetSpec(ctx, nodeCopy)
			return utilerrors.NewAggregate([]error{e1, e2})
		default:
			return fmt.Errorf("unknown cni mode: %v", c.cniMode)
		}
	}

	return nil
}

func (c *Controller) syncENISpec(ctx context.Context, node *v1.Node) error {
	nodeName := node.GetName()
	log.V(6).Infof(ctx, "syncing eni spec of node %v begins...", nodeName)
	defer log.V(6).Infof(ctx, "syncing eni spec of node %v ends...", nodeName)

	//  current we only support BCC
	if c.instanceType != metadata.InstanceTypeExBCC {
		return nil
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(ctx, c.ippoolName, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", c.ippoolName, err)
			return err
		}

		result.Spec.ENI.AvailabilityZone = c.bccInstance.ZoneName
		result.Spec.ENI.VPCID = c.bccInstance.VpcId
		result.Spec.ENI.Subnets = c.findSameZoneSubnets(ctx, c.eniSubnetCandidates)
		result.Spec.CreationSource = ipamgeneric.IPPoolCreationSourceCNI

		// update enterprise security groups
		if len(result.Spec.ENI.EnterpriseSecurityGroups) == 0 {
			if len(c.eniEnterpriseSecurityGroups) != 0 {
				result.Spec.ENI.EnterpriseSecurityGroups = c.eniEnterpriseSecurityGroups
			}
		}

		// update normal security groups
		if len(result.Spec.ENI.EnterpriseSecurityGroups) == 0 && len(result.Spec.ENI.SecurityGroups) == 0 {
			// respect user specified eni security group via configuration file,
			if len(c.eniSecurityGroups) != 0 {
				result.Spec.ENI.SecurityGroups = c.eniSecurityGroups
			} else {
				ids, err := c.getInstanceSecurityGroupID(ctx)
				if err != nil {
					return err
				}
				result.Spec.ENI.SecurityGroups = ids
			}
		}

		if len(result.Spec.ENI.Subnets) == 0 {
			msg := fmt.Sprintf("node %v in zone %v has no eni subnet in the same zone. subnet zone cache: %+v", nodeName, c.bccInstance.ZoneName, c.subnetZoneCache)
			log.Error(ctx, msg)
			c.eventRecorder.Event(&v1.ObjectReference{Kind: "ENISubnet", Name: "ENISubnetEmpty"}, v1.EventTypeWarning, "ENISubnetEmpty", msg)
		}

		_, updateErr := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(ctx, result, metav1.UpdateOptions{})
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v spec: %v", c.ippoolName, updateErr)
			return updateErr
		}

		return c.ipResourceManager.SyncCapacity(ctx)
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v spec: %v", c.ippoolName, retryErr)
		return retryErr
	}

	log.V(6).Infof(ctx, "update ippool %v spec successfully", c.ippoolName)
	return nil
}

func (c *Controller) syncPodSubnetSpec(ctx context.Context, node *v1.Node) error {
	nodeName := node.GetName()
	log.V(6).Infof(ctx, "syncing pod subnet spec of node %v begins...", nodeName)
	defer log.V(6).Infof(ctx, "syncing pod subnet spec of node %v ends...", nodeName)

	// TODO: current we only support BBC, will removed in the future.
	if c.instanceType != metadata.InstanceTypeExBBC {
		return nil
	}

	// cache instance
	if c.bbcInstance == nil {
		instance, err := c.cloudClient.GetBBCInstanceENI(ctx, c.instanceID)
		if err != nil {
			log.Errorf(ctx, "failed to describe instance %v: %v", c.instanceID, err)
			instance = &bbc.GetInstanceEniResult{}
		}
		log.Infof(ctx, "instance %v detail: %v", c.instanceID, log.ToJson(instance))
		c.bbcInstance = instance
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(ctx, c.ippoolName, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", c.ippoolName, err)
			return err
		}

		result.Spec.PodSubnets = c.findSameZoneSubnets(ctx, c.podSubnetCandidates)
		result.Spec.CreationSource = ipamgeneric.IPPoolCreationSourceCNI

		if len(result.Spec.PodSubnets) == 0 && c.bbcInstance.SubnetId != "" {
			result.Spec.PodSubnets = append(result.Spec.PodSubnets, c.bbcInstance.SubnetId)
		}

		_, updateErr := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(ctx, result, metav1.UpdateOptions{})
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v spec: %v", c.ippoolName, updateErr)
			return updateErr
		}

		return c.ipResourceManager.SyncCapacity(ctx)
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v spec: %v", c.ippoolName, retryErr)
		return retryErr
	}

	log.V(6).Infof(ctx, "update ippool %v spec successfully", c.ippoolName)
	return nil
}

func (c *Controller) syncRangeSpec(ctx context.Context, node *v1.Node) error {
	nodeName := node.GetName()
	log.V(6).Infof(ctx, "syncing ip range of node %v begins...", nodeName)
	defer log.V(6).Infof(ctx, "syncing ip range of node %v ends...", nodeName)

	// according to node specification, if spec.PodCIDRs is not empty, the first element must equal to spec.PodCIDR
	podCIDRs := make([]string, 0)
	if len(node.Spec.PodCIDRs) == 0 {
		podCIDRs = append(podCIDRs, node.Spec.PodCIDR)
	} else {
		podCIDRs = append(podCIDRs, node.Spec.PodCIDRs...)
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ippool, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(ctx, c.ippoolName, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", c.ippoolName, err)
			return err
		}

		IPv4Ranges := make([]v1alpha1.Range, 0)
		IPv6Ranges := make([]v1alpha1.Range, 0)
		for _, podCIDR := range podCIDRs {
			if k8sutilnet.IsIPv4CIDRString(podCIDR) {
				ipRange := v1alpha1.Range{
					Version: 4,
					CIDR:    podCIDR,
				}
				IPv4Ranges = append(IPv4Ranges, ipRange)
			} else if k8sutilnet.IsIPv6CIDRString(podCIDR) {
				ipRange := v1alpha1.Range{
					Version: 6,
					CIDR:    podCIDR,
				}
				IPv6Ranges = append(IPv6Ranges, ipRange)
			} else {
				log.Errorf(ctx, "pod cidr format error %s: %+v", podCIDR, err)
				return err
			}
		}

		ippool.Spec.IPv4Ranges = IPv4Ranges
		ippool.Spec.IPv6Ranges = IPv6Ranges
		ippool.Spec.CreationSource = ipamgeneric.IPPoolCreationSourceCNI
		_, updateErr := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(ctx, ippool, metav1.UpdateOptions{})
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v spec: %v", c.ippoolName, updateErr)
			return updateErr
		}

		return c.ipResourceManager.SyncCapacity(ctx)
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v spec ip range: %v", c.ippoolName, retryErr)
		return retryErr
	}

	log.V(6).Infof(ctx, "update ippool %v spec ip range successfully", c.ippoolName)

	return nil
}

// createOrUpdateIPPool creates or updates node-level IPPool CR
func (c *Controller) createOrUpdateIPPool(ctx context.Context) error {
	poolName := c.ippoolName
	_, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(ctx, poolName, metav1.GetOptions{})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}
		log.Infof(ctx, "ippool %s is not found, will create", poolName)

		ippool := &v1alpha1.IPPool{
			ObjectMeta: metav1.ObjectMeta{
				Name: poolName,
			},
			Spec: v1alpha1.IPPoolSpec{
				NodeSelector:   fmt.Sprintf("kubernetes.io/hostname=%s", c.nodeName),
				CreationSource: ipamgeneric.IPPoolCreationSourceCNI,
			},
		}
		if _, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(ctx, ippool, metav1.CreateOptions{}); err != nil {
			return err
		}
		log.Infof(ctx, "ippool %s is created successfully", poolName)
	}

	return nil
}

func (c *Controller) findSameZoneSubnets(ctx context.Context, subnets []string) []string {
	filteredSubnets := make([]string, 0)
	for _, s := range subnets {
		szone, ok := c.subnetZoneCache[s]
		if !ok {
			resp, err := c.cloudClient.DescribeSubnet(ctx, s)
			if err != nil {
				log.Errorf(ctx, "findSameZoneSubnets: skip subnet %v due to describe error: %v", s, err)
				continue
			}
			szone = resp.ZoneName
			c.subnetZoneCache[s] = szone
		}

		if c.bccInstance != nil {
			if szone == c.bccInstance.ZoneName {
				filteredSubnets = append(filteredSubnets, s)
				log.V(6).Infof(ctx, "add subnet %v at zone %v as node-level candidate", s, szone)
			}
		}

		if c.bbcInstance != nil {
			if szone == c.bbcInstance.ZoneName {
				filteredSubnets = append(filteredSubnets, s)
				log.V(6).Infof(ctx, "add subnet %v at zone %v as node-level candidate", s, szone)
			}

		}
	}

	return filteredSubnets
}

func (c *Controller) getInstanceSecurityGroupID(ctx context.Context) ([]string, error) {
	var ids []string

	securityGroups, err := c.cloudClient.ListSecurityGroup(ctx, "", c.instanceID)
	if err != nil {
		msg := fmt.Sprintf("failed to list security groups of instance %v: %v", c.instanceID, err)
		log.Error(ctx, msg)
		return nil, errors.New(msg)
	}

	for _, s := range securityGroups {
		ids = append(ids, s.Id)
	}

	return ids, nil
}
