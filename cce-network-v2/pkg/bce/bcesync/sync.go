package bcesync

import (
	"context"
	"sync"
	"time"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	"github.com/baidubce/bce-sdk-go/services/vpc"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	ccev2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

const (
	taskLogField = "task"
)

var log = logging.NewSubysLogger("bce-sync-manager")

// SyncManager synchronize data between k8s and VPC, run in operator
type SyncManager[T eni.Eni | vpc.RouteRule | vpc.Subnet | ccev2alpha1.SecurityGroupSpec] struct {
	sync.Mutex
	pool         map[string]T
	resyncPeriod time.Duration
	resync       func(ctx context.Context) ([]T, error)
	Name         string

	mngr *controller.Manager
}

func NewSyncManager[T eni.Eni | vpc.RouteRule | vpc.Subnet | ccev2alpha1.SecurityGroupSpec](name string, resyncPeriod time.Duration, resync func(ctx context.Context) ([]T, error)) *SyncManager[T] {
	s := &SyncManager[T]{
		pool:         make(map[string]T),
		Name:         name,
		resyncPeriod: resyncPeriod,
		mngr:         controller.NewManager(),
		resync:       resync,
	}
	return s
}

func (s *SyncManager[T]) Run() {
	s.mngr.UpdateController(s.Name,
		controller.ControllerParams{
			RunInterval: s.resyncPeriod,
			DoFunc: func(ctx context.Context) error {
				dataList, err := s.resync(ctx)
				if err != nil {
					log.WithField(taskLogField, s.Name).WithError(err).Error("resync failed")
					return nil
				}

				var pool = make(map[string]T)
				for i := 0; i < len(dataList); i++ {
					key := s.ingestKeywords(&dataList[i])
					if key != "" {
						pool[key] = dataList[i]
					} else {
						log.WithField(taskLogField, s.Name).WithField("item", logfields.Json(&dataList[i])).Info("unknown type. ignored")
					}
				}

				if len(pool) == 0 {
					log.WithField(taskLogField, s.Name).Warning("resync done. nothing to update")
				} else {
					s.Lock()
					s.pool = pool
					s.Unlock()

				}
				log.WithField(taskLogField, s.Name).Debugf("resync done. update count %d", len(s.pool))
				return nil
			},
		})
}

func (s *SyncManager[T]) RunImmediately() {
	s.mngr.TriggerController(s.Name)
}

// ingestKeywords fetch keywords from generic objects
func (s *SyncManager[T]) ingestKeywords(data interface{}) string {
	switch data.(type) {
	case *eni.Eni:
		return data.(*eni.Eni).EniId
	case *vpc.RouteRule:
		return data.(*vpc.RouteRule).RouteTableId + "-" + data.(*vpc.RouteRule).RouteRuleId
	case *vpc.Subnet:
		return data.(*vpc.Subnet).SubnetId
	case *bbc.GetInstanceEniResult:
		return data.(*bbc.GetInstanceEniResult).Id
	case *ccev2alpha1.SecurityGroupSpec:
		return data.(*ccev2alpha1.SecurityGroupSpec).ID
	}
	return ""
}

func (s *SyncManager[T]) AddItems(dataList []T) {
	s.Lock()
	defer s.Unlock()

	for i := 0; i < len(dataList); i++ {
		key := s.ingestKeywords(&dataList[i])
		if key != "" {
			s.pool[key] = dataList[i]
		} else {
			log.WithField(taskLogField, s.Name).WithField("item", logfields.Json(&dataList[i])).Info("unknown type. ignored")
		}
	}
}

func (s *SyncManager[T]) Get(key string) *T {
	s.Lock()
	defer s.Unlock()

	v, ok := s.pool[key]
	if ok {
		return &v
	}
	return nil
}
