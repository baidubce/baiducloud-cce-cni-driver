// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/baidu/jpaas-caas/baiducloud-cce-cni-driver/pkg/eniipam/ipam/bcc/psts.go - pod with topology spread

// modification history
// --------------------
// 2022/08/01, by wangeweiwei22, create psts

// If the pod is marked to use the pod topology distribution,
// the IP is assigned to the pod according to the topology distribution declared by the pod

package bcc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
)

func (ipam *IPAM) subnetTopologyAllocates(ctx context.Context, pod *corev1.Pod, enis []*enisdk.Eni, oldWep *networkingv1alpha1.WorkloadEndpoint, nodeName, containerID string) (newWep *networkingv1alpha1.WorkloadEndpoint, err error) {
	if len(enis) == 0 {
		return nil, fmt.Errorf("no suitable eni")
	}
	psts, err := ipam.tsCtl.Get(pod.GetNamespace(), networking.GetPodSubnetTopologySpreadName(pod))
	if err != nil {
		log.Errorf(ctx, "failed to get PodSubnetTopologySpread of pod (%v/%v): %v", pod.Namespace, pod.Name, err)
		return nil, err
	}
	if !networking.PSTSContainsAvailableSubnet(psts) {
		return nil, fmt.Errorf("PodSubnetTopologySpread (%s/%s) have none available subnet", psts.GetNamespace(), psts.GetName())
	}
	log.Infof(ctx, "try to use PodSubnetTopologySpread (%s/%s) for pod (%v/%v)", psts.GetNamespace(), psts.GetName(), pod.GetNamespace(), pod.GetName())

	var toAllocate *ipToAllocate = &ipToAllocate{
		eni:              enis[0],
		psts:             psts,
		candidateSubnets: filterAvailableSubnet(ctx, enis[0], psts.Status.AvailableSubnets),
		backoffCap:       ipamgeneric.CCECniTimeout / time.Duration(len(enis)),
		nodeName:         nodeName,
		pod:              pod,
	}
	if len(toAllocate.candidateSubnets) == 0 {
		return nil, fmt.Errorf("PodSubnetTopologySpread (%s/%s) no available subnet", psts.GetNamespace(), psts.GetName())
	}

	needSubnetLock := false
	defer func() {
		if needSubnetLock {
			ipam.unlockExclusiveSubnet(toAllocate.candidateSubnets)
		}
	}()
	// fixed ip mode and suffix of pod name is number
	if networking.IsFixedIPMode(psts) && networking.IsEndWithNum(pod.GetName()) {
		needSubnetLock = true
		ipam.lockExclusiveSubnet(toAllocate.candidateSubnets)
		err = ipam.fixedAllocateIPCrossSubnet(ctx, toAllocate, oldWep)
	}
	if networking.IsManualMode(psts) {
		needSubnetLock = true
		ipam.lockExclusiveSubnet(toAllocate.candidateSubnets)
		err = ipam.manualAllocateIPCrossSubnet(ctx, toAllocate)
	}
	// allocate ip with auto mode
	if networking.IsElasticMode(psts) {
		err = ipam.elasticAllocateIPCrossSubnet(ctx, toAllocate, enis)
	}
	if err != nil {
		log.Errorf(ctx, "%s error : %v", toAllocate.info(), err)
		return nil, err
	}

	if toAllocate.ipv4Result == "" && toAllocate.ipv6Result == "" {
		return nil, fmt.Errorf("all subnets (%v) bind enis (%v) failed", toAllocate.candidateSubnets, enis)
	}

	return ipam.__saveIPAllocationToWEP(ctx, toAllocate, oldWep, containerID)
}

