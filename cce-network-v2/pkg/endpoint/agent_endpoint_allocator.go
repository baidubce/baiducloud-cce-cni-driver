/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
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
package endpoint

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listerv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/pststrategy"
)

var (
	allocatorComponentName = "cce-ipam-endpoint-allocator"
	allocatorLog           = logging.DefaultLogger.WithField(logfields.LogSubsys, allocatorComponentName)
)

type rdmaEndpointAllocatorAttr struct {
	isRdmaEndpointAllocator bool
	primaryMacAddress       string
}

// EndpointAllocator It proxies the basic operation methods of ipam.
// Each time an IP application succeeds, a CCEEndpoint object will be created.
// During gc, the existing CCEEndpoint objects will be gc based on CCEEndpoint
type EndpointAllocator struct {
	dynamicIPAM ipam.IPAMAllocator
	rdmaAttr    rdmaEndpointAllocatorAttr
	c           ipam.Configuration
	nrsName     string

	cceEndpointClient *watchers.CCEEndpointClient
	podClient         *watchers.PodClient
	pstsLister        listerv2.PodSubnetTopologySpreadLister
	eventRecorder     record.EventRecorder
}

func NewIPAM(networkResourceSetName string, isRdmaEndpointAllocator bool, primaryMacAddress string, nodeAddressing types.NodeAddressing, c ipam.Configuration, owner ipam.Owner, watcher *watchers.K8sWatcher, mtuConfig ipam.MtuConfiguration) ipam.CNIIPAMServer {
	dynamicIPAM := ipam.NewIPAM(networkResourceSetName, nodeAddressing, c, owner, watcher, mtuConfig)
	allocatorLog.Infof("init dynamicIPAM for %s %s", networkResourceSetName, primaryMacAddress)
	e := &EndpointAllocator{
		dynamicIPAM:       dynamicIPAM,
		rdmaAttr:          rdmaEndpointAllocatorAttr{isRdmaEndpointAllocator, primaryMacAddress},
		cceEndpointClient: watcher.NewCCEEndpointClient(),
		podClient:         watcher.NewPodClient(),
		nrsName:           networkResourceSetName,
		c:                 c,

		pstsLister:    k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister(),
		eventRecorder: k8s.EventBroadcaster().NewRecorder(scheme.Scheme, corev1.EventSource{Component: allocatorComponentName}),
	}

	allocatorLog.Infof("wait for psts cache sync")
	k8s.CCEClient().Informers.Start(wait.NeverStop)

	e.Restore()

	// Start an interval based  background resync for safety, it will
	// synchronize the state regularly and resolve eventual deficit if the
	// event driven trigger fails, and also release excess IP addresses
	// if release-excess-ips is enabled
	gcer := newAgentGcer(e)
	go func() {
		mngr := controller.NewManager()
		mngr.UpdateController("psts-ipam-endpoint-interval-gc",
			controller.ControllerParams{
				RunInterval: c.GetCCEEndpointGC(),
				DoFunc:      gcer.gc,
			})
	}()
	return e
}

func (e *EndpointAllocator) Dump() (allocv4 map[string]string, allocv6 map[string]string, status string) {
	return e.dynamicIPAM.Dump()
}

func (e *EndpointAllocator) DebugStatus() string {
	return e.dynamicIPAM.DebugStatus()
}

func (e *EndpointAllocator) RestoreFinished() {
	e.dynamicIPAM.RestoreFinished()
}

