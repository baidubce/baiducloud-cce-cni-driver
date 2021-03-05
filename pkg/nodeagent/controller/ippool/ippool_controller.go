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
	"encoding/json"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/retry"
	k8sutilnet "k8s.io/utils/net"
	"modernc.org/mathutil"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	ccetypes "github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	utilpool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	DefaultPreAttachedENINum = 2
)

// Controller manipulates IPPool CRDs
type Controller struct {
	// Common
	kubeClient    kubernetes.Interface
	crdClient     clientset.Interface
	defaultIPPool string
	cniMode       types.ContainerNetworkMode
	instanceID    string
	nodeName      string
	// ENI
	cloudClient      cloud.Interface
	availabilityZone string   // node azone
	subnets          []string // cluster-level candidate subnets
	securityGroups   []string
	// Range
}

func New(
	kubeClient kubernetes.Interface,
	cloudClient cloud.Interface,
	crdClient clientset.Interface,
	cniMode ccetypes.ContainerNetworkMode,
	nodeName string,
	instanceID string,
	subnetList []string,
	securityGroupList []string,
) *Controller {
	ctx := log.NewContext()
	c := &Controller{
		kubeClient:    kubeClient,
		cloudClient:   cloudClient,
		crdClient:     crdClient,
		cniMode:       cniMode,
		nodeName:      nodeName,
		defaultIPPool: utilpool.GetDefaultIPPoolName(nodeName),
		instanceID:    instanceID,
	}

	c.subnets = subnetList
	log.Infof(ctx, "cluster-level eni candidate subnets are: %v", c.subnets)

	c.securityGroups = securityGroupList
	log.Infof(ctx, "security groups bound to eni are: %v", c.securityGroups)

	return c
}

func (c *Controller) SyncNode(nodeKey string, nodeLister corelisters.NodeLister) error {
	ctx := log.NewContext()

	isLocalNode := nodeKey == c.nodeName
	if isLocalNode {
		_, err := nodeLister.Get(nodeKey)
		if err != nil && !kerrors.IsNotFound(err) {
			return err
		}

		// node exists, then ensure pool exists
		if err := c.createOrUpdateIPPool(ctx); err != nil {
			log.Errorf(ctx, "failed to create ippool %v: %v", c.defaultIPPool, err)
			return err
		}

		switch {
		case types.IsCCECNIModeBasedOnBCCSecondaryIP(c.cniMode):
			return c.syncENISpec(ctx, nodeKey, nodeLister)
		case types.IsCCECNIModeBasedOnVPCRoute(c.cniMode) || types.IsKubenetMode(c.cniMode):
			return c.syncRangeSpec(ctx, nodeKey, nodeLister)
		case types.IsCCECNIModeBasedOnBBCSecondaryIP(c.cniMode):
		default:
			return fmt.Errorf("unknown cni mode: %v", c.cniMode)
		}
	}

	// if node is deleted, then delete default pool
	_, err := nodeLister.Get(nodeKey)
	if kerrors.IsNotFound(err) {
		// clean up pool of deleted node
		poolName := utilpool.GetDefaultIPPoolName(nodeKey)
		log.Errorf(ctx, "node %v is deleted, delete default ippool %v", nodeKey, poolName)
		if err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Delete(poolName, metav1.NewDeleteOptions(0)); err != nil && !kerrors.IsNotFound(err) {
			log.Errorf(ctx, "failed to delete ippool %v: %v", poolName, err)
		}
	}

	return nil
}

func (c *Controller) syncENISpec(ctx context.Context, nodeName string, nodeLister corelisters.NodeLister) error {
	log.Infof(ctx, "syncing eni spec of node %v begins...", nodeName)
	defer log.Infof(ctx, "syncing eni spec of node %v ends...", nodeName)

	instance, err := c.cloudClient.DescribeInstance(ctx, c.instanceID)
	if err != nil {
		log.Errorf(ctx, "failed to describe instance %v: %v", c.instanceID, err)
		return err
	}
	c.availabilityZone = instance.ZoneName

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(c.defaultIPPool, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", c.defaultIPPool, err)
			return err
		}

		result.Spec.ENI.AvailabilityZone = instance.ZoneName
		result.Spec.ENI.VPCID = instance.VpcId
		result.Spec.ENI.Subnets = c.findSameZoneSubnets(ctx, c.subnets)
		result.Spec.ENI.SecurityGroups = c.securityGroups

		_, updateErr := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(result)
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v spec: %v", c.defaultIPPool, updateErr)
			return updateErr
		}

		var maxENINum, maxIPPerENI int
		maxENINum = utileni.GetMaxENIPerNode(instance.CpuCount)
		maxIPPerENI = utileni.GetMaxIPPerENI(instance.MemoryCapacityInGB)

		err = c.patchENICapacityInfoToNode(ctx, maxENINum, maxIPPerENI)
		if err != nil {
			log.Errorf(ctx, "error patching cni capacity info: %v", err)
			return err
		}

		return nil
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v spec: %v", c.defaultIPPool, retryErr)
		return retryErr
	}

	log.Infof(ctx, "update ippool %v spec successfully", c.defaultIPPool)
	return nil
}

