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

package bcc

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	ENIAttachTimeout     int = 50
	ENIAttachMaxRetry    int = 10
	ENIReadyTimeToAttach     = 5 * time.Second
)

// findSuitableENIs find suitable enis for pod
func (ipam *IPAM) findSuitableENIs(ctx context.Context, pod *v1.Pod) ([]*enisdk.Eni, error) {
	log.Infof(ctx, "start to find suitable enis for pod (%v/%v)", pod.Namespace, pod.Name)
	ipam.lock.RLock()
	defer ipam.lock.RUnlock()

	nodeName := pod.Spec.NodeName

	// get eni list from eniCache
	enis, ok := ipam.eniCache[nodeName]
	if !ok || len(enis) == 0 {
		return nil, fmt.Errorf("no eni binded to node %s", nodeName)
	}

	// only one eni just return
	if len(enis) == 1 {
		return enis, nil
	}

	// for fix IP pod, candidate subnets should be in the same subnet
	if isFixIPStatefulSetPod(pod) {
		wep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(pod.Namespace).Get(pod.Name)
		if err == nil {
			// get old subnet of pod
			oldSubnet := wep.Spec.SubnetID
			log.Infof(ctx, "pod (%v/%v) was in subnet %v before, will choose an eni in this subnet", pod.Namespace, pod.Name, oldSubnet)
			// find eni in the same old subnet
			enis = listENIsBySubnet(enis, oldSubnet)
			if len(enis) == 0 {
				msg := fmt.Sprintf("schedule error: node %v has no eni in subnet %v", nodeName, oldSubnet)
				log.Error(ctx, msg)
				return nil, errors.New(msg)
			}
			// only one eni just return
			if len(enis) == 1 {
				return enis, nil
			}
			log.Infof(ctx, "list enis in subnet %v: %+v", oldSubnet, log.ToJson(enis))
		} else {
			if !kerrors.IsNotFound(err) {
				log.Errorf(ctx, "failed to get wep of pod (%v %v): %v", pod.Namespace, pod.Name, err)
				return nil, err
			}
		}
	}

	// sort eni by ip num
	unstableSortENIByPrivateIPNum(enis)
	log.V(6).Infof(ctx, "find suitable enis %v for pod (%v/%v) successfully", log.ToJson(enis), pod.Namespace, pod.Name)

	return enis, nil
}

func unstableSortENIByPrivateIPNum(enis []*enisdk.Eni) {
	rand.Shuffle(len(enis), func(i, j int) {
		enis[i], enis[j] = enis[j], enis[i]
	})
	sort.Slice(enis, func(i, j int) bool {
		return len(enis[i].PrivateIpSet) < len(enis[j].PrivateIpSet)
	})
}

func listENIsBySubnet(enis []*enisdk.Eni, subnetID string) []*enisdk.Eni {
	result := make([]*enisdk.Eni, 0)

	for _, eni := range enis {
		if eni.SubnetId == subnetID {
			result = append(result, eni)
		}
	}

	return result
}

func (ipam *IPAM) increaseENIIfRequired(ctx context.Context, nodes []*v1.Node) error {
	enis, err := ipam.cloud.ListENIs(ctx, ipam.vpcID)
	if err != nil {
		log.Errorf(ctx, "failed to list enis when trying to increase eni: %v", err)
		return err
	}

	// check each node
	for _, node := range nodes {
		ctx := log.NewContext()
		log.V(6).Infof(ctx, "check node %v whether need to increase eni", node.Name)

		// skip BBC if cluster is hybrid
		instanceType := util.GetNodeInstanceType(node)
		if instanceType != metadata.InstanceTypeExBCC {
			log.V(6).Infof(ctx, "node %v has instance type %v, skip increasing eni", node.Name, instanceType)
			continue
		}

		// after creating eni, wait 5s to attach, the status will be available or attaching
		// check node has available/attaching eni, prevent duplicate creation
		if ipam.nodeHasNewlyCreatedENI(ctx, node, enis) {
			log.Infof(ctx, "node %v may have newly created eni, assume added in previous check, skip check this time", node.Name)
			continue
		}

		// list attached enis of node
		attachedENIs, err := listAttachedENIs(ctx, ipam.clusterID, node, enis)
		if err != nil {
			return err
		}

		// check whether node needs to create eni
		needNewENI, err := ipam.needToCreateNewENI(ctx, node, attachedENIs)
		if err != nil {
			return err
		}

		// TODO: there may be a potential concurrency issue
		if needNewENI {
			log.Infof(ctx, "ipam decided to add a new eni to node %v", node.Name)
			go func(n *v1.Node) {
				err := ipam.createOneENI(ctx, n)
				if err != nil {
					log.Errorf(ctx, "failed to create one eni for node %v: %v", n.Name, err)
				}
			}(node)
		}

	}
	return nil
}

