package limit

import (
	"context"
	"fmt"
	"strconv"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	k8sutilnet "k8s.io/utils/net"
)

var (
	// template to patch node.capacity
	patchCapacityBodyTemplate = `{"op":"%s","path":"/status/capacity/cce.baidubce.com~1%s","value":"%d"}`
	patchAddOp                = "add"
	patchModiffyOp            = "replace"

	log = logging.NewSubysLogger("ip-resource-manager")
)

type simpleIPResourceManager struct {
	kubeClient        kubernetes.Interface
	preAttachedENINum int
	node              *corev1.Node
}

// NodeCapacity is the limit of the node
type NodeCapacity struct {
	// MaxENINum is the maximum number of ENI devices that can be attached to the node
	MaxENINum int
	// MaxIPPerENI is the maximum number of IP addresses that can be attached to the ENI device
	MaxIPPerENI int

	// CustomerENIResource is the maximum number of ENI devices that can be attached to the node
	// this value will patch to k8s node
	CustomerENIResource int
	CustomerIPResource  int
}

// patchENICapacityInfoToNode patches eni capacity info to node if not exists.
// so user can reset these values.
func (manager *simpleIPResourceManager) patchENICapacityInfoToNode(ctx context.Context, maxENINum, maxIPNum int) error {
	node := manager.node
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	// update node capacity
	needUpdateIPResourceFlag := true
	ipPathBody := fmt.Sprintf(patchCapacityBodyTemplate, patchAddOp, "ip", maxIPNum)
	if ipRe, ok := node.Status.Capacity[k8s.ResourceIPForNode]; ok {
		if ipRe.Value() == int64(maxIPNum) {
			needUpdateIPResourceFlag = false
		}
		ipPathBody = fmt.Sprintf(patchCapacityBodyTemplate, patchModiffyOp, "ip", maxIPNum)
	}

	needUpdateENIResourceFlag := true
	eniPathBody := fmt.Sprintf(patchCapacityBodyTemplate, patchAddOp, "eni", maxENINum)
	if eniRe, ok := node.Status.Capacity[k8s.ResourceENIForNode]; ok {
		if eniRe.Value() == int64(maxENINum) {
			needUpdateENIResourceFlag = false
		}
		eniPathBody = fmt.Sprintf(patchCapacityBodyTemplate, patchModiffyOp, "eni", maxENINum)
	}

	// patch annotations
	if needUpdateENIResourceFlag || needUpdateIPResourceFlag {
		patchData := []byte(fmt.Sprintf(`[%s, %s]`, ipPathBody, eniPathBody))
		_, err := manager.kubeClient.CoreV1().Nodes().Patch(ctx, manager.node.Name, types.JSONPatchType, patchData, metav1.PatchOptions{}, "status")
		if err != nil {
			return err
		}
		log.WithContext(ctx).Infof("patch ip resource (maxENI: %d, maxIP: %d) to node capacity success", maxENINum, maxIPNum)
	}

	return nil
}

// IPResourceManager SyncCapacity syncs node capacity
type IPResourceManager interface {
	// CalaculateCapacity calculate node capacity
	CalaculateCapacity() *NodeCapacity
	// SyncCapacity syncs node capacity
	SyncCapacity(ctx context.Context) error
}

type bbcIPResourceManager struct {
	*simpleIPResourceManager
	limiter *NodeCapacity
}

var _ IPResourceManager = &bbcIPResourceManager{}

// NewBBCIPResourceManager creates a new ip resource manager for BBC instance
func NewBBCIPResourceManager(kubeClient kubernetes.Interface, node *corev1.Node) IPResourceManager {
	return &bbcIPResourceManager{
		simpleIPResourceManager: &simpleIPResourceManager{
			kubeClient:        kubeClient,
			preAttachedENINum: 0,
			node:              node,
		},
	}
}

// CalaculateCapacity implements IPResourceManager
func (manager *bbcIPResourceManager) CalaculateCapacity() *NodeCapacity {
	var (
		maxENINum   = 1
		maxIPPerENI = 40
	)
	if operatorOption.Config.BCECustomerMaxENI != 0 {
		maxENINum = operatorOption.Config.BCECustomerMaxENI
	}

	if operatorOption.Config.BCECustomerMaxIP != 0 {
		maxIPPerENI = operatorOption.Config.BCECustomerMaxIP
	}
	manager.limiter = &NodeCapacity{
		MaxENINum:           maxENINum,
		MaxIPPerENI:         maxIPPerENI,
		CustomerENIResource: 0,
		CustomerIPResource:  maxIPPerENI,
	}
	return manager.limiter
}

func (manager *bbcIPResourceManager) SyncCapacity(ctx context.Context) error {

	return manager.patchENICapacityInfoToNode(ctx, manager.limiter.CustomerENIResource, manager.limiter.CustomerIPResource)
}

var _ IPResourceManager = &bbcIPResourceManager{}

