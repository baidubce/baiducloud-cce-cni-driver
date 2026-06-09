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

package nodediscovery

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	k8sTypes "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/agent"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/os"
)

const (
	rdmaDiscoverySubsys = "rdmaNodediscovery"
)

var rdLog = logging.DefaultLogger.WithField(logfields.LogSubsys, rdmaDiscoverySubsys)

const (
	ipvlanRequiredKernelVersion = "4.9"
	ipvlanKernelModuleName      = "ipvlan"
)

func canUseIPVlan(kernelVersion *version.Version, kernelModules []string) bool {
	if kernelVersion == nil || kernelVersion.LessThan(version.MustParseGeneric(ipvlanRequiredKernelVersion)) {
		return false
	}

	var ipvlanLoaded bool

	for _, module := range kernelModules {
		if module == ipvlanKernelModuleName {
			ipvlanLoaded = true
			break
		}
	}

	return ipvlanLoaded
}

func canUseIPVlanOnRdmaNode() (isCan bool, err error) {
	isCan = false

	kernelhandler := os.NewLinuxKernelHandler()
	// detect kernel version
	kernelVersionStr, err := kernelhandler.DetectKernelVersion(context.TODO())
	if err != nil {
		rdLog.Errorf("detect kernel version failed: %v", err)
		return false, err
	}
	kernelVersion, err := version.ParseGeneric(kernelVersionStr)
	if err != nil {
		rdLog.Errorf("parse kernel version failed: %v", err)
		return false, err
	}

	// GetModulesAndTryToLoadByWanted returns all installed kernel modules including ipvlanKernelModuleName.
	// Try to load the required kernel modules which are named in ipvlanKernelModuleName if not built in.
	kernelModules, err := kernelhandler.GetModulesAndTryToLoadByWanted(context.TODO(), []string{ipvlanKernelModuleName})
	if err != nil {
		rdLog.Errorf("get kernel modules failed: %v", err)
		return false, err
	}

	rdLog.Infof("kernel version: %v, kernel modules: %v", kernelVersion.String(), kernelModules)

	if canUseIPVlan(kernelVersion, kernelModules) {
		rdLog.Infof("ipvlan is supported")
		return true, nil
	}

	return false, err
}

// RdmaDiscovery represents a node discovery action
type RdmaDiscovery struct {
	*NodeDiscovery
}

// start configures the local node and starts node discovery. This is called on
// agent startup to configure the local node based on the configuration options
// passed to the agent. nodeName is the name to be used in the local agent.
func (rd *RdmaDiscovery) StartDiscovery() {
	k8sNode, err := rd.k8sNodeGetter.GetK8sNode(
		context.TODO(),
		nodeTypes.GetName(),
	)
	if err != nil && !k8serrors.IsNotFound(err) {
		rd.eventRecorder.Eventf(k8sNode, k8sTypes.EventTypeWarning, "FatalError01", "kubernetes Node resource not found: %v", err)
		rdLog.Fatalf("failed to fetch Kubernetes Node resource: %v", err)
		return
	}

	nrcs, err := rd.nrcsNodeGetter.GetNodeNrcs(nodeTypes.GetName())
	if err != nil {
		log.WithError(err).Warning("get node NRCs failed")
	}
	if nrcs != nil {
		if nrcs.Spec.AgentConfig.EnableRDMA != nil {
			option.Config.EnableRDMA = *nrcs.Spec.AgentConfig.EnableRDMA
			log.WithFields(
				logrus.Fields{
					"enableRDMA": option.Config.EnableRDMA,
					"nrcs":       nrcs.Name,
				},
			).Info("update enable rdma flag success")
		}
	}

	if !option.Config.EnableRDMA {
		return
	}
	rdLog.Info("RDMA is enabled, will be starting rdma node discovery")

	isCan, err := canUseIPVlanOnRdmaNode()
	if err != nil {
		rd.eventRecorder.Eventf(k8sNode, k8sTypes.EventTypeWarning, "KernelDetectError01", "ipvlan is not supported, skipping rdma node discovery, err: %v", err)
		rdLog.Info("ipvlan is not supported, skipping rdma node discovery")
		option.Config.EnableRDMA = false
		return
	}
	if !isCan {
		rd.eventRecorder.Eventf(k8sNode, k8sTypes.EventTypeWarning, "KernelDetectError02", "ipvlan is not supported, skipping rdma node discovery")
		rdLog.Infof("ipvlan is not supported, skipping rdma node discovery")
		option.Config.EnableRDMA = false
		return
	}

	rd.localNodeLock.Lock()
	defer rd.localNodeLock.Unlock()

	rd.fillLocalNode()

	go func() {
		rdLog.WithFields(
			logrus.Fields{
				logfields.Node: rd.localNode,
			}).Info("Adding local node to cluster")
		close(rd.Registered)
	}()

	go func() {
		select {
		case <-rd.Registered:
		case <-time.After(defaults.NodeInitTimeout):
			rdLog.Fatalf("Unable to initialize local node due to timeout")
		}
	}()

	rd.Manager.NodeUpdated(rd.localNode)
	close(rd.localStateInitialized)

	rd.updateLocalNode()
}

