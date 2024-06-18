package bandwidth

import (
	"math"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

const (
	latencyInMillis   = 25
	hardwareHeaderLen = 1500
	milliSeconds      = 1000
)

func (manager *BandwidthManager) setVethTC(cctx *link.ContainerContext, opt *ccev2.BindwidthOption) error {
	err := SetupTBFQdisc(cctx.HostDev, uint64(opt.Ingress))
	if err != nil {
		return errors.Wrapf(err, "can not setup tbf qdisc on host device %v/%s", cctx.HostDev.Attrs().Name)
	}
	err = SetupTBFQdisc(cctx.ContainerDev, uint64(opt.Egress))
	if err != nil {
		return errors.Wrapf(err, "can not setup tbf qdisc on container device %v/%s", cctx.ContainerDev.Attrs().Name)
	}
	return nil
}

func SetupTBFQdisc(dev netlink.Link, bandwidthInBytes uint64) error {
	if dev == nil {
		return nil
	}
	if bandwidthInBytes <= 0 {
		return nil
	}
	burst := burst(bandwidthInBytes, dev.Attrs().MTU+hardwareHeaderLen)
	buffer := buffer(bandwidthInBytes, burst)
	latency := latencyInUsec(latencyInMillis)
	limit := limit(bandwidthInBytes, latency, burst)

	tbf := &netlink.Tbf{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: dev.Attrs().Index,
			Handle:    netlink.MakeHandle(1, 0),
			Parent:    netlink.HANDLE_ROOT,
		},
		Rate:     bandwidthInBytes,
		Limit:    uint32(limit),
		Buffer:   uint32(buffer),
		Minburst: uint32(dev.Attrs().MTU),
	}

	if err := netlink.QdiscReplace(tbf); err != nil {
		return errors.Wrapf(err, "can not replace qdics %+v on device %v/%s", tbf, dev.Attrs().Namespace, dev.Attrs().Name)
	}

	return nil
}

func burst(rate uint64, mtu int) uint32 {
	return uint32(math.Ceil(math.Max(float64(rate)/milliSeconds, float64(mtu))))
}

func time2Tick(time uint32) uint32 {
	return uint32(float64(time) * float64(netlink.TickInUsec()))
}

func buffer(rate uint64, burst uint32) uint32 {
	return time2Tick(uint32(float64(burst) * float64(netlink.TIME_UNITS_PER_SEC) / float64(rate)))
}

func limit(rate uint64, latency float64, buffer uint32) uint32 {
	return uint32(float64(rate)*latency/float64(netlink.TIME_UNITS_PER_SEC)) + buffer
}

func latencyInUsec(latencyInMillis float64) float64 {
	return float64(netlink.TIME_UNITS_PER_SEC) * (latencyInMillis / 1000.0)
}