type bccIPResourceManager struct {
	limiter *NodeCapacity
	*simpleIPResourceManager
	cpuCount           int
	memoryCapacityInGB int
}

// CalaculateCapacity implements IPResourceManager
func (manager *bccIPResourceManager) CalaculateCapacity() *NodeCapacity {
	var (
		maxENINum   = GetMaxENIPerNode(manager.cpuCount)
		maxIPPerENI = GetMaxIPPerENI(manager.memoryCapacityInGB)
	)
	if operatorOption.Config.BCECustomerMaxENI != 0 {
		maxENINum = operatorOption.Config.BCECustomerMaxENI
	}

	if operatorOption.Config.BCECustomerMaxIP != 0 {
		maxIPPerENI = operatorOption.Config.BCECustomerMaxIP
	}

	manager.limiter = &NodeCapacity{
		MaxENINum:           maxENINum,
		MaxIPPerENI:         maxIPPerENI,
		CustomerENIResource: maxENINum,
		CustomerIPResource:  (maxIPPerENI - 1) * maxENINum,
	}
	return manager.limiter
}

// NewBCCIPResourceManager creates a new ip resource manager for BCC instance
func NewBCCIPResourceManager(kubeClient kubernetes.Interface, preAttachedENINum int, node *corev1.Node,
	cpuCount, memoryCapacityInGB int) IPResourceManager {
	return &bccIPResourceManager{
		simpleIPResourceManager: &simpleIPResourceManager{
			kubeClient:        kubeClient,
			preAttachedENINum: preAttachedENINum,
			node:              node,
		},
		cpuCount:           cpuCount,
		memoryCapacityInGB: memoryCapacityInGB,
	}
}

func (manager *bccIPResourceManager) SyncCapacity(ctx context.Context) error {
	return manager.patchENICapacityInfoToNode(ctx, manager.limiter.CustomerENIResource, manager.limiter.CustomerIPResource)
}

var _ IPResourceManager = &bccIPResourceManager{}

type noopResourceManager struct{}

func NewNoopIPResourceManager() IPResourceManager {
	return &noopResourceManager{}
}

// CalaculateCapacity implements IPResourceManager
func (*noopResourceManager) CalaculateCapacity() *NodeCapacity {
	return &NodeCapacity{}
}

// SyncCapacity implements IPResourceManager
func (*noopResourceManager) SyncCapacity(ctx context.Context) error {
	return nil
}

var _ IPResourceManager = &noopResourceManager{}

// In this mode, each node has its own CIDR of pod IP
type rangeIPResourceManager struct {
	*simpleIPResourceManager
}

// CalaculateCapacity implements IPResourceManager
func (*rangeIPResourceManager) CalaculateCapacity() *NodeCapacity {
	panic("unimplemented")
}

// NewRangeIPResourceManager creates a new ip resource manager for range mode
func NewRangeIPResourceManager(kubeClient kubernetes.Interface, preAttachedENINum int, node *corev1.Node) *rangeIPResourceManager {
	return &rangeIPResourceManager{
		simpleIPResourceManager: &simpleIPResourceManager{
			kubeClient:        kubeClient,
			preAttachedENINum: preAttachedENINum,
			node:              node,
		},
	}
}

// getIPRangeSize Calculate the maximum number of pod IPS according to the CIDR of the node
func (manager *rangeIPResourceManager) getIPRangeSize() int {
	node := manager.node
	// according to node specification, if spec.PodCIDRs is not empty, the first element must equal to spec.PodCIDR
	podCIDRs := make([]string, 0)
	if len(node.Spec.PodCIDRs) == 0 {
		podCIDRs = append(podCIDRs, node.Spec.PodCIDR)
	} else {
		for _, podCIDR := range node.Spec.PodCIDRs {
			podCIDRs = append(podCIDRs, podCIDR)
		}
	}
	cidrs, err := k8sutilnet.ParseCIDRs(podCIDRs)
	if err != nil {
		log.Errorf("parse cidr for ip range error: %v", err)
	}

	var ipv4RangeSize, ipv6RangeSize int64

	for _, podCIDR := range cidrs {
		size := k8sutilnet.RangeSize(podCIDR)
		if k8sutilnet.IsIPv4CIDR(podCIDR) {
			ipv4RangeSize += size
		} else {
			ipv6RangeSize += size
		}
	}
	return math.IntMax(int(ipv4RangeSize), int(ipv6RangeSize))
}

func (manager *rangeIPResourceManager) SyncCapacity(ctx context.Context) error {
	var maxENINum, maxIPPerENI int
	maxENINum = 1
	if operatorOption.Config.BCECustomerMaxENI != 0 {
		maxENINum = operatorOption.Config.BCECustomerMaxENI
	}
	maxIPPerENI = manager.getIPRangeSize()
	if operatorOption.Config.BCECustomerMaxIP != 0 {
		maxIPPerENI = operatorOption.Config.BCECustomerMaxIP
	}

	return manager.patchENICapacityInfoToNode(ctx, maxENINum, maxIPPerENI)
}

