package agent

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/qos"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/enim/eniprovider"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/health/plugin"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/os"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var initLog = logging.NewSubysLogger("agent-eni-init-factory")

// eniInitFactory bce ENI provider for k8s
// listening update event of ENI, create and set device/route on the woker machine
type eniInitFactory struct {
	// Available eni
	localENIs map[string]*ccev2.ENI
	fullENIs  map[string]*ccev2.ENI

	eniClient *watchers.ENIClient

	// the host os release
	release *os.OSRelease
}

// RegisterENIInitFatory create a eniInitFactory and register it to ENI event handler
func RegisterENIInitFatory(watcher *watchers.K8sWatcher) {
	eniHandler := &eniInitFactory{
		localENIs: make(map[string]*ccev2.ENI),
		fullENIs:  make(map[string]*ccev2.ENI),
		eniClient: watcher.NewENIClient(),
	}
	qos.InitEgressPriorityManager()
	eniHandler.release, _ = os.NewOSDistribution()

	watcher.RegisterENISubscriber(eniHandler)
	plugin.RegisterPlugin("eni-init-factory", eniHandler)
	qos.GlobalManager.Start(watcher.NewCCEEndpointClient())
}

func (eh *eniInitFactory) Check() error {
	if len(eh.localENIs) == 0 {
		return fmt.Errorf("no eni local config is provided ")
	}
	return nil
}

func (eh *eniInitFactory) OnAddENI(node *ccev2.ENI) error {
	return nil
}

// OnUpdateENI It will be recalled every 30s
func (eh *eniInitFactory) OnUpdateENI(oldObj, newObj *ccev2.ENI) error {
	var err error
	resource := newObj.DeepCopy()
	eh.fullENIs[resource.Spec.ENI.ID] = resource
	scopedLog := initLog.WithError(err).WithField("eni", resource.Spec.ENI.ID)

	if resource.Status.VPCStatus != ccev2.VPCENIStatusInuse {
		scopedLog.Debugf("eni is not in use, skip status [%s]", resource.Status.VPCStatus)
		return nil
	}

	if resource.Spec.MacAddress == "" {
		scopedLog.Debug("mac address is empty, skip update ENI")
		return nil
	}

	// downward compatibility with BBC models
	// TODO 2021-03-08: remove this after BBC secondary models are deprecated
	if resource.Spec.Type == ccev2.ENIForBBC {
		resource.Spec.UseMode = ccev2.ENIUseModePrimaryWithSecondaryIP
	}

	eniLink, err := newENILink(resource, eh.release)
	if err != nil {
		scopedLog.WithError(err).Error("Get eniLink falied")
		return err
	}
	switch resource.Spec.UseMode {
	case ccev2.ENIUseModePrimaryIP:
		if err = eniLink.rename(true); err != nil {
			scopedLog.WithError(err).Error("rename eniLink falied")
			return err
		}
	case ccev2.ENIUseModePrimaryWithSecondaryIP:
		// primary interface with secondary IP mode
		// do not need to rename eniLink
	default:
		// secondary interface with secondary IP mode for RDMA
		// do not need to rename eniLink
		if resource.Spec.Type != ccev2.ENIForHPC && resource.Spec.Type != ccev2.ENIForERI {
			// eni with secondary IP mode need to rename eniLink
			if err = eniLink.rename(false); err != nil {
				scopedLog.WithError(err).Error("rename eniLink falied")
				return err
			}
			// set rule when eni secondary IP mode
			err = eniLink.ensureLinkConfig()
			if err != nil {
				scopedLog.WithError(err).Error("set eniLink falied")
				return err
			}
			if resource.Status.CCEStatus == ccev2.ENIStatusReadyOnNode {
				if resource.Spec.InstallSourceBasedRouting {
					err = ensureENIRule(scopedLog, resource)
					if err != nil {
						scopedLog.WithError(err).Error("install source based routing falied")
						return err
					}
				}
			}
		}
	}

	// set eni neigbor config
	err = eniLink.ensureENINeigh()
	if err != nil {
		return fmt.Errorf("failed to set eni neighbor config: %w", err)
	}

	// set device and route on the woker machine only when eni bound at bcc
	if _, ok := eh.localENIs[resource.Spec.ENI.ID]; !ok {
		resource.Status.InterfaceIndex = eniLink.linkIndex
		resource.Status.InterfaceName = eniLink.linkName
		resource.Status.ENIIndex = eniLink.eniIndex
		if eniLink.ipv4Gateway != "" {
			resource.Status.GatewayIPv4 = eniLink.ipv4Gateway
		}
		if eniLink.ipv6Gateway != "" {
			resource.Status.GatewayIPv6 = eniLink.ipv6Gateway
		}

		if !reflect.DeepEqual(&resource.Status, &newObj.Status) {
			(&resource.Status).AppendCCEENIStatus(ccev2.ENIStatusReadyOnNode)

			_, err = eh.eniClient.ENIs().UpdateStatus(context.TODO(), resource, metav1.UpdateOptions{})
			if err != nil {
				scopedLog.WithError(err).Error("update eni status")
				return err
			}
		}
	}

	eh.localENIs[resource.Spec.ENI.ID] = resource
	qos.GlobalManager.ENIUpdateEventHandler(resource)

	return err
}

