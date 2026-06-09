package bcesync

import (
	"testing"

	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/stretchr/testify/assert"
)

func TestBorrowedSubnet_Borrow(t *testing.T) {
	mocksbn := ccemock.NewMockSubnet("sbnid", "176.16.0.0/24")
	bs := NewBorrowedSubnet(mocksbn)
	assert.Equal(t, 255, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 0, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	num := bs.Borrow("eni1", 1)
	assert.Equal(t, 1, num)
	assert.Equal(t, 254, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 1, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	num = bs.Borrow("eni1", 2)
	assert.Equal(t, 2, num)
	assert.Equal(t, 253, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 2, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	bs.forceBorrowForENI("eni1", 10)
	assert.Equal(t, 245, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 10, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	bs.Done("eni1", 3)
	assert.Equal(t, 245, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 7, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	bs.Cancel("eni1")
	assert.Equal(t, 252, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 0, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)
}

func Test_restoreBSM(t *testing.T) {
	ccemock.InitMockEnv()
	InitBSM()

	sbn := ccemock.NewMockSubnet("sbn-bbcprimary", "10.128.34.0/24")
	err := ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return
	}

	// to borrow
	nrs := ccemock.NewMockSimpleNrs("10.128.34.56", "bcc")
	nrs.Spec.ENI.MaxIPsPerENI = 40
	nrs.Spec.ENI.MaxAllocateENI = 8
	nrs.Spec.ENI.BurstableMehrfachENI = 1
	nrs.Annotations = map[string]string{}
	err = ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs})
	if !assert.NoError(t, err, "ensure nrs to informer failed") {
		return
	}
	eni, err := ccemock.NewMockEni("10.128.34.56", "i-wqdasds", "sbn-bbcprimary", "10.128.34.0/24", 0)
	eni.Spec.BorrowIPCount = 40
	assert.NoError(t, err, "create eni failed")
	err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni})
	if !assert.NoError(t, err, "ensure enis to informer failed") {
		return
	}

	// no borrowed
	eni, err = ccemock.NewMockEni("10.128.34.56", "i-wqdasds", "sbn-bbcprimary", "10.128.34.0/24", 40)
	assert.NoError(t, err, "create eni failed")
	err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni})
	if !assert.NoError(t, err, "ensure enis to informer failed") {
		return
	}

	InitBSM()

	bsb, err := GlobalBSM().EnsureSubnet(sbn.Spec.VPCID, sbn.Name)
	if !assert.NoError(t, err, "get subnet failed") {
		return
	}

	assert.Equal(t, 40, bsb.BorrowedIPsCount)
	assert.Equal(t, 215, bsb.BorrowedAvailableIPsCount)
}

func TestGetClusterInfoFromSubnet(t *testing.T) {
	tests := []struct {
		name              string
		subnet            *ccev1.Subnet
		clusterID         string
		expectedClusterID string
		expectedRegion    string
	}{
		{
			name: "subnet with availability zone and configured cluster ID",
			subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					AvailabilityZone: "cn-bj-a",
				},
			},
			clusterID:         "test-cluster-123",
			expectedClusterID: "test-cluster-123",
			expectedRegion:    "cn-bj-a",
		},
		{
			name: "subnet with availability zone and default cluster ID",
			subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					AvailabilityZone: "cn-bj-b",
				},
			},
			clusterID:         "",
			expectedClusterID: "default-cluster",
			expectedRegion:    "cn-bj-b",
		},
		{
			name: "subnet without availability zone and configured cluster ID",
			subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					AvailabilityZone: "",
				},
			},
			clusterID:         "test-cluster-456",
			expectedClusterID: "test-cluster-456",
			expectedRegion:    "unknown",
		},
		{
			name: "subnet without availability zone and default cluster ID",
			subnet: &ccev1.Subnet{
				Spec: ccev1.SubnetSpec{
					AvailabilityZone: "",
				},
			},
			clusterID:         "",
			expectedClusterID: "default-cluster",
			expectedRegion:    "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original config
			originalClusterID := option.Config.ClusterID
			defer func() {
				option.Config.ClusterID = originalClusterID
			}()

			// Set test cluster ID
			option.Config.ClusterID = tt.clusterID

			// Call function under test
			clusterID, region := getClusterInfoFromSubnet(tt.subnet)

			// Assert results
			assert.Equal(t, tt.expectedClusterID, clusterID)
			assert.Equal(t, tt.expectedRegion, region)
		})
	}
}

func TestCalculateSubnetIPCount(t *testing.T) {
	tests := []struct {
		name     string
		cidr     string
		expected int64
	}{
		{
			name:     "empty CIDR",
			cidr:     "",
			expected: 0,
		},
		{
			name:     "valid /24 CIDR",
			cidr:     "192.168.1.0/24",
			expected: 254, // 256 - 2 (network and broadcast addresses)
		},
		{
			name:     "valid /16 CIDR",
			cidr:     "10.0.0.0/16",
			expected: 65534, // 65536 - 2 (network and broadcast addresses)
		},
		{
			name:     "valid /8 CIDR",
			cidr:     "172.0.0.0/8",
			expected: 16777214, // 16777216 - 2 (network and broadcast addresses)
		},
		{
			name:     "valid /32 CIDR (single IP)",
			cidr:     "192.168.1.1/32",
			expected: 0, // /32 has no usable IPs (only network address)
		},
		{
			name:     "valid /30 CIDR (small subnet)",
			cidr:     "192.168.1.0/30",
			expected: 2, // 4 - 2 (network and broadcast addresses)
		},
		{
			name:     "invalid CIDR format",
			cidr:     "invalid-cidr",
			expected: 0,
		},
		{
			name:     "invalid CIDR with wrong format",
			cidr:     "192.168.1.1/33",
			expected: 0,
		},
		{
			name:     "invalid CIDR with non-numeric parts",
			cidr:     "192.168.abc.0/24",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call function under test
			result := calculateSubnetIPCount(tt.cidr)

			// Assert result
			assert.Equal(t, tt.expected, result)
		})
	}
}