func (ipam *IPAM) nodeHasNewlyCreatedENI(ctx context.Context, node *v1.Node, enis []enisdk.Eni) bool {
	hasNewENI := false
	instanceID, _ := util.GetInstanceIDFromNode(node)

	for _, eni := range enis {
		// if eni not owned by node, just ignore
		if !utileni.ENIOwnedByNode(&eni, ipam.clusterID, instanceID) {
			continue
		}

		// attaching or available
		if eni.Status == utileni.ENIStatusAvailable || eni.Status == utileni.ENIStatusAttaching {
			log.Infof(ctx, "node %v has unstable eni %v with status: %v", node.Name, eni.EniId, eni.Status)
			hasNewENI = true
			break
		}
	}

	return hasNewENI
}

func listAttachedENIs(ctx context.Context, clusterID string, node *v1.Node, enis []enisdk.Eni) ([]*enisdk.Eni, error) {
	var attachedENIs []*enisdk.Eni

	instanceID, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		return nil, err
	}

	// find out attached enis
	for idx, eni := range enis {
		// judge whether eni belongs to the node by eni.Name
		if !utileni.ENIOwnedByNode(&eni, clusterID, instanceID) {
			log.V(6).Infof(ctx, "eni(%v) does not owned by %s/%s/%s", eni.Name, clusterID, instanceID, node.Name)
			continue
		}

		// eni openapi may return with Status inuse but InstanceID empty.
		// we cannot handle this kind of situation
		if eni.Status == utileni.ENIStatusInuse && eni.InstanceId == "" {
			return nil, fmt.Errorf("invalid eni(%v): response: instanceID is empty", eni.Name)
		}

		if eni.Status == utileni.ENIStatusInuse && eni.InstanceId == instanceID {
			attachedENIs = append(attachedENIs, &enis[idx])
		}
	}
	return attachedENIs, nil
}

func (ipam *IPAM) needToCreateNewENI(ctx context.Context, node *v1.Node, attachedENIs []*enisdk.Eni) (bool, error) {
	preAttachedENINum, err := utileni.GetPreAttachedENINumFromNodeAnnotations(node)
	if err != nil {
		return false, err
	}
	// node attached enis less than pre-attached num
	if len(attachedENIs) < preAttachedENINum {
		msg := fmt.Sprintf("need to create eni due to: node %v has %v enis, less than pre-attached num %v", node.Name, len(attachedENIs), preAttachedENINum)
		log.Info(ctx, msg)
		ipam.eventRecorder.Event(node, v1.EventTypeNormal, "CreateNewENI", msg)
		return true, nil
	}

	maxENINum, err := utileni.GetMaxENINumFromNodeAnnotations(node)
	if err != nil {
		return false, err
	}
	maxIPPerENI, err := utileni.GetMaxIPPerENIFromNodeAnnotations(node)
	if err != nil {
		return false, err
	}
	// attached eni reach maximum, cannot add more
	if len(attachedENIs) >= maxENINum {
		return false, nil
	}

	ipNum, eniNum := ipam.getAvailableIPAndENINum(ctx, maxIPPerENI, attachedENIs)

	warmIPTarget, err := utileni.GetWarmIPTargetFromNodeAnnotations(node)
	if err != nil {
		return false, err
	}
	if ipNum < warmIPTarget {
		log.Warningf(ctx, "node %v has %v available IP(s) and %v ENI(s) if ignoring subnet that has no more ip", node.Name, ipNum, eniNum)
		msg := fmt.Sprintf("need to create eni due to: warm ip target(%v) not reached", warmIPTarget)
		log.Info(ctx, msg)
		ipam.eventRecorder.Event(node, v1.EventTypeNormal, "CreateNewENI", msg)
		return true, nil
	}

	return false, nil
}

