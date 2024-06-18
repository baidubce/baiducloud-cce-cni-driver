// Copyright Authors of Baidu AI Cloud
// SPDX-License-Identifier: Apache-2.0

package enim

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/agent"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/enim/eniprovider"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccev1Lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/netns"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var (
	allocatorLog  = logging.NewSubysLogger("eni-endpoint-allocator")
	eniGCTaskName = "eni-manager-interval-gc"

	allocatorTimeout         = time.Second * 30
	waitCNICleanNetnsTimeout = time.Second * 2

	FinalizerPrimaryENIEndpoint = "primary-eni"
)

// eniEndpointAllocator manage endpoint exclusive ENI logic
type eniEndpointAllocator struct {
	lock              lock.Mutex
	cceEndpointClient *watchers.CCEEndpointClient
	podClient         *watchers.PodClient
	eniClient         *watchers.ENIClient
	subnetLister      ccev1Lister.SubnetLister
	mngr              *controller.Manager

	// provider provides the actual ENI allocate and release logic
	provider eniprovider.ENIProvider

	defaultNs ns.NetNS
}

// newENIEndpointAllocator manage endpoint exclusive ENI logic
func newENIEndpointAllocator(c ipam.Configuration, watcher *watchers.K8sWatcher) ENIManagerServer {
	eem := &eniEndpointAllocator{
		cceEndpointClient: watcher.NewCCEEndpointClient(),
		podClient:         watcher.NewPodClient(),
		eniClient:         watcher.NewENIClient(),
		provider:          agent.NewPrimaryENIPovider(watcher),
	}
	eem.subnetLister = k8s.CCEClient().Informers.Cce().V1().Subnets().Lister()

	// default netns
	defaultNs, err := ns.GetCurrentNS()
	if err != nil {
		allocatorLog.WithError(err).Fatal("can not open current netns")
	}
	eem.defaultNs = defaultNs

	mngr := controller.NewManager()
	mngr.UpdateController(eniGCTaskName,
		controller.ControllerParams{
			RunInterval:     time.Second,
			DisableDebugLog: true,
			DoFunc: func(ctx context.Context) error {
				eem.GC()
				return nil
			},
		})
	return eem
}

// ADD select an available ENI from the ENI list and assign it to the
// network endpoint
func (eem *eniEndpointAllocator) ADD(owner, containerID, netnsPath string) (
	ipv4Result, ipv6Result *models.IPAMAddressResponse, err error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(owner)
	if err != err {
		err = fmt.Errorf("failed to split network endpoint owner %s", owner)
		return
	}

	var (
		ctx, cancelFun = context.WithTimeout(context.TODO(), option.Config.GetFixedIPTimeout())
		logEntry       = allocatorLog.WithFields(logrus.Fields{
			"namespace":   namespace,
			"name":        name,
			"method":      "ADD",
			"containerID": containerID,
		})
		startTime = time.Now()
	)
	ctx = logfields.WithtTraceID(ctx)
	logEntry = logEntry.WithContext(ctx)
	defer func() {
		logEntry = logEntry.WithFields(logrus.Fields{
			"ipv4":         logfields.Repr(ipv4Result),
			"ipv6":         logfields.Repr(ipv6Result),
			"duration(ms)": time.Now().Sub(startTime).Milliseconds(),
		})
		if err != nil {
			logEntry.WithError(err).Error("failed to add primary ENI")
			return
		}
		cancelFun()
		if ipv4Result != nil {
			logEntry = logEntry.WithField("ipv4", ipv4Result.IP)
		}
		if ipv6Result != nil {
			logEntry = logEntry.WithField("ipv6", ipv6Result.IP)
		}
		logEntry.Infof("primary ENI add success")
	}()

	return eem.allocateENI(ctx, logEntry, namespace, name, containerID, netnsPath)
}

