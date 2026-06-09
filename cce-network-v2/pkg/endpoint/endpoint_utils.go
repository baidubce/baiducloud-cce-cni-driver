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
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/common"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/netlinkwrapper"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
)

// GetEndpointFromCache
// GetEndpointCrossCache try get endpoint from cache
// if endpoint is missing the try get from apiserver
func GetEndpointCrossCache(ctx context.Context, cceEndpointClient *watchers.CCEEndpointClient, namespace string, name string) (*ccev2.CCEEndpoint, error) {
	oldEP, err := cceEndpointClient.Get(namespace, name)

	if kerrors.IsNotFound(err) {
		return nil, nil
	}
	return oldEP, err
}

// NewEndpointTemplate create a Elastic CCE Endpoint
func NewEndpointTemplate(containerID, netnsPath string, pod *corev1.Pod) *ccev2.CCEEndpoint {
	namespace := pod.Namespace
	name := pod.Name
	newEP := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Labels:      pod.Labels,
			Annotations: pod.Annotations,
		},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sNamespace: namespace,
				K8sPodName:   name,
				ContainerID:  containerID,
				K8sObjectID:  string(pod.UID),
				PodName:      name,
				Netns:        netnsPath,
			},
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{
					DirectIPAllocation: ccev2.DirectIPAllocation{
						Type:            ccev2.IPAllocTypeElastic,
						ReleaseStrategy: ccev2.ReleaseStrategyTTL,
					},
					NodeName: nodeTypes.GetName(),
				},
			},
		},
	}
	if newEP.Labels == nil {
		newEP.Labels = make(map[string]string)
	}
	newEP.Labels[k8s.LabelNodeName] = nodeTypes.GetName()

	if newEP.Annotations == nil {
		newEP.Annotations = make(map[string]string)
	}
	return newEP
}

// IsSameContainerID checks if two container ids are the same or not
func IsSameContainerID(ep *ccev2.CCEEndpoint, containerID string) bool {
	return ep != nil &&
		ep.Spec.ExternalIdentifiers != nil &&
		ep.Spec.ExternalIdentifiers.ContainerID == containerID
}