func (e *EndpointAllocator) DEL(owner, containerID string) (err error) {
	var name string
	namespace, podName, err := cache.SplitMetaNamespaceKey(owner)
	if err != nil {
		return err
	}

	// rename for rdma cep, rdma cep is like "podname-rdma-primarymacaddressinhex"
	// cep name is like "podname"
	name = bceutils.GetCEPNameFromPodName(e.rdmaAttr.isRdmaEndpointAllocator, podName, e.rdmaAttr.primaryMacAddress)

	var (
		logEntry = allocatorLog.WithFields(logrus.Fields{
			"namespace":   namespace,
			"name":        name,
			"module":      "ReleaseIPString",
			"containerid": containerID,
		})
	)
	logEntry.Debug("start cni del ip")

	defer func() {
		if err != nil {
			logEntry.WithError(err).Error("release ip error")
		}
		logEntry.Debug("cni del success")
	}()

	oldEP, err := e.cceEndpointClient.Get(namespace, name)
	if kerrors.IsNotFound(err) {
		// fall back to get object from kube-apiserver
		oldEP, err = e.cceEndpointClient.CCEEndpoints(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			logEntry.Debug("endpoint object not found")
			err = nil
			oldEP = nil
		}
	}
	if err != nil {
		return err
	}
	if isSameContainerID(oldEP, containerID) {
		return e.tryDeleteEndpointAfterPodDeleted(oldEP, false, logEntry)
	} else {
		logEntry.Infof("ignore to release ip case by container id is not match")
	}
	return nil
}

func isSameContainerID(oldEP *ccev2.CCEEndpoint, containerID string) bool {
	return oldEP != nil && oldEP.Spec.ExternalIdentifiers != nil && oldEP.Spec.ExternalIdentifiers.ContainerID == containerID
}

func hasRDMAResource(rl corev1.ResourceList) bool {
	for key, _ := range rl {
		arr := strings.Split(string(key), "/")
		if len(arr) != 2 {
			continue
		}
		if arr[0] == bceutils.PodResourceName {
			return true
		}
	}
	return false
}

func wantRoce(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if hasRDMAResource(container.Resources.Limits) || hasRDMAResource(container.Resources.Requests) {
			return true
		}
	}

	return false
}

// ADD allocates an IP for the given owner and returns the allocated IP.
func (e *EndpointAllocator) ADD(family, owner, containerID, netns string) (ipv4Result, ipv6Result *ipam.AllocationResult, err error) {
	var name string
	namespace, podName, err := cache.SplitMetaNamespaceKey(owner)
	if err != nil {
		return nil, nil, err
	}

	// rename for rdma cep, rdma cep is like "podname-rdma-primarymacaddressinhex"
	// cep name is like "podname"
	name = bceutils.GetCEPNameFromPodName(e.rdmaAttr.isRdmaEndpointAllocator, podName, e.rdmaAttr.primaryMacAddress)

	var (
		ctx, cancelFun = context.WithTimeout(logfields.NewContext(), e.c.GetFixedIPTimeout())
		logEntry       = allocatorLog.WithFields(logrus.Fields{
			"namespace":   namespace,
			"name":        name,
			"module":      "AllocateNext",
			"ipv4":        logfields.Repr(ipv4Result),
			"ipv6":        logfields.Repr(ipv6Result),
			"containerID": containerID,
		}).WithContext(ctx)

		psts *ccev2.PodSubnetTopologySpread
		pod  *corev1.Pod
	)
	logEntry.Debug("start cni ADD")

	defer func() {
		if err != nil {
			logEntry.WithError(err).Error("cni ADD error")
			return
		}
		cancelFun()
		if ipv4Result != nil {
			logEntry = logEntry.WithField("ipv4", ipv4Result.IP.String())
		}
		if ipv6Result != nil {
			logEntry = logEntry.WithField("ipv6", ipv6Result.IP.String())
		}
		logEntry.Infof("cni ADD success")
	}()

	pod, err = e.podClient.Get(namespace, podName)
	if err != nil {
		return nil, nil, fmt.Errorf("get pod (%s/%s) error %w", namespace, podName, err)
	}

	// if it is RdmaEndpointAllocator, do not allocate IP for pods without RDMA resource
	if e.isRDMAMode() && !wantRoce(pod) {
		return nil, nil, nil
	}

	isFixedPod := k8s.HaveFixedIPLabel(&pod.ObjectMeta)
	mode := e.c.IPAMMode()
	// rdma not support psts, so we need to check if it is rdma mode
	if e.isRDMAMode() {
		mode = ipamOption.IPAMRdma
	}

	switch mode {
	case ipamOption.IPAMVpcEni:
		psts, err = pststrategy.SelectPSTS(logEntry, e.pstsLister, pod)
		if err != nil {
			return
		}
	default:
		isFixedPod = false
		psts = nil
	}
	// allocate dynamic ip
	// allocate ip by psts
	// Wait circularly until the fixed IP is assigned by the operator or a timeout occurs
	return e.allocateIP(ctx, logEntry, containerID, family, owner, netns, psts, pod, isFixedPod)

}