// allocateENI task:
//  1. build the endpoint template
//
// 2. try to allocate eni, if error occured, try rollback eni status
// 3. update eni status
func (eem *eniEndpointAllocator) allocateENI(
	ctx context.Context, logEntry *logrus.Entry,
	namespace, name, containerID, netnsPath string) (
	ipv4Result, ipv6Result *models.IPAMAddressResponse, err error) {
	logEntry.WithField(logfields.Step, "1").Debug("start build ENI endpoint template")
	pod, err := eem.podClient.Get(namespace, name)
	if err != nil {
		err = fmt.Errorf("failed to get pod, please retry")
		return
	}
	ep, err := endpoint.GetEndpointCrossCache(ctx, eem.cceEndpointClient, namespace, name)
	if err != nil {
		return
	}
	if ep != nil {
		err = endpoint.DeleteEndpointAndWait(ctx, eem.cceEndpointClient, ep)
		if err != nil {
			return
		}
	}

	ep = endpoint.NewEndpointTemplate(containerID, netnsPath, pod)
	ep.Finalizers = append(ep.Finalizers, FinalizerPrimaryENIEndpoint)
	ep.Spec.Network.IPAllocation.Type = ccev2.IPAllocTypeENIPrimary
	ep.Spec.Network.IPAllocation.ReleaseStrategy = ccev2.ReleaseStrategyTTL

	logEntry.WithField(logfields.Step, "1").Debug("try to create endpoint on k8s")
	ep, err = eem.cceEndpointClient.CCEEndpoints(ep.Namespace).Create(ctx, ep, metav1.CreateOptions{})
	ep = ep.DeepCopy()
	reference := ep.AsObjectReference()

	logEntry.WithField(logfields.Step, "2").Debug("try to allocate eni thorugh bce provider")
	eni, err := eem.provider.AllocateENI(ctx, reference)
	if err != nil {
		return
	}
	logEntry = logEntry.WithFields(logrus.Fields{
		"eniID":    eni.Spec.ENI.ID,
		"subnetID": eni.Spec.ENI.SubnetID,
	})

	defer func() {
		if err != nil {
			logEntry.Error("try to rollback eni")
			e := eem.provider.ReleaseENI(ctx, reference)
			if e != nil {
				logEntry.WithError(e).Error("failed to rollback eni allocate, please feed back to CCE team")
			}
		}
	}()

	logEntry.WithField(logfields.Step, "3").Debug("try to fill endpoint by ENI")
	ipv4Result, ipv6Result, err = eem.fillEndpintByENI(ctx, eni, ep)
	if err != nil {
		return
	}

	logEntry.WithField(logfields.Step, "4").Debug("try to create endpoint on k8s")
	_, err = eem.cceEndpointClient.CCEEndpoints(ep.Namespace).Update(ctx, ep, metav1.UpdateOptions{})
	return
}

// fillEndpintByENI build a endpoint by eni
func (eem *eniEndpointAllocator) fillEndpintByENI(ctx context.Context, eni *ccev2.ENI, ep *ccev2.CCEEndpoint) (
	ipv4Result, ipv6Result *models.IPAMAddressResponse,
	err error) {
	var addrList ccev2.AddressPairList
	if ipv4Primary := ccev2.GetPrimaryIPs(eni.Spec.ENI.PrivateIPSet); ipv4Primary != nil {
		ipv4Primary.SubnetID = eni.Spec.ENI.SubnetID
		ipv4Result, err = eem.buildIPAMAddressByPrivateAddr(ipv4Primary, eni.Spec.ENI.MacAddress, strconv.Itoa(eni.Status.InterfaceIndex))
		if err != nil {
			return
		}
		addrList = append(addrList, &ccev2.AddressPair{
			IP:        ipv4Primary.PrivateIPAddress,
			Family:    ccev2.IPv4Family,
			CIDRs:     ipv4Result.Cidrs,
			Gateway:   ipv4Result.Gateway,
			VPCID:     eni.Spec.ENI.VpcID,
			Subnet:    eni.Spec.ENI.SubnetID,
			Interface: eni.Spec.ENI.ID,
		})
	}
	if ipv6Primary := ccev2.GetPrimaryIPs(eni.Spec.ENI.IPV6PrivateIPSet); ipv6Primary != nil {
		ipv6Primary.SubnetID = eni.Spec.ENI.SubnetID
		ipv6Result, err = eem.buildIPAMAddressByPrivateAddr(ipv6Primary, eni.Spec.ENI.MacAddress, strconv.Itoa(eni.Status.InterfaceIndex))
		if err != nil {
			return
		}
		addrList = append(addrList, &ccev2.AddressPair{
			IP:        ipv6Primary.PrivateIPAddress,
			Family:    ccev2.IPv6Family,
			CIDRs:     ipv4Result.Cidrs,
			Gateway:   ipv4Result.Gateway,
			VPCID:     eni.Spec.ENI.VpcID,
			Subnet:    eni.Spec.ENI.SubnetID,
			Interface: eni.Spec.ENI.ID,
		})
	}

	ep.Status = ccev2.EndpointStatus{
		ExternalIdentifiers: ep.Spec.ExternalIdentifiers,
		Networking: &ccev2.EndpointNetworking{
			Addressing: addrList,
			NodeIP:     nodeTypes.GetName(),
		},
	}
	endpoint.AppendEndpointStatus(&ep.Status, models.EndpointStateIPAllocated, models.EndpointStatusChangeCodeOk)
	return
}

