package main

import (
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/pkg/errors"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/bandwidth"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

func handleBandwidth(cctx *link.ContainerContext, opt *models.BandwidthOption) error {
	if opt == nil {
		return nil
	}
	switch opt.Mode {
	case ccev2.BindwidthModeEDT:
		logger.Warn("bandwidth mode edt have not been implemented yet")
		return nil
	default:
		switch cctx.Driver {
		case string(models.DatapathModeVeth), "":
			if opt.Ingress > 0 {
				err := bandwidth.SetupTBFQdisc(cctx.HostDev, uint64(opt.Ingress))
				if err != nil {
					return errors.Wrapf(err, "can not setup tbf qdisc on host device %s", cctx.HostDev.Attrs().Name)
				}
			}

			if opt.Egress > 0 {
				err := cctx.ContainerNetns.Do(func(_ ns.NetNS) error {
					return bandwidth.SetupTBFQdisc(cctx.ContainerDev, uint64(opt.Egress))
				})
				if err != nil {
					return errors.Wrapf(err, "can not setup tbf qdisc on container device %s", cctx.ContainerDev.Attrs().Name)
				}
			}
		}

	}
	return nil
}