// allocateIP allocates an IP for the given owner and returns the allocated IP.
func (e *EndpointAllocator) allocateIP(ctx context.Context, logEntry *logrus.Entry, containerID, family, owner, netns string, psts *ccev2.PodSubnetTopologySpread, pod *corev1.Pod, isFixedIPPod bool) (ipv4Result, ipv6Result *ipam.AllocationResult, err error) {
	epName := bceutils.GetCEPNameFromPodName(e.rdmaAttr.isRdmaEndpointAllocator, pod.Name, e.rdmaAttr.primaryMacAddress)
	// warning: old wep use node selector,
	// So here oldWEP from the cache may not exist,
	// we need to retrieve it from the api-server
	oldEP, err := e.cceEndpointClient.Get(pod.Namespace, epName)
	if kerrors.IsNotFound(err) {
		// fall back to get object from kube-apiserver
		oldEP, err = e.cceEndpointClient.CCEEndpoints(pod.Namespace).Get(context.TODO(), epName, metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			logEntry.Debug("endpoint object not found")
			err = nil
			oldEP = nil
		}
	}
	if err != nil {
		return
	}

	newEP := NewEndpointTemplate(containerID, netns, pod)
	ipTTLSeconds := k8s.ExtractFixedIPTTLSeconds(pod)
	if ipTTLSeconds != 0 {
		newEP.Spec.Network.IPAllocation.TTLSecondsAfterDeleted = &ipTTLSeconds
	}

	// rename for rdma cep, rdma cep is like "podname-rdma-primarymacaddressinhex"
	// cep name is like "podname"
	newEP.Name = epName
	if e.rdmaAttr.isRdmaEndpointAllocator {
		newEP.Annotations[k8s.AnnotationRDMAInfoMacAddress] = e.rdmaAttr.primaryMacAddress
		newEP.Annotations[k8s.PodLabelOwnerName] = pod.Name
		newEP.Spec.Network.IPAllocation.Type = ccev2.IPAllocTypeRDMA
		newEP.Labels[k8s.LabelENIType] = "rdma"
	}

	// add finalizer for delayed delete endpoint
	k8s.FinalizerAddRemoteIP(newEP)

	// allocate the new dynamic endpoint
	if psts == nil && !isFixedIPPod {
		if oldEP != nil {
			err := e.tryDeleteEndpointAfterPodDeleted(oldEP, true, logEntry.WithField("scope", "recreate cep"))
			if err != nil {
				return nil, nil, err
			}
			logEntry.Info("clean the old dynamic endpoint success")
		}

		ipv4Result, ipv6Result, err = e.dynamicIPAM.AllocateNext(family, owner)
		if err != nil {
			return
		}
		newEP.OwnerReferences = append(newEP.OwnerReferences, metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       pod.Name,
			UID:        pod.UID,
		})

		err = e.createDynamicEndpoint(ctx, newEP, ipv4Result, ipv6Result)
		if err == nil {
			logEntry.Infof("create dynamic endpoint success")
		}
		return
	} else {
		newEP, err = e.createDelegateEndpoint(ctx, psts, newEP, oldEP)
		if err != nil {
			return
		}
		logEntry.Info("create psts endpoint success")
	}

	ipv4Result, ipv6Result, err = e.waitEndpointIPAllocated(ctx, newEP)
	if err == nil {
		logEntry.Info("wait endpoint ip allocated success")
	}
	return ipv4Result, ipv6Result, err
}

