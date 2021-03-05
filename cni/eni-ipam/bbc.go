package main

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/types/current"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/rpc"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

type bbcENIMultiIP struct {
	name      string
	namespace string
}

var _ networkClient = &bbcENIMultiIP{}

func (client *bbcENIMultiIP) SetupNetwork(
	ctx context.Context,
	result *current.Result,
	ipamConf *IPAMConf,
	resp *rpc.AllocateIPReply,
) error {
	allocRespNetworkInfo := resp.GetENIMultiIP()

	if allocRespNetworkInfo == nil {
		err := errors.New(fmt.Sprintf("failed to allocate IP for pod (%v %v): NetworkInfo is nil", client.namespace, client.name))
		log.Errorf(ctx, err.Error())
		return err
	}

	log.Infof(ctx, "allocate IP %v for pod(%v %v) successfully", allocRespNetworkInfo.IP, client.namespace, client.name)

	allocatedIP := net.ParseIP(allocRespNetworkInfo.IP)
	if allocatedIP == nil {
		return fmt.Errorf("alloc IP %v format error", allocRespNetworkInfo.IP)
	}
	version := "4"
	addrBits := 32
	if allocatedIP.To4() == nil {
		version = "6"
		addrBits = 128
	}

	result.IPs = []*current.IPConfig{
		{
			Version: version,
			Address: net.IPNet{IP: allocatedIP, Mask: net.CIDRMask(addrBits, addrBits)},
		},
	}

	return nil
}

func (client *bbcENIMultiIP) TeardownNetwork(
	ctx context.Context,
	ipamConf *IPAMConf,
	resp *rpc.ReleaseIPReply,
) error {
	return nil
}