// getAvailableIPAndENINum gets num of available ips from given enis
func (ipam *IPAM) getAvailableIPAndENINum(ctx context.Context, maxIPPerENI int, enis []*enisdk.Eni) (int, int) {
	var ipNum, eniNum int

	for _, eni := range enis {
		subnet, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(eni.SubnetId)
		if err != nil {
			log.Errorf(ctx, "failed to get subnet crd %v: %v", eni.SubnetId, err)
			continue
		}

		// if subnet has no more ip, the existed eni in it will not be taken into account
		if isSubnetHasNoMoreIP(subnet) {
			log.Infof(ctx, "skip subnet %v due to no more ip", subnet.Spec.ID)
			continue
		}

		eniNum++
		ipNum += maxIPPerENI - len(eni.PrivateIpSet)
	}

	return ipNum, eniNum
}

// findAllCandidateSubnetsOfNode find largest priority pools to support dynamic config
func (ipam *IPAM) findAllCandidateSubnetsOfNode(ctx context.Context, node *v1.Node) ([]*v1alpha1.Subnet, error) {
	var result []*v1alpha1.Subnet

	// list all pools
	pools, err := ipam.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	log.V(6).Infof(ctx, "list all pools: %v", log.ToJson(pools.Items))

	// find all matched pools
	matchedPools, err := utilippool.GetMatchedIPPoolsByNode(node, pools.Items)
	if err != nil {
		return nil, err
	}
	log.Infof(ctx, "list all matched pools: %v", poolNameList(matchedPools))

	// sort matched pools by priority
	sort.Slice(matchedPools, func(i, j int) bool {
		return matchedPools[i].Spec.Priority > matchedPools[j].Spec.Priority
	})

	// find matched pools with largest priority.
	// if multiple pools has the same priority, they are all selected out.
	priorPools := findLargestPriorityPools(matchedPools)
	if len(priorPools) > 0 {
		log.Infof(ctx, "list matched pools with largest priority %v: %v", priorPools[0].Spec.Priority, poolNameList(priorPools))
	}

	// aggregate subnets
	for _, pool := range priorPools {
		for _, s := range pool.Spec.ENI.Subnets {
			subnet, err := ipam.crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(s)
			if err != nil {
				log.Errorf(ctx, "failed to get subnet %v crd: %v", s, err)
				return nil, err
			}
			result = append(result, subnet)
		}
	}

	return result, nil
}

func poolNameList(pools []v1alpha1.IPPool) []string {
	var result []string
	for _, pool := range pools {
		result = append(result, pool.Name)
	}
	return result
}

// TODO: this can be optimized by binary search
func findLargestPriorityPools(pools []v1alpha1.IPPool) []v1alpha1.IPPool {
	if len(pools) == 0 {
		return nil
	}

	// pools have been sorted, search from tail to head
	maxPriority := pools[0].Spec.Priority
	for i := len(pools) - 1; i >= 0; i-- {
		if pools[i].Spec.Priority == maxPriority {
			return pools[:i+1]
		}
	}

	return []v1alpha1.IPPool{pools[0]}
}

func (ipam *IPAM) createOneENI(ctx context.Context, node *v1.Node) error {
	subnets, err := ipam.findAllCandidateSubnetsOfNode(ctx, node)
	if err != nil {
		log.Errorf(ctx, "failed to find all candidates subnets: %v", err)
		return err
	}

	log.V(6).Infof(ctx, "find all candidates subnets: %v", log.ToJson(subnets))

	// find a subnet to create eni
	var subnet string
	switch ipam.subnetSelectionPolicy {
	case SubnetSelectionPolicyMostFreeIP:
		subnet, err = ipam.findSubnetWithMostAvailableIP(ctx, subnets)
		if err != nil {
			log.Errorf(ctx, "failed to find subnet to create eni: %v", err)
			return err
		}
	case SubnetSelectionPolicyLeastENI:
		// list all enis in vpc
		enis, err := ipam.cloud.ListENIs(ctx, ipam.vpcID)
		if err != nil {
			log.Errorf(ctx, "failed to list enis in vpc %s: %v", ipam.vpcID, err)
			return err
		}
		subnet, err = ipam.findSubnetWithLeastENI(ctx, node, subnets, enis)
		if err != nil {
			log.Errorf(ctx, "failed to find subnet to create eni: %v", err)
			return err
		}
	default:
		return fmt.Errorf("unknown subnet selection policy: %v", ipam.subnetSelectionPolicy)
	}

	log.Infof(ctx, "subnet %v is chosen to create a new eni for node %v", subnet, node.Name)

	secGroups, err := ipam.getSecurityGroupsFromDefaultIPPool(ctx, node)
	if err != nil {
		return err
	}
	if len(secGroups) == 0 {
		return errors.New("security groups bound to eni cannot be empty")
	}

	eniID, err := ipam.allocateENI(ctx, node, subnet, secGroups)
	if err != nil {
		log.Errorf(ctx, "failed to allocate(create and attach) new eni: %v", err)
		return err
	}
	log.Infof(ctx, "allocate(create and attach) eni %v successfully", eniID)

	return nil
}

