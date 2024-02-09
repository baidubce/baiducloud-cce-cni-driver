package bcc

import (
	"context"
	"fmt"
	"net"

	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	err := wait.PollImmediateUntil(wait.Jitter(ipam.gcPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()

		stsList, err := ipam.kubeInformer.Apps().V1().StatefulSets().Lister().List(labels.Everything())
		if err != nil {
			log.Errorf(ctx, "gc: error list sts in cluster: %v", err)
			return false, nil
		}

		wepSelector, err := wepListerSelector()
		if err != nil {
			log.Errorf(ctx, "gc: error parsing requirement: %v", err)
			return false, nil
		}

		wepList, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().List(wepSelector)
		if err != nil {
			log.Errorf(ctx, "gc: error list wep in cluster: %v", err)
			return false, nil
		}

		// release wep if sts is deleted
		ipam.gcDeletedSts(ctx, wepList)
		// release wep if sts scale down
		ipam.gcScaledDownSts(ctx, stsList)
		// release non-sts wep if pod not found
		ipam.gcLeakedPod(ctx, wepList)
		ipam.gcLeakedIPPool(ctx)
		ipam.gcLeakedIP(ctx)
		ipam.gcDeletedNode(ctx)

		return false, nil
	}, stopCh)

	if err != nil {
		return err
	}
	return nil
}

func (ipam *IPAM) deletePrivateIP(ctx context.Context, privateIP string, eniID string) error {
	ipam.bucket.Wait(1)
	return ipam.cloud.DeletePrivateIP(ctx, privateIP, eniID)
}

func (ipam *IPAM) gcLeakedIP(ctx context.Context) error {
	// list all pods
	pods, err := ipam.kubeInformer.Core().V1().Pods().Lister().List(labels.Everything())
	if err != nil {
		log.Errorf(ctx, "gc: error list pods in cluster: %v", err)
		return err
	}

	// list all weps whose owner is sts
	requirement, _ := labels.NewRequirement(ipamgeneric.WepLabelStsOwnerKey, selection.Exists, nil)
	selector := labels.NewSelector().Add(*requirement)
	stsWeps, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(v1.NamespaceAll).List(selector)
	if err != nil {
		log.Errorf(ctx, "gc: failed to list wep with selector: %v: %v", selector.String(), err)
		return err
	}

	var (
		podIPSet    = sets.NewString()
		stsPodIPSet = sets.NewString()
	)

	// store pod ip temporarily
	for _, pod := range pods {
		if !pod.Spec.HostNetwork && !k8sutil.IsPodFinished(pod) {
			if pod.Status.PodIP != "" {
				podIPSet.Insert(pod.Status.PodIP)
			} else {
				wep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(pod.Namespace).Get(pod.Name)
				if err != nil {
					podPhase := string(pod.Status.Phase)
					if pod.DeletionTimestamp != nil && pod.Status.Reason == "NodeLost" {
						podPhase = string(v1.PodUnknown)
					} else if pod.DeletionTimestamp != nil {
						podPhase = "Terminating"
					}
					log.Warningf(ctx, "gc: failed to get wep (%v/%v) with pod phase \"%v\": %v", pod.Namespace, pod.Name, podPhase, err)
					continue
				}
				podIPSet.Insert(wep.Spec.IP)
				log.Warningf(ctx, "gc: pod %v may be pending in the pod phase of pulling image, and the IP %v is not leaked", pod.Name, wep.Spec.IP)
			}
		}
	}

	// store sts pod ip temporarily
	for _, wep := range stsWeps {
		stsPodIPSet.Insert(wep.Spec.IP)
	}

	// build leaked ip cache
	ipam.buildPossibleLeakedIPCache(ctx, podIPSet, stsPodIPSet)

	// prune leaked ip
	ipam.pruneExpiredLeakedIP(ctx)

	return nil
}

