package bcesync

import (
	"fmt"
	"sync"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	globalBSM *bsm
	once      sync.Once
)

// bsm is a global map of borrowed subnets
type bsm struct {
	mutex   *lock.RWMutex
	subnets map[string]*BorrowedSubnet
}

func InitBSM() error {
	once.Do(func() {
		globalBSM = &bsm{mutex: &lock.RWMutex{}, subnets: make(map[string]*BorrowedSubnet)}
	})
	return restoreBSM()
}

// restore bsm when restarting
func restoreBSM() error {
	// restore all subnets
	sbns, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("bsm failed to list subnets: %v", err)
	}
	for _, sbn := range sbns {
		globalBSM.subnets[sbn.Spec.ID] = NewBorrowedSubnet(sbn)
	}

	// restore all tasks
	enis, err := k8s.CCEClient().Informers.Cce().V2().ENIs().Lister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("bsm failed to list enis: %v", err)
	}
	for _, eni := range enis {
		bsbn, err := globalBSM.GetSubnet(eni.Spec.SubnetID)
		if err != nil {
			log.WithField("task", "restoreBSM").Errorf("bsm failed to get subnet %q: %v", eni.Spec.SubnetID, err)
			continue
		}
		// should borrow cross subnet IP
		if eni.Spec.BorrowIPCount > 0 {
			if eni.Spec.BorrowIPCount-eni.Status.LendBorrowedIPCount > 0 {
				borrowed := bsbn.Borrow(eni.Spec.ID, eni.Spec.BorrowIPCount-eni.Status.LendBorrowedIPCount)
				if borrowed == 0 {
					metrics.IPAMErrorCounter.WithLabelValues(ccev2.ErrorCodeNoAvailableSubnetCreateENI, "ENI", eni.Spec.ID).Inc()
				}
			}
		}

	}

	log.WithField("task", "restoreBSM").Info("bsm restored successfully")
	return nil
}

func GlobalBSM() *bsm {
	return globalBSM
}

func (bsm *bsm) GetSubnet(sbnID string) (*BorrowedSubnet, error) {
	bsm.mutex.RLock()
	defer bsm.mutex.RUnlock()
	if subnet, ok := bsm.subnets[sbnID]; ok {
		return subnet, nil
	}
	sbn, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().Get(sbnID)
	if err == nil {
		bsbn := NewBorrowedSubnet(sbn)
		bsm.subnets[sbnID] = bsbn
		return bsbn, nil
	}
	return nil, err
}

func (bsm *bsm) EnsureSubnet(vpcID, sbnID string) (*BorrowedSubnet, error) {
	_, err := EnsureSubnet(vpcID, sbnID)
	if err != nil {
		return nil, err
	}

	return bsm.GetSubnet(sbnID)
}

// updateSubnet update subnet in bsm
func (bsm *bsm) updateSubnet(sbn *ccev1.Subnet) {
	bs, err := bsm.GetSubnet(sbn.Name)
	if err == nil {
		bs.update(sbn)
	}
}

type BorrowedSubnet struct {
	*ccev1.Subnet
	// mutex protects members below this field
	mutex                     *lock.RWMutex
	tasks                     map[string]IPBorrowTask
	SubnetId                  string `json:"subnet_id"`
	BorrowedIPsCount          int    `json:"borrowed_ips_count"`
	BorrowedAvailableIPsCount int    `json:"borrowed_available_ips_count"`
}

func NewBorrowedSubnet(subnet *ccev1.Subnet) *BorrowedSubnet {
	if subnet == nil {
		return nil
	}
	return &BorrowedSubnet{
		Subnet:                    subnet,
		mutex:                     &lock.RWMutex{},
		tasks:                     make(map[string]IPBorrowTask),
		SubnetId:                  subnet.Spec.ID,
		BorrowedAvailableIPsCount: subnet.Status.AvailableIPNum,
	}
}

func (bs *BorrowedSubnet) update(subnet *ccev1.Subnet) {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()
	bs.Subnet = subnet
	bs.BorrowedAvailableIPsCount = subnet.Status.AvailableIPNum - bs.BorrowedIPsCount
}

func (bs *BorrowedSubnet) Borrow(enid string, ipNum int) (borrowedIPNum int) {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()
	if bs.BorrowedAvailableIPsCount < ipNum {
		return
	}

	if task, ok := bs.tasks[enid]; !ok {
		bs.tasks[enid] = IPBorrowTask{SubnetId: bs.SubnetId, EniID: enid, IPNum: ipNum}
	} else {
		task.IPNum += ipNum
		bs.tasks[enid] = task
	}

	bs.BorrowedIPsCount += ipNum
	bs.BorrowedAvailableIPsCount = bs.Status.AvailableIPNum - bs.BorrowedIPsCount

	return ipNum
}

func (bs *BorrowedSubnet) forceBorrowForENI(enid string, ipNum int) {
	if ipNum <= 0 {
		return
	}
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	if task, ok := bs.tasks[enid]; ok {
		bs.BorrowedIPsCount -= task.IPNum
		task.IPNum = ipNum
		bs.tasks[enid] = task
	} else {
		bs.tasks[enid] = IPBorrowTask{SubnetId: bs.SubnetId, EniID: enid, IPNum: ipNum}
	}

	bs.BorrowedIPsCount += ipNum
	bs.BorrowedAvailableIPsCount = bs.Status.AvailableIPNum - bs.BorrowedIPsCount
}

func (bs *BorrowedSubnet) Done(enid string, ipNum int) {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()
	if task, ok := bs.tasks[enid]; ok {
		if task.IPNum < ipNum {
			ipNum = task.IPNum
			task.IPNum = 0
		} else {
			task.IPNum -= ipNum
		}
		if bs.BorrowedIPsCount < ipNum {
			bs.BorrowedIPsCount = 0
		} else {
			bs.BorrowedIPsCount -= ipNum
		}

		// do not need add BorrowedAvailableIPsCount
	}
}

func (bs *BorrowedSubnet) Cancel(eniID string) {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()
	if task, ok := bs.tasks[eniID]; ok {
		delete(bs.tasks, eniID)
		if bs.BorrowedIPsCount < task.IPNum {
			bs.BorrowedAvailableIPsCount += bs.BorrowedIPsCount
			bs.BorrowedIPsCount = 0
		} else {
			bs.BorrowedAvailableIPsCount += task.IPNum
			bs.BorrowedIPsCount -= task.IPNum
		}
	}
}

func (bs *BorrowedSubnet) CanBorrow(ipNum int) bool {
	return !bs.Status.HasNoMoreIP &&
		bs.Status.Enable &&
		bs.Status.AvailableIPNum >= ipNum &&
		bs.BorrowedAvailableIPsCount >= ipNum
}

func (bs *BorrowedSubnet) GetBorrowedIPNum(eniID string) int {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()

	if task, ok := bs.tasks[eniID]; ok {
		return task.IPNum
	} else {
		return 0
	}
}

type IPBorrowTask struct {
	SubnetId  string `json:"subnet_id"`
	EniID     string `json:"eni_id"`
	IPNum     int    `json:"ip_num"`
	TakeIPNum int    `json:"take_ip_num"`
}