func (ipam *IPAM) findSubnetWithLeastENI(ctx context.Context, node *v1.Node, subnets []*v1alpha1.Subnet, enis []enisdk.Eni) (string, error) {
	if len(subnets) == 0 {
		return "", fmt.Errorf("eni subnets cannot be empty")
	}

	// filter out enabled subnets
	var qualifiedSubnets []string
	for _, subnet := range subnets {
		if isSubnetQualifiedToCreateENI(subnet) {
			qualifiedSubnets = append(qualifiedSubnets, subnet.Spec.ID)
		}
	}
	if len(qualifiedSubnets) == 0 {
		return "", fmt.Errorf("cannot find a qualified subnet to create eni")
	}

	// start to find
	candidate := qualifiedSubnets[util.Random(0, len(qualifiedSubnets))]
	spreadResult := make(map[string]int)

	for _, subnet := range qualifiedSubnets {
		spreadResult[subnet] = 0
	}

	instanceID, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		return "", err
	}

	for _, eni := range enis {
		if !utileni.ENIOwnedByNode(&eni, ipam.clusterID, instanceID) {
			continue
		}
		_, ok := spreadResult[eni.SubnetId]
		if ok {
			spreadResult[eni.SubnetId]++
		} else {
			log.Warningf(ctx, "eni %v in subnet %v is not in candidate subnets: %v", eni.EniId, eni.SubnetId, subnets)
		}
	}

	log.Infof(ctx, "eni spread result of node %v: %+v", node.Name, spreadResult)

	// find the least one
	for subnet, cnt := range spreadResult {
		if cnt < spreadResult[candidate] {
			candidate = subnet
		}
	}

	return candidate, nil
}

func (ipam *IPAM) findSubnetWithMostAvailableIP(ctx context.Context, subnets []*v1alpha1.Subnet) (string, error) {
	if len(subnets) == 0 {
		return "", fmt.Errorf("eni subnets cannot be empty")
	}

	// filter out enabled subnets
	var qualifiedSubnets []*v1alpha1.Subnet
	for _, subnet := range subnets {
		if isSubnetQualifiedToCreateENI(subnet) {
			qualifiedSubnets = append(qualifiedSubnets, subnet)
		}
	}
	if len(qualifiedSubnets) == 0 {
		return "", fmt.Errorf("cannot find a qualified subnet to create eni")
	}

	// start to find
	candidate := qualifiedSubnets[util.Random(0, len(qualifiedSubnets))]
	// find the most one
	for _, subnet := range subnets {
		if subnet.Status.AvailableIPNum > candidate.Status.AvailableIPNum {
			candidate = subnet
		}
	}

	return candidate.Spec.ID, nil
}

func (ipam *IPAM) allocateENI(ctx context.Context, node *v1.Node, subnetID string, SecurityGroupIDs []string) (string, error) {
	start := time.Now()
	log.Infof(ctx, "allocate eni for node %v begins...", node.Name)
	defer log.Infof(ctx, "allocate eni for node %v ends...", node.Name)

	instanceID, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		return "", err
	}

	eniName := utileni.CreateNameForENI(ipam.clusterID, instanceID, node.Name)
	createENIArgs := &enisdk.CreateEniArgs{
		Name:             eniName,
		SubnetId:         subnetID,
		SecurityGroupIds: SecurityGroupIDs,
		PrivateIpSet: []enisdk.PrivateIp{{
			Primary:          true,
			PrivateIpAddress: "",
		}},
		Description: "auto created by cce-cni, do not modify",
	}

	eniID, err := ipam.cloud.CreateENI(ctx, createENIArgs)
	if err != nil {
		log.Errorf(ctx, "failed to create eni for node %v: %v", node.Name, err)
		if cloud.IsErrorSubnetHasNoMoreIP(err) {
			if e := ipam.declareSubnetHasNoMoreIP(ctx, subnetID, true); e != nil {
				log.Errorf(ctx, "failed to patch subnet %v that has no more ip: %v", subnetID, e)
			}
		}
		return "", err
	}
	log.Infof(ctx, "create eni %v for node %v successfully", eniID, node.Name)
	// wait for eni ready, or attach may fail
	time.Sleep(ENIReadyTimeToAttach)

	err = ipam.attachENIWithTimeout(ctx, node, eniID)
	if err != nil {
		log.Errorf(ctx, "failed to attach eni %v: %v", eniID, err)
		return eniID, err
	}

	log.Infof(ctx, "allocate eni %v for node %v successfully, takes %f s to finish", eniID, node.Name, time.Since(start).Seconds())
	return eniID, nil
}