func (ipam *IPAM) __saveIPAllocationToWEP(ctx context.Context, toAllocate *ipToAllocate, oldWep *networkingv1alpha1.WorkloadEndpoint, containerID string) (newWep *networkingv1alpha1.WorkloadEndpoint, err error) {
	var (
		pod  = toAllocate.pod
		psts = toAllocate.psts
	)
	// build new workload endpoint
	newWep = &networkingv1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels: map[string]string{
				ipamgeneric.WepLabelSubnetIDKey:     toAllocate.sbnID,
				ipamgeneric.WepLabelInstanceTypeKey: string(metadata.InstanceTypeExBCC),
			},
			Finalizers: []string{ipamgeneric.WepFinalizer},
		},
		Spec: networkingv1alpha1.WorkloadEndpointSpec{
			ContainerID:             containerID,
			IP:                      toAllocate.ipv4Result,
			IPv6:                    toAllocate.ipv6Result,
			Type:                    ipamgeneric.WepTypePod,
			Mac:                     toAllocate.eni.MacAddress,
			ENIID:                   toAllocate.eni.EniId,
			Node:                    pod.Spec.NodeName,
			SubnetID:                toAllocate.sbnID,
			UpdateAt:                metav1.Time{Time: ipam.clock.Now()},
			EnableFixIP:             strconv.FormatBool(networking.IsFixedIPMode(psts)),
			FixIPDeletePolicy:       string(networking.GetReleaseStrategy(psts)),
			ENISubnetID:             toAllocate.eni.SubnetId,
			SubnetTopologyReference: psts.GetName(),
		},
	}

	// use fixed ip mode
	if networking.IsFixedIPMode(psts) && networking.IsEndWithNum(pod.GetName()) {
		newWep.Spec.Type = ipamgeneric.WepTypeSts
		newWep.Spec.EnableFixIP = EnableFixIPTrue
		newWep.Labels[ipamgeneric.WepLabelStsOwnerKey] = util.GetStsName(newWep)
	}

	// to rollback if update wep error
	if oldWep != nil {
		newWep.ResourceVersion = oldWep.ResourceVersion
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(newWep.GetNamespace()).Update(ctx, newWep, metav1.UpdateOptions{})
	} else {
		// create new
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(newWep.GetNamespace()).Create(ctx, newWep, metav1.CreateOptions{})
	}
	if err != nil {
		goto delIP
	}
	log.Infof(ctx, "create wep with spec %+v for pod (%v %v) successfully", newWep.Spec, pod.GetNamespace(), pod.GetName())

	// update allocated pod cache
	ipam.lock.Lock()
	defer ipam.lock.Unlock()
	if ipam.removeAddIPBackoffCache(newWep.Spec.ENIID, true) {
		log.Infof(ctx, "remove backoff for eni %v when handling pod (%v %v) due to successful ip allocate", newWep.Spec.ENIID, pod.GetNamespace(), pod.GetName())
	}
	if toAllocate.ipv4Result != "" {
		ipam.allocated[toAllocate.ipv4Result] = newWep
	}
	if toAllocate.ipv6Result != "" {
		ipam.allocated[toAllocate.ipv6Result] = newWep
	}
	return newWep, nil

	// update wep error
delIP:
	ipam.rollbackIPAllocated(ctx, toAllocate, err, newWep)
	return nil, err
}

func (ipam *IPAM) rollbackIPAllocated(ctx context.Context, toAllocate *ipToAllocate, err error, newWep *networkingv1alpha1.WorkloadEndpoint) {
	ipResults := []string{toAllocate.ipv4Result, toAllocate.ipv6Result}
	for _, ipResult := range ipResults {
		if ipResult != "" {
			log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", toAllocate.pod.GetNamespace(), toAllocate.pod.GetName(), err)
			delErr := ipam.tryDeleteSubnetIPRetainAllocateCache(ctx, newWep)
			if delErr != nil {
				log.Errorf(ctx, "rollback: error deleting private IP %v for pod (%v %v): %v", ipResult, toAllocate.pod.GetNamespace(), toAllocate.pod.GetName(), delErr)
			}
		}
	}
}