func (e *EndpointAllocator) isRDMAMode() bool {
	return e.rdmaAttr.isRdmaEndpointAllocator
}

// Restore all the IP addresses applied for by the current machine to the node cache pool
func (e *EndpointAllocator) Restore() {
	if e.rdmaAttr.isRdmaEndpointAllocator {
		// restore dynamic rdma ip if need
		logEntry := allocatorLog.WithField("module", "Restore")
		eps, err := e.cceEndpointClient.List()
		if err != nil {
			logEntry.WithError(err).Error("list rdma endpoint error")
			return
		}
		logEntry.Infof("start to restore rdma endpoint")
		for _, ep := range eps {
			// skip the non rdma cceendpoint which name is like "podname-rdma-primarymacaddressindex"
			if !bceutils.IsCCERdmaEndpointName(ep.Name) {
				continue
			}
			// skip the rdma cceendpoint which primary mac address is not equal to current NetworkResourceSet
			if !bceutils.IsThisMasterMacCCERdmaEndpointName(ep.Name, e.rdmaAttr.primaryMacAddress) {
				// skip the rdma cceendpoint which primary mac address is not equal to current node
				// because the rdma cceendpoint is created by other node
				continue
			}
			// skip the cceendpoint which is deleting or deleted
			if ep.GetDeletionTimestamp() != nil || !ep.GetDeletionTimestamp().IsZero() {
				continue
			}
			epLog := logEntry.WithFields(logrus.Fields{
				"namespace": ep.Namespace,
				"name":      ep.Name,
			})
			ips, err := e.extractEndpointIPs(ep)
			if err != nil {
				epLog.WithError(err).Error("extract rdma endpoint ip error")
				continue
			}
			epLog.Infof("restore rdma ips %v", ips)
			for _, ip := range ips {
				_, podName := GetPodNameFromCEP(ep)
				_, err = e.dynamicIPAM.AllocateIPWithoutSyncUpstream(net.ParseIP(ip), ep.Namespace+"/"+podName)
				if err != nil {
					epLog.WithError(err).Error("AllocateIPWithoutSyncUpstream error")
				}
			}
		}
		logEntry.Infof("restore rdma endpoint end")
		return
	}

	// restore route ip if need
	if e.c.IPAMMode() == ipamOption.IPAMVpcRoute {
		var (
			result *ipam.AllocationResult
			err    error
		)
		if e.c.IPv4Enabled() {
			oldRouter := node.GetInternalIPv4Router()
			if oldRouter != nil {
				e.dynamicIPAM.AllocateIPWithoutSyncUpstream(oldRouter, ipamOption.IPAMVpcRoute)
			} else {
				result, err = e.dynamicIPAM.AllocateNextFamilyWithoutSyncUpstream(ipam.IPv4, "ipv4-"+ipamOption.IPAMVpcRoute)
				if err != nil {
					allocatorLog.WithError(err).Fatal("restore route ipv4 error")
				}
				node.SetInternalIPv4Router(result.IP)
				allocatorLog.Infof("restore route ipv4 %s success", result.IP.String())
			}

		} else if e.c.IPv6Enabled() {
			oldRouter := node.GetIPv6Router()
			if oldRouter != nil {
				e.dynamicIPAM.AllocateIPWithoutSyncUpstream(oldRouter, ipamOption.IPAMVpcRoute)
			} else {
				result, err = e.dynamicIPAM.AllocateNextFamilyWithoutSyncUpstream(ipam.IPv6, "ipv6-"+ipamOption.IPAMVpcRoute)
				if err != nil {
					allocatorLog.WithError(err).Fatal("restore route ipv6 error")
				}
				node.SetIPv6Router(result.IP)
				allocatorLog.Infof("restore route ipv6 %s success", result.IP.String())
			}
		}
	}

	// restore dynamic ip if need
	logEntry := allocatorLog.WithField("module", "Restore")
	eps, err := e.cceEndpointClient.List()
	if err != nil {
		logEntry.WithError(err).Error("list endpoint error")
		return
	}
	logEntry.Infof("start to restore endpoint")
	for _, ep := range eps {
		epLog := logEntry.WithFields(logrus.Fields{
			"namespace": ep.Namespace,
			"name":      ep.Name,
		})
		if IsFixedIPEndpoint(ep) || IsPSTSEndpoint(ep) || bceutils.IsCCERdmaEndpointName(ep.Name) {
			continue
		}
		// skip the cceendpoint which is deleting or deleted
		if ep.GetDeletionTimestamp() != nil || !ep.GetDeletionTimestamp().IsZero() {
			continue
		}
		ips, err := e.extractEndpointIPs(ep)
		if err != nil {
			epLog.WithError(err).Error("extract endpoint ip error")
			continue
		}
		epLog.Infof("restore ips %v", ips)
		for _, ip := range ips {
			_, err = e.dynamicIPAM.AllocateIPWithoutSyncUpstream(net.ParseIP(ip), ep.Namespace+"/"+ep.Name)
			if err != nil {
				epLog.WithError(err).Warnf("failed to restore ip %s, strict inspection mode will be activated", ip)
				pod, err := e.podClient.Get(ep.Namespace, ep.Name)
				if err == nil {
					if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
						epLog.Infof("pod is not running or pending, try to delete expired endpoint")
						e.tryDeleteEndpointAfterPodDeleted(ep, true, epLog)
						continue
					}
					if ep.Spec.ExternalIdentifiers == nil || ep.Spec.ExternalIdentifiers.K8sObjectID != string(pod.UID) {
						epLog.Infof("externalIdentifiers is not equal to pod uid, try to delete expired endpoint")
						e.tryDeleteEndpointAfterPodDeleted(ep, true, epLog)
						continue
					}
					err = wait.PollImmediate(time.Millisecond*200, time.Minute, func() (done bool, err error) {
						_, err = e.dynamicIPAM.AllocateIPWithoutSyncUpstream(net.ParseIP(ip), ep.Namespace+"/"+ep.Name)
						if err != nil {
							epLog.WithError(err).Warnf("failed to restore ip %s, will retry later", ip)
							return false, nil
						}
						epLog.Infof("restore ip %s success", ip)
						return true, nil
					})
					if err != nil {
						epLog.WithError(err).Fatal("failed to restore ip in strict inspection mode after 1 minute")
					}
				} else {
					if kerrors.IsNotFound(err) {
						epLog.Infof("pod not found, try to delete expired endpoint")
						e.tryDeleteEndpointAfterPodDeleted(ep, false, epLog)
						continue
					}
					epLog.WithError(err).Fatal("failed to restore ip in strict inspection mode, failed to get pod")
				}
			}
		}
	}
	logEntry.Infof("restore endpoint end")
}

