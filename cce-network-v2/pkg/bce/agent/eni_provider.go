package agent

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/qos"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/enim/eniprovider"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/health/plugin"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	osutil "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/os"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var initLog = logging.NewSubysLogger("agent-eni-init-factory")

const (
	pciDevicesPath = "/sys/bus/pci/devices"
	virtioVendorID = 0x1af4 // Virtio device vendor ID
	networkClass   = 0x0200 // Network device class code

	// Environment variable names for metrics
	envK8SNodeName = "K8S_NODE_NAME"
	envPodName     = "POD_NAME"

	// Default values for metrics
	defaultUnknownValue = "unknown"
)

// eniInitFactory bce ENI provider for k8s
// listening update event of ENI, create and set device/route on the woker machine
type eniInitFactory struct {
	// Available eni
	localENIs map[string]*ccev2.ENI
	fullENIs  map[string]*ccev2.ENI

	eniClient *watchers.ENIClient

	// the host os release
	release *osutil.OSRelease
}

// RegisterENIInitFatory create a eniInitFactory and register it to ENI event handler
func RegisterENIInitFatory(watcher *watchers.K8sWatcher) {
	eniHandler := &eniInitFactory{
		localENIs: make(map[string]*ccev2.ENI),
		fullENIs:  make(map[string]*ccev2.ENI),
		eniClient: watcher.NewENIClient(),
	}
	qos.InitEgressPriorityManager()
	eniHandler.release, _ = osutil.NewOSDistribution()

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

// checkAndRebindPCIDevice checks for missing PCI devices and attempts to rebind them
// Returns: whether device rebinding was successful
func (eh *eniInitFactory) checkAndRebindPCIDevice(scopedLog *logrus.Entry) (bool, error) {
	// Execute lspci command to get the list of Virtio network devices

	virtioDevices, err := findVirtioNetDevices()
	if err != nil {
		scopedLog.WithError(err).Error("Failed to execute lspci command")
		return false, fmt.Errorf("failed to execute lspci command: %w", err)
	}

	if len(virtioDevices) == 0 {
		scopedLog.Debug("No Virtio network devices found")
		return false, nil
	}
	needRebinding := false
	rebindSuccess := false

	// Check if each PCI device has a corresponding network interface
	for _, pciID := range virtioDevices {
		//pciID := match[1]
		scopedLog.Debugf("Checking PCI device: %s", pciID)

		// Check if the PCI device corresponds to a network interface
		// by checking symlinks in /sys/class/net/
		netDir, err := filepath.Glob("/sys/class/net/*")
		if err != nil {
			scopedLog.WithError(err).Error("Failed to list network interfaces")
			continue
		}

		deviceFound := false
		for _, devicePath := range netDir {
			// Get the directory name
			deviceName := filepath.Base(devicePath)
			// Check if the device directory contains a symlink to the PCI device
			linkPath := filepath.Join("/sys/class/net", deviceName)
			if linkDest, err := os.Readlink(linkPath); err == nil {
				if strings.Contains(linkDest, string(pciID)) {
					deviceFound = true
					break
				} else {
					scopedLog.Debugf("Device %s is not the PCI device %s", deviceName, pciID)
				}
			}
		}

		if !deviceFound {
			needRebinding = true
			scopedLog.Infof("Found missing PCI device: %s", pciID)

			// Unbind then rebind the PCI device
			unbindPath := filepath.Join("/sys/bus/pci/drivers/virtio-pci/unbind")
			err := os.WriteFile(unbindPath, []byte(pciID), 0200)
			if err != nil {
				scopedLog.WithError(err).Warningf("Failed to unbind PCI device: %s", pciID)
				// if unbind failed, continue to check the next PCI device
			}

			// Short wait to ensure unbind completes
			time.Sleep(500 * time.Millisecond)

			bindPath := filepath.Join("/sys/bus/pci/drivers/virtio-pci/bind")
			// 0200 is the permission to write only the file. If the bindPath do not exist,
			// the os will create the file with only owner can write (0600 permission).
			if _, err := os.Stat(bindPath); os.IsNotExist(err) {
				scopedLog.Warningf("Bind path does not exist, file: %s", bindPath)
				return false, fmt.Errorf("bind path does not exist: %s", bindPath)
			}

			err = os.WriteFile(bindPath, []byte(pciID), 0200)
			if err != nil {
				scopedLog.WithError(err).Errorf("Failed to rebind PCI device: %s", pciID)
				// if rebind failed, continue to check the next PCI device
				continue
			}

			scopedLog.Infof("Successfully rebound PCI device: %s", pciID)
			rebindSuccess = true
		} else {
			scopedLog.Debugf("PCI device already has a network interface: %s", pciID)
		}
	}

	// If any devices were rebound, wait for the network interfaces to initialize
	if needRebinding && rebindSuccess {
		scopedLog.Info("PCI devices were rebound, waiting 4 seconds for network interfaces to initialize")
		time.Sleep(4 * time.Second)
		return true, nil
	}

	if needRebinding && !rebindSuccess {
		return false, fmt.Errorf("%s: PCI devices need rebinding but the operation failed", ccev2.ENIMSGPCIERROR)
	}

	return false, nil
}

func findVirtioNetDevices() ([]string, error) {
	// 获取所有 PCI 设备目录
	deviceDirs, err := ioutil.ReadDir(pciDevicesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read PCI devices directory: %w", err)
	}

	var virtioDevices []string

	// 遍历所有 PCI 设备
	for _, deviceDir := range deviceDirs {
		pciID := deviceDir.Name()
		devicePath := filepath.Join(pciDevicesPath, pciID)

		// 检查设备是否是网络设备
		isNetDevice, err := isNetworkDevice(devicePath)
		if err != nil || !isNetDevice {
			continue
		}

		// 检查设备是否是 Virtio 设备
		isVirtio, err := isVirtioDevice(devicePath)
		if err != nil || !isVirtio {
			continue
		}

		virtioDevices = append(virtioDevices, pciID)
	}

	return virtioDevices, nil
}

func isNetworkDevice(devicePath string) (bool, error) {
	// 读取设备类文件
	classFile := filepath.Join(devicePath, "class")
	classData, err := os.ReadFile(classFile)
	if err != nil {
		return false, fmt.Errorf("failed to read class file: %w", err)
	}

	// 解析类值
	classStr := strings.TrimSpace(string(classData))
	classInt, err := strconv.ParseUint(strings.TrimPrefix(classStr, "0x"), 16, 32)
	if err != nil {
		return false, fmt.Errorf("failed to parse class: %w", err)
	}

	// 检查是否是网络设备 (class 0x02开头)
	return ((classInt >> 16) & 0xFF) == 0x02, nil
}

func isVirtioDevice(devicePath string) (bool, error) {
	// 读取厂商ID文件
	vendorFile := filepath.Join(devicePath, "vendor")
	vendorData, err := os.ReadFile(vendorFile)
	if err != nil {
		return false, fmt.Errorf("failed to read vendor file: %w", err)
	}

	// 解析厂商ID
	vendorStr := strings.TrimSpace(string(vendorData))
	vendorInt, err := strconv.ParseUint(strings.TrimPrefix(vendorStr, "0x"), 16, 32)
	if err != nil {
		return false, fmt.Errorf("failed to parse vendor ID: %w", err)
	}

	// 检查是否是 Virtio 设备
	return uint32(vendorInt) == virtioVendorID, nil
}

// rebindPCIDevice attempts to rebind PCI devices using exponential backoff strategy
// if error is from pci device, return pcerror.ErrPCIRebindFailed
func (eh *eniInitFactory) rebindPCIDevice(scopedLog *logrus.Entry, resource *ccev2.ENI) (*eniLink, error) {

	rebindSuccess, err := eh.checkAndRebindPCIDevice(scopedLog)

	if err != nil {
		scopedLog.WithError(err).Error("Failed to check and rebind PCI devices")

	} else {
		if rebindSuccess {
			scopedLog.Info("Successfully rebound PCI devices, attempting to recreate ENI link")
		}
		// Rebinding successful, attempt to recreate eniLink
		eniLink, err := newENILink(resource, eh.release)
		if err == nil {
			// Successfully created eniLink, continue with the original flow
			scopedLog.Info("PCI device rebinding successful, ENI link successfully recreated")

			return eniLink, nil
		} else {
			scopedLog.WithError(err).Error("PCI device rebinding successful, but ENI link creation failed")
		}
	}

	return nil, err
}

// OnUpdateENI It will be recalled every 30s before cce-network-v2/2.11.5 and
// cce-network-v2/2.12.7. But it is not necessary, so remove periodic recalled code.
func (eh *eniInitFactory) OnUpdateENI(oldObj, newObj *ccev2.ENI) error {
	var err error
	isNeedUpdateStatus := false
	resource := newObj.DeepCopy()
	eh.fullENIs[resource.Spec.ENI.ID] = resource
	scopedLog := initLog.WithError(err).WithField("eni", resource.Spec.ENI.ID)

	if !bceutils.IsAgentMgrENI(resource) {
		scopedLog.Debugf("secondary eni %s is not created by cce, skip update ENI", resource.Spec.ENI.ID)
		return nil
	}

	if resource.Status.VPCStatus != ccev2.VPCENIStatusInuse {
		scopedLog.Debugf("eni is not in use, skip status [%s]", resource.Status.VPCStatus)
		return nil
	}

	defer func() {
		if resource.Status.CCEStatus == ccev2.ENIStatusReadyOnNode {
			eh.localENIs[resource.Spec.ENI.ID] = resource
		} else {
			delete(eh.localENIs, resource.Spec.ENI.ID)
		}

		setENIStatusMetric(resource)
	}()

	if resource.Spec.MacAddress == "" {
		scopedLog.Errorf("mac address is empty, skip update ENI")
		(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGNoMacAddress)
		isNeedUpdateStatus = (resource.Status.CCEStatus != newObj.Status.CCEStatus)
		return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
	}

	// downward compatibility with BBC models
	// TODO 2021-03-08: remove this after BBC secondary models are deprecated
	if resource.Spec.Type == ccev2.ENIForBBC {
		resource.Spec.UseMode = ccev2.ENIUseModePrimaryWithSecondaryIP
	}

	eniLink, err := newENILink(resource, eh.release)
	if err != nil {
		scopedLog.WithError(err).Error("Failed to get eniLink, attempting to check and rebind PCI devices")
		// Attempt to check and rebind PCI devices
		if resource.Spec.UseMode == ccev2.ENIUseModeSecondaryIP &&
			(resource.Spec.Type == ccev2.ENIForBCC ||
				resource.Spec.Type == ccev2.ENIForEBC) {
			eniLink, err = eh.rebindPCIDevice(scopedLog, resource)
			if err != nil || eniLink == nil {
				if err != nil && strings.Contains(err.Error(), string(ccev2.ENIMSGPCIERROR)) {
					scopedLog.WithError(err).Error("PCI device rebinding failed")
					(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGPCIERROR)
				} else {
					(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGMissingMac)
					scopedLog.Infof("eni %s missing mac address", resource.Spec.ENI.ID)
				}
				// Only update status if the status has changed
				// If status is not ready, it will be retry in resync loop
				isNeedUpdateStatus = (resource.Status.CCEStatus != newObj.Status.CCEStatus)
				return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
			}
		} else {
			(&resource.Status).AppendCCEENIStatus(ccev2.ENIStatusErrorOnNode)
			return eh.updateENIStatus(resource, true, scopedLog)
		}

	}
	switch resource.Spec.UseMode {
	case ccev2.ENIUseModePrimaryIP:
		if err = eniLink.rename(true); err != nil {
			scopedLog.WithError(err).Error("rename eniLink falied")
			(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGReNameFailed)
			isNeedUpdateStatus = (resource.Status.CCEStatus != newObj.Status.CCEStatus)
			return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
		}
	case ccev2.ENIUseModePrimaryWithSecondaryIP:
		// primary interface with secondary IP mode
		// do not need to rename eniLink
	default:
		// secondary interface with secondary IP mode for RDMA
		// do not need to rename eniLink
		needRename := shouldRenameENI(resource)
		if needRename {
			// eni with secondary IP mode need to rename eniLink
			if err = eniLink.rename(false); err != nil {
				scopedLog.WithError(err).Error("rename eniLink falied")
				(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGReNameFailed)
				isNeedUpdateStatus = (resource.Status.CCEStatus != newObj.Status.CCEStatus)
				return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
			}
			// set rule when eni secondary IP mode
			err = eniLink.ensureLinkConfig()
			if err != nil {
				scopedLog.WithError(err).Error("set eniLink falied")
				(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGReNameFailed)
				isNeedUpdateStatus = (resource.Status.CCEStatus != newObj.Status.CCEStatus)
				return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
			}
			if resource.Spec.InstallSourceBasedRouting {
				var err error
				if resource.Spec.Type == ccev2.ENIForERI {
					err = ensureERIRule(scopedLog, resource)
				} else {
					err = ensureENIRule(scopedLog, resource)
				}
				if err != nil {
					scopedLog.WithError(err).Error("install source based routing failed")
					(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGInstallRuleFailed)
					isNeedUpdateStatus = (resource.Status.CCEStatus != newObj.Status.CCEStatus)
					return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
				}
			}
		}
	}

	// set eni neigbor config
	err = eniLink.ensureENINeigh()
	if err != nil {
		(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGInstallNeighborFailed)
		isNeedUpdateStatus = (resource.Status.CCEStatus != newObj.Status.CCEStatus)
		return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
	}

	// set device and route on the worker machine only when eni bound at bcc
	resource.Status.InterfaceIndex = eniLink.linkIndex
	resource.Status.InterfaceName = eniLink.linkName
	resource.Status.ENIIndex = eniLink.eniIndex
	if eniLink.ipv4Gateway != "" {
		resource.Status.GatewayIPv4 = eniLink.ipv4Gateway
	}
	if eniLink.ipv6Gateway != "" {
		resource.Status.GatewayIPv6 = eniLink.ipv6Gateway
	}

	// If the status has not changed, there is no need to update status
	if resource.Status.VPCStatus == ccev2.VPCENIStatusInuse {
		if resource.Status.InterfaceIndex > 0 {
			(&resource.Status).AppendCCEENIStatus(ccev2.ENIStatusReadyOnNode)
		} else {
			// Only set to ReadyOnNode when InterfaceIndex is not 0
			(&resource.Status).AppendCCEENIStatus(ccev2.ENIMSGMissingInumber)
		}

	}

	isNeedUpdateStatus = !reflect.DeepEqual(resource.Status, newObj.Status)
	return eh.updateENIStatus(resource, isNeedUpdateStatus, scopedLog)
}

func (eh *eniInitFactory) updateENIStatus(resource *ccev2.ENI, isNeedUpdateStatus bool, scopedLog *logrus.Entry) error {
	if !isNeedUpdateStatus {
		return nil
	}

	scopedLog.WithField("eniId", resource.Spec.ENI.ID).Debug("update eni status")

	_, err := eh.eniClient.ENIs().UpdateStatus(context.TODO(), resource, metav1.UpdateOptions{})
	if err != nil {
		scopedLog.WithError(err).Error("update eni status")
		return err
	}

	qos.GlobalManager.ENIUpdateEventHandler(resource)

	return nil
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
		// 增量覆盖：确保VPCStatus为inuse时的处理逻辑被正确测试
		initLog.Debugf("Processing ENI %s with VPCStatus=inuse, CCEStatus=%s",
			resource.Spec.ENI.ID, resource.Status.CCEStatus)

		// 即使InterfaceIndex为0也允许状态流转
		// 因为状态流转是由状态机控制的，我们只在eniInitFactory中控制InterfaceIndex与ReadyOnNode的绑定
		// 在这里应该尊重传入的状态
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

// shouldRenameENI determines whether an ENI should be renamed based on its type and management status
func shouldRenameENI(resource *ccev2.ENI) bool {
	if resource.Spec.Type == ccev2.ENIForHPC {
		return false
	}
	if resource.Spec.Type == ccev2.ENIForERI && !bceutils.IsERIManagedENI(resource) {
		return false
	}
	return true
}

// setENIStatusMetric sets the ENI status metric based on the resource status
// Status: 0=normal (ReadyOnNode), 1=abnormal (other states or empty state)
func setENIStatusMetric(resource *ccev2.ENI) {
	clusterID := option.Config.ClusterID
	if clusterID == "" {
		clusterID = defaultUnknownValue
	}

	eniID := resource.Spec.ENI.ID
	if eniID == "" {
		eniID = defaultUnknownValue
	}

	nodeName := os.Getenv(envK8SNodeName)
	if nodeName == "" {
		nodeName = defaultUnknownValue
	}

	podName := os.Getenv(envPodName)
	if podName == "" {
		podName = defaultUnknownValue
	}

	// Determine status: 0=normal, 1=abnormal
	// Only ReadyOnNode is considered normal, all other states (including empty) are abnormal
	status := float64(0)
	reason := ""
	if resource.Status.CCEStatus != ccev2.ENIStatusReadyOnNode {
		status = 1
		reason = string(resource.Status.CCEStatus)
	}

	metrics.ENIStatus.WithLabelValues(clusterID, eniID, nodeName, podName, reason).Set(status)
}
