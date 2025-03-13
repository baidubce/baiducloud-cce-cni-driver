package rdma

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

// patchRdmaEniCapacityInfoToNode patches eni capacity info to node if not exists.
// so user can reset these values.
func (manager *simpleIPQuotaManager) patchRdmaEniCapacityInfoToNode(ctx context.Context, maxRdmaEniNum, maxRdmaIpNum int) error {
	node := manager.node
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	// update node capacity
	needUpdateIPResourceFlag := true
	ipPathBody := fmt.Sprintf(patchCapacityBodyTemplate, patchAddOp, "rdmaip", maxRdmaIpNum)
	if ipRe, ok := node.Status.Capacity[k8s.ResourceRdmaIpForNode]; ok {
		if ipRe.Value() == int64(maxRdmaIpNum) {
			needUpdateIPResourceFlag = false
		}
		ipPathBody = fmt.Sprintf(patchCapacityBodyTemplate, patchModiffyOp, "rdmaip", maxRdmaIpNum)
	}

	needUpdateENIResourceFlag := true
	eniPathBody := fmt.Sprintf(patchCapacityBodyTemplate, patchAddOp, "rdmaeni", maxRdmaEniNum)
	if eniRe, ok := node.Status.Capacity[k8s.ResourceRdmaEniForNode]; ok {
		if eniRe.Value() == int64(maxRdmaEniNum) {
			needUpdateENIResourceFlag = false
		}
		eniPathBody = fmt.Sprintf(patchCapacityBodyTemplate, patchModiffyOp, "rdmaeni", maxRdmaEniNum)
	}

	// patch annotations
	if needUpdateENIResourceFlag || needUpdateIPResourceFlag {
		patchData := []byte(fmt.Sprintf(`[%s, %s]`, ipPathBody, eniPathBody))
		_, err := manager.kubeClient.CoreV1().Nodes().Patch(ctx, manager.node.Name, types.JSONPatchType, patchData, metav1.PatchOptions{}, "status")
		if err != nil {
			return err
		}
		log.WithContext(ctx).Infof("patch rdma ip resource of node [%s]  (maxRdmaEni: %d, maxRdmaIp: %d) to node capacity success", node.Name, maxRdmaEniNum, maxRdmaIpNum)
	}
	return nil
}

// RdmaEniQuotaManager SyncCapacity syncs node capacity
type RdmaEniQuotaManager interface {
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

var _ RdmaEniQuotaManager = &customerIPQuota{}

func newCustomerIPQuota(
	log *logrus.Entry,
	kubeClient kubernetes.Interface, node *corev1.Node, instanceID string,
	bceclient cloud.Interface,
) RdmaEniQuotaManager {
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
	if operatorOption.Config.BCECustomerMaxRdmaIP != 0 {
		max = operatorOption.Config.BCECustomerMaxRdmaIP
	}
	ciq.maxIPPerENI = max
}

// SyncCapacityToK8s implements IPResourceManager.
func (ciq *customerIPQuota) SyncCapacityToK8s(ctx context.Context) error {
	maxIP := ciq.maxENINum * (ciq.maxIPPerENI - 1)
	if maxIP <= 0 {
		maxIP = ciq.maxIPPerENI - 1
	}
	return ciq.patchRdmaEniCapacityInfoToNode(ctx, ciq.maxENINum, maxIP)
}