func (ipam *IPAM) buildPossibleLeakedIPCache(ctx context.Context, podIPSet, stsPodIPSet sets.String) {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	for nodeName, enis := range ipam.eniCache {
		idleIPs, err := ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
		if err != nil {
			log.Errorf(ctx, "gc: failed to get idle ips in datastore of node %v: %v", nodeName, err)
		}
		idleIPSet := sets.NewString(idleIPs...)

		for _, eni := range enis {
			for _, ip := range eni.PrivateIpSet {
				if !ip.Primary {
					key := eniAndIPAddrKey{nodeName, eni.EniId, ip.PrivateIpAddress}
					// ip not in pod and neither in sts wep, nor in idle pool
					if !podIPSet.Has(ip.PrivateIpAddress) && !stsPodIPSet.Has(ip.PrivateIpAddress) && !idleIPSet.Has(ip.PrivateIpAddress) {
						if _, ok := ipam.possibleLeakedIPCache[key]; !ok {
							ipam.possibleLeakedIPCache[key] = ipam.clock.Now()
							log.Warningf(ctx, "gc: eni %v on node %v may has IP %v leaked", eni.EniId, nodeName, ip.PrivateIpAddress)
						}
					} else {
						// if ip in pod, maybe a false positive
						if _, ok := ipam.possibleLeakedIPCache[key]; ok {
							delete(ipam.possibleLeakedIPCache, key)
							log.Warningf(ctx, "gc: remove IP %v on eni %v from possibleLeakedIPCache", ip.PrivateIpAddress, eni.EniId)
						} else {
							// If the eni network card bound to the IP address changes,
							// but the private IP of eni has not been synchronized yet,
							// and the IP has been allocated to the new Pod, the garbage
							// collection process for the IP should be cancelled
							for tmpKey := range ipam.possibleLeakedIPCache {
								if tmpKey.ipAddr == ip.PrivateIpAddress {
									log.Warningf(ctx, "gc: cancel garbage collection of inuse IP %s which bound to eni %s ", ip.PrivateIpAddress, eni.EniId)
									delete(ipam.possibleLeakedIPCache, tmpKey)
								}
							}

						}
					}
				}
			}
		}
	}
}

func (ipam *IPAM) pruneExpiredLeakedIP(ctx context.Context) {
	var (
		leakedIPs []eniAndIPAddrKey
	)

	// prepare leaked ips for cleanup
	ipam.lock.Lock()
	for key, activeTime := range ipam.possibleLeakedIPCache {
		if ipam.clock.Since(activeTime) > ipamgeneric.LeakedPrivateIPExpiredTimeout {
			log.Infof(ctx, "gc: found leaked ip on eni %v: %v", key.eniID, key.ipAddr)
			leakedIPs = append(leakedIPs, eniAndIPAddrKey{key.nodeName, key.eniID, key.ipAddr})
			delete(ipam.possibleLeakedIPCache, key)
		}
	}
	ipam.lock.Unlock()

	// let's cleanup leaked ips
	for _, tuple := range leakedIPs {
		err := ipam.__tryDeleteIPByIPAndENI(ctx, tuple.nodeName, tuple.ipAddr, tuple.eniID, true)
		if err != nil && !(cloud.IsErrorENIPrivateIPNotFound(err) || cloud.IsErrorENINotFound(err)) {
			log.Errorf(ctx, "gc: stop delete leaked private IP %v on %v, try next round", tuple.ipAddr, tuple.eniID)
		}
	}
}

// https://cloud.baidu.com/doc/BCC/s/Vkaz2btvi
// When the BCC is released, the elastic network card will not be released.
func (ipam *IPAM) gcDeletedNode(ctx context.Context) error {
	for _, node := range ipam.datastore.ListNodes() {
		_, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(node)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof(ctx, "gc: detect node %v has been deleted, clean up datastore", node)

				// clean up datastore
				delErr := ipam.datastore.DeleteNodeFromStore(node)
				if delErr != nil {
					log.Errorf(ctx, "gc: error delete node %v from datastore: %v", node, delErr)
				}

				ipam.lock.Lock()
				if ch, ok := ipam.increasePoolEventChan[node]; ok {
					delete(ipam.increasePoolEventChan, node)
					close(ch)
					log.Infof(ctx, "gc: clean up increase pool goroutine for node %v", node)
				}
				ipam.lock.Unlock()

				continue
			}
			return err
		}
	}

	return nil
}

// Delete privite ip at ipam and cloud vpc
// This method is usually used for the release of fixed IP
func (ipam *IPAM) tryDeleteIPByWep(ctx context.Context, wep *networkingv1alpha1.WorkloadEndpoint) error {
	return ipam.__tryDeleteIPByIPAndENI(ctx, wep.Spec.Node, wep.Spec.IP, wep.Spec.ENIID, true)
}

