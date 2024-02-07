package privatecloudbase

import (
	"context"
	"fmt"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase/api"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

var _ endpoint.DirectIPAllocator = &InstancesManager{}

// NodeEndpoint implements endpoint.DirectIPAllocator
func (m *InstancesManager) NodeEndpoint(cep *ccev2.CCEEndpoint) (endpoint.DirectEndpointOperation, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	node, ok := m.nodeMap[cep.Spec.Network.IPAllocation.NodeIP]
	if !ok {
		return nil, fmt.Errorf("node %s not found", cep.Spec.Network.IPAllocation.NodeIP)
	}
	return &fixedIPOperation{
		InstancesManager: m,
		subnetID:         node.subnetID,
	}, nil
}

// ResyncPool implements endpoint.DirectIPAllocator
func (*InstancesManager) ResyncPool(ctx context.Context, scopedLog *logrus.Entry) (map[string]ipamTypes.AllocationMap, error) {
	return nil, nil
}

type fixedIPOperation struct {
	*InstancesManager
	subnetID string
}

// FilterAvailableSubnet implements endpoint.DirectEndpointOperation
func (*fixedIPOperation) FilterAvailableSubnet(param []*ccev1.Subnet) []*ccev1.Subnet {
	return param
}

// AllocateIP When the fixed IP pod is scheduled for the first time,
// it is used to assign an available IP address to the fixed IP pod.
func (m *fixedIPOperation) AllocateIP(ctx context.Context, allocation *endpoint.DirectIPAction) error {
	var allocatedIP *api.AllocatedIP

	// reuse ip
	if len(allocation.Addressing) > 0 {
		addr := allocation.Addressing[0]
		allocatedIP = m.reuseIPMap.GetIP(m.subnetID, addr.IP)
	}

	// allocate a new ip
	if allocatedIP == nil {
		req := api.AcquireIPBySubnetRequest{
			Subnet: m.subnetID,
			ID:     api.PodID(allocation.Owner),
		}
		allocated, err := m.allocateFixedIPWithRetry(ctx, req)
		if err != nil {
			return err
		}
		allocatedIP = &allocated.AllocatedIP
	}

	addr := &ccev2.AddressPair{
		IP:      allocatedIP.IP,
		Family:  ccev2.IPv4Family,
		Gateway: allocatedIP.GW,
		CIDRs:   []string{allocatedIP.CIDR},
	}

	allocation.NodeSelectorRequirement = newSubnetAffinity(m.subnetID)
	// save ip result to reuse ip map
	m.reuseIPMap.AddIP(allocatedIP)

	sbn := m.GetSubnet(m.subnetID)
	if sbn != nil {
		addr.CIDRs = []string{sbn.CIDR}
	}
	allocation.Addressing = []*ccev2.AddressPair{addr}
	return nil
}

func (m *fixedIPOperation) allocateFixedIPWithRetry(ctx context.Context, req api.AcquireIPBySubnetRequest) (*api.AllocatedIPResponse, error) {
	allocated, err := m.api.AcquireIPBySubnet(ctx, req)
	if err != nil && strings.Contains(err.Error(), "is not same as the request") {
		rr := &endpoint.DirectIPAction{
			Addressing: []*ccev2.AddressPair{{
				IP: allocated.AllocatedIP.IP,
			}},
		}
		_ = m.DeleteIP(ctx, rr)
		return m.api.AcquireIPBySubnet(ctx, req)

	}
	return allocated, nil
}

func (m *fixedIPOperation) DeleteIP(ctx context.Context, allocation *endpoint.DirectIPAction) error {
	sbnID := m.subnetID
	for _, addr := range allocation.Addressing {

		allocated := m.reuseIPMap.GetIP(sbnID, addr.IP)
		if allocated != nil {
			req := api.BatchReleaseIPRequest{
				IPs: []string{addr.IP},
				VPC: allocated.VPC,
			}
			// 1. release ip from iaas api
			err := m.api.BatchReleaseIP(ctx, req)
			if err != nil {
				return err
			}

			m.reuseIPMap.DeleteByIP(sbnID, addr.IP)
			m.IPPool.DeleteByIP(sbnID, addr.IP)
		}
	}
	return nil
}

func (m *InstancesManager) ResyncFixedIPs(ctx context.Context, scopedLog *logrus.Entry) (map[string]ipamTypes.AllocationMap, error) {
	return m.reuseIPMap.toAllocationMap(), nil
}

func newSubnetAffinity(subnet string) []corev1.NodeSelectorRequirement {
	requirement := corev1.NodeSelectorRequirement{
		Key:      LabelPrivateCloudBaseTopologySwitchName,
		Operator: corev1.NodeSelectorOpIn,
		Values:   []string{subnet},
	}
	return []corev1.NodeSelectorRequirement{requirement}
}