// The fixed IP list may have been modified by the user.
// The previous fixed IP is no longer in the latest list.
// At this time, try to allocate IP again
func (ipam *IPAM) fixedAllocateIPCrossSubnet(ctx context.Context, toAllocate *ipToAllocate, oldWep *networkingv1alpha1.WorkloadEndpoint) error {
	if networking.OwnerByPodSubnetTopologySpread(oldWep, toAllocate.psts) {
		ipam.tryDeleteSubnetIPRetainAllocateCache(ctx, oldWep)

		toAllocate.ipv4 = oldWep.Spec.IP
		toAllocate.sbnID = oldWep.Spec.SubnetID

		if !networking.PSTSContainersIP(toAllocate.ipv4, toAllocate.psts) {
			toAllocate.ipv4 = ""
		}
	}
	return ipam.manualAllocateIPCrossSubnet(ctx, toAllocate)
}

// only private subnets can use fixed IP
// BCE does not support IP migration between Enis,
// there have a gap between IP release and allocation.
// In this gap, other requests may allocate IP from this subnet,
// resulting in fixed IP being preempted by other clients.
func (ipam *IPAM) elasticAllocateIPCrossSubnet(ctx context.Context, toAllocate *ipToAllocate, enis []*enisdk.Eni) error {
	for i := 0; i < len(enis); i++ {
		toAllocate.eni = enis[i]
		for _, sbnID := range toAllocate.candidateSubnets {
			toAllocate.sbnID = sbnID
			toAllocate.needIPv4 = true
			info := toAllocate.info()

			sbn, _ := ipam.sbnCtl.Get(toAllocate.sbnID)
			if sbn == nil || sbn.Spec.Exclusive {
				return fmt.Errorf("%s error: subnet (%s) is exclusive. It is forbidden to allocate IP", info, toAllocate.sbnID)
			}
			err := ipam.tryAllocateIPsCrossSubnet(ctx, toAllocate)
			if err != nil {
				return fmt.Errorf("%s error : %w", toAllocate.info(), err)
			}
			return nil
		}
	}
	return nil
}

func (ipam *IPAM) manualAllocateIPCrossSubnet(ctx context.Context, toAllocate *ipToAllocate) error {
	// Use fixed IP for the first time and preempt IP from the IP list
	if toAllocate.ipv4 == "" {
		if len(toAllocate.candidateSubnets) == 0 {
			return fmt.Errorf("no available subnet")
		}
		ipam.priorityIPAndSubnet(ctx, toAllocate)
	}

	if toAllocate.sbnID == "" {
		return fmt.Errorf("no available ip in subnets (%v). It is forbidden to allocate IP manual", toAllocate.candidateSubnets)
	}

	// only private subnets can use fixed IP
	// BCE does not support IP migration between Enis,
	// there have a gap between IP release and allocation.
	// In this gap, other requests may allocate IP from this subnet,
	// resulting in fixed IP being preempted by other clients.
	sbn, _ := ipam.sbnCtl.Get(toAllocate.sbnID)
	if sbn == nil || !sbn.Spec.Exclusive {
		return fmt.Errorf("subnet (%s) is not exclusive. It is forbidden to allocate IP manual", toAllocate.sbnID)
	}

	return ipam.tryAllocateIPsCrossSubnet(ctx, toAllocate)

}

// tryAllocateIPsCrossSubnet allocate ipv4 oand ipv6 by toAllocate.ipv4 and toAllocate.ipv6
// it will try allocate elstic ipv4 address if value of toAllocate.needIPv4 euqal to true
// it will try allocate elstic ipv6 address if value of toAllocate.needIPv6 euqal to true
// result of allocation will be writed at toAllocate.ipv4Result and toAllocate.ipv6Result
func (ipam *IPAM) tryAllocateIPsCrossSubnet(ctx context.Context, toAllocate *ipToAllocate) error {
	info := toAllocate.info()
	var (
		enableIPv4 = toAllocate.ipv4 != "" || toAllocate.needIPv4
		enableIPv6 = toAllocate.ipv6 != "" || toAllocate.needIPv6
	)

	if enableIPv4 {
		ipResult, err := ipam.__allocateIPCrossSubnet(ctx, toAllocate.eni, toAllocate.ipv4, toAllocate.sbnID, toAllocate.backoffCap, toAllocate.nodeName)
		if err != nil {
			return fmt.Errorf("%s error: %v", info, err.Error())
		}
		log.Infof(ctx, "%s ipv4 %s success", info, ipResult)
		toAllocate.ipv4Result = ipResult
	}

	if enableIPv6 {
		ipResult, err := ipam.__allocateIPCrossSubnet(ctx, toAllocate.eni, toAllocate.ipv6, toAllocate.sbnID, toAllocate.backoffCap, toAllocate.nodeName)
		if err != nil {
			return fmt.Errorf("%s error: %v", info, err.Error())
		}
		log.Infof(ctx, "%s ipv4 %s success", info, ipResult)
		toAllocate.ipv6Result = ipResult
	}
	return nil
}