func (eh *eniInitFactory) OnDeleteENI(eni *ccev2.ENI) error {
	initLog.WithField("eni", eni.Spec.ENI.ID).Warn("eni was deleted by external")
	return nil
}

var _ subscriber.ENI = &eniInitFactory{}

// primaryENIPovider only handler ENI which use mode is Primary
// It maintains the resource pool of the exclusive ENI and handles all the
// applications for the exclusive ENI and the state machine requests for release.
// The provider also realizes the competitive management of ENI resources.
// Only one endpoint is allowed to use an ENI device at the same time
type primaryENIPovider struct {
	lock sync.Mutex

	// all of primary ENIs
	fullENIs     map[string]*ccev2.ENI
	inuseENIs    map[string]*ccev2.ENI
	avaiableENIs map[string]*ccev2.ENI

	eniClient *watchers.ENIClient
}

// NewPrimaryENIPovider create a new primary ENI povider and register it to ENI event handler
func NewPrimaryENIPovider(watcher *watchers.K8sWatcher) eniprovider.ENIProvider {
	pep := &primaryENIPovider{
		fullENIs:     make(map[string]*ccev2.ENI),
		inuseENIs:    make(map[string]*ccev2.ENI),
		avaiableENIs: make(map[string]*ccev2.ENI),

		eniClient: watcher.NewENIClient(),
	}

	watcher.RegisterENISubscriber(pep)
	plugin.RegisterPlugin("primaryENIPovider", pep)
	return pep
}

// Check whether a primary ENI is already being used or not
func (pep *primaryENIPovider) Check() error {
	if len(pep.avaiableENIs) == 0 && len(pep.inuseENIs) == 0 {
		return fmt.Errorf("no ENIs available")
	}
	return nil
}