func (e *EndpointAllocator) extractEndpointIPs(ep *ccev2.CCEEndpoint) ([]string, error) {
	var ips []string

	if ep.Status.Networking != nil && len(ep.Status.Networking.Addressing) != 0 {
		for _, pair := range ep.Status.Networking.Addressing {
			ips = append(ips, pair.IP)
		}
	} else {
		// Demote to directly recycle the pod ip
		pod, _ := e.podClient.Get(ep.Namespace, ep.Name)
		if pod != nil {
			for _, ip := range pod.Status.PodIPs {
				if ip.String() != "" {
					ips = append(ips, ip.String())
				}
			}
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("no available ip")
	}
	return ips, nil
}

// waitEndpointIPAllocated Wait circularly until the fixed IP is assigned by the operator or a timeout occurs
// If the status of the endpoint does not complete the IP allocation until the context timeout,
// it is considered that the IP allocation has failed and is returned directly.
func (e *EndpointAllocator) waitEndpointIPAllocated(ctx context.Context, newEP *ccev2.CCEEndpoint) (ipv4Result, ipv6Result *ipam.AllocationResult, err error) {
	err = wait.PollImmediateUntilWithContext(ctx, time.Second/2, func(context.Context) (done bool, err error) {
		ep, err := e.cceEndpointClient.Get(newEP.Namespace, newEP.Name)
		if err != nil {
			return false, nil
		}
		// The ID of the spec is the same as that of the status,
		//indicating that the synchronization is already in progress
		if ep.Status.ExternalIdentifiers != nil &&
			ep.Status.ExternalIdentifiers.K8sObjectID == newEP.Spec.ExternalIdentifiers.K8sObjectID &&
			ep.Status.Networking != nil && len(ep.Status.Networking.Addressing) > 0 {
			for _, address := range ep.Status.Networking.Addressing {
				result := &ipam.AllocationResult{
					IP:              net.ParseIP(address.IP),
					GatewayIP:       address.Gateway,
					InterfaceNumber: address.Interface,
					CIDRs:           address.CIDRs,
				}
				if address.Family == ccev2.IPv4Family {
					ipv4Result = result
				} else if address.Family == ccev2.IPv6Family {
					ipv6Result = result
				}
			}
			return true, nil
		}
		return false, nil
	})
	return ipv4Result, ipv6Result, err
}

// createDelegateEndpoint create or update a fixed CCEEndpoint
// If the old CCEEndpoint object does not exist, create a new object directly. Otherwise
// if the spec of the old object is different from that of the new object,
// the new object should be used as the standard to update the old object.
func (e *EndpointAllocator) createDelegateEndpoint(ctx context.Context, psts *ccev2.PodSubnetTopologySpread, newEP, oldEP *ccev2.CCEEndpoint) (ep *ccev2.CCEEndpoint, err error) {
	if psts != nil {
		newEP.Spec.Network.IPAllocation.PSTSName = psts.Name
		if psts.Spec.Strategy != nil {
			newEP.Spec.Network.IPAllocation.ReleaseStrategy = psts.Spec.Strategy.ReleaseStrategy
			newEP.Spec.Network.IPAllocation.Type = psts.Spec.Strategy.Type
			if psts.Spec.Strategy.Type == ccev2.IPAllocTypeFixed {
				newEP.Spec.Network.IPAllocation.ReleaseStrategy = ccev2.ReleaseStrategyNever
			} else if psts.Spec.Strategy.EnableReuseIPAddress && psts.Spec.Strategy.TTL != nil {
				seconds := psts.Spec.Strategy.TTL.Seconds()
				if seconds > 0 {
					ttl := int64(seconds)
					newEP.Spec.Network.IPAllocation.TTLSecondsAfterDeleted = &ttl
				}
			}

		}
	}

	// fixed ip endpoint is not use psts
	if psts == nil {
		if !k8s.HaveFixedIPLabel(oldEP) {
			return nil, fmt.Errorf("endpoint is not use fixed ip, please clean it")
		}
		newEP.Spec.Network.IPAllocation.Type = ccev2.IPAllocTypeFixed
		newEP.Spec.Network.IPAllocation.ReleaseStrategy = ccev2.ReleaseStrategyNever
		return e.createReuseIPEndpoint(ctx, newEP, oldEP)
	} else if pststrategy.EnableReuseIPPSTS(psts) && oldEP != nil {
		// reuse ip psts
		if oldEP.Spec.Network.IPAllocation == nil || oldEP.Spec.Network.IPAllocation.PSTSName != psts.Name {
			return nil, fmt.Errorf("psts name is not equal for reuse ip mode, old: %s, new: %s, please delete this cep object", oldEP.Spec.Network.IPAllocation.PSTSName, psts.Name)
		}
		return e.createReuseIPEndpoint(ctx, newEP, oldEP)
	} else {
		// not reuse ip psts or old endpoint is not found, the oldEP maybe is nil, the old cep name is logged by the new cep name
		allocatorLog.WithField("namespace", psts.Namespace).
			WithField("pstsName", psts.Name).
			WithField("oldCceendpoint", oldEP).
			WithField("cceendpointName", newEP.Name).
			Info("can not reuse ip for this psts or cceendpoint")
		// cross subnet, delete old endpoint
		err = DeleteEndpointAndWait(ctx, e.cceEndpointClient, oldEP)
		if err != nil {
			return nil, fmt.Errorf("wait endpoint delete error: %w", err)
		}
		// create endpoint spec, Waiting for status updates
		ep, err = recreateCEP(ctx, e.cceEndpointClient, newEP)
	}
	return ep, err
}

func recreateCEP(ctx context.Context, cceEndpointClient *watchers.CCEEndpointClient, newEP *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	// create endpoint spec, Waiting for status updates
	ep, err := cceEndpointClient.CCEEndpoints(newEP.Namespace).Create(ctx, newEP, metav1.CreateOptions{})
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			goto recreate
		} else {
			return nil, fmt.Errorf("create endpoint error: %w", err)
		}
	}
	return ep, err