// priorityIPAndSubnet
// The optimization strategy mainly selects the subnet with
// the most candidate IP as the priority subnet when there are
// multiple subnets according to the sorting rules. The rule
// of preferred IP is to select the first unused IP under the subnet
func (ipam *IPAM) priorityIPAndSubnet(ctx context.Context, toAllocate *ipToAllocate) {
	ipam.lock.RLock()
	defer ipam.lock.RUnlock()

	var (
		ips []string
	)

	ipv4Map := ipam.filterCandidateIPs(ctx, toAllocate.psts, toAllocate.candidateSubnets, 4)
	for id, arr := range ipv4Map {
		if len(arr) > len(ips) {
			toAllocate.sbnID = id
			ips = arr
		}
	}
	if len(ips) > 0 {
		toAllocate.ipv4 = ips[0]
	}

	ips = make([]string, 0)
	ipv6Map := ipam.filterCandidateIPs(ctx, toAllocate.psts, toAllocate.candidateSubnets, 6)
	for _, arr := range ipv6Map {
		if len(arr) > len(ips) {
			ips = arr
		}
	}
	if len(ips) > 0 {
		toAllocate.ipv6 = ips[0]
	}
}

// lockExclusiveSubnet lock the subnet to prevent concurrent preemption of fixed IP
// In the unallocated fixed IP queue, the allocator always selects
// the IP at the head of the queue to try to allocate to the pod,
// so we lock here to prevent invalid preemption.
func (ipam *IPAM) lockExclusiveSubnet(subnetIDs []string) {
	ipam.exclusiveSubnetCond.L.Lock()
	for !ipam.__checkExclusiveSubnetCondition(subnetIDs) {
		ipam.exclusiveSubnetCond.Wait()
	}
	ipam.__updateExclusiveSubnetCondition(true, subnetIDs)
	ipam.exclusiveSubnetCond.L.Unlock()
}

func (ipam *IPAM) unlockExclusiveSubnet(subnetIDs []string) {
	ipam.exclusiveSubnetCond.L.Lock()

	ipam.__updateExclusiveSubnetCondition(false, subnetIDs)
	ipam.exclusiveSubnetCond.L.Unlock()
	ipam.exclusiveSubnetCond.Broadcast()
}

func (ipam *IPAM) __checkExclusiveSubnetCondition(subnetIDs []string) bool {
	for _, key := range subnetIDs {
		lock, ok := ipam.exclusiveSubnetFlag[key]
		if ok && lock {
			return false
		}
	}
	return true
}

func (ipam *IPAM) __updateExclusiveSubnetCondition(use bool, subnetIDs []string) {
	for _, key := range subnetIDs {
		ipam.exclusiveSubnetFlag[key] = use
	}
}

// filterAvailableSubnet keep the subnet that is in the same zone as eni and enabled
func filterAvailableSubnet(ctx context.Context, eni *enisdk.Eni, availableSubnets map[string]networkingv1alpha1.SubnetPodStatus) []string {
	var candidateSubnets []string
	for sbnID, sbnStatus := range availableSubnets {
		if sbnStatus.AvailabilityZone != eni.ZoneName {
			log.V(3).Infof(ctx, "filter subnet %s in zone %s", sbnID, sbnStatus.AvailabilityZone)
			continue
		}
		if sbnStatus.HasNoMoreIP {
			log.V(3).Infof(ctx, "filter subnet %s with no more ip %s", sbnID, sbnStatus.AvailabilityZone)
			continue
		}
		candidateSubnets = append(candidateSubnets, sbnID)
	}

	// Sort according to the number of available IPS. The subnet with more available IPS will be ranked at the top
	sort.Slice(candidateSubnets, func(i, j int) bool {
		return availableSubnets[candidateSubnets[i]].AvailableIPNum > availableSubnets[candidateSubnets[j]].AvailableIPNum
	})
	return candidateSubnets
}

