package vpceni

import (
	"context"
	"fmt"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

var (
	// template to patch node.capacity
	patchCapacityBodyTemplate = `{"op":"%s","path":"/status/capacity/cce.baidubce.com~1%s","value":"%d"}`
	patchAddOp                = "add"
	patchModiffyOp            = "replace"
)

type simpleIPQuotaManager struct {
	kubeClient kubernetes.Interface
	node       *corev1.Node
	instanceID string
}

// patchENICapacityInfoToNode patches eni capacity info to node if not exists.
// so user can reset these values.
func (manager *simpleIPQuotaManager) patchENICapacityInfoToNode(ctx context.Context, maxENINum, maxIPNum int) error {
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
		log.WithContext(ctx).Infof("patch ip resource of node [%s]  (maxENI: %d, maxIP: %d) to node capacity success", node.Name, maxENINum, maxIPNum)
	}
	return nil
}

// ENIQuotaManager SyncCapacity syncs node capacity
type ENIQuotaManager interface {
	GetMaxENI() int
	SetMaxENI(max int)
	GetMaxIP() int
	SetMaxIP(max int)

	// SyncCapacity syncs node capacity
	SyncCapacityToK8s(ctx context.Context) error
}

type customerIPQuota struct {
	*simpleIPQuotaManager

	log *logrus.Entry

	// bceclient is used to get customer quota from cloud
	bceclient cloud.Interface

	maxENINum   int
	maxIPPerENI int
}

var _ ENIQuotaManager = &customerIPQuota{}

func newCustomerIPQuota(
	log *logrus.Entry,
	kubeClient kubernetes.Interface, node *corev1.Node, instanceID string,
	bceclient cloud.Interface,
) ENIQuotaManager {
	return &customerIPQuota{
		simpleIPQuotaManager: &simpleIPQuotaManager{
			kubeClient: kubeClient,
			node:       node,
			instanceID: instanceID,
		},
		log:       log,
		bceclient: bceclient,
	}
}

// GetMaxENI implements IPResourceManager.
func (ciq *customerIPQuota) GetMaxENI() int {
	return ciq.maxENINum
}

// GetMaxIP implements IPResourceManager.
func (ciq *customerIPQuota) GetMaxIP() int {
	return ciq.maxIPPerENI
}

// SetMaxENI implements IPResourceManager.
func (ciq *customerIPQuota) SetMaxENI(max int) {
	ciq.maxENINum = max
}

// SetMaxIP implements IPResourceManager.
func (ciq *customerIPQuota) SetMaxIP(max int) {
	if operatorOption.Config.BCECustomerMaxIP != 0 {
		max = operatorOption.Config.BCECustomerMaxIP
	}
	ciq.maxIPPerENI = max
}

// SyncCapacityToK8s implements IPResourceManager.
func (ciq *customerIPQuota) SyncCapacityToK8s(ctx context.Context) error {
	maxIP := ciq.maxENINum * (ciq.maxIPPerENI -1)
	if maxIP <= 0 {
		maxIP = ciq.maxIPPerENI -1
	}
	return ciq.patchENICapacityInfoToNode(ctx, ciq.maxENINum, maxIP)
}

// calculateMaxIPPerENI returns the max num of IPs that can be attached to single ENI
// Ref: https://cloud.baidu.com/doc/VPC/s/0jwvytzll
func calculateMaxIPPerENI(memoryCapacityInGB int) int {
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

// calculateMaxENIPerNode returns the max num of ENIs that can be attached to a node
func calculateMaxENIPerNode(CPUCount int) int {
	maxENINum := 0

	switch {
	case CPUCount > 0 && CPUCount < 8:
		maxENINum = CPUCount
	case CPUCount >= 8:
		maxENINum = 8
	}

	return maxENINum
}