var _ IPResourceManager = &rangeIPResourceManager{}

type crossVPCEniResourceManager struct {
	*simpleIPResourceManager
	bccInstance *bccapi.InstanceModel
}

// CalaculateCapacity implements IPResourceManager
func (*crossVPCEniResourceManager) CalaculateCapacity() *NodeCapacity {
	panic("unimplemented")
}

func NewCrossVPCEniResourceManager(kubeClient kubernetes.Interface, node *corev1.Node, bccInstance *bccapi.InstanceModel) *crossVPCEniResourceManager {
	return &crossVPCEniResourceManager{
		simpleIPResourceManager: &simpleIPResourceManager{
			kubeClient: kubeClient,
			node:       node,
		},
		bccInstance: bccInstance,
	}
}

func (manager *crossVPCEniResourceManager) SyncCapacity(ctx context.Context) error {
	var (
		maxEniNum        int
		maxEniNumByAnno  int
		maxEniNumByLabel int
		node             *corev1.Node
	)

	maxEniNum = GetMaxENIPerNode(manager.bccInstance.CpuCount)
	if operatorOption.Config.BCECustomerMaxENI != 0 {
		maxEniNum = operatorOption.Config.BCECustomerMaxENI
	}

	node = manager.node

	maxEniNumStr, ok := node.Annotations[k8s.NodeAnnotationMaxCrossVPCEni]
	if ok {
		i, err := strconv.ParseInt(maxEniNumStr, 10, 32)
		if err != nil {
			return err
		}
		maxEniNumByAnno = int(i)

		if maxEniNumByAnno < maxEniNum {
			maxEniNum = maxEniNumByAnno
		}
	}

	maxEniNumStr, ok = node.Labels[k8s.NodeLabelMaxCrossVPCEni]
	if ok {
		i, err := strconv.ParseInt(maxEniNumStr, 10, 32)
		if err != nil {
			return err
		}
		maxEniNumByLabel = int(i)

		if maxEniNumByLabel < maxEniNum {
			maxEniNum = maxEniNumByLabel
		}
	}

	return manager.patchCrossVPCEniCapacityInfoToNode(ctx, maxEniNum)
}

func (manager *crossVPCEniResourceManager) patchCrossVPCEniCapacityInfoToNode(ctx context.Context, maxEniNum int) error {
	var (
		patchBodyTemplate         = `[{"op":"%s","path":"/status/capacity/cross-vpc-eni.cce.io~1eni","value":"%d"}]`
		patchBody                 = fmt.Sprintf(patchBodyTemplate, patchAddOp, maxEniNum)
		node                      = manager.node
		needUpdateEniResourceFlag = true
	)

	resource, ok := node.Status.Capacity[k8s.ResourceCrossVPCEni]
	if ok {
		if resource.Value() == int64(maxEniNum) {
			needUpdateEniResourceFlag = false
		}
		patchBody = fmt.Sprintf(patchBodyTemplate, patchModiffyOp, maxEniNum)
	}

	if needUpdateEniResourceFlag {
		_, err := manager.kubeClient.CoreV1().Nodes().Patch(ctx, manager.node.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{}, "status")
		if err != nil {
			log.WithContext(ctx).Errorf("failed to patch %v capacity: %v", k8s.ResourceCrossVPCEni, err)
			return err
		}
		log.WithContext(ctx).Infof("patch %v capacity %v to node capacity success", k8s.ResourceCrossVPCEni, maxEniNum)
	}
	return nil
}

var _ IPResourceManager = &crossVPCEniResourceManager{}

// GetMaxIPPerENI returns the max num of IPs that can be attached to single ENI
// Ref: https://cloud.baidu.com/doc/VPC/s/0jwvytzll
func GetMaxIPPerENI(memoryCapacityInGB int) int {
	maxIPNum := 0

	switch {
	case memoryCapacityInGB > 0 && memoryCapacityInGB < 2:
		maxIPNum = 2
	case memoryCapacityInGB >= 2 && memoryCapacityInGB <= 8:
		maxIPNum = 8
	case memoryCapacityInGB > 8 && memoryCapacityInGB <= 32:
		maxIPNum = 16
	case memoryCapacityInGB > 32 && memoryCapacityInGB <= 64:
		maxIPNum = 30
	case memoryCapacityInGB > 64:
		maxIPNum = 40
	}
	return maxIPNum
}

// GetMaxENIPerNode returns the max num of ENIs that can be attached to a node
func GetMaxENIPerNode(CPUCount int) int {
	maxENINum := 0

	switch {
	case CPUCount > 0 && CPUCount < 8:
		maxENINum = CPUCount
	case CPUCount >= 8:
		maxENINum = 8
	}

	return maxENINum
}