func (rd *RdmaDiscovery) updateLocalNode() {
	if k8s.IsEnabled() {
		// CRD IPAM endpoint restoration depends on the completion of this
		// to avoid custom resource update conflicts.
		rd.UpdateNetResourceSetResource()
	}
}

// UpdateLocalNode syncs the internal localNode object with the actual state of
// the local node and publishes the corresponding updated KV store entry and/or
// NetResourceSet object
func (rd *RdmaDiscovery) UpdateLocalNode() {
	rd.localNodeLock.Lock()
	defer rd.localNodeLock.Unlock()

	rd.fillLocalNode()
	rd.updateLocalNode()
}

// Close shuts down the node discovery engine
func (rd *RdmaDiscovery) Close() {
	rd.Manager.Close()
}

// UpdateNetResourceSetResource updates the NetResourceSet resource representing the
// local node
func (rd *RdmaDiscovery) UpdateNetResourceSetResource() {
	if !option.Config.AutoCreateNetResourceSetResource {
		return
	}

	nodeName := nodeTypes.GetName()
	rdmaIFs, err := bceutils.GetRdmaIFsInfoAll(nodeName, rdLog)
	if err != nil {
		rdLog.WithError(err).WithField("nodeAddressing", nodeName).Warning("Failed to get rdma IFs info")
		return
	}

	// // Check whether ERI management is enabled (based on config or node label)
	// if option.Config.EnableERIManagement {
	// 	rd.createOrUpdateERIManagedNRS()
	// }

	rdmaIfNum := len(rdmaIFs)
	if rdmaIfNum == 0 && !option.Config.EnableERIManagement {
		option.Config.EnableRDMA = false
		return
	}

	rdLog.WithField(logfields.Node, nodeTypes.GetName()).WithField("rdmaIFs", rdmaIFs).Infof("Discovery %d RDMA interface for this node", rdmaIfNum)

	rdLog.WithField(logfields.Node, nodeTypes.GetName()).Info("Creating or updating RDMA NetResourceSet resource")

	canReturn := false
	hasNonERIManagedInterfaces := false
	for macAddress, rii := range rdmaIFs {
		// Check if it is an ERI interface and ERI management is enabled, if so, agent should exit
		if rd.shouldExitOnRegularERINRS(rii) {
			rd.exitOnRegularERINRS(rii, macAddress)
			// This function will call Fatal, so execution won't continue past this point
			continue
		}
		hasNonERIManagedInterfaces = true

		annotations := map[string]string{
			k8s.AnnotationRDMAInfoMacAddress:  macAddress,
			k8s.AnnotationRDMAInfoVifFeatures: rii.VifFeatures,
		}

		if rd.createOrUpdateNetResourceSet(rii.NetResourceSetName, annotations, rii.LabelSelectorValue, rii.VifFeatures, rii.MacAddress, "RDMA") {
			canReturn = true
		}
	}
	// If all interfaces are ERI managed (agent exits on conflict), or any interface is processed successfully, return normally
	if canReturn || !hasNonERIManagedInterfaces {
		return
	}
	rdLog.Fatal("Could not create or update NetResourceSet resource, despite retries")
}

