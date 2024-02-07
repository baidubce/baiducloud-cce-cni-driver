package syncer

import (
	"context"
	"time"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccev2lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
)

// ENIEventHandler should implement the behavior to handle ENI
type ENIEventHandler interface {
	Create(resource *ccev2.ENI) error
	Update(resource *ccev2.ENI) error
	Delete(name string) error
	ResyncENI(context.Context) time.Duration
}

// ENIUpdater interface to update resource
type ENIUpdater interface {
	Lister() ccev2lister.ENILister
	Create(subnet *ccev2.ENI) (*ccev2.ENI, error)
	Update(newResource *ccev2.ENI) (*ccev2.ENI, error)
	Delete(name string) error
	UpdateStatus(newResource *ccev2.ENI) (*ccev2.ENI, error)
}

type ENISyncher interface {
	Init(ctx context.Context) error
	StartENISyncer(context.Context, ENIUpdater) ENIEventHandler
}
