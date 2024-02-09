package bcc

import (
	"context"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/iprange"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

// customAllocateIPCrossSubnet
func (ipam *IPAM) customAllocateIPCrossSubnet(ctx context.Context, toAllocate *ipToAllocate, oldWep *networkingv1alpha1.WorkloadEndpoint) error {
	var err error
	toAllocate.iprange, err = iprange.NewCustomRangePool(toAllocate.psts, ipam)
	if err != nil {
		return err
	}

	if networking.OwnerByPodSubnetTopologySpread(oldWep, toAllocate.psts) && networking.IsReuseIPCustomPSTS(toAllocate.psts) {
		ipam.tryDeleteSubnetIPRetainAllocateCache(ctx, oldWep)

		if toAllocate.iprange.IPInRange(oldWep.Spec.IP) {
			toAllocate.ipv4 = oldWep.Spec.IP
			toAllocate.sbnID = oldWep.Spec.SubnetID
		} else {
			log.Warningf(ctx, "old ip %s not in psts range", oldWep.Spec.IP)
		}
	}
	return ipam.manualAllocateIPCrossSubnet(ctx, toAllocate)
}

// FilterIP return true if ip was not used
func (ipam *IPAM) FilterIP(ip net.IP) bool {
	if ipam.allocated.Exists(ip.String()) || ipam.reusedIPs.Exists(ip.String()) {
		return false
	}
	return true
}
