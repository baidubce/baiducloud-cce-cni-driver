package ippool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	k8sutilnet "k8s.io/utils/net"
	"modernc.org/mathutil"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/crossvpceni"
	utileni "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
)

var (
	// Maximum number of binding Eni devices per node
	// If the user's IP limit is not the default limit of using VPC,
	// you need to turn on this item to expand the pod capacity of a single machine
	customerMaxENINum   int
	customerMaxIPPerENI int

	// template to patch node.capacity
	patchCapacityBodyTemplate = `{"op":"%s","path":"/status/capacity/cce.baidubce.com~1%s","value":"%d"}`
	patchAddOp                = "add"
	patchModiffyOp            = "replace"
)

func IPResourceFlags(flagset *pflag.FlagSet) {
	flagset.IntVar(&customerMaxENINum, "customer-max-eni-num", customerMaxENINum, "Maximum number of binding Eni devices per node. The default value of 0 means that the default resource limit of VPC is used")
	flagset.IntVar(&customerMaxIPPerENI, "customer-max-ip-per-eni", customerMaxIPPerENI, "Maximum number of ip  per Eni devices. The default value of 0 means that the default resource limit of VPC is used")
}

type simpleIPResourceManager struct {
	kubeClient        kubernetes.Interface
	preAttachedENINum int
	node              *corev1.Node
}

// patchENICapacityInfoToNode patches eni capacity info to node if not exists.
// so user can reset these values.
func (manager *simpleIPResourceManager) patchENICapacityInfoToNode(ctx context.Context, maxENINum, maxIPPerENI int) error {
	node := manager.node
	old := node.DeepCopy()

	// in accordance with 1c1g bcc
	preAttachedENINum := mathutil.Min(manager.preAttachedENINum, maxENINum)

	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	node.Annotations[utileni.NodeAnnotationMaxENINum] = strconv.Itoa(maxENINum)
	node.Annotations[utileni.NodeAnnotationMaxIPPerENI] = strconv.Itoa(maxIPPerENI)
	node.Annotations[utileni.NodeAnnotationWarmIPTarget] = strconv.Itoa(maxIPPerENI / 2)
	node.Annotations[utileni.NodeAnnotationPreAttachedENINum] = strconv.Itoa(preAttachedENINum)

	// patch annotations
	if !reflect.DeepEqual(node.Annotations, old.Annotations) {
		json, err := json.Marshal(node.Annotations)
		if err != nil {
			return err
		}

		patchData := []byte(fmt.Sprintf(`{"metadata":{"annotations":%s}}`, json))
		_, err = manager.kubeClient.CoreV1().Nodes().Patch(ctx, manager.node.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
		if err != nil {
			return err
		}
		logger.Infof(ctx, "patch ip resource (eni: %d, ipPerENI: %d) to node annotaion success", maxENINum, maxIPPerENI)
	}

	// update node capacity
	needUpdateIPResourceFlag := true
	maxIP := maxENINum * maxIPPerENI
	ipPathBody := fmt.Sprintf(patchCapacityBodyTemplate, patchAddOp, "ip", maxIP)
	if ipRe, ok := node.Status.Capacity[networking.ResourceIPForNode]; ok {
		if ipRe.Value() == int64(maxIP) {
			needUpdateIPResourceFlag = false
		}
		ipPathBody = fmt.Sprintf(patchCapacityBodyTemplate, patchModiffyOp, "ip", maxIP)
	}

	needUpdateENIResourceFlag := true
	eniPathBody := fmt.Sprintf(patchCapacityBodyTemplate, patchAddOp, "eni", maxENINum)
	if eniRe, ok := node.Status.Capacity[networking.ResourceENIForNode]; ok {
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
		logger.Infof(ctx, "patch ip resource (maxENI: %d, maxIP: %d) to node capacity success", maxENINum, maxIP)
	}

	return nil
}

type IPResourceManager interface {
	SyncCapacity(ctx context.Context) error
}

type bbcIPResourceManager struct {
	*simpleIPResourceManager
}

func NewBBCIPResourceManager(kubeClient kubernetes.Interface, preAttachedENINum int, node *corev1.Node) *bbcIPResourceManager {
	return &bbcIPResourceManager{
		simpleIPResourceManager: &simpleIPResourceManager{
			kubeClient:        kubeClient,
			preAttachedENINum: preAttachedENINum,
			node:              node,
		},
	}
}

func (manager *bbcIPResourceManager) SyncCapacity(ctx context.Context) error {
	var (
		maxENINum   = 1
		maxIPPerENI = 40
	)
	if maxENINum < customerMaxENINum {
		maxENINum = customerMaxENINum
	}

	if maxIPPerENI < customerMaxIPPerENI {
		maxIPPerENI = customerMaxIPPerENI
	}

	return manager.patchENICapacityInfoToNode(ctx, maxENINum, maxIPPerENI)
}

var _ IPResourceManager = &bbcIPResourceManager{}

type bccIPResourceManager struct {
	*simpleIPResourceManager
	bccInstance *bccapi.InstanceModel
}

func NewBCCIPResourceManager(kubeClient kubernetes.Interface, preAttachedENINum int, node *corev1.Node, bccInstance *bccapi.InstanceModel) *bccIPResourceManager {
	return &bccIPResourceManager{
		simpleIPResourceManager: &simpleIPResourceManager{
			kubeClient:        kubeClient,
			preAttachedENINum: preAttachedENINum,
			node:              node,
		},
		bccInstance: bccInstance,
	}
}

func (manager *bccIPResourceManager) SyncCapacity(ctx context.Context) error {
	var maxENINum, maxIPPerENI int
	maxENINum = utileni.GetMaxENIPerNode(manager.bccInstance.CpuCount)
	if maxENINum < customerMaxENINum {
		maxENINum = customerMaxENINum
	}
	maxIPPerENI = utileni.GetMaxIPPerENI(manager.bccInstance.MemoryCapacityInGB)
	if maxIPPerENI < customerMaxIPPerENI {
		maxIPPerENI = customerMaxIPPerENI
	}

	return manager.patchENICapacityInfoToNode(ctx, maxENINum, maxIPPerENI)
}

var _ IPResourceManager = &bccIPResourceManager{}

// In this mode, each node has its own CIDR of pod IP
type rangeIPResourceManager struct {
	*simpleIPResourceManager
}

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
		logger.Errorf(context.TODO(), "parse cidr for ip range error: %v", err)
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
	return mathutil.Max(int(ipv4RangeSize), int(ipv6RangeSize))
}