func (c *Controller) syncRangeSpec(ctx context.Context, nodeName string, nodeLister corelisters.NodeLister) error {
	log.Infof(ctx, "syncing ip range of node %v begins...", nodeName)
	defer log.Infof(ctx, "syncing ip range of node %v ends...", nodeName)

	node, err := nodeLister.Get(nodeName)
	if err != nil {
		log.Errorf(ctx, "failed to get node %v: %v", nodeName, err)
		return err
	}

	// according to node specification, if spec.PodCIDRs is not empty, the first element must equal to spec.PodCIDR
	podCIDRs := make([]string, 0)
	if len(node.Spec.PodCIDRs) == 0 {
		podCIDRs = append(podCIDRs, node.Spec.PodCIDR)
	} else {
		for _, podCIDR := range node.Spec.PodCIDRs {
			podCIDRs = append(podCIDRs, podCIDR)
		}
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ippool, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(c.defaultIPPool, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get ippool %v: %v", c.defaultIPPool, err)
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
		_, updateErr := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Update(ippool)
		if updateErr != nil {
			log.Errorf(ctx, "error updating ippool %v spec: %v", c.defaultIPPool, updateErr)
			return updateErr
		}

		return nil
	})

	if retryErr != nil {
		log.Errorf(ctx, "retry: error updating ippool %v spec ip range: %v", c.defaultIPPool, retryErr)
		return retryErr
	}

	log.Infof(ctx, "update ippool %v spec ip range successfully", c.defaultIPPool)
	return nil
}

// createOrUpdateIPPool creates or updates node-level IPPool CR
func (c *Controller) createOrUpdateIPPool(ctx context.Context) error {
	poolName := c.defaultIPPool
	_, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(poolName, metav1.GetOptions{})
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
				NodeSelector: fmt.Sprintf("kubernetes.io/hostname=%s", c.nodeName),
			},
		}
		if _, err := c.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Create(ippool); err != nil {
			return err
		}
		log.Infof(ctx, "ippool %s is created successfully", poolName)
	}

	return nil
}

// findSameZoneSubnets 从 cluster-level 的子网挑选和 node 同可用区的子网
func (c *Controller) findSameZoneSubnets(ctx context.Context, subnets []string) []string {
	filteredSubnets := make([]string, 0)
	for _, s := range subnets {
		resp, err := c.cloudClient.DescribeSubnet(ctx, s)
		if err != nil {
			log.Errorf(ctx, "findSameZoneSubnets: skip subnet %v due to describe error: %v", s, err)
			continue
		}
		if resp.ZoneName == c.availabilityZone {
			filteredSubnets = append(filteredSubnets, s)
			log.Infof(ctx, "add subnet %v at zone %v as node-level candidate", s, resp.ZoneName)
		}
	}

	return filteredSubnets
}

// patchENICapacityInfoToNode patches eni capacity info to node if not exists.
// so user can reset these values.
func (c *Controller) patchENICapacityInfoToNode(ctx context.Context, maxENINum, maxIPPerENI int) error {
	node, err := c.kubeClient.CoreV1().Nodes().Get(c.nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// in accordance with 1c1g bcc
	preAttachedENINum := mathutil.Min(DefaultPreAttachedENINum, maxENINum)

	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	isCreated := true

	if _, ok := node.Annotations[utileni.NodeAnnotationMaxENINum]; !ok {
		isCreated = false
		node.Annotations[utileni.NodeAnnotationMaxENINum] = strconv.Itoa(maxENINum)
	}

	if _, ok := node.Annotations[utileni.NodeAnnotationMaxIPPerENI]; !ok {
		isCreated = false
		node.Annotations[utileni.NodeAnnotationMaxIPPerENI] = strconv.Itoa(maxIPPerENI)
	}

	if _, ok := node.Annotations[utileni.NodeAnnotationWarmIPTarget]; !ok {
		isCreated = false
		// set default warm-ip-target
		node.Annotations[utileni.NodeAnnotationWarmIPTarget] = strconv.Itoa(maxIPPerENI - 1)
	}

	if _, ok := node.Annotations[utileni.NodeAnnotationPreAttachedENINum]; !ok {
		isCreated = false
		// set default pre-attached-eni-num
		node.Annotations[utileni.NodeAnnotationPreAttachedENINum] = strconv.Itoa(preAttachedENINum)
	}

	// patch annotations
	if !isCreated {
		json, err := json.Marshal(node.Annotations)
		if err != nil {
			return err
		}

		patchData := []byte(fmt.Sprintf(`{"metadata":{"annotations":%s}}`, json))
		_, err = c.kubeClient.CoreV1().Nodes().Patch(c.nodeName, ktypes.StrategicMergePatchType, patchData)
		if err != nil {
			return err
		}
	}

	return nil
}