func (ipam *IPAM) attachENIWithTimeout(ctx context.Context, node *v1.Node, eniID string) error {
	start := time.Now()
	instanceID, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		return err
	}
	log.Infof(ctx, "attach eni %v to instance %v begins...", eniID, instanceID)
	defer log.Infof(ctx, "attach eni %v to instance %v ends...", eniID, instanceID)

	err = ipam.cloud.AttachENI(ctx, &enisdk.EniInstance{
		InstanceId: instanceID,
		EniId:      eniID,
	})
	if err != nil {
		log.Errorf(ctx, "failed to attach eni %v to instance %v: %v", eniID, instanceID, err)
		_ = ipam.rollbackFailedENI(ctx, eniID)
		return err
	}
	log.Infof(ctx, "request of attaching eni %v to instance %v is sent successfully", eniID, instanceID)

	// stat eni synchronously to check status
	sleepTime := time.Duration(ENIAttachTimeout/ENIAttachMaxRetry) * time.Second

	for i := 0; i < ENIAttachMaxRetry; i++ {
		statResp, err := ipam.cloud.StatENI(ctx, eniID)
		if err != nil {
			log.Errorf(ctx, "failed to stat eni %v while attaching: %v", eniID, err)
			time.Sleep(sleepTime)
			continue
		}

		log.Infof(ctx, "attempt(%d/%d) to stat eni %v, current status: %v, time consuming: %f s", i+1, ENIAttachMaxRetry, eniID, statResp.Status, time.Since(start).Seconds())

		if statResp.Status == utileni.ENIStatusInuse {
			log.Infof(ctx, "attach eni %v to instance %v successfully", eniID, instanceID)
			return nil
		}
		if statResp.Status == utileni.ENIStatusAvailable || statResp.Status == utileni.ENIStatusDetaching {
			msg := fmt.Sprintf("failed to attach eni %v while attaching due to wrong status: %v", eniID, statResp.Status)
			log.Error(ctx, msg)
			_ = ipam.rollbackFailedENI(ctx, eniID)
			return errors.New(msg)
		}

		time.Sleep(sleepTime)
	}
	// attach eni timeout
	msg := fmt.Sprintf("failed to attach eni %v due to timeout", eniID)
	log.Error(ctx, msg)
	_ = ipam.rollbackFailedENI(ctx, eniID)
	return errors.New(msg)
}

func (ipam *IPAM) rollbackFailedENI(ctx context.Context, eniID string) error {
	log.Infof(ctx, "rollback: delete failed eni %v begins...", eniID)
	defer log.Infof(ctx, "rollback: delete failed eni %v ends...", eniID)

	err := ipam.cloud.DeleteENI(ctx, eniID)
	if err != nil {
		log.Errorf(ctx, "resource may leak, failed to delete eni %v: %v", eniID, err)
		return err
	}
	return nil
}

func (ipam *IPAM) getSecurityGroupsFromDefaultIPPool(ctx context.Context, node *v1.Node) ([]string, error) {
	ippoolName := utilippool.GetNodeIPPoolName(node.Name)

	ippool, err := ipam.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(ctx, ippoolName, metav1.GetOptions{})
	if err != nil {
		log.Errorf(ctx, "failed to get ippool %v: %v", ippoolName, err)
		return nil, err
	}

	return ippool.Spec.ENI.SecurityGroups, nil
}

func isSubnetQualifiedToCreateENI(subnet *v1alpha1.Subnet) bool {
	return subnet.Status.Enable && !subnet.Status.HasNoMoreIP
}

func isSubnetHasNoMoreIP(subnet *v1alpha1.Subnet) bool {
	return subnet.Status.HasNoMoreIP
}
