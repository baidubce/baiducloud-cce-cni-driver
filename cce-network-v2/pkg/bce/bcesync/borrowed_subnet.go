package bcesync

import (
	"fmt"
	"sync"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/math"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	globalBSM *bsm
	once      sync.Once
)

const (
	IPKindAvailable     = "available"
	IPKindBorrowed      = "borrowed"
	IPKindBorrowedAvail = "borrowed_avail"
	IPKindCount         = "count"
	IPKindUsed          = "used"

	EniTypeSubnet = "subnet"
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
	return resyncBSM()
}

// restore bsm when restarting
func resyncBSM() error {
	// restore all subnets
	sbns, err := k8s.CCEClient().Informers.Cce().V1().Subnets().Lister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("bsm failed to list subnets: %v", err)
	}

	var subnets = make(map[string]*BorrowedSubnet)
	for _, sbn := range sbns {
		subnets[sbn.Spec.ID] = NewBorrowedSubnet(sbn)
	}

	// restore all tasks
	enis, err := k8s.CCEClient().Informers.Cce().V2().ENIs().Lister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("bsm failed to list enis: %v", err)
	}
	for _, eni := range enis {
		// ignore eni without subnetID like RDMA device
		if eni.Spec.SubnetID == "" {
			continue
		}
		bsbn, ok := subnets[eni.Spec.SubnetID]
		if !ok {
			sbn, err := GlobalBSM().GetSubnet(eni.Spec.SubnetID)
			if err != nil {
				log.WithField("task", "restoreBSM").Errorf("bsm failed to get subnet %q: %v", eni.Spec.SubnetID, err)
				continue
			}
			bsbn = sbn
			subnets[sbn.Spec.ID] = bsbn
		}

		// should borrow cross subnet IP
		if eni.Spec.BorrowIPCount > 0 {
			ipCount := math.IntMax(eni.Spec.BorrowIPCount-len(eni.Spec.PrivateIPSet), 0)
			if ipCount > 0 {
				count := bsbn.forceBorrowForENI(eni.Spec.ID, ipCount)
				if count != ipCount {
					metrics.IPAMErrorCounter.WithLabelValues(ccev2.ErrorCodeNoAvailableSubnetCreateENI, "ENI", eni.Spec.ID).Inc()
				}
			}
		}

		var usedIPCount int
		for _, ipset := range eni.Spec.PrivateIPSet {
			if ipset.SubnetID == eni.Spec.SubnetID {
				usedIPCount++
			}
		}
		metrics.SubnetIPsGuage.WithLabelValues(IPKindBorrowed, eni.Spec.ID, eni.Spec.SubnetID, eni.Spec.NodeName).Set(float64(bsbn.GetBorrowedIPNum(eni.Spec.ID)))
		metrics.SubnetIPsGuage.WithLabelValues(IPKindUsed, eni.Spec.ID, eni.Spec.SubnetID, eni.Spec.NodeName).Set(float64(usedIPCount))
	}

	for id, bs := range subnets {
		metrics.SubnetIPsGuage.WithLabelValues(IPKindAvailable, EniTypeSubnet, id, EniTypeSubnet).Set(float64(bs.Status.AvailableIPNum))
		metrics.SubnetIPsGuage.WithLabelValues(IPKindBorrowed, EniTypeSubnet, id, EniTypeSubnet).Set(float64(bs.BorrowedIPsCount))
		metrics.SubnetIPsGuage.WithLabelValues(IPKindBorrowedAvail, EniTypeSubnet, id, EniTypeSubnet).Set(float64(bs.BorrowedAvailableIPsCount))
	}
	GlobalBSM().mutex.Lock()
	GlobalBSM().subnets = subnets
	GlobalBSM().mutex.Unlock()

	log.WithField("task", "restoreBSM").Debug("resync bsm successfully")
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

func (bsm *bsm) ForceBorrowForENI(eni *ccev2.ENI) {
	if eni != nil && eni.Spec.BorrowIPCount > 0 {
		bsbn, e := bsm.EnsureSubnet(eni.Spec.ENI.VpcID, eni.Spec.ENI.SubnetID)
		if e == nil {
			ipCount := math.IntMax(eni.Spec.BorrowIPCount-len(eni.Spec.PrivateIPSet), 0)
			bsbn.forceBorrowForENI(eni.Name, ipCount)
		}
	}
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

func (bs *BorrowedSubnet) logger() *logrus.Entry {
	return log.WithFields(logrus.Fields{
		"module":                    "borrowedSubnet",
		"sbnID":                     bs.SubnetId,
		"BorrowedIPsCount":          bs.BorrowedIPsCount,
		"BorrowedAvailableIPsCount": bs.BorrowedAvailableIPsCount,
		"AvailableIPNum":            bs.Status.AvailableIPNum,
	})
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
		bs.logger().WithFields(logrus.Fields{
			"task":      "borrow",
			"eniID":     enid,
			"needIPNum": ipNum,
			"tasks":     logfields.Json(bs.tasks),
		}).Warning("subnet not enough available ips to borrow by eni")
		return
	}

	return bs._forceBorrowForENI(enid, ipNum)
}

// forceBorrowForENI borrow ip for eni
// return borrowed ip num
func (bs *BorrowedSubnet) forceBorrowForENI(enid string, ipNum int) int {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	return bs._forceBorrowForENI(enid, ipNum)
}

func (bs *BorrowedSubnet) _forceBorrowForENI(enid string, ipNum int) int {
	var (
		eniBorrowedIPNum int
		sbnAvailBorrowIP int
	)
	if task, ok := bs.tasks[enid]; ok {
		bs.BorrowedIPsCount -= task.IPNum
		sbnAvailBorrowIP = bs.Status.AvailableIPNum - bs.BorrowedIPsCount
		eniBorrowedIPNum = math.IntMin(sbnAvailBorrowIP, ipNum)
		task.IPNum = eniBorrowedIPNum
		bs.tasks[enid] = task
	} else {
		sbnAvailBorrowIP = bs.Status.AvailableIPNum - bs.BorrowedIPsCount
		eniBorrowedIPNum = math.IntMin(sbnAvailBorrowIP, ipNum)
		bs.tasks[enid] = IPBorrowTask{SubnetId: bs.SubnetId, EniID: enid, IPNum: eniBorrowedIPNum}
	}

	bs.BorrowedIPsCount += eniBorrowedIPNum
	bs.BorrowedAvailableIPsCount = bs.Status.AvailableIPNum - bs.BorrowedIPsCount

	if eniBorrowedIPNum < ipNum {
		bs.logger().WithFields(logrus.Fields{
			"task":                            "forceBorrowForENI",
			"eniID":                           "enid",
			"sbnID":                           bs.SubnetId,
			"needIPNum":                       ipNum,
			"eniBorrowedIPNum":                eniBorrowedIPNum,
			"subnetBorrowedAvailableIPsCount": bs.BorrowedAvailableIPsCount,
			"subnetAvailableIPsCount":         bs.Status.AvailableIPNum,
			"subnetBorrowedIPNum":             bs.BorrowedIPsCount,
		}).Warning("not enough available ips to force borrow")
	}

	return eniBorrowedIPNum
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
