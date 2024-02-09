package client

import (
	"context"
)

type EniResult struct {
	EniID        string
	MacAddress   string
	PrivateIPSet []PrivateIP
}

type PrivateIP struct {
	Primary          bool
	PrivateIPAddress string
}

type IaaSClient interface {
	ListEnis(ctx context.Context, vpcID, instanceID string) ([]EniResult, error)
	AddPrivateIP(ctx context.Context, eniID, privateIP string) (string, error)
	DeletePrivateIP(ctx context.Context, eniID, privateIP string) error

	GetMwepType() string
}