// createOrUpdateNetResourceSet handles the common logic for creating or updating NetResourceSet resources
func (rd *RdmaDiscovery) createOrUpdateNetResourceSet(nrsName string, annotations map[string]string, labelSelectorValue, vifFeatures, macAddress, resourceType string) bool {
	cceClient := k8s.CCEClient()

	performGet := true
	var netResourceSet *ccev2.NetResourceSet
	for retryCount := 0; retryCount < maxRetryCount; retryCount++ {
		performUpdate := true
		if performGet {
			var err error
			netResourceSet, err = cceClient.CceV2().NetResourceSets().Get(context.TODO(), nrsName, metav1.GetOptions{})
			if err != nil {
				performUpdate = false
				netResourceSet = &ccev2.NetResourceSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:        nrsName,
						Labels:      map[string]string{},
						Annotations: annotations,
					},
				}
			} else {
				performGet = false
			}
		}

		// Fill in basic configuration
		if err := rd.mutateNodeResource(netResourceSet, labelSelectorValue, vifFeatures, macAddress); err != nil {
			rdLog.WithError(err).WithField("retryCount", retryCount).WithField("resourceType", resourceType).Warning("Unable to mutate nodeResource")
			continue
		}

		performGet = true
		if performUpdate {
			if _, err := cceClient.CceV2().NetResourceSets().Update(context.TODO(), netResourceSet, metav1.UpdateOptions{}); err != nil {
				if k8serrors.IsConflict(err) {
					rdLog.WithError(err).WithField("resourceType", resourceType).Warn("Unable to update NetResourceSet resource, will retry")
					continue
				}
				rdLog.WithError(err).WithField("resourceType", resourceType).Fatal("Unable to update NetResourceSet resource")
			} else {
				rdLog.WithField("resourceType", resourceType).Info("Successfully updated NetResourceSet resource")
				return true
			}
		} else {
			if _, err := cceClient.CceV2().NetResourceSets().Create(context.TODO(), netResourceSet, metav1.CreateOptions{}); err != nil {
				if k8serrors.IsConflict(err) {
					rdLog.WithError(err).WithField("resourceType", resourceType).Warn("Unable to create NetResourceSet resource, will retry")
					continue
				}
				rdLog.WithError(err).WithField("resourceType", resourceType).Fatal("Unable to create NetResourceSet resource")
			} else {
				rdLog.WithField("resourceType", resourceType).Info("Successfully created NetResourceSet resource")
				return true
			}
		}
	}

	rdLog.WithField("resourceType", resourceType).Fatal("Could not create or update NetResourceSet resource, despite retries")
	return false
}

func (rd *RdmaDiscovery) createOrUpdateERIManagedNRS() {
	nodeName := nodeTypes.GetName()
	instanceID, err := agent.GetInstanceID()
	if err != nil {
		rdLog.WithError(err).Fatal("get instance id fail")
	}
	eriManagedNRSName := bceutils.GetEriMangedNrsName(nodeName)

	rdLog.WithField("nrsName", eriManagedNRSName).Info("Creating ERI managed NetResourceSet")

	eriLabelSelectorValue := bceutils.GetRdmaNrsLabelSelectorValueFromNetResourceSetName(eriManagedNRSName, nodeName, instanceID, "", string(ccev2.ENIForERI))

	// Create a dedicated NRS for ERI management
	annotations := map[string]string{
		k8s.AnnotationRDMAInfoVifFeatures: string(ccev2.ENIForERI),
		k8s.AnnotationERIManaged:          "true", // Mark as ERI managed
	}

	rd.createOrUpdateNetResourceSet(eriManagedNRSName, annotations, eriLabelSelectorValue, string(ccev2.ENIForERI), "", "ERI managed")
}