// filtercindidateIPs
// origin: all of subnet and ip
// candidateSubnets: subnet to filter
// return: ip can be allocated
func (ipam *IPAM) filterCandidateIPs(ctx context.Context, psts *networkingv1alpha1.PodSubnetTopologySpread, candidateSubnets []string, version int) map[string][]string {
	var condidateIPs map[string][]string = make(map[string][]string)

	for _, sbnID := range candidateSubnets {
		set := sets.NewString()
		if sa, ok := psts.Spec.Subnets[sbnID]; ok {
			var (
				ipArray []string = sa.IPv4
				ipRange []string = sa.IPv4Range
				sbnCIDR string   = ""
			)
			sbn, _ := ipam.sbnCtl.Get(sbnID)
			if sbn != nil {
				sbnCIDR = sbn.Spec.CIDR
			} else {
				continue
			}

			if version == 6 {
				ipArray = sa.IPv6
				ipRange = sa.IPv6Range
			}
			for _, ranges := range ipRange {
				ips := cidr.ListIPsFromCIDRString(ranges)
				for _, ip := range ips {
					if cidr.IsUnicastIP(ip, sbnCIDR) {
						ipArray = append(ipArray, ip.String())
					}
				}
			}

			for _, ip := range ipArray {
				if _, ok := ipam.allocated[ip]; !ok {
					set.Insert(ip)
				}
			}

		}
		condidateIPs[sbnID] = set.List()
	}
	return condidateIPs
}

func (ipam *IPAM) __allocateIPCrossSubnet(ctx context.Context, eni *enisdk.Eni, ipToAllocate, subnetID string, backoffCap time.Duration, nodeName string) (string, error) {
	var (
		ipResult []string
		err      error
	)
	allocIPMaxTry := 3
	for i := 0; i < allocIPMaxTry; i++ {
		ipam.tryBackoff(eni.EniId)
		log.Infof(ctx, "try to add IP %v to %v cross subnet", ipToAllocate, eni.EniId)
		if ipToAllocate != "" {
			ipResult, err = ipam.__batchAddPrivateIpCrossSubnet(ctx, eni.EniId, subnetID, []string{ipToAllocate}, 1)
		} else {
			ipResult, err = ipam.__batchAddPrivateIpCrossSubnet(ctx, eni.EniId, subnetID, []string{}, 1)
		}

		if err != nil {
			log.Warningf(ctx, "failed to allocate private IP %v cross subnet %s on eni %s : %v", ipToAllocate, subnetID, eni.EniId, err)

			// If the current Eni does not have enough memory,
			// try to release idle IP through degradation to meet the cross subnet IP allocation
			if cloud.IsErrorVmMemoryCanNotAttachMoreIpException(err) {
				aerr := ipam.tryReleaseIdleIP(ctx, nodeName, eni.EniId)
				if aerr == nil {
					// do retry again
					continue
				}
			}

			if cloud.IsErrorSubnetHasNoMoreIP(err) {
				if e := ipam.sbnCtl.DeclareSubnetHasNoMoreIP(ctx, eni.SubnetId, true); e != nil {
					log.Errorf(ctx, "failed to patch subnet %v that has no more ip: %v", eni.SubnetId, e)
				}
			}

			if cloud.IsErrorRateLimit(err) || cloud.IsErrorSubnetHasNoMoreIP(err) {
				ipam.addIPBackoff(eni.EniId, backoffCap)
				continue
			}

			return "", err
		} else if err == nil {
			break
		}
	}

	if len(ipResult) < 1 {
		msg := "unexpected result from eni openapi cross subnet: at least one ip should be added"
		log.Error(ctx, msg)
		return "", errors.New(msg)
	}

	log.Infof(ctx, "add private IP cross subnet %v successfully", ipResult)

	ipam.datastore.Synchronized(func() error {
		for _, ip := range ipResult {
			// ipam.incPrivateIPNumCache(eni.EniId, false)
			ipam.datastore.AddPrivateIPToStoreUnsafe(nodeName, eni.EniId, ip, true, true)
			ipam.incPrivateIPNumCache(eni.EniId, true)
		}
		return nil
	})

	metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, eni.SubnetId).Inc()

	return ipResult[0], nil
}

