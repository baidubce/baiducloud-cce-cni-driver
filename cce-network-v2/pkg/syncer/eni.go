package syncer

import (
	"context"
	"time"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccev2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	ccev2lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	ccev2alpha1lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2alpha1"
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

// SecurityGroupUpdater interface to update resource
type SecurityGroupUpdater interface {
	Lister() ccev2alpha1lister.SecurityGroupLister
	Create(subnet *ccev2alpha1.SecurityGroup) (*ccev2alpha1.SecurityGroup, error)
	Update(newResource *ccev2alpha1.SecurityGroup) (*ccev2alpha1.SecurityGroup, error)
	Delete(name string) error
	UpdateStatus(newResource *ccev2alpha1.SecurityGroup) (*ccev2alpha1.SecurityGroup, error)
}

type SecurityGroupSyncher interface {
	Init(ctx context.Context) error
	StartSecurityGroupSyncer(context.Context, SecurityGroupUpdater) SecurityGroupEventHandler
}

// SecurityGroupEventHandler should implement the behavior to handle SecurityGroup
type SecurityGroupEventHandler interface {
	Create(resource *ccev2alpha1.SecurityGroup) error
	Update(resource *ccev2alpha1.SecurityGroup) error
	Delete(name string) error
	ResyncSecurityGroup(context.Context) time.Duration
}
