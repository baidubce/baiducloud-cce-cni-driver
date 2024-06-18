package client

import (
	"context"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

var (
	log = logging.NewSubysLogger("bce-rdma-client")
)

const (
	IntTypeERI = string(ccev2.ENIForERI)
	IntTypeHPC = string(ccev2.ENIForHPC)
)

type EniResult struct {
	Type         string
	Id           string
	MacAddress   string
	VpcID        string
	SubnetID     string
	ZoneName     string
	PrivateIpSet []PrivateIP
}

type PrivateIP struct {
	Primary          bool
	PrivateIpAddress string
}

type IaaSClient interface {
	ListEnis(ctx context.Context, vpcID, instanceID string) ([]EniResult, error)
	AddPrivateIP(ctx context.Context, eniID, privateIP string) (string, error)
	DeletePrivateIP(ctx context.Context, eniID, privateIP string) error
	BatchAddPrivateIP(ctx context.Context, eniID string, privateIPs []string, count int) ([]string, error)
	BatchDeletePrivateIP(ctx context.Context, eniID string, privateIPs []string) error

	GetRDMAIntType() string
}