func (ipam *IPAM) tryBackoff(eniid string) {
	var toSleep time.Duration
	ipam.lock.RLock()
	if v, ok := ipam.addIPBackoffCache[eniid]; ok {
		toSleep = v.Step()
	}
	ipam.lock.RUnlock()
	if toSleep != 0 {
		time.Sleep(toSleep)
	}
}

func (ipam *IPAM) __batchAddPrivateIpCrossSubnet(ctx context.Context, eniID, subnetID string, privateIPs []string, batchAddNum int) ([]string, error) {
	ipam.bucket.Wait(1)
	return ipam.cloud.BatchAddPrivateIpCrossSubnet(ctx, eniID, subnetID, privateIPs, batchAddNum)
}

func (ipam *IPAM) reconcileRelationOfWepEni(stopCh <-chan struct{}) error {
	return wait.PollImmediateUntil(wait.Jitter(ipam.eniSyncPeriod, 0.5), func() (bool, error) {
		ipam.syncRelationOfWepEni()
		return false, nil
	}, stopCh)
}

// Fix the data consistency of wep and eni,
// which causes that the IP address cannot be recycled correctly
func (ipam *IPAM) syncRelationOfWepEni() {
	ctx := logger.NewContext()
	wepLister := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister()

	var toUpdateWeps []*networkingv1alpha1.WorkloadEndpoint

	ipam.lock.RLock()
	for _, enis := range ipam.eniCache {
		for _, eni := range enis {
			for _, addr := range eni.PrivateIpSet {
				if wep, ok := ipam.allocated[addr.PrivateIpAddress]; ok {
					wep, err := wepLister.WorkloadEndpoints(wep.Namespace).Get(wep.Name)
					if err != nil {
						continue
					}
					if wep.Spec.ENIID != eni.EniId &&
						wep.Spec.UpdateAt.Add(2*ipam.eniSyncPeriod).Before(time.Now()) {
						wep = wep.DeepCopy()
						wep.Spec.ENIID = eni.EniId
						wep.Spec.UpdateAt = metav1.Now()
						toUpdateWeps = append(toUpdateWeps, wep)
					}
				}
			}
		}
	}
	ipam.lock.RUnlock()

	for _, wep := range toUpdateWeps {
		if !isFixIPStatefulSetPodWep(wep) {
			continue
		}
		ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(ctx, wep, metav1.UpdateOptions{})
		logger.Infof(ctx, "repairFixedIPWepRelationOfEni to wep(%s/%s)", wep.Namespace, wep.Name)
	}
}

type ipToAllocate struct {
	// candadite ip to be allocated
	ipv4 string
	ipv6 string

	// allocate ip if candadite ip of ipv4 or ipv6 is empty
	needIPv4 bool
	needIPv6 bool

	// param to allocate ip
	sbnID            string
	candidateSubnets []string
	pod              *corev1.Pod
	psts             *networkingv1alpha1.PodSubnetTopologySpread
	eni              *enisdk.Eni
	nodeName         string
	backoffCap       time.Duration

	// ip allocate result
	ipv4Result string
	ipv6Result string
}

func (toAllocate *ipToAllocate) info() string {
	return fmt.Sprintf("pod(%s/%s) allocate fixed ip (%s) from subnet(%s) to eni (%s) by PodSubnetTopologySpread(%s/%s)", toAllocate.pod.GetNamespace(), toAllocate.pod.GetName(), toAllocate.ipv4, toAllocate.sbnID, toAllocate.eni.EniId, toAllocate.psts.GetNamespace(), toAllocate.psts.GetName())
}