// AllocateENI implements eniprovider.ENIProvider
func (pep *primaryENIPovider) AllocateENI(ctx context.Context, endpoint *ccev2.ObjectReference) (*ccev2.ENI, error) {
	pep.lock.Lock()
	defer pep.lock.Unlock()

	// reuse the eni's that were already allocated for this endpoint
	for key := range pep.inuseENIs {
		eni := pep.inuseENIs[key]
		if eni.Status.EndpointReference.Namespace == endpoint.Namespace &&
			eni.Status.EndpointReference.Name == endpoint.Name {
			if eni.Status.EndpointReference.UID == endpoint.UID {
				return eni, nil
			} else {
				go func() {
					pep.ReleaseENI(ctx, eni.Status.EndpointReference)
				}()
			}
		}
	}

	if len(pep.avaiableENIs) == 0 {
		return nil, fmt.Errorf("no available ENI to be allocated")
	}
	var key string
	var err error
	for key = range pep.avaiableENIs {
		break
	}
	eni := pep.avaiableENIs[key].DeepCopy()
	(&eni.Status).AppendCCEENIStatus(ccev2.ENIStatusUsingInPod)
	eni.Status.EndpointReference = endpoint
	if !reflect.DeepEqual(eni.Status, pep.avaiableENIs[key].Status) {
		eni, err = pep.eniClient.ENIs().UpdateStatus(ctx, eni, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("update ENI status: %v", err)
		}
	}
	pep.putInuseENI(eni, false)
	return eni, nil
}

// ReleaseENI implements eniprovider.ENIProvider
func (pep *primaryENIPovider) ReleaseENI(ctx context.Context, endpoint *ccev2.ObjectReference) error {
	pep.lock.Lock()
	defer pep.lock.Unlock()

	// same uid, release the eni
	for _, eni := range pep.inuseENIs {
		if eni.Status.EndpointReference != nil &&
			eni.Status.EndpointReference.Name == endpoint.Name &&
			eni.Status.EndpointReference.Namespace == endpoint.Namespace &&
			eni.Status.EndpointReference.UID == endpoint.UID {
			var err error
			newENI := eni.DeepCopy()
			newENI.Status.EndpointReference = nil
			(&newENI.Status).AppendCCEENIStatus(ccev2.ENIStatusReadyOnNode)
			if !reflect.DeepEqual(eni.Status, newENI.Status) {
				newENI, err = pep.eniClient.ENIs().UpdateStatus(ctx, newENI, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("failed to update ENI status: %v", err)
				}
			}
			pep.putAvailableENI(newENI, false)
			return nil
		}
	}
	return nil
}

// OnAddENI implements subscriber.ENI
func (*primaryENIPovider) OnAddENI(node *ccev2.ENI) error {
	return nil
}

// OnDeleteENI implements subscriber.ENI
func (pep *primaryENIPovider) OnDeleteENI(eni *ccev2.ENI) error {
	pep.lock.Lock()
	defer pep.lock.Unlock()

	delete(pep.fullENIs, eni.Name)
	delete(pep.inuseENIs, eni.Name)
	delete(pep.avaiableENIs, eni.Name)
	return nil
}

// OnUpdateENI implements subscriber.ENI
func (pep *primaryENIPovider) OnUpdateENI(oldObj *ccev2.ENI, resource *ccev2.ENI) error {
	if resource.Status.VPCStatus == ccev2.VPCENIStatusInuse {
		switch resource.Status.CCEStatus {
		case ccev2.ENIStatusReadyOnNode:
			pep.putAvailableENI(resource, true)
		case ccev2.ENIStatusUsingInPod:
			pep.putInuseENI(resource, true)
		default:
			pep.OnDeleteENI(resource)
		}
		return nil
	}
	return pep.OnDeleteENI(resource)
}

func (pep *primaryENIPovider) putInuseENI(resource *ccev2.ENI, useLock bool) {
	if useLock {
		pep.lock.Lock()
		defer pep.lock.Unlock()
	}
	pep.inuseENIs[resource.Name] = resource
	pep.fullENIs[resource.Name] = resource
	delete(pep.avaiableENIs, resource.Name)
}

func (pep *primaryENIPovider) putAvailableENI(resource *ccev2.ENI, useLock bool) {
	if useLock {
		pep.lock.Lock()
		defer pep.lock.Unlock()
	}
	pep.avaiableENIs[resource.Name] = resource
	pep.fullENIs[resource.Name] = resource
	delete(pep.inuseENIs, resource.Name)
}

var _ subscriber.ENI = &primaryENIPovider{}
var _ eniprovider.ENIProvider = &primaryENIPovider{}