func (eem *eniEndpointAllocator) buildIPAMAddressByPrivateAddr(primaryAddr *models.PrivateIP, mac, number string) (*models.IPAMAddressResponse, error) {
	addrResult := &models.IPAMAddressResponse{
		IP:              primaryAddr.PrivateIPAddress,
		MasterMac:       mac,
		InterfaceNumber: number,
	}
	sbn, err := eem.subnetLister.Get(primaryAddr.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get eni subnet by subnet ID: %s", primaryAddr.SubnetID)
	}
	addrResult.Cidrs = []string{sbn.Spec.CIDR}

	_, cidrNet, err := net.ParseCIDR(sbn.Spec.CIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CIDR (%s) of subnet by subnet ID: %s", sbn.Spec.CIDR, primaryAddr.SubnetID)
	}
	// ipNet := net.IPNet{
	// 	IP:   net.ParseIP(primaryAddr.PrivateIPAddress),
	// 	Mask: cidrNet.Mask,
	// }
	addrResult.IP = primaryAddr.PrivateIPAddress

	// gateway address usually the first IP is IPv4 address
	gw := ip.GetIPAtIndex(*cidrNet, int64(1))
	if gw == nil {
		return nil, fmt.Errorf("failed to generate gateway by CIDR (%s) of subnet (%s)", sbn.Spec.CIDR, primaryAddr.SubnetID)
	}
	addrResult.Gateway = gw.String()

	return addrResult, nil
}

// DEL  recycle the endpoint has been recycled and needs to trigger the
// reuse of ENI
func (eem *eniEndpointAllocator) DEL(owner, containerID, netnsPath string) (err error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(owner)
	if err != err {
		err = fmt.Errorf("failed to split network endpoint owner %s", owner)
		return
	}

	var (
		ctx, cancelFun = context.WithTimeout(context.TODO(), option.Config.GetFixedIPTimeout())
		logEntry       = allocatorLog.WithFields(logrus.Fields{
			"namespace":   namespace,
			"name":        name,
			"method":      "DEL",
			"containerID": containerID,
		})
		startTime = time.Now()
	)
	ctx = logfields.WithtTraceID(ctx)
	logEntry = logEntry.WithContext(ctx)
	defer func() {
		logEntry = logEntry.WithFields(logrus.Fields{
			"duration(ms)": time.Now().Sub(startTime).Milliseconds(),
		})
		if err != nil {
			logEntry.WithError(err).Error("failed to del primary ENI")
			return
		}
		cancelFun()
		logEntry.Infof("del primary ENI success")
	}()

	return eem.delEndpointBySameContainerID(ctx, logEntry, namespace, name, containerID, netnsPath)
}