func generateRdmaENISpec(vifFeatures string) (eni *api.ENISpec, err error) {
	vpcID, _, _, availabilityZone, err := agent.GetInstanceMetadata()
	if err != nil {
		return
	}

	if option.Config.ENI == nil {
		err = fmt.Errorf("ENIConfig is nil")
		return
	}

	eni = option.Config.ENI.DeepCopy()

	eni.VpcID = vpcID
	eni.InstanceType = vifFeatures
	eni.UseMode = string(ccev2.ENIUseModeSecondaryIP)
	eni.AvailabilityZone = availabilityZone
	eni.PreAllocateENI = 0
	eni.MaxAllocateENI = 0

	return
}

func (rd *RdmaDiscovery) mutateNodeResource(rdmaNetResourceSet *ccev2.NetResourceSet, labelSelectorValue, vifFeatures, macAddress string) error {
	// reset NetworkresourceSet.Spec.Addresses to avoid stale data
	rdmaNetResourceSet.Spec.Addresses = []ccev2.NodeAddress{}

	// If we are unable to fetch the K8s Node resource and the NetResourceSet does
	// not have an OwnerReference set, then somehow we are running in an
	// environment where only the NetResourceSet exists. Do not proceed as this is
	// unexpected.
	//
	// Note that we can rely on the OwnerReference to be set on the NetResourceSet
	// as this was added in sufficiently earlier versions of CCE (v1.6).
	// Source:
	// https://github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/commit/5c365f2c6d7930dcda0b8f0d5e6b826a64022a4f
	k8sNode, err := rd.k8sNodeGetter.GetK8sNode(
		context.TODO(),
		nodeTypes.GetName(),
	)
	switch {
	case err != nil && k8serrors.IsNotFound(err) && len(rdmaNetResourceSet.ObjectMeta.OwnerReferences) == 0:
		rdLog.WithError(err).WithField(
			logfields.NodeName, nodeTypes.GetName(),
		).Fatal(
			"Kubernetes Node resource does not exist, setting OwnerReference on " +
				"NetResourceSet is impossible. This is unexpected. Please investigate " +
				"why CCE is running on a Node that supposedly does not exist " +
				"according to Kubernetes.",
		)
	case err != nil && !k8serrors.IsNotFound(err):
		return fmt.Errorf("failed to fetch Kubernetes Node resource: %w", err)
	}

	rdmaNetResourceSet.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "v1",
		Kind:       "Node",
		Name:       nodeTypes.GetName(),
		UID:        k8sNode.UID,
	}}

	// Get the addresses from k8s node and add them as part of CCE Node.
	// CCE Node should contain all addresses from k8s.
	k8sNodeParsed := k8s.ParseNode(k8sNode)

	// overwrite the labels from k8s node
	if rdmaNetResourceSet.ObjectMeta.Labels == nil {
		rdmaNetResourceSet.ObjectMeta.Labels = map[string]string{}
	}
	for key, v := range k8sNodeParsed.Labels {
		rdmaNetResourceSet.ObjectMeta.Labels[key] = v
	}
	rdmaNetResourceSet.ObjectMeta.Labels[k8s.LabelNodeName] = labelSelectorValue

	for _, k8sAddress := range k8sNodeParsed.IPAddresses {
		k8sAddressStr := k8sAddress.IP.String()
		rdmaNetResourceSet.Spec.Addresses = append(rdmaNetResourceSet.Spec.Addresses, ccev2.NodeAddress{
			Type: k8sAddress.Type,
			IP:   k8sAddressStr,
		})
	}

	for _, address := range rd.localNode.IPAddresses {
		netResourceSetAddress := address.IP.String()
		var found bool
		for _, nodeResourceAddress := range rdmaNetResourceSet.Spec.Addresses {
			if nodeResourceAddress.IP == netResourceSetAddress {
				found = true
				break
			}
		}
		if !found {
			rdmaNetResourceSet.Spec.Addresses = append(rdmaNetResourceSet.Spec.Addresses, ccev2.NodeAddress{
				Type: address.Type,
				IP:   netResourceSetAddress,
			})
		}
	}

	instanceID, err := agent.GetInstanceID()
	if err != nil || instanceID == "" {
		rd.eventRecorder.Eventf(k8sNode, k8sTypes.EventTypeWarning, "MetaAPIError01", "failed to get instance id: %v", err)
		log.WithError(err).Fatal("get instance id fail")
	}
	rdmaNetResourceSet.Spec.InstanceID = instanceID

	if rdmaNetResourceSet.Spec.ENI == nil {
		eni, err := generateRdmaENISpec(vifFeatures)
		if err != nil {
			rd.eventRecorder.Eventf(k8sNode, k8sTypes.EventTypeWarning, "MetaAPIError02", "generate eni metadata error: %v", err)
			rdLog.WithError(err).Fatal("generate ENI spec fail")
		}
		rdmaNetResourceSet.Spec.ENI = eni

		// if eni use mode was set on the label of local node, we use it override the eni spec
		if eniUseMode, ok := rdmaNetResourceSet.Labels[k8s.LabelENIUseMode]; ok {
			rdmaNetResourceSet.Spec.ENI.UseMode = eniUseMode
		}
		if rdmaNetResourceSet.Spec.ENI.UseMode == string(ccev2.ENIUseModeSecondaryIP) {
			if option.Config.ENI != nil {
				rdmaNetResourceSet.Spec.ENI.InstallSourceBasedRouting = option.Config.ENI.InstallSourceBasedRouting
			}
		}
	}

	// reset eni spec when it is restart
	if rdmaNetResourceSet.Spec.ENI.UseMode != string(ccev2.ENIUseModePrimaryIP) {
		if option.Config.RDMAIPPoolMinAllocateIPs != 0 {
			rdmaNetResourceSet.Spec.IPAM.MinAllocate = option.Config.RDMAIPPoolMinAllocateIPs
		}
		if option.Config.RDMAIPPoolPreAllocate != 0 {
			rdmaNetResourceSet.Spec.IPAM.PreAllocate = option.Config.RDMAIPPoolPreAllocate
		}
		if option.Config.RDMAIPPoolMaxAboveWatermark != 0 {
			rdmaNetResourceSet.Spec.IPAM.MaxAboveWatermark = option.Config.RDMAIPPoolMaxAboveWatermark
		}
		if option.Config.ENI.RouteTableOffset > 0 {
			rdmaNetResourceSet.Spec.ENI.RouteTableOffset = option.Config.ENI.RouteTableOffset
		}
	}

	// update subnet and security group ids
	rdmaNetResourceSet.Spec.ENI.SecurityGroups = option.Config.ENI.SecurityGroups
	if len(rdmaNetResourceSet.Spec.ENI.SubnetIDs) == 0 {
		rdmaNetResourceSet.Spec.ENI.SubnetIDs = option.Config.ENI.SubnetIDs
	}
	if option.Config.ENI.UsePrimaryAddress != nil {
		rdmaNetResourceSet.Spec.ENI.UsePrimaryAddress = option.Config.ENI.UsePrimaryAddress
	}

	return nil
}