recreate:
	err = DeleteEndpointAndWait(ctx, cceEndpointClient, newEP)
	if err != nil {
		return nil, fmt.Errorf("wait endpoint delete error: %w", err)
	}
	ep, err = cceEndpointClient.CCEEndpoints(newEP.Namespace).Create(ctx, newEP, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create endpoint error: %w", err)
	}
	return ep, nil
}

// createFixedEndpoint create or update a fixed CCEEndpoint
// If the old CCEEndpoint object does not exist, create a new object directly. Otherwise
// if the spec of the old object is different from that of the new object,
// the new object should be used as the standard to update the old object.
func (e *EndpointAllocator) createReuseIPEndpoint(ctx context.Context, newEP, oldEP *ccev2.CCEEndpoint) (ep *ccev2.CCEEndpoint, err error) {
	if oldEP == nil {
		// create endpoint spec, Waiting for status updates
		ep, err = e.cceEndpointClient.CCEEndpoints(newEP.Namespace).Create(ctx, newEP, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create endpoint error: %w", err)
		}
	} else {
		// update endpoint spec, Waiting for status updates
		if !reflect.DeepEqual(newEP.Spec, oldEP.Spec) {
			oldEP = oldEP.DeepCopy()
			oldEP.Labels = newEP.Labels
			oldEP.Spec = newEP.Spec
			oldEP.Finalizers = newEP.Finalizers
		}
		AppendEndpointStatus(&oldEP.Status, models.EndpointStateRestoring, models.EndpointStatusChangeCodeOk)
		ep, err = e.cceEndpointClient.CCEEndpoints(newEP.Namespace).Update(ctx, oldEP, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("update endpoint error: %w", err)
		}
	}
	return
}