func (ipam *IPAM) __tryDeleteIPByIPAndENI(ctx context.Context, nodeName, ip, eniID string, deleteAllocateCache bool) error {
	err := ipam.__deleteIPFromCloud(ctx, ip, eniID)
	if err != nil {
		return err
	}
	ipam.__deleteIPFromCache(ctx, nodeName, ip, eniID, deleteAllocateCache)
	return nil
}

func (ipam *IPAM) __deleteIPFromCloud(ctx context.Context, ip, eniID string) error {
	err := ipam.deletePrivateIP(context.Background(), ip, eniID)
	if err != nil {
		log.Errorf(ctx, "__deleteIPFromCloud failed to delete private IP %v on %v: %v", ip, eniID, err)
	} else {
		log.Infof(ctx, "__deleteIPFromCloud delete private IP %v on %v successfully", ip, eniID)
	}
	if err != nil && !(cloud.IsErrorENIPrivateIPNotFound(err) || cloud.IsErrorENINotFound(err)) {
		log.Errorf(ctx, "__deleteIPFromCloud stop delete ip, try next round")
		return err
	}
	return nil
}

func (ipam *IPAM) __deleteIPFromCache(ctx context.Context, nodeName, ip, eniID string, deleteAllocateCache bool) {
	ipam.datastore.Synchronized(func() error {
		// delete ip from datastore
		ipam.datastore.ReleasePodPrivateIPUnsafe(nodeName, eniID, ip)
		ipam.datastore.DeletePrivateIPFromStoreUnsafe(nodeName, eniID, ip)
		return nil
	})

	ipam.lock.Lock()
	defer ipam.lock.Unlock()
	if deleteAllocateCache {
		ipam.removeIPFromCache(ip, true)
	}
	ipam.decPrivateIPNumCache(eniID, true)
}

// tryDeleteWep Attempt to delete workload endpoint object
// This operation will release the cache of IPAM to related IP
// and the binding information of cloud to IP and Eni network cards
func (ipam *IPAM) tryDeleteIPAndWep(ctx context.Context, wep *networkingv1alpha1.WorkloadEndpoint) (err error) {
	if wep.Spec.SubnetTopologyReference != "" {
		err = ipam.tryDeleteCrossSubnetIPByWep(ctx, wep)
	} else {
		err = ipam.tryDeleteIPByWep(ctx, wep)
	}
	if err != nil {
		log.Errorf(ctx, "gc: error delete private IP %v cross subnet for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
		return err
	}
	ipam.removeIPFromLeakedCache(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
	return ipam.tryDeleteWep(ctx, wep)
}

// Delete workload objects from the k8s cluster
func (ipam *IPAM) tryDeleteWep(ctx context.Context, wep *networkingv1alpha1.WorkloadEndpoint) (err error) {
	// remove finalizers
	wep.Finalizers = nil
	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Update(ctx, wep, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf(ctx, "tryDeleteWep failed to update wep for pod (%v %v): %v", wep.Namespace, wep.Name, err)
		return err
	}
	// delete wep
	if err := ipam.crdClient.CceV1alpha1().WorkloadEndpoints(wep.Namespace).Delete(ctx, wep.Name, *metav1.NewDeleteOptions(0)); err != nil {
		log.Errorf(ctx, "tryDeleteWep failed to delete wep for orphaned pod (%v %v): %v", wep.Namespace, wep.Name, err)
	} else {
		log.Infof(ctx, "tryDeleteWep delete wep for orphaned pod (%v %v) successfully", wep.Namespace, wep.Name)
	}
	return err
}

// Delete privite ip at ipam and cloud vpc
// This method is usually used for the release IP when cross subnet
func (ipam *IPAM) tryDeleteCrossSubnetIPByWep(ctx context.Context, wep *networkingv1alpha1.WorkloadEndpoint) error {
	return ipam.__tryDeleteIPByIPAndENI(ctx, wep.Spec.Node, wep.Spec.IP, wep.Spec.ENIID, true)
}

// tryDeleteCrossSubnetIPRetainAllocateCache Unbind the IP from the Eni,
// but it should be released from the local cache,
// so as to prevent multiple stateful pods from being allocated
// to the same IP when fixed IP is allocated concurrently
func (ipam *IPAM) tryDeleteSubnetIPRetainAllocateCache(ctx context.Context, wep *networkingv1alpha1.WorkloadEndpoint) error {
	return ipam.__tryDeleteIPByIPAndENI(ctx, wep.Spec.Node, wep.Spec.IP, wep.Spec.ENIID, false)
}

func (ipam *IPAM) tryReleaseIdleIP(ctx context.Context, nodeName, eniID string) error {
	tempIP, err := ipam.datastore.AllocatePodPrivateIPByENI(nodeName, eniID)
	if err != nil {
		log.Errorf(ctx, "tryReleaseIdleIP allocate temp ip at node(%s) eni(%s) error: %v", nodeName, eniID, err)
		return err
	}
	return ipam.__tryDeleteIPByIPAndENI(ctx, nodeName, tempIP, eniID, true)
}

func (ipam *IPAM) removeIPFromCache(ipAddr string, lockless bool) {
	if !lockless {
		ipam.lock.Lock()
		defer ipam.lock.Unlock()
	}
	delete(ipam.allocated, ipAddr)
}

func (ipam *IPAM) removeIPFromLeakedCache(node, eniID, ipAddr string) {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	delete(ipam.possibleLeakedIPCache, eniAndIPAddrKey{node, eniID, ipAddr})
}

func (ipam *IPAM) gcDeletedSts(ctx context.Context, wepList []*networkingv1alpha1.WorkloadEndpoint) error {
	for _, wep := range wepList {
		// only delete ip if sts requires fix ip
		if !isFixIPStatefulSetPodWep(wep) {
			continue
		}
		// don't delete ip if policy is Never
		if wep.Spec.FixIPDeletePolicy == FixIPDeletePolicyNever {
			continue
		}
		stsName := util.GetStsName(wep)
		_, err := ipam.kubeInformer.Apps().V1().StatefulSets().Lister().StatefulSets(wep.Namespace).Get(stsName)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof(ctx, "gc: sts (%v %v) has been deleted, will release private IP and clean up orphaned wep ", wep.Namespace, stsName)
				err = ipam.tryDeleteIPAndWep(ctx, wep)
				if err != nil {
					log.Errorf(ctx, "gc sts (%v %v) error %v", wep.Namespace, stsName, err)
				}
			} else {
				log.Errorf(ctx, "gc: failed to get sts (%v %v): %v", wep.Namespace, stsName, err)
			}
		}
	}

	return nil
}