func (rd *RdmaDiscovery) RegisterK8sNodeGetter(k8sNodeGetter k8sNodeGetter) {
	rd.k8sNodeGetter = k8sNodeGetter
}

// LocalAllocCIDRsUpdated informs the agent that the local allocation CIDRs have
// changed. This will inform the datapath node manager to update the local node
// routes accordingly.
// The first CIDR in ipv[46]AllocCIDRs is presumed to be the primary CIDR: This
// CIDR remains assigned to the local node and may not be switched out or be
// removed.
func (rd *RdmaDiscovery) LocalAllocCIDRsUpdated(ipv4AllocCIDRs, ipv6AllocCIDRs []*cidr.CIDR) {
	rd.localNodeLock.Lock()
	defer rd.localNodeLock.Unlock()

	if option.Config.EnableIPv4 && len(ipv4AllocCIDRs) > 0 {
		ipv4PrimaryCIDR, ipv4SecondaryCIDRs := splitAllocCIDRs(ipv4AllocCIDRs)
		validatePrimaryCIDR(rd.localNode.IPv4AllocCIDR, ipv4PrimaryCIDR, ipam.IPv4)
		rd.localNode.IPv4AllocCIDR = ipv4PrimaryCIDR
		rd.localNode.IPv4SecondaryAllocCIDRs = ipv4SecondaryCIDRs
	}

	if option.Config.EnableIPv6 && len(ipv6AllocCIDRs) > 0 {
		ipv6PrimaryCIDR, ipv6SecondaryCIDRs := splitAllocCIDRs(ipv6AllocCIDRs)
		validatePrimaryCIDR(rd.localNode.IPv6AllocCIDR, ipv6PrimaryCIDR, ipam.IPv6)
		rd.localNode.IPv6AllocCIDR = ipv6PrimaryCIDR
		rd.localNode.IPv6SecondaryAllocCIDRs = ipv6SecondaryCIDRs
	}

	rd.Manager.NodeUpdated(rd.localNode)
}