// AppendEndpointStatus appends node status to given pod endpoint status
func AppendEndpointStatus(newStatus *ccev2.EndpointStatus, status models.EndpointState, code string) bool {
	update := newStatus.State != string(status)
	// update state and log
	newStatus.State = string(status)
	newLog := &models.EndpointStatusChange{
		Code:      code,
		State:     status,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if len(newStatus.Log) == 0 {
		newStatus.Log = append(newStatus.Log, newLog)
		return update
	}
	if len(newStatus.Log) > 8 {
		newStatus.Log = newStatus.Log[5:]
		update = true
	}
	lastLog := newStatus.Log[len(newStatus.Log)-1]

	if lastLog.State != status || lastLog.Code != code {
		newStatus.Log = append(newStatus.Log, newLog)
	} else {
		newStatus.Log[len(newStatus.Log)-1] = newLog
		update = true
	}
	return update
}

func IsFixedIPEndpoint(resource *ccev2.CCEEndpoint) bool {
	return resource.Spec.Network.IPAllocation != nil && resource.Spec.Network.IPAllocation.Type == ccev2.IPAllocTypeFixed
}

func IsPSTSEndpoint(resource *ccev2.CCEEndpoint) bool {
	return resource.Spec.Network.IPAllocation != nil && resource.Spec.Network.IPAllocation.PSTSName != ""
}

// DeleteEndpointAndWait delete endpoint and wait for it to be deleted
func DeleteEndpointAndWait(ctx context.Context, cceEndpointClient *watchers.CCEEndpointClient, oldEP *ccev2.CCEEndpoint) (err error) {
	if oldEP == nil {
		return nil
	}
	err = cceEndpointClient.CCEEndpoints(oldEP.Namespace).Delete(ctx, oldEP.Name, metav1.DeleteOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return fmt.Errorf("delete endpoint error: %w", err)
	}

	err = wait.PollImmediateUntilWithContext(ctx, time.Second/2, func(context.Context) (done bool, err error) {
		ep, err := cceEndpointClient.Get(oldEP.Namespace, oldEP.Name)
		if !kerrors.IsNotFound(err) || ep != nil {
			return false, nil
		}
		return true, nil
	})
	return err
}

func ConverteIPAllocation2EndpointAddress(result *ipam.AllocationResult, family ccev2.IPFamily) *ccev2.AddressPair {
	if result == nil {
		return nil
	}
	return &ccev2.AddressPair{
		IP:        result.IP.String(),
		Family:    family,
		Interface: result.InterfaceNumber,
		Gateway:   result.GatewayIP,
		CIDRs:     result.CIDRs,
	}
}

func GetPodNameFromCEP(cep *ccev2.CCEEndpoint) (namesapce, name string) {
	if cep == nil {
		return "", ""
	}

	if cep.Spec.ExternalIdentifiers.K8sPodName != "" {
		return cep.Namespace, cep.Spec.ExternalIdentifiers.K8sPodName
	}
	return cep.Namespace, cep.Name
}

func getIpFromLink(netlink netlinkwrapper.NetLink, link netlink.Link, family int, ip string) (bool, error) {
	ips, err := netlink.AddrList(link, family)
	if err != nil {
		return false, err
	}
	for _, ipNet := range ips {
		if ipNet.IP.String() == ip {
			return true, nil
		}
	}
	return false, nil
}

// findVethInterface finds veth interface by pod name and containerID
func findVethInterface(netlinkImpl netlinkwrapper.NetLink, podName, containerID, namespace string, logEntry *logrus.Entry, epName string) (netlink.Link, string) {
	// generate veth name from pod name and namespace
	vethName1 := common.VethNameForPod(podName, namespace, "veth")
	vethLink1, err1 := netlinkImpl.LinkByName(vethName1)

	// generate veth name from containerID and namespace (if containerID is available)
	var vethName2 string
	var vethLink2 netlink.Link
	var err2 error
	if containerID != "" {
		vethName2 = common.VethNameForPod(containerID, namespace, "veth")
		vethLink2, err2 = netlinkImpl.LinkByName(vethName2)
	}

	// use the veth that exists (prefer containerID-based veth if both exist)
	if err2 == nil && vethLink2 != nil {
		// containerID-based veth exists
		return vethLink2, vethName2
	}
	if err1 == nil && vethLink1 != nil {
		// podName-based veth exists
		return vethLink1, vethName1
	}

	// neither veth exists, pod resources are not present on the node
	logEntry.WithField("vethName1", vethName1).
		WithField("vethName2", vethName2).
		WithField("CCEEndpoint", epName).
		WithField("ContainerID", containerID).
		WithField("Error1", err1).
		WithField("Error2", err2).
		Debug("veth link not found by pod name or containerID, pod resources not present")
	return nil, ""
}

// checkRoutesExistInMainTable checks if routes exist in main routing table for the given IPs.
// It returns true if at least one route is found matching any of the IPs with the specified link index.
// Returns an error if route listing fails.
func checkRoutesExistInMainTable(netlinkImpl netlinkwrapper.NetLink, ips []net.IP, linkIndex int) (bool, error) {
	if len(ips) == 0 {
		return false, nil
	}

	// Group IPs by family to minimize route list calls
	ipv4IPs := make([]net.IP, 0)
	ipv6IPs := make([]net.IP, 0)

	for _, ip := range ips {
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			ipv4IPs = append(ipv4IPs, ip)
		} else {
			ipv6IPs = append(ipv6IPs, ip)
		}
	}

	// Check IPv4 routes
	if len(ipv4IPs) > 0 {
		routes, err := netlinkImpl.RouteList(nil, netlink.FAMILY_V4)
		if err != nil {
			return false, fmt.Errorf("failed to list IPv4 routes: %w", err)
		}

		if found := checkRoutesForIPs(routes, ipv4IPs, linkIndex); found {
			return true, nil
		}
	}

	// Check IPv6 routes
	if len(ipv6IPs) > 0 {
		routes, err := netlinkImpl.RouteList(nil, netlink.FAMILY_V6)
		if err != nil {
			return false, fmt.Errorf("failed to list IPv6 routes: %w", err)
		}

		if found := checkRoutesForIPs(routes, ipv6IPs, linkIndex); found {
			return true, nil
		}
	}

	return false, nil
}