func (ipam *IPAM) gcScaledDownSts(ctx context.Context, stsList []*appv1.StatefulSet) error {
	for _, sts := range stsList {
		replicas := int(*sts.Spec.Replicas)
		requirement, err := labels.NewRequirement(ipamgeneric.WepLabelStsOwnerKey, selection.Equals, []string{sts.Name})
		if err != nil {
			log.Errorf(ctx, "gc: error parsing requirement: %v", err)
			return err
		}
		selector := labels.NewSelector().Add(*requirement)

		// find wep whose owner is sts
		weps, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(sts.Namespace).List(selector)
		if err != nil {
			log.Errorf(ctx, "gc: failed to list wep with selector: %v: %v", selector.String(), err)
			continue
		}
		if replicas < len(weps) {
			podTemplateAnnotations := sts.Spec.Template.ObjectMeta.Annotations
			if podTemplateAnnotations == nil || podTemplateAnnotations[StsPodAnnotationFixIPDeletePolicy] != FixIPDeletePolicyNever {
				log.Infof(ctx, "gc: sts (%v %v) has scaled down from %v to %v, will release private IP and clean up orphaned wep", sts.Namespace, sts.Name, len(weps), replicas)
			}
			for _, wep := range weps {
				// only delete ip if sts requires fix ip
				if !isFixIPStatefulSetPodWep(wep) {
					continue
				}
				// don't delete ip if policy is Never
				if wep.Spec.FixIPDeletePolicy == FixIPDeletePolicyNever {
					continue
				}
				index := util.GetStsPodIndex(wep)
				if index < 0 || index < replicas {
					continue
				}
				stsPodName := fmt.Sprintf("%s-%d", sts.Name, index)
				log.Infof(ctx, "gc: try to release orphaned wep (%v %v)", sts.Namespace, stsPodName)
				err = ipam.tryDeleteIPAndWep(ctx, wep)
				if err != nil {
					log.Errorf(ctx, "gc: try to release orphaned wep (%v %v) error %v", sts.Namespace, stsPodName, err)
				}
			}
		}
	}

	return nil
}

