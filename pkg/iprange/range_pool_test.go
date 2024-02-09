package iprange

import (
	"net"
	"testing"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	"github.com/stretchr/testify/suite"
	"k8s.io/apimachinery/pkg/labels"
	k8sutilnet "k8s.io/utils/net"
)

type CustomRangePoolTest struct {
	suite.Suite
}

// 每次测试前设置上下文
func (suite *CustomRangePoolTest) SetupTest() {

}

// 模拟全过滤器
type mockFilter struct{}

func (*mockFilter) FilterIP(ip net.IP) bool {
	return true
}

func (suite *CustomRangePoolTest) TestSimpleRangePool() {
	sbn := data.MockSubnet("default", "sbn-test", "10.7.8.0/24")
	l := labels.Set{"app": "demo"}
	psts := data.MockPodSubnetTopologySpreadWithSubnet("default", "psts-test", sbn, l)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:                 networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy:      networkingv1alpha1.ReleaseStrategyTTL,
		TTL:                  networkingv1alpha1.DefaultReuseIPTTL,
		EnableReuseIPAddress: true,
	}

	// case1: with start and end
	allocation := psts.Spec.Subnets["sbn-test"]
	custom := networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				Start: "10.7.8.56",
				End:   "10.7.8.57",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation
	filter := []IPFilter{&mockFilter{}}
	pool, err := NewCustomRangePool(psts, filter...)
	if suite.NoErrorf(err, "create pool error") {
		if suite.Truef(pool.IPInRange("10.7.8.56"), "ip not in range") {
			suite.Equalf("10.7.8.56", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
		}
	}

	// case2: no start and end
	custom = networkingv1alpha1.CustomAllocation{
		Family:        k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{},
	}
	allocation.Custom = []networkingv1alpha1.CustomAllocation{custom}
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCustomRangePool(psts, filter...)
	if suite.NoErrorf(err, "create pool error") {
		if suite.Truef(pool.IPInRange("10.7.8.56"), "ip should in range") {
			suite.Equalf("10.7.8.2", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
		}
		suite.Falsef(pool.IPInRange("10.7.7.56"), "ip should not in range")
	}

	// case 3: mutiple custom ip range
	allocation = psts.Spec.Subnets["sbn-test"]
	custom = networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				Start: "10.7.8.56",
				End:   "10.7.8.190",
			},
			{
				Start: "10.7.8.58",
				End:   "10.7.8.190",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCustomRangePool(psts, filter...)
	suite.Errorf(err, "overlapping error")

	allocation = psts.Spec.Subnets["sbn-test"]
	custom = networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				End: "10.7.8.190",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCustomRangePool(psts, filter...)
	suite.Errorf(err, "start is nil")

	// case 5: no range
	allocation.Custom = []networkingv1alpha1.CustomAllocation{}
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCustomRangePool(psts, filter...)
	if suite.NoErrorf(err, "create pool error") {
		if suite.Truef(pool.IPInRange("10.7.8.56"), "ip should in range") {
			suite.Equalf("10.7.8.2", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
		}
		suite.Falsef(pool.IPInRange("10.7."), "ip should in range")
		suite.Falsef(pool.IPInRange("10.7.7.56"), "ip should not in range")
	}
}

func (suite *CustomRangePoolTest) TestSimpleRangePoolNoStartAndEnd() {
	sbn := data.MockSubnet("default", "sbn-test", "10.7.8.0/24")
	l := labels.Set{"app": "demo"}
	psts := data.MockPodSubnetTopologySpreadWithSubnet("default", "psts-test", sbn, l)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:                 networkingv1alpha1.IPAllocTypeCustom,
		ReleaseStrategy:      networkingv1alpha1.ReleaseStrategyTTL,
		TTL:                  networkingv1alpha1.DefaultReuseIPTTL,
		EnableReuseIPAddress: true,
	}
	allocation := psts.Spec.Subnets["sbn-test"]
	custom := networkingv1alpha1.CustomAllocation{
		Family: k8sutilnet.IPv4,
		CustomIPRange: []networkingv1alpha1.CustomIPRange{
			{
				Start: "10.7.8.56",
				End:   "10.7.8.57",
			},
		},
	}
	allocation.Custom = append(allocation.Custom, custom)
	psts.Spec.Subnets["sbn-test"] = allocation
	filter := []IPFilter{&mockFilter{}}
	pool, err := NewCustomRangePool(psts, filter...)
	if suite.NoErrorf(err, "create pool error") {
		if suite.Truef(pool.IPInRange("10.7.8.56"), "ip not in range") {
			suite.Equalf("10.7.8.56", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
		}
	}

	// case no subnet
	psts.Spec.Subnets = make(map[string]networkingv1alpha1.SubnetAllocation)
	pool, err = NewCustomRangePool(psts, filter...)
	suite.Errorf(err, "create pool error")
}

func (suite *CustomRangePoolTest) TestSimpleCIDRPool() {
	sbn := data.MockSubnet("default", "sbn-test", "10.7.8.0/24")
	l := labels.Set{"app": "demo"}
	psts := data.MockPodSubnetTopologySpreadWithSubnet("default", "psts-test", sbn, l)
	psts.Spec.Strategy = &networkingv1alpha1.IPAllocationStrategy{
		Type:            networkingv1alpha1.IPAllocTypeManual,
		ReleaseStrategy: networkingv1alpha1.ReleaseStrategyTTL,
	}
	allocation := psts.Spec.Subnets["sbn-test"]

	allocation.IPv4Range = []string{"10.7.8.4/30"}

	psts.Spec.Subnets["sbn-test"] = allocation
	filter := []IPFilter{&mockFilter{}}
	pool, err := NewCIDRRangePool(psts, filter...)
	if suite.NoErrorf(err, "create pool error") {
		if suite.Truef(pool.IPInRange("10.7.8.4"), "ip not in range") {
			suite.Equalf("10.7.8.4", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
		}
	}

	// case with cidr
	allocation.IPv4Range = []string{}
	allocation.IPv4 = []string{}
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCIDRRangePool(psts, filter...)
	if suite.NoErrorf(err, "create pool error") {
		if suite.Truef(pool.IPInRange("10.7.8.2"), "ip not in range") {
			suite.Equalf("10.7.8.2", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
		}
	}

	// case 2: with ip list
	allocation.IPv4Range = []string{"10.7.8.4/30"}
	allocation.IPv4 = []string{"10.7.8.128", "10.7.8.192"}
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCIDRRangePool(psts, filter...)
	if suite.NoErrorf(err, "create pool error") {
		_ = pool.String()
		if suite.Falsef(pool.IPInRange("10.7.8.10"), "ip not in range") {
			suite.Equalf("10.7.8.4", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
			suite.Equalf("10.7.8.4", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
			suite.Equalf("10.7.8.4", pool.FirstAvailableIP("sbn-test").String(), "first ip not euqal")
		}
		suite.Falsef(pool.IPInRange("10.7.8.129"), "ip not in range")
		suite.Truef(pool.IPInRange("10.7.8.128"), "ip not in range")
	}

	// case 3: with ip list
	allocation.IPv4Range = []string{"10.7.8.4/30"}
	allocation.IPv4 = []string{"10.7.8.7", "10.7.8.19"}
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCIDRRangePool(psts, filter...)
	suite.Errorf(err, "create pool error")

	// case 4: not in range
	allocation.IPv4Range = []string{"10.7.8.4/30"}
	allocation.IPv4 = []string{"10.7.7.7", "10.7.8.19"}
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCIDRRangePool(psts, filter...)
	suite.Errorf(err, "create pool error")

	// case 5 : cidr not in range
	allocation.IPv4Range = []string{"10.7.7.4/30"}
	allocation.IPv4 = []string{"10.7.8.7", "10.7.8.19"}
	psts.Spec.Subnets["sbn-test"] = allocation
	pool, err = NewCIDRRangePool(psts, filter...)
	suite.Errorf(err, "create pool error")

	// case 6 : no subnet
	psts.Spec.Subnets = make(map[string]networkingv1alpha1.SubnetAllocation)
	pool, err = NewCIDRRangePool(psts, filter...)
	suite.Errorf(err, "create pool error")
}

func TestIPRange(t *testing.T) {
	suite.Run(t, new(CustomRangePoolTest))
}