// checkRoutesForIPs checks if any route in the routes list matches any of the given IPs with the specified link index.
func checkRoutesForIPs(routes []netlink.Route, ips []net.IP, linkIndex int) bool {
	for _, route := range routes {
		// Check if route is in main table (Table == 0 means main table)
		if route.Table != 0 {
			continue
		}

		// Check if route device matches the link index
		if route.LinkIndex != linkIndex {
			continue
		}

		// Check if route destination matches any of the IPs
		if route.Dst != nil && route.Dst.IP != nil {
			for _, ip := range ips {
				if route.Dst.IP.Equal(ip) {
					return true
				}
			}
		}
	}
	return false
}

// cleanupVethInterface deletes the veth interface
func cleanupVethInterface(netlinkImpl netlinkwrapper.NetLink, vethLink netlink.Link, vethName string, logEntry *logrus.Entry) bool {
	logEntry.Infof("node network resources still exist, cleaning up veth interface")
	err := netlinkImpl.LinkDel(vethLink)
	if err != nil {
		logEntry.WithField("VethName", vethName).
			WithError(err).
			Error("failed to delete veth interface during cleanup")
		return false
	}
	logEntry.WithField("VethName", vethName).
		Infof("deleted veth interface during cleanup")
	return true
}

func isThisCEPReadyForDelete(logEntry *logrus.Entry, ep *ccev2.CCEEndpoint, cleanupIfNotReady bool) bool {
	isReadyForDelete := true

	defer func() {
		logEntry.WithField("isThisCEPReadyForDelete", isReadyForDelete).
			WithField("namespace", ep.Namespace).
			WithField("CCEEndpoint", ep.Name).
			Infof("The state of ReadyForDelete is %v", isReadyForDelete)
	}()

	if ep.Status.Networking == nil || ep.Status.ExternalIdentifiers == nil {
		return true
	}

	// get pod name and namespace from CEP
	namespace, podName := GetPodNameFromCEP(ep)
	if namespace == "" || podName == "" {
		return true
	}

	// get containerID from CEP
	containerID := ""
	if ep.Spec.ExternalIdentifiers != nil {
		containerID = ep.Spec.ExternalIdentifiers.ContainerID
	}

	// find veth interface
	vethLink, vethName := findVethInterface(netlinkImpl, podName, containerID, namespace, logEntry, ep.Name)
	if vethLink == nil {
		// no veth found, ready for delete
		return true
	}

	// extract IPs from endpoint
	ips := make([]net.IP, 0)
	if ep.Status.Networking != nil && len(ep.Status.Networking.Addressing) != 0 {
		for _, address := range ep.Status.Networking.Addressing {
			ipStr := address.IP
			if ipStr == "" {
				continue
			}
			ip := net.ParseIP(ipStr)
			if ip != nil {
				ips = append(ips, ip)
			}
		}
	}

	// check if routes exist in main routing table
	hasRoutes, err := checkRoutesExistInMainTable(netlinkImpl, ips, vethLink.Attrs().Index)
	if err != nil {
		logEntry.WithError(err).Error("failed to check routes in main routing table")
		return false
	}
	if !hasRoutes {
		// no routes found, ready for delete
		return true
	}

	// routes exist, cleanup if requested
	if cleanupIfNotReady {
		if !cleanupVethInterface(netlinkImpl, vethLink, vethName, logEntry) {
			// cleanup failed
			return false
		}
		// cleanup succeeded
		return true
	}

	// routes exist and cleanup not requested
	return false
}