func (ipam *IPAM) gcLeakedPod(ctx context.Context, wepList []*networkingv1alpha1.WorkloadEndpoint) error {
	for _, wep := range wepList {
		// only gc non-fix ip pod
		if isFixIPStatefulSetPodWep(wep) {
			continue
		}
		_, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(wep.Namespace).Get(wep.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("gc: try to release leaked wep (%v %v)", wep.Namespace, wep.Name)
				log.Info(ctx, msg)
				ipam.eventRecorder.Event(&v1.ObjectReference{
					Kind: "wep",
					Name: fmt.Sprintf("%v %v", wep.Namespace, wep.Name),
				}, v1.EventTypeWarning, "PodLeaked", msg)
				ipam.gcIPAndDeleteWep(ctx, wep)
			} else {
				log.Errorf(ctx, "gc: failed to get pod (%v %v): %v", wep.Namespace, wep.Name, err)
			}
		}
	}

	return nil
}

// gcWepAndIP and delete wep
// If the IP pool mode is used, when the idle IP is less than
// the maximum IP size, the IP address will be recycled for reuse.
// In other cases, the IP address will be deleted directly
//
// case 1. delete ip from cloud when cross subnet
// case 2. release privite ip to idle pool
// mark ip as unassigned in datastore, then delete wep
// case 3. delete ip from cloud
func (ipam *IPAM) gcIPAndDeleteWep(ctx context.Context, wep *networkingv1alpha1.WorkloadEndpoint) (err error) {
	idle := ipam.idleIPNum(wep.Spec.Node)

	if wep.Spec.SubnetTopologyReference != "" {
		err = ipam.tryDeleteCrossSubnetIPByWep(ctx, wep)
		if err != nil {
			err = fmt.Errorf("gc: error delete private IP %v cross subnet for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
			log.Errorf(ctx, "%v", err)
			return
		} else {
			goto delWep
		}
	}

	if idle < ipam.idleIPPoolMaxSize {
		log.Infof(ctx, "gc: try to only release wep for pod (%v %v) due to idle ip (%v) less than max idle pool", wep.Namespace, wep.Name, idle)
		err = ipam.datastore.ReleasePodPrivateIP(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
		if err != nil {
			log.Errorf(ctx, "gc: error releasing private IP %v from datastore for pod (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
		} else {
			goto delWep
		}
	}

	err = ipam.tryDeleteIPByWep(ctx, wep)
	if err != nil {
		err = fmt.Errorf("gc: error deleting private IP %v by wep (%v %v): %v", wep.Spec.IP, wep.Namespace, wep.Name, err)
		log.Errorf(ctx, err.Error())
		return
	}
delWep:
	metric.MultiEniMultiIPEniIPCount.WithLabelValues(metric.MetaInfo.ClusterID, metric.MetaInfo.VPCID, wep.Spec.SubnetID).Dec()
	log.Infof(ctx, "release private IP %v for pod (%v %v) successfully", wep.Spec.IP, wep.Name, wep.Name)
	ipam.removeIPFromCache(wep.Spec.IP, false)
	ipam.removeIPFromLeakedCache(wep.Spec.Node, wep.Spec.ENIID, wep.Spec.IP)
	err = ipam.tryDeleteWep(ctx, wep)
	if err != nil {
		log.Errorf(ctx, "gc: try delete wep (%v %v) error:  %v", wep.Namespace, wep.Name, err)
	}
	return err
}

func (ipam *IPAM) gcLeakedIPPool(ctx context.Context) error {
	pools, err := ipam.crdInformer.Cce().V1alpha1().IPPools().Lister().List(labels.Everything())
	if err != nil {
		log.Errorf(ctx, "gc: failed to list ippools: %v", err)
		return nil
	}

	for _, p := range pools {
		nodeName := utilippool.GetNodeNameFromIPPoolName(p.Name)
		_, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(nodeName)
		if err != nil && errors.IsNotFound(err) {
			// We only delete ippool created by cni.
			// To be compatible with prior version, nodeName in ip format is also considered as leaked one.
			if p.Spec.CreationSource == ipamgeneric.IPPoolCreationSourceCNI || net.ParseIP(nodeName) != nil {
				e := ipam.crdClient.CceV1alpha1().IPPools(p.Namespace).Delete(ctx, p.Name, *metav1.NewDeleteOptions(0))
				if e != nil && !errors.IsNotFound(e) {
					log.Errorf(ctx, "gc: failed to delete ippool %v: %v", p.Name, e)
				} else {
					log.Infof(ctx, "gc: delete leaked ippool %v successfully", p.Name)
				}
			}
		}
	}

	return nil
}
