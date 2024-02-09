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

// EndpointAllocator It proxies the basic operation methods of ipam.
// Each time an IP application succeeds, a CCEEndpoint object will be created.
// During gc, the existing CCEEndpoint objects will be gc based on CCEEndpoint
type EndpointAllocator struct {
	dynamicIPAM ipam.IPAMAllocator
	c           ipam.Configuration

	cceEndpointClient *watchers.CCEEndpointClient
	podClient         *watchers.PodClient
	pstsLister        listerv2.PodSubnetTopologySpreadLister
	eventRecorder     record.EventRecorder
}

func NewIPAM(nodeAddressing types.NodeAddressing, c ipam.Configuration, owner ipam.Owner, watcher *watchers.K8sWatcher, mtuConfig ipam.MtuConfiguration) ipam.CNIIPAMServer {
	dynamicIPAM := ipam.NewIPAM(nodeAddressing, c, owner, watcher, mtuConfig)
	e := &EndpointAllocator{
		dynamicIPAM:       dynamicIPAM,
		cceEndpointClient: watcher.NewCCEEndpointClient(),
		podClient:         watcher.NewPodClient(),
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
	go func() {
		mngr := controller.NewManager()
		mngr.UpdateController("psts-ipam-endpoint-interval-gc",
			controller.ControllerParams{
				RunInterval: c.GetCCEEndpointGC(),
				DoFunc: func(ctx context.Context) error {
					e.gc()
					return nil
				},
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
	namespace, name, err := cache.SplitMetaNamespaceKey(owner)
	if err != nil {
		return err
	}

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
		return e.tryDeleteEndpointAfterPodDeleted(oldEP, logEntry)
	} else {
		logEntry.Infof("ignore to release ip case by container id is not match")
	}
	return nil
}

func isSameContainerID(oldEP *ccev2.CCEEndpoint, containerID string) bool {
	return oldEP != nil && oldEP.Spec.ExternalIdentifiers != nil && oldEP.Spec.ExternalIdentifiers.ContainerID == containerID
}

// ADD allocates an IP for the given owner and returns the allocated IP.
func (e *EndpointAllocator) ADD(family, owner, containerID, netns string) (ipv4Result, ipv6Result *ipam.AllocationResult, err error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(owner)
	if err != nil {
		return nil, nil, err
	}

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
	logEntry.Debug("start cni add")

	defer func() {
		if err != nil {
			logEntry.WithError(err).Error("cni add error")
			return
		}
		cancelFun()
		if ipv4Result != nil {
			logEntry = logEntry.WithField("ipv4", ipv4Result.IP.String())
		}
		if ipv6Result != nil {
			logEntry = logEntry.WithField("ipv6", ipv6Result.IP.String())
		}
		logEntry.Infof("cni add success")
	}()

	pod, err = e.podClient.Get(namespace, name)
	if err != nil {
		return nil, nil, fmt.Errorf("get pod (%s/%s) error %w", namespace, name, err)
	}

	isFixedPod := k8s.HaveFixedIPLabel(&pod.ObjectMeta)
	// warning: old wep use node selector,
	// So here oldWEP from the cache may not exist,
	// we need to retrieve it from the api-server
	oldEP, err := GetEndpointCrossCache(ctx, e.cceEndpointClient, namespace, name)
	if err != nil {
		return
	}

	switch e.c.IPAMMode() {
	case ipamOption.IPAMVpcEni:
		psts, err = pststrategy.SelectPSTS(logEntry, e.pstsLister, pod)
		if err != nil {
			return
		}
	case ipamOption.IPAMVpcRoute:
		isFixedPod = false
		psts = nil
	}
	// allocate dynamic ip
	// allocate ip by psts
	// Wait circularly until the fixed IP is assigned by the operator or a timeout occurs
	return e.allocateIP(ctx, logEntry, containerID, family, owner, netns, psts, oldEP, pod, isFixedPod)

}

// allocateIP allocates an IP for the given owner and returns the allocated IP.
func (e *EndpointAllocator) allocateIP(ctx context.Context, logEntry *logrus.Entry, containerID, family, owner, netns string, psts *ccev2.PodSubnetTopologySpread, oldEP *ccev2.CCEEndpoint, pod *corev1.Pod, isFixedIPPod bool) (ipv4Result, ipv6Result *ipam.AllocationResult, err error) {
	newEP := NewEndpointTemplate(containerID, netns, pod)
	ipTTLSeconds := k8s.ExtractFixedIPTTLSeconds(pod)
	if ipTTLSeconds != 0 {
		newEP.Spec.Network.IPAllocation.TTLSecondsAfterDeleted = &ipTTLSeconds
	}

	// allocate the new dynamic endpoint
	if psts == nil {
		if !isFixedIPPod {
			if oldEP != nil {
				err := DeleteEndpointAndWait(ctx, e.cceEndpointClient, oldEP)
				if err != nil {
					return nil, nil, err
				}
				logEntry.Info("clean the old dynamic endpoint success")
			}

			ipv4Result, ipv6Result, err = e.dynamicIPAM.AllocateNext(family, owner)
			if err != nil {
				return
			}
			err = e.createDynamicEndpoint(ctx, newEP, ipv4Result, ipv6Result)
			if err == nil {
				logEntry.Infof("create dynamic endpoint success")
			}
			return
		} else {
			// allocate fixed ip
			newEP, err = e.createDelegateEndpoint(ctx, nil, newEP, oldEP)
			if err != nil {
				return
			}
			logEntry.Info("create fixed endpoint success")
		}
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

// Restore all the IP addresses applied for by the current machine to the node cache pool
func (e *EndpointAllocator) Restore() {
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
		if IsFixedIPEndpoint(ep) || IsPSTSEndpoint(ep) {
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
				epLog.WithError(err).Error("AllocateIPWithoutSyncUpstream error")
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
	k8s.FinalizerAddRemoteIP(newEP)

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

// Periodically run garbage collection. When the pod on the node has been killed,
// the IP occupied by the pod should be released
func (e *EndpointAllocator) gc() {
	logEntry := allocatorLog.WithField("module", "gc")
	eps, err := e.cceEndpointClient.List()
	if err != nil {
		logEntry.WithError(err).Error("list endpoint error")
		return
	}
	for _, ep := range eps {
		pod, err := e.podClient.Get(ep.Namespace, ep.Name)
		if err != nil && kerrors.IsNotFound(err) {
			e.tryDeleteEndpointAfterPodDeleted(ep, logEntry)
			continue
		}

		// ep is expire
		if ep.Spec.ExternalIdentifiers != nil && ep.Spec.ExternalIdentifiers.K8sObjectID != string(pod.UID) {
			e.tryDeleteEndpointAfterPodDeleted(ep, logEntry)
		}
	}

	e.dynamicUsedIPsGC(logEntry)
}

func (e *EndpointAllocator) dynamicUsedIPsGC(logEntry *logrus.Entry) {
	allocv4, allocv6, _ := e.dynamicIPAM.Dump()
	releaseExpiredIPs := func(alloc map[string]string) {
		if len(alloc) == 0 {
			return
		}
		for addr, owner := range alloc {
			scopedLog := logEntry.WithFields(logrus.Fields{
				"ip":    addr,
				"owner": owner,
				"step":  "dynamicUsedIPsGC",
			})
			if strings.HasSuffix(owner, ipamOption.IPAMVpcRoute) {
				continue
			}
			namespace, name, err := cache.SplitMetaNamespaceKey(owner)
			if err != nil {
				scopedLog.WithError(err).Error("split owner error")
				continue
			}
			_, err = e.podClient.Get(namespace, name)
			if err != nil && kerrors.IsNotFound(err) {
				_, err := e.cceEndpointClient.CCEEndpoints(namespace).Get(context.TODO(), name, metav1.GetOptions{})
				if err != nil && kerrors.IsNotFound(err) {
					err = e.dynamicIPAM.ReleaseIPString(addr)
					scopedLog.WithError(err).Info("gc ip when pod deleted")
				}

				continue
			}
		}
	}
	releaseExpiredIPs(allocv4)
	releaseExpiredIPs(allocv6)
}

// tryDeleteEndpointAfterPodDeleted The garbage collection dynamic endpoint first recycles the pre allocated
// IP address of the node, and then deletes the object. If the IP cannot be recycled,
// the corresponding object will also be kept continuously
func (e *EndpointAllocator) tryDeleteEndpointAfterPodDeleted(ep *ccev2.CCEEndpoint, logEntry *logrus.Entry) (err error) {
	logEntry = logEntry.WithFields(logrus.Fields{
		"namespace": ep.Namespace,
		"name":      ep.Name,
		"info":      "delete dynamic endpoint after pod deleted",
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

	logEntry.Infof("start")
	defer func() {
		if err != nil {
			logEntry.WithError(err).Error("release ip failed")
		} else {
			logEntry.Infof("release ip success")
		}
	}()

	ips, err = e.extractEndpointIPs(ep)

	for _, ip := range ips {
		if ip != "" {
			err = e.dynamicIPAM.ReleaseIPString(ip)
			if err != nil {
				logEntry.Warningf("failed to release ip %s", ip)
			}
		}
	}

deleteObj:
	// delete endpoint
	logEntry.WithField("step", "delete").Info("maker endpoint deleted")
	return e.cceEndpointClient.CCEEndpoints(ep.Namespace).Delete(context.TODO(), ep.Name, metav1.DeleteOptions{})
}