func (manager *rangeIPResourceManager) SyncCapacity(ctx context.Context) error {
	var maxENINum, maxIPPerENI int
	maxENINum = 1
	if maxENINum < customerMaxENINum {
		maxENINum = customerMaxENINum
	}
	maxIPPerENI = manager.getIPRangeSize()
	if maxIPPerENI < customerMaxIPPerENI {
		maxIPPerENI = customerMaxIPPerENI
	}

	return manager.patchENICapacityInfoToNode(ctx, maxENINum, maxIPPerENI)
}

var _ IPResourceManager = &rangeIPResourceManager{}

type crossVPCEniResourceManager struct {
	*simpleIPResourceManager
	bccInstance *bccapi.InstanceModel
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
		node             *v1.Node
	)

	maxEniNum = utileni.GetMaxENIPerNode(manager.bccInstance.CpuCount)
	if maxEniNum < customerMaxENINum {
		maxEniNum = customerMaxENINum
	}

	node = manager.node

	maxEniNumStr, ok := node.Annotations[crossvpceni.NodeAnnotationMaxCrossVPCEni]
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

	maxEniNumStr, ok = node.Labels[crossvpceni.NodeLabelMaxCrossVPCEni]
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

	resource, ok := node.Status.Capacity[networking.ResourceCrossVPCEni]
	if ok {
		if resource.Value() == int64(maxEniNum) {
			needUpdateEniResourceFlag = false
		}
		patchBody = fmt.Sprintf(patchBodyTemplate, patchModiffyOp, maxEniNum)
	}

	if needUpdateEniResourceFlag {
		_, err := manager.kubeClient.CoreV1().Nodes().Patch(ctx, manager.node.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{}, "status")
		if err != nil {
			logger.Errorf(ctx, "failed to patch %v capacity: %v", networking.ResourceCrossVPCEni, err)
			return err
		}
		logger.Infof(ctx, "patch %v capacity %v to node capacity success", networking.ResourceCrossVPCEni, maxEniNum)
	}
	return nil
}

var _ IPResourceManager = &crossVPCEniResourceManager{}