// createDynamicEndpoint dynamic endpint means that
// its lifecycle is equivalent to the lifecycle of the container:
// *The lifecycle of de is greater than that of sandbox containers
// *The lifecycle of de is smaller than that of nrs
//
// To avoid residual CEP objects after the agent is killed, we will bind the lifecycle of CEP and NRS
func (e *EndpointAllocator) createDynamicEndpoint(ctx context.Context, newEP *ccev2.CCEEndpoint, ipv4Result, ipv6Result *ipam.AllocationResult) error {
	newEP.Spec.Network.IPAllocation.ReleaseStrategy = ccev2.ReleaseStrategyTTL
	newEP.Status = ccev2.EndpointStatus{
		ExternalIdentifiers: newEP.Spec.ExternalIdentifiers,
		Networking: &ccev2.EndpointNetworking{
			NodeIP: nodeTypes.GetName(),
		},
	}
	ipv4 := ConverteIPAllocation2EndpointAddress(ipv4Result, ccev2.IPv4Family)
	newEP.Status.Networking.Addressing = append(newEP.Status.Networking.Addressing, ipv4)
	if ipv6Result != nil {
		ipv6 := ConverteIPAllocation2EndpointAddress(ipv6Result, ccev2.IPv6Family)
		newEP.Status.Networking.Addressing = append(newEP.Status.Networking.Addressing, ipv6)
	}
	newEP.Status.Networking.IPs = newEP.Status.Networking.Addressing.ToIPsString()

	AppendEndpointStatus(&newEP.Status, models.EndpointStateIPAllocated, models.EndpointStatusChangeCodeOk)
	_, err := recreateCEP(ctx, e.cceEndpointClient, newEP)
	if err != nil {
		if ipv4Result != nil {
			_ = e.dynamicIPAM.ReleaseIPString(ipv4Result.IP.String())
		}
		if ipv6Result != nil {
			_ = e.dynamicIPAM.ReleaseIPString(ipv6Result.IP.String())
		}

	}
	return err
}

