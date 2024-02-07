package syncer

import (
	"context"
	"time"

	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev1lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
)

// SubnetEventHandler should implement the behavior to handle Subnet
type SubnetEventHandler interface {
	Create(resource *ccev1.Subnet) error
	Update(resource *ccev1.Subnet) error
	Delete(name string) error
	ResyncSubnet(context.Context) time.Duration
}

// SubnetUpdater interface to update resource
type SubnetUpdater interface {
	Lister() ccev1lister.SubnetLister
	Create(subnet *ccev1.Subnet) (*ccev1.Subnet, error)
	Update(newResource *ccev1.Subnet) (*ccev1.Subnet, error)
	Delete(name string) error
	UpdateStatus(newResource *ccev1.Subnet) (*ccev1.Subnet, error)
}

type SubnetSyncher interface {
	Init(ctx context.Context) error
	StartSubnetSyncher(context.Context, SubnetUpdater) SubnetEventHandler
}
