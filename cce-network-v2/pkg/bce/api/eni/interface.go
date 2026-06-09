package eni

import (
	"github.com/baidubce/bce-sdk-go/services/eni"
)

// ClientInterface defines the interface for ENI client operations
// This interface includes both the local client methods and the embedded eni.Client methods
//
//go:generate mockgen -destination=./mock/mock.go -package=mock -source=./interface.go
type ClientInterface interface {
	// Local client methods
	ListEnis(args *eni.ListEniArgs) (*eni.ListEniResult, error)
	ListEris(args *eni.ListEniArgs) (*eni.ListEniResult, error)

	// Embedded eni.Client methods
	CreateEni(args *eni.CreateEniArgs) (*eni.CreateEniResult, error)
	UpdateEni(args *eni.UpdateEniArgs) error
	DeleteEni(args *eni.DeleteEniArgs) error
	ListEni(args *eni.ListEniArgs) (*eni.ListEniResult, error)
	GetEniDetail(eniId string) (*eni.Eni, error)
	AddPrivateIp(args *eni.EniPrivateIpArgs) (*eni.AddPrivateIpResult, error)
	BatchAddPrivateIp(args *eni.EniBatchPrivateIpArgs) (*eni.BatchAddPrivateIpResult, error)
	BatchAddPrivateIpCrossSubnet(args *eni.EniBatchAddPrivateIpCrossSubnetArgs) (*eni.BatchAddPrivateIpResult, error)
	DeletePrivateIp(args *eni.EniPrivateIpArgs) error
	BatchDeletePrivateIp(args *eni.EniBatchPrivateIpArgs) error
	AttachEniInstance(args *eni.EniInstance) error
	DetachEniInstance(args *eni.EniInstance) error
	BindEniPublicIp(args *eni.BindEniPublicIpArgs) error
	UnBindEniPublicIp(args *eni.UnBindEniPublicIpArgs) error
	UpdateEniSecurityGroup(args *eni.UpdateEniSecurityGroupArgs) error
	UpdateEniEnterpriseSecurityGroup(args *eni.UpdateEniEnterpriseSecurityGroupArgs) error
	GetEniQuota(args *eni.EniQuoteArgs) (*eni.EniQuoteInfo, error)
	GetEniStatus(eniId string) (*eni.EniStatusInfo, error)
}