var (
	_ ipam.CNIIPAMServer = &EndpointAllocator{}
)

// tryDeleteEndpointAfterPodDeleted The garbage collection dynamic endpoint first recycles the pre allocated
// IP address of the node, and then deletes the object. If the IP cannot be recycled,
// the corresponding object will also be kept continuously
func (e *EndpointAllocator) tryDeleteEndpointAfterPodDeleted(ep *ccev2.CCEEndpoint, releaseByOwner bool, logEntry *logrus.Entry) (err error) {
	namespace, name := GetPodNameFromCEP(ep)
	owner := fmt.Sprintf("%s/%s", namespace, name)
	logEntry = logEntry.WithFields(logrus.Fields{
		"namespace":      ep.Namespace,
		"name":           ep.Name,
		"info":           "delete dynamic endpoint after pod deleted",
		"releaseByOwner": releaseByOwner,
		"owner":          owner,
	})

	var (
		ips []string
	)

	if IsFixedIPEndpoint(ep) {
		return nil
	}

	// if the endpoint is psts, it should be deleted
	if ep.Spec.Network.IPAllocation.PSTSName != "" {
		psts, err := e.pstsLister.PodSubnetTopologySpreads(ep.Namespace).Get(ep.Spec.Network.IPAllocation.PSTSName)
		if kerrors.IsNotFound(err) {
			logEntry.Warnf("cep will be deleted when psts %s not found", ep.Spec.Network.IPAllocation.PSTSName)
			goto deleteObj
		}
		if err != nil {
			logEntry.WithError(err).Errorf("failed to get psts %s", ep.Spec.Network.IPAllocation.PSTSName)
			return err
		}
		// ignored if psts is enable reuse ip
		if pststrategy.EnableReuseIPPSTS(psts) {
			return nil
		}
		goto deleteObj
	}

	defer func() {
		if err != nil {
			logEntry.WithError(err).Error("release ip failed")
		} else {
			logEntry.Infof("release ip success")
		}
	}()

	if releaseByOwner {
		err = e.dynamicIPAM.ReleaseIPString(owner)
		if err != nil {
			logEntry.WithField("err", err).Warningf("failed to release ip by oner")
		}
	} else {
		if ep.Status.Networking != nil && len(ep.Status.Networking.Addressing) != 0 {
			for _, pair := range ep.Status.Networking.Addressing {
				ips = append(ips, pair.IP)
			}
		}

		for _, ip := range ips {
			if ip != "" {
				err = e.dynamicIPAM.ReleaseIPString(ip)
				if err != nil {
					logEntry.WithField("err", err).Warningf("failed to release ip %s", ip)
				}
			}
		}
	}

deleteObj:
	// delete endpoint
	// remove finalizer first
	ep.Finalizers = []string{}
	_, err = e.cceEndpointClient.CCEEndpoints(ep.Namespace).Update(context.TODO(), ep, metav1.UpdateOptions{})
	// ignore not found error
	if kerrors.IsNotFound(err) {
		err = nil
	}
	if err != nil {
		logEntry.WithField("err", err).WithField("cep", ep.Name).Warning("failed to remove finalizer of cep before delete")
	}
	logEntry.WithField("step", "delete").Info("maker endpoint deleted")
	return e.cceEndpointClient.CCEEndpoints(ep.Namespace).Delete(context.TODO(), ep.Name, metav1.DeleteOptions{})
}