func (rd *RdmaDiscovery) ResourceType() string {
	return ccev2.NetResourceSetEventHandlerTypeRDMA
}

// shouldExitOnRegularERINRS checks whether agent should exit when finding regular ERI RDMA NRS
func (rd *RdmaDiscovery) shouldExitOnRegularERINRS(rii bceutils.RdmaIfInfo) bool {
	return option.Config.EnableERIManagement && rii.VifFeatures == string(ccev2.ENIForERI)
}

// exitOnRegularERINRS exits the agent when finding regular ERI RDMA NRS
func (rd *RdmaDiscovery) exitOnRegularERINRS(rii bceutils.RdmaIfInfo, macAddress string) {
	regularNRSName := rii.NetResourceSetName

	rdLog.WithFields(logrus.Fields{
		"regularNRSName": regularNRSName,
		"macAddress":     macAddress,
	}).Error("Found regular ERI RDMA NRS while ERI management is enabled")

	cceClient := k8s.CCEClient()

	// Check if the regular ERI NRS exists
	_, err := cceClient.CceV2().NetResourceSets().Get(context.TODO(), regularNRSName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// NRS doesn't exist, this is normal, no need to exit
			rdLog.WithField("regularNRSName", regularNRSName).Debug("Regular ERI NRS not found, continuing normally")
			return
		}
		rdLog.WithError(err).WithFields(logrus.Fields{
			"regularNRSName": regularNRSName,
			"macAddress":     macAddress,
		}).Warning("Failed to check regular ERI RDMA NRS existence, continuing normally")
		return
	}

	// If we reach here, the regular ERI NRS exists and ERI management is enabled
	// This is a conflict situation, agent should exit
	rdLog.WithFields(logrus.Fields{
		"regularNRSName": regularNRSName,
		"macAddress":     macAddress,
	}).Fatal("Regular ERI RDMA NRS exists while ERI management is enabled. " +
		"This indicates a configuration conflict. Please clean up the regular ERI NRS manually " +
		"or disable ERI management before restarting the agent.")
}

// ERIOwnerWrapper is a simple ERI management owner wrapper
// Only overrides UpdateNetResourceSetResource method, other methods are directly proxied to RdmaDiscovery
type ERIOwnerWrapper struct {
	*RdmaDiscovery
}

// NewERIOwnerWrapper creates a simple ERI management owner wrapper
func NewERIOwnerWrapper(rdmaDiscovery *RdmaDiscovery) *ERIOwnerWrapper {
	return &ERIOwnerWrapper{
		RdmaDiscovery: rdmaDiscovery,
	}
}

// UpdateNetResourceSetResource overrides the method, only handles ERI managed NRS
func (ew *ERIOwnerWrapper) UpdateNetResourceSetResource() {
	if !option.Config.AutoCreateNetResourceSetResource {
		return
	}

	// Only create/update ERI managed NRS, not handle regular RDMA NRS
	rdLog.Info("Updating ERI managed NetResourceSet resource only")
	ew.createOrUpdateERIManagedNRS()
}