// delEndpointBySameContainerID only delete endpoint when same container id between endpoint and container
func (eem *eniEndpointAllocator) delEndpointBySameContainerID(
	ctx context.Context, logEntry *logrus.Entry,
	namespace, name, containerID, netnsPath string) (err error) {
	logEntry.WithField(logfields.Step, "1").Debug("start validate endpoint")
	ep, err := endpoint.GetEndpointCrossCache(ctx, eem.cceEndpointClient, namespace, name)
	if err != nil || ep == nil {
		// have already been cleaned up
		if kerrors.IsNotFound(err) {
			err = nil
		}
		return
	}

	ipAllocateType := "None"
	if ep.Spec.Network.IPAllocation != nil {
		ipAllocateType = string(ep.Spec.Network.IPAllocation.Type)
	}
	if ep.Spec.Network.IPAllocation.Type != ccev2.IPAllocTypeENIPrimary {
		return fmt.Errorf("can not release endpoint with type (%s)", ipAllocateType)
	}

	if ep.Spec.ExternalIdentifiers.ContainerID != containerID {
		logEntry.WithField(logfields.Step, "2").Debug("the endpont is not the same containerID as in the container, so don't delete the endpoint")
		return
	}

	logEntry.WithField(logfields.Step, "3").Debug("try to delete endpoint on k8s")
	return eem.cceEndpointClient.CCEEndpoints(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// GC the eni manager needs to implement the garbage cleaning logic for
// the endpoint. When the endpoint no longer exists, it needs to trigger
// the ENI release logic.
// Warning When releasing ENI, it also involves device management. It is
// necessary to move the network device put into the container namespace
// to the host namespace, and rename the device when moving.
func (eem *eniEndpointAllocator) GC() error {

	logEntry := allocatorLog.WithField("method", "GC").WithContext(logfields.NewContext())
	ctx := logfields.NewContext()
	// move device from host into container ns
	// We don't care if an error was returned
	eem.cleanExpiredCEP(logEntry, ctx)

	// clean up the ENI that has been released
	enis, err := eem.eniClient.List()
	if err != nil {
		logEntry.WithError(err).Error("failed to list ENI")
		return err
	}
	for _, eni := range enis {
		if eni.Status.EndpointReference != nil {
			eniLog := logEntry.WithFields(logrus.Fields{
				"namespace": eni.Status.EndpointReference.Namespace,
				"name":      eni.Status.EndpointReference.Name,
				"uid":       eni.Status.EndpointReference.UID,
				"eni":       eni.Name,
			})
			cep, err := eem.cceEndpointClient.CCEEndpoints(eni.Status.EndpointReference.Namespace).Get(ctx, eni.Status.EndpointReference.Name, metav1.GetOptions{})
			if kerrors.IsNotFound(err) {
				eniLog.Warnf("cce endpoint not found")
				if len(eni.Status.CCEStatusChangeLog) > 0 &&
					eni.Status.CCEStatusChangeLog[len(eni.Status.CCEStatusChangeLog)-1].Time.
						Add(defaults.CCEEndpointGCInterval).
						Before(time.Now()) {
					err = eem.provider.ReleaseENI(ctx, eni.Status.EndpointReference)
					if err != nil {
						eniLog.WithError(err).Error("release primary ENI failed")
						continue
					}
					eniLog.Info("release primary ENI successfully while cce endpoint not found")
				}
			}
			if err == nil && cep != nil {
				// if the cce endpoint is not the same as the eni, it means that the cce endpoint has been deleted
				if string(cep.GetUID()) != eni.Status.EndpointReference.UID {
					releaseLog := eniLog.WithField("cepUID", cep.GetUID())
					err = eem.provider.ReleaseENI(ctx, eni.Status.EndpointReference)
					if err != nil {
						releaseLog.WithError(err).Error("release primary ENI failed")
					}
					releaseLog.Info("release primary ENI successfully while eni reference is not the same as cce endpoint")
				}
			}
		}
	}
	return nil
}

// cleanExpiredCEP clean expired CEP
// 1. CEP with no status
// 2. CEP with status but eni interface still exist in container namespace
func (eem *eniEndpointAllocator) cleanExpiredCEP(logEntry *logrus.Entry, ctx context.Context) {
	expiredCEPs, noStatusCEPs := eem.scanExpiredEndpoint(logEntry)
	for _, expired := range expiredCEPs {
		var eniName string
		for _, address := range expired.Status.Networking.Addressing {
			if address.Interface != "" {
				eniName = address.Interface
				break
			}
		}
		eni, err := eem.eniClient.Get(eniName)
		if err != nil {
			logEntry.WithError(err).Errorf("failed to get ENI(%s)", eniName)
			continue
		}

		gcLog := logEntry.WithFields(logrus.Fields{
			"namespace": expired.Namespace,
			"name":      expired.Name,
			"eniName":   eniName,
		})
		mac := eni.Spec.ENI.MacAddress
		if _, err := link.FindENILinkByMac(mac); err != nil {
			err := netns.WithContainerNetns(expired.Status.ExternalIdentifiers.Netns, func(nn ns.NetNS) error {
				dev, err := link.FindENILinkByMac(mac)
				if err != nil {
					return fmt.Errorf("failed to find dev: %v", err)
				}
				return link.MoveAndRenameLink(dev, eem.defaultNs, dev.Attrs().Alias)
			})
			if err != nil {
				gcLog.WithError(err).Warnf("failed to move link to init netns")
			}
		}
		gcLog.Infof("eni have been move to init netns")

		err = eem.provider.ReleaseENI(ctx, expired.AsObjectReference())
		if err != nil {
			gcLog.WithError(err).Error("release primary ENI failed")
			continue
		}
		eem.removePrimaryENIFinalizer(gcLog, ctx, expired)
	}

	for _, expired := range noStatusCEPs {
		gcLog := logEntry.WithFields(logrus.Fields{
			"namespace": expired.Namespace,
			"name":      expired.Name,
		})
		eem.removePrimaryENIFinalizer(gcLog, ctx, expired)
	}
}

// removePrimaryENIFinalizer remove primary ENI finalizer
func (eem *eniEndpointAllocator) removePrimaryENIFinalizer(gcLog *logrus.Entry, ctx context.Context, expired *ccev2.CCEEndpoint) {
	var finalizers []string
	for _, f := range expired.Finalizers {
		if f != FinalizerPrimaryENIEndpoint {
			finalizers = append(finalizers, f)
		}
	}
	expired.Finalizers = finalizers
	_, err := eem.cceEndpointClient.CCEEndpoints(expired.Namespace).Update(ctx, expired, metav1.UpdateOptions{})
	if err != nil {
		gcLog.WithError(err).Error("fail to update cce endpoint")
		return
	}
	gcLog.Infof("successfully release primary ENI")
}

// scanExpiredEndpoint scan expired endpoint.
// Endpoints that have expired are those that have been triggered for deletion
// and have exceeded the time allowed for CNI to recycle them, which is 2 seconds
// by default.
func (eem *eniEndpointAllocator) scanExpiredEndpoint(logEntry *logrus.Entry) (expiredEPs, noStatusEPs []*ccev2.CCEEndpoint) {
	endpoints, err := eem.cceEndpointClient.List()
	if err != nil {
		logEntry.Errorf("Error listing endpoints: %s", err)
		return
	}
	for _, ep := range endpoints {
		if ep.Spec.Network.IPAllocation == nil || ep.Spec.Network.IPAllocation.Type != ccev2.IPAllocTypeENIPrimary {
			continue
		}

		if ep.DeletionTimestamp == nil || ep.DeletionTimestamp.Add(waitCNICleanNetnsTimeout).After(time.Now()) {
			continue
		}
		if ep.Status.ExternalIdentifiers == nil || ep.Status.ExternalIdentifiers.Netns == "" {
			noStatusEPs = append(noStatusEPs, ep.DeepCopy())
			continue
		}
		expiredEPs = append(expiredEPs, ep.DeepCopy())
	}
	return
}

var _ ENIManagerServer = &eniEndpointAllocator{}
